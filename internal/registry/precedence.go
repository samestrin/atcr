package registry

import (
	"fmt"
	"strings"
)

// MaxTimeoutSecs caps timeout values at every tier (24h); larger values
// would overflow time.Duration arithmetic long before being useful.
const MaxTimeoutSecs = 86400

// DefaultMaxParallel is the embedded-tier bound on concurrent parallel-lane
// agent calls. 10 preserves the effective behavior of v1 rosters (≤~10 agents,
// AC 01-04's "10 concurrent agent calls" target) while capping a larger or
// misconfigured roster. 0 is the documented unbounded escape hatch.
const DefaultMaxParallel = 10

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
	}

	if reg != nil {
		applyTier(&s, reg.PayloadMode, reg.TimeoutSecs, reg.PayloadByteBudget, reg.MaxParallel)
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
