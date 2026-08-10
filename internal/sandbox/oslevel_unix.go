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
		if cmd.Process == nil {
			return nil
		}
		// The negative PID targets the whole group (pgid == leader PID, above).
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// The group is already gone — the ordinary already-exited race, not
			// a failure. Report ProcessDone so exec.Wait ignores it, matching
			// the default cancel's behavior.
			return os.ErrProcessDone
		}
		return err
	}
}
