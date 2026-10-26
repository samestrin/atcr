//go:build unix

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// configureSandboxProcessGroup puts the sandboxed command in its own process
// group and replaces exec.CommandContext's default cancel so a timeout SIGKILLs
// the entire group rather than only the direct child. A workload that forked (a
// build spawning a compiler, a test spawning a server) is therefore reaped
// instead of outliving its deadline — the leak DockerBackend closes by killing
// its container outright (docker.go:305-311).
//
// This mirrors configureProcessGroup in internal/verify/localvalidate_pgroup_unix.go;
// the two are deliberately separate because that one is unexported in a
// different package, and duplicating ~15 lines is cheaper than exporting a
// process-control helper across a package boundary.
//
// The unix/!unix split exists because syscall.SysProcAttr has no Setpgid field
// on Windows and syscall.Kill does not exist there — a single file referencing
// either would break `GOOS=windows go build`, which .goreleaser.yaml releases
// and ubuntu-only CI never checks. The pair is exhaustive by construction, so
// no GOOS can fall through a gap an explicit darwin/linux/other list could leave.
//
// The cancel also kills the leader's descendants that left the group with
// setsid/setpgid (see killSandboxDescendants): the group kill alone cannot reach
// them, and on darwin no PID namespace kills them afterwards.
func configureSandboxProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Snapshot and signal the detached descendants BEFORE the group kill:
		// once the leader dies they are reparented and no parent link leads to
		// them any more, and the group kill frees the pids of every member.
		if cmd.Process != nil {
			killSandboxDescendants(cmd.Process.Pid, sandboxDescendants(cmd))
		}
		err := killSandboxProcessGroup(cmd)
		if errors.Is(err, syscall.ESRCH) {
			// The group is already gone — the ordinary already-exited race, not
			// a failure. Report ProcessDone so exec.Wait ignores it, matching
			// the default cancel's behavior.
			return os.ErrProcessDone
		}
		return err
	}
}

// sandboxLimitShell runs the rlimit wrapper. The path is absolute so atcr's
// own PATH cannot substitute another binary, as for psParents' /bin/ps.
const sandboxLimitShell = "/bin/sh"

// sandboxLimitArgv0 is the wrapper's $0. Every shell prefixes its own errors
// (a failed exec included) with "$0:", which is sandboxLimitDiagnosticPrefix.
const sandboxLimitArgv0 = "atcr-sandbox-limits"

// sandboxMemoryRlimit is the rlimit MaxMemoryBytes sets on this host, and
// sandboxMemoryUlimitFlag the ulimit option that sets it (KiB, in every
// /bin/sh atcr meets: dash, bash, zsh and busybox ash).
// sandboxMemoryLimitEnforced is false where the host kernel accepts no memory
// rlimit at all, and the child then runs without one. All three follow the
// host kernel, runtime.GOOS, not b.platform(): setrlimit is answered by the
// kernel atcr is running on.
var sandboxMemoryRlimit, sandboxMemoryUlimitFlag, sandboxMemoryLimitEnforced = sandboxMemoryLimitFor(runtime.GOOS)

// sandboxMemoryLimitFor picks the memory rlimit per kernel.
//
// linux: RLIMIT_DATA. Address-space limits count reservations, and the Go
// toolchain reserves far more than it commits, so an RLIMIT_AS at the 512 MiB
// default can fail an ordinary `go build` (the plan's Risks). RLIMIT_DATA has
// counted private writable mappings, anonymous mmap included, since 4.7.
//
// darwin: none. This is the plan's gated outcome, measured on the
// integration-macos job (macos-latest, PR #603): XNU rejects lowering either
// limit with EINVAL, "ulimit: data seg size: cannot modify limit: Invalid
// argument" for RLIMIT_DATA and "ulimit: virtual memory: cannot modify limit:
// Invalid argument" for RLIMIT_AS. A wrapper that set either would fail
// closed and refuse every darwin run, so darwin leaves memory unenforced and
// Preflight refuses an operator-set memory cap there (epic 35.24 T4).
func sandboxMemoryLimitFor(goos string) (resource int, ulimitFlag string, enforced bool) {
	if goos == "darwin" {
		return 0, "", false
	}
	return syscall.RLIMIT_DATA, "-d", true
}

// sandboxGetrlimit is syscall.Getrlimit; tests replace it to present an
// inherited limit the host cannot be made to have.
var sandboxGetrlimit = syscall.Getrlimit

// applySandboxResourceLimits rewrites cmd to start under the memory cap:
//
//	/bin/sh -c 'ulimit -d <KiB> || ...; exec "$@"' atcr-sandbox-limits <tool> <args...>
//
// syscall.SysProcAttr has no rlimit field, and setting the limit on atcr itself
// around the spawn would leak into every concurrent run, so a shell sets it in
// the child and then execs the sandbox tool. exec keeps the pid: the process
// cmd.Process names, the group configureSandboxProcessGroup creates and the
// root the cancel path's descendant walk starts from are all still the tool.
// The wrapper sits OUTSIDE sandbox-exec/bwrap, so the containment profile and
// argv are unchanged, and the limit is inherited by everything inside.
//
// Only the memory cap is applied, and only where sandboxMemoryLimitEnforced:
// on darwin cmd is left unchanged. MaxProcs is never set as
// RLIMIT_NPROC: that limit counts every process (on linux, every thread) of
// the invoking user across the host, not one run's tree, so a cap of 256
// refuses every fork for a user whose desktop already runs that many, and it
// bounds nothing per run. The board ruled it out on darwin (EST-1340, Q2); on
// linux the PID cap is cgroup pids.max (epic 35.24 T3).
//
// The limit is an integer formatted by atcr, never operator text, and the
// tool's argv travels as "$@" rather than inside the -c string, so nothing
// here is parsed as shell syntax. If the wrapper cannot set the limit it does
// not exec the tool: it exits sandboxLimitExitCode with a
// sandboxLimitDiagnosticPrefix line, which classifyToolExit reports as a
// sandbox fault rather than as the workload's exit.
//
// A non-positive MaxMemoryBytes (a struct-literal backend NewOSLevelBackend
// never floored) gets the default cap, never none and never a 1 KiB one.
//
// A tool that cannot be executed is left unwrapped, so cmd.Start fails with
// the original error and no process is created at all, exactly as before the
// wrapper existed. Wrapped, the shell would instead start and exit 127, and a
// missing sandbox binary would read as the workload's exit. If the tool
// vanishes between this check and the exec, the shell's own error names $0,
// which carries sandboxLimitDiagnosticPrefix, so the run is still a fault.
//
// With a non-nil cg (linux, epic 35.24 T3) the wrapper joins that cgroup
// instead of setting the rlimit: it writes its own pid to cg's cgroup.procs
// and then execs, so the tool and everything it forks are counted by the
// cgroup's memory.max and pids.max from their first instruction. The rlimit
// is not set as well, so the cgroup is the one mechanism that enforces and
// T5's integration test can tell which one did. The procs path travels as a
// positional parameter, like the tool's argv, and a failed join exits
// sandboxLimitExitCode without running the tool, as a failed ulimit does.
func applySandboxResourceLimits(cmd *exec.Cmd, cfg OSLevelConfig, cg *sandboxCgroup) {
	if (cg == nil && !sandboxMemoryLimitEnforced) || cmd.Err != nil {
		return
	}
	if _, err := exec.LookPath(cmd.Path); err != nil {
		return
	}
	var args []string
	if cg != nil {
		args = []string{sandboxLimitShell, "-c", sandboxCgroupScript(), sandboxLimitArgv0, cg.procsPath(), cmd.Path}
	} else {
		maxBytes := cfg.MaxMemoryBytes
		if maxBytes <= 0 {
			maxBytes = DefaultOSLevelConfig().MaxMemoryBytes
		}
		args = []string{sandboxLimitShell, "-c", sandboxLimitScript(sandboxMemoryLimitKiB(maxBytes)), sandboxLimitArgv0, cmd.Path}
	}
	cmd.Path = sandboxLimitShell
	cmd.Args = append(args, cmd.Args[1:]...)
}

// sandboxMemoryLimitKiB converts the configured cap to the KiB ulimit takes,
// rounding down so the cap never exceeds it, with a floor of 1 because
// `ulimit -d 0` would refuse the tool's first allocation. A lower limit atcr
// already runs under wins: the child would inherit it anyway, and raising it
// would either fail (a hard limit) or loosen what the operator's shell set (a
// soft one). An unreadable inherited limit leaves the configured cap; if that
// is above the hard limit the wrapper fails closed.
func sandboxMemoryLimitKiB(maxBytes int64) uint64 {
	limit := uint64(maxBytes)
	var cur syscall.Rlimit
	if err := sandboxGetrlimit(sandboxMemoryRlimit, &cur); err == nil && uint64(cur.Cur) < limit {
		limit = uint64(cur.Cur)
	}
	return max(limit/1024, 1)
}

// sandboxLimitScript is the wrapper's -c program for a memory cap of kib KiB.
// Neither -H nor -S is given, so every shell sets both: the workload cannot
// raise the soft limit back up to an unbounded hard one.
func sandboxLimitScript(kib uint64) string {
	return fmt.Sprintf(`ulimit %s %d || { echo "%s cannot set the memory limit (%s %d KiB)" >&2; exit %d; }; exec "$@"`,
		sandboxMemoryUlimitFlag, kib, sandboxLimitDiagnosticPrefix, sandboxMemoryUlimitFlag, kib, sandboxLimitExitCode)
}

// sandboxCgroupScript is the wrapper's -c program when the run has a cgroup:
// $1 is the cgroup's procs file, and the tool's argv follows it.
func sandboxCgroupScript() string {
	return fmt.Sprintf(`echo "$$" > "$1" || { echo "%s cannot join the run's cgroup ($1)" >&2; exit %d; }; shift; exec "$@"`,
		sandboxLimitDiagnosticPrefix, sandboxLimitExitCode)
}

// sandboxCgroupMount is where the cgroup v2 hierarchy is mounted; the path in
// /proc/self/cgroup is relative to it.
const sandboxCgroupMount = "/sys/fs/cgroup"

// sandboxCgroupControllers are the controllers a run's cgroup needs: memory
// for memory.max and pids for pids.max. Both must already be enabled in the
// root's cgroup.subtree_control. atcr does not enable them itself: that would
// change the host's cgroup tree beyond the run, and cgroup v2 refuses it
// anyway while the root holds a process, as a root holding atcr does.
var sandboxCgroupControllers = []string{"memory", "pids"}

// sandboxCgroup is one run's cgroup v2 directory, a child of the cgroup root.
type sandboxCgroup struct {
	dir string
}

func (cg *sandboxCgroup) procsPath() string { return filepath.Join(cg.dir, "cgroup.procs") }

// sandboxRmdir is syscall.Rmdir: a cgroup directory is removed with rmdir
// while its interface files are still listed, which os.RemoveAll cannot do.
// Tests replace it, because a fake cgroup root holds regular files.
var sandboxRmdir = syscall.Rmdir

// sandboxCgroupWrite writes one cgroup interface file. Tests replace it to
// make a single limit fail to apply.
var sandboxCgroupWrite = func(path, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}

// sandboxCgroupRemoveBudget bounds how long remove waits for a cgroup whose
// killed processes have not finished exiting. rmdir answers EBUSY until the
// last one is gone, which a SIGKILL makes a matter of milliseconds.
var sandboxCgroupRemoveBudget = time.Second

// discoverSandboxCgroupRoot finds the cgroup atcr itself runs in, the only one
// it can expect to have been delegated. goos is the host kernel: cgroups are
// linux-only.
func discoverSandboxCgroupRoot(goos string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("cgroup v2 is linux-only, not %s", goos)
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	rel, ok := parseCgroupV2Path(raw)
	if !ok {
		return "", errors.New("/proc/self/cgroup has no cgroup v2 entry")
	}
	return filepath.Join(sandboxCgroupMount, rel), nil
}

// parseCgroupV2Path returns the unified hierarchy's path from a
// /proc/<pid>/cgroup listing: the "0::<path>" line, which a hybrid host lists
// next to its v1 hierarchies.
func parseCgroupV2Path(raw []byte) (string, bool) {
	for _, line := range strings.Split(string(raw), "\n") {
		if rel, ok := strings.CutPrefix(line, "0::"); ok && strings.HasPrefix(rel, "/") {
			return rel, true
		}
	}
	return "", false
}

// newSandboxCgroup creates a run's cgroup under root and writes its caps:
// memory.max from MaxMemoryBytes, memory.swap.max 0 so the memory cap is not
// served from swap instead (best effort: the file exists only where the
// kernel accounts swap), and pids.max from MaxProcs. Non-positive caps (a
// struct-literal backend) get the defaults: 0 in either file would refuse the
// tool outright.
//
// It returns an error, and leaves nothing behind, when root is not a cgroup v2
// directory, does not delegate both controllers, is not writable, or a cap
// cannot be written: a cgroup without its cap reads as protection and is not.
func newSandboxCgroup(root string, cfg OSLevelConfig) (*sandboxCgroup, error) {
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err != nil {
		return nil, fmt.Errorf("%s is not a cgroup v2 directory: %w", root, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil {
		return nil, err
	}
	enabled := strings.Fields(string(raw))
	var missing []string
	for _, c := range sandboxCgroupControllers {
		if !slices.Contains(enabled, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s does not delegate the %s controller(s) to its children", root, strings.Join(missing, ", "))
	}
	if err := syscall.Access(filepath.Join(root, "cgroup.procs"), 2 /* W_OK */); err != nil {
		return nil, fmt.Errorf("cannot move processes under %s: %w", root, err)
	}
	dir, err := os.MkdirTemp(root, "atcr-run-*")
	if err != nil {
		return nil, err
	}
	cg := &sandboxCgroup{dir: dir}
	def := DefaultOSLevelConfig()
	maxBytes, maxProcs := cfg.MaxMemoryBytes, cfg.MaxProcs
	if maxBytes <= 0 {
		maxBytes = def.MaxMemoryBytes
	}
	if maxProcs <= 0 {
		maxProcs = def.MaxProcs
	}
	for _, limit := range []struct{ file, value string }{
		{"memory.max", strconv.FormatInt(maxBytes, 10)},
		{"pids.max", strconv.Itoa(maxProcs)},
	} {
		if err := sandboxCgroupWrite(filepath.Join(dir, limit.file), limit.value); err != nil {
			_ = cg.remove()
			return nil, fmt.Errorf("cannot set %s: %w", limit.file, err)
		}
	}
	_ = sandboxCgroupWrite(filepath.Join(dir, "memory.swap.max"), "0")
	return cg, nil
}

// remove kills whatever is still in the cgroup and removes it. The kill
// (cgroup.kill, linux 5.14+, best effort) reaches what reapSandboxProcessGroup
// cannot: a descendant that left the process group and outlived a clean exit
// is still in the cgroup. rmdir answers EBUSY until the last killed process
// has exited, so it is retried within sandboxCgroupRemoveBudget; any other
// error is returned at once.
func (cg *sandboxCgroup) remove() error {
	_ = sandboxCgroupWrite(filepath.Join(cg.dir, "cgroup.kill"), "1")
	deadline := time.Now().Add(sandboxCgroupRemoveBudget)
	for {
		err := sandboxRmdir(cg.dir)
		if err == nil || !errors.Is(err, syscall.EBUSY) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// newRunCgroup creates a run's cgroup under b.cgroupRoot or, unset, the cgroup
// atcr runs in.
func (b *OSLevelBackend) newRunCgroup(cfg OSLevelConfig) (*sandboxCgroup, error) {
	root := b.cgroupRoot
	if root == "" {
		var err error
		if root, err = discoverSandboxCgroupRoot(runtime.GOOS); err != nil {
			return nil, err
		}
	}
	return newSandboxCgroup(root, cfg)
}

// probeSandboxCgroup reports whether a run would get a cgroup, by creating one
// exactly as Run does and removing it again. Preflight asks it rather than
// inspecting the root itself, so the answer cannot drift from what Run does.
func (b *OSLevelBackend) probeSandboxCgroup() error {
	cg, err := b.newRunCgroup(b.cfg)
	if err != nil {
		return err
	}
	return cg.remove()
}

// hostCapSupport is the default capSupport: what the running kernel can
// enforce, with the cgroup probed for real.
func (b *OSLevelBackend) hostCapSupport() osLevelCapSupport {
	return osLevelCapSupportFor(runtime.GOOS, sandboxMemoryLimitEnforced, b.probeSandboxCgroup())
}

// openSandboxCgroup picks the mechanism that enforces this run's caps and logs
// it: a per-run cgroup under b.cgroupRoot (or, unset, the cgroup atcr runs in)
// where the host delegates one, otherwise T2's rlimit wrapper, which caps
// memory per process and leaves the PID cap unenforced. The fallback is a
// notice, not a refusal: whether an operator-set cap may run without its
// mechanism is Preflight's decision (epic 35.24 T4). A nil return means the
// fallback.
func (b *OSLevelBackend) openSandboxCgroup(logger *slog.Logger, cfg OSLevelConfig, logCmd string) *sandboxCgroup {
	cg, err := b.newRunCgroup(cfg)
	if err == nil {
		logger.Info("sandbox exec resource caps", "backend", osLevelBackendName, "command", logCmd,
			"mechanism", "cgroup", "cgroup", cg.dir)
		return cg
	}
	mechanism, unenforced := "rlimit", "pids"
	if !sandboxMemoryLimitEnforced {
		mechanism, unenforced = "none", "memory, pids"
	}
	logger.Info("sandbox exec resource caps: cgroup v2 unavailable, falling back", "backend", osLevelBackendName,
		"command", logCmd, "mechanism", mechanism, "unenforced", unenforced, "reason", err)
	return nil
}

// reapSandboxProcessGroup SIGKILLs whatever is left of the sandboxed process
// group after cmd.Run has returned, on EVERY exit path rather than only on
// cancellation. runWith calls it before its deferred os.RemoveAll(scratchDir):
// a descendant the workload backgrounded and detached from its output pipes
// (Go's telemetry writer is the measured case) otherwise outlives a clean exit
// and recreates the scratch tree after it has been removed, and nothing ever
// deletes it again. That is the normal-exit gap internal/verify's
// configureProcessGroup documents and leaves open (localvalidate_pgroup_unix.go:22-28).
//
// An ESRCH means the group is already empty — the common case — and is not an
// error. Any other error is returned for the caller to log.
//
// Limits:
//   - Only the spawned group is targeted (pgid == the leader PID Setpgid
//     assigned), so no sibling of atcr is touched. A descendant that left the
//     group with setsid/setpgid and survives a CLEAN exit escapes this reap: a
//     known open gap. cmd.Wait has already reaped the leader, so neither a
//     pgid nor a parent link leads to it any more. The cancel path does not
//     share the gap for descendants it can see, because it snapshots them while
//     the leader is still running (configureSandboxProcessGroup). On Linux
//     bwrap's --unshare-pid and --die-with-parent kill such a descendant anyway
//     when the sandbox's PID 1 exits; darwin's sandbox-exec has no backstop.
//   - cmd.Wait has already reaped the leader, so if the group is empty its PID
//     may in principle be reused. kill(-pid) only reaches a process that is
//     itself a group LEADER with that ID, and a new process becomes one only by
//     calling setpgid/setsid inside that window — the kernel never hands out a
//     PID that is still in use as a live group's ID.
func reapSandboxProcessGroup(cmd *exec.Cmd) error {
	err := killSandboxProcessGroup(cmd)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// killSandboxProcessGroup SIGKILLs the sandboxed command's whole process group.
// The negative PID targets the group (pgid == leader PID, see
// configureSandboxProcessGroup).
func killSandboxProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return sandboxKill(-cmd.Process.Pid, syscall.SIGKILL)
}

// sandboxKill is syscall.Kill; tests replace it to observe which pids the
// cancel path signals, and in what order.
var sandboxKill = syscall.Kill

// sandboxGetpgid is syscall.Getpgid; tests replace it to present a pid whose
// group cannot be read without depending on a real pid staying free.
var sandboxGetpgid = syscall.Getpgid

// sandboxScanBudget bounds how long the cancel path spends reading the process
// table before it kills the group. The workload keeps running for the whole
// scan, so the bound is what a cancel can cost on top of the kill itself; a
// /proc walk normally takes a few milliseconds and ps(1) tens of them.
const sandboxScanBudget = 250 * time.Millisecond

// sandboxProcessTable reads the host's pid -> ppid table, from /proc where
// there is one and from ps(1) otherwise. Tests replace it to present a table
// the host cannot be made to produce on demand, such as a recycled pid.
//
// A non-nil table returned with a non-nil error is partial: every entry in it
// was read, but the scan did not see every process.
var sandboxProcessTable = func(ctx context.Context) (map[int]int, error) {
	parents, err := procParents(ctx)
	if err != nil && len(parents) == 0 && ctx.Err() == nil {
		parents, err = psParents(ctx)
	}
	return parents, err
}

// sandboxSnapshotDegraded is told when the cancel path's descendant snapshot
// failed, was partial, or ran out of its budget, so the cancel fell back to
// the group kill alone for the descendants it could not see. cmd.Cancel
// cannot carry the error (exec.Cmd treats any it returns as the run's error),
// and configureSandboxProcessGroup has no run context to log through, so the
// default writes to the process-wide logger. Tests replace it to observe the
// report.
var sandboxSnapshotDegraded = func(err error) {
	slog.Warn("sandbox exec cancel: descendant snapshot incomplete; detached descendants it missed may survive the group kill",
		"backend", osLevelBackendName, "error", err)
}

// sandboxDescendants returns the PIDs of every live descendant of the
// sandboxed leader, found by parent links rather than by group, so one that
// called setsid/setpgid is included.
//
// It returns nil unless the leader is still unreaped both before and after the
// process table is read. cmd.Cancel can race Wait reaping the leader, and a
// reaped leader's pid may already belong to an unrelated process whose subtree
// the walk would return. Until Wait reaps it the leader (alive or a zombie)
// holds its pid, so a snapshot bracketed by two such checks lists only its
// own descendants.
//
// It is best effort: a descendant forked after the snapshot, or one whose
// parent already exited (and was reparented to init), is not found. The table
// read is bounded by sandboxScanBudget; a read that fails or overruns it is
// reported to sandboxSnapshotDegraded and yields nil, and a partial one is
// reported and walked for what it did see. The group kill still runs either
// way.
func sandboxDescendants(cmd *exec.Cmd) []int {
	if cmd.Process == nil || !sandboxLeaderUnreaped(cmd) {
		return nil
	}
	parents, err := readSandboxProcessTable(sandboxScanBudget)
	if err != nil {
		sandboxSnapshotDegraded(err)
	}
	if len(parents) == 0 || !sandboxLeaderUnreaped(cmd) {
		return nil
	}
	return descendantsOf(cmd.Process.Pid, parents)
}

// readSandboxProcessTable runs sandboxProcessTable under a budget-long context
// and stops waiting for it when the budget is spent, so a reader that does not
// honour its context (a /proc read stuck in the kernel) still cannot hold the
// group kill back. An abandoned reader finishes on its own into a buffered
// channel nobody reads.
func readSandboxProcessTable(budget time.Duration) (map[int]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	type table struct {
		parents map[int]int
		err     error
	}
	read := sandboxProcessTable // read here, not in the goroutine that may outlive this call
	done := make(chan table, 1)
	go func() {
		parents, err := read(ctx)
		done <- table{parents, err}
	}()
	select {
	case t := <-done:
		return t.parents, t.err
	case <-ctx.Done():
		return nil, fmt.Errorf("process table not read within %v: %w", budget, ctx.Err())
	}
}

// sandboxLeaderUnreaped reports whether Wait has not yet reaped the leader.
// It asks os.Process rather than calling kill(pid, 0), which succeeds for
// whatever process holds the pid now: os.Process answers from a pidfd on Linux
// and from its own reaped flag elsewhere, where only the instant between
// wait4 returning and that flag being set is left uncovered.
func sandboxLeaderUnreaped(cmd *exec.Cmd) bool {
	return cmd.Process.Signal(syscall.Signal(0)) == nil
}

// killSandboxDescendants SIGKILLs each snapshotted PID that is outside the
// leader's process group, and runs before the group kill. A pid still in the
// group is left to that kill: signalling it by pid afterwards would target a
// pid the group kill had just freed (nothing holds a killed member as a zombie
// once its leader is dead too), which an unrelated process may already hold.
// A pid whose group cannot be read has exited and is skipped; kill errors are
// ignored.
//
// A narrow window remains: a detached descendant that exits after the snapshot
// and is reaped by its parent before its kill frees its pid. That needs the
// parent, still alive at this point, to wait for it inside those microseconds
// and the PID allocator to wrap round to the pid at once.
func killSandboxDescendants(leader int, pids []int) {
	for _, pid := range pids {
		pgid, err := sandboxGetpgid(pid)
		if err != nil || pgid == leader {
			continue
		}
		_ = sandboxKill(pid, syscall.SIGKILL)
	}
}

// descendantsOf walks a pid -> ppid table from root and returns every
// descendant, root itself excluded.
func descendantsOf(root int, parents map[int]int) []int {
	children := make(map[int][]int, len(parents))
	for pid, ppid := range parents {
		children[ppid] = append(children[ppid], pid)
	}
	var out []int
	queue := []int{root}
	seen := map[int]bool{root: true}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range children[next] {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

// procParents reads the pid -> ppid table from /proc (Linux). A process that
// exits mid-scan simply drops out. It stops when ctx is done, and returns the
// entries it has read with an error saying the table is partial; an entry it
// could not read for any reason other than its process exiting does the same.
func procParents(ctx context.Context) (map[int]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int)
	var unread error
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return parents, fmt.Errorf("/proc scan stopped after %d entries: %w", len(parents), err)
		}
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		raw, readErr := os.ReadFile("/proc/" + e.Name() + "/stat")
		if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, syscall.ESRCH) {
			continue // exited mid-scan
		}
		if readErr != nil {
			if unread == nil {
				unread = readErr
			}
			continue
		}
		if ppid, ok := parseProcStatPPID(raw); ok {
			parents[pid] = ppid
		}
	}
	if len(parents) == 0 {
		return nil, errors.New("no process entries under /proc")
	}
	if unread != nil {
		return parents, fmt.Errorf("/proc scan skipped unreadable entries: %w", unread)
	}
	return parents, nil
}

// parseProcStatPPID extracts the ppid from a /proc/<pid>/stat line. The comm
// field is parenthesised and may itself contain spaces or ')', so parsing
// starts after the LAST ')': the fields there are state, then ppid.
func parseProcStatPPID(stat []byte) (int, bool) {
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// psParents reads the pid -> ppid table from ps(1), for unix hosts without
// /proc (darwin, the BSDs). The path is absolute so atcr's own PATH cannot
// substitute another binary, and ctx (bounded by sandboxScanBudget on the
// cancel path) kills a hung ps before it can delay the group kill that follows.
func psParents(ctx context.Context) (map[int]int, error) {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-A", "-o", "pid=", "-o", "ppid=").Output()
	if err != nil {
		return nil, err
	}
	return parsePSParents(out)
}

// parsePSParents parses `ps -A -o pid= -o ppid=` output, one "pid ppid" pair
// per line.
func parsePSParents(out []byte) (map[int]int, error) {
	parents := make(map[int]int)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr != nil || ppidErr != nil {
			continue
		}
		parents[pid] = ppid
	}
	if len(parents) == 0 {
		return nil, errors.New("ps listed no processes")
	}
	return parents, nil
}
