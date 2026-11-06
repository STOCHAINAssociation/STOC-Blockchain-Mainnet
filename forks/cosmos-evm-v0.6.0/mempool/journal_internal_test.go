package mempool

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
)

var journalTestChainID = big.NewInt(9001)

var testIdentity = &ChainIdentity{ChainID: "stoc_9001-1", InitialHeight: 1, GenesisTime: 1_700_000_000_000_000_000}

func journalTestTx(t *testing.T, key *ecdsa.PrivateKey, nonce uint64, data []byte) *ethtypes.Transaction {
	t.Helper()
	to := ethcommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(journalTestChainID), &ethtypes.DynamicFeeTx{
		ChainID:   journalTestChainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       100_000,
		To:        &to,
		Data:      data,
	})
	require.NoError(t, err)
	return tx
}

func hashesOf(txs []*ethtypes.Transaction) []ethcommon.Hash {
	out := make([]ethcommon.Hash, len(txs))
	for i, tx := range txs {
		out[i] = tx.Hash()
	}
	return out
}

func entriesOf(seen uint64, txs ...*ethtypes.Transaction) []journalEntry {
	out := make([]journalEntry, len(txs))
	for i, tx := range txs {
		out[i] = journalEntry{FirstSeen: seen, Tx: tx}
	}
	return out
}

func testJournalFile(height uint64, entries []journalEntry) *journalFile {
	return &journalFile{
		Version: journalVersion, ChainID: testIdentity.ChainID, InitialHeight: testIdentity.InitialHeight,
		GenesisTime: testIdentity.GenesisTime, Height: height, Time: uint64(time.Now().Unix()), Entries: entries,
	}
}

func TestJournal_RoundTrip(t *testing.T) {
	key, _ := crypto.GenerateKey()
	txs := []*ethtypes.Transaction{journalTestTx(t, key, 0, nil), journalTestTx(t, key, 1, []byte{1, 2, 3})}
	jf := testJournalFile(42, entriesOf(1_700_000_123, txs...))

	data, err := encodeJournal(jf, journalMaxBytes)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), JournalDirName, JournalFileName)
	require.NoError(t, writeJournalFile(path, data))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
	// No temp file left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	got, err := readJournalFile(path, journalMaxBytes)
	require.NoError(t, err)
	require.Equal(t, uint64(42), got.Height)
	require.Equal(t, jf.Time, got.Time)
	require.Equal(t, testIdentity.ChainID, got.ChainID)
	require.Equal(t, testIdentity.GenesisTime, got.GenesisTime)
	require.Equal(t, hashesOf(txs), hashesOf(got.txs()))
	require.Equal(t, uint64(1_700_000_123), got.Entries[1].FirstSeen)
}

func TestJournal_SelectBounds(t *testing.T) {
	keys := make([]*ecdsa.PrivateKey, 3)
	pending := map[ethcommon.Address][]*ethtypes.Transaction{}
	queued := map[ethcommon.Address][]*ethtypes.Transaction{}
	for i := range keys {
		keys[i], _ = crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(keys[i].PublicKey)
		// Out of order on purpose: selection sorts by nonce.
		pending[addr] = []*ethtypes.Transaction{journalTestTx(t, keys[i], 1, nil), journalTestTx(t, keys[i], 0, nil)}
		queued[addr] = []*ethtypes.Transaction{journalTestTx(t, keys[i], 5, nil)}
	}
	groups := []map[ethcommon.Address][]*ethtypes.Transaction{pending, queued}

	all := selectJournalTxs(groups, journalMaxTxs, journalMaxBytes)
	require.Len(t, all, 9)
	// Pending first, nonce order per sender; queued after.
	for i := 0; i < 6; i += 2 {
		require.Equal(t, uint64(0), all[i].Nonce())
		require.Equal(t, uint64(1), all[i+1].Nonce())
	}
	for i := 6; i < 9; i++ {
		require.Equal(t, uint64(5), all[i].Nonce())
	}

	// Tx count bound.
	require.Len(t, selectJournalTxs(groups, 4, journalMaxBytes), 4)

	// Byte bound: room for exactly two txs.
	two := all[0].Size() + journalEntryOverhead + all[1].Size() + journalEntryOverhead
	require.Len(t, selectJournalTxs(groups, journalMaxTxs, two), 2)
	require.Empty(t, selectJournalTxs(groups, journalMaxTxs, 10))

	// Duplicates across groups are written once.
	dup := []map[ethcommon.Address][]*ethtypes.Transaction{pending, pending}
	require.Len(t, selectJournalTxs(dup, journalMaxTxs, journalMaxBytes), 6)

	// A journal with more than journalMaxTxs txs is refused on write and read.
	many := make([]journalEntry, journalMaxTxs+1)
	for i := range many {
		many[i] = journalEntry{FirstSeen: 1, Tx: all[0]}
	}
	_, err := encodeJournal(testJournalFile(1, many), journalMaxBytes)
	require.ErrorIs(t, err, errJournalOversized)
	raw, err := rlp.EncodeToBytes(testJournalFile(1, many))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), JournalFileName)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	_, err = readJournalFile(path, journalMaxBytes)
	require.ErrorIs(t, err, errJournalOversized)

	// Byte bound on encode.
	_, err = encodeJournal(testJournalFile(1, entriesOf(1, all...)), 64)
	require.ErrorIs(t, err, errJournalOversized)
}

func newJournalTestMempool(path string) *ExperimentalEVMMempool {
	m := &ExperimentalEVMMempool{logger: log.NewNopLogger(), journal: newMempoolJournal(path)}
	m.journal.identity.Store(testIdentity)
	return m
}

// Corrupt, oversized or unknown-version journals are logged, deleted and
// skipped; startup goes on.
func TestJournal_BadFilesAreDeleted(t *testing.T) {
	key, _ := crypto.GenerateKey()
	v1 := testJournalFile(5, entriesOf(1, journalTestTx(t, key, 0, nil)))
	v1.Version = 1
	wrongVersion, err := rlp.EncodeToBytes(v1)
	require.NoError(t, err)

	cases := map[string]func(path string){
		"corrupt": func(path string) {
			require.NoError(t, os.WriteFile(path, []byte("definitely not rlp"), 0o600))
		},
		"truncated": func(path string) {
			good, err := encodeJournal(testJournalFile(5, entriesOf(1, journalTestTx(t, key, 0, nil))), journalMaxBytes)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, good[:len(good)-5], 0o600))
		},
		"oversized": func(path string) {
			f, err := os.Create(path)
			require.NoError(t, err)
			require.NoError(t, f.Truncate(journalMaxBytes+1)) // sparse
			require.NoError(t, f.Close())
		},
		"wrong version": func(path string) {
			require.NoError(t, os.WriteFile(path, wrongVersion, 0o600))
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), JournalFileName)
			mk(path)
			m := newJournalTestMempool(path)
			jf, ok := m.loadJournal()
			require.False(t, ok)
			require.Nil(t, jf)
			_, err := os.Stat(path)
			require.ErrorIs(t, err, os.ErrNotExist, "bad journal must be deleted")
		})
	}

	// Missing file: nothing to do.
	m := newJournalTestMempool(filepath.Join(t.TempDir(), JournalFileName))
	_, ok := m.loadJournal()
	require.False(t, ok)
}

// A good journal is read once and deleted before anything is re-submitted;
// leftover temp files of an interrupted write are deleted too.
func TestJournal_LoadDeletesJournalAndTempFiles(t *testing.T) {
	key, _ := crypto.GenerateKey()
	txs := []*ethtypes.Transaction{journalTestTx(t, key, 0, nil)}
	data, err := encodeJournal(testJournalFile(7, entriesOf(1_700_000_000, txs...)), journalMaxBytes)
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), JournalDirName)
	path := filepath.Join(dir, JournalFileName)
	require.NoError(t, writeJournalFile(path, data))
	for _, name := range []string{JournalFileName + ".tmp-123", JournalFileName + ".tmp-abc"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("partial"), 0o600))
	}
	unrelated := filepath.Join(dir, "keep.me")
	require.NoError(t, os.WriteFile(unrelated, []byte("x"), 0o600))

	m := newJournalTestMempool(path)
	jf, ok := m.loadJournal()
	require.True(t, ok)
	require.Equal(t, hashesOf(txs), hashesOf(jf.txs()))
	left, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, left, 1, "journal and temp files deleted")
	require.Equal(t, "keep.me", left[0].Name())
	require.Len(t, m.journal.takeUnsent(), 1)
	fs, ok := m.journal.firstSeenOf(txs[0].Hash())
	require.True(t, ok)
	require.Equal(t, uint64(1_700_000_000), fs)

	// A halted journal (mempool closing) is not loaded.
	require.NoError(t, writeJournalFile(path, data))
	m2 := newJournalTestMempool(path)
	m2.journal.halt()
	_, ok = m2.loadJournal()
	require.False(t, ok)
	_, err = os.Stat(path)
	require.NoError(t, err, "a journal not loaded must stay on disk")
}

// Identity and height checks applied before any replay.
func TestJournal_CheckIdentityAndHeight(t *testing.T) {
	jf := testJournalFile(100, nil)
	require.NoError(t, checkJournal(jf, testIdentity, 100))
	require.NoError(t, checkJournal(jf, testIdentity, 95), "a rollback of a few blocks is tolerated")

	other := *testIdentity
	other.ChainID = "stoc_9001-2"
	require.ErrorIs(t, checkJournal(jf, &other, 100), errJournalForeign)
	other = *testIdentity
	other.GenesisTime++
	require.ErrorIs(t, checkJournal(jf, &other, 100), errJournalForeign, "same chain-id, new genesis")
	other = *testIdentity
	other.InitialHeight = 1000
	require.ErrorIs(t, checkJournal(jf, &other, 100), errJournalForeign)

	require.Error(t, checkJournal(jf, nil, 100), "unknown node identity")
	require.Error(t, checkJournal(jf, &ChainIdentity{}, 100), "unknown node identity")
	require.Error(t, checkJournal(testJournalFile(0, nil), testIdentity, 100), "height 0 is unknown")
	require.Error(t, checkJournal(jf, testIdentity, 20), "chain wiped")
}

// The age check uses each entry's first-seen time.
func TestJournal_FreshEntriesUseFirstSeen(t *testing.T) {
	key, _ := crypto.GenerateKey()
	now := time.Now()
	young := journalEntry{FirstSeen: uint64(now.Add(-time.Hour).Unix()), Tx: journalTestTx(t, key, 0, nil)}
	old := journalEntry{FirstSeen: uint64(now.Add(-journalMaxAge - time.Minute).Unix()), Tx: journalTestTx(t, key, 1, nil)}
	unknown := journalEntry{FirstSeen: 0, Tx: journalTestTx(t, key, 2, nil)}
	got := freshEntries([]journalEntry{young, old, unknown}, now)
	require.Len(t, got, 1)
	require.Equal(t, young.Tx.Hash(), got[0].Tx.Hash())
}

// Review L4 (round 4): an entry "first seen" in the future would never
// expire; anything beyond the clock skew is dropped.
func TestJournal_FutureFirstSeenRejected(t *testing.T) {
	key, _ := crypto.GenerateKey()
	now := time.Now()
	skewed := journalEntry{FirstSeen: uint64(now.Add(journalClockSkew / 2).Unix()), Tx: journalTestTx(t, key, 0, nil)}
	future := journalEntry{FirstSeen: uint64(now.Add(journalClockSkew + time.Minute).Unix()), Tx: journalTestTx(t, key, 1, nil)}
	far := journalEntry{FirstSeen: ^uint64(0), Tx: journalTestTx(t, key, 2, nil)}
	got := freshEntries([]journalEntry{skewed, future, far}, now)
	require.Len(t, got, 1)
	require.Equal(t, skewed.Tx.Hash(), got[0].Tx.Hash())
}

// The entry cap is enforced while decoding: decoding stops at entry cap+1.
func TestJournal_DecodeStopsAtCap(t *testing.T) {
	key, _ := crypto.GenerateKey()
	tx := journalTestTx(t, key, 0, nil)
	good := entriesOf(1, tx, tx, tx)
	raw, err := rlp.EncodeToBytes(testJournalFile(9, good))
	require.NoError(t, err)

	jf, err := decodeJournal(raw, 3)
	require.NoError(t, err)
	require.Len(t, jf.Entries, 3)
	require.Equal(t, uint64(9), jf.Height)
	require.Equal(t, testIdentity.ChainID, jf.ChainID)

	_, err = decodeJournal(raw, 2)
	require.ErrorIs(t, err, errJournalOversized)

	// A corrupt tail after the cap is never reached: the cap error wins,
	// proving decoding stopped instead of reading the whole list first.
	type rawJournal struct {
		Version       uint64
		ChainID       string
		InitialHeight uint64
		GenesisTime   uint64
		Height        uint64
		Time          uint64
		Entries       []rlp.RawValue
	}
	rawEntries := make([]rlp.RawValue, 0, 4)
	for _, e := range good {
		bz, err := rlp.EncodeToBytes(e)
		require.NoError(t, err)
		rawEntries = append(rawEntries, bz)
	}
	corrupt, err := rlp.EncodeToBytes([]any{uint64(1), []byte{0x02, 0xde, 0xad}}) // not a valid typed tx
	require.NoError(t, err)
	rawEntries = append(rawEntries, corrupt)
	raw2, err := rlp.EncodeToBytes(rawJournal{
		Version: journalVersion, ChainID: testIdentity.ChainID, InitialHeight: testIdentity.InitialHeight,
		GenesisTime: testIdentity.GenesisTime, Height: 9, Time: 1, Entries: rawEntries,
	})
	require.NoError(t, err)
	_, err = decodeJournal(raw2, 2)
	require.ErrorIs(t, err, errJournalOversized)
	_, err = decodeJournal(raw2, 10)
	require.Error(t, err)
	require.NotErrorIs(t, err, errJournalOversized)

	// Trailing bytes after the journal are rejected.
	_, err = decodeJournal(append(append([]byte{}, raw...), 0x80), 10)
	require.Error(t, err)
}

// A failed directory fsync after a successful rename is a warning: the
// journal is in place and the write counts as done.
func TestJournal_DirSyncFailureIsNotAWriteFailure(t *testing.T) {
	old := syncDir
	syncDir = func(string) error { return errors.New("fsync: not supported") }
	defer func() { syncDir = old }()

	key, _ := crypto.GenerateKey()
	path := filepath.Join(t.TempDir(), JournalDirName, JournalFileName)
	data, err := encodeJournal(testJournalFile(3, entriesOf(uint64(time.Now().Unix()), journalTestTx(t, key, 0, nil))), journalMaxBytes)
	require.NoError(t, err)
	require.ErrorIs(t, writeJournalFile(path, data), errJournalDirSync)

	m := newJournalTestMempool(path)
	require.NoError(t, m.persistJournal(testJournalFile(3, entriesOf(uint64(time.Now().Unix()), journalTestTx(t, key, 1, nil)))))
	got, err := readJournalFile(path, journalMaxBytes)
	require.NoError(t, err)
	require.Len(t, got.Entries, 1)
}
