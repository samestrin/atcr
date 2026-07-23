package atomicwrite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteGroup_WritesAllFiles(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.json")
	p2 := filepath.Join(dir, "b.json")

	require.NoError(t, WriteGroup([]Entry{
		{Path: p1, Data: []byte("alpha\n")},
		{Path: p2, Data: []byte("beta\n")},
	}))

	a, err := os.ReadFile(p1)
	require.NoError(t, err)
	assert.Equal(t, "alpha\n", string(a))

	b, err := os.ReadFile(p2)
	require.NoError(t, err)
	assert.Equal(t, "beta\n", string(b))
}

func TestWriteGroup_FailsOnInvalidPath(t *testing.T) {
	dir := t.TempDir()
	invalidPath := filepath.Join(dir, "nonexistent", "f.json")

	err := WriteGroup([]Entry{
		{Path: invalidPath, Data: []byte("data\n")},
	})
	require.Error(t, err, "WriteGroup must fail when parent directory does not exist")
}

func TestWriteGroup_NoOpForEmpty(t *testing.T) {
	require.NoError(t, WriteGroup(nil))
	require.NoError(t, WriteGroup([]Entry{}))
}

func TestWriteGroup_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	require.NoError(t, os.WriteFile(p, []byte("old"), 0o644))

	require.NoError(t, WriteGroup([]Entry{{Path: p, Data: []byte("new\n")}}))

	got, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(got))
}
