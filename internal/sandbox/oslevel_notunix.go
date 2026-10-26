//go:build !unix

package sandbox

import (
	"log/slog"
	"os/exec"
)

// configureSandboxProcessGroup is a no-op on non-unix platforms: there is no
// POSIX process group to create and no syscall.Kill to reap one with, so
// exec.CommandContext's default single-process cancel stays installed.
//
// Nothing is lost in practice because toolPath consults osLevelToolFor first and
// refuses every such GOOS unconditionally — including when an operator has set
// tool_path — so Run never spawns a process here. This file exists so
// `GOOS=windows go build ./...` succeeds: .goreleaser.yaml ships windows/amd64
// and windows/arm64, while CI is ubuntu-latest only and would never surface the
// break before a release tag.
func configureSandboxProcessGroup(_ *exec.Cmd) {}

// reapSandboxProcessGroup is a no-op for the same reason: no process group was
// created, and Run never spawns a process on these platforms.
func reapSandboxProcessGroup(_ *exec.Cmd) error { return nil }

// applySandboxResourceLimits is a no-op for the same reason: there is no
// /bin/sh to set an rlimit with, and Run never spawns a process on these
// platforms.
func applySandboxResourceLimits(_ *exec.Cmd, _ OSLevelConfig, _ *sandboxCgroup) {}

// sandboxCgroup is never created on these platforms; see openSandboxCgroup.
type sandboxCgroup struct {
	dir string
}

// openSandboxCgroup returns nil: cgroups are linux-only, and Run never spawns
// a process on these platforms.
func (b *OSLevelBackend) openSandboxCgroup(_ *slog.Logger, _ OSLevelConfig, _ string) *sandboxCgroup {
	return nil
}

func (*sandboxCgroup) remove() error { return nil }
