package history

import "time"

// LoadShardsSince is the windowed read: it will select shards by filename before
// opening them. Stub — currently reads every shard.
func LoadShardsSince(dir string, since time.Duration, now time.Time) ([]Record, error) {
	return LoadShards(dir)
}

// LoadAllSince is the windowed union of both read locations. Stub — currently
// reads every shard.
func LoadAllSince(shardDir, legacyPath string, since time.Duration, now time.Time) ([]Record, error) {
	return LoadAll(shardDir, legacyPath)
}
