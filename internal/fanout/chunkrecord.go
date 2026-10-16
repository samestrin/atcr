package fanout

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/payload"
)

// chunksFile is the per-persona durable chunk record written beside status.json
// at sources/pool/raw/agent/<persona>/chunks.json (Epic 35.16.13 T1).
const chunksFile = "chunks.json"

// chunksSchemaVersion stamps the chunks.json shape so a later reader (chunk-level
// resume) can refuse a record it does not understand instead of misreading it.
const chunksSchemaVersion = 1

// Bin outcomes in chunks.json. pending and deferred are written before dispatch;
// ok / failed / timeout are the engine's own Result.Status values, copied in as
// each bin resolves. deferred marks a bin the old 64-bin baseline ceiling cut
// (Epic 35.16.13 T2); it is never dispatched, so it stays deferred and readers
// must treat it as unreviewed. A new run no longer writes it (Epic 35.16.14 T5
// refuses an over-budget plan instead), but a ledger written before then still
// carries it and must resume.
//
// leased is the work ledger's in-flight state (Epic 35.16.14 T1): a worker holds
// the bin. It lives only in memory and is never written, so a run killed
// mid-drain leaves a leased bin on disk as its last persisted outcome (pending,
// or the previous attempt's), which every reader already treats as open.
const (
	chunkOutcomePending  = "pending"
	chunkOutcomeDeferred = "deferred"
	chunkOutcomeLeased   = "leased"
)

// chunkRecord is the on-disk shape of one persona's chunks.json: the full
// planned bin list, written before any bin is dispatched and rewritten as each
// bin resolves, so a run killed mid-fan-out still names every bin it planned and
// which of them never finished. It follows cli/benchmark_checkpoint.go's rule:
// the unit's record is on disk before the run advances past it.
type chunkRecord struct {
	SchemaVersion int              `json:"schema_version"`
	Agent         string           `json:"agent"`
	Bins          []chunkRecordBin `json:"bins"`
}

// chunkRecordBin is one planned bin: its index in the persona's chunk order, the
// repo-relative files it carries (the primary's planned set), and its outcome.
//
// The two optional fields are stamped as the bin resolves (Epic 35.16.13 T5) so
// the coverage reader can apply the same "reviewed" predicate the file-hash
// index uses without the raw Result in hand. Both are omitempty, so a bin that
// resolved plainly is written exactly as before.
type chunkRecordBin struct {
	Index   int      `json:"index"`
	Files   []string `json:"files"`
	Outcome string   `json:"outcome"`
	// ContributedNothing marks an ok bin whose lane refused its content
	// (contributedNothing): it did not read the files, so it covers none of them.
	ContributedNothing bool `json:"contributed_nothing,omitempty"`
	// RePacked marks a bin served by a fallback that re-packed the payload to its
	// own budget; ServedFiles is then the subset it actually reviewed
	// (servedCoverage), possibly empty. Without RePacked the serving agent
	// reviewed all of Files.
	RePacked    bool     `json:"re_packed,omitempty"`
	ServedFiles []string `json:"served_files,omitempty"`
	// Attempts counts the times a worker leased the bin (Epic 35.16.14 T1). A
	// lease is counted when its attempt resolves and persisted with that outcome,
	// so an attempt in flight when the run is killed is not on disk.
	// omitempty: a bin never leased (pending, deferred) is written exactly as
	// before, and a record written before this field reads as zero attempts.
	Attempts int `json:"attempts,omitempty"`
	// Hashes maps each file the bin shipped to the sha256 of the content it
	// shipped (cache.HashText, the file-hash index's own hash), stamped when the
	// bin is planned. A resume records a prior bin's file in the index only when
	// this hash still matches the file (narrowReviewed), so an edit made after
	// the bin was reviewed is never recorded as reviewed. A deferred bin ships
	// nothing and has none; a record written before this field has none either,
	// and its files are then left out of a resume's write-back (fail-open).
	Hashes map[string]string `json:"hashes,omitempty"`
	// OverWindow marks a bin holding one file larger than its reviewer's context
	// window, stamped when the bin is planned (Epic 35.16.14 fix AC1b): the
	// outer retry never requeues it, and a coverage reader names it as the
	// reason the bin went unreviewed. omitempty, so every other bin is written
	// exactly as before.
	OverWindow bool `json:"over_window,omitempty"`
}

// reviewed is the file-hash index's bin predicate: the bin finished ok and its
// lane did not refuse its content. It is the one "reviewed" rule every reader
// of chunks.json applies — coverage (ReadCoverage), the gap over a resume's
// carried bins (carriedGap) and the resume's open set (open) — so a bin the
// coverage line counts as unreviewed is always one a resume re-dispatches.
func (b chunkRecordBin) reviewed() bool {
	return b.Outcome == StatusOK && !b.ContributedNothing
}

// coveredFiles returns the files the bin reviewed: none unless it is reviewed,
// only the subset its server kept when a fallback re-packed it, and all of
// Files otherwise.
func (b chunkRecordBin) coveredFiles() []string {
	switch {
	case !b.reviewed():
		return nil
	case b.RePacked:
		return b.ServedFiles
	default:
		return b.Files
	}
}

// open reports whether a resume must re-dispatch the bin: it is not reviewed,
// or a re-packed server left one of its files uncovered. An ok bin that
// contributed nothing is open, not done.
func (b chunkRecordBin) open() bool {
	if !b.reviewed() {
		return true
	}
	covered := map[string]bool{}
	for _, f := range b.coveredFiles() {
		covered[f] = true
	}
	for _, f := range b.Files {
		if !covered[f] {
			return true
		}
	}
	return false
}

// chunkLedger owns the chunks.json records for one engine run: the work ledger
// a run drains (Epic 35.16.14). Every mutation happens under mu, so concurrent
// per-bin leases and completions are serialised, and each file is written from a
// snapshot taken under mu, so it is always whole and consistent
// (atomicfs.WriteFileAtomic never leaves a torn file).
//
// Writes are coalesced per persona (flush): a completion returns only once a
// write holding its outcome is on disk, but completions that land while a write
// is in flight share the next one instead of each rewriting the whole file.
type chunkLedger struct {
	log     *slog.Logger
	rawBase string
	mu      sync.Mutex
	// records is keyed by the persona's on-disk directory name.
	records map[string]*chunkRecord
	// leased holds the slots a worker currently holds. In memory only.
	leased map[int]bool
	// gen counts each persona record's mutations; written is the gen its last
	// write captured. Both under mu.
	gen     map[string]uint64
	written map[string]uint64
	// writeMu serialises one persona's file writes, outside mu.
	writeMu map[string]*sync.Mutex
	// writes counts the file writes flush made, under mu.
	writes int
	// bySlot maps a slot index to its persona dir and bin index; a slot with no
	// entry (bulk, chunked-diff) is not recorded.
	bySlot map[int]chunkSlotRef
	// order keeps the personas in first-seen slot order so writes are
	// deterministic.
	order []string
	// carried holds, per persona dir, the files of the prior run's reviewed bins
	// a resume replays instead of re-dispatching (carryReviewed, chunkmerge.go).
	carried map[string]map[string]bool
	// carriedGaps holds, per persona dir, the files of those carried bins that
	// were never reviewed (carriedGap), which the resumed slots' own gap cannot
	// see because the resume dispatches none of them.
	carriedGaps map[string]fileGap
	// failed is the first write failure (writeErr). A failed write may leave no
	// record at all, which a coverage reader would take for "binned nothing", so
	// it is kept for the review result instead of only being logged.
	failed error
}

type chunkSlotRef struct {
	dir string
	bin int
}

// newChunkLedger plans a chunks.json record for every persona the baseline
// (--all / --dir) multi-chunk fan-out split into more than one bin. A slot is a
// baseline bin when its primary carries both a chunk total above one and the
// file set its chunk ships (chunkFiles); the chunked-diff path has no file set
// and a bulk slot has a chunk total of at most one, so neither is recorded and a
// run with no baseline bins returns nil (no file is ever written).
func newChunkLedger(logger *slog.Logger, poolDir string, slots []Slot) *chunkLedger {
	l := &chunkLedger{
		log:     logger,
		rawBase: filepath.Join(poolDir, poolRawAgentDir),
		records: map[string]*chunkRecord{},
		bySlot:  map[int]chunkSlotRef{},
		leased:  map[int]bool{},
		gen:     map[string]uint64{},
		written: map[string]uint64{},
		writeMu: map[string]*sync.Mutex{},
	}
	for i, s := range slots {
		if !recordedBin(s) {
			continue
		}
		dir, err := agentDirName(s.Primary.Name)
		if err != nil {
			// writePool rejects the same name later; nothing to record here.
			continue
		}
		rec, ok := l.records[dir]
		if !ok {
			rec = &chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: s.Primary.Name}
			l.records[dir] = rec
			l.writeMu[dir] = &sync.Mutex{}
			l.order = append(l.order, dir)
		}
		bin := len(rec.Bins)
		rec.Bins = append(rec.Bins, chunkRecordBin{
			Index:   bin,
			Files:   append([]string(nil), s.Primary.chunkFiles...),
			Outcome: chunkOutcomePending,
			Hashes:  entryHashes(s.entries),
		})
		l.bySlot[i] = chunkSlotRef{dir: dir, bin: bin}
		// Bins the ceiling deferred follow the persona's dispatched bins. No slot
		// maps to them, so no completion ever moves them off deferred.
		for _, files := range s.deferredBins {
			rec.Bins = append(rec.Bins, chunkRecordBin{
				Index:   len(rec.Bins),
				Files:   append([]string(nil), files...),
				Outcome: chunkOutcomeDeferred,
			})
		}
	}
	if len(l.records) == 0 {
		return nil
	}
	return l
}

// entryHashes maps each shipped entry to the hash the file-hash index records
// for it (captureBaselineWriteback hashes the same way). nil for no entries.
func entryHashes(entries []payload.FileEntry) map[string]string {
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.Path] = cache.HashText(e.Body)
	}
	return out
}

// recordedBin reports whether a slot is a baseline bin the ledger records: its
// primary carries the file set its chunk ships and is one of several bins, or a
// chunk-aware resume rebuilt it from a prior record (Slot.resumedBin), where it
// may be the persona's only dispatched bin.
func recordedBin(s Slot) bool {
	return len(s.Primary.chunkFiles) > 0 && (s.Primary.ChunkTotal > 1 || s.resumedBin)
}

// writePlan writes every persona's full planned bin list before dispatch.
func (l *chunkLedger) writePlan() {
	for _, dir := range l.order {
		l.flush(dir, l.touch(dir))
	}
}

// touch records a mutation of dir's record and returns the generation a write
// must capture to hold it.
func (l *chunkLedger) touch(dir string) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gen[dir]++
	return l.gen[dir]
}

// slotStart is the engine's per-slot lease hook: a worker has taken the bin.
// It marks the bin leased, in memory only; nothing is written until the attempt
// resolves (slotDone), which counts it. The count waits for the outcome because
// another bin's completion rewrites the whole persona record meanwhile, and an
// attempt in flight must not reach disk.
func (l *chunkLedger) slotStart(i int) {
	if _, ok := l.bySlot[i]; !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leased[i] = true
}

// slotDone is the engine's per-slot completion hook: it stamps the resolved
// bin's outcome, releases its lease, and returns only once that persona's
// chunks.json holds the outcome and its attempts count. A bin can complete more
// than once (a re-queued unit), so every field of the outcome is restamped and
// none is left over from the earlier completion.
func (l *chunkLedger) slotDone(i int, r Result) {
	ref, ok := l.bySlot[i]
	if !ok {
		return
	}
	l.mu.Lock()
	bin := &l.records[ref.dir].Bins[ref.bin]
	if l.leased[i] {
		bin.Attempts++
		delete(l.leased, i)
	}
	bin.Outcome = r.Status
	bin.ContributedNothing = r.Status == StatusOK && contributedNothing(r)
	bin.RePacked = r.servedRePacked
	bin.ServedFiles = nil
	if r.servedRePacked {
		bin.ServedFiles = append([]string(nil), r.servedChunkFiles...)
	}
	l.gen[ref.dir]++
	want := l.gen[ref.dir]
	l.mu.Unlock()
	l.flush(ref.dir, want)
}

// recorded reports whether slot i is a bin this ledger records: the units the
// outer per-unit retry covers (Epic 35.16.14 T3).
func (l *chunkLedger) recorded(i int) bool {
	_, ok := l.bySlot[i]
	return ok
}

// retryable reports whether the outer per-unit retry covers slot i: a recorded
// bin that is not over its reviewer's window. A bin that can never fit is tried
// once (Epic 35.16.14 fix AC1b); requeueing it would only spend the attempt
// budget on a call the provider must refuse.
func (l *chunkLedger) retryable(i int) bool {
	return l.recorded(i)
}

// slotRequeue is the engine's requeue hook: the bin's attempt failed retryably
// and it goes back to the queue. It counts the attempt, releases the lease and
// returns the bin to pending, clearing the failed attempt's outcome fields, and
// returns only once chunks.json holds that, so a run killed during the backoff
// leaves the bin open with its attempts counted.
func (l *chunkLedger) slotRequeue(i int, _ Result) {
	ref, ok := l.bySlot[i]
	if !ok {
		return
	}
	l.mu.Lock()
	bin := &l.records[ref.dir].Bins[ref.bin]
	if l.leased[i] {
		bin.Attempts++
		delete(l.leased, i)
	}
	bin.Outcome = chunkOutcomePending
	bin.ContributedNothing = false
	bin.RePacked = false
	bin.ServedFiles = nil
	l.gen[ref.dir]++
	want := l.gen[ref.dir]
	l.mu.Unlock()
	l.flush(ref.dir, want)
}

// states counts every planned bin by its current state, leased included, so the
// counts always sum to the planned bin total. A leased bin counts as leased in
// place of its recorded outcome.
func (l *chunkLedger) states() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for _, dir := range l.order {
		for _, bin := range l.records[dir].Bins {
			out[bin.Outcome]++
		}
	}
	for i := range l.leased {
		ref := l.bySlot[i]
		out[l.records[ref.dir].Bins[ref.bin].Outcome]--
		out[chunkOutcomeLeased]++
	}
	for k, n := range out {
		if n == 0 {
			delete(out, k)
		}
	}
	return out
}

// flush makes sure dir's chunks.json holds at least generation want. Writes for
// one persona run one at a time under writeMu; a caller whose mutation an
// earlier write already captured returns without writing, which is how
// completions that land during a write coalesce into the next one.
func (l *chunkLedger) flush(dir string, want uint64) {
	wm := l.writeMu[dir]
	wm.Lock()
	defer wm.Unlock()
	l.mu.Lock()
	if l.written[dir] >= want {
		l.mu.Unlock()
		return
	}
	snap := l.gen[dir]
	data, err := json.MarshalIndent(l.records[dir], "", "  ")
	agent := l.records[dir].Agent
	l.mu.Unlock()
	if err == nil {
		err = l.write(dir, append(data, '\n'))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// A failed write still advances written: the failure is kept (failed) and
	// the next mutation retries with a fresh snapshot.
	l.written[dir] = snap
	l.writes++
	if err != nil {
		path := filepath.Join(l.rawBase, dir, chunksFile)
		l.log.Warn("chunks.json write failed", "agent", agent, "path", path, "err", err)
		if l.failed == nil {
			l.failed = fmt.Errorf("%s: %w", path, err)
		}
	}
}

// write puts one persona's record on disk. Best-effort, like the transcript: a
// write failure is logged and the review continues, because losing the record
// must never cost the review itself. The first failure is kept for writeErr, so
// the coverage gate fails closed rather than reading a missing record as an
// unbinned run.
func (l *chunkLedger) write(dir string, data []byte) error {
	path := filepath.Join(l.rawBase, dir, chunksFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
}

// writeErr returns the ledger's first write failure, or nil (also for a nil
// ledger: a bulk or diff run records nothing, so nothing failed).
func (l *chunkLedger) writeErr() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failed
}
