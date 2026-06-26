// Package sandbox provides an opt-in, container-isolated executor for running
// untrusted, model-authored commands and scripts during a review (Epic 11.0).
//
// Execution is the last and most dangerous rung of the review ladder: it runs
// code an LLM wrote on the operator's machine. Containment is therefore not a
// feature but the precondition. Every Backend MUST guarantee, for every Run:
//
//   - no network (the run cannot exfiltrate or call out),
//   - a read-only view of the snapshot (the run cannot mutate the work tree),
//   - resource caps (memory, CPU, PIDs) so a run cannot exhaust the host,
//   - non-root, dropped capabilities, and no-new-privileges.
//
// The package never enables itself: callers opt in via `--exec` and a backend
// that passes Preflight. Bare-metal execution is intentionally unsupported.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RunSpec describes a single sandboxed execution request. Exactly one of Command
// or Script must be set.
type RunSpec struct {
	// Command is an argv executed directly inside the sandbox. No shell
	// interpolation is performed. Mutually exclusive with Script.
	Command []string
	// Script is a shell script body fed to `/bin/sh -s` over stdin and executed
	// in the writable scratch overlay. It is never interpolated into argv.
	// Mutually exclusive with Command.
	Script string
	// Timeout bounds the wall-clock duration of the run. Zero uses the backend
	// default.
	Timeout time.Duration
	// SnapshotDir is the host directory mounted read-only as the working tree.
	SnapshotDir string
}

func (s RunSpec) validate() error {
	hasCmd := len(s.Command) > 0
	hasScript := s.Script != ""
	switch {
	case hasCmd && hasScript:
		return errors.New("sandbox: RunSpec must set exactly one of Command or Script, not both")
	case !hasCmd && !hasScript:
		return errors.New("sandbox: RunSpec must set one of Command or Script")
	}
	if s.SnapshotDir == "" {
		return errors.New("sandbox: RunSpec.SnapshotDir is required")
	}
	// The snapshot dir is interpolated into the `-v <dir>:/work:ro` mount spec.
	// A path containing ':' could inject extra mount options (e.g. strip :ro), so
	// reject it; require an absolute path so the mount source is unambiguous.
	if !filepath.IsAbs(s.SnapshotDir) {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must be absolute, got %q", s.SnapshotDir)
	}
	if strings.ContainsRune(s.SnapshotDir, ':') {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must not contain ':' (mount-spec injection), got %q", s.SnapshotDir)
	}
	return nil
}

// RunResult is the captured outcome of a sandboxed execution. A non-zero program
// exit is reported via ExitCode, never as a Go error.
type RunResult struct {
	// Command is a human-readable rendering of what was executed, suitable for
	// the evidence_exec block and the report.
	Command string
	// ExitCode is the container's exit status (the program's, since the entry
	// process is the program). 124 is the conventional timeout code.
	ExitCode int
	// Output is the combined stdout+stderr, truncated to the backend budget.
	Output string
	// TimedOut is true when the run exceeded its deadline and was killed.
	TimedOut bool
}

// Backend is a pluggable sandbox executor. Docker is the only implementation in
// Epic 11.0; the interface keeps Podman (or a remote runner) a drop-in later.
type Backend interface {
	// Name identifies the backend for diagnostics and the evidence trail.
	Name() string
	// Preflight verifies the backend is usable: runtime installed, daemon
	// reachable, base image present, and a trivial container runs to completion.
	// It MUST pass before Run is used; the CLI refuses `--exec` otherwise.
	Preflight(ctx context.Context) error
	// Run executes spec in isolation and returns the captured result. err is
	// reserved for backend faults (spawn failure, malformed spec); a non-zero
	// program exit is NOT an error.
	Run(ctx context.Context, spec RunSpec) (RunResult, error)
}

// timeoutExitCode is the conventional exit status for a killed-on-timeout run,
// matching coreutils `timeout`.
const timeoutExitCode = 124

var _ Backend = (*DockerBackend)(nil)

// truncate shortens s to at most limit bytes (without splitting a UTF-8 rune),
// appending a marker when it cut anything.
func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	marker := fmt.Sprintf("\n[...truncated %d bytes...]", len(s)-limit)
	keep := limit
	for keep > 0 && (s[keep]&0xC0) == 0x80 { // back up off a UTF-8 continuation byte
		keep--
	}
	return s[:keep] + marker
}
