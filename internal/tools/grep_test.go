package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func grep(t *testing.T, d *Dispatcher, args string) (ToolResult, error) {
	t.Helper()
	return d.Execute(context.Background(), "grep", json.RawMessage(args))
}

func TestGrep_MatchesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func Foo() {}\n")
	writeFile(t, root, "b.go", "func Bar() {}\n")
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"func \\w+"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "a.go:1: func Foo() {}")
	assert.Contains(t, res.Content, "b.go:1: func Bar() {}")
}

func TestGrep_GlobFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func A() {}\n")
	writeFile(t, root, "b.go", "func B() {}\n")
	writeFile(t, root, "readme.md", "func text\n")
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"func","glob":"*.go"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "a.go")
	assert.Contains(t, res.Content, "b.go")
	assert.NotContains(t, res.Content, "readme.md")
}

func TestGrep_NoMatches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "package main\n")
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"xyz123"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "no matches for 'xyz123'")
	assert.False(t, res.Truncated)
}

func TestGrep_MatchCapTruncation(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("match line\n")
	}
	writeFile(t, root, "a.go", sb.String())
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxGrepMatches: 10, MaxResultBytes: 1 << 20, MaxGrepLineBytes: 512})

	res, err := grep(t, d, `{"pattern":"match"}`)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Equal(t, 10, strings.Count(res.Content, "a.go:"))
	assert.Contains(t, res.Content, "truncated")
}

func TestGrep_InvalidRegex(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := grep(t, d, `{"pattern":"[invalid"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestGrep_EmptyPattern(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := grep(t, d, `{"pattern":""}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pattern cannot be empty")
}

func TestGrep_DirRestrictsSearch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.go", "func InSrc() {}\n")
	writeFile(t, root, "test/b.go", "func InTest() {}\n")
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"func","dir":"src"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "InSrc")
	assert.NotContains(t, res.Content, "InTest")
}

func TestGrep_DirIsFileError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "x\n")
	d := newTestDispatcher(t, root)

	_, err := grep(t, d, `{"pattern":"func","dir":"src/main.go"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a directory")
}

func TestGrep_GlobNoMatches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "func A() {}\n")
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"func","glob":"*.md"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "no matches")
}

// TestGrep_TruncatedOriginalBytesIsBytes verifies that OriginalBytes in a
// match-truncated grep result is a byte count, not a match count.
// Before the fix: OriginalBytes = total_matches (e.g. 2000), which is
// Grep must skip a .GIT directory (case-insensitive match, catching macOS/Windows
// case-preserving filesystems where the entry name is ".GIT" not ".git").
func TestGrep_SkipsDotGITCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	// Create .GIT (uppercase) with sensitive content; the walker must skip it.
	gitDir := filepath.Join(root, ".GIT")
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte("url = https://user:secret@host\n"), 0o644))
	d := newTestDispatcher(t, root)

	res, err := grep(t, d, `{"pattern":"url"}`)
	require.NoError(t, err)
	assert.NotContains(t, res.Content, ".GIT", ".GIT directory must be skipped even when directory name is uppercase")
}

// incoherent with the byte-count semantics capResult and read_file use.
func TestGrep_TruncatedOriginalBytesIsBytes(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("match\n")
	}
	writeFile(t, root, "a.go", sb.String())
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxGrepMatches: 5, MaxResultBytes: 1 << 20, MaxGrepLineBytes: 512})

	res, err := grep(t, d, `{"pattern":"match"}`)
	require.NoError(t, err)
	require.True(t, res.Truncated)
	// OriginalBytes must equal the content byte length (consistent with read_file / list_files).
	// Before fix it equals the match count (2000), which is far larger and incoherent.
	assert.Equal(t, len(res.Content), res.OriginalBytes,
		"OriginalBytes must be a byte count equal to content length, not match count; got %d, content len %d",
		res.OriginalBytes, len(res.Content))
}
