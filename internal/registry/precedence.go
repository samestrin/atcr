package registry

import (
	"errors"
	"fmt"
	"strings"
)

// MaxTimeoutSecs caps timeout values at every tier (24h); larger values
// would overflow time.Duration arithmetic long before being useful.
const MaxTimeoutSecs = 86400

// MaxAgentTurns caps the per-agent turn budget at every tier; a larger
// value would let a misconfigured or malicious roster run away indefinitely
// once Epic 2.0 activates the tool loop.
const MaxAgentTurns = 1000

// DefaultMaxTurns is the turn budget applied at load when an agent enables
// tools (tools=true) without an explicit max_turns. 10 covers a typical
// evidence-gathering loop (3-10 tool calls) while bounding a thrashing model;
// it stays well under the MaxAgentTurns hard cap.
const DefaultMaxTurns = 10

// MaxToolBudgetBytes caps the cumulative tool-result budget at the largest
// value that could ever be consumed: the per-payload byte budget times the
// maximum turn count. Anything larger is almost certainly a typo and is
// indistinguishable from the unlimited (0) sentinel in practice.
const MaxToolBudgetBytes = DefaultPayloadByteBudget * MaxAgentTurns

// DefaultMaxParallel is the embedded-tier bound on concurrent parallel-lane
// agent calls. 10 preserves the effective behavior of v1 rosters (≤~10 agents,
// AC 01-04's "10 concurrent agent calls" target) while capping a larger or
// misconfigured roster. 0 is the documented unbounded escape hatch.
const DefaultMaxParallel = 10

// Retry/backoff tunables (Epic 4.6). The llmclient already implements the
// retry engine (exponential backoff + jitter + Retry-After); these expose its
// budget through the shared-settings precedence chain. DefaultMaxRetries is 5
// (the epic AC default) at the embedded tier — NOT the llmclient
// defaultMaxRetries=2 constant, which stays the bare-New() fallback for the
// doctor self-test and other direct clients. InitialBackoffMs is the fallback
// base delay between retries when no server Retry-After is present.
const (
	// DefaultMaxRetries is the embedded-tier retry budget (5 → 6 attempts total).
	DefaultMaxRetries = 5
	// MaxRetriesCap bounds max_retries at every tier; a larger budget would let
	// a single rate-limited agent stall the fan-out far past any useful window.
	// 0 is valid (a single attempt, no retry).
	MaxRetriesCap = 10
	// DefaultInitialBackoffMs is the embedded-tier base delay (ms) between
	// retries; it matches the llmclient default (500ms) so unconfigured behavior
	// is unchanged apart from the larger retry budget.
	DefaultInitialBackoffMs = 500
	// MaxInitialBackoffMs caps the configurable base delay (ms) at the
	// llmclient's per-retry backoff ceiling (30s); a larger base would be
	// clamped anyway.
	MaxInitialBackoffMs = 30000
)

// validateRetryBounds checks the Epic 4.6 retry budget fields against their shared
// caps and returns one message per out-of-range field (empty when both are valid).
// maxRetries and backoffMs are pointers so a nil (unset) field is skipped rather
// than rejected. It returns bare messages — not finished errors — so each tier can
// wrap them in its own error type (entryError attribution for the per-agent
// load-time tier, plain errors for the global and resolve-time tiers) while sharing
// one definition of the bounds, which is the actual drift risk this consolidates.
func validateRetryBounds(maxRetries, backoffMs *int) []string {
	var msgs []string
	if maxRetries != nil && (*maxRetries < 0 || *maxRetries > MaxRetriesCap) {
		msgs = append(msgs, fmt.Sprintf("max_retries must be within 0..%d", MaxRetriesCap))
	}
	if backoffMs != nil && (*backoffMs <= 0 || *backoffMs > MaxInitialBackoffMs) {
		msgs = append(msgs, fmt.Sprintf("initial_backoff_ms must be within 1..%d", MaxInitialBackoffMs))
	}
	return msgs
}

// Settings are the effective shared review settings after precedence
// resolution: CLI flag > project config > registry > embedded default.
// Each field resolves independently; a tier participates only where it
// explicitly sets a value.
//
// fail_on is deliberately absent: the CI gate is opt-in (no embedded
// default), so gate resolution lives in ResolveGateThreshold with its own
// tier-specific error semantics. DefaultFailOn seeds only the config
// template `atcr init` generates.
type Settings struct {
	PayloadMode string
	TimeoutSecs int
	// PayloadByteBudget is the per-payload byte budget fed to
	// payload.ApplyByteBudget; 0 is the documented unlimited escape hatch
	// (AC 06-03).
	PayloadByteBudget int64
	// MaxParallel bounds concurrent parallel-lane agent calls in the fan-out
	// engine; 0 is the documented unbounded escape hatch.
	MaxParallel int
	// MaxRetries is the resolved retry budget passed to the llmclient per call
	// (Epic 4.6); 0 means a single attempt with no retry.
	MaxRetries int
	// InitialBackoffMs is the resolved base delay (ms) between retries when no
	// server Retry-After header is present (Epic 4.6).
	InitialBackoffMs int
}

// CLIOverrides carries explicitly-set CLI flag values (nil = flag not set).
// A set-but-empty string is treated as unset rather than as an override.
type CLIOverrides struct {
	PayloadMode       *string
	TimeoutSecs       *int
	PayloadByteBudget *int64
	MaxParallel       *int
}

// ResolveSettings applies the precedence chain. proj and reg may be nil;
// absent tiers simply fall through to the next one. CLI values are validated
// here because they bypass the load-time checks the file tiers go through.
func ResolveSettings(cli CLIOverrides, proj *ProjectConfig, reg *Registry) (Settings, error) {
	s := Settings{
		PayloadMode:       DefaultPayloadMode,
		TimeoutSecs:       DefaultTimeoutSecs,
		PayloadByteBudget: DefaultPayloadByteBudget,
		MaxParallel:       DefaultMaxParallel,
		MaxRetries:        DefaultMaxRetries,
		InitialBackoffMs:  DefaultInitialBackoffMs,
	}

	if reg != nil {
		applyTier(&s, reg.PayloadMode, reg.TimeoutSecs, reg.PayloadByteBudget, reg.MaxParallel)
		// Retry tunables live only at the registry (global) tier and the agent
		// tier (Epic 4.6) — the project tier intentionally does not carry them,
		// so they are overlaid here rather than through applyTier.
		if reg.MaxRetries != nil {
			s.MaxRetries = *reg.MaxRetries
		}
		if reg.InitialBackoffMs != nil {
			s.InitialBackoffMs = *reg.InitialBackoffMs
		}
	}
	if proj != nil {
		applyTier(&s, proj.PayloadMode, proj.TimeoutSecs, proj.PayloadByteBudget, proj.MaxParallel)
	}

	if cli.PayloadByteBudget != nil {
		// Same rule payload.ValidateBudget enforces (the package boundary
		// forbids importing it here): zero is valid and means unlimited.
		if *cli.PayloadByteBudget < 0 {
			return Settings{}, fmt.Errorf("byte budget must be >= 0, got %d", *cli.PayloadByteBudget)
		}
		s.PayloadByteBudget = *cli.PayloadByteBudget
	}
	if cli.TimeoutSecs != nil {
		if *cli.TimeoutSecs <= 0 || *cli.TimeoutSecs > MaxTimeoutSecs {
			return Settings{}, fmt.Errorf("timeout must be within 1..%d seconds", MaxTimeoutSecs)
		}
		s.TimeoutSecs = *cli.TimeoutSecs
	}
	if cli.MaxParallel != nil {
		// The CLI tier bypasses the file-load checks; validate here. 0 is the
		// unbounded escape hatch (parallels payload_byte_budget), only negative
		// is rejected.
		if *cli.MaxParallel < 0 {
			return Settings{}, fmt.Errorf("max_parallel must be >= 0 (0 = unbounded), got %d", *cli.MaxParallel)
		}
		s.MaxParallel = *cli.MaxParallel
	}
	if v := deref(cli.PayloadMode); v != "" {
		// The CLI tier bypasses the file-load enum checks, so validate here:
		// an invalid --payload value must fail before any review work, not
		// surface deep inside payload.Build.
		if !payloadModeValid(v) {
			return Settings{}, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", v)
		}
		s.PayloadMode = v
	}
	// Post-resolution sanity: a directly-constructed proj/reg (bypassing the
	// file loader) can carry out-of-range values. Catch them here so the engine
	// never receives them.
	//
	// MaxParallel: n<=0 is the unbounded escape hatch; only negative is invalid.
	if s.MaxParallel < 0 {
		return Settings{}, fmt.Errorf("max_parallel must be >= 0 (0 = unbounded), got %d", s.MaxParallel)
	}
	// TimeoutSecs: review.go's `p.TimeoutSec > 0` guard silently skips the
	// timeout on <=0 values (no timeout applied — inverse of intent).
	if s.TimeoutSecs <= 0 || s.TimeoutSecs > MaxTimeoutSecs {
		return Settings{}, fmt.Errorf("timeout_secs must be within 1..%d, got %d", MaxTimeoutSecs, s.TimeoutSecs)
	}
	// PayloadByteBudget: 0 = unlimited (valid); negative is always invalid.
	if s.PayloadByteBudget < 0 {
		return Settings{}, fmt.Errorf("payload_byte_budget must be >= 0 (0 = unlimited), got %d", s.PayloadByteBudget)
	}
	// Retry tunables (Epic 4.6): a directly-constructed reg (bypassing the file
	// loader) can carry out-of-range values; catch them so the engine never
	// receives them. 0 retries is valid (single attempt); the base delay must be
	// positive so the exponential schedule has a non-zero starting point.
	if msgs := validateRetryBounds(&s.MaxRetries, &s.InitialBackoffMs); len(msgs) > 0 {
		return Settings{}, errors.New(msgs[0])
	}
	// Per-agent retry overrides are read directly by EffectiveMaxRetries /
	// EffectiveInitialBackoffMs, bypassing the global resolution above, so a
	// directly-constructed reg (skipping LoadRegistry's validateAgent) could
	// otherwise smuggle out-of-range per-agent values straight to the engine.
	// Re-check them here for the same defense-in-depth reason as the global tier,
	// walking in sorted order so the error is deterministic.
	if reg != nil {
		for _, name := range sortedKeys(reg.Agents) {
			a := reg.Agents[name]
			if msgs := validateRetryBounds(a.MaxRetries, a.InitialBackoffMs); len(msgs) > 0 {
				return Settings{}, fmt.Errorf("agent '%s': %s", name, msgs[0])
			}
		}
	}
	return s, nil
}

// applyTier overlays one configuration tier's explicitly-set values onto s.
// Whitespace-only strings count as unset. byteBudget and maxParallel are
// pointers so an explicit 0 (the unlimited/unbounded escape hatch) survives
// default application.
func applyTier(s *Settings, payloadMode string, timeoutSecs *int, byteBudget *int64, maxParallel *int) {
	if v := strings.TrimSpace(payloadMode); v != "" {
		s.PayloadMode = v
	}
	if timeoutSecs != nil {
		s.TimeoutSecs = *timeoutSecs
	}
	if byteBudget != nil {
		s.PayloadByteBudget = *byteBudget
	}
	if maxParallel != nil {
		s.MaxParallel = *maxParallel
	}
}

// deref returns the trimmed value of p, or "" when p is nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
