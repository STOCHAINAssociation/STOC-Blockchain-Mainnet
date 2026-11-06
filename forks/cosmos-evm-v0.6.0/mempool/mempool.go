package mempool

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/holiman/uint256"

	cmtmempool "github.com/cometbft/cometbft/mempool"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/mempool/miner"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
)

var _ sdkmempool.ExtMempool = &ExperimentalEVMMempool{}

// AllowUnsafeSyncInsert makes Insert wait for the legacypool reorg to finish.
// Tests only. In production it must stay false: a sync Insert runs inside
// CheckTx (CometBFT local-client mutex held) and the reorg it waits on calls
// BroadcastTxFn -> BroadcastTxSync -> CheckTx on the same mutex = deadlock
// (upstream cosmos/evm #890).
var AllowUnsafeSyncInsert = false

const (
	// SubscriberName is the name of the event bus subscriber for the EVM mempool
	SubscriberName = "evm"
	// fallbackBlockGasLimit is the default block gas limit is 0 or missing in genesis file
	fallbackBlockGasLimit = 100_000_000
	// orphanDecodeCacheSize bounds the LRU cache that memoizes IsOrphanEVMTx
	// decode results (SA-M7 audit-2026-05-29). Sized to cover full CometBFT
	// mempool worst-case (5000 cosmos + ~5000 EVM) plus headroom for in-flight
	// recheck churn so steady-state hit rate ≈ 100%.
	orphanDecodeCacheSize = 16384
)

// orphanDecodeEntry caches the decoded shape of a tx byte sequence used by
// IsOrphanEVMTx. Only the EVM tx hash + isEVM flag are cached; the
// legacypool.Has() check is still performed on every call so cache entries
// stay valid across legacypool mutations.
type orphanDecodeEntry struct {
	ethHash ethcommon.Hash
	isEVM   bool
}

type (
	// ExperimentalEVMMempool is a unified mempool that manages both EVM and Cosmos SDK transactions.
	// It provides a single interface for transaction insertion, selection, and removal while
	// maintaining separate pools for EVM and Cosmos transactions. The mempool handles
	// fee-based transaction prioritization and manages nonce sequencing for EVM transactions.
	ExperimentalEVMMempool struct {
		/** Keepers **/
		vmKeeper VMKeeperI

		/** Mempools **/
		txPool       *txpool.TxPool
		legacyTxPool *legacypool.LegacyPool
		cosmosPool   sdkmempool.ExtMempool

		/** Utils **/
		logger        log.Logger
		txConfig      client.TxConfig
		blockchain    *Blockchain
		blockGasLimit uint64 // Block gas limit from consensus parameters
		minTip        *uint256.Int

		// clientCtx is used to re-broadcast txs promoted from queued to
		// pending. It is set after CometBFT starts (SetClientCtx); the value
		// passed to the constructor has no RPC client yet (upstream #890).
		clientCtxMu sync.RWMutex
		clientCtx   client.Context

		/** Verification **/
		anteHandler sdk.AnteHandler

		/** Concurrency **/
		mtx sync.Mutex

		// eventBus is read lock-free by HasEventBus, which x/vm EndBlock
		// calls inside FinalizeBlock (STOChain fix: never wait on eventMu).
		eventBus atomic.Pointer[cmttypes.EventBus]

		// orphanDecodeCache memoizes IsOrphanEVMTx decode results (SA-M7).
		// SA-AUDIT-2026-06-05 H1: keyed by sha256(txBytes) ([32]byte) instead
		// of xxhash64 to eliminate the ~2^-32 collision risk that could let a
		// crafted-pair attacker invert the orphan verdict for an in-pool tx.
		// sha256 cost (~50ns/200B tx) is negligible vs the proto decode it
		// memoizes (~10μs). Value caches the decoded shape so we skip repeated
		// proto unmarshals during the RecheckTx burst that fires every block.
		orphanDecodeCache *lru.Cache[[32]byte, orphanDecodeEntry]

		// recheckRebroadcast re-gossips EVM txs that CometBFT rejected or
		// dropped (CheckTx/Recheck) for a temporary reason while the
		// legacypool kept them (STOChain fix, see recheck_rebroadcast.go).
		recheckRebroadcast *recheckRebroadcaster

		// STOChain fix (M5): eventMu serialises SetEventBus/Close and guards
		// eventStop; the new-block loop re-subscribes when the event bus
		// cancels it.
		eventMu           sync.Mutex
		eventStop         chan struct{}
		eventResubscribes atomic.Int64

		// STOChain fix (M3): journal persists legacypool txs across a
		// graceful restart (nil: disabled). customBroadcast is true when
		// the config supplied BroadCastTxFn.
		journal         *mempoolJournal
		customBroadcast bool

		// startupContext primes the pool after a restart (see
		// EVMMempoolConfig.StartupContext).
		startupContext func() (sdk.Context, error)

		closeOnce sync.Once
		closeErr  error

		// removePanicHeight is the last height a recovered panic in the
		// Remove ante check was logged at (log once per height).
		removePanicHeight atomic.Int64
	}
)

// EVMMempoolConfig contains configuration options for creating an EVMsdkmempool.
// It allows customization of the underlying mempools, verification functions,
// and broadcasting functions used by the sdkmempool.
type EVMMempoolConfig struct {
	LegacyPoolConfig *legacypool.Config
	CosmosPoolConfig *sdkmempool.PriorityNonceMempoolConfig[math.Int]
	AnteHandler      sdk.AnteHandler
	BroadCastTxFn    func(txs []*ethtypes.Transaction) error
	BlockGasLimit    uint64 // Block gas limit from consensus parameters
	MinTip           *uint256.Int
	// StartupContext, if set, returns a read-only context at the latest
	// committed height that works before the first block after a restart,
	// when the regular query context still fails (STOChain fix). It is only
	// used to prime the pool at startup.
	StartupContext func() (sdk.Context, error)
	// JournalPath is the file the legacypool txs are written to on Close and
	// re-submitted from after restart (STOChain fix). Empty disables it.
	JournalPath string
}

// NewExperimentalEVMMempool creates a new unified mempool for EVM and Cosmos transactions.
// It initializes both EVM and Cosmos transaction pools, sets up blockchain interfaces,
// and configures fee-based prioritization. The config parameter allows customization
// of pools and verification functions, with sensible defaults created if not provided.
func NewExperimentalEVMMempool(
	getCtxCallback func(height int64, prove bool) (sdk.Context, error),
	logger log.Logger,
	vmKeeper VMKeeperI,
	feeMarketKeeper FeeMarketKeeperI,
	txConfig client.TxConfig,
	clientCtx client.Context,
	config *EVMMempoolConfig,
	cosmosPoolMaxTx int,
) *ExperimentalEVMMempool {
	var (
		cosmosPool sdkmempool.ExtMempool
		blockchain *Blockchain
	)

	// add the mempool name to the logger
	logger = logger.With(log.ModuleKey, "ExperimentalEVMMempool")

	logger.Debug("creating new EVM mempool")

	if config == nil {
		panic("config must not be nil")
	}

	if config.BlockGasLimit == 0 {
		logger.Warn("block gas limit is 0, setting to fallback", "fallback_limit", fallbackBlockGasLimit)
		config.BlockGasLimit = fallbackBlockGasLimit
	}

	blockchain = NewBlockchain(getCtxCallback, logger, vmKeeper, feeMarketKeeper, config.BlockGasLimit)

	// Create txPool from configuration
	legacyConfig := legacypool.DefaultConfig
	if config.LegacyPoolConfig != nil {
		legacyConfig = *config.LegacyPoolConfig
	}

	legacyPool := legacypool.New(legacyConfig, blockchain)

	txPool, err := txpool.New(uint64(0), blockchain, []txpool.SubPool{legacyPool})
	if err != nil {
		panic(err)
	}

	if len(txPool.Subpools) != 1 {
		panic("tx pool should contain one subpool")
	}
	if _, ok := txPool.Subpools[0].(*legacypool.LegacyPool); !ok {
		panic("tx pool should contain only legacypool")
	}

	// TODO: move this logic to evmd.createMempoolConfig and set the max tx there
	// Create Cosmos Mempool from configuration
	cosmosPoolConfig := config.CosmosPoolConfig
	if cosmosPoolConfig == nil {
		// Default configuration
		defaultConfig := sdkmempool.PriorityNonceMempoolConfig[math.Int]{}
		defaultConfig.TxPriority = sdkmempool.TxPriority[math.Int]{
			GetTxPriority: func(goCtx context.Context, tx sdk.Tx) math.Int {
				ctx := sdk.UnwrapSDKContext(goCtx)
				cosmosTxFee, ok := tx.(sdk.FeeTx)
				if !ok {
					return math.ZeroInt()
				}
				found, coin := cosmosTxFee.GetFee().Find(vmKeeper.GetEvmCoinInfo(ctx).Denom)
				if !found {
					return math.ZeroInt()
				}

				gasPrice := coin.Amount.Quo(math.NewIntFromUint64(cosmosTxFee.GetGas()))

				return gasPrice
			},
			Compare: func(a, b math.Int) int {
				return a.BigInt().Cmp(b.BigInt())
			},
			MinValue: math.ZeroInt(),
		}
		cosmosPoolConfig = &defaultConfig
	}

	cosmosPoolConfig.MaxTx = cosmosPoolMaxTx
	cosmosPool = sdkmempool.NewPriorityMempool(*cosmosPoolConfig)

	decodeCache, err := lru.New[[32]byte, orphanDecodeEntry](orphanDecodeCacheSize)
	if err != nil {
		// lru.New only errors on size <= 0; orphanDecodeCacheSize is a positive const.
		panic(fmt.Sprintf("failed to create orphan decode LRU cache: %v", err))
	}

	evmMempool := &ExperimentalEVMMempool{
		vmKeeper:          vmKeeper,
		txPool:            txPool,
		legacyTxPool:      txPool.Subpools[0].(*legacypool.LegacyPool),
		cosmosPool:        cosmosPool,
		logger:            logger,
		txConfig:          txConfig,
		blockchain:        blockchain,
		blockGasLimit:     config.BlockGasLimit,
		minTip:            config.MinTip,
		anteHandler:       config.AnteHandler,
		orphanDecodeCache: decodeCache,
		clientCtx:         clientCtx,
		startupContext:    config.StartupContext,
	}

	// Set up the broadcast function. The default reads the client context at
	// call time, so the context installed by SetClientCtx after CometBFT
	// starts is the one used (upstream #890: the captured value was empty and
	// every re-broadcast failed with "no RPC client is defined").
	if config.BroadCastTxFn != nil {
		legacyPool.BroadcastTxFn = config.BroadCastTxFn
		evmMempool.customBroadcast = true
	} else {
		// The EVM mempool broadcasts transactions when it promotes them
		// from queued into pending, noting their readiness to be executed.
		legacyPool.BroadcastTxFn = evmMempool.defaultBroadcastTxFn
	}

	evmMempool.recheckRebroadcast = &recheckRebroadcaster{
		logger:          logger,
		set:             newRecheckDroppedSet(recheckDroppedCap),
		getTx:           evmMempool.legacyTxPool.Get,
		baseFee:         evmMempool.recheckBaseFee,
		checkIncludable: evmMempool.recheckAnteCheck,
		broadcast:       evmMempool.recheckBroadcast,
	}
	if config.JournalPath != "" {
		evmMempool.journal = newMempoolJournal(config.JournalPath)
	}

	vmKeeper.SetEvmMempool(evmMempool)

	return evmMempool
}

// GetBlockchain returns the blockchain interface used for chain head event notifications.
// This is primarily used to notify the mempool when new blocks are finalized.
func (m *ExperimentalEVMMempool) GetBlockchain() *Blockchain {
	return m.blockchain
}

// GetTxPool returns the underlying EVM txpool.
// This provides direct access to the EVM-specific transaction management functionality.
func (m *ExperimentalEVMMempool) GetTxPool() *txpool.TxPool {
	return m.txPool
}

// SetClientCtx sets the client context used to re-broadcast promoted txs.
// Call it once the CometBFT node is running and the context has an RPC client.
// STOChain fix (M3): the first context with an RPC client also starts the
// one-time, asynchronous re-submission of the txs journaled on shutdown.
func (m *ExperimentalEVMMempool) SetClientCtx(clientCtx client.Context) {
	m.clientCtxMu.Lock()
	m.clientCtx = clientCtx
	m.clientCtxMu.Unlock()
	if clientCtx.Client != nil {
		m.startJournalReplay()
	}
}

func (m *ExperimentalEVMMempool) getClientCtx() client.Context {
	m.clientCtxMu.RLock()
	defer m.clientCtxMu.RUnlock()
	return m.clientCtx
}

// rejectBelowBaseFee rejects an EVM tx whose fee cap is below the current base
// fee. Such a tx is filtered out of every proposal (txPool.Pending with
// BaseFee) yet would sit in the pool, so the sender sees it as pending
// forever. Backport of upstream cosmos/evm #1223 / #1233.
func (m *ExperimentalEVMMempool) rejectBelowBaseFee(ethTx *ethtypes.Transaction) error {
	baseFee := m.blockchain.CurrentBlock().BaseFee
	if baseFee != nil && ethTx.GasFeeCapIntCmp(baseFee) < 0 {
		return sdkerrors.ErrInsufficientFee.Wrapf(
			"max fee per gas (%s) is lower than the base fee (%s)",
			ethTx.GasFeeCap(), baseFee,
		)
	}
	return nil
}

// Insert adds a transaction to the appropriate mempool (EVM or Cosmos).
// EVM transactions are routed to the EVM transaction pool, while all other
// transactions are inserted into the Cosmos sdkmempool. The method assumes
// transactions have already passed CheckTx validation.
func (m *ExperimentalEVMMempool) Insert(goCtx context.Context, tx sdk.Tx) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	ctx := sdk.UnwrapSDKContext(goCtx)
	blockHeight := ctx.BlockHeight()

	m.logger.Debug("inserting transaction into mempool", "block_height", blockHeight)
	ethMsg, err := m.getEVMMessage(tx)
	if err == nil {
		// Insert into EVM pool
		hash := ethMsg.Hash()
		m.logger.Debug("inserting EVM transaction", "tx_hash", hash)
		ethTx := ethMsg.AsTransaction()
		if err := m.rejectBelowBaseFee(ethTx); err != nil {
			m.logger.Debug("rejecting EVM transaction below base fee", "error", err, "tx_hash", hash)
			return err
		}
		ethTxs := []*ethtypes.Transaction{ethTx}
		// Async add (upstream #890): a sync add deadlocks CheckTx against the
		// promotion re-broadcast. Validation errors are still returned here;
		// only the reorg/promotion runs in the background.
		errs := m.txPool.Add(ethTxs, AllowUnsafeSyncInsert)
		if len(errs) > 0 && errs[0] != nil {
			if errors.Is(errs[0], legacypool.ErrPoolNotReady) {
				m.logger.Debug("EVM mempool not ready, rejecting transaction", "tx_hash", hash)
				return errs[0]
			}
			m.logger.Error("failed to insert EVM transaction", "error", errs[0], "tx_hash", hash)
			return errs[0]
		}
		m.logger.Debug("EVM transaction inserted successfully", "tx_hash", hash)
		return nil
	}

	// Insert into cosmos pool for non-EVM transactions
	m.logger.Debug("inserting Cosmos transaction", "error", err)
	err = m.cosmosPool.Insert(goCtx, tx)
	if err != nil {
		m.logger.Error("failed to insert Cosmos transaction", "error", err)
	} else {
		m.logger.Debug("Cosmos transaction inserted successfully")
	}
	return err
}

// InsertInvalidNonce handles transactions that failed with nonce gap errors.
// It attempts to insert EVM transactions into the pool as non-local transactions,
// allowing them to be queued for future execution when the nonce gap is filled.
// Non-EVM transactions are discarded as regular Cosmos flows do not support nonce gaps.
func (m *ExperimentalEVMMempool) InsertInvalidNonce(txBytes []byte) error {
	tx, err := m.txConfig.TxDecoder()(txBytes)
	if err != nil {
		return err
	}

	var ethTxs []*ethtypes.Transaction
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return fmt.Errorf("%w, got %d", ErrExpectedOneMessage, len(msgs))
	}
	for _, msg := range tx.GetMsgs() {
		ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
		if ok {
			ethTx := ethMsg.AsTransaction()
			if err := m.rejectBelowBaseFee(ethTx); err != nil {
				return err
			}
			ethTxs = append(ethTxs, ethTx)
			continue
		}
	}
	errs := m.txPool.Add(ethTxs, false)
	if errs != nil {
		if len(errs) != 1 {
			return fmt.Errorf("%w, got %d", ErrExpectedOneError, len(errs))
		}
		return errs[0]
	}
	return nil
}

// Select returns a unified iterator over both EVM and Cosmos transactions.
// The iterator prioritizes transactions based on their fees and manages proper
// sequencing. The i parameter contains transaction hashes to exclude from selection.
func (m *ExperimentalEVMMempool) Select(goCtx context.Context, i [][]byte) sdkmempool.Iterator {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	ctx := sdk.UnwrapSDKContext(goCtx)

	evmIterator, cosmosIterator := m.getIterators(goCtx, i)

	combinedIterator := NewEVMMempoolIterator(evmIterator, cosmosIterator, m.logger, m.txConfig, m.vmKeeper.GetEvmCoinInfo(ctx).Denom, m.blockchain.Config().ChainID, m.blockchain)

	return combinedIterator
}

// CountTx returns the total number of transactions in both EVM and Cosmos pools.
// This provides a combined count across all mempool types.
func (m *ExperimentalEVMMempool) CountTx() int {
	pending, _ := m.txPool.Stats()
	return m.cosmosPool.CountTx() + pending
}

// Remove removes a transaction from the appropriate sdkmempool.
// For EVM transactions, removal is typically handled automatically by the pool
// based on nonce progression. Cosmos transactions are removed from the Cosmos pool.
func (m *ExperimentalEVMMempool) Remove(tx sdk.Tx) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// STOChain fix: synchronised read (NotifyNewBlock replaces latestCtx
	// concurrently).
	if m.blockchain.LatestHeight() == 0 {
		return nil
	}

	m.logger.Debug("removing transaction from mempool")

	// getEVMMessage decides the branch. It only reads the msgs of an
	// already decoded tx; a panic there happens before the branch is known,
	// so it is left to propagate exactly as before (recovering it would
	// change the Cosmos branch's outcome).
	msg, err := m.getEVMMessage(tx)
	if err == nil {
		m.removeEVMTx(tx, msg)
		return nil
	}

	if errors.Is(err, ErrNoMessages) {
		return err
	}

	m.logger.Debug("removing Cosmos transaction")
	err = m.cosmosPool.Remove(tx)
	if err != nil {
		m.logger.Error("failed to remove Cosmos transaction", "error", err)
	} else {
		m.logger.Debug("Cosmos transaction removed successfully")
	}
	return err
}

// removeEVMTx is the EVM branch of Remove.
//
// STOChain fix: Remove runs inside FinalizeBlock's runTx for every delivered
// tx. A panic here would be recovered by runTx into a failed tx result on this
// node only, i.e. a fork of this node. The whole EVM branch is node-local
// legacypool bookkeeping, so a panic is recovered here, the tx is kept (the
// legacypool drops it by nonce on reset) and Remove still returns nil, as it
// always does for EVM txs. The Cosmos branch is not covered: its return value
// is passed to runTx and must stay exactly as it was.
func (m *ExperimentalEVMMempool) removeEVMTx(tx sdk.Tx, msg *evmtypes.MsgEthereumTx) {
	defer func() {
		if r := recover(); r != nil {
			m.logRemovePanic(r)
		}
	}()
	// Comet will attempt to remove transactions from the mempool after completing successfully.
	// We should not do this with EVM transactions because removing them causes the subsequent ones to
	// be dequeued as temporarily invalid, only to be requeued a block later.
	// The EVM mempool handles removal based on account nonce automatically.
	hash := msg.Hash()
	if m.shouldRemoveFromEVMPool(tx) {
		m.logger.Debug("manually removing EVM transaction", "tx_hash", hash)
		m.legacyTxPool.RemoveTx(hash, false, true)
	} else {
		m.logger.Debug("skipping manual removal of EVM transaction, leaving to mempool to handle", "tx_hash", hash)
	}
}

// logRemovePanic logs a recovered panic of the EVM Remove branch, at most once
// per height.
func (m *ExperimentalEVMMempool) logRemovePanic(r any) {
	height := m.blockchain.LatestHeight()
	if m.removePanicHeight.Swap(height) != height {
		m.logger.Error("recovered panic in EVM mempool Remove, keeping transaction",
			"panic", r, "height", height)
	}
}

// shouldRemoveFromEVMPool determines whether an EVM transaction should be manually removed.
// It uses the AnteHandler to check if the transaction failed for reasons
// other than nonce gaps or successful execution, in which case manual removal is needed.
//
// STOChain fix: Remove runs inside FinalizeBlock's runTx, where a panic would
// fail that node's block execution. A panic here is node-local mempool
// bookkeeping only, so it is recovered and the tx is kept (the legacypool
// still drops it by nonce or on its own checks).
func (m *ExperimentalEVMMempool) shouldRemoveFromEVMPool(tx sdk.Tx) (remove bool) {
	defer func() {
		if r := recover(); r != nil {
			remove = false
			m.logRemovePanic(r)
		}
	}()
	if m.anteHandler == nil {
		m.logger.Debug("no ante handler available, keeping transaction")
		return false
	}

	// If it was a successful transaction or a sequence error, we let the mempool handle the cleaning.
	// If it was any other Cosmos or antehandler related issue, then we remove it.
	ctx, err := m.blockchain.GetLatestContext()
	if err != nil {
		m.logger.Debug("cannot get latest context for validation, keeping transaction", "error", err)
		return false // Cannot validate, keep transaction
	}
	if ctx.MultiStore() == nil {
		m.logger.Debug("latest context has no store, keeping transaction")
		return false
	}

	// STOChain fix: run the ante on a throw-away branch of the latest
	// context. The ante deducts fees and increments the nonce; on the shared
	// latestCtx those writes accumulated across every Remove call of a block
	// (each delivered tx, each Recheck failure, each PrepareProposal invalid
	// tx) and leaked into the legacypool state view (StateAt), the recheck
	// re-broadcast simulation and later verdicts until the next block, even
	// for txs that were never included. The branch is never written back.
	ctx, _ = ctx.CacheContext()
	_, err = m.anteHandler(ctx, tx, true)
	// Keep nonce gap transactions, remove others that fail validation
	if errors.Is(err, ErrNonceGap) || errors.Is(err, sdkerrors.ErrInvalidSequence) || errors.Is(err, sdkerrors.ErrOutOfGas) {
		m.logger.Debug("nonce gap detected, keeping transaction", "error", err)
		return false
	}

	if err != nil {
		m.logger.Debug("transaction validation failed, should be removed", "error", err)
	} else {
		m.logger.Debug("transaction validation succeeded, should be kept")
	}

	return err != nil
}

// SelectBy iterates through transactions until the provided filter function returns false.
// It uses the same unified iterator as Select but allows early termination based on
// custom criteria defined by the filter function.
func (m *ExperimentalEVMMempool) SelectBy(goCtx context.Context, i [][]byte, f func(sdk.Tx) bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	ctx := sdk.UnwrapSDKContext(goCtx)

	evmIterator, cosmosIterator := m.getIterators(goCtx, i)

	combinedIterator := NewEVMMempoolIterator(evmIterator, cosmosIterator, m.logger, m.txConfig, m.vmKeeper.GetEvmCoinInfo(ctx).Denom, m.blockchain.Config().ChainID, m.blockchain)

	for combinedIterator != nil && f(combinedIterator.Tx()) {
		combinedIterator = combinedIterator.Next()
	}
}

// SetEventBus sets CometBFT event bus to listen for new block header event.
// STOChain fix (M5): the subscription is buffered and re-established when the
// event bus cancels it (see eventbus.go).
//
// STOChain fix: it first primes the pool from the latest committed block, so
// the pool can validate txs (and answer pending nonces) before the first new
// block after a restart. Subscribe is bounded by eventBusCallTimeout.
func (m *ExperimentalEVMMempool) SetEventBus(eventBus *cmttypes.EventBus) {
	m.primeFromLatestBlock()

	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	_ = m.stopNewBlockLoopLocked()
	m.eventBus.Store(eventBus)
	stop := make(chan struct{})
	m.eventStop = stop
	ctx, cancel := context.WithTimeout(context.Background(), eventBusCallTimeout)
	defer cancel()
	sub, err := subscribeNewBlocks(ctx, eventBus)
	if err != nil {
		// STOChain fix: never panic here (this runs after CometBFT started).
		// The pubsub loop can stall for seconds behind a slow unbuffered
		// subscriber (tx indexer on an archive node, blocksync catch-up).
		// Keep starting and subscribe in the background with backoff; the
		// first successful subscription catches up with one NotifyNewBlock.
		m.logger.Error("EVM mempool could not subscribe to new-block events, retrying in the background",
			"error", err)
		go func() {
			sub := m.resubscribeNewBlocks(eventBus, stop)
			if sub == nil {
				return
			}
			m.catchUpNewBlock()
			m.runNewBlockLoop(eventBus, sub, stop, m.onNewBlock)
		}()
		return
	}
	go func() {
		// a block may have been committed between the prime and the
		// subscription; catch up once without dropping the primed context
		m.catchUpNewBlock()
		m.runNewBlockLoop(eventBus, sub, stop, m.onNewBlock)
	}()
}

// catchUpNewBlock runs the per-block work once after (re)subscribing at
// startup. Unlike onNewBlock it keeps the primed startup context when no block
// has been committed since the restart (STOChain fix).
func (m *ExperimentalEVMMempool) catchUpNewBlock() {
	if !m.GetBlockchain().NotifyNewBlockIfAvailable() {
		return
	}
	if m.recheckRebroadcast != nil {
		m.recheckRebroadcast.schedule()
	}
}

// primeTimeout bounds the wait for the pool's startup reset.
const primeTimeout = 10 * time.Second

// primeFromLatestBlock installs the latest committed block as the pool head
// and waits (bounded) for the pool reset, so the pool has chain state right
// after start instead of after the first new block (STOChain fix, N3).
//
// After a real restart the regular query context fails until the first
// commit (BaseApp.Init leaves the check state at height 0), so the latest
// context comes from StartupContext then. Without it the pool stays not
// ready until the first new block (inserts get ErrPoolNotReady).
func (m *ExperimentalEVMMempool) primeFromLatestBlock() {
	if ctx, err := m.blockchain.newLatestContext(); err == nil {
		m.blockchain.notifyNewBlockWithContext(ctx)
	} else if m.startupContext == nil {
		m.logger.Info("EVM mempool not primed at startup: latest context unavailable", "error", err)
		return
	} else if ctx, serr := m.startupContext(); serr == nil {
		m.blockchain.notifyNewBlockWithContext(ctx)
	} else {
		m.logger.Info("EVM mempool not primed at startup: latest context unavailable",
			"error", err, "startup_error", serr)
		return
	}
	done := make(chan error, 1)
	go func() { done <- m.txPool.Sync() }()
	select {
	case err := <-done:
		if err != nil {
			m.logger.Error("EVM mempool startup reset failed", "error", err)
			return
		}
	case <-time.After(primeTimeout):
		m.logger.Error("timed out waiting for the EVM mempool startup reset", "timeout", primeTimeout)
		return
	}
	m.logger.Info("EVM mempool primed from the latest block",
		"height", m.legacyTxPool.HeadNumber(), "ready", m.legacyTxPool.HasState())
}

// onNewBlock is the per-block work of the new-block loop. Keep it minimal:
// the loop must keep draining the event bus.
func (m *ExperimentalEVMMempool) onNewBlock() {
	m.GetBlockchain().NotifyNewBlock()
	// STOChain fix: re-broadcast EVM txs dropped by CheckTx/Recheck
	// that are includable again. Runs in its own goroutine so this
	// subscriber keeps draining the event bus.
	if m.recheckRebroadcast != nil {
		m.recheckRebroadcast.schedule()
	}
}

// HasEventBus returns true if the blockchain is configured to use an event bus for block notifications.
func (m *ExperimentalEVMMempool) HasEventBus() bool {
	return m.eventBus.Load() != nil
}

// Close unsubscribes from the CometBFT event bus and shuts down the mempool.
// STOChain fix: it is idempotent, writes the tx journal (M3) before the
// pools stop, and never waits more than closeTimeout for the txpool so a
// stuck background reorg cannot hang node shutdown.
func (m *ExperimentalEVMMempool) Close() error {
	m.closeOnce.Do(func() { m.closeErr = m.close() })
	return m.closeErr
}

// closeTimeout bounds the wait for the txpool goroutines on Close.
const closeTimeout = 10 * time.Second

func (m *ExperimentalEVMMempool) close() error {
	var errs []error
	m.eventMu.Lock()
	if err := m.stopNewBlockLoopLocked(); err != nil {
		errs = append(errs, fmt.Errorf("failed to unsubscribe from event bus: %w", err))
	}
	m.eventMu.Unlock()

	if err := m.writeJournal(); err != nil {
		errs = append(errs, fmt.Errorf("failed to write EVM mempool journal: %w", err))
	}

	done := make(chan error, 1)
	go func() { done <- m.txPool.Close() }()
	select {
	case err := <-done:
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to close txpool: %w", err))
		}
	case <-time.After(closeTimeout):
		errs = append(errs, fmt.Errorf("timed out after %s waiting for the txpool to close", closeTimeout))
	}

	return errors.Join(errs...)
}

// IsOrphanEVMTx returns true if the given tx bytes encode an EVM tx whose
// hash is no longer present in the legacypool. Used by the CheckTx wrapper
// on RecheckTx to drop CometBFT mempool entries that legacypool has already
// evicted (e.g. cumulative-balance reject, replacement, eviction). Without
// this hook the CometBFT mempool grows unbounded: legacypool drops a stale
// tx, CometBFT recheck sees ante pass on the single-tx view, tx never mines.
//
// SA-H1 audit-2026-05-29: decode failure on RecheckTx now returns true (drop).
// The tx passed initial CheckTx (which also calls TxDecoder), so a subsequent
// decode failure indicates the bytes are no longer round-trippable (proto
// registry change, soft fork, or codec drift). Leaving such bytes in the
// CometBFT mempool re-opens the original orphan bug the patch was meant to
// close. Shape failures (len(msgs) != 1, not MsgEthereumTx) are NOT EVM txs
// at all — leave alone (return false → let normal CheckTx decide).
//
// SA-M6 audit-2026-05-29 (FALSE POSITIVE — TOCTOU benign): the read of
// m.legacyTxPool.Has(hash) at line below is not synchronized with concurrent
// txPool.Insert. A sub-ms race is possible (orphan→drop-from-CometBFT while
// peer re-injects into legacypool). This is benign because:
//   1. Block proposal in getIterators() reads EVM pending set directly from
//      m.txPool.Pending(), NOT from CometBFT mempool. A tx present in
//      legacypool is still proposed regardless of CometBFT bookkeeping.
//   2. If the dropped tx is re-broadcast by any peer (or re-submitted by
//      the original sender), normal CheckTx adds it back to CometBFT.
// Adding a lock here would serialize a hot per-RecheckTx path for no
// correctness gain.
func (m *ExperimentalEVMMempool) IsOrphanEVMTx(txBytes []byte) bool {
	hash, isEVM, ok := m.decodeEVMTxHash(txBytes)
	if !ok {
		// Tx that was admitted once and no longer decodes is dead — drop.
		return true
	}
	if !isEVM {
		return false
	}
	return !m.legacyTxPool.Has(hash)
}

// decodeEVMTxHash returns the EVM tx hash encoded in txBytes. isEVM is false
// for txs that are not a single MsgEthereumTx; ok is false if the bytes do not
// decode at all.
func (m *ExperimentalEVMMempool) decodeEVMTxHash(txBytes []byte) (hash ethcommon.Hash, isEVM bool, ok bool) {
	// SA-M7 audit-2026-05-29: memoize decode result. RecheckTx fires every
	// block for every CometBFT mempool entry — at 5000 entries the proto
	// decode consumed ~6% of block budget. The legacypool.Has() lookup is
	// still performed by callers on every call (O(1) map) so cached entries
	// remain correct as the pool churns.
	cacheKey := sha256.Sum256(txBytes)
	if entry, found := m.orphanDecodeCache.Get(cacheKey); found {
		return entry.ethHash, entry.isEVM, true
	}

	tx, err := m.txConfig.TxDecoder()(txBytes)
	if err != nil {
		// Do NOT cache: a transient decode error should not pin a "drop"
		// verdict if the codec recovers (e.g. proto registry reloads).
		// SA-AUDIT-2026-06-11 fix19 A16-DELTA-LOW-2: surface the drop at
		// Debug — a sustained stream of decode-fail drops here is a codec
		// drift / proto registry regression signal that was previously
		// invisible (the eviction itself is correct, observability only).
		m.logger.Debug("IsOrphanEVMTx: decode failed, dropping as orphan", "err", err)
		return ethcommon.Hash{}, false, false
	}
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		m.orphanDecodeCache.Add(cacheKey, orphanDecodeEntry{isEVM: false})
		return ethcommon.Hash{}, false, true
	}
	ethMsg, isMsg := msgs[0].(*evmtypes.MsgEthereumTx)
	if !isMsg {
		m.orphanDecodeCache.Add(cacheKey, orphanDecodeEntry{isEVM: false})
		return ethcommon.Hash{}, false, true
	}
	hash = ethMsg.AsTransaction().Hash()
	m.orphanDecodeCache.Add(cacheKey, orphanDecodeEntry{ethHash: hash, isEVM: true})
	return hash, true, true
}

// getEVMMessage validates that the transaction contains exactly one message and returns it if it's an EVM message.
// Returns an error if the transaction has no messages, multiple messages, or the single message is not an EVM transaction.
func (m *ExperimentalEVMMempool) getEVMMessage(tx sdk.Tx) (*evmtypes.MsgEthereumTx, error) {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return nil, ErrNoMessages
	}
	if len(msgs) != 1 {
		return nil, fmt.Errorf("%w, got %d", ErrExpectedOneMessage, len(msgs))
	}
	ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx)
	if !ok {
		return nil, ErrNotEVMTransaction
	}
	return ethMsg, nil
}

// getIterators prepares iterators over pending EVM and Cosmos transactions.
// It configures EVM transactions with proper base fee filtering and priority ordering,
// while setting up the Cosmos iterator with the provided exclusion list.
func (m *ExperimentalEVMMempool) getIterators(goCtx context.Context, i [][]byte) (*miner.TransactionsByPriceAndNonce, sdkmempool.Iterator) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	baseFee := m.vmKeeper.GetBaseFee(ctx)
	var baseFeeUint *uint256.Int
	if baseFee != nil {
		baseFeeUint = uint256.MustFromBig(baseFee)
	}

	m.logger.Debug("getting iterators")

	pendingFilter := txpool.PendingFilter{
		MinTip:       m.minTip,
		BaseFee:      baseFeeUint,
		BlobFee:      nil,
		OnlyPlainTxs: true,
		OnlyBlobTxs:  false,
	}
	evmPendingTxes := m.txPool.Pending(pendingFilter)
	orderedEVMPendingTxes := miner.NewTransactionsByPriceAndNonce(nil, evmPendingTxes, baseFee)

	cosmosPendingTxes := m.cosmosPool.Select(ctx, i)

	return orderedEVMPendingTxes, cosmosPendingTxes
}

// isAlreadyKnownResponse reports whether a non-zero CheckTx response only says
// the tx is already held (SDK mempool cache code or legacypool "already known").
func isAlreadyKnownResponse(codespace string, code uint32, rawLog string) bool {
	if codespace == sdkerrors.RootCodespace && code == sdkerrors.ErrTxInMempoolCache.ABCICode() {
		return true
	}
	return strings.Contains(rawLog, txpool.ErrAlreadyKnown.Error())
}

// defaultBroadcastTxFn re-broadcasts promoted EVM txs through the client
// context installed by SetClientCtx.
func (m *ExperimentalEVMMempool) defaultBroadcastTxFn(txs []*ethtypes.Transaction) error {
	m.logger.Debug("broadcasting EVM transactions", "tx_count", len(txs))
	err := broadcastEVMTransactions(m.getClientCtx(), m.txConfig, txs)
	if err != nil {
		// legacypool logs through the go-ethereum logger, which this node does
		// not surface; log here so failed re-broadcasts are visible.
		m.logger.Error("failed to re-broadcast promoted EVM transactions", "tx_count", len(txs), "error", err)
	}
	return err
}

// broadcastEVMTransactions converts Ethereum transactions to Cosmos SDK format and broadcasts them.
// The bytes are built exactly like eth_sendRawTransaction does (signed
// MsgEthereumTx + BuildTx with the EVM denom), so a re-broadcast matches the
// original CometBFT tx and its cache entry (upstream #890). Every tx is
// attempted even if an earlier one fails; the errors are joined.
func broadcastEVMTransactions(clientCtx client.Context, txConfig client.TxConfig, ethTxs []*ethtypes.Transaction) error {
	if clientCtx.Client == nil {
		return errors.New("client context has no RPC client yet")
	}
	if clientCtx.TxConfig == nil {
		clientCtx = clientCtx.WithTxConfig(txConfig)
	}
	ethSigner := ethtypes.LatestSigner(evmtypes.GetEthChainConfig())

	var errs []error
	for _, ethTx := range ethTxs {
		msg := &evmtypes.MsgEthereumTx{}
		if err := msg.FromSignedEthereumTx(ethTx, ethSigner); err != nil {
			errs = append(errs, fmt.Errorf("failed to convert ethereum transaction %s: %w", ethTx.Hash().Hex(), err))
			continue
		}

		cosmosTx, err := msg.BuildTx(clientCtx.TxConfig.NewTxBuilder(), evmtypes.GetEVMCoinDenom())
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build cosmos tx %s: %w", ethTx.Hash().Hex(), err))
			continue
		}

		txBytes, err := clientCtx.TxConfig.TxEncoder()(cosmosTx)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to encode transaction %s: %w", ethTx.Hash().Hex(), err))
			continue
		}

		res, err := clientCtx.BroadcastTxSync(txBytes)
		if err != nil {
			// CometBFT answers a tx it already holds with ErrTxInCache;
			// that is the expected outcome for most re-broadcasts.
			if errors.Is(err, cmtmempool.ErrTxInCache) || strings.Contains(err.Error(), cmtmempool.ErrTxInCache.Error()) {
				continue
			}
			errs = append(errs, fmt.Errorf("failed to broadcast transaction %s: %w", ethTx.Hash().Hex(), err))
			continue
		}
		if res.Code != 0 && !isAlreadyKnownResponse(res.Codespace, res.Code, res.RawLog) {
			errs = append(errs, fmt.Errorf("transaction %s rejected by mempool: code=%d, log=%s", ethTx.Hash().Hex(), res.Code, res.RawLog))
		}
	}
	return errors.Join(errs...)
}
