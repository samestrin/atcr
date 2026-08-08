//go:build !unix

package sandbox

import "os/exec"

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
