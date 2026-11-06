package mempool_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/mempool"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	testGenesisTime = time.Unix(1_700_000_000, 0)
	testIdentity    = mempool.ChainIdentity{
		ChainID: "stoc_9001-1", InitialHeight: 1, GenesisTime: uint64(testGenesisTime.UnixNano()),
	}
)

// fakeCometRPC answers Status and records BroadcastTxSync bytes.
type fakeCometRPC struct {
	client.CometRPC
	catchingUp *atomic.Bool

	mu        *sync.Mutex
	broadcast *[][]byte
}

func (f fakeCometRPC) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{CatchingUp: f.catchingUp.Load()}}, nil
}

func (f fakeCometRPC) BroadcastTxSync(_ context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.broadcast = append(*f.broadcast, append([]byte(nil), tx...))
	return &coretypes.ResultBroadcastTx{Code: 0, Hash: tx.Hash()}, nil
}

func newFakeRPC(catchingUp bool) fakeCometRPC {
	f := fakeCometRPC{catchingUp: &atomic.Bool{}, mu: &sync.Mutex{}, broadcast: &[][]byte{}}
	f.catchingUp.Store(catchingUp)
	return f
}

func (f fakeCometRPC) sent() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), *f.broadcast...)
}

func withJournal(path string) harnessOption {
	return func(cfg *mempool.EVMMempoolConfig, _ *func(int64, bool) (sdk.Context, error)) {
		cfg.JournalPath = path
	}
}

// newJournalHarness is a harness with the journal on and the chain identity
// set, like server start does.
func newJournalHarness(t *testing.T, path string, opts ...harnessOption) *recheckHarness {
	t.Helper()
	h := newRecheckHarness(t, append([]harnessOption{withJournal(path)}, opts...)...)
	h.mp.SetChainIdentity(testIdentity.ChainID, int64(testIdentity.InitialHeight), testGenesisTime)
	return h
}

func journalPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), mempool.JournalDirName, mempool.JournalFileName)
}

func senderOf(t *testing.T, tx *ethtypes.Transaction) ethcommon.Address {
	t.Helper()
	from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
	require.NoError(t, err)
	return from
}

func sortSenderNonce(t *testing.T, txs []*ethtypes.Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		a, b := senderOf(t, txs[i]), senderOf(t, txs[j])
		if a != b {
			return a.Cmp(b) < 0
		}
		return txs[i].Nonce() < txs[j].Nonce()
	})
}

// journalTxs fills h's legacypool with two pending txs of one sender and one
// queued (nonce-gap) tx of another, and returns them in sender+nonce order.
func journalTxs(t *testing.T, h *recheckHarness) []*ethtypes.Transaction {
	t.Helper()
	k1, _ := crypto.GenerateKey()
	k2, _ := crypto.GenerateKey()
	txs := []*ethtypes.Transaction{
		h.signTx(k1, 0, 1_000_000_000),
		h.signTx(k1, 1, 1_000_000_000),
		h.signTx(k2, 3, 1_000_000_000), // queued: nonce gap
	}
	for _, err := range h.mp.GetTxPool().Add(txs, true) {
		require.NoError(t, err)
	}
	h.takeSent()
	pending, queued := h.mp.GetTxPool().Stats()
	require.Equal(t, 2, pending)
	require.Equal(t, 1, queued)
	sortSenderNonce(t, txs)
	return txs
}

func txHashes(txs []*ethtypes.Transaction) []ethcommon.Hash {
	out := make([]ethcommon.Hash, len(txs))
	for i, tx := range txs {
		out[i] = tx.Hash()
	}
	return out
}

func waitSent(t *testing.T, h *recheckHarness, n int) []*ethtypes.Transaction {
	t.Helper()
	var sent []*ethtypes.Transaction
	require.Eventually(t, func() bool {
		sent = append(sent, h.takeSent()...)
		return len(sent) >= n
	}, 5*time.Second, 10*time.Millisecond)
	return sent
}

// Close writes pending+queued txs to the journal; after restart the journal
// is read once, deleted, and its txs go to BroadcastTxFn in sender+nonce order.
func TestJournal_CloseWritesAndRestartResubmits(t *testing.T) {
	path := journalPath(t)

	before := newJournalHarness(t, path)
	want := journalTxs(t, before)
	require.NoError(t, before.mp.Close())
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	hdr, _, err := mempool.ReadTestJournal(path)
	require.NoError(t, err)
	require.Equal(t, testIdentity, hdr.Identity)
	require.Equal(t, uint64(1), hdr.Height)

	after := newJournalHarness(t, path)
	after.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(false)))
	require.Equal(t, txHashes(want), txHashes(waitSent(t, after, len(want))))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "journal must be deleted once read")

	// Replay runs once only.
	after.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(false)))
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, after.takeSent())
}

// While CometBFT catches up nothing is re-submitted; a shutdown meanwhile
// writes the not-yet-sent txs back with the ORIGINAL header.
func TestJournal_WaitsForSyncAndWritesBackOnClose(t *testing.T) {
	defer mempool.SetJournalReadyPoll(10 * time.Millisecond)()
	path := journalPath(t)

	before := newJournalHarness(t, path)
	want := journalTxs(t, before)
	require.NoError(t, before.mp.Close())
	origHdr, origEntries, err := mempool.ReadTestJournal(path)
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond) // a rewrite would get a new Time
	after := newJournalHarness(t, path)
	after.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(true)))
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}, 5*time.Second, 10*time.Millisecond, "journal read and deleted")
	time.Sleep(100 * time.Millisecond)
	require.Empty(t, after.takeSent(), "nothing re-submitted while catching up")

	require.NoError(t, after.mp.Close())
	hdr, entries, err := mempool.ReadTestJournal(path)
	require.NoError(t, err, "unsent txs written back")
	require.Equal(t, origHdr, hdr, "original header kept")
	require.Len(t, entries, len(origEntries))
	for i := range entries {
		require.Equal(t, origEntries[i].FirstSeen, entries[i].FirstSeen)
	}

	again := newJournalHarness(t, path)
	again.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(false)))
	require.Equal(t, txHashes(want), txHashes(waitSent(t, again, len(want))))
}

// Close before the journal was loaded: with nothing new the file is left
// byte for byte; with new pool txs the carried entries keep their first-seen
// time.
func TestJournal_CloseBeforeReplayKeepsOriginal(t *testing.T) {
	path := journalPath(t)
	seed := newJournalHarness(t, path)
	k, _ := crypto.GenerateKey()
	old := seed.signTx(k, 0, 1_000_000_000)
	firstSeen := uint64(time.Now().Add(-2 * time.Hour).Unix())
	written := uint64(time.Now().Add(-time.Hour).Unix())
	orig := mempool.TestJournalHeader{Identity: testIdentity, Height: 1, Time: written}
	require.NoError(t, mempool.WriteTestJournal(path, orig, []mempool.TestJournalEntry{{FirstSeen: firstSeen, Tx: old}}))
	origBytes, err := os.ReadFile(path)
	require.NoError(t, err)

	// No RPC client: no replay. Empty pool: no rewrite.
	idle := newJournalHarness(t, path)
	require.NoError(t, idle.mp.Close())
	now, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(origBytes, now), "unread journal must not be rewritten")

	// New pool content: merged, carried entry keeps its first-seen time.
	busy := newJournalHarness(t, path)
	k2, _ := crypto.GenerateKey()
	fresh := busy.signTx(k2, 0, 1_000_000_000)
	busy.addPending(fresh)
	require.NoError(t, busy.mp.Close())
	_, entries, err := mempool.ReadTestJournal(path)
	require.NoError(t, err)
	got := map[ethcommon.Hash]uint64{}
	for _, e := range entries {
		got[e.Tx.Hash()] = e.FirstSeen
	}
	require.Len(t, got, 2)
	require.Equal(t, firstSeen, got[old.Hash()])
	require.Greater(t, got[fresh.Hash()], firstSeen)
}

// A journal of another chain (chain-id or genesis), with height 0, or whose
// entries are older than the max age is not replayed.
func TestJournal_DiscardedJournals(t *testing.T) {
	seed := newJournalHarness(t, journalPath(t))
	k, _ := crypto.GenerateKey()
	tx := seed.signTx(k, 0, 1_000_000_000)
	recent := uint64(time.Now().Add(-time.Hour).Unix())
	nowSec := uint64(time.Now().Unix())

	otherChain := testIdentity
	otherChain.ChainID = "stoc_9001-2"
	otherGenesis := testIdentity
	otherGenesis.GenesisTime++
	cases := map[string]struct {
		hdr       mempool.TestJournalHeader
		firstSeen uint64
	}{
		"chain-id mismatch": {mempool.TestJournalHeader{Identity: otherChain, Height: 1, Time: nowSec}, recent},
		"genesis mismatch":  {mempool.TestJournalHeader{Identity: otherGenesis, Height: 1, Time: nowSec}, recent},
		"height 0":          {mempool.TestJournalHeader{Identity: testIdentity, Height: 0, Time: nowSec}, recent},
		"chain reset":       {mempool.TestJournalHeader{Identity: testIdentity, Height: 5000, Time: nowSec}, recent},
		"older than 24h": {
			mempool.TestJournalHeader{Identity: testIdentity, Height: 1, Time: nowSec},
			uint64(time.Now().Add(-25 * time.Hour).Unix()),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := journalPath(t)
			require.NoError(t, mempool.WriteTestJournal(path, tc.hdr, []mempool.TestJournalEntry{{FirstSeen: tc.firstSeen, Tx: tx}}))
			h := newJournalHarness(t, path)
			h.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(false)))
			require.Eventually(t, func() bool {
				_, err := os.Stat(path)
				return os.IsNotExist(err)
			}, 5*time.Second, 10*time.Millisecond)
			time.Sleep(100 * time.Millisecond)
			require.Empty(t, h.takeSent())
		})
	}
}

// A tx keeps its first-seen time across restarts: re-journaled after replay,
// it can never be replayed more than 24h after it was first seen.
func TestJournal_FirstSeenSurvivesRestarts(t *testing.T) {
	path := journalPath(t)
	seed := newJournalHarness(t, path)
	k, _ := crypto.GenerateKey()
	tx := seed.signTx(k, 0, 1_000_000_000)
	firstSeen := uint64(time.Now().Add(-23 * time.Hour).Unix())
	require.NoError(t, mempool.WriteTestJournal(path,
		mempool.TestJournalHeader{Identity: testIdentity, Height: 1, Time: uint64(time.Now().Unix())},
		[]mempool.TestJournalEntry{{FirstSeen: firstSeen, Tx: tx}}))

	h := newJournalHarness(t, path)
	h.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(false)))
	sent := waitSent(t, h, 1)
	require.Equal(t, tx.Hash(), sent[0].Hash())
	// The re-submitted tx enters the pool again (as CheckTx would do).
	h.addPending(tx)
	require.NoError(t, h.mp.Close())
	_, entries, err := mempool.ReadTestJournal(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, firstSeen, entries[0].FirstSeen, "first-seen time must not be refreshed")
}

// Latest height unknown (0): nothing is written.
func TestJournal_UnknownHeightWritesNothing(t *testing.T) {
	path := journalPath(t)
	h := newJournalHarness(t, path, func(_ *mempool.EVMMempoolConfig, getCtx *func(int64, bool) (sdk.Context, error)) {
		inner := *getCtx
		*getCtx = func(height int64, prove bool) (sdk.Context, error) {
			ctx, err := inner(height, prove)
			return ctx.WithBlockHeight(0), err
		}
	})
	journalTxs(t, h)
	require.NoError(t, h.mp.Close())
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Replay through the production broadcast path (broadcastEVMTransactions
// with a CometBFT client): the client receives the eth_sendRawTransaction
// bytes of every journaled tx, in sender+nonce order.
func TestJournal_ReplayThroughDefaultBroadcastPath(t *testing.T) {
	path := journalPath(t)
	before := newJournalHarness(t, path)
	want := journalTxs(t, before)
	require.NoError(t, before.mp.Close())

	after := newJournalHarness(t, path, func(cfg *mempool.EVMMempoolConfig, _ *func(int64, bool) (sdk.Context, error)) {
		cfg.BroadCastTxFn = nil
	})
	rpc := newFakeRPC(false)
	after.mp.SetClientCtx(client.Context{}.WithClient(rpc).WithTxConfig(after.txConfig))
	require.Eventually(t, func() bool { return len(rpc.sent()) >= len(want) }, 5*time.Second, 10*time.Millisecond)
	got := rpc.sent()
	require.Len(t, got, len(want))
	for i, tx := range want {
		require.Equal(t, after.cometTxBytes(tx), got[i], "tx %d bytes", i)
	}
}

// A foreign journal still waiting for sync when the node stops is written
// back with its own header, never re-stamped with this chain's identity.
func TestJournal_ForeignJournalNotRestamped(t *testing.T) {
	defer mempool.SetJournalReadyPoll(10 * time.Millisecond)()
	path := journalPath(t)
	seed := newJournalHarness(t, path)
	k, _ := crypto.GenerateKey()
	tx := seed.signTx(k, 0, 1_000_000_000)
	foreign := testIdentity
	foreign.ChainID = "other_1-1"
	hdr := mempool.TestJournalHeader{Identity: foreign, Height: 1, Time: uint64(time.Now().Unix())}
	require.NoError(t, mempool.WriteTestJournal(path, hdr, []mempool.TestJournalEntry{{FirstSeen: hdr.Time, Tx: tx}}))

	h := newJournalHarness(t, path)
	h.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(true))) // catching up
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}, 5*time.Second, 10*time.Millisecond)

	// Pool has a new tx: the foreign entry must be dropped, not merged.
	k2, _ := crypto.GenerateKey()
	fresh := h.signTx(k2, 0, 1_000_000_000)
	h.addPending(fresh)
	require.NoError(t, h.mp.Close())
	got, entries, err := mempool.ReadTestJournal(path)
	require.NoError(t, err)
	require.Equal(t, testIdentity, got.Identity)
	require.Len(t, entries, 1)
	require.Equal(t, fresh.Hash(), entries[0].Tx.Hash())

	// Empty pool: written back with the foreign header untouched.
	require.NoError(t, mempool.WriteTestJournal(path, hdr, []mempool.TestJournalEntry{{FirstSeen: hdr.Time, Tx: tx}}))
	h2 := newJournalHarness(t, path)
	h2.mp.SetClientCtx(client.Context{}.WithClient(newFakeRPC(true)))
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, h2.mp.Close())
	got, _, err = mempool.ReadTestJournal(path)
	require.NoError(t, err)
	require.Equal(t, hdr, got)
}
