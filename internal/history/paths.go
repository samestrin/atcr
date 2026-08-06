package history

import "path/filepath"

// ShardDir returns the monthly-shard directory for a repo rooted at root:
// <root>/.atcr/history. Centralizing the layout here is the single source of
// truth for where shards live, so the write hooks (atcr review, atcr resume) and
// the read path (atcr history) cannot drift apart on the location — the drift
// that let review/resume write shards a repo-root-relative query never read.
//
// Epic 19.4 put the shards under .planning/history so a team could commit and
// share trend data; Epic 35.14 moved them back under .atcr/ because a standalone
// atcr user has no .planning/ directory at all, leaving `atcr history` with
// nowhere to read or write. Sharing the ledger through version control was the
// narrower need, and .atcr/ is where atcr owns its artifacts (Epic 35.13). Any
// pre-existing .planning/history/ shards are deliberately left in place and are
// NOT read: atcr's history code resolves no path under .planning/ (35.14 AC3).
func ShardDir(root string) string {
	return filepath.Join(root, ".atcr", "history")
}

// LegacyLedgerPath returns the pre-19.4 flat ledger for a repo rooted at root:
// <root>/.atcr/findings-history.jsonl. It is paired with ShardDir so the two
// storage locations that make up the full queryable history are defined in one
// place and stay consistent across every caller.
func LegacyLedgerPath(root string) string {
	return filepath.Join(root, ".atcr", "findings-history.jsonl")
}
