package history

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PruneResult reports what a prune pass did: the base names of the shards it
// removed (sorted, so the report is deterministic) and how many *.jsonl files it
// retained. Kept counts every retained candidate file, including one whose stem
// is not a YYYY-MM month — such a file is deliberately never a deletion
// candidate, so it is retained by definition. Removed is empty when nothing was
// past the horizon, a distinction the caller needs in order to say "pruned N"
// versus "nothing to prune".
//
// Removed is built incrementally: on a failed pass it names the shards already
// unlinked before the failure. Deletion cannot be rolled back, so a caller must
// report those rather than treating the error as a clean abort.
type PruneResult struct {
	Removed []string
	Kept    int
}

// PruneShards deletes whole monthly shards under dir whose month lies entirely
// before now-horizon, and reports what it removed.
//
// Retention here is bounded by TIME, at FILE granularity — deliberately unlike
// the sibling debt store, which compacts by folding to the latest record per id.
// A trend ledger answers "how did this package's severity profile move over six
// months"; folding by id would destroy exactly that (Epic 35.14 Design Decision
// 2). So nothing inside a surviving shard is ever rewritten: a shard is removed
// whole or left entirely alone.
//
// The horizon test is the same month-intersection rule the read path uses, so
// retention and selection can never disagree about a boundary month: a month
// that merely CONTAINS the cutoff is kept, because it still holds in-horizon
// records. What survives a prune is exactly what a query over the same window
// would have read.
//
// Deletion is conservative in three further ways, because this is the only
// destructive operation in the package:
//
//   - Only *.jsonl regular files are candidates; directories and any other file
//     in the shard dir are untouched.
//   - A stem that does not parse as YYYY-MM is NEVER deleted. It cannot be proven
//     past the horizon, and treating an unrecognized name as prunable would make
//     an unrelated file destroyable by a prune.
//   - A non-positive horizon is an error, not a wipe. "Retain nothing" must not
//     share a spelling with an unset or miscomputed horizon.
//
// The legacy flat ledger is not a shard and is never considered: it is a single
// file spanning every pre-19.4 month, so deleting it would be record-granularity
// pruning by proxy, and it is documented read-only.
//
// A missing dir is nothing to prune, not an error. A shard that cannot be
// removed fails the call, after any earlier removals have already happened —
// deletion cannot be rolled back, so PruneResult reports what was actually
// removed rather than pretending the pass was atomic.
func PruneShards(dir string, horizon time.Duration, now time.Time) (PruneResult, error) {
	var res PruneResult
	if horizon <= 0 {
		return res, fmt.Errorf("retention horizon must be positive, got %s", horizon)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("reading history shards: %w", err)
	}

	cutoff := now.Add(-horizon)
	var doomed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if shardMonthIntersects(e.Name(), cutoff) {
			res.Kept++
			continue
		}
		doomed = append(doomed, e.Name())
	}
	sort.Strings(doomed)

	for _, name := range doomed {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return res, fmt.Errorf("removing history shard %s: %w", name, err)
		}
		res.Removed = append(res.Removed, name)
	}
	return res, nil
}
