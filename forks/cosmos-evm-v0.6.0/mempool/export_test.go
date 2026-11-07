package mempool

import (
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// ProcessRecheckDropped runs one re-broadcast round synchronously (tests only).
func (m *ExperimentalEVMMempool) ProcessRecheckDropped() { m.recheckRebroadcast.process() }

// RecheckDroppedLen returns the number of remembered recheck drops (tests only).
func (m *ExperimentalEVMMempool) RecheckDroppedLen() int { return m.recheckRebroadcast.set.len() }

// RecheckDroppedHas reports whether hash is remembered (tests only).
func (m *ExperimentalEVMMempool) RecheckDroppedHas(hash ethcommon.Hash) bool {
	return m.recheckRebroadcast.set.has(hash)
}

// SetJournalReadyPoll overrides the journal replay readiness poll interval
// and returns a restore func (tests only).
func SetJournalReadyPoll(d time.Duration) (restore func()) {
	old := journalReadyPoll
	journalReadyPoll = d
	return func() { journalReadyPoll = old }
}

// TestJournalEntry is a journal entry as seen by tests.
type TestJournalEntry struct {
	FirstSeen uint64
	Tx        *ethtypes.Transaction
}

// TestJournalHeader is a journal header as seen by tests.
type TestJournalHeader struct {
	Identity ChainIdentity
	Height   uint64
	Time     uint64
}

// ReadTestJournal reads a journal file (tests only).
func ReadTestJournal(path string) (TestJournalHeader, []TestJournalEntry, error) {
	jf, err := readJournalFile(path, journalMaxBytes)
	if err != nil {
		return TestJournalHeader{}, nil, err
	}
	h := TestJournalHeader{
		Identity: ChainIdentity{ChainID: jf.ChainID, InitialHeight: jf.InitialHeight, GenesisTime: jf.GenesisTime},
		Height:   jf.Height,
		Time:     jf.Time,
	}
	entries := make([]TestJournalEntry, len(jf.Entries))
	for i, e := range jf.Entries {
		entries[i] = TestJournalEntry{FirstSeen: e.FirstSeen, Tx: e.Tx}
	}
	return h, entries, nil
}

// WriteTestJournal writes a journal file (tests only).
func WriteTestJournal(path string, h TestJournalHeader, entries []TestJournalEntry) error {
	jf := &journalFile{
		Version: journalVersion, ChainID: h.Identity.ChainID, InitialHeight: h.Identity.InitialHeight,
		GenesisTime: h.Identity.GenesisTime, Height: h.Height, Time: h.Time,
	}
	for _, e := range entries {
		jf.Entries = append(jf.Entries, journalEntry{FirstSeen: e.FirstSeen, Tx: e.Tx})
	}
	data, err := encodeJournal(jf, journalMaxBytes)
	if err != nil {
		return err
	}
	return writeJournalFile(path, data)
}

// SetEventBusCallTimeout overrides the Subscribe/Unsubscribe timeout and the
// re-subscribe backoff, and returns a restore func (tests only).
func SetEventBusCallTimeout(timeout, backoff time.Duration) (restore func()) {
	oldT, oldMin, oldMax := eventBusCallTimeout, eventBusResubscribeMinBackoff, eventBusResubscribeMaxBackoff
	eventBusCallTimeout, eventBusResubscribeMinBackoff, eventBusResubscribeMaxBackoff = timeout, backoff, backoff
	return func() {
		eventBusCallTimeout, eventBusResubscribeMinBackoff, eventBusResubscribeMaxBackoff = oldT, oldMin, oldMax
	}
}
