//go:build !unix

package verify

import "os/exec"

// configureProcessGroup is a no-op on non-unix platforms: process-group reaping
// via a negative-PID SIGKILL is a POSIX construct with no portable equivalent.
// Windows and other targets fall back to exec.CommandContext's default
// direct-child kill plus cmd.WaitDelay. Windows process-group semantics are out of
// scope (see the epic plan).
func configureProcessGroup(cmd *exec.Cmd) {}
