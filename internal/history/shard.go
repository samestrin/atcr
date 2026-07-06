package history

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// shardMonthLayout is the year-month stem of a monthly history shard file.
const shardMonthLayout = "2006-01"

// ShardPath returns the monthly shard file for a run at ts under dir, e.g.
// <dir>/2026-07.jsonl. The month is taken in UTC so shard names are
// deterministic regardless of the caller's local zone, and every record from a
// single run (all stamped with the same ts) lands in exactly one shard.
//
// Because the file name is derived from ts, a run in a new month writes to a new
// file and never reopens a prior month's shard — that is what stops old shards
// from churning fresh git blobs once the month rolls over (Epic 19.4 AC3).
func ShardPath(dir string, ts time.Time) string {
	return filepath.Join(dir, ts.UTC().Format(shardMonthLayout)+".jsonl")
}

// LoadShards reads every monthly shard (*.jsonl) under dir and returns the
// merged records across all months, ordered by shard file name — which, for the
// YYYY-MM naming, is chronological. A missing or empty dir is a valid empty
// history, not an error (mirroring Load), so `atcr history` answers a query
// across whatever shards exist without the caller naming one (Epic 19.4 AC2).
// Malformed lines inside a shard are skipped by Load; an unreadable shard is a
// hard error.
func LoadShards(dir string) ([]Record, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("globbing history shards: %w", err)
	}
	sort.Strings(matches)

	var all []Record
	for _, path := range matches {
		recs, err := Load(path)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
	}
	return all, nil
}

// LoadAll returns the full queryable history: every monthly shard under shardDir
// (LoadShards) merged with the legacy flat ledger at legacyPath, if it still
// exists. The legacy file — the pre-19.4 .atcr/findings-history.jsonl — is read
// in place and read-only; it is never moved or rewritten (Epic 19.4: "no complex
// write-migrations"), so a project that accrued history before sharding keeps
// that data visible alongside new shards. An absent shard dir or legacy file is
// simply empty history, not an error.
func LoadAll(shardDir, legacyPath string) ([]Record, error) {
	shards, err := LoadShards(shardDir)
	if err != nil {
		return nil, err
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		return nil, err
	}
	return append(legacy, shards...), nil
}
