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

// The os.Open and io.ReadAll error arms of readRunResultLimited were added lines
// with no coverage: every other test reaches the function through a readable file,
// so a regression in either arm — a dropped wrap, a lost path prefix — would pass
// the suite silently. Both arms are staged honestly here: a chmod-0 file passes
// os.Stat (stat needs directory execute, not file read) and fails os.Open; a
// directory path opens fine and fails io.ReadAll with EISDIR. chmod cannot block
// root, so the permission fixture skips there — the directory fixture still runs.
func TestReadRunResultLimited_UnreadableFileFailsAtTheOpenArm(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod does not block root")
	}
	path := filepath.Join(t.TempDir(), "run-result.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := readRunResultLimited(path)
	require.Error(t, err, "an unreadable run-result must fail, not read empty")
	require.Contains(t, err.Error(), "reading run-result",
		"the Open arm wraps the failure with the path context, like its siblings")
	// The stat arm must NOT have fired: os.Stat succeeds on a chmod-0 file, so an
	// error here can only have come from the Open arm this test exists to pin.
	require.NotContains(t, err.Error(), "no such file",
		"the stat arm passing is what makes this fixture reach the Open arm at all")
}

func TestReadRunResultLimited_DirectoryAsPathFailsAtTheReadArm(t *testing.T) {
	_, err := readRunResultLimited(t.TempDir())
	require.Error(t, err, "a directory is not a readable run-result")
	require.Contains(t, err.Error(), "reading run-result",
		"the ReadAll arm wraps the failure with the path context, like its siblings")
}
