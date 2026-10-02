package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reply that carries a bare </think> — one no <think> opened — is a reply that
// may have started mid-thought, because a chat template put the opener in the
// prompt. llmclient.SplitThink deliberately leaves such a closer in place
// (decided 2026-09-30) and HasEnclosingThinkBlock deliberately does not refuse
// on it (a lone closer encloses nothing). Both decisions are right on their own
// terms, and together they left the object BEFORE the closer — the draft the
// model abandoned — as the first one a first-match parser reaches.
//
// These tests pin the committed object on both lanes, and pin the shape that
// must NOT be re-broken: a reply that merely NAMES the closer in trailing prose
// still has its real envelope before it, and refusing or skipping that one is
// the regression the 2026-09-30 reversal was written to remove.

// TestParseVerdict_BareCloserDraftDoesNotWin is the skeptic half. Before the
// fix this returned `refuted` — the draft — which internal/reconcile/gate.go
// treats as "a skeptic disproved it" at ANY severity, so a real CRITICAL finding
// silently stops failing CI.
func TestParseVerdict_BareCloserDraftDoesNotWin(t *testing.T) {
	t.Parallel()
	answer := `Checking the call sites. {"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v := verdictFromAnswer(answer)

	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict,
		"the committed verdict follows the closer; the draft before it was abandoned")
	assert.Equal(t, "REAL", v.Notes)
}

// TestParseVerdict_TrailingProseNamingCloserKeepsItsVerdict is the
// no-regression half, and it is the reason the lane cannot simply skip to the
// suffix unconditionally. Here the envelope comes FIRST and the closer appears
// in prose after it, so the suffix holds no verdict at all and the whole answer
// must still be parsed.
func TestParseVerdict_TrailingProseNamingCloserKeepsItsVerdict(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"confirmed","reasoning":"REAL"}` + "\n" +
		`Note: SplitThink never scans for a bare </think>.`

	v := verdictFromAnswer(answer)

	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict,
		"a closer in trailing prose is not a mid-thought resume — the verdict before it is committed")
	assert.Equal(t, "REAL", v.Notes)
}

// TestParseVerdict_CloserInsideAJSONStringIsNotABoundary pins the masking
// interaction. The closer here sits inside a string VALUE, which is the skeptic
// quoting the tag while judging think-handling code — the likeliest input in
// this repo. maskJSONStrings blanks it, so it is not a boundary and the single
// envelope stands.
func TestParseVerdict_CloserInsideAJSONStringIsNotABoundary(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"confirmed","reasoning":"the strip never looks for </think>"}`

	v := verdictFromAnswer(answer)

	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
}

// TestParseExecutorAnswer_BareCloserDraftDoesNotWin is the executor half, and
// the more expensive failure: the draft is a PATCH, and --auto-fix writes it to
// tracked source. Before the fix parseExecutorResponse returned DRAFT-PATCH.
func TestParseExecutorAnswer_BareCloserDraftDoesNotWin(t *testing.T) {
	t.Parallel()
	answer := `Reading the file. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	fix, err := executorFixFromAnswer(answer)

	require.NoError(t, err)
	assert.Equal(t, "REAL-PATCH", fix,
		"the committed patch follows the closer; writing the draft would patch tracked source wrongly")
}

// TestParseExecutorAnswer_TrailingProseNamingCloserKeepsItsFix is the executor's
// no-regression half — the shape the sprint's HasEnclosingThinkBlock switch was
// written to stop refusing.
func TestParseExecutorAnswer_TrailingProseNamingCloserKeepsItsFix(t *testing.T) {
	t.Parallel()
	answer := `{"fix":"REAL-PATCH","explanation":"real"}` + "\n" +
		`I left the bare </think> handling alone.`

	fix, err := executorFixFromAnswer(answer)

	require.NoError(t, err)
	assert.Equal(t, "REAL-PATCH", fix)
}
