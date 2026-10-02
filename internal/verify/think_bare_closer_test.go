package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reply that carries a bare </think> — one no <think> opened — may have
// started mid-thought, because a chat template put the opener in the prompt.
// llmclient.SplitThink deliberately leaves such a closer in place (decided
// 2026-09-30) and HasEnclosingThinkBlock deliberately does not refuse on it (a
// lone closer encloses nothing). Both decisions are right on their own terms,
// and together they left the envelope BEFORE the closer — the draft the model
// abandoned — as the first one a first-match parser reaches.
//
// These tests pin the three-way rule classifyUnopenedCloser applies, on both
// lanes. The middle case is the one worth stating plainly: when BOTH sides carry
// an envelope, the tag structure of `{draft} </think> {real}` is identical to
// that of `{real} … "</think>" {example}`, so neither side is provably committed
// and the lane refuses rather than picking. A disclosed unverifiable verdict or a
// declined fix is recoverable; a silently wrong one is not.

// --- skeptic lane ---

// TestVerdictFromAnswer_EnvelopeOnBothSidesIsRefused is the defect's headline
// shape. Before the fix this returned `refuted` — the draft — and
// internal/reconcile/gate.go treats `refuted` as "a skeptic disproved it" at ANY
// severity, so a real CRITICAL finding silently stopped failing CI.
func TestVerdictFromAnswer_EnvelopeOnBothSidesIsRefused(t *testing.T) {
	t.Parallel()
	answer := `Checking the call sites. {"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, ambiguous := verdictFromAnswer(answer)

	assert.True(t, ambiguous,
		"an envelope on both sides of an unopened closer is not resolvable from tag structure")
	assert.Nil(t, v, "an ambiguous reply yields no verdict to grade")
}

// TestVerdictFromAnswer_OnlyAfterTheCloserTakesTheSuffix is the unambiguous
// mid-thought resume: nothing before the closer parses, so there is no competing
// candidate and the suffix is the committed answer.
func TestVerdictFromAnswer_OnlyAfterTheCloserTakesTheSuffix(t *testing.T) {
	t.Parallel()
	answer := `still weighing whether the guard holds` + "\n" +
		`</think>` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, ambiguous := verdictFromAnswer(answer)

	require.False(t, ambiguous)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "REAL", v.Notes)
}

// TestVerdictFromAnswer_TrailingProseNamingCloserKeepsItsVerdict is the
// no-regression half, and the reason the lane cannot skip to the suffix
// unconditionally. The envelope comes FIRST and the closer appears in prose
// after it, so the suffix holds no verdict and the whole answer must still be
// read — the regression the 2026-09-30 reversal was written to remove.
func TestVerdictFromAnswer_TrailingProseNamingCloserKeepsItsVerdict(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"confirmed","reasoning":"REAL"}` + "\n" +
		`Note: SplitThink never scans for a bare </think>.`

	v, ambiguous := verdictFromAnswer(answer)

	require.False(t, ambiguous)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "REAL", v.Notes)
}

// TestVerdictFromAnswer_CloserInsideAJSONStringIsNotABoundary pins the masking
// interaction. The closer sits inside a string VALUE — the skeptic quoting the
// tag while judging think-handling code, the likeliest input in this repo — so
// maskJSONStrings blanks it and the single envelope stands.
func TestVerdictFromAnswer_CloserInsideAJSONStringIsNotABoundary(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"confirmed","reasoning":"the strip never looks for </think>"}`

	v, ambiguous := verdictFromAnswer(answer)

	require.False(t, ambiguous)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
}

// TestVerdictFromAnswer_NoCloserIsUnchanged guards the ordinary reply: the rule
// must be inert when the shape it keys on is absent.
func TestVerdictFromAnswer_NoCloserIsUnchanged(t *testing.T) {
	t.Parallel()
	v, ambiguous := verdictFromAnswer(`{"verdict":"refuted","reasoning":"legit"}`)

	require.False(t, ambiguous)
	require.NotNil(t, v)
	assert.Equal(t, verdictRefuted, v.Verdict)
	assert.Equal(t, "legit", v.Notes)
}

// --- executor lane ---

// TestExecutorFixFromAnswer_EnvelopeOnBothSidesIsRefused is the costlier half:
// before the fix parseExecutorResponse returned DRAFT-PATCH, and --auto-fix
// writes the returned patch to tracked source.
func TestExecutorFixFromAnswer_EnvelopeOnBothSidesIsRefused(t *testing.T) {
	t.Parallel()
	answer := `Reading the file. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	fix, ambiguous, err := executorFixFromAnswer(answer)

	assert.True(t, ambiguous, "two fix envelopes around an unopened closer name no committed patch")
	assert.NoError(t, err, "ambiguity is a refusal, not a parse error")
	assert.Empty(t, fix, "writing either patch would be a guess applied to tracked source")
}

// TestExecutorFixFromAnswer_OnlyAfterTheCloserTakesTheSuffix is the unambiguous
// resume on this lane.
func TestExecutorFixFromAnswer_OnlyAfterTheCloserTakesTheSuffix(t *testing.T) {
	t.Parallel()
	answer := `weighing two approaches` + "\n" +
		`</think>` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	fix, ambiguous, err := executorFixFromAnswer(answer)

	require.False(t, ambiguous)
	require.NoError(t, err)
	assert.Equal(t, "REAL-PATCH", fix)
}

// TestExecutorFixFromAnswer_TrailingProseNamingCloserKeepsItsFix is the
// executor's no-regression half — the shape the sprint's HasEnclosingThinkBlock
// switch was written to stop refusing. Losing the repair here is worse than in
// the verify lane, because the fix is dropped entirely.
func TestExecutorFixFromAnswer_TrailingProseNamingCloserKeepsItsFix(t *testing.T) {
	t.Parallel()
	answer := `{"fix":"REAL-PATCH","explanation":"real"}` + "\n" +
		`I left the bare </think> handling alone.`

	fix, ambiguous, err := executorFixFromAnswer(answer)

	require.False(t, ambiguous)
	require.NoError(t, err)
	assert.Equal(t, "REAL-PATCH", fix)
}

// TestExecutorFixFromAnswer_NoCloserIsUnchanged guards inertness on the
// ordinary reply.
func TestExecutorFixFromAnswer_NoCloserIsUnchanged(t *testing.T) {
	t.Parallel()
	fix, ambiguous, err := executorFixFromAnswer(`{"fix":"ONLY-PATCH","explanation":"x"}`)

	require.False(t, ambiguous)
	require.NoError(t, err)
	assert.Equal(t, "ONLY-PATCH", fix)
}
