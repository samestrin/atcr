package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
)

// ErrExecNoBackend is returned when `--exec` is requested but no sandbox backend
// is configured. It is the refuse-without-backend gate (Epic 11.0 SC-1): the
// command must hard-error without executing anything.
var ErrExecNoBackend = errors.New("--exec requires a [sandbox] block in .atcr/config.yaml (backend, image, test_command); none is configured")

// ErrSandboxNoUsableBackend marks the outcome where the primary backend failed
// its preflight AND a configured fallback was attempted and failed too, so
// nothing usable remains. Both resolvers wrap it — one sentinel, never two — so
// a caller's errors.Is check means the same thing at the `--exec` and
// `--auto-fix` call sites, and so a message edit cannot silently change control
// flow in a fail-closed path the way substring matching would.
//
// It is attached ONLY when a fallback was actually configured and attempted.
// The no-fallback-configured refusal keeps returning its existing un-sentineled
// error verbatim, which is what makes errors.Is(err, ErrSandboxNoUsableBackend)
// false — and therefore CLI output byte-identical — for every operator who
// never opted in.
//
// It is NOT a warn-and-continue signal. Both resolvers still refuse: a fallback
// that was configured but is broken is not an operator accepting unsandboxed
// host execution (that is --no-sandbox, which has its own explicit flag and its
// own mandatory warning).
var ErrSandboxNoUsableBackend = errors.New("no usable sandbox backend")

// newOSLevelBackendFn is the substitution seam for the OS-level fallback,
// mirroring cli/autofix.go's `var resolveAutoFixSandboxFn = verify.ResolveAutoFixSandbox`.
// Tests reassign it to return a fake whose Preflight outcome they control, so
// the resolvers' fallback branches are provable on a runner with neither
// sandbox-exec nor bwrap installed.
//
// It is declared with an explicit sandbox.Backend return type rather than as a
// bare reference to sandbox.NewOSLevelBackend: that constructor returns the
// unexported *osLevelBackend (see TD-002), a type no package outside
// internal/sandbox can name — so a bare reference would compile here but be
// unassignable from a test, which is the entire point of the seam.
var newOSLevelBackendFn = func(cfg sandbox.OSLevelConfig) sandbox.Backend {
	return sandbox.NewOSLevelBackend(cfg)
}

// osLevelFallbackConfigured reports whether sc opts in to the OS-level fallback.
// The match is against the exact sentinel, never "non-empty": a SandboxConfig
// that skipped Validate() (a direct caller, a test, a future construction path)
// must not be able to engage a fail-closed bypass with a value the allowlist
// would have rejected. Absence of the field is the safe default, and nothing —
// not a missing docker binary, not a CI env var — infers the fallback on the
// operator's behalf.
func osLevelFallbackConfigured(sc *registry.SandboxConfig) bool {
	return sc != nil && strings.TrimSpace(sc.Fallback) == registry.SandboxFallbackOSLevel
}

// osLevelFallbackConfig builds the OS-level backend config from the operator's
// sandbox block. Only the timeout carries over: Memory/CPUs/PidsLimit are
// docker-specific knobs the OS-level backend has no equivalent for (TD-001), and
// silently accepting them here would imply caps it does not enforce.
func osLevelFallbackConfig(sc *registry.SandboxConfig) sandbox.OSLevelConfig {
	cfg := sandbox.DefaultOSLevelConfig()
	if sc != nil && sc.TimeoutSecs != nil {
		cfg.Timeout = time.Duration(*sc.TimeoutSecs) * time.Second
	}
	return cfg
}

// ResolveExecBackend implements the execution gate. When execEnabled is false it
// returns nil (execution off — the normal path). When true it REQUIRES a
// configured sandbox backend that passes a preflight check; any failure is an
// error so the caller refuses the run. It never enables execution implicitly.
//
// Returns the ready backend, the resolved test command, and the per-run timeout.
func ResolveExecBackend(ctx context.Context, execEnabled bool, sc *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
	if !execEnabled {
		return nil, nil, 0, nil
	}
	if sc == nil {
		return nil, nil, 0, ErrExecNoBackend
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
	timeout := cfg.Timeout
	if sc.TimeoutSecs != nil {
		timeout = time.Duration(*sc.TimeoutSecs) * time.Second
		cfg.Timeout = timeout
	}
	backend := sandbox.NewDockerBackend(cfg)
	if err := backend.Preflight(ctx); err != nil {
		// Fallback branch: a strict conditional WRAPPING the existing refusal, not a
		// restructure of it. When no fallback is configured, the line below runs
		// exactly as it always has — that untouched path is what keeps
		// TestResolveExecBackend_PreflightFailureRefuses passing unmodified.
		if osLevelFallbackConfigured(sc) {
			osCfg := osLevelFallbackConfig(sc)
			osBackend := newOSLevelBackendFn(osCfg)
			if osErr := osBackend.Preflight(ctx); osErr != nil {
				return nil, nil, 0, fmt.Errorf("--exec preflight failed: docker: %v; os-level fallback also failed: %v: %w", err, osErr, ErrSandboxNoUsableBackend)
			}
			return osBackend, sc.TestCommand, osCfg.Timeout, nil
		}
		return nil, nil, 0, fmt.Errorf("--exec preflight failed: %w", err)
	}
	return backend, sc.TestCommand, timeout, nil
}
