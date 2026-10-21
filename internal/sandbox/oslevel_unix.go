//go:build unix

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
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
func configureSandboxProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
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
//     group with setsid/setpgid escapes this reap, exactly as it escapes the
//     cancel path; on Linux bwrap's --unshare-pid and --die-with-parent kill it
//     anyway when the sandbox's PID 1 exits.
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
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
