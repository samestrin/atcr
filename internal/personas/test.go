package personas

import (
	"fmt"
	"os"
)

// FixtureOutcome is the result of running a persona's fixture cases.
type FixtureOutcome struct {
	HasFixture bool
	Passed     int
	Total      int
}

// FixtureRunner executes a persona's fixture cases. It is injectable so the CLI
// `test` path stays free of live LLM calls in CI.
type FixtureRunner interface {
	RunFixture(name string) (FixtureOutcome, error)
}

// TestPersona resolves name (a built-in or a community persona installed under
// personasDir) and delegates fixture execution to runner. It errors if the
// persona is neither a built-in nor installed.
func TestPersona(personasDir, name string, runner FixtureRunner) (FixtureOutcome, error) {
	if !isBuiltin(name) {
		dest, err := personaPath(personasDir, name)
		if err != nil {
			return FixtureOutcome{}, err
		}
		if _, err := os.Stat(dest); err != nil {
			if os.IsNotExist(err) {
				return FixtureOutcome{}, fmt.Errorf("persona %q is not installed", name)
			}
			return FixtureOutcome{}, fmt.Errorf("failed to stat persona %q: %w", name, err)
		}
	}
	return runner.RunFixture(name)
}
