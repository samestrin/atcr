package tools

import (
	"context"
	"encoding/json"
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
