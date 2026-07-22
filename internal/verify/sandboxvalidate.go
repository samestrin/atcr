package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
)

// sandboxvalidate.go is the container-isolated sibling of localvalidate.go's
// RunConfiguredValidation (Sprint 32.0, Story 1). The host path is left untouched;
// this file adds a parallel entry point that runs the same operator-supplied
// validation argv inside a sandbox.Backend so an LLM-authored build/test command
// never executes directly on the host or CI runner. Both paths return the identical
// ValidationResult contract, so runAutoFix's three post-call branches
// (verr != nil / !res.Passed() / success) are consumed unchanged regardless of
// which path produced the result.

// RunSandboxedValidation runs argv (an explicit argument list, never a shell
// string) inside the supplied sandbox.Backend rather than on the host, returning a
// ValidationResult identical in shape to RunConfiguredValidation's. It reuses the
// host path's pre-flight guards — empty argv and a missing working directory are
// the same StartError-carrying refusals, evaluated BEFORE any Backend.Run. In
// production (where dir is the resolved absolute applyTarget) the start-failure
// behavior matches the host path; for a non-production empty or relative dir the
// two diverge but both fail closed — the sandbox path defers to the backend's
// RunSpec.validate(), which rejects an empty/relative SnapshotDir.
//
// The timeout is forwarded into RunSpec.Timeout exactly as received; unlike
// RunConfiguredValidation this adapter never substitutes defaultValidationTimeout
// for a zero value (RunConfiguredValidation remains the sole place that defaults,
// and a zero RunSpec.Timeout defers to the backend's own default). The container is
// the timeout-enforcement boundary here: the backend kills the run at
// RunSpec.Timeout, so no ctx-level deadline backstop is layered on — a ctx
// cancellation would surface from Backend.Run as a Go error and misroute a genuine
// timeout into the StartError ("cannot validate") branch instead of TimedOut. A
// non-nil error from Backend.Run is a backend fault (daemon unreachable, malformed
// spec, Docker runtime fault) and becomes StartError plus a non-nil returned error
// — the "cannot even validate" branch — never a fabricated non-zero program exit.
func RunSandboxedValidation(ctx context.Context, backend sandbox.Backend, argv []string, dir string, timeout time.Duration) (ValidationResult, error) {
	if len(argv) == 0 {
		err := errors.New("auto-fix validation command not found or not executable: no command configured")
		return ValidationResult{StartError: err}, err
	}

	// Mirror the host path's guard exactly (localvalidate.go:93): an empty dir is
	// not stat-checked here and instead defers to the backend's RunSpec.validate(),
	// which rejects an empty/relative SnapshotDir before any container spawn. In
	// production dir is always the resolved absolute applyTarget, so this branch
	// runs; the empty-dir delegation only matters to callers that omit it, and it
	// still fails closed (StartError) via the backend.
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			startErr := fmt.Errorf("validation working directory does not exist: %s: %w", dir, err)
			return ValidationResult{StartError: startErr}, startErr
		}
	}

	spec := sandbox.RunSpec{
		Command:     argv,
		Timeout:     timeout,
		SnapshotDir: dir,
		// Opt --auto-fix validation into the ephemeral writable /work overlay: a non-Go
		// validate_command (npm run build -> dist/, cargo build -> target/, Python ->
		// __pycache__) must be able to write into its working directory, which the
		// default read-only /work mount blocks with EROFS. The snapshot stays read-only
		// (mounted at /src); only the throwaway /work tmpfs copy is writable, and it dies
		// with the container. --exec's call sites leave Writable false, unchanged.
		Writable: true,
	}

	start := time.Now()
	rr, runErr := backend.Run(ctx, spec)
	elapsed := time.Since(start)

	res := translateRunResult(rr, runErr)
	// Duration is measured here (around Backend.Run), not inside translateRunResult:
	// sandbox.RunResult carries no duration, so the pure mapping cannot know it. This
	// preserves parity with the host os/exec path, which populates Duration too.
	res.Duration = elapsed
	if res.StartError != nil {
		return res, res.StartError
	}
	return res, nil
}

// translateRunResult is the pure sandbox.RunResult -> ValidationResult mapping,
// shared implicitly with the host path's contract via the common ValidationResult
// type and its Passed() gate. It performs no I/O and does not set Duration (the
// caller measures wall-clock around Backend.Run). Per the translation-gap table
// (AC 01-02):
//
//   - runErr != nil is a backend fault (spawn failure, malformed spec, Docker
//     runtime fault such as exit 125-127 or signal death) -> StartError, so the
//     call site takes its "cannot even validate" branch; a fabricated non-zero
//     program ExitCode is never synthesized from a fault.
//   - Output (combined stdout+stderr) -> Stdout only; Stderr is left empty — a
//     deliberate, documented stream-collapse, since the sandbox returns one stream.
//   - TimedOut -> TimedOut directly. When TimedOut, ExitCode is left at zero so the
//     conventional timeout code 124 is not double-reported as a program failure;
//     Passed() already fails via its !TimedOut clause.
//   - StdoutTruncated/StderrTruncated are left false: the sandbox reports no
//     per-stream truncation signal, so the flag is not guessed.
func translateRunResult(rr sandbox.RunResult, runErr error) ValidationResult {
	if runErr != nil {
		// Preserve any partial output for the failure report, but the outcome is a
		// start failure, not a program exit — leave ExitCode at zero.
		return ValidationResult{
			Stdout:     rr.Output,
			StartError: fmt.Errorf("auto-fix sandbox validation could not run: %w", runErr),
		}
	}
	res := ValidationResult{
		Stdout:   rr.Output,
		TimedOut: rr.TimedOut,
	}
	if !rr.TimedOut {
		res.ExitCode = rr.ExitCode
	}
	return res
}
