package registry

import (
	"fmt"
	"strings"
)

// MaxTimeoutSecs caps timeout values at every tier (24h); larger values
// would overflow time.Duration arithmetic long before being useful.
const MaxTimeoutSecs = 86400

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
}

// CLIOverrides carries explicitly-set CLI flag values (nil = flag not set).
// A set-but-empty string is treated as unset rather than as an override.
type CLIOverrides struct {
	PayloadMode *string
	TimeoutSecs *int
}

// ResolveSettings applies the precedence chain. proj and reg may be nil;
// absent tiers simply fall through to the next one. CLI values are validated
// here because they bypass the load-time checks the file tiers go through.
func ResolveSettings(cli CLIOverrides, proj *ProjectConfig, reg *Registry) (Settings, error) {
	s := Settings{
		PayloadMode: DefaultPayloadMode,
		TimeoutSecs: DefaultTimeoutSecs,
	}

	if reg != nil {
		applyTier(&s, reg.PayloadMode, reg.TimeoutSecs)
	}
	if proj != nil {
		applyTier(&s, proj.PayloadMode, proj.TimeoutSecs)
	}

	if cli.TimeoutSecs != nil {
		if *cli.TimeoutSecs <= 0 || *cli.TimeoutSecs > MaxTimeoutSecs {
			return Settings{}, fmt.Errorf("timeout must be within 1..%d seconds", MaxTimeoutSecs)
		}
		s.TimeoutSecs = *cli.TimeoutSecs
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
// Whitespace-only strings count as unset.
func applyTier(s *Settings, payloadMode string, timeoutSecs *int) {
	if v := strings.TrimSpace(payloadMode); v != "" {
		s.PayloadMode = v
	}
	if timeoutSecs != nil {
		s.TimeoutSecs = *timeoutSecs
	}
}

// deref returns the trimmed value of p, or "" when p is nil.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
