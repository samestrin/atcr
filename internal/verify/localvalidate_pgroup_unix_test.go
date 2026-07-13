//go:build unix

package verify

// Unix-only regression tests for process-group reaping on validation timeout
// (epic 22.0). exec.CommandContext SIGKILLs only the DIRECT child on timeout, so
// a validation command such as `sh -c "... sleep 100"` leaves the grandchild
// sleep orphaned and still running past the deadline. These tests assert that the
// entire process group is reaped. Fixtures use real POSIX shell/sleep, matching
// localvalidate_test.go conventions (no exec mocking). The //go:build unix tag
// keeps them off Windows, where process-group semantics are out of scope.

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// processAlive reports whether pid names a live process. Signal 0 probes for
// existence without delivering a signal: nil means the process exists, ESRCH
// means it has been reaped.
func processAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) != syscall.ESRCH
}

// TestRunConfiguredValidation_TimeoutReapsGrandchild is the epic's core guarantee:
// when a timed-out validation command has spawned a grandchild, the group SIGKILL
// reaps the grandchild too — it is not left orphaned. The fixture backgrounds a
// long sleep, prints its PID to stdout (captured before the deadline), then waits,
// forcing a real grandchild rather than letting sh exec-optimize into the sleep.
func TestRunConfiguredValidation_TimeoutReapsGrandchild(t *testing.T) {
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", "sleep 100 & echo $! ; wait"}, t.TempDir(), 250*time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.TimedOut, "run must have timed out")

	pidStr := strings.TrimSpace(res.Stdout)
	require.NotEmpty(t, pidStr, "fixture must have printed the grandchild PID before the deadline")
	pid, convErr := strconv.Atoi(pidStr)
	require.NoError(t, convErr, "stdout must be a numeric PID, got %q", pidStr)

	require.Eventually(t, func() bool {
		return !processAlive(pid)
	}, 5*time.Second, 20*time.Millisecond,
		"grandchild sleep (pid %d) must be reaped with the process group, not orphaned past the deadline", pid)
}

// TestRunConfiguredValidation_TimeoutReapsWholeGroup asserts the cancel/timeout
// path SIGKILLs the entire group — both the leader (the shell, $$) and the
// grandchild ($!) — not just one of them. The fixture prints the leader PID on
// the first stdout line and the grandchild PID on the second before waiting.
func TestRunConfiguredValidation_TimeoutReapsWholeGroup(t *testing.T) {
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", "echo $$ ; sleep 100 & echo $! ; wait"}, t.TempDir(), 250*time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.TimedOut, "run must have timed out")

	lines := strings.Fields(strings.TrimSpace(res.Stdout))
	require.Len(t, lines, 2, "fixture must print leader PID and grandchild PID, got %q", res.Stdout)
	leaderPID, convErr := strconv.Atoi(lines[0])
	require.NoError(t, convErr)
	grandchildPID, convErr := strconv.Atoi(lines[1])
	require.NoError(t, convErr)

	require.Eventually(t, func() bool {
		return !processAlive(leaderPID) && !processAlive(grandchildPID)
	}, 5*time.Second, 20*time.Millisecond,
		"both group leader (pid %d) and grandchild (pid %d) must be reaped on the cancel/timeout path", leaderPID, grandchildPID)
}

// TestRunConfiguredValidation_SpawningCommandPassesUnaffected guards AC2: placing
// the command in its own group and overriding cancel must NOT affect a run that
// spawns a subprocess but exits cleanly within the timeout. Cancel fires only on
// a done context, so this happy-path run still passes.
func TestRunConfiguredValidation_SpawningCommandPassesUnaffected(t *testing.T) {
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", "sleep 0.1 & wait"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	require.False(t, res.TimedOut, "a command that exits within the timeout must not be marked timed out")
	require.True(t, res.Passed(), "a clean exit-0 run that spawned a subprocess must still pass the gate")
}
