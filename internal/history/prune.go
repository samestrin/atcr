package history

import "time"

// PruneResult reports what a prune pass removed. Stub.
type PruneResult struct {
	Removed []string
	Kept    int
}

// PruneShards deletes whole shards older than a retention horizon. Stub.
func PruneShards(dir string, horizon time.Duration, now time.Time) (PruneResult, error) {
	return PruneResult{}, nil
}
