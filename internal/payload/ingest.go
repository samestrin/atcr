package payload

import "errors"

// DefaultMaxDiffBytes is the default per-file size cap for diff-file reads,
// mirroring benchmark.MaxDiffBytes (10 MiB): a hostile or accidental multi-GB
// diff in externally-sourced input must not cause unbounded allocation.
const DefaultMaxDiffBytes int64 = 10 * 1024 * 1024

// BuildEntriesFromDiff parses unified diff text into per-file FileEntry values —
// the same []FileEntry shape BuildEntries(ModeDiff, ...) returns.
func BuildEntriesFromDiff(diffText string) ([]FileEntry, error) {
	return nil, errors.New("not implemented")
}

// BuildEntriesFromDiffFile reads a diff file and delegates to BuildEntriesFromDiff.
func BuildEntriesFromDiffFile(path string, maxBytes int64) ([]FileEntry, error) {
	return nil, errors.New("not implemented")
}
