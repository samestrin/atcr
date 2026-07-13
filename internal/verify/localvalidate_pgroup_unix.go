//go:build unix

package verify

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup places the validation command in its own process group
// (Setpgid) and replaces exec.CommandContext's default cancel — which SIGKILLs
// only the DIRECT child — with one that SIGKILLs the entire group. A validation
// command like `sh -c "... sleep 60"` spawns grandchildren; killing only the shell
// leaves them orphaned and still running past the deadline (and able to hold the
// stdout pipe open past cmd.WaitDelay). Because Setpgid makes the child the group
// leader, its pgid equals its PID, so syscall.Kill(-pid, SIGKILL) reaps the shell
// and every process it spawned. cmd.WaitDelay is retained as a backstop for the
// I/O pipes; the group kill is now the primary reaper rather than WaitDelay alone.
//
// Cancel fires only when the run's context is done (timeout or parent
// cancellation), never on a clean pass/fail exit, so normal validation runs are
// unaffected. NOTE: a command that exits 0 while leaving a backgrounded
// grandchild behind is not group-reaped on the clean-exit path; only the
// timeout/cancel path (and the cmd.WaitDelay backstop) reaps orphans. This is
// acceptable for the epic's timeout-only scope, but callers should not assume
// group reaping on successful short-lived validations. Unix only — Windows
// process-group semantics are out of scope (see the epic plan).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID targets the whole group (pgid == leader PID, set above).
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// The group is already gone (normal already-exited race). Report it as
			// ProcessDone so exec.Wait ignores it, matching the default cancel.
			return os.ErrProcessDone
		}
		return err
	}
}
