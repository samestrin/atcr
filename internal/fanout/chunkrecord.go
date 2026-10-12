package fanout

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// chunksFile is the per-persona durable chunk record written beside status.json
// at sources/pool/raw/agent/<persona>/chunks.json (Epic 35.16.13 T1).
const chunksFile = "chunks.json"

// chunksSchemaVersion stamps the chunks.json shape so a later reader (chunk-level
// resume) can refuse a record it does not understand instead of misreading it.
const chunksSchemaVersion = 1

// Bin outcomes in chunks.json. pending and deferred are written before dispatch;
// ok / failed / timeout are the engine's own Result.Status values, copied in as
// each bin resolves. deferred marks a bin the maxChunksPerAgent ceiling cut
// (capChunks); it is never dispatched, so it stays deferred and readers must
// treat it as unreviewed.
const (
	chunkOutcomePending  = "pending"
	chunkOutcomeDeferred = "deferred"
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

// chunkLedger owns the chunks.json records for one engine run. Every mutation
// and write happens under mu, so concurrent per-bin completions from the
// parallel lane are serialised and each file is always a whole, consistent
// snapshot (atomicfs.WriteJSON never leaves a torn file).
type chunkLedger struct {
	log     *slog.Logger
	rawBase string
	mu      sync.Mutex
	// records is keyed by the persona's on-disk directory name.
	records map[string]*chunkRecord
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
			l.order = append(l.order, dir)
		}
		bin := len(rec.Bins)
		rec.Bins = append(rec.Bins, chunkRecordBin{
			Index:   bin,
			Files:   append([]string(nil), s.Primary.chunkFiles...),
			Outcome: chunkOutcomePending,
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

// recordedBin reports whether a slot is a baseline bin the ledger records: its
// primary carries the file set its chunk ships and is one of several bins, or a
// chunk-aware resume rebuilt it from a prior record (Slot.resumedBin), where it
// may be the persona's only dispatched bin.
func recordedBin(s Slot) bool {
	return len(s.Primary.chunkFiles) > 0 && (s.Primary.ChunkTotal > 1 || s.resumedBin)
}

// writePlan writes every persona's full planned bin list before dispatch.
func (l *chunkLedger) writePlan() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, dir := range l.order {
		l.writeLocked(dir)
	}
}

// slotDone is the engine's per-slot completion hook: it stamps the resolved
// bin's outcome and rewrites that persona's chunks.json before returning.
func (l *chunkLedger) slotDone(i int, r Result) {
	ref, ok := l.bySlot[i]
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	bin := &l.records[ref.dir].Bins[ref.bin]
	bin.Outcome = r.Status
	bin.ContributedNothing = r.Status == StatusOK && contributedNothing(r)
	if r.servedRePacked {
		bin.RePacked = true
		bin.ServedFiles = append([]string(nil), r.servedChunkFiles...)
	}
	l.writeLocked(ref.dir)
}

// writeLocked writes one persona's record. Best-effort, like the transcript: a
// write failure is logged and the review continues, because losing the record
// must never cost the review itself. Caller holds mu.
func (l *chunkLedger) writeLocked(dir string) {
	path := filepath.Join(l.rawBase, dir, chunksFile)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err == nil {
		err = atomicfs.WriteJSON(path, l.records[dir])
	}
	if err != nil {
		l.log.Warn("chunks.json write failed", "agent", l.records[dir].Agent, "path", path, "err", err)
	}
}

func (l *chunkLedger) writeErr() error { return nil }
