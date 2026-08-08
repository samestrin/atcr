//go:build unix

package sandbox

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the sandboxed command in its own process group so a
// timeout can kill the entire tree, not just the direct child.
//
// This is split across build-constrained files because syscall.SysProcAttr has
// no Setpgid field on Windows and syscall.Kill does not exist there — a single
// file referencing either would break `GOOS=windows go build`, which
// .goreleaser.yaml releases and ubuntu-only CI never checks. The `unix` /
// `!unix` pair is exhaustive by construction, so no GOOS can fall through the
// gap that an explicit darwin/linux/other list could leave.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup SIGKILLs the process group led by pid. The negative pid is
// the POSIX "whole group" target.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
