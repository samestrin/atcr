//go:build unix

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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

// sandboxProcessTable reads the host's pid -> ppid table, from /proc where
// there is one and from ps(1) otherwise. Tests replace it to present a table
// the host cannot be made to produce on demand, such as a recycled pid.
var sandboxProcessTable = func() (map[int]int, error) {
	parents, err := procParents()
	if err != nil {
		parents, err = psParents()
	}
	return parents, err
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
// It is best effort, and returns nil when the process table cannot be read: a
// descendant forked after the snapshot, or one whose parent already exited
// (and was reparented to init), is not found. The group kill still runs either
// way.
func sandboxDescendants(cmd *exec.Cmd) []int {
	if cmd.Process == nil || !sandboxLeaderUnreaped(cmd) {
		return nil
	}
	parents, err := sandboxProcessTable()
	if err != nil || !sandboxLeaderUnreaped(cmd) {
		return nil
	}
	return descendantsOf(cmd.Process.Pid, parents)
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
// exits mid-scan simply drops out.
func procParents() (map[int]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parents := make(map[int]int)
	for _, e := range entries {
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		raw, readErr := os.ReadFile("/proc/" + e.Name() + "/stat")
		if readErr != nil {
			continue
		}
		if ppid, ok := parseProcStatPPID(raw); ok {
			parents[pid] = ppid
		}
	}
	if len(parents) == 0 {
		return nil, errors.New("no process entries under /proc")
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
// substitute another binary, and the bound keeps a hung ps from delaying the
// group kill that follows.
func psParents() (map[int]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
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
