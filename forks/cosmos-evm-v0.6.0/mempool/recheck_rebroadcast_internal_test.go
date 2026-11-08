package mempool

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var internalTestChainID = big.NewInt(9001)

const (
	timeoutForTest = 5 * time.Second
	tickForTest    = 10 * time.Millisecond
)

func signedDynTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, feeCap int64) *ethtypes.Transaction {
	t.Helper()
	to := ethcommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(internalTestChainID), &ethtypes.DynamicFeeTx{
		ChainID:   internalTestChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	require.NoError(t, err)
	return tx
}

// fakeRebroadcaster wires a recheckRebroadcaster to an in-memory "pool".
type fakeRebroadcaster struct {
	mu        sync.Mutex
	pool      map[ethcommon.Hash]*ethtypes.Transaction
	baseFee   *big.Int
	anteErr   map[ethcommon.Hash]error
	anteCalls [][]*ethtypes.Transaction
	sent      [][]*ethtypes.Transaction
	r         *recheckRebroadcaster
}

func newFakeRebroadcaster(capacity int) *fakeRebroadcaster {
	f := &fakeRebroadcaster{
		pool:    make(map[ethcommon.Hash]*ethtypes.Transaction),
		anteErr: make(map[ethcommon.Hash]error),
		baseFee: big.NewInt(1_000_000_000),
	}
	f.r = &recheckRebroadcaster{
		logger: log.NewNopLogger(),
		set:    newRecheckDroppedSet(capacity),
		getTx: func(h ethcommon.Hash) *ethtypes.Transaction {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.pool[h]
		},
		baseFee: func() *big.Int {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.baseFee
		},
		checkIncludable: func(txs []*ethtypes.Transaction) []error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.anteCalls = append(f.anteCalls, txs)
			errs := make([]error, len(txs))
			for i, tx := range txs {
				errs[i] = f.anteErr[tx.Hash()]
			}
			return errs
		},
		broadcast: func(txs []*ethtypes.Transaction) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sent = append(f.sent, txs)
			return nil
		},
	}
	return f
}

func (f *fakeRebroadcaster) addToPool(tx *ethtypes.Transaction) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pool[tx.Hash()] = tx
}

func TestRecheckDroppedSet_Bounded(t *testing.T) {
	s := newRecheckDroppedSet(3)
	h := func(b byte) ethcommon.Hash { return ethcommon.Hash{b} }

	for i := byte(1); i <= 3; i++ {
		added, full := s.add(h(i))
		require.True(t, added)
		require.False(t, full)
	}
	// Duplicate is a no-op, not "full".
	added, full := s.add(h(1))
	require.False(t, added)
	require.False(t, full)

	// Full: refused, size stays at cap.
	added, full = s.add(h(4))
	require.False(t, added)
	require.True(t, full)
	require.Equal(t, 3, s.len())
	require.False(t, s.has(h(4)))

	// Room again after a removal.
	s.remove(h(2))
	added, _ = s.add(h(4))
	require.True(t, added)
	require.Equal(t, []ethcommon.Hash{h(1), h(3), h(4)}, s.snapshot())

	s.moveToBack(h(1))
	require.Equal(t, []ethcommon.Hash{h(3), h(4), h(1)}, s.snapshot())
}

func TestRecheckDroppedCapIs10k(t *testing.T) {
	require.Equal(t, 10_000, recheckDroppedCap)
}

func TestIsTemporaryRecheckError(t *testing.T) {
	require.True(t, isTemporaryRecheckError(sdkerrors.ErrInsufficientFee.Wrap("max fee per gas less than block base fee")))
	require.True(t, isTemporaryRecheckError(sdkerrors.ErrInsufficientFunds.Wrap("failed to check sender balance")))
	require.False(t, isTemporaryRecheckError(sdkerrors.ErrInvalidSequence))
	require.False(t, isTemporaryRecheckError(ErrNonceLow))
	require.False(t, isTemporaryRecheckError(errors.New("boom")))
	require.False(t, isTemporaryRecheckError(nil))
}

// Fee-stuck tx: kept while fee cap < base fee, re-broadcast exactly once
// when the base fee falls back, then forgotten.
func TestRecheckRebroadcaster_FeeStuckThenIncludable(t *testing.T) {
	f := newFakeRebroadcaster(recheckDroppedCap)
	key, _ := crypto.GenerateKey()
	tx := signedDynTx(t, key, 0, 1_000_000_000)
	f.addToPool(tx)

	f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFee)
	require.Equal(t, 1, f.r.set.len())

	// Base fee above the fee cap: no ante run, no broadcast, still tracked.
	f.baseFee = big.NewInt(1_125_000_000)
	f.r.process()
	require.Empty(t, f.sent)
	require.Empty(t, f.anteCalls)
	require.True(t, f.r.set.has(tx.Hash()))

	// Base fee back at the fee cap: re-broadcast once.
	f.baseFee = big.NewInt(1_000_000_000)
	f.r.process()
	require.Len(t, f.sent, 1)
	require.Equal(t, []*ethtypes.Transaction{tx}, f.sent[0])
	require.Equal(t, 0, f.r.set.len())

	// Next block: nothing more.
	f.r.process()
	require.Len(t, f.sent, 1)
}

func TestRecheckRebroadcaster_ForgetsTxGoneFromPool(t *testing.T) {
	f := newFakeRebroadcaster(recheckDroppedCap)
	key, _ := crypto.GenerateKey()
	tx := signedDynTx(t, key, 0, 1_000_000_000)

	// Not in the pool (mined / replaced / evicted).
	f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFee)
	f.r.process()
	require.Empty(t, f.sent)
	require.Equal(t, 0, f.r.set.len())
}

func TestRecheckRebroadcaster_AnteFailureKeepsTx(t *testing.T) {
	f := newFakeRebroadcaster(recheckDroppedCap)
	key, _ := crypto.GenerateKey()
	tx := signedDynTx(t, key, 0, 1_000_000_000)
	f.addToPool(tx)
	f.anteErr[tx.Hash()] = sdkerrors.ErrInsufficientFunds

	f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFunds)
	f.r.process()
	require.Empty(t, f.sent)
	require.True(t, f.r.set.has(tx.Hash()))

	// Funds arrive.
	delete(f.anteErr, tx.Hash())
	f.r.process()
	require.Len(t, f.sent, 1)
	require.Equal(t, 0, f.r.set.len())
}

// Txs of one sender are checked and re-broadcast in nonce order so the
// re-broadcast CheckTx of nonce n+1 sees nonce n already applied.
func TestRecheckRebroadcaster_NonceOrder(t *testing.T) {
	f := newFakeRebroadcaster(recheckDroppedCap)
	key, _ := crypto.GenerateKey()
	tx0 := signedDynTx(t, key, 0, 1_000_000_000)
	tx1 := signedDynTx(t, key, 1, 1_000_000_000)
	tx2 := signedDynTx(t, key, 2, 1_000_000_000)
	for _, tx := range []*ethtypes.Transaction{tx2, tx0, tx1} {
		f.addToPool(tx)
		f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFee)
	}

	f.r.process()
	require.Len(t, f.sent, 1)
	require.Equal(t, []uint64{0, 1, 2}, []uint64{f.sent[0][0].Nonce(), f.sent[0][1].Nonce(), f.sent[0][2].Nonce()})
}

// schedule must not run overlapping rounds.
func TestRecheckRebroadcaster_ScheduleNoOverlap(t *testing.T) {
	f := newFakeRebroadcaster(recheckDroppedCap)
	key, _ := crypto.GenerateKey()
	tx := signedDynTx(t, key, 0, 1_000_000_000)
	f.addToPool(tx)
	f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFee)

	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	f.r.broadcast = func(txs []*ethtypes.Transaction) error {
		entered <- struct{}{}
		<-release
		return nil
	}
	f.r.schedule()
	<-entered
	// A second round while the first is blocked is skipped.
	f.r.record(tx.Hash(), sdkerrors.ErrInsufficientFee)
	f.r.schedule()
	require.Len(t, entered, 0)
	close(release)
	require.Eventually(t, func() bool { return !f.r.running.Load() }, timeoutForTest, tickForTest)
}
