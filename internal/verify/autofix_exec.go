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
// Writable /work overlay (non-Go validators supported) — WHEN THE DOCKER BACKEND
// IS RETURNED. Since the os-level fallback landed, this function has two return
// shapes and only the paragraph below describes the Docker one. Under the
// os-level backend none of its mechanics hold: the copy is a host-side Go walk
// (sandbox.seedWritableCopy), there is no image and no /work mount on darwin
// (the run chdirs into the scratch copy instead), and the requirement is that
// the HOST's PATH carries the toolchain rather than the image. The guarantee
// that survives on both is the one that matters to a caller: the snapshot is not
// mutated and the writable copy is ephemeral.
//
// Docker backend specifics: the validation runs with
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
		// The os-level fallback, structurally identical to ResolveExecBackend's
		// branch and sharing its seam, its gate helper, and its ONE sentinel — the
		// consistency here is a requirement, not a style preference: an operator
		// who learns what `--exec` does on a Docker-less host must not find
		// `--auto-fix` behaving differently. Only the return shape differs (no
		// []string/timeout slot; the auto-fix budget travels on RunSpec.Timeout).
		//
		// This is a different bypass from `enabled == false` above: --no-sandbox
		// accepts unsandboxed host execution, sandbox.fallback substitutes a
		// still-contained backend. Neither implies the other.
		// Same three-part gate as ResolveExecBackend, for the same reasons: opted
		// in, not interrupted, and Docker genuinely unavailable rather than
		// misconfigured. An operator who learns what --exec does on a Docker-less
		// host must not find --auto-fix classifying causes differently.
		if osLevelFallbackConfigured(sc) && ctx.Err() == nil && errors.Is(err, sandbox.ErrDockerUnavailable) {
			osBackend := newOSLevelBackendFn(osLevelFallbackConfig(sc))
			if osErr := osBackend.Preflight(ctx); osErr != nil {
				// Same post-preflight interrupt re-check as ResolveExecBackend:
				// an osErr wrapping context.Canceled means the operator pressed
				// ctrl-C mid-preflight — an interrupt, not a
				// neither-backend-usable outcome — so the sentinel stays off and
				// the both-causes shape is returned without it.
				if ctx.Err() != nil || errors.Is(osErr, context.Canceled) || errors.Is(osErr, context.DeadlineExceeded) {
					return nil, fmt.Errorf("--auto-fix sandbox preflight failed: docker: %w; os-level fallback also failed: %w", boundedCause{err}, osErr)
				}
				// boundedCause for the same reason as the --exec resolver: chain
				// preserved, rendered text bounded before it can reach stderr.
				return nil, fmt.Errorf("--auto-fix sandbox preflight failed: docker: %w; os-level fallback also failed: %w: %w", boundedCause{err}, osErr, ErrSandboxNoUsableBackend)
			}
			warnOSLevelFallbackEngaged(ctx, sc, err)
			return osBackend, nil
		}
		return nil, fmt.Errorf("--auto-fix sandbox preflight failed: %w", err)
	}
	return backend, nil
}

// checkSnapshotUsableFn is the seam CheckOSLevelSnapshotUsable uses to call
// sandbox.CheckSnapshotUsable. Tests swap it to assert the writable shape they
// pass is forwarded unchanged.
var checkSnapshotUsableFn = sandbox.CheckSnapshotUsable

// CheckOSLevelSnapshotUsable pre-validates the directory a sandboxed validation
// will actually be handed, when — and only when — the os-level backend was
// selected. It returns nil for a nil backend (sandboxing off) and for the
// docker backend (which enforces its own mounts), and REFUSES any other
// backend it cannot positively identify: dispatching on name alone is
// fail-open for a decorating wrapper, which reports its own Name() and would
// otherwise turn this gate into a silent no-op.
//
// It is a SIBLING of ResolveAutoFixSandbox rather than a parameter on it. The
// resolver's three pinned regression tests call it in its two-argument shape, so
// threading the apply target through the resolver would force edits to all
// three, which is exactly the failure mode AC 03-03 names. The caller already
// holds the resolved target, so passing it here costs nothing.
//
// WHY sc IS A PARAMETER even though no field of it changes the verdict today.
// This looked like a dead argument and an earlier review proposed deleting it;
// it is inert by DESIGN, not by oversight, and the design is load-bearing:
//
//   - The config is reconstructed HERE, via osLevelFallbackConfig(sc), rather
//     than accepted pre-built from the caller. So the day OSLevelConfig gains a
//     config-sourced field (a ToolPath or ScratchDir from the operator's sandbox
//     block), this check picks it up automatically, with no edit here and — the
//     part that matters — no change to either resolver's signature.
//   - Dropping the parameter would pin the check to DefaultOSLevelConfig forever.
//     The gate would then keep reporting success while silently validating
//     something other than what the run uses: a fail-OPEN drift, and the exact
//     future risk the TD row that proposed the deletion named itself.
//   - Accepting the resolver's already-built OSLevelConfig instead would require
//     ResolveExecBackend/ResolveAutoFixSandbox to RETURN it, changing the return
//     shapes AC 03-03 pins. Reconstructing from sc gets the same guarantee at no
//     such cost.
//
// TestCheckOSLevelSnapshotUsable_ThreadsOperatorConfigIntoTheCheck is the
// mechanical proof that the operator's value reaches the checker, so this wiring
// cannot rot unobserved.
//
// The gap this closes (TD-019): Preflight probes its own temp snapshot, so a
// green preflight says nothing about the caller's repository root. A repo
// checked out at $HOME, or at a path containing a sandbox-exec profile
// metacharacter, passes resolution and then fails at Run — for --auto-fix, after
// the patch has already been applied, surfacing as an opaque "validation could
// not run". Refusing here turns that into the generator's own specific message,
// before anything is touched.
func CheckOSLevelSnapshotUsable(backend sandbox.Backend, sc *registry.SandboxConfig, snapshotDir string, writable bool) error {
	// Fail-closed dispatch: nil (sandboxing off) and the docker backend (it
	// enforces its own mounts) are the recognized no-check shapes; the os-level
	// backend gets the snapshot check below. ANY other name is refused outright
	// rather than passed — a decorating wrapper reports its own Name(), so
	// name-based pass-through would silently no-op the gate for exactly the
	// backend shape it exists to check.
	if backend == nil || backend.Name() == registry.SandboxBackendDocker {
		return nil
	}
	if backend.Name() != registry.SandboxFallbackOSLevel {
		return fmt.Errorf("unrecognized sandbox backend %q: refusing to skip the os-level snapshot check (fail-closed)", backend.Name())
	}
	// writable is a parameter, not a hardcoded true, because the two call sites
	// genuinely differ and the shapes are NOT interchangeable — measured: for
	// os.TempDir() the read-only check passes while the writable one refuses
	// (the scratch dir would sit inside the snapshot). --auto-fix always runs
	// Writable (RunSandboxedValidation hardcodes it); --exec never does. Checking
	// the wrong shape would validate a run that never happens.
	if err := checkSnapshotUsableFn(osLevelFallbackConfig(sc), snapshotDir, writable); err != nil {
		return fmt.Errorf("os-level sandbox cannot contain this directory: %w", err)
	}
	return nil
}
