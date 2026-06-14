package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func readFile(t *testing.T, d *Dispatcher, args string) (ToolResult, error) {
	t.Helper()
	return d.Execute(context.Background(), "read_file", json.RawMessage(args))
}

func TestReadFile_SmallFileLineNumbered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\nimport \"fmt\"\nfunc main() {}\n")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"src/main.go"}`)
	require.NoError(t, err)
	assert.Equal(t, "1: package main\n2: import \"fmt\"\n3: func main() {}\n", res.Content)
	assert.False(t, res.Truncated)
	assert.Equal(t, len(res.Content), res.OriginalBytes)
}

func TestReadFile_SliceStartEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\nimport \"fmt\"\nfunc main() {}\n")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"src/main.go","start_line":2,"end_line":3}`)
	require.NoError(t, err)
	assert.Equal(t, "2: import \"fmt\"\n3: func main() {}\n", res.Content)
	assert.False(t, res.Truncated)
}

func TestReadFile_EmptyFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "empty.go", "")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"empty.go"}`)
	require.NoError(t, err)
	assert.Equal(t, "", res.Content)
	assert.False(t, res.Truncated)
	assert.Equal(t, 0, res.OriginalBytes)
}

func TestReadFile_ByteCapTruncation(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("abcdefghij\n", 500) // ~5500 rendered bytes
	writeFile(t, root, "large.go", big)
	d := newTestDispatcher(t, root)
	d.SetLimits(Limits{MaxReadFileBytes: 100, MaxResultBytes: 1 << 20})

	res, err := readFile(t, d, `{"path":"large.go"}`)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.LessOrEqual(t, len(res.Content), 100)
	assert.Greater(t, res.OriginalBytes, 100)
	assert.Contains(t, res.Content, "truncated")
}

func TestReadFile_StartEqualsEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"file.go","start_line":5,"end_line":5}`)
	require.NoError(t, err)
	assert.Equal(t, "5: l5\n", res.Content)
}

func TestReadFile_StartBeyondFileLength(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\n")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"file.go","start_line":10,"end_line":12}`)
	require.NoError(t, err)
	assert.Equal(t, "", res.Content)
	assert.False(t, res.Truncated)
}

func TestReadFile_EndBeyondFileLength(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\nl4\nl5\n")
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"file.go","start_line":3,"end_line":100}`)
	require.NoError(t, err)
	assert.Equal(t, "3: l3\n4: l4\n5: l5\n", res.Content)
}

func TestReadFile_StartGreaterThanEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\n")
	d := newTestDispatcher(t, root)

	_, err := readFile(t, d, `{"path":"file.go","start_line":5,"end_line":2}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_line cannot be greater than end_line")
}

func TestReadFile_DirectoryError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	d := newTestDispatcher(t, root)

	_, err := readFile(t, d, `{"path":"src"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestReadFile_NotFound(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := readFile(t, d, `{"path":"missing.go"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

func TestReadFile_InvalidArgumentType(t *testing.T) {
	d := newTestDispatcher(t, t.TempDir())
	_, err := readFile(t, d, `{"path":123}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

func TestReadFile_NonPositiveStartLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\n")
	d := newTestDispatcher(t, root)

	for _, sl := range []int{0, -1, -10} {
		args := fmt.Sprintf(`{"path":"file.go","start_line":%d}`, sl)
		_, err := readFile(t, d, args)
		require.Error(t, err, "start_line=%d must be rejected", sl)
		assert.Contains(t, err.Error(), "start_line", "start_line=%d", sl)
	}
}

func TestReadFile_NonPositiveEndLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "file.go", "l1\nl2\nl3\n")
	d := newTestDispatcher(t, root)

	for _, el := range []int{0, -1, -10} {
		args := fmt.Sprintf(`{"path":"file.go","end_line":%d}`, el)
		_, err := readFile(t, d, args)
		require.Error(t, err, "end_line=%d must be rejected", el)
		assert.Contains(t, err.Error(), "end_line", "end_line=%d", el)
	}
}

// TestReadFile_VeryLongLine_Truncated verifies that a line exceeding the
// scanner's 10MB max-token is returned as truncated content rather than an error.
func TestReadFile_VeryLongLine_Truncated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-file test in short mode")
	}
	root := t.TempDir()
	// One line just over the 10MB scanner token limit.
	writeFile(t, root, "huge.txt", strings.Repeat("x", 10*1024*1024+1))
	d := newTestDispatcher(t, root)

	res, err := readFile(t, d, `{"path":"huge.txt"}`)
	// Before fix: err contains "token too long"
	// After fix: err == nil, Truncated == true
	require.NoError(t, err, "line > 10MB must return truncated content, not an error")
	assert.True(t, res.Truncated, "line > 10MB must be marked Truncated")
}
