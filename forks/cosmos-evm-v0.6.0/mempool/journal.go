package mempool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// STOChain fix (audit M3): keep the txs held by this node's legacypool across
// a graceful restart. Off by default (app.toml evm.mempool.journal).
//
// Txs that live only in this node's legacypool (nonce-gap txs answered OK by
// eth_sendRawTransaction, fee-stuck "reverse orphans") were lost on restart:
// the wallet saw its tx accepted and then eth_getTransactionByHash = null.
//
// On Close the pending and queued txs are written (bounded) to a node-local
// journal file outside the chain data directory. After restart, once the
// mempool has an RPC client (SetClientCtx), the journal is read once, deleted,
// and its txs are re-submitted in sender+nonce order through the re-broadcast
// path (BroadcastTxSync -> CheckTx), asynchronously, so normal CheckTx
// validation applies: txs mined meanwhile or no longer valid are rejected.
// This is node-local only and never affects consensus.
//
// Integrity: the header carries the chain identity (chain-id, initial height,
// genesis time) and the height it was written at; every entry carries the
// time its tx was first journaled or first seen by this node, kept across
// restarts. A journal of another chain, of a wiped chain (height far above the
// current head) or with an unknown height is discarded, and no entry is ever
// replayed more than journalMaxAge after it was first seen.

const (
	// JournalDirName is the journal directory under the node home (mode
	// 0700). It is deliberately outside <home>/data.
	JournalDirName = "evm-mempool"
	// JournalFileName is the journal file name in JournalDirName.
	JournalFileName = "journal.rlp"

	journalVersion = 2
	// journalMaxTxs and journalMaxBytes bound the journal on write and read.
	journalMaxTxs   = 10_000
	journalMaxBytes = 32 << 20
	// journalHeaderSlack is reserved for the RLP envelope and header fields.
	journalHeaderSlack = 1024
	// journalEntryOverhead covers the RLP list/string prefixes and the
	// FirstSeen field of one entry.
	journalEntryOverhead = 24
	// journalMaxAge: no entry is replayed later than this after it was first
	// seen (same bound as pendingHardEvictAfter in legacypool).
	journalMaxAge = 24 * time.Hour
	// journalHeightSlack tolerates a `rollback` of a few blocks. A chain
	// lower than the journal height by more than this was wiped (devnet), and
	// its old signed txs must not be replayed onto the new chain.
	journalHeightSlack = 10
	// journalClockSkew is how far in the future a first-seen time may be
	// (clock adjustments). Later entries would never expire: rejected.
	journalClockSkew = 5 * time.Minute
	// journalReplayBatch is the number of txs per BroadcastTxFn call.
	journalReplayBatch = 100
	// journalCollectTimeout bounds reading the pool content on Close.
	journalCollectTimeout = 10 * time.Second
)

// journalReadyPoll is how often replay checks that the node is ready. Each
// journal copies it at construction (tests override it before that).
var journalReadyPoll = 2 * time.Second

var (
	errJournalOversized = errors.New("EVM mempool journal exceeds its size bound")
	errJournalForeign   = errors.New("EVM mempool journal belongs to another chain")
	// errJournalDirSync: the journal was renamed into place but the
	// directory fsync failed. The write itself succeeded.
	errJournalDirSync = errors.New("EVM mempool journal directory fsync failed")
)

// syncDir fsyncs a directory (variable for tests).
var syncDir = func(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ChainIdentity identifies the chain a journal was written on.
type ChainIdentity struct {
	ChainID       string
	InitialHeight uint64
	GenesisTime   uint64 // unix nanoseconds
}

func (c *ChainIdentity) known() bool { return c != nil && c.ChainID != "" && c.GenesisTime != 0 }

// journalEntry is one journaled tx.
type journalEntry struct {
	FirstSeen uint64 // unix seconds the tx was first seen or journaled
	Tx        *ethtypes.Transaction
}

// journalFile is the on-disk format (RLP).
type journalFile struct {
	Version       uint64
	ChainID       string
	InitialHeight uint64
	GenesisTime   uint64
	Height        uint64 // latest committed height when written
	Time          uint64 // unix seconds when written
	Entries       []journalEntry
}

func (jf *journalFile) txs() []*ethtypes.Transaction {
	out := make([]*ethtypes.Transaction, len(jf.Entries))
	for i, e := range jf.Entries {
		out[i] = e.Tx
	}
	return out
}

// mempoolJournal is the per-mempool journal state.
type mempoolJournal struct {
	path      string
	readyPoll time.Duration
	identity  atomic.Pointer[ChainIdentity]

	replayOnce sync.Once
	// fileMu serialises replay's read+delete with Close's write, so a
	// shutdown right after start neither loses nor deletes a fresh journal.
	fileMu   sync.Mutex
	loaded   atomic.Bool // the file was read (or found absent) by replay
	stop     chan struct{}
	stopOnce sync.Once

	mu sync.Mutex
	// unsent holds journaled entries read but not re-submitted yet; Close
	// writes them back (with the original header) so a shutdown during
	// replay does not lose them.
	unsent []journalEntry
	// loadedHeader is the header (no entries) of the journal read at start.
	loadedHeader journalFile
	// firstSeen keeps the first-seen time of every tx read from the journal
	// (bounded by journalMaxTxs), so a re-submitted tx keeps its age when it
	// is journaled again.
	firstSeen map[ethcommon.Hash]uint64
}

func newMempoolJournal(path string) *mempoolJournal {
	return &mempoolJournal{path: path, readyPoll: journalReadyPoll, stop: make(chan struct{})}
}

func (j *mempoolJournal) halt() { j.stopOnce.Do(func() { close(j.stop) }) }

func (j *mempoolJournal) setUnsent(entries []journalEntry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.unsent = entries
}

func (j *mempoolJournal) takeUnsent() []journalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.unsent
	j.unsent = nil
	return out
}

func (j *mempoolJournal) firstSeenOf(hash ethcommon.Hash) (uint64, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	t, ok := j.firstSeen[hash]
	return t, ok
}

// SetChainIdentity records the identity of the chain this node runs (from the
// genesis document). Journals are stamped with it and journals of another
// chain are discarded. Without it nothing is journaled or replayed.
func (m *ExperimentalEVMMempool) SetChainIdentity(chainID string, initialHeight int64, genesisTime time.Time) {
	if m.journal == nil {
		return
	}
	if initialHeight < 0 {
		initialHeight = 0
	}
	m.journal.identity.Store(&ChainIdentity{
		ChainID:       chainID,
		InitialHeight: uint64(initialHeight),
		GenesisTime:   uint64(genesisTime.UnixNano()), // #nosec G115 -- genesis is after 1970
	})
}

// checkJournal returns an error if jf must not be replayed on the chain
// identified by id with head height head.
func checkJournal(jf *journalFile, id *ChainIdentity, head uint64) error {
	if !id.known() {
		return errors.New("chain identity unknown")
	}
	if jf.ChainID != id.ChainID || jf.InitialHeight != id.InitialHeight || jf.GenesisTime != id.GenesisTime {
		return fmt.Errorf("%w (journal chain-id %q, node chain-id %q)", errJournalForeign, jf.ChainID, id.ChainID)
	}
	if jf.Height == 0 {
		return errors.New("journal height unknown")
	}
	if head+journalHeightSlack < jf.Height {
		return fmt.Errorf("journal written at height %d, head is %d (chain reset?)", jf.Height, head)
	}
	return nil
}

// freshEntries drops entries first seen more than journalMaxAge before now,
// and entries "first seen" more than journalClockSkew in the future (they
// would never expire).
func freshEntries(entries []journalEntry, now time.Time) []journalEntry {
	out := make([]journalEntry, 0, len(entries))
	for _, e := range entries {
		if e.FirstSeen == 0 || e.Tx == nil || e.FirstSeen > uint64(math.MaxInt64) {
			continue
		}
		seen := time.Unix(int64(e.FirstSeen), 0)
		if now.Sub(seen) > journalMaxAge || seen.After(now.Add(journalClockSkew)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// selectJournalTxs flattens the pool content into at most maxTxs txs whose
// encoded size stays within maxBytes: groups in order (pending, queued,
// carried), senders in address order, each sender's txs in nonce order. It
// stops at the first tx that does not fit, so every sender keeps a contiguous
// nonce prefix.
func selectJournalTxs(groups []map[ethcommon.Address][]*ethtypes.Transaction, maxTxs int, maxBytes uint64) []*ethtypes.Transaction {
	var (
		out  []*ethtypes.Transaction
		size uint64
		seen = make(map[ethcommon.Hash]struct{})
	)
	for _, group := range groups {
		senders := make([]ethcommon.Address, 0, len(group))
		for addr := range group {
			senders = append(senders, addr)
		}
		sort.Slice(senders, func(i, k int) bool { return senders[i].Cmp(senders[k]) < 0 })
		for _, addr := range senders {
			txs := append([]*ethtypes.Transaction(nil), group[addr]...)
			sort.SliceStable(txs, func(i, k int) bool { return txs[i].Nonce() < txs[k].Nonce() })
			for _, tx := range txs {
				if _, dup := seen[tx.Hash()]; dup {
					continue
				}
				txSize := tx.Size() + journalEntryOverhead
				if len(out) >= maxTxs || size+txSize > maxBytes {
					return out
				}
				seen[tx.Hash()] = struct{}{}
				out = append(out, tx)
				size += txSize
			}
		}
	}
	return out
}

// encodeJournal encodes jf and enforces the bounds.
func encodeJournal(jf *journalFile, maxBytes int) ([]byte, error) {
	if len(jf.Entries) > journalMaxTxs {
		return nil, errJournalOversized
	}
	data, err := rlp.EncodeToBytes(jf)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errJournalOversized
	}
	return data, nil
}

func journalTmpPattern(path string) string { return filepath.Base(path) + ".tmp-*" }

// writeJournalFile writes data atomically: the directory is created with
// mode 0700, a temp file (0600) in it is written and fsynced, renamed over
// path, and the directory is fsynced so the rename is durable.
func writeJournalFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, journalTmpPattern(path))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	if syncErr := syncDir(dir); syncErr != nil {
		// The file is in place; only the rename's durability is unsure.
		return fmt.Errorf("%w: %v", errJournalDirSync, syncErr)
	}
	return nil
}

// removeJournalTmpFiles deletes temp files left by an interrupted write.
func removeJournalTmpFiles(path string) (removed int, err error) {
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), journalTmpPattern(path)))
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, name := range matches {
		if rmErr := os.Remove(name); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			errs = append(errs, rmErr)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// readJournalFile reads and decodes the journal, refusing files larger than
// maxBytes. A missing file returns an error matching os.ErrNotExist.
func readJournalFile(path string, maxBytes int64) (*journalFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes > %d", errJournalOversized, info.Size(), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: file grew while reading", errJournalOversized)
	}
	return decodeJournal(data, journalMaxTxs)
}

// decodeJournal decodes the journalFile RLP (fields in struct order) and
// stops as soon as more than maxEntries entries are seen, instead of decoding
// the whole list first.
func decodeJournal(data []byte, maxEntries int) (*journalFile, error) {
	wrap := func(err error) error { return fmt.Errorf("decode EVM mempool journal: %w", err) }
	s := rlp.NewStream(bytes.NewReader(data), uint64(len(data)))
	if _, err := s.List(); err != nil {
		return nil, wrap(err)
	}
	var jf journalFile
	var err error
	if jf.Version, err = s.Uint64(); err != nil {
		return nil, wrap(err)
	}
	if jf.Version != journalVersion {
		return nil, fmt.Errorf("unsupported EVM mempool journal version %d", jf.Version)
	}
	if err := s.Decode(&jf.ChainID); err != nil {
		return nil, wrap(err)
	}
	for _, field := range []*uint64{&jf.InitialHeight, &jf.GenesisTime, &jf.Height, &jf.Time} {
		if *field, err = s.Uint64(); err != nil {
			return nil, wrap(err)
		}
	}
	if _, err := s.List(); err != nil {
		return nil, wrap(err)
	}
	for {
		var e journalEntry
		err := s.Decode(&e)
		if errors.Is(err, rlp.EOL) {
			break
		}
		if err != nil {
			return nil, wrap(err)
		}
		if len(jf.Entries) >= maxEntries {
			return nil, fmt.Errorf("%w: more than %d txs", errJournalOversized, maxEntries)
		}
		jf.Entries = append(jf.Entries, e)
	}
	if err := s.ListEnd(); err != nil { // entries
		return nil, wrap(err)
	}
	if err := s.ListEnd(); err != nil { // journalFile
		return nil, wrap(err)
	}
	if _, _, err := s.Kind(); !errors.Is(err, io.EOF) {
		return nil, wrap(errors.New("trailing data after the journal"))
	}
	return &jf, nil
}

// latestHeight returns the latest committed height seen by the mempool, or 0.
func (m *ExperimentalEVMMempool) latestHeight() uint64 {
	ctx, err := m.blockchain.GetLatestContext()
	if err != nil || ctx.BlockHeight() < 0 {
		return 0
	}
	return uint64(ctx.BlockHeight())
}

// writeJournal is called by Close. It writes the legacypool content plus the
// journaled entries not re-submitted yet. Rules:
//   - no chain identity or unknown latest height: write nothing;
//   - nothing new in the pool: an unread journal is left untouched, and
//     entries loaded but not re-submitted are written back with their
//     original header (Height, Time);
//   - otherwise the header is stamped now; carried-over entries are kept only
//     if their journal is valid for this chain and height, and every entry
//     keeps its original first-seen time.
func (m *ExperimentalEVMMempool) writeJournal() error {
	j := m.journal
	if j == nil {
		return nil
	}
	j.halt()
	j.fileMu.Lock()
	defer j.fileMu.Unlock()

	id := j.identity.Load()
	if !id.known() {
		m.logger.Debug("EVM mempool journal: chain identity unknown, not writing")
		return nil
	}

	type content struct {
		pending, queued map[ethcommon.Address][]*ethtypes.Transaction
	}
	ch := make(chan content, 1)
	go func() {
		p, q := m.legacyTxPool.Content()
		ch <- content{p, q}
	}()
	var c content
	select {
	case c = <-ch:
	case <-time.After(journalCollectTimeout):
		return fmt.Errorf("timed out after %s reading the legacypool content", journalCollectTimeout)
	}

	now := time.Now()
	poolTxs := selectJournalTxs([]map[ethcommon.Address][]*ethtypes.Transaction{c.pending, c.queued}, journalMaxTxs, journalMaxBytes-journalHeaderSlack)
	unsent := freshEntries(j.takeUnsent(), now)

	if len(poolTxs) == 0 {
		if !j.loaded.Load() || len(unsent) == 0 {
			// Nothing new: leave any unread journal exactly as written.
			return nil
		}
		// Write back exactly the header that was read (identity, height,
		// time): the next start validates it like any journal.
		j.mu.Lock()
		hdr := j.loadedHeader
		j.mu.Unlock()
		hdr.Entries = unsent
		return m.persistJournal(&hdr)
	}

	height := m.latestHeight()
	if height == 0 {
		m.logger.Error("EVM mempool journal: latest height unknown, not writing", "tx_count", len(poolTxs))
		return nil
	}

	// Carry over journaled entries that were never re-submitted (the loaded
	// remainder, or the unread file when Close runs before replay), but only
	// if their journal is valid for this chain and height: the new header is
	// stamped with the current identity and height.
	var carried []journalEntry
	if len(unsent) > 0 {
		j.mu.Lock()
		hdr := j.loadedHeader
		j.mu.Unlock()
		if err := checkJournal(&hdr, id, height); err == nil {
			carried = unsent
		} else {
			m.logger.Info("EVM mempool journal: dropping journaled txs not re-submitted", "reason", err)
		}
	}
	if !j.loaded.Load() {
		if jf, err := readJournalFile(j.path, journalMaxBytes); err == nil {
			if err := checkJournal(jf, id, height); err == nil {
				carried = append(carried, freshEntries(jf.Entries, now)...)
			} else {
				m.logger.Info("EVM mempool journal: dropping unread journal", "reason", err)
			}
		}
	}
	seen := make(map[ethcommon.Hash]uint64, len(carried))
	carriedTxs := make([]*ethtypes.Transaction, 0, len(carried))
	for _, e := range carried {
		if old, ok := seen[e.Tx.Hash()]; !ok || e.FirstSeen < old {
			seen[e.Tx.Hash()] = e.FirstSeen
		}
		carriedTxs = append(carriedTxs, e.Tx)
	}
	groups := []map[ethcommon.Address][]*ethtypes.Transaction{c.pending, c.queued}
	if len(carriedTxs) > 0 {
		groups = append(groups, groupBySender(carriedTxs))
	}
	txs := selectJournalTxs(groups, journalMaxTxs, journalMaxBytes-journalHeaderSlack)
	entries := make([]journalEntry, 0, len(txs))
	for _, tx := range txs {
		fs, ok := seen[tx.Hash()]
		if !ok {
			fs, ok = j.firstSeenOf(tx.Hash())
		}
		if !ok {
			fs = uint64(tx.Time().Unix()) // #nosec G115 -- unix time is positive
		}
		entries = append(entries, journalEntry{FirstSeen: fs, Tx: tx})
	}
	entries = freshEntries(entries, now)
	if len(entries) == 0 {
		return nil
	}
	return m.persistJournal(&journalFile{
		Version: journalVersion, ChainID: id.ChainID, InitialHeight: id.InitialHeight, GenesisTime: id.GenesisTime,
		Height: height, Time: uint64(now.Unix()), // #nosec G115 -- unix time is positive
		Entries: entries,
	})
}

func (m *ExperimentalEVMMempool) persistJournal(jf *journalFile) error {
	data, err := encodeJournal(jf, journalMaxBytes)
	if err != nil {
		return err
	}
	if err := writeJournalFile(m.journal.path, data); err != nil {
		if !errors.Is(err, errJournalDirSync) {
			return err
		}
		m.logger.Warn("EVM mempool journal written, but its directory fsync failed", "error", err)
	}
	m.logger.Info("wrote EVM mempool journal", "tx_count", len(jf.Entries), "bytes", len(data),
		"height", jf.Height, "path", m.journal.path)
	return nil
}

func groupBySender(txs []*ethtypes.Transaction) map[ethcommon.Address][]*ethtypes.Transaction {
	out := make(map[ethcommon.Address][]*ethtypes.Transaction)
	for _, tx := range txs {
		from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			continue
		}
		out[from] = append(out[from], tx)
	}
	return out
}

// startJournalReplay starts the one-time replay in the background.
func (m *ExperimentalEVMMempool) startJournalReplay() {
	j := m.journal
	if j == nil {
		return
	}
	j.replayOnce.Do(func() { go m.replayJournal() })
}

// replayJournal reads and deletes the journal, waits until the node can
// validate txs, then re-submits them. It never blocks startup and stops
// between batches when the mempool closes.
func (m *ExperimentalEVMMempool) replayJournal() {
	j := m.journal
	jf, ok := m.loadJournal()
	if !ok || len(jf.Entries) == 0 {
		return
	}

	// Wait until the legacypool has chain state and CometBFT has caught up.
	for !m.journalReplayReady() {
		if len(freshEntries(jf.Entries, time.Now())) == 0 {
			j.takeUnsent()
			m.logger.Info("discarding EVM mempool journal: every entry is older than the max age",
				"tx_count", len(jf.Entries), "max_age", journalMaxAge)
			return
		}
		select {
		case <-j.stop:
			return // Close writes unsent back to the journal.
		case <-time.After(j.readyPoll):
		}
	}
	if err := checkJournal(jf, j.identity.Load(), m.legacyTxPool.HeadNumber()); err != nil {
		j.takeUnsent()
		m.logger.Info("discarding EVM mempool journal", "tx_count", len(jf.Entries), "reason", err)
		return
	}
	entries := freshEntries(jf.Entries, time.Now())
	if dropped := len(jf.Entries) - len(entries); dropped > 0 {
		m.logger.Info("EVM mempool journal: dropping entries older than the max age",
			"dropped", dropped, "max_age", journalMaxAge)
	}
	sort.SliceStable(entries, func(a, b int) bool {
		return senderNonceLess(entries[a].Tx, entries[b].Tx)
	})
	j.setUnsent(entries)

	var rejected int
	for start := 0; start < len(entries); start += journalReplayBatch {
		if isStopped(j.stop) {
			return
		}
		end := min(start+journalReplayBatch, len(entries))
		batch := make([]*ethtypes.Transaction, 0, end-start)
		for _, e := range entries[start:end] {
			batch = append(batch, e.Tx)
		}
		if err := m.journalBroadcast(batch); err != nil {
			rejected += countJoined(err)
			m.logger.Debug("EVM mempool journal txs rejected on re-submit", "error", err)
		}
		j.setUnsent(entries[end:])
	}
	j.takeUnsent()
	m.logger.Info("re-submitted EVM txs from mempool journal",
		"tx_count", len(entries), "rejected", rejected, "journal_height", jf.Height)
}

func senderNonceLess(a, b *ethtypes.Transaction) bool {
	fa, _ := ethtypes.Sender(ethtypes.LatestSignerForChainID(a.ChainId()), a)
	fb, _ := ethtypes.Sender(ethtypes.LatestSignerForChainID(b.ChainId()), b)
	if fa != fb {
		return fa.Cmp(fb) < 0
	}
	return a.Nonce() < b.Nonce()
}

// loadJournal deletes leftover temp files, reads the journal once and deletes
// it. Corrupt or oversized files are logged and deleted. ok is false when
// there is nothing to replay.
func (m *ExperimentalEVMMempool) loadJournal() (jf *journalFile, ok bool) {
	j := m.journal
	j.fileMu.Lock()
	defer j.fileMu.Unlock()
	if isStopped(j.stop) {
		return nil, false
	}
	if n, err := removeJournalTmpFiles(j.path); err != nil {
		m.logger.Error("failed to delete leftover EVM mempool journal temp files", "error", err)
	} else if n > 0 {
		m.logger.Info("deleted leftover EVM mempool journal temp files", "count", n)
	}
	jf, err := readJournalFile(j.path, journalMaxBytes)
	j.loaded.Store(true)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false
		}
		m.logger.Error("discarding unreadable EVM mempool journal", "path", j.path, "error", err)
		if rmErr := os.Remove(j.path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			m.logger.Error("failed to delete EVM mempool journal", "path", j.path, "error", rmErr)
		}
		return nil, false
	}
	if rmErr := os.Remove(j.path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		// Never replay a journal that would be replayed again next start.
		m.logger.Error("failed to delete EVM mempool journal, not replaying it", "path", j.path, "error", rmErr)
		return nil, false
	}
	// Published under fileMu: a Close that runs next writes these back with
	// the original header.
	j.mu.Lock()
	j.unsent = jf.Entries
	j.loadedHeader = *jf
	j.loadedHeader.Entries = nil
	j.firstSeen = make(map[ethcommon.Hash]uint64, len(jf.Entries))
	for _, e := range jf.Entries {
		if e.Tx != nil {
			j.firstSeen[e.Tx.Hash()] = e.FirstSeen
		}
	}
	j.mu.Unlock()
	return jf, true
}

// journalReplayReady reports whether re-submitted txs can be validated.
func (m *ExperimentalEVMMempool) journalReplayReady() bool {
	if !m.legacyTxPool.HasState() {
		return false
	}
	cctx := m.getClientCtx()
	if cctx.Client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventBusCallTimeout)
	defer cancel()
	status, err := cctx.Client.Status(ctx)
	if err != nil || status == nil {
		return false
	}
	return !status.SyncInfo.CatchingUp
}

// journalBroadcast re-submits txs through BroadcastTxFn when the config
// supplied one, otherwise through broadcastEVMTransactions (the default
// BroadcastTxFn path, without its per-call Error log: stale journal txs are
// expected to be rejected).
func (m *ExperimentalEVMMempool) journalBroadcast(txs []*ethtypes.Transaction) error {
	if m.customBroadcast {
		return m.legacyTxPool.BroadcastTxFn(txs)
	}
	return broadcastEVMTransactions(m.getClientCtx(), m.txConfig, txs)
}

func countJoined(err error) int {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		return len(joined.Unwrap())
	}
	return 1
}
