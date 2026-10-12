//go:build unix

package localdebt

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/localdebt/backfill.go:505 — a FIFO named summary.json blocked the
// ownership check's open forever, and the backfill pass holds the store lock
// while it runs. A non-regular summary.json owns nothing, and the read returns
// rather than waiting on a writer that never comes.
func TestReviewDirOwnsRun_FIFOSummaryOwnsNothing(t *testing.T) {
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o750))
	if err := syscall.Mkfifo(filepath.Join(reviewDir, "reconciled", "summary.json"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	done := make(chan bool, 1)
	go func() { done <- reviewDirOwnsRun(reviewDir, "2026-08-04T10:00:00Z-multi-agent") }()
	select {
	case owned := <-done:
		assert.False(t, owned)
	case <-time.After(5 * time.Second):
		t.Fatal("reviewDirOwnsRun blocked opening a FIFO summary.json")
	}
}
