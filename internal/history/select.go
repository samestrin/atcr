package history

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadShardsSince reads only the monthly shards under dir whose month intersects
// the window [now-since, ∞) and returns their merged records, ordered by shard
// file name (chronological, for the YYYY-MM naming).
//
// The point is that selection happens on the FILENAME, before anything is
// opened. Month sharding is a real index here, unlike in the sibling debt store:
// the dominant query is time-windowed and the file name encodes the month, so a
// `--since 30d` query opens at most two MONTH-NAMED shards no matter how many
// years of history a repo has accrued (Epic 35.14 Design Decision 3) — plus any
// *.jsonl whose stem does not parse as YYYY-MM and any future-dated shard, both
// always selected (below), and the legacy flat ledger, which LoadAllSince always
// reads in full. Enumeration still costs one directory read, which is O(months)
// in stat calls but zero in bytes parsed — the part that grows with the ledger.
//
// Selection may over-select but must never under-select: it works at file
// granularity, so a shard is taken whole as soon as any instant in its month is
// in the window, and callers still apply Filter for the exact record-level
// window. Two cases are therefore deliberately inclusive:
//
//   - A month that merely CONTAINS the cutoff is selected whole, since it also
//     holds in-window records.
//   - A stem that does not parse as YYYY-MM is selected. An unknown month cannot
//     be proven out of window, and silently dropping data is a worse failure than
//     reading one extra file — the same tolerant-read posture Load takes toward a
//     malformed line.
//
// The window has a lower bound only, so a future-dated shard (clock skew, or a
// machine running ahead) is always selected. A non-positive since selects
// nothing: ParseSince rejects such values at the CLI boundary, so reaching here
// means a caller bug, and "silently load everything" is the wrong failure mode
// for a query whose whole purpose is to be bounded.
//
// That the READ paths return empty here while PruneShards ERRORS on a
// non-positive horizon is deliberate, not an inconsistency left unresolved.
// A read that degrades to empty is recoverable — re-run the query — so the
// bounded-query contract is satisfied by returning nothing. PruneShards is this
// package's one irreversible operation, and "retain nothing" would wipe the
// ledger, so it is the only entry point where a strict error contract earns its
// keep. Both readers return a FULLY empty result (LoadAllSince guards the same
// way before touching the legacy ledger), so no caller can observe the
// half-populated result the two behaviors might otherwise suggest.
//
// A missing dir is a valid empty history, not an error (mirrors LoadShards).
// An unreadable shard is skipped with a warning to diag (default os.Stderr).
func LoadShardsSince(dir string, since time.Duration, now time.Time, diag ...io.Writer) ([]Record, error) {
	if since <= 0 {
		return []Record{}, nil // an explicit empty result, not a nil slice: the caller asked a bounded query
	}
	cutoff := now.Add(-since)
	return loadShardsWhere(dir, func(name string) bool {
		return shardMonthIntersects(name, cutoff)
	}, diag...)
}

// isShardFile is the single "is a history shard candidate" predicate shared by
// selection (loadShardsWhere), pruning (PruneShards), and existence checks
// (HasAny): a non-directory *.jsonl entry. Centralizing it is what stops the
// three paths from drifting on what counts as a shard — the same drift risk
// Epic 35.14 centralized ShardDir to eliminate at directory granularity.
func isShardFile(e os.DirEntry) bool {
	return !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl")
}

// diagWriter resolves the optional diagnostic writer threaded through the
// loader chain: os.Stderr unless the caller supplied one. The caller's writer
// (e.g. cobra's ErrOrStderr) is what makes a skipped-shard warning visible to
// the CLI's own stderr contract and test harness — a process-global os.Stderr
// write bypasses both.
func diagWriter(diag []io.Writer) io.Writer {
	if len(diag) > 0 && diag[0] != nil {
		return diag[0]
	}
	return os.Stderr
}

// loadShardsWhere is the single shard-reading implementation behind both
// LoadShards (keep everything) and LoadShardsSince (keep the window). keep is
// consulted on the file NAME only, before the file is opened — that is what
// makes the windowed read cheap, and keeping one implementation is what stops
// the two entry points from drifting on ordering, tolerance, or which files
// count as shards.
//
// Results are ordered by shard file name, which for the YYYY-MM naming is
// chronological. A missing dir is a valid empty history, not an error.
// An unreadable shard is skipped with a warning to diag (default os.Stderr).
func loadShardsWhere(dir string, keep func(name string) bool, diag ...io.Writer) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading history shards: %w", err)
	}

	var selected []string
	for _, e := range entries {
		if !isShardFile(e) {
			continue
		}
		if !e.Type().IsRegular() {
			// Reads never open special files: a FIFO named *.jsonl would hang
			// Load on open, and a symlink would read a file outside the shard
			// dir. (PruneShards instead unlinks such entries AS links — the
			// paths share the isShardFile name check but differ here.)
			continue
		}
		if !keep(e.Name()) {
			continue
		}
		selected = append(selected, filepath.Join(dir, e.Name()))
	}
	sort.Strings(selected)

	var all []Record
	for _, path := range selected {
		recs, err := Load(path)
		if err != nil {
			// Skip an unreadable individual shard so the remaining shards stay
			// queryable, mirroring Load's line-level tolerance for torn writes.
			_, _ = fmt.Fprintf(diagWriter(diag), "warning: skipping unreadable history shard %s: %v\n", path, err)
			continue
		}
		all = append(all, recs...)
	}
	return all, nil
}

// shardMonthIntersects reports whether the shard named name can hold a record at
// or after cutoff, judged from its filename alone. A stem that is not a valid
// YYYY-MM month returns true: it cannot be proven out of window.
//
// The test is against the END of the shard's month — the first instant of the
// NEXT month, exclusive — so the month containing the cutoff is kept. Comparing
// against the month's start instead would drop it and lose every in-window
// record it holds.
func shardMonthIntersects(name string, cutoff time.Time) bool {
	stem := strings.TrimSuffix(name, ".jsonl")
	month, err := time.Parse(shardMonthLayout, stem)
	if err != nil {
		return true
	}
	// time.Parse yields the first instant of the month in UTC; the shard's month
	// ends immediately before the same instant one month later.
	return month.AddDate(0, 1, 0).After(cutoff)
}

// LoadAllSince is LoadAll narrowed to a time window: the shards whose month
// intersects [now-since, ∞) (LoadShardsSince) unioned with the legacy pre-19.4
// flat ledger, deduplicated on (Timestamp, ID) exactly as LoadAll does (both
// run through unionWithLegacy, so the merge cannot drift).
//
// A non-positive since selects NOTHING, legacy included — the same posture
// LoadShardsSince documents: reaching here with such a value is a caller bug
// (ParseSince rejects it at the CLI boundary), and "silently load everything"
// is the wrong failure mode for a query whose whole purpose is to be bounded.
//
// The legacy ledger is always READ in full. It is a single flat file with no
// month in its name, so there is nothing to select on — but its records are
// cut to the window before the dedupe pass, so a narrow --since does not pay
// a dedupe sized by the entire pre-19.4 history. It is read in place and
// never rewritten.
//
// Records still need Filter afterwards: selection is a file-granularity
// pre-filter that can over-select (a shard is taken whole when its month
// intersects), never a substitute for the record-level window check. An
// unreadable shard is skipped with a warning to diag (default os.Stderr).
func LoadAllSince(shardDir, legacyPath string, since time.Duration, now time.Time, diag ...io.Writer) ([]Record, error) {
	if since <= 0 {
		return []Record{}, nil
	}
	shards, err := LoadShardsSince(shardDir, since, now, diag...)
	if err != nil {
		return nil, err
	}
	return unionWithLegacy(shards, legacyPath, now.Add(-since))
}

// HasAny reports whether either read location holds anything at all: at least
// one *.jsonl shard under shardDir, or a non-empty legacy ledger at legacyPath.
//
// It exists because windowed selection made "zero records loaded" ambiguous. A
// repo whose only shards fall outside --since loads nothing, which is not the
// same as a repo that has never run a review, and the two deserve different
// notices. The check stats rather than parses — no ledger bytes are read — so
// asking the question does not undo the selection it exists to support.
//
// Any error (missing dir, unreadable entry) reports false: the question is
// "is there something to say history exists", and an unanswerable question is
// not a yes. Both locations use the same emptiness test — a shard, like the
// legacy ledger, counts only when it is a non-empty FILE: a zero-byte
// 2026-08.jsonl is no more history than a zero-byte ledger.
func HasAny(shardDir, legacyPath string) bool {
	if entries, err := os.ReadDir(shardDir); err == nil {
		for _, e := range entries {
			if !isShardFile(e) {
				continue
			}
			if info, err := e.Info(); err == nil && info.Size() > 0 {
				return true
			}
		}
	}
	if info, err := os.Stat(legacyPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return false
}
