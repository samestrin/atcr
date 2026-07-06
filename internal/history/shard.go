package history

import (
	"path/filepath"
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
