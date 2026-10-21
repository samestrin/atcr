//go:build unix

package fanout

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A status.json that is a named pipe is skipped without opening it: reading a
// FIFO with no writer blocks forever, so the regular-file check is what keeps a
// poll from hanging.
func TestReadReviewStatus_NonRegularStatusSkipped(t *testing.T) {
	dir, raw := liveReview(t, "fifo", "good")
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "fifo"), 0o755))
	if err := syscall.Mkfifo(filepath.Join(raw, "fifo", statusFile), 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	writeAgentStatus(t, raw, "good", StatusOK)

	type result struct {
		st  *ReviewStatus
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := ReadReviewStatus(dir, "x")
		done <- result{st, err}
	}()
	select {
	case r := <-done:
		require.NoError(t, r.err)
		assert.Equal(t, AgentPending, agentRow(t, r.st, "fifo").State)
		assert.Equal(t, []string{"good"}, r.st.CompletedAgents)
	case <-time.After(10 * time.Second):
		// Unblock the stuck reader so the test binary can exit.
		if w, err := os.OpenFile(filepath.Join(raw, "fifo", statusFile), os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("ReadReviewStatus blocked opening a FIFO status.json")
	}
}
