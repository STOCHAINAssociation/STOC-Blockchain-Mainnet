package mempool_test

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// N3: right after a restart the pool had no chain state until the second
// new block. The pending nonce panicked ("method handler crashed") and
// inserts failed with "exceeds block gas limit". Now the nonce falls back to
// 0 (the RPC then uses the committed nonce), inserts fail with a retryable
// ErrPoolNotReady, and SetEventBus primes the pool from the latest block.
func TestStartup_PoolNotReadyUntilPrimed(t *testing.T) {
	var appLoaded atomic.Bool
	h := newRecheckHarness(t, func(_ *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		inner := *getCtx
		*getCtx = func(height int64, prove bool) (sdk.Context, error) {
			if !appLoaded.Load() {
				return sdk.Context{}, errors.New("stoc is not ready; please wait for first block")
			}
			ctx, err := inner(height, prove)
			return ctx.WithBlockHeight(100), err
		}
	})
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	to := ethcommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(h.chainID), &ethtypes.DynamicFeeTx{
		ChainID: h.chainID, Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1_000_000_000),
		Gas: 1_000_000, To: &to, Value: big.NewInt(0),
	})
	require.NoError(t, err)
	sdkTx, err := h.txConfig.TxDecoder()(h.cometTxBytes(tx))
	require.NoError(t, err)
	_, ok := sdkTx.GetMsgs()[0].(*vmtypes.MsgEthereumTx)
	require.True(t, ok)
	lp := h.mp.GetTxPool().Subpools[0].(*legacypool.LegacyPool)

	// Not ready: no panic, nonce 0, clear retryable insert error.
	require.False(t, lp.HasState())
	require.NotPanics(t, func() {
		require.Equal(t, uint64(0), h.mp.GetTxPool().PoolNonce(addr))
		require.Equal(t, uint64(0), h.mp.GetTxPool().Nonce(addr))
	})
	err = h.mp.Insert(sdk.Context{}.WithBlockHeight(100), sdkTx)
	require.ErrorIs(t, err, legacypool.ErrPoolNotReady)
	require.NotContains(t, err.Error(), "gas limit")

	// App loaded, CometBFT started: SetEventBus primes the pool from the
	// latest committed block before any new block arrives.
	appLoaded.Store(true)
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })
	h.mp.SetEventBus(bus)
	require.True(t, lp.HasState())
	require.Equal(t, uint64(100), lp.HeadNumber())
	require.NoError(t, h.mp.Insert(sdk.Context{}.WithBlockHeight(100), sdkTx))
	require.True(t, h.mp.GetTxPool().Has(tx.Hash()))
}

// Review M3 (round 4): after a real restart the regular query context fails
// until the first commit (BaseApp check state at height 0). The prime then
// uses StartupContext, so the pool is ready before any new block.
func TestStartup_PrimedFromStartupContextAfterRestart(t *testing.T) {
	var regularWorks atomic.Bool
	var startupCalls atomic.Int64
	h := newRecheckHarness(t, func(cfg *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		inner := *getCtx
		*getCtx = func(height int64, prove bool) (sdk.Context, error) {
			if !regularWorks.Load() {
				return sdk.Context{}, errors.New("context did not contain latest block height in either check state or finalize block state")
			}
			return inner(height, prove)
		}
		cfg.StartupContext = func() (sdk.Context, error) {
			startupCalls.Add(1)
			ctx, err := inner(0, false)
			return ctx.WithBlockHeight(100), err
		}
	})
	lp := h.mp.GetTxPool().Subpools[0].(*legacypool.LegacyPool)
	require.False(t, lp.HasState())

	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })
	h.mp.SetEventBus(bus)
	require.Equal(t, int64(1), startupCalls.Load())
	require.True(t, lp.HasState(), "pool ready before the first new block")
	require.Equal(t, uint64(100), lp.HeadNumber())

	key, _ := crypto.GenerateKey()
	tx := h.signTx(key, 0, 1_000_000_000)
	sdkTx, err := h.txConfig.TxDecoder()(h.cometTxBytes(tx))
	require.NoError(t, err)
	require.NoError(t, h.mp.Insert(sdk.Context{}.WithBlockHeight(100), sdkTx))
}

// Without a startup context the prime is a no-op after a restart: the pool
// stays not ready (inserts get ErrPoolNotReady) until the first new block.
func TestStartup_NoStartupContextStaysNotReady(t *testing.T) {
	h := newRecheckHarness(t, func(_ *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		*getCtx = func(int64, bool) (sdk.Context, error) { return sdk.Context{}, errors.New("not ready") }
	})
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })
	h.mp.SetEventBus(bus)
	lp := h.mp.GetTxPool().Subpools[0].(*legacypool.LegacyPool)
	require.False(t, lp.HasState())
}

// After the prime, the startup catch-up must not drop the primed context
// while no block has been committed since the restart (regular context still
// failing), and must advance once it works.
func TestStartup_CatchUpKeepsPrimedContext(t *testing.T) {
	var regularWorks atomic.Bool
	h := newRecheckHarness(t, func(cfg *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		inner := *getCtx
		*getCtx = func(height int64, prove bool) (sdk.Context, error) {
			if !regularWorks.Load() {
				return sdk.Context{}, errors.New("context did not contain latest block height in either check state or finalize block state")
			}
			ctx, err := inner(height, prove)
			return ctx.WithBlockHeight(101), err
		}
		cfg.StartupContext = func() (sdk.Context, error) {
			ctx, err := inner(0, false)
			return ctx.WithBlockHeight(100), err
		}
	})
	lp := h.mp.GetTxPool().Subpools[0].(*legacypool.LegacyPool)
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })
	h.mp.SetEventBus(bus)
	require.True(t, lp.HasState())

	// catch-up with no new commit: primed context stays
	require.False(t, h.mp.GetBlockchain().NotifyNewBlockIfAvailable())
	require.True(t, lp.HasState(), "catch-up must not drop the primed context")
	require.Equal(t, int64(100), h.mp.GetBlockchain().LatestHeight())

	// a block got committed: catch-up advances the head
	regularWorks.Store(true)
	require.True(t, h.mp.GetBlockchain().NotifyNewBlockIfAvailable())
	require.Equal(t, int64(101), h.mp.GetBlockchain().LatestHeight())
}
