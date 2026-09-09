package localdebt

import (
	"io/fs"
	"strings"
)

// shardExt is the extension every debt shard file carries. The store is sharded by
// month ("2026-08.jsonl"), and the name is the month.
const shardExt = ".jsonl"

// IsShardEntry reports whether a directory entry is a debt shard file: a non-directory
// whose name ends in ".jsonl".
//
// It is the ONE definition of that predicate FOR THE DEBT STORE. It used to be written
// out at nine call sites across store.go, streaming.go, backfill.go and
// cli/debt_resolve.go, in two spellings (the positive form and its De Morgan inverse),
// which is a drift hazard with teeth rather than a tidiness complaint: ReadAll and the backfill rewrite walk must
// agree on what a shard is, or a record ReadAll returns has no shard the rewrite walk
// will visit, and the pass reports Rewritten > 0 with RewrittenLines == 0.
//
// Exported because cli/debt_resolve.go shares the predicate and cannot see an
// unexported one. The package is under internal/, so this is module-visible, not public
// API.
//
// It is deliberately NOT shared with internal/history or internal/scorecard. They shard
// their own unrelated directories and keep their own copies; folding three stores onto
// one predicate would couple them for a resemblance rather than a shared contract.
//
// The IsDir half is load-bearing, not defensive: os.ReadFile on a directory returns "is
// a directory", so a directory named like a shard aborts a walk that reaches it rather
// than being skipped.
//
// Deliberately a byte-for-byte transcription of the predicate it replaced, HasSuffix
// included. A file named exactly ".jsonl" satisfies it, as it always has; tightening
// that here would be a behaviour change smuggled into a de-duplication.
func IsShardEntry(e fs.DirEntry) bool {
	return !e.IsDir() && strings.HasSuffix(e.Name(), shardExt)
}
