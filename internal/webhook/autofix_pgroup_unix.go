//go:build unix

package webhook

import (
	"errors"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// configureProcessGroup starts cmd in its own process group (Setpgid) and
// cancels it with terminate: SIGTERM to the atcr child alone, which stops its
// own subprocesses as part of its cleanup. The returned reap, called once
// cmd.Wait has returned, kills what is left of the group (a grandchild that
// ignored or outlived the child's cleanup) when autoFixKillGrace has passed
// since that SIGTERM. Until then it waits for the group to empty by itself.
//
// Setpgid makes the child the group leader, so its pgid equals its PID and
// syscall.Kill(-pid, ...) reaches every process it started. A PID is not
// reused while a group with that ID has a member, so polling the group cannot
// hit an unrelated process. Like internal/verify's configureProcessGroup, only
// the cancel path reaps: a child that exits by itself is left as it is.
func configureProcessGroup(cmd *exec.Cmd) (reap func()) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	grace := autoFixKillGrace
	var cancelled atomic.Pointer[time.Time]
	cmd.Cancel = func() error {
		now := time.Now()
		cancelled.CompareAndSwap(nil, &now)
		return terminate(cmd.Process)
	}
	return func() {
		at := cancelled.Load()
		if at == nil || cmd.Process == nil {
			return
		}
		pgid := cmd.Process.Pid
		deadline := at.Add(grace)
		for groupAlive(pgid) {
			left := time.Until(deadline)
			if left <= 0 {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				return
			}
			time.Sleep(min(left, 50*time.Millisecond))
		}
	}
}

// groupAlive reports whether the process group pgid has a member left.
func groupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}
