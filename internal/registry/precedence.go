package registry

// Settings are the effective shared review settings after precedence
// resolution: CLI flag > project config > registry > embedded default.
// Each field resolves independently; a tier participates only where it
// explicitly sets a value.
type Settings struct {
	PayloadMode string
	TimeoutSecs int
	FailOn      string
}

// CLIOverrides carries explicitly-set CLI flag values (nil = flag not set).
type CLIOverrides struct {
	PayloadMode *string
	TimeoutSecs *int
	FailOn      *string
}

// ResolveSettings applies the precedence chain. proj and reg may be nil;
// absent tiers simply fall through to the next one.
func ResolveSettings(cli CLIOverrides, proj *ProjectConfig, reg *Registry) Settings {
	s := Settings{
		PayloadMode: DefaultPayloadMode,
		TimeoutSecs: DefaultTimeoutSecs,
		FailOn:      DefaultFailOn,
	}

	if reg != nil {
		applyTier(&s, reg.PayloadMode, reg.TimeoutSecs, reg.FailOn)
	}
	if proj != nil {
		applyTier(&s, proj.PayloadMode, proj.TimeoutSecs, proj.FailOn)
	}
	if cli.PayloadMode != nil {
		s.PayloadMode = *cli.PayloadMode
	}
	if cli.TimeoutSecs != nil {
		s.TimeoutSecs = *cli.TimeoutSecs
	}
	if cli.FailOn != nil {
		s.FailOn = *cli.FailOn
	}
	return s
}

// applyTier overlays one configuration tier's explicitly-set values onto s.
func applyTier(s *Settings, payloadMode string, timeoutSecs *int, failOn string) {
	if payloadMode != "" {
		s.PayloadMode = payloadMode
	}
	if timeoutSecs != nil {
		s.TimeoutSecs = *timeoutSecs
	}
	if failOn != "" {
		s.FailOn = failOn
	}
}
