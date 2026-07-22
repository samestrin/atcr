package verify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
)

// ErrAutoFixSandboxUnconfigured is returned when `--auto-fix` runs under its
// sandboxed-by-default posture but no `[sandbox]` block is configured and the
// operator did not explicitly opt out. It is the inverse of ErrExecNoBackend:
// `--exec` is opt-IN (off is the safe default), whereas `--auto-fix` sandboxing
// is opt-OUT (on is the safe default), so an unconfigured sandbox here is a
// fail-closed refusal rather than a silent skip. It is a distinct sentinel so
// callers/tests can `errors.Is`-check the two features independently.
var ErrAutoFixSandboxUnconfigured = errors.New("--auto-fix requires a [sandbox] block in .atcr/config.yaml for its validation step (backend, image, test_command), or an explicit --no-sandbox opt-out; none is configured")

// ResolveAutoFixSandbox builds and preflights the sandbox backend that isolates
// the `--auto-fix` validation step. It mirrors ResolveExecBackend's resolve-and-
// preflight shape (field overrides applied only when set, Preflight required
// before a ready backend is returned) but INVERTS the default polarity:
//
//   - ResolveExecBackend(ctx, execEnabled, sc): opt-IN — execEnabled=false is the
//     safe no-op default, so `enabled == false` returns (nil, nil).
//   - ResolveAutoFixSandbox(ctx, enabled, sc): opt-OUT — `enabled` defaults to
//     TRUE for auto-fix call sites, so `sc == nil` under `enabled == true` is a
//     hard refusal (ErrAutoFixSandboxUnconfigured), never a silent fallback to
//     unsandboxed host execution.
//
// enabled == false is the shape Story 3's `--no-sandbox` opt-out produces: it
// short-circuits before any Docker config construction or Preflight call and
// returns (nil, nil), symmetric with ResolveExecBackend's disabled path.
//
// It returns a ready sandbox.Backend and a nil error, or a nil backend and a
// non-nil error — never a partial success (a non-nil backend alongside an error).
//
// It intentionally does NOT return a resolved timeout: unlike ResolveExecBackend,
// the auto-fix per-run budget is carried by RunSpec.Timeout at the dispatch site
// (sourced from auto_fix.validate_timeout), so sandbox.timeout_secs only ever acts
// as the backend's fallback default and can never silently shrink the operator's
// validation budget.
//
// Writable /work overlay (non-Go validators supported): the validation runs with
// the patched working tree mounted read-only at /src and copied via `cp -a` into a
// writable /work tmpfs (RunSandboxedValidation sets RunSpec.Writable; see internal/
// sandbox/docker.go). A validate_command that writes UNDER the project dir — npm
// run build -> dist/, cargo build -> target/, Python __pycache__, most non-Go
// builders and codegen — writes into that ephemeral /work copy instead of hitting
// EROFS, so a valid non-Go fix is validated and its PR opened rather than reverted.
// The /src snapshot stays read-only for the container's lifetime and the /work copy
// dies with the container, so no host file is mutated. The validation image must
// provide /bin/sh and cp (alpine/golang-family images do; distroless/scratch do
// not). See docs/auto-fix.md.
//
// Design tension (open follow-up, deliberately NOT resolved here): SandboxConfig.
// Validate() unconditionally requires Image + TestCommand because it was written
// for `--exec`'s run_tests tool. An operator who adds a `[sandbox]` block solely
// to sandbox `--auto-fix` (which runs auto_fix.validate_command, not test_command)
// is still forced to set an unused test_command. Loosening that here would weaken
// `--exec`'s existing contract, so this sprint keeps Validate() unchanged (pinned
// by a regression test) and leaves a split-validation / parallel-light-validation
// path as future work.
//
// Overlay sizing note: WorkSize ("512m") and ScratchSize ("64m") rely on
// DefaultDockerConfig defaults and are bounded by the operator's Memory cap;
// they are deliberately not exposed as separate YAML knobs on SandboxConfig.
func ResolveAutoFixSandbox(ctx context.Context, enabled bool, sc *registry.SandboxConfig) (sandbox.Backend, error) {
	if !enabled {
		return nil, nil
	}
	if sc == nil {
		return nil, ErrAutoFixSandboxUnconfigured
	}
	cfg := sandbox.DefaultDockerConfig()
	if sc.DockerPath != "" {
		cfg.DockerPath = sc.DockerPath
	}
	if sc.Image != "" {
		cfg.Image = sc.Image
	}
	if sc.Memory != "" {
		cfg.Memory = sc.Memory
	}
	if sc.CPUs != "" {
		cfg.CPUs = sc.CPUs
	}
	if sc.PidsLimit != nil {
		cfg.PidsLimit = *sc.PidsLimit
	}
	if sc.TimeoutSecs != nil {
		cfg.Timeout = time.Duration(*sc.TimeoutSecs) * time.Second
	}
	backend := sandbox.NewDockerBackend(cfg)
	if err := backend.Preflight(ctx); err != nil {
		return nil, fmt.Errorf("--auto-fix sandbox preflight failed: %w", err)
	}
	return backend, nil
}
