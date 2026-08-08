//go:build !unix

package sandbox

import (
	"errors"
	"os/exec"
)

// setProcessGroup is a no-op on non-unix platforms: there is no process group
// to create. Nothing is lost in practice because osLevelToolFor rejects every
// such GOOS, so no sandboxed process is ever spawned here — this file exists so
// `GOOS=windows go build ./...` succeeds (.goreleaser.yaml ships windows
// binaries; CI is ubuntu-latest only and would never catch the break).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup always fails on non-unix platforms, so the caller falls back
// to killing the single process. It fails loudly rather than reporting a kill
// it did not perform.
func killProcessGroup(_ int) error {
	return errors.New("sandbox: process-group kill is not supported on this platform")
}
