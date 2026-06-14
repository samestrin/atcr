package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listFiles(t *testing.T, d *Dispatcher, args string) (ToolResult, error) {
	t.Helper()
	return d.Execute(context.Background(), "list_files", json.RawMessage(args))
}

func TestListFiles_Root(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "x\n")
	writeFile(t, root, "b.go", "x\n")
	writeFile(t, root, "src/keep.go", "x\n")
	d := newTestDispatcher(t, root)

	res, err := listFiles(t, d, `{}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "f a.go")
	assert.Contains(t, res.Content, "f b.go")
	assert.Contains(t, res.Content, "d src")
}

func TestListFiles_Subdirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/pkg/util.go", "x\n")
	d := newTestDispatcher(t, root)

	res, err := listFiles(t, d, `{"dir":"src"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "d pkg")
	assert.Contains(t, res.Content, "f pkg/util.go")
}

func TestListFiles_RecursiveWithinDepthCap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/pkg/helper.go", "x\n")
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxListDepth: 5, MaxListFiles: 1000, MaxResultBytes: 1 << 20})

	res, err := listFiles(t, d, `{"dir":"src"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "d pkg")
	assert.Contains(t, res.Content, "f pkg/helper.go")
}

func TestListFiles_DepthCapTruncation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/b/c/d/deep.go", "x\n")
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxListDepth: 1, MaxListFiles: 1000, MaxResultBytes: 1 << 20})

	res, err := listFiles(t, d, `{}`)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Contains(t, res.Content, "depth cap")
}

func TestListFiles_EntryCapTruncation(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		writeFile(t, root, n, "x\n")
	}
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxListDepth: 5, MaxListFiles: 2, MaxResultBytes: 1 << 20})

	res, err := listFiles(t, d, `{}`)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Contains(t, res.Content, "entries truncated")
}

func TestListFiles_EmptyDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "empty"), 0o755))
	d := newTestDispatcher(t, root)

	res, err := listFiles(t, d, `{"dir":"empty"}`)
	require.NoError(t, err)
	assert.Equal(t, "", res.Content)
	assert.False(t, res.Truncated)
}

func TestListFiles_DefaultDirIsRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "x\n")
	d := newTestDispatcher(t, root)

	omitted, err := listFiles(t, d, `{}`)
	require.NoError(t, err)
	empty, err := listFiles(t, d, `{"dir":""}`)
	require.NoError(t, err)
	assert.Equal(t, omitted.Content, empty.Content)
}

func TestListFiles_DirIsFileError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "x\n")
	d := newTestDispatcher(t, root)

	_, err := listFiles(t, d, `{"dir":"src/main.go"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a directory")
}

func TestListFiles_DirectoryNotFound(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := listFiles(t, d, `{"dir":"missing"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory not found")
}

// list_files must skip a .GIT directory (case-insensitive, catching macOS/Windows
// case-preserving filesystems where the entry name is ".GIT" not ".git").
func TestListFiles_SkipsDotGITCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	gitDir := filepath.Join(root, ".GIT")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644))
	d := newTestDispatcher(t, root)

	res, err := listFiles(t, d, `{}`)
	require.NoError(t, err)
	assert.NotContains(t, res.Content, ".GIT", "listing must not expose .GIT directory")
}
