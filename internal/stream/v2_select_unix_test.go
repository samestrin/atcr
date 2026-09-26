//go:build unix

package stream

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectFindingsFile_FIFOToonCountsAsAbsent: a FIFO named findings.toon
// would block a reader forever, so selection treats it as absent.
func TestSelectFindingsFile_FIFOToonCountsAsAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "findings.toon"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	writeFile(t, dir, "findings.txt", Version+"\n")

	got, err := SelectFindingsFile(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "findings.txt"), got)
}

// A FIFO named findings.txt would block a reader forever, so selection treats
// it as absent too (TD-026).
func TestSelectFindingsFile_FIFOTxtCountsAsAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "findings.txt"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	_, err := SelectFindingsFile(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "got %v", err)
}

// The read opens the selected file once and checks the handle, so a symlink or
// FIFO swapped in after selection is refused, and a FIFO never blocks (TD-030).
func TestReadFindingsFile_RefusesASwappedInSymlinkOrFIFO(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	require.NoError(t, os.WriteFile(target, []byte(VersionV2+"\n"), 0o644))
	link := filepath.Join(dir, "findings.toon")
	require.NoError(t, os.Symlink(target, link))
	_, err := readFindingsFile(link)
	assert.Error(t, err, "a symlink is not followed")

	fifo := filepath.Join(dir, "fifo.toon")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := readFindingsFile(fifo); done <- err }()
	select {
	case err := <-done:
		assert.Error(t, err, "a FIFO is not a regular file")
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}

// A findings directory selection cannot search is an error, not an absent
// file: reporting fs.ErrNotExist would send every reader down its "no
// findings" branch for a pool it never read.
func TestSelectFindingsFile_UnsearchableDirIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root searches a mode-0 directory")
	}
	dir := t.TempDir()
	writeFile(t, dir, "findings.txt", Version+"\n")
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := SelectFindingsFile(dir)
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist), "got %v", err)
}
