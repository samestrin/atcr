package localdebt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IsShardEntry is the single definition of the shard predicate, called by every walk in
// the tree. Both halves matter and are asserted separately.
//
// The IsDir half is load-bearing rather than defensive: os.ReadFile on a directory
// returns "is a directory", so a directory named like a shard aborts whichever walk
// reaches it instead of being skipped.
//
// The extension half is what keeps a non-shard file out of the collision map that names
// which file an in-place rewrite would touch.
func TestIsShardEntry(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-08.jsonl"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".jsonl"), []byte(""), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "2026-07.jsonl"), 0o750))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = IsShardEntry(e)
	}

	assert.True(t, got["2026-08.jsonl"], "an ordinary month shard is a shard")
	assert.False(t, got["notes.txt"], "a non-.jsonl file is not a shard")
	assert.False(t, got["2026-07.jsonl"], "a DIRECTORY named like a shard is not a shard; reading it would abort the walk")

	// Transcribed behaviour, pinned so a later tidy-up is a deliberate choice rather
	// than an accident: the predicate has always used HasSuffix, so a file named
	// exactly ".jsonl" satisfies it.
	assert.True(t, got[".jsonl"], "HasSuffix semantics: a bare .jsonl counts, as it always has")
}
