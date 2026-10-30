//go:build unix

package sandbox

// Unix-only tests for the cancel path's descendant kill (epic 74 T2). They use
// syscall.Getpgid and the unix-only helpers in oslevel_unix.go, which do not
// exist on Windows; the //go:build unix tag keeps `GOOS=windows go vet` clean.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/log"
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
	parents, err := procParents(context.Background())
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
	parents, err := psParents(context.Background())
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
	// The cleanup's group kill reaches the grandchild only if the leader leads
	// its own group; without Setpgid it shares the test binary's group and
	// kill(-leaderPid) hits nothing.
	pgid, err := syscall.Getpgid(grandchild)
	require.NoError(t, err)
	require.Equal(t, cmd.Process.Pid, pgid, "the grandchild must be in the leader's process group")

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
	sandboxProcessTable = func(context.Context) (map[int]int, error) {
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
	sandboxProcessTable = func(context.Context) (map[int]int, error) {
		_ = cmd.Wait() // the leader exits and is reaped mid-snapshot
		return map[int]int{unrelated: cmd.Process.Pid}, nil
	}
	t.Cleanup(func() { sandboxProcessTable = orig })
	kills := recordSandboxKills(t)

	_ = cmd.Cancel()
	assert.NotContains(t, *kills, unrelated)
}

func TestKillSandboxDescendants_SkipsAPidWhoseGroupCannotBeRead(t *testing.T) {
	// A snapshotted descendant that has exited and been waited on fails
	// Getpgid with ESRCH, and its pid may already belong to an unrelated
	// process. It must not be signalled. Getpgid is faked so no real pid has
	// to stay free, and the kill is recorded without being delivered.
	const gone, live, leader = 1 << 30, 1<<30 + 1, 1<<30 + 2
	origGetpgid, origKill := sandboxGetpgid, sandboxKill
	sandboxGetpgid = func(pid int) (int, error) {
		if pid == gone {
			return 0, syscall.ESRCH
		}
		return pid, nil // a detached descendant leads its own group
	}
	var kills []int
	sandboxKill = func(pid int, _ syscall.Signal) error {
		kills = append(kills, pid)
		return nil
	}
	t.Cleanup(func() { sandboxGetpgid, sandboxKill = origGetpgid, origKill })

	killSandboxDescendants(leader, []int{gone, live})
	assert.Equal(t, []int{live}, kills, "only the pid whose group was read as outside the leader's is signalled")
}

func TestSandboxCancel_BeforeStartSignalsNothing(t *testing.T) {
	// Cancel before Start has no process: there is nothing to walk from or
	// kill, and it must not dereference a nil Process.
	kills := recordSandboxKills(t)
	cmd := exec.CommandContext(context.Background(), "true")
	configureSandboxProcessGroup(cmd)
	assert.NoError(t, cmd.Cancel())
	assert.Empty(t, *kills)
}

// recordSnapshotDegradations replaces the degraded-snapshot hook with one that
// records each reported error.
func recordSnapshotDegradations(t *testing.T) *[]error {
	t.Helper()
	var got []error
	orig := sandboxSnapshotDegraded
	sandboxSnapshotDegraded = func(err error) { got = append(got, err) }
	t.Cleanup(func() { sandboxSnapshotDegraded = orig })
	return &got
}

// startSandboxLeader starts a long-running leader under the sandbox cancel and
// reaps its group on cleanup.
func startSandboxLeader(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "sleep", "30")
	configureSandboxProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	return cmd
}

func TestSandboxCancel_ReportsAFailedSnapshotAndStillKillsTheGroup(t *testing.T) {
	// Epic 74 F2: when neither /proc nor ps can be read the cancel falls back
	// to the group-only kill. That fallback used to be silent; it must be
	// reported, and must not change what Cancel returns.
	cmd := startSandboxLeader(t)
	tableErr := errors.New("process table unreadable")
	orig := sandboxProcessTable
	sandboxProcessTable = func(context.Context) (map[int]int, error) { return nil, tableErr }
	t.Cleanup(func() { sandboxProcessTable = orig })
	reported := recordSnapshotDegradations(t)
	kills := recordSandboxKills(t)

	require.NoError(t, cmd.Cancel())
	assert.Contains(t, *kills, -cmd.Process.Pid, "the group kill must still run")
	require.Len(t, *reported, 1, "a failed snapshot must be reported")
	assert.ErrorIs(t, (*reported)[0], tableErr)
}

func TestSandboxCancel_ReportsAPartialSnapshotAndUsesWhatItRead(t *testing.T) {
	// A table read that stops early (its budget ran out, or entries could not
	// be read) is still a real snapshot of the descendants it did see. Those
	// are killed, and the gap is reported.
	cmd := startSandboxLeader(t)
	detached := startSleeper(t) // its own group, so the per-pid kill reaches it
	partialErr := errors.New("scan stopped early")
	orig := sandboxProcessTable
	sandboxProcessTable = func(context.Context) (map[int]int, error) {
		return map[int]int{detached: cmd.Process.Pid}, partialErr
	}
	t.Cleanup(func() { sandboxProcessTable = orig })
	reported := recordSnapshotDegradations(t)
	kills := recordSandboxKills(t)

	require.NoError(t, cmd.Cancel())
	assert.Contains(t, *kills, detached, "a descendant the partial snapshot found must be killed")
	assert.Contains(t, *kills, -cmd.Process.Pid)
	require.Len(t, *reported, 1, "a partial snapshot must be reported")
	assert.ErrorIs(t, (*reported)[0], partialErr)
}

func TestSandboxCancel_AHungSnapshotDelaysTheGroupKillOnlyByTheBudget(t *testing.T) {
	// Epic 74 F2: the workload keeps running while the snapshot is taken, so a
	// slow or hung table read (a ps that never returns) must not hold the group
	// kill back beyond the scan budget, even if the reader ignores its context.
	cmd := startSandboxLeader(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	orig := sandboxProcessTable
	sandboxProcessTable = func(context.Context) (map[int]int, error) {
		<-release
		return nil, nil
	}
	t.Cleanup(func() { sandboxProcessTable = orig })
	reported := recordSnapshotDegradations(t)
	kills := recordSandboxKills(t)

	start := time.Now()
	require.NoError(t, cmd.Cancel())
	elapsed := time.Since(start)

	assert.Contains(t, *kills, -cmd.Process.Pid, "the group kill must run once the budget is spent")
	assert.Less(t, elapsed, sandboxScanBudget+time.Second, "the scan held the kill back for %v", elapsed)
	require.Len(t, *reported, 1, "an abandoned snapshot must be reported")
	assert.ErrorIs(t, (*reported)[0], context.DeadlineExceeded)
}

func TestSandboxScanBudget_IsWellUnderTheOldTwoSecondBound(t *testing.T) {
	// The finding asked for a bound "well under the current 2s (e.g. 250ms)".
	assert.LessOrEqual(t, sandboxScanBudget, 250*time.Millisecond)
	assert.Positive(t, sandboxScanBudget)
}

func TestSandboxCancel_ACleanSnapshotReportsNothing(t *testing.T) {
	cmd := startSandboxLeader(t)
	reported := recordSnapshotDegradations(t)
	require.NoError(t, cmd.Cancel())
	assert.Empty(t, *reported)
}

func TestProcParents_StopsAndReportsWhenItsContextIsDone(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc on this host")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := procParents(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPSParents_HonoursItsContext(t *testing.T) {
	if _, err := os.Stat("/bin/ps"); err != nil {
		t.Skip("/bin/ps is not installed on this host")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := psParents(ctx)
	assert.Error(t, err)
}

// limitsReport is the fake sandbox tool body the resource-limit tests run: it
// prints its own pid, its process group, and the soft and hard memory limit
// (the host's sandboxMemoryUlimitFlag) it was started with, one per line, so a
// test can compare all four against what the parent configured.
var limitsReport = `echo "$$"
ps -o pgid= -p "$$"
ulimit -S ` + sandboxMemoryUlimitFlag + `
ulimit -H ` + sandboxMemoryUlimitFlag + `
`

// requireHostMemoryLimit skips a test that needs the wrapper to set a memory
// limit on a host whose kernel accepts none (darwin, see sandboxMemoryLimitFor).
func requireHostMemoryLimit(t *testing.T) {
	t.Helper()
	if !sandboxMemoryLimitEnforced {
		t.Skipf("%s accepts no memory rlimit, so the child runs unwrapped", runtime.GOOS)
	}
}

func parseLimitsReport(t *testing.T, out string) (pid, pgid int, soft, hard string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 4, "unexpected report: %q", out)
	var err error
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	require.NoError(t, err)
	pgid, err = strconv.Atoi(strings.TrimSpace(lines[1]))
	require.NoError(t, err)
	return pid, pgid, strings.TrimSpace(lines[2]), strings.TrimSpace(lines[3])
}

func TestApplySandboxResourceLimits_ChildStartsWithTheLimitAndKeepsItsPid(t *testing.T) {
	requireHostMemoryLimit(t)
	// Epic 35.24 T2: the cap is applied to the spawned child by a /bin/sh
	// wrapper that sets the rlimit and then execs the sandbox tool. exec keeps
	// the pid, so the process Go started IS the tool: the pid cmd.Process
	// reports, the group configureSandboxProcessGroup created, and the pid the
	// cancel path's descendant walk starts from all still name it.
	tool := writeFakeOSLevel(t, limitsReport)
	cfg := DefaultOSLevelConfig()
	cfg.MaxMemoryBytes = 300 * 1024 * 1024

	cmd := exec.CommandContext(context.Background(), tool)
	configureSandboxProcessGroup(cmd)
	applySandboxResourceLimits(cmd, cfg, nil)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Run(), "output: %s", out.String())

	pid, pgid, soft, hard := parseLimitsReport(t, out.String())
	assert.Equal(t, cmd.Process.Pid, pid, "the wrapper must exec the tool in place, not fork it")
	assert.Equal(t, pid, pgid, "the tool must still lead the process group Setpgid created")
	assert.Equal(t, "307200", soft, "soft data limit in KiB")
	assert.Equal(t, "307200", hard, "hard data limit in KiB, so the workload cannot raise it")
	assert.Equal(t, sandboxLimitShell, cmd.Path)
	assert.Equal(t, tool, cmd.Args[len(cmd.Args)-1], "the tool's own argv follows the wrapper unchanged")
}

func TestApplySandboxResourceLimits_PassesTheToolArgvThroughVerbatim(t *testing.T) {
	requireHostMemoryLimit(t)
	// The tool's arguments travel as "$@", never inside the -c string, so an
	// argument holding shell syntax reaches the tool as data.
	tool := writeFakeOSLevel(t, `for a in "$@"; do printf '%s\n' "$a"; done`)
	args := []string{"a b", "$(echo injected)", "'; exit 3; '", ""}
	cmd := exec.Command(tool, args...)
	applySandboxResourceLimits(cmd, DefaultOSLevelConfig(), nil)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "output: %s", out)
	assert.Equal(t, strings.Join(args, "\n")+"\n", string(out))
}

func TestApplySandboxResourceLimits_NeverLoosensAStricterInheritedLimit(t *testing.T) {
	requireHostMemoryLimit(t)
	// A data limit atcr itself already runs under is lower than the cap: the
	// child keeps the stricter one instead of the wrapper failing to raise it
	// (an unprivileged process cannot raise its hard limit) or raising the soft
	// one past what the operator's shell set.
	orig := sandboxGetrlimit
	t.Cleanup(func() { sandboxGetrlimit = orig })
	sandboxGetrlimit = func(resource int, lim *syscall.Rlimit) error {
		require.Equal(t, sandboxMemoryRlimit, resource)
		lim.Cur = 100 * 1024 * 1024
		lim.Max = 200 * 1024 * 1024
		return nil
	}
	assert.Equal(t, uint64(100*1024), sandboxMemoryLimitKiB(512*1024*1024))

	sandboxGetrlimit = func(int, *syscall.Rlimit) error { return errors.New("getrlimit failed") }
	assert.Equal(t, uint64(512*1024), sandboxMemoryLimitKiB(512*1024*1024),
		"an unreadable inherited limit leaves the configured cap; the wrapper fails closed if it cannot be set")
}

func TestSandboxMemoryLimitKiB_NeverRoundsToZero(t *testing.T) {
	// The KiB value rounds down, so the cap never exceeds the configured bytes,
	// except that `ulimit -d 0` would refuse the tool's first allocation: a
	// sub-KiB cap becomes the smallest settable limit instead.
	orig := sandboxGetrlimit
	t.Cleanup(func() { sandboxGetrlimit = orig })
	sandboxGetrlimit = func(int, *syscall.Rlimit) error { return errors.New("unused") }
	assert.Equal(t, uint64(1), sandboxMemoryLimitKiB(100))
	assert.Equal(t, uint64(1), sandboxMemoryLimitKiB(1024))
	assert.Equal(t, uint64(1), sandboxMemoryLimitKiB(2047))
	assert.Equal(t, uint64(2), sandboxMemoryLimitKiB(2048))
}

func TestSandboxLimitScript_IsIntegerOnlyAndExecsTheTool(t *testing.T) {
	requireHostMemoryLimit(t)
	s := sandboxLimitScript(524288)
	assert.Contains(t, s, "ulimit "+sandboxMemoryUlimitFlag+" 524288 ")
	assert.True(t, strings.HasSuffix(s, `exec "$@"`), "script: %s", s)
	assert.NotContains(t, s, "-u", "never RLIMIT_NPROC: it counts every process of the invoking user, not one run")
	assert.NotContains(t, s, "-p")
}

func TestSandboxLimitScript_AFailedLimitIsAToolFaultNotAWorkloadExit(t *testing.T) {
	requireHostMemoryLimit(t)
	// If the wrapper cannot set the limit it must not exec the tool unbounded,
	// and its exit must classify as a sandbox fault on both platforms rather
	// than be reported as the workload's verdict. The outer shell lowers the
	// hard limit below what the script asks for, which an unprivileged
	// process cannot raise back.
	if os.Geteuid() == 0 {
		t.Skip("root can raise a hard limit, so the set cannot be made to fail")
	}
	marker := filepath.Join(t.TempDir(), "ran")
	script := sandboxLimitScript(4096)
	cmd := exec.Command(sandboxLimitShell, "-c", `ulimit `+sandboxMemoryUlimitFlag+` 2048 && exec "$0" -c "$1" limits touch "$2"`,
		sandboxLimitShell, script, marker)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "output: %s", out)
	assert.Equal(t, sandboxLimitExitCode, ee.ExitCode())
	assert.NoFileExists(t, marker, "the tool must not run when its limit could not be set")
	for _, goos := range []string{"darwin", "linux"} {
		isFault, reason := classifyToolExit(goos, ee.ExitCode(), string(out))
		assert.True(t, isFault, goos)
		assert.Contains(t, reason, sandboxLimitDiagnosticPrefix, goos)
	}
}

func TestOSLevelBackendRun_StartsTheToolUnderTheMemoryCap(t *testing.T) {
	requireHostMemoryLimit(t)
	// The cap reaches the real spawn path, not only the helper.
	var seen OSLevelConfig
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, limitsReport)
	cfg.MaxMemoryBytes = 256 * 1024 * 1024
	b := NewOSLevelBackend(cfg)
	b.cgroupRoot = notACgroupRoot(t)
	captureContainmentCfg(t, &seen)
	res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)
	_, _, soft, hard := parseLimitsReport(t, res.Output)
	assert.Equal(t, "262144", soft)
	assert.Equal(t, "262144", hard)
}

func TestOSLevelBackendRun_AStructLiteralBackendGetsTheDefaultMemoryCap(t *testing.T) {
	requireHostMemoryLimit(t)
	// A backend built without NewOSLevelBackend has MaxMemoryBytes 0. It must
	// run under the default cap, not without one and not under a 1 KiB one
	// that would refuse the tool's first allocation.
	var seen OSLevelConfig
	b := &OSLevelBackend{cfg: OSLevelConfig{ToolPath: writeFakeOSLevel(t, limitsReport)}, cgroupRoot: notACgroupRoot(t)}
	captureContainmentCfg(t, &seen)
	res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)
	_, _, soft, hard := parseLimitsReport(t, res.Output)
	want := strconv.FormatInt(DefaultOSLevelConfig().MaxMemoryBytes/1024, 10)
	assert.Equal(t, want, soft)
	assert.Equal(t, want, hard)
}

func TestApplySandboxResourceLimits_LeavesAnUnexecutableToolUnwrapped(t *testing.T) {
	// A missing or non-executable sandbox binary must keep failing at Start,
	// with no process created, rather than become a shell's exit 127.
	notExec := filepath.Join(t.TempDir(), "not-executable")
	require.NoError(t, os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644))
	for _, tool := range []string{"/nonexistent/os-sandbox-binary-xyz", notExec, t.TempDir()} {
		cmd := exec.Command(tool, "arg")
		applySandboxResourceLimits(cmd, DefaultOSLevelConfig(), nil)
		assert.Equal(t, tool, cmd.Path, tool)
		assert.Equal(t, []string{tool, "arg"}, cmd.Args, tool)
	}
}

func TestSandboxLimitScript_AToolThatVanishedBeforeExecIsAFault(t *testing.T) {
	requireHostMemoryLimit(t)
	// The tool passed the pre-wrap check and was then removed: the shell's own
	// exec error names $0, so the run is classified as a fault, not as the
	// workload's exit 127.
	cmd := exec.Command(sandboxLimitShell, "-c", sandboxLimitScript(512*1024), sandboxLimitArgv0,
		"/nonexistent/os-sandbox-binary-xyz")
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "output: %s", out)
	for _, goos := range []string{"darwin", "linux"} {
		isFault, reason := classifyToolExit(goos, ee.ExitCode(), string(out))
		assert.True(t, isFault, "%s: exit %d output %q", goos, ee.ExitCode(), out)
		assert.Contains(t, reason, sandboxLimitDiagnosticPrefix, goos)
	}
}

func TestSandboxMemoryLimitFor_PicksTheRlimitEachKernelAccepts(t *testing.T) {
	// darwin rejects lowering both RLIMIT_DATA and RLIMIT_AS (EINVAL, measured
	// on integration-macos), so it gets no memory limit; linux uses
	// RLIMIT_DATA, which does not count the Go toolchain's reservations.
	_, _, enforced := sandboxMemoryLimitFor("darwin")
	assert.False(t, enforced)
	res, flag, enforced := sandboxMemoryLimitFor("linux")
	assert.True(t, enforced)
	assert.Equal(t, syscall.RLIMIT_DATA, res)
	assert.Equal(t, "-d", flag)
}

func TestApplySandboxResourceLimits_LeavesTheCommandAloneWhereNoLimitIsEnforced(t *testing.T) {
	// On a kernel that accepts no memory rlimit, wrapping would only make the
	// shell fail closed and refuse every run, so the spawn is left as it was.
	orig := sandboxMemoryLimitEnforced
	t.Cleanup(func() { sandboxMemoryLimitEnforced = orig })
	sandboxMemoryLimitEnforced = false
	tool := writeFakeOSLevel(t, "exit 0")
	cmd := exec.Command(tool, "arg")
	applySandboxResourceLimits(cmd, DefaultOSLevelConfig(), nil)
	assert.Equal(t, tool, cmd.Path)
	assert.Equal(t, []string{tool, "arg"}, cmd.Args)
}

// notACgroupRoot returns a directory with no cgroup.controllers file, so a
// backend given it as its cgroup root always takes the rlimit fallback. Tests
// of the rlimit path use it to stay on that path on a host that does delegate
// a cgroup to atcr.
func notACgroupRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// fakeCgroupRoot builds a directory shaped like a delegated cgroup v2 root:
// the controllers available, the ones enabled for its children, and the procs
// file a child joins through. Every file is a regular file, so the writes land
// where the test can read them back.
func fakeCgroupRoot(t *testing.T, subtreeControl string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu io memory pids\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte(subtreeControl+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cgroup.procs"), nil, 0o644))
	return root
}

// cgroupRemoval is what a fake rmdir saw: the cgroup directory and the files
// in it at the moment atcr removed it.
type cgroupRemoval struct {
	dir   string
	files map[string]string
}

// recordCgroupRemoval replaces sandboxRmdir for a fake root. A real cgroupfs
// directory is removed with rmdir while its interface files are still listed;
// a fake one holds regular files, so the stand-in records them and then
// removes the tree.
func recordCgroupRemoval(t *testing.T) *[]cgroupRemoval {
	t.Helper()
	var got []cgroupRemoval
	orig := sandboxRmdir
	t.Cleanup(func() { sandboxRmdir = orig })
	sandboxRmdir = func(dir string) error {
		files := map[string]string{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return err
			}
			files[e.Name()] = strings.TrimSpace(string(raw))
		}
		got = append(got, cgroupRemoval{dir: dir, files: files})
		return os.RemoveAll(dir)
	}
	return &got
}

func runWithLogs(t *testing.T, b *OSLevelBackend) (RunResult, string) {
	t.Helper()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))
	res, err := b.Run(ctx, RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err, "logs: %s", buf.String())
	return res, buf.String()
}

func TestOSLevelBackendRun_WritesTheCapsToAPerRunCgroupAndRemovesItAfterTheRun(t *testing.T) {
	// Epic 35.24 T3: where the host delegates a cgroup v2 subtree with the
	// memory and pids controllers enabled, each run gets its own child cgroup
	// carrying memory.max and pids.max, the tool joins it before it execs (so
	// every process it forks is counted), and the cgroup is gone once the run
	// has returned.
	root := fakeCgroupRoot(t, "memory pids")
	removed := recordCgroupRemoval(t)
	var seen OSLevelConfig
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, `echo "$$"`)
	cfg.MaxMemoryBytes = 300 * 1024 * 1024
	cfg.MaxProcs = 64
	b := NewOSLevelBackend(cfg)
	b.cgroupRoot = root
	captureContainmentCfg(t, &seen)

	res, logs := runWithLogs(t, b)
	require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)

	require.Len(t, *removed, 1, "the run's cgroup must be removed exactly once")
	rm := (*removed)[0]
	assert.Equal(t, root, filepath.Dir(rm.dir), "the run's cgroup is a child of the root")
	assert.True(t, strings.HasPrefix(filepath.Base(rm.dir), "atcr-run-"), rm.dir)
	assert.Equal(t, strconv.Itoa(300*1024*1024), rm.files["memory.max"])
	assert.Equal(t, "0", rm.files["memory.swap.max"], "the memory cap must not be served from swap")
	assert.Equal(t, "64", rm.files["pids.max"])
	assert.Equal(t, strings.TrimSpace(res.Output), rm.files["cgroup.procs"],
		"the tool itself, by its own pid, joined the cgroup before it ran")
	assert.Equal(t, "1", rm.files["cgroup.kill"], "whatever outlived the reap is killed before the rmdir")
	assert.NoDirExists(t, rm.dir)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "atcr-run-"), "left behind: %s", e.Name())
	}

	assert.Contains(t, logs, "mechanism=cgroup")
	assert.Contains(t, logs, "cgroup="+rm.dir)
}

func TestOSLevelBackendRun_TheCgroupPathReplacesTheRlimit(t *testing.T) {
	// Under a cgroup the memory cap is memory.max, not RLIMIT_DATA: a run that
	// carried both could be stopped by the rlimit, and nothing would show the
	// cgroup was the mechanism that enforced (T5 proves which one did).
	root := fakeCgroupRoot(t, "memory pids")
	recordCgroupRemoval(t)
	var seen OSLevelConfig
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, `ulimit -H -d`)
	b := NewOSLevelBackend(cfg)
	b.cgroupRoot = root
	captureContainmentCfg(t, &seen)

	inherited, err := exec.Command(sandboxLimitShell, "-c", "ulimit -H -d").Output()
	require.NoError(t, err)
	res, _ := runWithLogs(t, b)
	require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)
	assert.Equal(t, strings.TrimSpace(string(inherited)), strings.TrimSpace(res.Output),
		"the child inherits atcr's own data limit, no rlimit of its own")
}

func TestOSLevelBackendRun_FallsBackToTheRlimitAndSaysWhy(t *testing.T) {
	// A host that does not delegate both controllers gets T2's rlimit (or, on
	// a kernel that accepts none, no memory cap), and the run's log says which
	// mechanism applied and why the cgroup did not.
	cases := map[string]struct {
		root   func(t *testing.T) string
		reason string
	}{
		"pids not delegated":     {func(t *testing.T) string { return fakeCgroupRoot(t, "memory") }, "pids"},
		"memory not delegated":   {func(t *testing.T) string { return fakeCgroupRoot(t, "cpu pids") }, "memory"},
		"not a cgroup v2 root":   {notACgroupRoot, "cgroup.controllers"},
		"root does not exist at": {func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") }, "gone"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			removed := recordCgroupRemoval(t)
			root := tc.root(t)
			var seen OSLevelConfig
			cfg := DefaultOSLevelConfig()
			cfg.ToolPath = writeFakeOSLevel(t, "exit 0")
			b := NewOSLevelBackend(cfg)
			b.cgroupRoot = root
			captureContainmentCfg(t, &seen)

			res, logs := runWithLogs(t, b)
			require.Equal(t, 0, res.ExitCode, "output: %s", res.Output)
			assert.Empty(t, *removed, "no cgroup was created, so none is removed")
			want := "mechanism=rlimit"
			if !sandboxMemoryLimitEnforced {
				want = "mechanism=none"
			}
			assert.Contains(t, logs, want)
			assert.Contains(t, logs, "falling back")
			assert.Contains(t, logs, tc.reason)
			if entries, err := os.ReadDir(root); err == nil {
				for _, e := range entries {
					assert.False(t, strings.HasPrefix(e.Name(), "atcr-run-"), "created: %s", e.Name())
				}
			}
		})
	}
}

func TestOSLevelBackendRun_ACgroupThatCannotTakeItsLimitsIsRemovedAndFallsBack(t *testing.T) {
	// The cgroup was created but a limit could not be written: it must not be
	// used (a cgroup without its cap reads as protection and is not), it must
	// not be left behind, and the run falls back like any other refusal.
	root := fakeCgroupRoot(t, "memory pids")
	removed := recordCgroupRemoval(t)
	orig := sandboxCgroupWrite
	t.Cleanup(func() { sandboxCgroupWrite = orig })
	sandboxCgroupWrite = func(path, value string) error {
		if filepath.Base(path) == "pids.max" {
			return errors.New("pids.max: permission denied")
		}
		return orig(path, value)
	}
	var seen OSLevelConfig
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, "exit 0")
	b := NewOSLevelBackend(cfg)
	b.cgroupRoot = root
	captureContainmentCfg(t, &seen)

	_, logs := runWithLogs(t, b)
	require.Len(t, *removed, 1)
	assert.NoDirExists(t, (*removed)[0].dir)
	assert.Contains(t, logs, "falling back")
	assert.Contains(t, logs, "pids.max: permission denied")
}

func TestOSLevelBackendRun_AStructLiteralBackendGetsTheDefaultCapsInItsCgroup(t *testing.T) {
	// MaxMemoryBytes and MaxProcs are 0 on a backend NewOSLevelBackend never
	// floored. 0 in memory.max or pids.max would refuse the tool outright, so
	// the cgroup gets the defaults instead.
	root := fakeCgroupRoot(t, "memory pids")
	removed := recordCgroupRemoval(t)
	var seen OSLevelConfig
	b := &OSLevelBackend{cfg: OSLevelConfig{ToolPath: writeFakeOSLevel(t, "exit 0")}, cgroupRoot: root}
	captureContainmentCfg(t, &seen)
	runWithLogs(t, b)
	require.Len(t, *removed, 1)
	def := DefaultOSLevelConfig()
	assert.Equal(t, strconv.FormatInt(def.MaxMemoryBytes, 10), (*removed)[0].files["memory.max"])
	assert.Equal(t, strconv.Itoa(def.MaxProcs), (*removed)[0].files["pids.max"])
}

func TestSandboxCgroupScript_AJoinThatFailsIsAToolFaultNotAWorkloadExit(t *testing.T) {
	// If the wrapper cannot move itself into the run's cgroup it must not exec
	// the tool uncapped, and the exit must classify as a sandbox fault.
	marker := filepath.Join(t.TempDir(), "ran")
	cmd := exec.Command("/usr/bin/touch", marker)
	applySandboxResourceLimits(cmd, DefaultOSLevelConfig(),
		&sandboxCgroup{dir: filepath.Join(t.TempDir(), "missing", "atcr-run-x")})
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "output: %s", out)
	assert.Equal(t, sandboxLimitExitCode, ee.ExitCode())
	assert.NoFileExists(t, marker, "the tool must not run outside its cgroup")
	for _, goos := range []string{"darwin", "linux"} {
		isFault, reason := classifyToolExit(goos, ee.ExitCode(), string(out))
		assert.True(t, isFault, goos)
		assert.Contains(t, reason, sandboxLimitDiagnosticPrefix, goos)
	}
}

func TestSandboxCgroupScript_PassesTheToolArgvThroughVerbatim(t *testing.T) {
	// The procs path and the tool's argv travel as positional parameters, never
	// inside the -c string, so neither is parsed as shell syntax.
	dir := filepath.Join(t.TempDir(), "atcr-run-$(echo x)")
	require.NoError(t, os.Mkdir(dir, 0o755))
	tool := writeFakeOSLevel(t, `for a in "$@"; do printf '%s\n' "$a"; done`)
	args := []string{"a b", "$(echo injected)", "'; exit 3; '", ""}
	cmd := exec.Command(tool, args...)
	applySandboxResourceLimits(cmd, DefaultOSLevelConfig(), &sandboxCgroup{dir: dir})
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "output: %s", out)
	assert.Equal(t, strings.Join(args, "\n")+"\n", string(out))
	assert.FileExists(t, filepath.Join(dir, "cgroup.procs"))
}

func TestSandboxCgroupRemove_WaitsOutABusyCgroupWithinItsBudget(t *testing.T) {
	// rmdir answers EBUSY while a killed process has not finished exiting; the
	// removal retries until it clears, and gives up with the error after its
	// budget rather than holding the run open.
	origRmdir, origBudget := sandboxRmdir, sandboxCgroupRemoveBudget
	t.Cleanup(func() { sandboxRmdir, sandboxCgroupRemoveBudget = origRmdir, origBudget })
	cg := &sandboxCgroup{dir: t.TempDir()}

	calls := 0
	sandboxRmdir = func(string) error {
		calls++
		if calls < 3 {
			return syscall.EBUSY
		}
		return nil
	}
	require.NoError(t, cg.remove())
	assert.Equal(t, 3, calls)

	sandboxCgroupRemoveBudget = 50 * time.Millisecond
	sandboxRmdir = func(string) error { return syscall.EBUSY }
	start := time.Now()
	err := cg.remove()
	assert.ErrorIs(t, err, syscall.EBUSY)
	assert.Less(t, time.Since(start), time.Second)

	calls = 0
	sandboxRmdir = func(string) error { calls++; return syscall.EACCES }
	assert.ErrorIs(t, cg.remove(), syscall.EACCES)
	assert.Equal(t, 1, calls, "only EBUSY is worth waiting out")
}

func TestOSLevelBackendRun_AFailedCgroupRemovalIsLoggedNotFatal(t *testing.T) {
	root := fakeCgroupRoot(t, "memory pids")
	origRmdir, origBudget := sandboxRmdir, sandboxCgroupRemoveBudget
	t.Cleanup(func() { sandboxRmdir, sandboxCgroupRemoveBudget = origRmdir, origBudget })
	sandboxCgroupRemoveBudget = 10 * time.Millisecond
	sandboxRmdir = func(string) error { return syscall.EBUSY }
	var seen OSLevelConfig
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, "exit 0")
	b := NewOSLevelBackend(cfg)
	b.cgroupRoot = root
	captureContainmentCfg(t, &seen)
	res, logs := runWithLogs(t, b)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, logs, "cgroup removal")
	assert.Contains(t, logs, syscall.EBUSY.Error(), "the logged error is the rmdir's own (its text differs per kernel)")
}

func TestProbeSandboxCgroup_CreatesAndRemovesARealRunCgroup(t *testing.T) {
	// Preflight's PID-cap capability is the same newSandboxCgroup Run uses, not
	// a separate guess, and the probe leaves nothing behind.
	root := fakeCgroupRoot(t, "memory pids")
	removed := recordCgroupRemoval(t)
	b := &OSLevelBackend{cfg: DefaultOSLevelConfig(), cgroupRoot: root}

	require.NoError(t, b.probeSandboxCgroup())
	require.Len(t, *removed, 1)
	assert.Equal(t, "256", (*removed)[0].files["pids.max"])
	assert.NoDirExists(t, (*removed)[0].dir)
}

func TestProbeSandboxCgroup_ReportsARootThatCannotHostARun(t *testing.T) {
	for name, root := range map[string]string{
		"not a cgroup":         notACgroupRoot(t),
		"pids not delegated":   fakeCgroupRoot(t, "memory"),
		"memory not delegated": fakeCgroupRoot(t, "pids"),
	} {
		t.Run(name, func(t *testing.T) {
			b := &OSLevelBackend{cfg: DefaultOSLevelConfig(), cgroupRoot: root}
			assert.Error(t, b.probeSandboxCgroup())
		})
	}
}

func TestParseCgroupV2Path(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"unified only":     {"0::/user.slice/user-1000.slice/session-2.scope\n", "/user.slice/user-1000.slice/session-2.scope", true},
		"namespace root":   {"0::/\n", "/", true},
		"hybrid hierarchy": {"12:pids:/user.slice\n1:name=systemd:/x\n0::/user.slice/a.scope\n", "/user.slice/a.scope", true},
		"v1 only":          {"12:pids:/user.slice\n1:name=systemd:/x\n", "", false},
		"empty":            {"", "", false},
		"relative":         {"0::user.slice\n", "", false},
	}
	for name, tc := range cases {
		got, ok := parseCgroupV2Path([]byte(tc.in))
		assert.Equal(t, tc.ok, ok, name)
		assert.Equal(t, tc.want, got, name)
	}
}

func TestDiscoverSandboxCgroupRoot_IsLinuxOnly(t *testing.T) {
	root, err := discoverSandboxCgroupRoot("darwin")
	assert.Error(t, err)
	assert.Empty(t, root)
	if runtime.GOOS == "linux" {
		raw, readErr := os.ReadFile("/proc/self/cgroup")
		require.NoError(t, readErr)
		rel, ok := parseCgroupV2Path(raw)
		if !ok {
			t.Skip("host has no cgroup v2 entry")
		}
		root, err = discoverSandboxCgroupRoot("linux")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(sandboxCgroupMount, rel), root)
	}
}
