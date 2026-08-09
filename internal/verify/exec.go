package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/log"
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
// bare reference to sandbox.NewOSLevelBackend: the seam's tests substitute
// fakes that are not *sandbox.OSLevelBackend at all, so the wider interface
// type keeps the substitution assignable. Callers that need the concrete type
// (e.g. to assert timeout propagation) can still cast, as the Docker parity
// test does with *sandbox.DockerBackend.
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

// warnOSLevelFallbackEngaged records that the isolation model changed under the
// operator, at the moment it changes. Engaging the fallback is not a like-for-
// like substitution: the OS-level backend runs as the invoking user rather than
// uid 65534, and honors none of the docker-only knobs — Memory, CPUs, PidsLimit
// are dropped (the backend has no equivalent; TD-001/TD-021), and Image is
// unused, so `test_command` meets whatever toolchain the host's PATH provides
// rather than the declared one (TD-022).
//
// This is a log line, deliberately NOT a refusal: the operator asked for this
// fallback explicitly, and refusing on a set cap would turn an opt-in into a
// config error for the exact hosts the feature exists to serve. It is also not
// the neither-backend-usable path, which stays a hard refusal.
//
// KNOWN ASYMMETRY with its sibling bypass warning, deliberately recorded rather
// than silently accepted: warnNoSandbox writes unconditionally to the command's
// stderr, so it cannot be lost. This one goes through the context logger, so
// ATCR_LOG_LEVEL=error silences it, and an embedder whose context carries no
// logger discards it entirely. That is defensible — --no-sandbox accepts
// UNSANDBOXED host execution while this substitutes a still-contained backend,
// so the two do not warrant identical insistence — but Phase 5's docs must state
// that the notice is log-level-suppressible rather than imply a guarantee.
func warnOSLevelFallbackEngaged(ctx context.Context, sc *registry.SandboxConfig, cause error) {
	dropped := make([]string, 0, 4)
	if sc != nil {
		if strings.TrimSpace(sc.Memory) != "" {
			dropped = append(dropped, "sandbox.memory")
		}
		if strings.TrimSpace(sc.CPUs) != "" {
			dropped = append(dropped, "sandbox.cpus")
		}
		if sc.PidsLimit != nil {
			dropped = append(dropped, "sandbox.pids_limit")
		}
		if strings.TrimSpace(sc.Image) != "" {
			dropped = append(dropped, "sandbox.image")
		}
	}
	log.FromContext(ctx).Warn("os-level sandbox fallback engaged",
		"backend", registry.SandboxFallbackOSLevel,
		"docker_preflight_error", truncateCause(cause),
		"unenforced_config", strings.Join(dropped, ","),
		"unenforced_defaults", osLevelUnenforcedDefaults,
		"runs_as", "invoking user (not uid 65534)")
}

// osLevelUnenforcedDefaults names the containment the OS-level backend cannot
// provide, INDEPENDENT of what the operator wrote down.
//
// The `unenforced_config` attribute alone lists only explicitly-set keys, so the
// most common config — image + test_command, all Validate() requires — logged an
// empty list, which reads as "nothing was unenforced" while the fallback in fact
// discards every hardened DockerConfig default (docker.go:62-76). What is lost
// is a property of the backend, not of the config file, so it is stated as one.
const osLevelUnenforcedDefaults = "memory/cpu/pid caps, cap-drop ALL, no-new-privileges, read-only rootfs, uid 65534; host /tmp is readable and writable"

// truncateCause bounds a preflight error before it is logged. Docker's failure
// text carries the daemon's raw stderr and, on the timeout branch, the full
// `docker run` argv — so it can be long, and can echo an operator's DOCKER_HOST
// endpoint. The root redactor only matches Bearer/sk- shapes, so credentials
// embedded in a URL would pass straight through into stderr and CI logs
// (TD-026). Bounding it is the cheap half; the redactor is the real fix.
func truncateCause(err error) string {
	const max = 300
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) <= max {
		return msg
	}
	n := max
	for n > 0 && !utf8.ValidString(msg[:n]) {
		n--
	}
	return msg[:n] + "… (truncated)"
}

// boundedCause carries a cause whose RENDERED form is truncateCause's, while
// errors.Is/errors.As still reach the original through Unwrap.
//
// It exists because the neither-backend-usable returns are the one place both
// obligations apply at once. They %w-wrap the docker cause so a caller can still
// tell a daemon fault from an interrupt — but that error is also RETURNED, and a
// returned error is printed to stderr by runMain, which is not the logger and so
// never saw truncateCause. The warning path was bounded and this one was not,
// so the daemon's raw stderr and the full `docker run` argv (absolute host
// paths, a DOCKER_HOST endpoint) reached the terminal and CI logs verbatim.
//
// errors.Join is deliberately NOT used for this: its Error() concatenates the
// joined messages, so joining the cause back in to preserve the chain would
// reprint the untruncated text and defeat the bound.
type boundedCause struct{ cause error }

func (b boundedCause) Error() string { return truncateCause(b.cause) }
func (b boundedCause) Unwrap() error { return b.cause }

// ResolveExecBackend implements the execution gate. When execEnabled is false it
// returns nil (execution off — the normal path). When true it first attempts the
// configured docker backend and returns it on a passing preflight. If
// sandbox.fallback is set to "os-level" and the docker preflight fails for a
// non-interrupt reason, it attempts the OS-level backend and returns it when and
// only when that backend's own preflight passes. See osLevelUnenforcedDefaults
// for the containment the OS-level shape does not enforce. It never enables
// execution implicitly.
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
		if osLevelFallbackConfigured(sc) && ctx.Err() == nil && errors.Is(err, sandbox.ErrDockerUnavailable) {
			// ctx.Err() gates the branch because a docker preflight that failed
			// only because the operator pressed ctrl-C is not evidence that docker
			// is unusable. Without it, an interrupt spends a temp dir and two more
			// spawn attempts before reporting itself as "no usable sandbox backend".
			//
			// ErrDockerUnavailable gates it for the same reason one class further
			// out: Preflight ALSO fails on operator configuration faults (base image
			// absent, memory/cpus rejected against the host, invalid
			// scratch_size/work_size, the trivial hardened container failing). Those
			// are not evidence Docker is unusable either, and downgrading on them
			// hands an operator who just tightened a cap a backend that enforces
			// none of them. Only the unavailable class may fall back; every other
			// class keeps the pre-existing hard refusal below.
			osCfg := osLevelFallbackConfig(sc)
			osBackend := newOSLevelBackendFn(osCfg)
			if osErr := osBackend.Preflight(ctx); osErr != nil {
				// Re-check cancellation AFTER the OS-level preflight: the branch's
				// ctx.Err() gate only covers a signal arriving BEFORE it, but the
				// preflight spawns real processes and cli/main.go cancels the root
				// context on the first SIGINT, so an interrupt can land inside this
				// window with osErr wrapping context.Canceled. That is an
				// interrupt, not a neither-backend-usable outcome — attaching
				// ErrSandboxNoUsableBackend would tell the operator their sandbox
				// configuration is broken when they simply pressed ctrl-C. Return
				// the same both-causes shape minus the sentinel instead.
				if ctx.Err() != nil || errors.Is(osErr, context.Canceled) || errors.Is(osErr, context.DeadlineExceeded) {
					return nil, nil, 0, fmt.Errorf("--exec preflight failed: docker: %w; os-level fallback also failed: %w", boundedCause{err}, osErr)
				}
				// Both causes are %w-wrapped, not %v-formatted: this is the one path
				// with two distinct causes to tell apart, so it is the last place a
				// caller should lose errors.Is on context.Canceled, ErrOSLevelNoContainment,
				// or a docker-side sentinel.
				//
				// The docker cause travels inside boundedCause: still %w, so the chain
				// is unchanged, but rendered through truncateCause so the daemon's raw
				// stderr does not reach runMain's terminal print in full. The os-level
				// cause is left bare — it is atcr's own generator message, short and
				// bounded by construction, and truncating it would elide the
				// containment reason an operator needs verbatim.
				return nil, nil, 0, fmt.Errorf("--exec preflight failed: docker: %w; os-level fallback also failed: %w: %w", boundedCause{err}, osErr, ErrSandboxNoUsableBackend)
			}
			warnOSLevelFallbackEngaged(ctx, sc, err)
			return osBackend, sc.TestCommand, osCfg.Timeout, nil
		}
		return nil, nil, 0, fmt.Errorf("--exec preflight failed: %w", err)
	}
	return backend, sc.TestCommand, timeout, nil
}

// EmitPendingFallbackWarning fires the fallback-engaged notice a resolver
// deferred, once the caller has committed to the run.
//
// STUB — wired but deliberately inert; see the RED test.
func EmitPendingFallbackWarning(ctx context.Context, b sandbox.Backend) {}
