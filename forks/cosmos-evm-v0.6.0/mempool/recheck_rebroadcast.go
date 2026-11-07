package mempool

import (
	"container/list"
	"errors"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// STOChain fix: re-broadcast EVM txs that CometBFT dropped on Recheck for a
// temporary reason ("reverse orphans").
//
// Scenario: a tx accepted by an RPC fullnode with fee cap == base fee lives in
// that node's legacypool and CometBFT mempool. Blocks fill up, the base fee
// rises above the fee cap, and every CometBFT Recheck fails the ante
// (CanTransfer: ErrInsufficientFee). CometBFT drops the tx from its mempool
// and cache; the legacypool keeps it as pending (geth semantics: fee-stuck,
// wait). Peers that saw the gossip only after the fee rose rejected it at
// New CheckTx, so the tx exists ONLY in this node's legacypool. Proposers
// select EVM txs from their own legacypool, so nobody ever proposes it, and
// the #890 re-broadcast only fires on queued->pending promotion. When the base
// fee falls back nothing re-gossips the tx: it stays stuck until the pool
// lifetime evicts it and the sender's nonce is blocked meanwhile.
//
// Fix: remember such drops and, on each new block, re-broadcast the ones that
// became includable again through the same path as #890 (BroadcastTxFn), so
// CometBFT stores and gossips them again.

const (
	// recheckDroppedCap bounds the number of remembered drops. Entries are
	// only recorded for txs held by the legacypool (itself bounded by
	// GlobalSlots + GlobalQueue), so the cap is a safety net. When full, new
	// drops are refused; stale entries are pruned every block.
	recheckDroppedCap = 10_000
	// recheckAnteChecksPerBlock bounds the ante simulations run per block.
	// Entries that are checked and kept move to the back of the queue, so a
	// large set is walked round-robin instead of starving its tail.
	recheckAnteChecksPerBlock = 1_000
)

// recheckDroppedSet is a bounded FIFO set of EVM tx hashes. It has its own
// mutex and never calls out while holding it, so it can be used from inside
// CheckTx (CometBFT local-client mutex held) without deadlock risk.
type recheckDroppedSet struct {
	mu    sync.Mutex
	cap   int
	order *list.List // of ethcommon.Hash, oldest first
	index map[ethcommon.Hash]*list.Element
}

func newRecheckDroppedSet(capacity int) *recheckDroppedSet {
	return &recheckDroppedSet{
		cap:   capacity,
		order: list.New(),
		index: make(map[ethcommon.Hash]*list.Element),
	}
}

// add records hash. It returns added=false if the hash is already present or
// the set is full (full=true).
func (s *recheckDroppedSet) add(hash ethcommon.Hash) (added bool, full bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.index[hash]; ok {
		return false, false
	}
	if s.order.Len() >= s.cap {
		return false, true
	}
	s.index[hash] = s.order.PushBack(hash)
	return true, false
}

func (s *recheckDroppedSet) remove(hash ethcommon.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.index[hash]; ok {
		s.order.Remove(el)
		delete(s.index, hash)
	}
}

// moveToBack moves hash behind every other entry (round-robin fairness).
func (s *recheckDroppedSet) moveToBack(hash ethcommon.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.index[hash]; ok {
		s.order.MoveToBack(el)
	}
}

func (s *recheckDroppedSet) has(hash ethcommon.Hash) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.index[hash]
	return ok
}

func (s *recheckDroppedSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}

// snapshot returns all hashes, oldest first.
func (s *recheckDroppedSet) snapshot() []ethcommon.Hash {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ethcommon.Hash, 0, s.order.Len())
	for el := s.order.Front(); el != nil; el = el.Next() {
		out = append(out, el.Value.(ethcommon.Hash))
	}
	return out
}

// recheckRebroadcaster decides, once per block, which remembered drops to
// re-broadcast. Its dependencies are plain functions so the decision logic
// can be unit tested without a running node.
type recheckRebroadcaster struct {
	logger log.Logger
	set    *recheckDroppedSet

	// getTx returns the tx if the legacypool still holds it, nil otherwise.
	getTx func(ethcommon.Hash) *ethtypes.Transaction
	// baseFee returns the base fee the next block will use (nil: no fee market).
	baseFee func() *big.Int
	// checkIncludable runs the ante simulation for txs (ordered by sender and
	// nonce) and returns one error per tx; nil means the tx would be
	// accepted. A nil func skips the ante check.
	checkIncludable func(txs []*ethtypes.Transaction) []error
	// broadcast re-broadcasts txs to the CometBFT mempool (BroadcastTxFn).
	broadcast func(txs []*ethtypes.Transaction) error

	// running prevents overlapping rounds: a slow round (CheckTx contention)
	// makes the next block's round a no-op instead of piling up goroutines.
	running atomic.Bool
}

// isTemporaryRecheckError reports whether a Recheck failure may clear up by
// itself (base fee falls, sender gets funds), so the tx is worth re-gossiping
// later instead of being forgotten by CometBFT for good.
func isTemporaryRecheckError(err error) bool {
	return errors.Is(err, sdkerrors.ErrInsufficientFee) || errors.Is(err, sdkerrors.ErrInsufficientFunds)
}

// record remembers a tx rejected by CheckTx. The caller has checked that the
// tx is an EVM tx held by the legacypool.
func (r *recheckRebroadcaster) record(hash ethcommon.Hash, reason error) {
	added, full := r.set.add(hash)
	switch {
	case added:
		r.logger.Debug("EVM tx rejected by CheckTx but kept in legacypool; will re-broadcast when includable",
			"tx_hash", hash, "reason", reason)
	case full:
		r.logger.Debug("recheck-dropped set full, not tracking EVM tx",
			"tx_hash", hash, "cap", r.set.cap, "reason", reason)
	}
}

// schedule starts a round in the background unless one is already running or
// there is nothing to do. It never blocks the caller.
func (r *recheckRebroadcaster) schedule() {
	if r.set.len() == 0 {
		return
	}
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.running.Store(false)
		r.process()
	}()
}

// process runs one round synchronously. No lock that CheckTx needs is held
// while calling checkIncludable or broadcast.
func (r *recheckRebroadcaster) process() {
	hashes := r.set.snapshot()
	if len(hashes) == 0 {
		return
	}
	baseFee := r.baseFee()

	candidates := make([]*ethtypes.Transaction, 0, len(hashes))
	for _, hash := range hashes {
		tx := r.getTx(hash)
		if tx == nil {
			// Mined, replaced or evicted: nothing left to re-broadcast.
			r.set.remove(hash)
			continue
		}
		if baseFee != nil && tx.GasFeeCapIntCmp(baseFee) < 0 {
			// Still fee-stuck; keep for a later block.
			continue
		}
		if len(candidates) < recheckAnteChecksPerBlock {
			candidates = append(candidates, tx)
		}
	}
	if len(candidates) == 0 {
		return
	}

	// Order by sender and nonce: the ante simulation and the re-broadcast
	// CheckTx both need nonce n before n+1.
	sortBySenderAndNonce(candidates)

	var errs []error
	if r.checkIncludable != nil {
		errs = r.checkIncludable(candidates)
	}

	ready := make([]*ethtypes.Transaction, 0, len(candidates))
	for i, tx := range candidates {
		if errs != nil && i < len(errs) && errs[i] != nil {
			r.logger.Debug("recheck-dropped EVM tx not includable yet, keeping",
				"tx_hash", tx.Hash(), "reason", errs[i])
			r.set.moveToBack(tx.Hash())
			continue
		}
		ready = append(ready, tx)
	}
	if len(ready) == 0 {
		return
	}

	// Forget before broadcasting: if it is dropped again on a later Recheck
	// it is simply recorded again.
	for _, tx := range ready {
		r.set.remove(tx.Hash())
	}
	r.logger.Info("re-broadcasting EVM txs dropped by CometBFT recheck",
		"tx_count", len(ready), "first_tx_hash", ready[0].Hash(), "tracked", r.set.len())
	if r.broadcast == nil {
		return
	}
	if err := r.broadcast(ready); err != nil {
		r.logger.Error("failed to re-broadcast recheck-dropped EVM txs",
			"tx_count", len(ready), "error", err)
	}
}

func sortBySenderAndNonce(txs []*ethtypes.Transaction) {
	senders := make(map[ethcommon.Hash]ethcommon.Address, len(txs))
	for _, tx := range txs {
		// Pool txs are already signature-checked; the sender is cached.
		from, _ := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
		senders[tx.Hash()] = from
	}
	sort.SliceStable(txs, func(i, j int) bool {
		a, b := senders[txs[i].Hash()], senders[txs[j].Hash()]
		if a != b {
			return a.Cmp(b) < 0
		}
		return txs[i].Nonce() < txs[j].Nonce()
	})
}

// RecordTemporaryDrop is called by the CheckTx handler when CheckTx (New or
// Recheck) fails. It remembers EVM txs that the legacypool still holds and
// that failed for a temporary reason, so they can be re-broadcast once
// includable. It only takes the set mutex and the legacypool read lock;
// never m.mtx.
func (m *ExperimentalEVMMempool) RecordTemporaryDrop(txBytes []byte, err error) {
	if m == nil || m.recheckRebroadcast == nil || !isTemporaryRecheckError(err) {
		return
	}
	hash, isEVM, ok := m.decodeEVMTxHash(txBytes)
	if !ok || !isEVM || !m.legacyTxPool.Has(hash) {
		return
	}
	m.recheckRebroadcast.record(hash, err)
}

// recheckBaseFee returns the base fee used by New CheckTx (rejectBelowBaseFee
// and the ante read the same feemarket value).
func (m *ExperimentalEVMMempool) recheckBaseFee() *big.Int {
	return m.blockchain.CurrentBlock().BaseFee
}

// recheckBroadcast re-broadcasts through the legacypool BroadcastTxFn, i.e.
// the exact #890 path (same CometBFT tx bytes as eth_sendRawTransaction).
func (m *ExperimentalEVMMempool) recheckBroadcast(txs []*ethtypes.Transaction) error {
	if m.legacyTxPool.BroadcastTxFn == nil {
		return nil
	}
	return m.legacyTxPool.BroadcastTxFn(txs)
}

// recheckAnteCheck simulates the ante handler (as shouldRemoveFromEVMPool
// does) for txs ordered by sender and nonce. All txs share one branch of the
// latest context so nonce n's increment and fee deduction are visible to
// n+1, like CheckTx's checkState. Each tx runs on its own sub-branch that is
// written back only on success. Nothing is ever written to the latest
// context itself.
func (m *ExperimentalEVMMempool) recheckAnteCheck(txs []*ethtypes.Transaction) []error {
	errs := make([]error, len(txs))
	if m.anteHandler == nil {
		return errs
	}
	latest, err := m.blockchain.GetLatestContext()
	if err != nil || latest.MultiStore() == nil {
		for i := range errs {
			errs[i] = errors.New("latest context unavailable")
		}
		return errs
	}
	shared, _ := latest.CacheContext()
	for i, ethTx := range txs {
		sdkTx, err := ethTxToCosmosTx(m.txConfig, ethTx)
		if err != nil {
			errs[i] = err
			continue
		}
		txCtx, write := shared.CacheContext()
		if _, err := m.anteHandler(txCtx, sdkTx, true); err != nil {
			errs[i] = err
			continue
		}
		write()
	}
	return errs
}

// ethTxToCosmosTx wraps a signed Ethereum tx the same way
// broadcastEVMTransactions and eth_sendRawTransaction do.
func ethTxToCosmosTx(txConfig client.TxConfig, ethTx *ethtypes.Transaction) (sdk.Tx, error) {
	msg := &evmtypes.MsgEthereumTx{}
	if err := msg.FromSignedEthereumTx(ethTx, ethtypes.LatestSigner(evmtypes.GetEthChainConfig())); err != nil {
		return nil, err
	}
	return msg.BuildTx(txConfig.NewTxBuilder(), evmtypes.GetEVMCoinDenom())
}
