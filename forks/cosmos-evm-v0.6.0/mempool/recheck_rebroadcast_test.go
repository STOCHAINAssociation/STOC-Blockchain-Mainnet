package mempool_test

import (
	"crypto/ecdsa"
	"math/big"
	"sync"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/config"
	"github.com/cosmos/evm/encoding"
	"github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/mempool/mocks"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	"github.com/cosmos/evm/x/vm/statedb"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// recheckHarness is a real ExperimentalEVMMempool (real legacypool, real tx
// codec) over mocked keepers, with a mocked BroadcastTxFn and ante handler.
type recheckHarness struct {
	t        *testing.T
	mp       *mempool.ExperimentalEVMMempool
	txConfig client.TxConfig
	chainID  *big.Int

	mu      sync.Mutex
	baseFee *big.Int
	anteErr error
	sent    []*ethtypes.Transaction
}

func configureEVMForTest(t *testing.T) {
	t.Helper()
	// Global EVM config may already be set by another test in this binary.
	_ = vmtypes.SetChainConfig(vmtypes.DefaultChainConfig(config.EighteenDecimalsChainID))
	_ = vmtypes.NewEVMConfigurator().WithEVMCoinInfo(config.ChainsCoinInfo[config.EighteenDecimalsChainID]).Configure()
	require.NotNil(t, vmtypes.GetChainConfig())
}

// harnessOption customises the mempool config or the latest-context
// callback before the mempool is built.
type harnessOption func(cfg *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error))

func newRecheckHarness(t *testing.T, opts ...harnessOption) *recheckHarness {
	t.Helper()
	configureEVMForTest(t)

	encCfg := encoding.MakeConfig(config.EighteenDecimalsChainID)
	vmtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	h := &recheckHarness{
		t:        t,
		txConfig: encCfg.TxConfig,
		chainID:  vmtypes.GetEthChainConfig().ChainID,
		baseFee:  big.NewInt(1_000_000_000),
	}

	vmKeeper := mocks.NewVMKeeper(t)
	feeMarketKeeper := mocks.NewFeeMarketKeeper(t)
	vmKeeper.On("GetBaseFee", mock.Anything).Return(func(sdk.Context) *big.Int {
		h.mu.Lock()
		defer h.mu.Unlock()
		return new(big.Int).Set(h.baseFee)
	}).Maybe()
	feeMarketKeeper.On("GetBlockGasWanted", mock.Anything).Return(uint64(0)).Maybe()
	vmKeeper.On("GetParams", mock.Anything).Return(vmtypes.DefaultParams()).Maybe()
	vmKeeper.On("GetEvmCoinInfo", mock.Anything).Return(config.ChainsCoinInfo[config.EighteenDecimalsChainID]).Maybe()
	vmKeeper.On("GetAccount", mock.Anything, mock.Anything).Return(func(sdk.Context, ethcommon.Address) *statedb.Account {
		return &statedb.Account{Nonce: 0, Balance: uint256.NewInt(1_000_000_000_000_000_000), CodeHash: ethtypes.EmptyCodeHash.Bytes()}
	}).Maybe()
	vmKeeper.On("GetState", mock.Anything, mock.Anything, mock.Anything).Return(ethcommon.Hash{}).Maybe()
	vmKeeper.On("GetCode", mock.Anything, mock.Anything).Return([]byte{}).Maybe()
	vmKeeper.On("GetCodeHash", mock.Anything, mock.Anything).Return(ethtypes.EmptyCodeHash).Maybe()
	vmKeeper.On("KVStoreKeys").Return(make(map[string]*storetypes.KVStoreKey)).Maybe()
	vmKeeper.On("SetEvmMempool", mock.Anything).Return().Maybe()

	baseCtx := sdktestutil.DefaultContext(storetypes.NewKVStoreKey("test"), storetypes.NewTransientStoreKey("transient_test"))
	getCtx := func(int64, bool) (sdk.Context, error) {
		return baseCtx.
			WithBlockHeight(1).
			WithBlockHeader(cmtproto.Header{Height: 1, AppHash: []byte("00000000000000000000000000000000")}).
			WithEventManager(sdk.NewEventManager()), nil
	}

	cfg := &mempool.EVMMempoolConfig{
		LegacyPoolConfig: &legacypool.DefaultConfig,
		AnteHandler: func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return ctx, h.anteErr
		},
		BroadCastTxFn: func(txs []*ethtypes.Transaction) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.sent = append(h.sent, txs...)
			return nil
		},
		BlockGasLimit: 100_000_000,
	}
	for _, opt := range opts {
		opt(cfg, &getCtx)
	}
	h.mp = mempool.NewExperimentalEVMMempool(getCtx, log.NewNopLogger(), vmKeeper, feeMarketKeeper, h.txConfig, client.Context{}, cfg, 100)
	t.Cleanup(func() { _ = h.mp.Close() })
	return h
}

func (h *recheckHarness) setBaseFee(v int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.baseFee = big.NewInt(v)
}

func (h *recheckHarness) setAnteErr(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.anteErr = err
}

func (h *recheckHarness) takeSent() []*ethtypes.Transaction {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.sent
	h.sent = nil
	return out
}

func (h *recheckHarness) signTx(key *ecdsa.PrivateKey, nonce uint64, feeCap int64) *ethtypes.Transaction {
	to := ethcommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(h.chainID), &ethtypes.DynamicFeeTx{
		ChainID:   h.chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	require.NoError(h.t, err)
	return tx
}

// cometTxBytes builds the CometBFT tx bytes the same way
// eth_sendRawTransaction and the #890 re-broadcast do.
func (h *recheckHarness) cometTxBytes(tx *ethtypes.Transaction) []byte {
	msg := &vmtypes.MsgEthereumTx{}
	require.NoError(h.t, msg.FromSignedEthereumTx(tx, ethtypes.LatestSignerForChainID(h.chainID)))
	cosmosTx, err := msg.BuildTx(h.txConfig.NewTxBuilder(), vmtypes.GetEVMCoinDenom())
	require.NoError(h.t, err)
	bz, err := h.txConfig.TxEncoder()(cosmosTx)
	require.NoError(h.t, err)
	return bz
}

// addPending puts tx in the legacypool as pending, then discards the
// queued->pending promotion broadcast so only re-broadcasts are counted.
func (h *recheckHarness) addPending(tx *ethtypes.Transaction) {
	errs := h.mp.GetTxPool().Add([]*ethtypes.Transaction{tx}, true)
	require.Len(h.t, errs, 1)
	require.NoError(h.t, errs[0])
	require.True(h.t, h.mp.GetTxPool().Has(tx.Hash()))
	pending, _ := h.mp.GetTxPool().Stats()
	require.Equal(h.t, 1, pending)
	h.takeSent()
}

func (h *recheckHarness) recheck(txBytes []byte, runErr error) *abci.ResponseCheckTx {
	res, err := mempool.NewCheckTxHandler(h.mp)(runTxReturning(runErr), &abci.RequestCheckTx{
		Tx:   txBytes,
		Type: abci.CheckTxType_Recheck,
	})
	require.NoError(h.t, err)
	return res
}

var errBelowBaseFee = sdkerrors.ErrInsufficientFee.Wrap("max fee per gas less than block base fee (1000000000 < 1125000000)")

// Reproduces the testnet fee-stuck scenario: a tx with fee cap == base fee is
// dropped by CometBFT on Recheck after the base fee rises, while the
// legacypool keeps it. Once the base fee falls back, the tx must be
// re-broadcast exactly once through BroadcastTxFn.
func TestRecheckDrop_FeeStuckTxRebroadcastOnceWhenBaseFeeFalls(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	victim := h.signTx(key, 0, 1_000_000_000) // fee cap == base fee
	h.addPending(victim)
	bz := h.cometTxBytes(victim)

	// Blocks fill up: base fee rises above the victim's fee cap and the
	// ante (CanTransfer) fails on Recheck.
	h.setBaseFee(1_125_000_000)
	res := h.recheck(bz, errBelowBaseFee)
	require.NotEqual(t, uint32(0), res.Code, "CometBFT must still drop the tx")
	require.True(t, h.mp.RecheckDroppedHas(victim.Hash()))
	require.True(t, h.mp.GetTxPool().Has(victim.Hash()), "legacypool keeps the fee-stuck tx")

	// New block, base fee still too high: nothing re-broadcast, still tracked.
	h.mp.ProcessRecheckDropped()
	require.Empty(t, h.takeSent())
	require.Equal(t, 1, h.mp.RecheckDroppedLen())

	// Base fee falls back to the fee cap: re-broadcast exactly that tx once.
	h.setBaseFee(1_000_000_000)
	h.mp.ProcessRecheckDropped()
	sent := h.takeSent()
	require.Len(t, sent, 1)
	require.Equal(t, victim.Hash(), sent[0].Hash())
	require.Equal(t, 0, h.mp.RecheckDroppedLen())

	// Following blocks: no duplicate re-broadcast.
	h.mp.ProcessRecheckDropped()
	require.Empty(t, h.takeSent())

	// The re-broadcast tx encodes to the original CometBFT bytes.
	require.Equal(t, bz, h.cometTxBytes(sent[0]))
}

// A tracked tx that leaves the legacypool (mined, replaced, evicted) is
// forgotten without a re-broadcast.
func TestRecheckDrop_ForgottenWhenRemovedFromPool(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	victim := h.signTx(key, 0, 1_000_000_000)
	h.addPending(victim)

	h.setBaseFee(1_125_000_000)
	h.recheck(h.cometTxBytes(victim), errBelowBaseFee)
	require.Equal(t, 1, h.mp.RecheckDroppedLen())

	lp := h.mp.GetTxPool().Subpools[0].(*legacypool.LegacyPool)
	lp.RemoveTx(victim.Hash(), false, true)
	require.False(t, h.mp.GetTxPool().Has(victim.Hash()))

	h.setBaseFee(1_000_000_000)
	h.mp.ProcessRecheckDropped()
	require.Empty(t, h.takeSent())
	require.Equal(t, 0, h.mp.RecheckDroppedLen())
}

// Insufficient funds is temporary too; the ante simulation gates the
// re-broadcast until the sender can pay.
func TestRecheckDrop_InsufficientFundsWaitsForAnte(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	victim := h.signTx(key, 0, 1_000_000_000)
	h.addPending(victim)

	fundsErr := sdkerrors.ErrInsufficientFunds.Wrap("failed to check sender balance")
	h.recheck(h.cometTxBytes(victim), fundsErr)
	require.Equal(t, 1, h.mp.RecheckDroppedLen())

	h.setAnteErr(fundsErr)
	h.mp.ProcessRecheckDropped()
	require.Empty(t, h.takeSent())
	require.Equal(t, 1, h.mp.RecheckDroppedLen())

	h.setAnteErr(nil)
	h.mp.ProcessRecheckDropped()
	require.Len(t, h.takeSent(), 1)
	require.Equal(t, 0, h.mp.RecheckDroppedLen())
}

// Only temporary failures of txs still held by the legacypool are tracked.
func TestRecheckDrop_NotTracked(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	inPool := h.signTx(key, 0, 1_000_000_000)
	h.addPending(inPool)
	notInPool := h.signTx(key, 5, 1_000_000_000)

	// Permanent error.
	h.recheck(h.cometTxBytes(inPool), sdkerrors.ErrInvalidRequest.Wrap("bad"))
	require.Equal(t, 0, h.mp.RecheckDroppedLen())

	// Not held by the legacypool: the orphan check drops it first.
	res := h.recheck(h.cometTxBytes(notInPool), errBelowBaseFee)
	require.NotEqual(t, uint32(0), res.Code)
	require.Equal(t, 0, h.mp.RecheckDroppedLen())

	// Fresh tx rejected at New CheckTx (never entered the legacypool).
	res, err := mempool.NewCheckTxHandler(h.mp)(runTxReturning(errBelowBaseFee), &abci.RequestCheckTx{
		Tx:   h.cometTxBytes(notInPool),
		Type: abci.CheckTxType_New,
	})
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), res.Code)
	require.Equal(t, 0, h.mp.RecheckDroppedLen())

	// Not an EVM tx at all.
	h.recheck([]byte("not a tx"), errBelowBaseFee)
	require.Equal(t, 0, h.mp.RecheckDroppedLen())
}

// Variant: a queued tx promoted to pending while the base fee is above its
// fee cap is re-broadcast by #890, but that New CheckTx fails the ante, so it
// never enters CometBFT while the legacypool keeps it. It must be tracked
// and re-broadcast once the base fee falls.
func TestRecheckDrop_PromotionRebroadcastRejectedIsTracked(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	tx := h.signTx(key, 0, 1_000_000_000)
	h.addPending(tx)

	h.setBaseFee(1_125_000_000)
	res, err := mempool.NewCheckTxHandler(h.mp)(runTxReturning(errBelowBaseFee), &abci.RequestCheckTx{
		Tx:   h.cometTxBytes(tx),
		Type: abci.CheckTxType_New,
	})
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), res.Code)
	require.True(t, h.mp.RecheckDroppedHas(tx.Hash()))

	h.setBaseFee(1_000_000_000)
	h.mp.ProcessRecheckDropped()
	sent := h.takeSent()
	require.Len(t, sent, 1)
	require.Equal(t, tx.Hash(), sent[0].Hash())
}
