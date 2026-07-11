package main

import (
	"context"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/spf13/cobra"
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

// spyFixtureRunner records every RunFixture invocation so tests can assert that
// the local gate rejects invalid inputs before the runner is ever consulted.
type spyFixtureRunner struct {
	outcome personas.FixtureOutcome
	calls   []string
}

func (s *spyFixtureRunner) RunFixture(name string) (personas.FixtureOutcome, error) {
	s.calls = append(s.calls, name)
	return s.outcome, nil
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
	personasSubmitContinuation = func(*cobra.Command, string) error { called = true; return nil }
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
			// A spy runner lets us prove the gate fails before the fixture runner is consulted.
			spy := &spyFixtureRunner{outcome: personas.FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}}
			withFixtureRunner(t, spy)

			stdout, _, err := executeSplit(t, "personas", "submit", tc.arg)
			require.Error(t, err)
			assert.Equal(t, exitFailure, exitCode(err))
			assert.Contains(t, err.Error(), tc.wantMsg)
			assert.Empty(t, stdout, "invalid name must not write to stdout")
			assert.False(t, *called, "no fork/PR/gh continuation on an invalid name")
			assert.Empty(t, spy.calls, "fixture runner must not be consulted for an invalid name")
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

// TestPersonasSubmit_ContinuationErrorPropagates confirms the phase-exit
// contract Phase 2 depends on: once the local gate passes and control reaches the
// continuation seam, an error the seam returns is surfaced by RunE (non-nil,
// exit 1) rather than swallowed. Phase 2 attaches the fork+PR flow here, so a
// fork/push/PR failure must fail the command.
func TestPersonasSubmit_ContinuationErrorPropagates(t *testing.T) {
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}})
	old := personasSubmitContinuation
	personasSubmitContinuation = func(*cobra.Command, string) error { return errors.New("fork+PR failed") }
	t.Cleanup(func() { personasSubmitContinuation = old })

	_, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, err.Error(), "fork+PR failed")
}

// TestPersonas_HelpListsSubmit confirms `submit` is registered as the seventh
// persona subcommand.
func TestPersonas_HelpListsSubmit(t *testing.T) {
	out, err := execute(t, "personas", "--help")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^  submit\s+`, out, "submit must appear as a registered subcommand in the help list")
}

// stubGitHub is a command-level GitHubSubmitter stub: it lets the REAL Phase-2
// continuation run end-to-end while replacing every gh/git interaction, so a
// command test exercises the full precondition→fork→push→PR flow with zero real
// gh binary or network calls (AC 02-03).
type stubGitHub struct {
	url string
	err error // returned from CheckPrecondition to fail the whole flow early
}

func (s stubGitHub) CheckPrecondition(context.Context) error { return s.err }
func (stubGitHub) Fork(context.Context, string) error        { return nil }
func (stubGitHub) PushBranch(context.Context, string, string, string) (string, error) {
	return "octocat:persona-submit/sasha", nil
}
func (s stubGitHub) CreatePR(context.Context, personas.PRRequest) (string, error) {
	return s.url, nil
}

// withPersonasGitHub swaps the package-level gh seam for a stub for the duration
// of a test and restores it after, matching the personasClient/personasFixtureRunner
// restoration pattern so seam state never leaks between tests (AC 02-03 Edge Case 1).
func withPersonasGitHub(t *testing.T, g personas.GitHubSubmitter) {
	t.Helper()
	old := personasGitHub
	personasGitHub = g
	t.Cleanup(func() { personasGitHub = old })
}

// withPersonasSubmissionsDir points the `submitted` marker write at a temp dir so
// a command-level submit test never touches the real ~/.config/atcr/submissions
// (Phase 3 marker wiring), matching the seam-restoration pattern above.
func withPersonasSubmissionsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := personasSubmissionsDir
	personasSubmissionsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { personasSubmissionsDir = old })
	return dir
}

// TestPersonasSubmit_ForkPRHappyPath covers AC 02-02 Scenario 1 at the command
// level: a passing fixture gate hands off to the real continuation, which drives
// the stubbed seam and prints the resulting PR URL to stdout.
func TestPersonasSubmit_ForkPRHappyPath(t *testing.T) {
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}})
	withPersonasGitHub(t, stubGitHub{url: "https://github.com/samestrin/atcr/pull/42"})
	subDir := withPersonasSubmissionsDir(t)

	stdout, stderr, err := executeSplit(t, "personas", "submit", "sasha")
	require.NoError(t, err)
	assert.Contains(t, stdout, "https://github.com/samestrin/atcr/pull/42", "PR URL is printed to stdout on success")
	assert.Empty(t, stderr)

	got, ok, err := personas.ReadSubmission(subDir, "sasha")
	require.NoError(t, err)
	require.True(t, ok, "submitted marker must be persisted after a successful PR")
	assert.Equal(t, "octocat", got.Submitter, "submitter is derived from the pushed head ref")
	assert.Equal(t, "-", got.Version, "version falls back to '-' when the persona unit is not installed locally")
	assert.True(t, got.FixturePassed, "marker records that the fixture gate passed")
}

// TestPersonasSubmit_ForkPRFailurePropagates covers AC 02-01 at the command
// level: a precondition failure from the seam surfaces as a non-zero exit with
// the seam's message, and nothing is printed to stdout.
func TestPersonasSubmit_ForkPRFailurePropagates(t *testing.T) {
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}})
	withPersonasGitHub(t, stubGitHub{err: errors.New("gh auth check failed: not logged in")})

	stdout, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, err.Error(), "gh auth check failed")
	assert.Empty(t, stdout, "no PR URL on a blocked submission")
}

// TestPersonasSubmit_GateBlocksBeforeSeam confirms a failing fixture gate blocks
// before the gh seam is ever consulted: the stub would panic-free record nothing,
// and the command fails at the gate (AC 02-01 precondition ordering + Phase 1 gate).
func TestPersonasSubmit_GateBlocksBeforeSeam(t *testing.T) {
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}})
	withPersonasGitHub(t, stubGitHub{url: "https://should.not/pull/1"})

	stdout, _, err := executeSplit(t, "personas", "submit", "sasha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fixture defined")
	assert.Empty(t, stdout, "a blocked gate never reaches the fork+PR seam")
}
