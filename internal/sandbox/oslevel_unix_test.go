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
	"slices"
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

// startSleeper starts a `sleep 30` child of the test process and kills it on
// cleanup, so a process-table reader has a known pid -> ppid pair to find.
func startSleeper(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func TestProcParents_FindsAChildOfThisProcess(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc on this host")
	}
	pid := startSleeper(t)
	parents, err := procParents()
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), parents[pid])
}

func TestPSParents_FindsAChildOfThisProcess(t *testing.T) {
	// The darwin/BSD path. It runs on Linux CI too wherever /bin/ps exists, so
	// the fallback is exercised by CI and not only on a developer's Mac.
	if _, err := os.Stat("/bin/ps"); err != nil {
		t.Skip("/bin/ps is not installed on this host")
	}
	pid := startSleeper(t)
	parents, err := psParents()
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), parents[pid])
}

func TestSandboxDescendants_FindsGrandchildrenAndNotTheLeader(t *testing.T) {
	// The leader backgrounds a grandchild and records its pid; the snapshot
	// must reach it through the intermediate shell.
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	cmd := exec.Command("sh", "-c", `sleep 30 & echo $! > "$0.tmp" && mv "$0.tmp" "$0"; wait`, pidFile)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	var grandchild int
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		grandchild, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	got := sandboxDescendants(cmd)
	assert.Contains(t, got, grandchild)
	assert.NotContains(t, got, cmd.Process.Pid, "the leader is killed with its group, not listed")
}

func TestSandboxDescendants_UnstartedCommandHasNone(t *testing.T) {
	assert.Nil(t, sandboxDescendants(exec.Command("true")))
}

func TestDescendantsOf_WalksTheTreeAndExcludesRoot(t *testing.T) {
	parents := map[int]int{
		1:  0,
		10: 1,
		11: 10, // child of the root
		12: 11, // grandchild, e.g. one that called setsid
		13: 12,
		20: 1, // unrelated sibling of the root
		21: 20,
	}
	got := descendantsOf(10, parents)
	assert.ElementsMatch(t, []int{11, 12, 13}, got)
	assert.Empty(t, descendantsOf(13, parents))
}

func TestDescendantsOf_TerminatesOnACycle(t *testing.T) {
	// A torn snapshot (a pid reused between two reads) could in principle show
	// a parent loop; the walk must still terminate.
	got := descendantsOf(1, map[int]int{2: 1, 3: 2, 1: 3})
	assert.ElementsMatch(t, []int{2, 3}, got)
}

func TestParseProcStatPPID(t *testing.T) {
	cases := []struct {
		name string
		stat string
		want int
		ok   bool
	}{
		{"plain", "123 (sleep) S 45 123 123 0 -1", 45, true},
		{"comm with spaces and parens", "123 (a) b (c) R 67 1 1", 67, true},
		{"no closing paren", "123 sleep S 45", 0, false},
		{"truncated after comm", "123 (sleep) S", 0, false},
		{"non-numeric ppid", "123 (sleep) S x", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProcStatPPID([]byte(tc.stat))
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParsePSParents(t *testing.T) {
	got, err := parsePSParents([]byte("    1     0\n  412     1\n\ngarbage line here\n  x 1\n  500   412\n"))
	require.NoError(t, err)
	assert.Equal(t, map[int]int{1: 0, 412: 1, 500: 412}, got)

	_, err = parsePSParents([]byte("PID PPID\n"))
	assert.Error(t, err, "output with no pid/ppid pairs must not read as an empty process table")
}

func TestKillSandboxDescendants_KillsEachListedPid(t *testing.T) {
	// No "already gone" pid is listed alongside: a reaped pid can be reused by
	// an unrelated process on the host, which this test would then SIGKILL.
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	killSandboxDescendants(0, []int{cmd.Process.Pid}) // no process group has id 0

	select {
	case err := <-done:
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, syscall.SIGKILL, exitErr.Sys().(syscall.WaitStatus).Signal())
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the listed pid was not killed")
	}
}

// recordSandboxKills wraps sandboxKill so a test sees every pid the cancel path
// signals, in order. The signal is still delivered.
func recordSandboxKills(t *testing.T) *[]int {
	t.Helper()
	var got []int
	orig := sandboxKill
	sandboxKill = func(pid int, sig syscall.Signal) error {
		got = append(got, pid)
		return orig(pid, sig)
	}
	t.Cleanup(func() { sandboxKill = orig })
	return &got
}

// readPidFile waits for a pid a test script writes (via a rename, so a partial
// write is never read) and kills it on cleanup.
func readPidFile(t *testing.T, path string) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "%s was never written", path)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

func TestSandboxCancel_SignalsDetachedDescendantsBeforeTheGroupAndSkipsGroupMembers(t *testing.T) {
	// Epic 74 F1: the group kill used to run first. It kills the leader and
	// every in-group descendant at once, and with nothing left to hold them as
	// zombies their pids are free; the per-pid kill that followed could then
	// hit a recycled pid. A descendant outside the group must be signalled
	// while the group is still alive, and one inside it must be left to the
	// group kill.
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid(1) is not installed on this host")
	}
	dir := t.TempDir()
	cmd := exec.CommandContext(context.Background(), "sh", "-c",
		`sleep 30 & echo $! > "$0/group.tmp" && mv "$0/group.tmp" "$0/group"
setsid sh -c 'echo $$ > "$0/detached.tmp" && mv "$0/detached.tmp" "$0/detached"; exec sleep 30' "$0" </dev/null >/dev/null 2>&1 &
wait`, dir)
	configureSandboxProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	inGroup := readPidFile(t, filepath.Join(dir, "group"))
	detached := readPidFile(t, filepath.Join(dir, "detached"))
	pgid, err := syscall.Getpgid(detached)
	require.NoError(t, err)
	require.Equal(t, detached, pgid, "the descendant must have left the spawned process group")

	kills := recordSandboxKills(t)
	require.NoError(t, cmd.Cancel())

	groupAt := slices.Index(*kills, -cmd.Process.Pid)
	detachedAt := slices.Index(*kills, detached)
	require.NotEqual(t, -1, groupAt, "the group must be killed: %v", *kills)
	require.NotEqual(t, -1, detachedAt, "the detached descendant must be killed: %v", *kills)
	assert.Less(t, detachedAt, groupAt, "the detached descendant must be signalled before the group kill: %v", *kills)
	assert.NotContains(t, *kills, inGroup, "an in-group descendant is the group kill's, not signalled by pid")
}

func TestSandboxCancel_SkipsTheDescendantReapWhenTheLeaderIsAlreadyReaped(t *testing.T) {
	// Epic 74 F1: cmd.Cancel can race Wait reaping the leader. The walk from a
	// reaped leader's pid then reads whatever process now holds that pid, so
	// the descendant reap must be skipped. The process table here claims an
	// unrelated live process (standing in for a recycled pid's child) is the
	// leader's child; it must not be signalled.
	unrelated := startSleeper(t)
	cmd := exec.CommandContext(context.Background(), "true")
	configureSandboxProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Wait())

	walked := false
	orig := sandboxProcessTable
	sandboxProcessTable = func() (map[int]int, error) {
		walked = true
		return map[int]int{unrelated: cmd.Process.Pid}, nil
	}
	t.Cleanup(func() { sandboxProcessTable = orig })
	kills := recordSandboxKills(t)

	assert.ErrorIs(t, cmd.Cancel(), os.ErrProcessDone)
	assert.False(t, walked, "a reaped leader must not be walked from")
	assert.NotContains(t, *kills, unrelated)
}

func TestSandboxCancel_DiscardsASnapshotTakenWhileTheLeaderWasReaped(t *testing.T) {
	// The leader can be reaped while the table is being read, after the first
	// liveness check. A snapshot is trusted only if the leader is still
	// unreaped once it has been taken: until then no process can hold its pid.
	unrelated := startSleeper(t)
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
	configureSandboxProcessGroup(cmd)
	require.NoError(t, cmd.Start())

	orig := sandboxProcessTable
	sandboxProcessTable = func() (map[int]int, error) {
		_ = cmd.Wait() // the leader exits and is reaped mid-snapshot
		return map[int]int{unrelated: cmd.Process.Pid}, nil
	}
	t.Cleanup(func() { sandboxProcessTable = orig })
	kills := recordSandboxKills(t)

	_ = cmd.Cancel()
	assert.NotContains(t, *kills, unrelated)
}
