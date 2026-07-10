package personas

import "fmt"

// SubmitGate runs the local pre-submission gate for name and reports whether the
// persona may proceed to a fork+PR. It is the reused, network-free front half of
// `atcr personas submit`: it validates the persona name with the same guard the
// install/remove paths use (validatePersonaName), then runs the fixture gate via
// runner. A nil return means the persona cleared the gate; a non-nil error means
// submission is blocked and no GitHub interaction (fork/branch/PR) must occur.
//
// The three blocking conditions, in order:
//   - invalid name — rejected before any resolution, closing the path-traversal
//     class already fixed for install/remove;
//   - no fixture (outcome.HasFixture is false) — a submission-specific, blocking
//     error (deliberately distinct from `personas test`'s softer, non-blocking
//     "No fixture defined" wording), since an unvetted prompt with no fixture
//     cannot clear the gate;
//   - fixture not fully passing (outcome.Passed != outcome.Total).
//
// A zero-case fixture (HasFixture true, Total 0) satisfies Passed == Total
// (0 == 0), so the sole failing predicate does not trip and the persona proceeds
// — an explicit choice: the gate blocks only a genuine fixture failure, never an
// empty fixture that already rendered.
func SubmitGate(name string, runner FixtureRunner) error {
	if err := validatePersonaName(name); err != nil {
		return err
	}
	outcome, err := TestPersona(name, runner)
	if err != nil {
		return err
	}
	if !outcome.HasFixture {
		return fmt.Errorf("cannot submit %q: no fixture defined — add a fixture before submitting", name)
	}
	if outcome.Passed != outcome.Total {
		return fmt.Errorf("cannot submit %q: fixture failed (%d/%d cases passed)", name, outcome.Passed, outcome.Total)
	}
	return nil
}
