package main

import (
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errSubmitFixtureRunner drives the runner-error path (AC 01-02 Error Scenario
// 2): a broken installed unit whose YAML/model check fails surfaces a non-nil
// error from RunFixture, distinct from a HasFixture:false outcome.
type errSubmitFixtureRunner struct{ err error }

func (e errSubmitFixtureRunner) RunFixture(string) (personas.FixtureOutcome, error) {
	return personas.FixtureOutcome{}, e.err
}

// withSubmitContinuation swaps the Phase-2 fork+PR continuation seam for the
// duration of a test and returns a pointer to a flag set true when the seam is
// reached. The local gate must reach the continuation only on a full fixture
// pass (AC 01-03 Scenario 1); every gate failure must leave it false, proving
// zero fork/PR/`gh` side effects (AC 01-01 / 01-02 / 01-03 Edge Case 3).
func withSubmitContinuation(t *testing.T) *bool {
	t.Helper()
	called := false
	old := personasSubmitContinuation
	personasSubmitContinuation = func(string) error { called = true; return nil }
	t.Cleanup(func() { personasSubmitContinuation = old })
	return &called
}

// TestPersonasSubmit_InvalidName covers AC 01-01: an invalid persona name is
// rejected by validatePersonaName before any fixture/fork/PR work, exits
// non-zero, and produces zero continuation (fork/PR/`gh`) side effects.
//
// The asserted messages reflect the ACTUAL validatePersonaName behavior: its
// strict character-class regex (paths.go:16) rejects dotted and leading-slash
// names at the char-class check before the absolute-path or segment checks are
// reached, so `../../etc/passwd` and `/etc/passwd` surface the "only letters..."
// message. The reachable segment error requires a regex-passing name with an
// empty segment (`foo//bar`). The security property — rejection with no side
// effects — holds for every case regardless of which message fires.
func TestPersonasSubmit_InvalidName(t *testing.T) {
	cases := []struct {
		name    string
		arg     string
		wantMsg string
	}{
		{"empty", "", `must not be empty`},
		{"traversal_dotdot", "../../etc/passwd", `only letters, digits, '_', '-', and '/' are allowed`},
		{"traversal_embedded", "foo/../bar", `only letters, digits, '_', '-', and '/' are allowed`},
		{"absolute", "/etc/passwd", `only letters, digits, '_', '-', and '/' are allowed`},
		{"disallowed_semicolon", "bad;rm", `only letters, digits, '_', '-', and '/' are allowed`},
		{"disallowed_space", "bad name", `only letters, digits, '_', '-', and '/' are allowed`},
		{"empty_segment", "foo//bar", `contains an invalid path segment`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := withSubmitContinuation(t)
			// A stub runner would still never be reached; inject one anyway to prove
			// the gate fails before the fixture runner is consulted.
			withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}})

			stdout, _, err := executeSplit(t, "personas", "submit", tc.arg)
			require.Error(t, err)
			assert.Equal(t, exitFailure, exitCode(err))
			assert.Contains(t, err.Error(), tc.wantMsg)
			assert.Empty(t, stdout, "invalid name must not write to stdout")
			assert.False(t, *called, "no fork/PR/gh continuation on an invalid name")
		})
	}
}

// TestPersonasSubmit_NoFixture covers AC 01-02: a persona with no fixture is
// blocked with a distinct, submission-specific message (not `personas test`'s
// softer "No fixture defined" wording), exits non-zero, and reaches no
// continuation.
func TestPersonasSubmit_NoFixture(t *testing.T) {
	called := withSubmitContinuation(t)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}})

	stdout, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, err.Error(), `cannot submit "sasha": no fixture defined — add a fixture before submitting`)
	assert.NotContains(t, err.Error(), "No fixture defined for persona", "must not reuse `personas test` wording")
	assert.Empty(t, stdout)
	assert.False(t, *called, "no continuation when the fixture is missing")
}

// TestPersonasSubmit_RunnerError covers AC 01-02 Error Scenario 2: a runner that
// returns a non-nil error (e.g. a broken installed unit) propagates that error
// and blocks submission before any continuation.
func TestPersonasSubmit_RunnerError(t *testing.T) {
	called := withSubmitContinuation(t)
	withFixtureRunner(t, errSubmitFixtureRunner{err: errors.New(`persona "sasha": bound model missing from structured metadata`)})

	_, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, err.Error(), "bound model missing from structured metadata")
	assert.False(t, *called, "no continuation when the runner errors")
}

// TestPersonasSubmit_FixtureFails covers AC 01-03 Edge Cases 1 & 2: a partial or
// complete fixture failure blocks with the exact pass/fail counts and reaches no
// continuation.
func TestPersonasSubmit_FixtureFails(t *testing.T) {
	cases := []struct {
		name          string
		outcome       personas.FixtureOutcome
		wantCountsMsg string
	}{
		{"partial", personas.FixtureOutcome{HasFixture: true, Passed: 2, Total: 3}, `fixture failed (2/3 cases passed)`},
		{"complete", personas.FixtureOutcome{HasFixture: true, Passed: 0, Total: 1}, `fixture failed (0/1 cases passed)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := withSubmitContinuation(t)
			withFixtureRunner(t, stubFixtureRunner{tc.outcome})

			stdout, _, err := executeSplit(t, "personas", "submit", "sasha")
			require.Error(t, err)
			assert.Equal(t, exitFailure, exitCode(err))
			assert.Contains(t, err.Error(), tc.wantCountsMsg)
			assert.Empty(t, stdout)
			assert.False(t, *called, "no continuation when the fixture does not fully pass")
		})
	}
}

// TestPersonasSubmit_FixturePasses covers AC 01-03 Scenario 1: a fully passing
// fixture clears the local gate and hands off to the (stubbed) Phase-2
// continuation without returning a gate error.
func TestPersonasSubmit_FixturePasses(t *testing.T) {
	called := withSubmitContinuation(t)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 3, Total: 3}})

	_, stderr, err := executeSplit(t, "personas", "submit", "sasha")
	require.NoError(t, err)
	assert.True(t, *called, "a full fixture pass must reach the Phase-2 continuation")
	assert.Empty(t, stderr, "a clean gate pass writes no diagnostics")
}

// TestPersonasSubmit_ZeroCaseFixtureProceeds covers AC 01-03 Scenario 2: a
// HasFixture:true, Total:0 outcome satisfies Passed==Total (0==0), so the sole
// failing predicate (Passed != Total) does not trip and submission proceeds like
// a full pass — an explicit choice matching the story's single-predicate gate.
func TestPersonasSubmit_ZeroCaseFixtureProceeds(t *testing.T) {
	called := withSubmitContinuation(t)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 0, Total: 0}})

	_, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.NoError(t, err)
	assert.True(t, *called, "a zero-case fixture (0==0) proceeds past the gate")
}

// TestPersonas_HelpListsSubmit confirms `submit` is registered as the seventh
// persona subcommand.
func TestPersonas_HelpListsSubmit(t *testing.T) {
	out, err := execute(t, "personas", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "submit")
}
