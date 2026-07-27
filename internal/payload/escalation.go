package payload

// EscalationConfig is the resolved (defaults-applied) per-file escalation
// configuration the builder acts on.
type EscalationConfig struct {
	ChurnRatio    float64
	MinHunks      int
	HunkGapLines  int
	MinCyclomatic int
	MaxFiles      int
}

// EscalationOverrides is the optional, unset-distinguishable form of
// EscalationConfig.
type EscalationOverrides struct {
	ChurnRatio    *float64
	MinHunks      *int
	HunkGapLines  *int
	MinCyclomatic *int
	MaxFiles      *int
}

// DefaultEscalationConfig returns the built-in escalation thresholds.
func DefaultEscalationConfig() EscalationConfig { return EscalationConfig{} }

// ResolveEscalationConfig applies defaults to o.
func ResolveEscalationConfig(o EscalationOverrides) EscalationConfig {
	return EscalationConfig{}
}

// fileSignals are the per-file measurements the escalation heuristic scores.
type fileSignals struct {
	changedLines int
	headLines    int
	hunks        []lineRange
	cyclomatic   int
}

// escalate returns the payload mode a file should actually be rendered in.
func (c EscalationConfig) escalate(base PayloadMode, s fileSignals) PayloadMode {
	return base
}
