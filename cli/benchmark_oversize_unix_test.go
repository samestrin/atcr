//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// The LimitReader arm of readRunResultLimited covers the window in which a file
// grows between the stat and the read — the one shape a stat alone cannot catch.
// Its distinct message ("grew past ... while it was being read") is pinned here,
// separately from the stat arm's, because the two arms wrap the same sentinel and
// previously masked each other behind one shared-text assertion.
//
// A FIFO is how the stat-passes-read-overflows shape is staged honestly: os.Stat on
// a named pipe reports size 0, so the stat arm passes, while the read pulls more
// than maxRunResultBytes through the LimitReader. A regular file cannot reach this
// arm at all — its stat size IS its read size, so the stat arm always fires first.
func TestReadRunResultLimited_GrowthArmFiresWhenTheFileGrowsPastTheCeiling(t *testing.T) {
	orig := maxRunResultBytes
	maxRunResultBytes = 64
	defer func() { maxRunResultBytes = orig }()

	fifo := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600), "a named pipe stages the stat-passes-read-overflows shape")

	big := make([]byte, 128)
	go func() {
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer func() { _ = w.Close() }()
		_, _ = w.Write(big)
	}()

	_, err := readRunResultLimited(fifo)
	require.Error(t, err, "a read past the ceiling must be rejected")
	require.ErrorIs(t, err, errRunResultTooLarge, "the growth arm wraps the same sentinel")
	require.Contains(t, err.Error(), "grew past 64 bytes while it was being read",
		"the growth arm's distinct message — naming the growth window — must be pinned")
	require.NotContains(t, err.Error(), " is ",
		"the growth arm fired here; the stat arm's message must not appear")
}
