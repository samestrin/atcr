//go:build unix

package localdebt

import (
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryJSON is a minimal reconciled/summary.json body naming one reconciled_at.
const summaryJSON = `{"reconciled_at":"2026-08-04T10:00:00Z"}`

// writeRegularSummary creates reviewDir/reconciled/summary.json as a regular file.
func writeRegularSummary(t *testing.T, reviewDir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o750))
	path := filepath.Join(reviewDir, "reconciled", "summary.json")
	require.NoError(t, os.WriteFile(path, []byte(summaryJSON), 0o600))
	return path
}

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

// TD db483cca5f54a5d2 — readSummary Lstats and THEN opens, so a FIFO swapped in
// between the two syscalls still blocked the open forever while the backfill
// pass holds the store lock. The Lstat saw a regular file; the default opener
// must not block on what the path became. The seam swaps the file at open time
// and delegates to the pass's real default opener, exactly the race window.
func TestReadSummary_FIFOSwappedInAfterLstatOwnsNothing(t *testing.T) {
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	path := writeRegularSummary(t, reviewDir)
	open := func(name string) (*os.File, error) {
		require.NoError(t, os.Remove(name))
		if err := syscall.Mkfifo(name, 0o600); err != nil {
			t.Skipf("mkfifo unsupported: %v", err)
		}
		return defaultBackfillSeams().open(name)
	}

	type result struct {
		at        string
		oversized bool
	}
	done := make(chan result, 1)
	go func() {
		at, oversized := readSummary(reviewDir, open, slog.Default())
		done <- result{at, oversized}
	}()
	select {
	case got := <-done:
		assert.Empty(t, got.at, "a FIFO swapped in after the Lstat owns nothing")
		assert.False(t, got.oversized)
	case <-time.After(5 * time.Second):
		// Release the blocked opener so the goroutine does not outlive the test.
		if w, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("readSummary blocked opening a FIFO swapped in after the Lstat")
	}
}

// TD db483cca5f54a5d2 — even an opener that hands back a non-regular handle must
// not be read: the check belongs on the DESCRIPTOR, not only on the path before
// the open. The seam returns a FIFO handle with a writer already drained, so a
// readSummary without the handle check parses the payload and credits ownership
// to a non-regular file (and with a live writer it would block in io.ReadAll).
func TestReadSummary_NonRegularHandleOwnsNothing(t *testing.T) {
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	writeRegularSummary(t, reviewDir)
	open := func(name string) (*os.File, error) {
		require.NoError(t, os.Remove(name))
		if err := syscall.Mkfifo(name, 0o600); err != nil {
			t.Skipf("mkfifo unsupported: %v", err)
		}
		r, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		w, err := os.OpenFile(name, os.O_WRONLY, 0)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
		_, _ = w.Write([]byte(summaryJSON))
		_ = w.Close()
		return r, nil
	}

	done := make(chan string, 1)
	go func() {
		at, _ := readSummary(reviewDir, open, slog.Default())
		done <- at
	}()
	select {
	case at := <-done:
		assert.Empty(t, at, "a non-regular handle owns nothing, whatever it can be read to say")
	case <-time.After(5 * time.Second):
		t.Fatal("readSummary read from a non-regular handle instead of refusing it")
	}
}

// TD db483cca5f54a5d2 — the default summary opener refuses a symlink at the open
// (O_NOFOLLOW on unix), so a link swapped in after the Lstat cannot redirect the
// read. Every default-open path owes this: the seams default, the ownershipCache
// zero value, and the uncached reviewDirOwnsRun wrapper.
func TestSummaryDefaultOpen_RefusesSymlink(t *testing.T) {
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o750))
	target := filepath.Join(reviewDir, "elsewhere.json")
	require.NoError(t, os.WriteFile(target, []byte(summaryJSON), 0o600))
	link := filepath.Join(reviewDir, "reconciled", "summary.json")
	require.NoError(t, os.Symlink(target, link))

	f, err := defaultBackfillSeams().open(link)
	if f != nil {
		_ = f.Close()
	}
	assert.Error(t, err, "the default opener must refuse a symlinked summary.json")

	const runID = "2026-08-04T10:00:00Z-multi-agent"
	assert.False(t, (&ownershipCache{}).owns(reviewDir, runID),
		"the cache's default open must refuse a symlinked summary.json")
	assert.False(t, reviewDirOwnsRun(reviewDir, runID),
		"the uncached wrapper's default open must refuse a symlinked summary.json")
}
