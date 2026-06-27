package payload

// MaxSprintPlanBytes is the fixed byte ceiling applied to sprint-plan content
// before it is wrapped in a SCOPE CONSTRAINT block and prepended to every agent
// prompt. Stub value; real implementation follows in GREEN.
const MaxSprintPlanBytes int64 = 16384

// ReadSprintPlan is a stub pending GREEN implementation.
func ReadSprintPlan(path string) (string, error) { return "", nil }

// ScopeConstraint is a stub pending GREEN implementation.
func ScopeConstraint(content string) (string, bool) { return "", false }
