//go:build !unix

package webhook

import "os/exec"

// configureProcessGroup cancels cmd with terminate, which kills the direct
// child where SIGTERM cannot be sent. Process groups and a negative-PID kill
// are POSIX constructs with no portable equivalent, so a grandchild is not
// reaped here; cmd.WaitDelay still bounds the wait on its pipes.
func configureProcessGroup(cmd *exec.Cmd) (reap func()) {
	cmd.Cancel = func() error { return terminate(cmd.Process) }
	return func() {}
}
