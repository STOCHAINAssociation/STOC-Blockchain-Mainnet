package mempool_test

import (
	"crypto/ecdsa"
	"encoding/binary"
	"sync"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/mempool"
	vmtypes "github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"

	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// statefulAnte mimics the parts of the EVM mono decorator that write state,
// in the same order: balance check, fee deduction, then nonce check and
// increment. Nonce and balance live in a KV store of the context, so writes
// are visible to later calls on the same context.
type statefulAnte struct {
	key storetypes.StoreKey
	fee uint64

	mu   sync.Mutex
	errs map[ethcommon.Hash]error
}

func nonceKey(a ethcommon.Address) []byte   { return append([]byte("n/"), a.Bytes()...) }
func balanceKey(a ethcommon.Address) []byte { return append([]byte("b/"), a.Bytes()...) }

func readU64(store storetypes.KVStore, k []byte) uint64 {
	bz := store.Get(k)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

func writeU64(store storetypes.KVStore, k []byte, v uint64) {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, v)
	store.Set(k, bz)
}

func (a *statefulAnte) handle(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
	ethTx := tx.GetMsgs()[0].(*vmtypes.MsgEthereumTx).AsTransaction()
	from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(ethTx.ChainId()), ethTx)
	if err != nil {
		return ctx, err
	}
	store := ctx.KVStore(a.key)
	err = func() error {
		balance := readU64(store, balanceKey(from))
		if balance < a.fee {
			return errorsmod.Wrap(sdkerrors.ErrInsufficientFunds, "failed to check sender balance")
		}
		writeU64(store, balanceKey(from), balance-a.fee)
		nonce := readU64(store, nonceKey(from))
		switch {
		case ethTx.Nonce() > nonce:
			return errorsmod.Wrapf(mempool.ErrNonceGap, "tx nonce: %d, account nonce: %d", ethTx.Nonce(), nonce)
		case ethTx.Nonce() < nonce:
			return errorsmod.Wrapf(mempool.ErrNonceLow, "got %d, expected %d", ethTx.Nonce(), nonce)
		}
		writeU64(store, nonceKey(from), nonce+1)
		return nil
	}()
	a.mu.Lock()
	a.errs[ethTx.Hash()] = err
	a.mu.Unlock()
	return ctx, err
}

func (a *statefulAnte) errFor(hash ethcommon.Hash) (error, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	err, ok := a.errs[hash]
	return err, ok
}

// TestRemove_AnteRunsOnBranchOfLatestContext covers audit finding M1:
// shouldRemoveFromEVMPool (called by baseapp Remove for every delivered tx,
// every Recheck failure and every PrepareProposal invalid tx) must not write
// the ante's fee deduction and nonce increment into the shared latest
// context, and the resulting verdicts must be the intended ones.
func TestRemove_AnteRunsOnBranchOfLatestContext(t *testing.T) {
	key := storetypes.NewKVStoreKey("ante_state")
	committed := sdktestutil.DefaultContext(key, storetypes.NewTransientStoreKey("ante_transient"))

	funded, _ := crypto.GenerateKey()
	broke, _ := crypto.GenerateKey()
	fundedAddr := crypto.PubkeyToAddress(funded.PublicKey)
	brokeAddr := crypto.PubkeyToAddress(broke.PublicKey)

	// Committed state: funded sender at nonce 0 with balance for 100 fees,
	// broke sender at nonce 0 with nothing.
	writeU64(committed.KVStore(key), nonceKey(fundedAddr), 0)
	writeU64(committed.KVStore(key), balanceKey(fundedAddr), 100)

	ante := &statefulAnte{key: key, fee: 1, errs: make(map[ethcommon.Hash]error)}
	h := newRecheckHarness(t, func(cfg *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		cfg.AnteHandler = ante.handle
		// Like CreateQueryContext: a fresh cache branch of committed state.
		*getCtx = func(int64, bool) (sdk.Context, error) {
			return committed.
				WithMultiStore(committed.MultiStore().CacheMultiStore()).
				WithBlockHeight(1).
				WithBlockHeader(cmtproto.Header{Height: 1, AppHash: []byte("00000000000000000000000000000000")}).
				WithEventManager(sdk.NewEventManager()), nil
		}
	})
	// Install latestCtx (Remove is a no-op before the first block).
	h.mp.GetBlockchain().NotifyNewBlock()
	latest, err := h.mp.GetBlockchain().GetLatestContext()
	require.NoError(t, err)

	decode := func(tx *ethtypes.Transaction) sdk.Tx {
		sdkTx, err := h.txConfig.TxDecoder()(h.cometTxBytes(tx))
		require.NoError(t, err)
		return sdkTx
	}
	addToPool := func(txs ...*ethtypes.Transaction) {
		errs := h.mp.GetTxPool().Add(txs, true)
		for _, err := range errs {
			require.NoError(t, err)
		}
		h.takeSent()
	}
	sign := func(k *ecdsa.PrivateKey, nonce uint64) *ethtypes.Transaction {
		return h.signTx(k, nonce, 1_000_000_000)
	}

	// (b1) Three txs of one sender delivered in one block, Remove called in
	// order. On a branch, nonce 0 passes and nonces 1 and 2 see a nonce gap
	// (the shared context no longer carries nonce 0's increment). Every
	// verdict is "keep": the legacypool drops them by nonce on reset.
	seq := []*ethtypes.Transaction{sign(funded, 0), sign(funded, 1), sign(funded, 2)}
	addToPool(seq...)
	for _, tx := range seq {
		require.NoError(t, h.mp.Remove(decode(tx)))
	}
	err0, ok := ante.errFor(seq[0].Hash())
	require.True(t, ok)
	require.NoError(t, err0)
	for _, tx := range seq[1:] {
		errN, ok := ante.errFor(tx.Hash())
		require.True(t, ok)
		require.ErrorIs(t, errN, mempool.ErrNonceGap, "nonce %d must see the committed nonce", tx.Nonce())
	}
	for _, tx := range seq {
		require.True(t, h.mp.GetTxPool().Has(tx.Hash()), "nonce %d must be kept", tx.Nonce())
	}

	// (b2) An invalid tx (insufficient funds) is removed from the legacypool.
	invalid := sign(broke, 0)
	addToPool(invalid)
	require.NoError(t, h.mp.Remove(decode(invalid)))
	errInv, ok := ante.errFor(invalid.Hash())
	require.True(t, ok)
	require.ErrorIs(t, errInv, sdkerrors.ErrInsufficientFunds)
	require.False(t, h.mp.GetTxPool().Has(invalid.Hash()), "invalid tx must be removed")

	// (a) The shared latest context was never written: nonce and balance
	// are still the committed values. latest is the context installed by
	// NotifyNewBlock; GetLatestContext keeps returning it until the next block.
	after := latest
	require.Equal(t, uint64(0), readU64(after.KVStore(key), nonceKey(fundedAddr)))
	require.Equal(t, uint64(100), readU64(after.KVStore(key), balanceKey(fundedAddr)))
	require.Nil(t, after.KVStore(key).Get(nonceKey(brokeAddr)))
	require.Nil(t, after.KVStore(key).Get(balanceKey(brokeAddr)))
	require.Empty(t, after.EventManager().Events())
}

// Item 7 hardening: a panic in the Remove ante check (inside FinalizeBlock's
// runTx) is recovered, Remove still returns nil and the tx is kept.
func TestRemove_RecoversAntePanic(t *testing.T) {
	h := newRecheckHarness(t, func(cfg *mempool.EVMMempoolConfig, _ *func(int64, bool) (sdk.Context, error)) {
		cfg.AnteHandler = func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) { panic("boom") }
	})
	h.mp.GetBlockchain().NotifyNewBlock()
	key, _ := crypto.GenerateKey()
	tx := h.signTx(key, 0, 1_000_000_000)
	h.addPending(tx)
	sdkTx, err := h.txConfig.TxDecoder()(h.cometTxBytes(tx))
	require.NoError(t, err)
	require.NotPanics(t, func() {
		require.NoError(t, h.mp.Remove(sdkTx))
		require.NoError(t, h.mp.Remove(sdkTx))
	})
	require.True(t, h.mp.GetTxPool().Has(tx.Hash()))
}

// Remove reads the latest height while NotifyNewBlock replaces the latest
// context (run with -race).
func TestRemove_ConcurrentWithNotifyNewBlock(t *testing.T) {
	h := newRecheckHarness(t)
	key, _ := crypto.GenerateKey()
	tx := h.signTx(key, 0, 1_000_000_000)
	h.addPending(tx)
	sdkTx, err := h.txConfig.TxDecoder()(h.cometTxBytes(tx))
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			h.mp.GetBlockchain().NotifyNewBlock()
		}
	}()
	for i := 0; i < 50; i++ {
		require.NoError(t, h.mp.Remove(sdkTx))
	}
	<-done
}
