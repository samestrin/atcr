//go:build unix

package sandbox

// Unix-only tests for the cancel path's descendant kill (epic 74 T2). They use
// syscall.Getpgid and the unix-only helpers in oslevel_unix.go, which do not
// exist on Windows; the //go:build unix tag keeps `GOOS=windows go vet` clean.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOSLevelBackendRun_CancelKillsADescendantThatDetachedWithSetsid(t *testing.T) {
	// Epic 74 AC1: the group SIGKILL only reaches processes still in the spawned
	// group, so a descendant that called setsid escaped a cancelled run and could
	// recreate the scratch tree after os.RemoveAll. On Linux bwrap's PID
	// namespace kills it anyway; darwin's sandbox-exec has no such backstop, and
	// this shim (no namespace either) exercises the same unix cancel path.
	//
	// The cancel path snapshots the leader's descendants while the leader is
	// still alive. That is best effort: a descendant forked between the snapshot
	// and the kill, or one whose parent exited before the cancel, is not found.
	// This test covers the case the snapshot can see, a detached descendant whose
	// parent (the shim) is still running when the run is cancelled.
	//
	// As in the slow-descendant test above, the descendant sleeps 30s, far
	// longer than the poll, and writes its pid outside the scratch tree.
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid(1) is not installed on this host")
	}
	var seen OSLevelConfig
	pidFile := filepath.Join(t.TempDir(), "detached.pid")
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, `setsid sh -c 'echo $$ > "$0.tmp" && mv "$0.tmp" "$0"; sleep 30; mkdir -p "$HOME/recreated"; touch "$HOME/recreated/late"' '`+pidFile+`' </dev/null >/dev/null 2>&1 &
sleep 30`)
	cfg.MaxConcurrent = 1
	b := NewOSLevelBackend(cfg)
	captureContainmentCfg(t, &seen)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		res RunResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := b.Run(ctx, RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
		done <- outcome{res, err}
	}()

	var pid int
	require.Eventually(t, func() bool {
		raw, readErr := os.ReadFile(pidFile)
		if readErr != nil {
			return false
		}
		pid, readErr = strconv.Atoi(strings.TrimSpace(string(raw)))
		return readErr == nil
	}, 10*time.Second, 20*time.Millisecond, "the detached descendant never recorded its pid")
	t.Cleanup(func() {
		if p, findErr := os.FindProcess(pid); findErr == nil {
			_ = p.Kill()
		}
	})
	// Prove the escape the test is about: setsid made the descendant the leader
	// of its own group, so kill(-leaderPid) alone cannot reach it.
	pgid, err := syscall.Getpgid(pid)
	require.NoError(t, err)
	require.Equal(t, pid, pgid, "the descendant must have left the spawned process group")

	cancel()
	var out outcome
	select {
	case out = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	require.NoError(t, out.err)
	require.True(t, out.res.TimedOut, "a cancelled run folds into the timeout outcome")
	require.NotEmpty(t, seen.ScratchDir)
	t.Cleanup(func() { _ = os.RemoveAll(seen.ScratchDir) })

	require.Eventually(t, func() bool {
		p, findErr := os.FindProcess(pid)
		if findErr != nil {
			return true
		}
		return errors.Is(p.Signal(syscall.Signal(0)), os.ErrProcessDone)
	}, 2*time.Second, 20*time.Millisecond,
		"descendant (pid %d) that detached with setsid must be killed when the run is cancelled", pid)
	assert.NoDirExists(t, seen.ScratchDir,
		"a cancelled run must not leave a descendant able to recreate the scratch tree")
}
