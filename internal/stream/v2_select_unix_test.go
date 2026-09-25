//go:build unix

package stream

import (
	"path/filepath"
	"syscall"
	"testing"

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
