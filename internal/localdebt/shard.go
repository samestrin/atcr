package localdebt

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// RecordFiles lists every file of the store at dir that holds records, as paths, in
// the order every read path reads them: month shards in lexical order, then the
// event files of the event layout (events.go) in name order, which is by finding id
// and then event time.
//
// It is the ONE enumeration of a store's record files, for the same reason
// IsShardEntry is the one shard predicate: ReadAll, StreamSummaries, the compaction
// read and cli/debt_resolve.go's hydration must see the same files in the same
// order, or a store holding month shards, event files, or both folds differently
// depending on which path read it. The order is load-bearing: the fold's full-tie
// rule is "last append wins", which is positional.
//
// A missing store, or a store with no items directory, is not an error. Errors are
// redacted through basePathErr like every other read path.
func RecordFiles(dir string) ([]string, error) {
	files, err := shardFilePaths(dir)
	if err != nil {
		return nil, err
	}
	events, err := eventFilePaths(dir)
	if err != nil {
		return nil, err
	}
	return append(files, events...), nil
}

// shardFilePaths lists the store's month shard files in lexical order. A missing
// store is empty, not an error.
func shardFilePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}
	var files []string
	for _, e := range entries {
		if IsShardEntry(e) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// eventFilePaths lists the store's event files in name order. A missing items
// directory is empty, not an error.
func eventFilePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(ItemsDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading localdebt items dir: %w", basePathErr(err))
	}
	var files []string
	for _, e := range entries {
		if IsEventEntry(e) {
			files = append(files, filepath.Join(ItemsDir(dir), e.Name()))
		}
	}
	return files, nil
}
