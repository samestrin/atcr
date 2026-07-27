package payload

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// filehash.go hosts the persisted per-file "reviewed-state" index that lets a
// repeat baseline scan (`atcr review --all` / `--dir`) skip files whose content is
// byte-for-byte unchanged since the last completed run (Sprint 35.0, Story 4).
//
// This is DELIBERATELY NOT internal/cache.Store. cache.Store (Epic 5.2) is a
// payload/prompt-level LLM-response cache keyed by the full rendered prompt digest;
// it answers "have I already paid for this exact agent call?". FileHashIndex answers
// a different question — "has this file's content changed since I last reviewed it?"
// — at per-file granularity, before any chunk or prompt exists. The only thing the
// two share is the digest FORMAT: both use cache.HashText's canonical
// "sha256:<hex>" string. FileHashIndex must never be built on, wrap, or extend
// cache.Store (Story 4's stated top risk).
//
// On-disk schema (.atcr/index/file-hashes.json) is a JSON object keyed by
// repo-root-relative, slash-normalized path, each value carrying the file's content
// hash and the id of the last completed run that reviewed it:
//
//	{
//	  "internal/payload/fullrepo.go": {
//	    "hash": "sha256:ab12…",
//	    "last_reviewed_run_id": "run-abc123"
//	  }
//	}
//
// The index is written LAST, only after a run reaches a completed state, via
// atomicfs.WriteJSON (temp-then-rename) so an interrupted run never leaves a partial
// index and a concurrent run's last writer wins with a fully-formed file.

// fileHashEntry is one path's recorded review state: the content digest that was
// reviewed and the id of the run that reviewed it.
type fileHashEntry struct {
	Hash              string `json:"hash"`
	LastReviewedRunID string `json:"last_reviewed_run_id"`
}

// FileHashIndex is a path→(content-hash, last-reviewed-run-id) map persisted under
// .atcr/index/file-hashes.json. It is consulted before chunking to drop unchanged
// files (Unchanged) and rewritten after a completed run (Save). All methods are
// nil-receiver-safe so a nil index (e.g. when --fresh bypasses loading, AC 04-04)
// behaves as "no entry for every path" on reads and a no-op on mutations, matching
// the ignoreMatcher.match / FileIndex.Has nil-safety convention.
//
// Concurrency contract: the backing map is NOT internally synchronized. Reads
// (Unchanged/Get/Paths) happen during single-threaded candidate enumeration BEFORE
// fan-out, and mutations (Record/Trim) happen on a single goroutine AFTER a run has
// completed, when writing the index back — never from the per-agent review
// goroutines. Callers must not mutate a FileHashIndex from more than one goroutine
// (doing so would trip Go's fatal "concurrent map writes"); no lock is taken because
// the enumerate-then-review-then-record lifecycle never accesses it concurrently.
type FileHashIndex struct {
	entries map[string]fileHashEntry
}

// FileHashIndexPath returns the canonical on-disk location of the file-hash index
// for the repository rooted at root: .atcr/index/file-hashes.json.
func FileHashIndexPath(root string) string {
	return filepath.Join(root, ".atcr", "index", "file-hashes.json")
}

// newFileHashIndex returns an empty, ready-to-use index.
func newFileHashIndex() *FileHashIndex {
	return &FileHashIndex{entries: make(map[string]fileHashEntry)}
}

// Load reads the index at path. It NEVER returns an error and NEVER returns nil: a
// missing, empty, unreadable, or corrupt/wrong-shape file degrades to an empty index
// (a full scan), mirroring loadGitignore/loadAtcrignore's graceful-degradation
// convention (ignore.go). The logger is used to record that degradation (Debug for
// the routine missing/empty cases, Warn for corruption) — see AC 04-03; a nil logger
// is tolerated (logging is skipped). The signature takes a logger, unlike the
// sprint-plan's abbreviated `Load(path) *FileHashIndex`, because the corrupt-index
// case must emit a discoverable Warn line and the whole payload package already
// threads *slog.Logger for exactly this purpose.
func Load(path string, logger *slog.Logger) *FileHashIndex {
	idx := newFileHashIndex()

	data, err := os.ReadFile(path)
	if err != nil {
		// Missing is the routine first-run state — Debug at most, never Warn/Error
		// (AC 04-03 Happy Path 1). Any other read error (e.g. permissions) is equally
		// non-fatal: degrade to a full scan.
		if logger != nil && !os.IsNotExist(err) {
			logger.Debug("payload: file-hash index unreadable, running a full scan", "path", path, "err", err)
		}
		return idx
	}
	if len(data) == 0 {
		// Empty (0-byte) file: same graceful fallback as missing, but noted at Debug
		// so it is observably distinct from a real parse failure (AC 04-03 Edge Case 1).
		if logger != nil {
			logger.Debug("payload: file-hash index is empty, running a full scan", "path", path)
		}
		return idx
	}

	var parsed map[string]fileHashEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Corrupt / wrong-shape JSON (e.g. an array instead of the expected object) is
		// an anomaly the user must be able to discover — Warn naming path + error — but
		// still non-fatal: empty index, full scan, rebuilt on the next completed run
		// (AC 04-03 Edge Cases 2-3).
		if logger != nil {
			logger.Warn("payload: file-hash index is corrupt, ignoring it and running a full scan (it will be rebuilt)", "path", path, "err", err)
		}
		return newFileHashIndex()
	}
	if parsed != nil {
		idx.entries = parsed
	}
	return idx
}

// Unchanged reports whether path has a recorded entry whose hash equals the supplied
// content digest — i.e. the file is byte-for-byte identical to what was last
// reviewed, so it can be skipped. Nil-receiver-safe: a nil index reports false for
// every path (treat as unreviewed). The decision is content-based only; mtime/size/
// git-status are never consulted (AC 04-02 Edge Case 4).
func (idx *FileHashIndex) Unchanged(path, hash string) bool {
	if idx == nil || idx.entries == nil {
		return false
	}
	e, ok := idx.entries[path]
	return ok && e.Hash == hash
}

// Record stamps path with its content hash and the current run id, adding a new
// entry or overwriting an existing one. It is called for every file SUBMITTED in the
// current run (per-run stamping, never per-chunk): a file reviewed in any chunk of a
// completed run is recorded once under that run's id.
func (idx *FileHashIndex) Record(path, hash, runID string) {
	if idx == nil {
		return
	}
	if idx.entries == nil {
		idx.entries = make(map[string]fileHashEntry)
	}
	idx.entries[path] = fileHashEntry{Hash: hash, LastReviewedRunID: runID}
}

// Trim prunes entries whose path is not in keep (the current git ls-files tracked
// set), so the index stays bounded to currently-tracked files and a deleted file's
// stale entry does not linger (AC 04-01 Edge Case 1). Callers pass the live tracked
// set.
//
// A NIL keep set means "unknown tracked set — keep everything", NOT "trim all": the
// tracked set originates from stream.BuildFileIndex, which degrades to nil on a
// transient git failure, and a single git hiccup must not wipe the whole accumulated
// skip index (4.5.A LOW). An EMPTY-but-non-nil keep set is a genuine "nothing is
// tracked" signal and does trim everything.
func (idx *FileHashIndex) Trim(keep map[string]struct{}) {
	if idx == nil || idx.entries == nil || keep == nil {
		return
	}
	for p := range idx.entries {
		if _, ok := keep[p]; !ok {
			delete(idx.entries, p)
		}
	}
}

// Get returns the recorded hash and run id for path. Nil-receiver-safe.
func (idx *FileHashIndex) Get(path string) (hash, runID string, ok bool) {
	if idx == nil || idx.entries == nil {
		return "", "", false
	}
	e, ok := idx.entries[path]
	return e.Hash, e.LastReviewedRunID, ok
}

// Paths returns the sorted set of recorded paths. Nil-receiver-safe (returns nil).
func (idx *FileHashIndex) Paths() []string {
	if idx == nil || len(idx.entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(idx.entries))
	for p := range idx.entries {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Save writes the index to path via atomicfs.WriteJSON (temp-then-rename), creating
// the containing .atcr/index/ directory if needed. It is called only after a run
// reaches a completed state, so an interrupted run leaves the prior on-disk index
// untouched. A directory-create or write failure is wrapped as
// "writing file-hash index: <err>" for the caller to log at Warn WITHOUT failing an
// otherwise-successful review (AC 04-01 Error Scenario 1).
func (idx *FileHashIndex) Save(path string) error {
	if idx == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("writing file-hash index: %w", err)
	}
	entries := idx.entries
	if entries == nil {
		entries = map[string]fileHashEntry{}
	}
	if err := atomicfs.WriteJSON(path, entries); err != nil {
		return fmt.Errorf("writing file-hash index: %w", err)
	}
	return nil
}
