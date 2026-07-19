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
// the same StartError-carrying refusals, evaluated BEFORE any Backend.Run — so a
// caller sees byte-for-byte identical start-failure behavior on either path.
//
// The timeout is forwarded into RunSpec.Timeout exactly as received; unlike
// RunConfiguredValidation this adapter never substitutes defaultValidationTimeout
// for a zero value (RunConfiguredValidation remains the sole place that defaults,
// and a zero RunSpec.Timeout defers to the backend's own default). A non-nil error
// from Backend.Run is a backend fault (daemon unreachable, malformed spec, Docker
// runtime fault) and becomes StartError plus a non-nil returned error — the "cannot
// even validate" branch — never a fabricated non-zero program exit.
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
	}

	start := time.Now()
	rr, runErr := backend.Run(ctx, spec)
	elapsed := time.Since(start)

	res := ValidationResult{
		Stdout:   rr.Output,
		TimedOut: rr.TimedOut,
	}
	res.Duration = elapsed
	if runErr != nil {
		res.StartError = fmt.Errorf("auto-fix sandbox validation could not run: %w", runErr)
		return res, res.StartError
	}
	if !rr.TimedOut {
		res.ExitCode = rr.ExitCode
	}
	return res, nil
}
