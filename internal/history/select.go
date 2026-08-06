package history

import (
	"fmt"
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
// `--since 30d` query opens at most two files no matter how many years of
// history a repo has accrued (Epic 35.14 Design Decision 3). Enumeration still
// costs one directory read, which is O(months) in stat calls but zero in bytes
// parsed — the part that grows with the ledger.
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
// A missing dir is a valid empty history, not an error (mirrors LoadShards).
func LoadShardsSince(dir string, since time.Duration, now time.Time) ([]Record, error) {
	if since <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading history shards: %w", err)
	}

	cutoff := now.Add(-since)

	var selected []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if !shardMonthIntersects(e.Name(), cutoff) {
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
			// queryable, mirroring LoadShards and Load's line-level tolerance.
			_, _ = fmt.Fprintf(os.Stderr, "warning: skipping unreadable history shard %s: %v\n", path, err)
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
// flat ledger, deduplicated on (Timestamp, ID) exactly as LoadAll does.
//
// The legacy ledger is always read in full. It is a single flat file with no
// month in its name, so there is nothing to select on — the shard-selection
// optimization applies only to the sharded location. It is read in place and
// never rewritten.
//
// Records still need Filter afterwards: selection is a file-granularity
// pre-filter that can over-select, never a substitute for the record-level
// window check.
func LoadAllSince(shardDir, legacyPath string, since time.Duration, now time.Time) ([]Record, error) {
	shards, err := LoadShardsSince(shardDir, since, now)
	if err != nil {
		return nil, err
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		return nil, err
	}
	return dedupeOccurrences(append(legacy, shards...)), nil
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
// not a yes.
func HasAny(shardDir, legacyPath string) bool {
	if entries, err := os.ReadDir(shardDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				return true
			}
		}
	}
	if info, err := os.Stat(legacyPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return false
}
