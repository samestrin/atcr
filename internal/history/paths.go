package history

import "path/filepath"

// ShardDir returns the monthly-shard directory for a repo rooted at root:
// <root>/.planning/history. Centralizing the layout here is the single source of
// truth for where shards live, so the write hooks (atcr review, atcr resume) and
// the read path (atcr history) cannot drift apart on the location — the drift
// that let review/resume write shards a repo-root-relative query never read.
func ShardDir(root string) string {
	return filepath.Join(root, ".planning", "history")
}

// LegacyLedgerPath returns the pre-19.4 flat ledger for a repo rooted at root:
// <root>/.atcr/findings-history.jsonl. It is paired with ShardDir so the two
// storage locations that make up the full queryable history are defined in one
// place and stay consistent across every caller.
func LegacyLedgerPath(root string) string {
	return filepath.Join(root, ".atcr", "findings-history.jsonl")
}
