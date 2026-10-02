package verify

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
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

// --- the production arms, driven end to end ---
//
// Every test above calls verdictFromAnswer / executorFixFromAnswer directly, so
// each proves the RULE and none proves the ARM that ships. The two tests below
// drive invokeSkeptic and invokeExecutor themselves, because what a consumer
// actually reads is decided there and nowhere else: the logged failure class, the
// Notes token every downstream reader keys on, and — on the executor lane — the
// empty fix that stops --auto-fix writing a draft patch to tracked source.
//
// That gap was not theoretical. Neutralising either `if ambiguous {` arm left
// `go test ./...` green across the whole repo, which is the same shape as the row
// filed against internal/debate/debate.go:308: a test that re-proves the
// predicate beside the branch instead of through it
// (TD internal/verify/invoke.go:207, internal/verify/executor.go:804).

// TestInvokeSkeptic_AmbiguousUnopenedCloserRefuses pins the skeptic arm through
// the production path. The reply has to clear three earlier guards to reach it,
// and does: StatusOK and not truncated (the fake answers in one clean turn), not
// salvaged, and HasEnclosingThinkBlock false because a lone closer encloses
// nothing. Grading the draft here is durable damage — a draft `refuted` clears
// internal/reconcile/gate.go at any severity and is charged to the reviewer's
// survived_skeptic_rate — so the arm's disclosure is the deliverable, not an
// implementation detail.
func TestInvokeSkeptic_AmbiguousUnopenedCloserRefuses(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	cc := finalChat(`Checking the call sites. {"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`)

	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher(), false)

	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict,
		"neither envelope is provably committed, so the lane must grade neither")
	assert.Equal(t, "ambiguous_unopened_closer", v.Notes,
		"the Notes token is the only record of WHY this collapsed; gate.go and the scorecard read it")
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.NotEqual(t, verdictRefuted, v.Verdict,
		"the draft verdict must never reach the gate as a disproof")

	out := buf.String()
	assert.Contains(t, out, "class=ambiguous_unopened_closer",
		"the refusal must be disclosed under its own class, not folded into a generic failure")
}

// TestInvokeExecutor_AmbiguousUnopenedCloserRefuses pins the executor arm through
// the production path. The empty fix is the load-bearing half: invokeExecutor's
// only other empty-fix returns are a parse error and a provider failure, and a
// non-empty return here would be a patch written to tracked source under
// --auto-fix.
func TestInvokeExecutor_AmbiguousUnopenedCloserRefuses(t *testing.T) {
	t.Parallel()
	cc := finalChat(`Reading the file. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`)

	fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], cc, okDispatcher(), 0, "")

	assert.Empty(t, fix, "applying either patch would be a guess written to tracked source")
	assert.NotEqual(t, "DRAFT-PATCH", fix, "the abandoned draft must never be returned as the patch")
	assert.Contains(t, warn, "agent_mode refused",
		"the decline must read as a refusal, not as a parse error or a provider failure")
	assert.Contains(t, warn, "both sides",
		"the warning must name the ambiguity so an operator knows the reply shape caused it")
}

// --- the envelope predicate, which classifyUnopenedCloser does NOT own ---
//
// classifyUnopenedCloser is shared by both lanes, but the `hasEnvelope` function
// it takes is not, and that is where the two lanes diverged. The skeptic's
// parseVerdict ITERATES candidate balanced objects for a `verdict` key;
// parseExecutorResponse took only the first object with no key filter. So an
// executor reply whose committed section opens with a location or plan object
// before the patch read as "no envelope here", classifyUnopenedCloser fell back
// to sectionWholeAnswer, and the ABANDONED DRAFT was returned as the patch —
// written to tracked source under --auto-fix
// (TD internal/verify/executor.go:916).

// TestExecutorFixFromAnswer_DecoyObjectBeforeTheFixIsStillAnEnvelope is the
// proven defect input. A location object between the closer and the patch is as
// ordinary a reply shape as the draft-before-closer one the rule was built for,
// and the byte-identical shape on the skeptic lane was already refused — so this
// is the same defect the round-2 blocker named, closed only where it was pointed.
func TestExecutorFixFromAnswer_DecoyObjectBeforeTheFixIsStillAnEnvelope(t *testing.T) {
	t.Parallel()
	answer := `Reading the file. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"file":"internal/auth/token.go","line":42}` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	fix, ambiguous, err := executorFixFromAnswer(answer)

	assert.True(t, ambiguous,
		"a decoy object in front of the patch must not make the committed section look empty")
	assert.NoError(t, err)
	assert.Empty(t, fix)
	assert.NotEqual(t, "DRAFT-PATCH", fix,
		"the abandoned draft is the one patch that must never reach tracked source")
}

// TestBothLanesAgreeOnTheDecoyShape states the symmetry the two "cannot drift"
// comments assert. Same tag structure, same decoy, one lane each — if the
// predicates ever diverge again this is the test that says so.
func TestBothLanesAgreeOnTheDecoyShape(t *testing.T) {
	t.Parallel()
	verdictAnswer := `Checking. {"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`{"file":"internal/auth/token.go","line":42}` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`
	fixAnswer := `Checking. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"file":"internal/auth/token.go","line":42}` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	_, verdictAmbiguous := verdictFromAnswer(verdictAnswer)
	_, fixAmbiguous, _ := executorFixFromAnswer(fixAnswer)

	assert.True(t, verdictAmbiguous, "skeptic lane refuses the decoy shape")
	assert.Equal(t, verdictAmbiguous, fixAmbiguous,
		"the lanes share classifyUnopenedCloser but not the envelope predicate; they must still agree")
}

// TestExecutorFixFromAnswer_DecoyBeforeTheFixOnACleanResumeKeepsTheFix is the
// availability half, and the reason the predicate had to gain iteration rather
// than the caller gaining a special case. Nothing precedes the closer, so there
// is no competing candidate: the committed section holds a plan object AND the
// patch, and the patch must be found rather than dropped as "missing fix field".
func TestExecutorFixFromAnswer_DecoyBeforeTheFixOnACleanResumeKeepsTheFix(t *testing.T) {
	t.Parallel()
	answer := `weighing two approaches` + "\n" +
		`</think>` + "\n" +
		`{"file":"internal/auth/token.go","line":42}` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	fix, ambiguous, err := executorFixFromAnswer(answer)

	require.False(t, ambiguous)
	require.NoError(t, err)
	assert.Equal(t, "REAL-PATCH", fix,
		"a plan object in front of the patch must not cost the repair")
}
