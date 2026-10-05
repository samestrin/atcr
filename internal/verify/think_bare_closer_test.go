package verify

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
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
// closerTag returns the bare think-closer tag as a string literal, kept in one
// place so a test fixture cannot silently lose it to an editor or a tool that
// treats the raw tag as markup.
func closerTag() string { return "\u003c/think\u003e" }

func TestVerdictFromAnswer_EnvelopeOnBothSidesIsRefused(t *testing.T) {
	t.Parallel()
	answer := `Checking the call sites. {"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, ambiguous, _ := verdictFromAnswer(answer)

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

	v, ambiguous, _ := verdictFromAnswer(answer)

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

	v, ambiguous, _ := verdictFromAnswer(answer)

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

	v, ambiguous, _ := verdictFromAnswer(answer)

	require.False(t, ambiguous)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
}

// TestVerdictFromAnswer_NoCloserIsUnchanged guards the ordinary reply: the rule
// must be inert when the shape it keys on is absent.
func TestVerdictFromAnswer_NoCloserIsUnchanged(t *testing.T) {
	t.Parallel()
	v, ambiguous, _ := verdictFromAnswer(`{"verdict":"refuted","reasoning":"legit"}`)

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
// (TD internal/verify/invoke.go:218, internal/verify/executor.go:804).

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

	_, verdictAmbiguous, _ := verdictFromAnswer(verdictAnswer)
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

// --- the THIRD "nothing usable here" diagnostic the predicate missed ---
//
// carriesVerdict excluded two of parseVerdict's three such diagnostics —
// empty_response and malformed_output: — but not invalid_verdict:, which
// parseVerdict returns for an object that HAS a verdict key holding a value
// outside the enum (verdict.go:65). So an out-of-enum verdict object counted as
// an envelope, and the suffix it sat in looked committed.
//
// The cost is availability, in the one direction that matters most in this repo:
// a reply about think handling. A real confirmed verdict, prose naming </think>,
// and an out-of-enum example quoted after it is a reply this codebase's own
// reviewers produce — and it collapsed to AMBIGUOUS, which gate.go:110 then
// passes over under --require-verified (TD internal/verify/invoke.go:701).

// TestCarriesVerdict_OutOfEnumVerdictIsNotAnEnvelope pins the predicate directly,
// one case per diagnostic, so the clause cannot be removed without a failure that
// names which diagnostic leaked.
func TestCarriesVerdict_OutOfEnumVerdictIsNotAnEnvelope(t *testing.T) {
	t.Parallel()

	assert.False(t, carriesVerdict(`{"verdict":"maybe"}`),
		"an out-of-enum verdict is parseVerdict's invalid_verdict: diagnostic, not a usable verdict")
	assert.False(t, carriesVerdict(""),
		"empty_response is not an envelope")
	assert.False(t, carriesVerdict(`no object here at all`),
		"malformed_output: is not an envelope")
	// TD internal/verify/think_bare_closer_test.go:343: the assertion above is
	// guaranteed by carriesVerdict's own no-brace early return and never reaches
	// parseVerdict, so the malformed_output: clause could be dropped with the suite
	// green. These two DO reach the parser and come back malformed_output: — a
	// keyless balanced object and a brace-only object — so the clause is pinned.
	assert.False(t, carriesVerdict(`{"file":"a.go","line":1}`),
		"a parseable object with no verdict key is parseVerdict's malformed_output: — not an envelope")
	assert.False(t, carriesVerdict(`{}`),
		"a brace-only object is malformed_output: too — not an envelope")
	assert.True(t, carriesVerdict(`{"verdict":"confirmed","reasoning":"REAL"}`),
		"a real verdict IS an envelope — the predicate must not have been widened into refusing everything")
}

// TestVerdictFromAnswer_OutOfEnumExampleAfterTheCloserKeepsTheVerdict is the
// proven input, graded end to end through the shared rule. Before the fix the
// suffix's out-of-enum example counted as an envelope, both sides carried one,
// and the lane refused a verdict it had already been given.
//
// The assertion direction is the whole safety argument for this change: the fix
// can only narrow the AMBIGUOUS classification, so it can only turn refusals back
// into graded verdicts — never a graded verdict into a refusal.
func TestVerdictFromAnswer_OutOfEnumExampleAfterTheCloserKeepsTheVerdict(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"confirmed","reasoning":"REAL"}` + "\n" +
		`A reply that ends on </think> began mid-thought.` + "\n" +
		`An out-of-enum verdict is written {"verdict":"maybe"} and parses to nothing usable.`

	v, ambiguous, _ := verdictFromAnswer(answer)

	require.False(t, ambiguous,
		"the suffix holds no usable verdict, so the tag structure is whole-answer and nothing is ambiguous")
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict,
		"the committed verdict was already given; quoting an invalid one after a bare closer must not withdraw it")
	assert.Equal(t, "REAL", v.Notes,
		"the graded verdict must be the real envelope's, not the quoted example's")
}

// TestVerdictFromAnswer_UnusableSuffixFallsBackToWholeAnswerForAllThree records
// the residual this change INHERITS rather than creates, because the tag
// structure of the shape it recovers is identical to a shape it cannot:
//
//	{real} … prose "</think>" {bad example}   → recovered (the point of the fix)
//	{draft} </think> {bad}                    → grades the draft (the residual)
//
// Once a suffix holds no usable verdict, ClassifyUnopenedCloser returns
// SectionWholeAnswer by construction and parseVerdict reads the answer end to
// end — so a draft before the closer is graded. The empty_response and
// malformed_output rows are INHERITED: that was already true of them before this
// change. The invalid_verdict row is NOT — it is a behaviour change this change
// makes. Before the third clause existed, an out-of-enum object DID count as an
// envelope, so a draft before the closer plus a quoted out-of-enum example after it
// and the gate blocks on unverifiable); after it, the suffix carries no envelope,
// the whole answer is read, and the draft is graded refuted (which never blocks).
// Inherited and introduced rows are asserted together only so the reader can see
// the set at a glance; read the invalid_verdict row as the one this diff moved
// (TD internal/verify/invoke.go:766 holds the behaviour decision, and
// invoke.go's relaxation note lists only the benign direction).
//
// Separating the two shapes is not possible at this layer: they differ only in
// whether the pre-closer text was abandoned, which nothing in the tag structure
// records — the same accepted loss SplitThink's own doc states.
func TestVerdictFromAnswer_UnusableSuffixFallsBackToWholeAnswerForAllThree(t *testing.T) {
	t.Parallel()
	draft := `{"verdict":"refuted","reasoning":"DRAFT"}` + "\n</think>\n"

	for name, suffix := range map[string]string{
		"invalid_verdict":  `{"verdict":"maybe"}`,
		"malformed_output": `not an object at all`,
		"empty_response":   ``,
	} {
		// TD internal/verify/think_bare_closer_test.go:396: map iteration order is
		// randomised and require.* aborts the whole test, so without a subtest only
		// ONE regressed diagnostic would ever be reported. t.Run evaluates each row
		// independently and reports them all.
		t.Run(name, func(t *testing.T) {
			v, ambiguous, _ := verdictFromAnswer(draft + suffix)

			require.False(t, ambiguous, name+": an unusable suffix carries no envelope, so nothing is ambiguous")
			require.NotNil(t, v)
			assert.Equal(t, verdictRefuted, v.Verdict,
				name+": the whole-answer fallback reads the pre-closer text — identical across all three diagnostics")
		})
	}
}

// TestVerdictFromAnswer_ADiagnosticNoteNeverQuotesOnlyTheFragment is the
// invariant that closed TD internal/verify/invoke.go:683 without a disclosure
// string, and the test that keeps it closed.
//
// The filed defect: on sectionAfterCloser, parseVerdict sees only the suffix, so
// an `invalid_verdict: X (raw: …)` note embedded only the post-closer fragment
// while the prefix was kept nowhere — and docs/verification.md says the skeptic
// lane keeps removed text "nowhere", so that note was the only record. An
// operator reading it concluded the fragment was the whole reply.
//
// Excluding invalid_verdict: from carriesVerdict closed it by CONSTRUCTION, which
// is why no disclosure was added: a note explaining a truncation that can no
// longer happen is the enrichment-that-never-fires that gate.go:210 warns reads,
// in review, as one that works. All three of parseVerdict's "nothing usable here"
// diagnostics are now excluded, so sectionAfterCloser is reachable ONLY when the
// suffix holds a real verdict — and a real verdict carries no raw embed at all.
//
// The coupling is the fragile part, so it is asserted rather than commented: if a
// future change lets ANY diagnostic count as an envelope again, the fragment-
// quoting note comes straight back and this test is what says so.
func TestVerdictFromAnswer_ADiagnosticNoteNeverQuotesOnlyTheFragment(t *testing.T) {
	t.Parallel()
	const prefix = "weighing two approaches"

	// Every shape whose suffix is unusable must route to the WHOLE answer, so the
	// raw embed is complete — it still contains the pre-closer text.
	for name, suffix := range map[string]string{
		"invalid_verdict":  `{"verdict":"maybe"}`,
		"malformed_output": `no object here`,
		"empty_response":   ``,
	} {
		t.Run(name, func(t *testing.T) {
			answer := prefix + "\n response\n" + suffix
			section, _ := classifyUnopenedCloser(answer, carriesVerdict)
			v, _, _ := verdictFromAnswer(answer)

			require.Equal(t, sectionWholeAnswer, section,
				name+": an unusable suffix is not an envelope, so the whole answer is read")
			require.NotNil(t, v)
			assert.Contains(t, v.Notes, prefix,
				name+": the raw embed must quote the COMPLETE reply, never the post-closer fragment alone")
		})
	}

	// TD internal/verify/think_bare_closer_test.go:430: the invariant claimed above
	// was false at HEAD and passed only because this table omitted the coupling
	// breaker. A suffix holding a decoy out-of-enum object FOLLOWED BY a real verdict
	// reaches sectionAfterCloser — the real verdict is usable, so the section is the
	// suffix — and parseVerdict must grade the real verdict rather than embed a
	// fragment-quoting diagnostic for the decoy. Asserted here so the invariant the
	// comment claims is actually pinned.
	{
		suffix := `An example is {"verdict":"maybe"}.` + "\n" + `{"verdict":"confirmed","reasoning":"REAL"}`
		answer := prefix + "\n" + closerTag() + "\n" + suffix
		section, text := classifyUnopenedCloser(answer, carriesVerdict)
		require.Equal(t, sectionAfterCloser, section,
			"a usable verdict behind a decoy in the suffix IS the committed section")
		parsed, _ := parseVerdict(text)
		assert.Equal(t, verdictConfirmed, parsed.Verdict,
			"the committed verdict, not the decoy, must be graded")
		assert.NotContains(t, parsed.Notes, "(raw:",
			"a real verdict carries no fragment-quoting raw embed")
	}

	// And the one shape that DOES reach sectionAfterCloser carries no diagnostic to
	// truncate: a usable verdict's Notes is the model's reasoning, not a raw embed.
	answer := prefix + "\n</think>\n" + `{"verdict":"confirmed","reasoning":"REAL"}`
	section, _ := classifyUnopenedCloser(answer, carriesVerdict)
	v, ambiguous, _ := verdictFromAnswer(answer)

	require.Equal(t, sectionAfterCloser, section, "a real verdict after a lone closer IS the committed section")
	require.False(t, ambiguous)
	assert.Equal(t, "REAL", v.Notes)
	assert.NotContains(t, v.Notes, "(raw:",
		"the only section parsed from a fragment is the one that carries a real verdict, which has no raw embed")
	assert.NotContains(t, v.Notes, "invalid_verdict:")
	assert.NotContains(t, v.Notes, "malformed_output:")
}

// TestCarriesVerdict_RecoveredVerdictReachesTheGateAsItself is the consumer trace
// for the T1 change, and it records the one consequence the risk analysis did not:
// narrowing AMBIGUOUS does not only relax the gate in the strict direction.
//
// IsFailing (internal/reconcile/gate.go:96) is the consumer. Under the DEFAULT
// gate (requireVerified=false) an `unverifiable` finding at or above the threshold
// BLOCKS, while a `refuted` one never does. So for a reply whose real verdict was
// refuted, recovering it FLIPS a blocking finding to a non-blocking one — a
// relaxation, not a tightening.
//
// That is the correct outcome and the point of the fix: the skeptic disproved the
// finding, and the old behaviour blocked CI on a verdict it had refused to read.
// But "it can only turn refusals back into graded verdicts" reads as though the
// gate can only get stricter, and on this path it does not. Asserted here so the
// direction is a recorded decision rather than a surprise in a later review.
func TestCarriesVerdict_RecoveredVerdictReachesTheGateAsItself(t *testing.T) {
	t.Parallel()
	const quoted = "\nAn out-of-enum example is written {\"verdict\":\"maybe\"}, which parses to nothing.\n"

	// The baseline, stated once rather than re-asserted per row: whatever the real
	// verdict was, the pre-fix AMBIGUOUS collapse graded it `unverifiable`, and the
	// default gate BLOCKS on that. It is asserted here because it is what makes the
	// per-row results below a change rather than a description — but it does not
	// depend on the rows, so folding it into the loop would only make it look like it
	// did.
	require.True(t,
		reconcile.IsFailing("HIGH", "", &reconcile.Verification{Verdict: verdictUnverifiable}, "MEDIUM", false),
		"the pre-fix collapse graded unverifiable, which the default gate blocks on — the baseline both rows move from")

	for _, tc := range []struct {
		verdict        string
		blocksAfter    bool
		blocksVerified bool
		why            string
	}{
		{"confirmed", true, true, "a recovered confirmed still blocks — and is the only verdict that blocks under --require-verified"},
		{"refuted", false, false, "a recovered refuted STOPS blocking: the skeptic disproved the finding, which is what the gate is told to honour"},
	} {
		answer := `{"verdict":"` + tc.verdict + `","reasoning":"REAL"}` +
			"\nA reply ending on </think> began mid-thought." + quoted

		v, ambiguous, _ := verdictFromAnswer(answer)

		require.False(t, ambiguous, tc.verdict+": the quoted example is not an envelope")
		require.NotNil(t, v)
		require.Equal(t, tc.verdict, v.Verdict, tc.verdict+": the real verdict must be recovered intact")

		assert.Equal(t, tc.blocksAfter,
			reconcile.IsFailing("HIGH", "", v, "MEDIUM", false), tc.why)
		// TD internal/verify/think_bare_closer_test.go:501: the row comment claimed a
		// --require-verified property that no assertion tested. Under the strict gate
		// only a CONFIRMED finding counts, so the refuted row must stop blocking there
		// too — which is the direction narrowing AMBIGUOUS actually moves.
		assert.Equal(t, tc.blocksVerified,
			reconcile.IsFailing("HIGH", "", v, "MEDIUM", true),
			tc.verdict+": the --require-verified gate counts only confirmed findings")
	}
}

// --- the predicate must ITERATE, not judge the first object ---
//
// Excluding invalid_verdict: is only half an envelope test. parseVerdict
// short-circuits on the FIRST object carrying a verdict key — in-enum or not — so
// delegating the whole question to it answers "is the first verdict-keyed object
// usable", not "does this text hold a usable verdict". A post-closer section whose
// committed verdict sits BEHIND a quoted out-of-enum example therefore read as
// empty, classifyUnopenedCloser fell back to the whole answer, and the abandoned
// pre-closer draft was graded.
//
// That is strictly worse than the bug the exclusion fixed: a `refuted` draft never
// blocks CI (reconcile.IsFailing:104), so a disclosed AMBIGUOUS refusal was
// replaced by a silently wrong verdict — the one outcome classifyUnopenedCloser's
// doc says the rule exists to prevent.
//
// It is also the SAME defect, in the same predicate pair, that the round-2 blocker
// closed on the executor lane (TD internal/verify/executor.go:916): a decoy object
// in front of the real envelope made the committed section look empty. The doc's
// "Both predicates iterate now" is the invariant, and these tests are what hold the
// skeptic half of it.

// TestCarriesVerdict_IteratesPastAnOutOfEnumObject pins the predicate directly.
func TestCarriesVerdict_IteratesPastAnOutOfEnumObject(t *testing.T) {
	t.Parallel()

	assert.True(t, carriesVerdict(`An example is {"verdict":"maybe"}, and my answer is `+
		`{"verdict":"confirmed","reasoning":"REAL"}`),
		"an out-of-enum object in FRONT of the real verdict must not make the section look empty")
	assert.True(t, carriesVerdict(`{"file":"a.go","line":1}`+"\n"+`{"verdict":"confirmed","reasoning":"R"}`),
		"nor must a decoy object with no verdict key — the executor lane's proven shape")
	assert.False(t, carriesVerdict(`{"verdict":"maybe"}`+"\n"+`{"verdict":"also-bad"}`),
		"but a section holding ONLY unusable objects still carries no envelope")
	// TD internal/verify/invoke.go:727 (testing): the unbalanced-brace advance had
	// no test — every iteration case above used balanced objects, so the
	// `rest = rest[IndexByte(rest,'{')+1:]` branch could be deleted with the suite
	// green. An unbalanced leading brace must not hide the verdict behind it.
	assert.True(t, carriesVerdict(`here is an unbalanced { brace followed by `+
		`{"verdict":"confirmed","reasoning":"REAL"}`),
		"an unbalanced leading brace must be stepped past, not swallow the verdict after it")
	assert.False(t, carriesVerdict(`an unbalanced { brace and nothing else`),
		"and an unbalanced brace with no verdict behind it still carries no envelope")
}

// TestVerdictFromAnswer_DecoyBeforeTheRealVerdictIsStillAnEnvelope is the shape
// end to end: both sides carry an envelope, so the lane must refuse rather than
// grade either one.
func TestVerdictFromAnswer_DecoyBeforeTheRealVerdictIsStillAnEnvelope(t *testing.T) {
	t.Parallel()
	answer := `{"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`An out-of-enum example is {"verdict":"maybe"}.` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, ambiguous, _ := verdictFromAnswer(answer)

	assert.True(t, ambiguous,
		"a quoted example in front of the committed verdict must not collapse the section to empty")
	assert.Nil(t, v, "a refusal returns no verdict")
	if v != nil {
		assert.NotEqual(t, verdictRefuted, v.Verdict,
			"grading the abandoned draft is the durable damage: a draft refuted never blocks the gate")
	}
}

// TestBothLanesAgreeOnTheOutOfEnumDecoyShape is the drift guard for the doc claim
// that both predicates iterate. The executor half was already pinned by
// TestExecutorFixFromAnswer_DecoyObjectBeforeTheFixIsStillAnEnvelope; this asserts
// the skeptic half reaches the same answer on the byte-equivalent shape, so the two
// cannot diverge again without a failure that says so.
func TestBothLanesAgreeOnTheOutOfEnumDecoyShape(t *testing.T) {
	t.Parallel()
	verdictAnswer := `{"verdict":"refuted","reasoning":"DRAFT"}` + "\n" +
		`</think>` + "\n" +
		`An example is {"verdict":"maybe"}.` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`
	fixAnswer := `{"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"file":"internal/auth/token.go","line":42}` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`

	_, verdictAmbiguous, _ := verdictFromAnswer(verdictAnswer)
	_, fixAmbiguous, _ := executorFixFromAnswer(fixAnswer)

	assert.True(t, fixAmbiguous, "the executor lane iterates past a decoy — the round-2 fix")
	assert.Equal(t, fixAmbiguous, verdictAmbiguous,
		"and the skeptic lane must too, or classifyUnopenedCloser's shared-invariant doc is false")
}

// TestInvokeSkeptic_AfterCloserGradeRecordsTheDiscardedPrefix closes the half of
// TD internal/verify/invoke.go:683 that the carriesVerdict narrowing did NOT close.
//
// Excluding invalid_verdict: removed the misleading diagnostic — a note can no
// longer quote a fragment as if it were the whole reply. It did nothing about the
// silence: a grade taken from the post-closer section dropped everything ahead of
// it with no note and no log line, so an operator could not tell that grade from
// one read end to end. The text is deliberately not retained (it is a verdict the
// model withdrew), so the length is the record.
func TestInvokeSkeptic_AfterCloserGradeRecordsTheDiscardedPrefix(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(),
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cc := finalChat("weighing two approaches\n</think>\n" + `{"verdict":"confirmed","reasoning":"REAL"}`)

	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher(), false)

	require.NoError(t, err)
	require.NotNil(t, v)
	require.Equal(t, verdictConfirmed, v.Verdict, "the committed verdict is after the closer and must be graded")

	// TD internal/verify/think_bare_closer_test.go:623: the magnitude was unpinned —
	// only the zero/non-zero split was guarded, so a 1000x-wrong count rode through
	// green. The fixture is "weighing two approaches\n" + the closer + "\n" + the
	// envelope, so exactly 32 prefix bytes are dropped. Asserted against
	// verdictFromAnswer directly AND in the log.
	_, _, discarded := verdictFromAnswer("weighing two approaches\n</think>\n" + `{"verdict":"confirmed","reasoning":"REAL"}`)
	assert.Equal(t, 32, discarded,
		"the dropped prefix length must be exact, not merely non-zero")

	out := buf.String()
	assert.Contains(t, out, "verdict_after_unopened_closer",
		"the split must leave a record, or a fragment-sourced grade is indistinguishable from a whole-answer one")
	assert.Contains(t, out, "discarded_prefix_bytes=32",
		"and the record must say exactly how much was dropped, since the text itself is kept nowhere")
	assert.NotContains(t, out, "level=WARN msg=\"skeptic answer taken after a bare closer\"",
		"this is a successful grade on a legitimate reply shape — warning on it would train the reader to ignore the class")
}

// And the complement: an answer read end to end must NOT claim a discarded prefix,
// or the record above becomes noise on every reply.
func TestInvokeSkeptic_WholeAnswerGradeRecordsNoDiscard(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(),
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cc := finalChat(`{"verdict":"confirmed","reasoning":"REAL"}`)

	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher(), false)

	require.NoError(t, err)
	require.Equal(t, verdictConfirmed, v.Verdict)
	assert.NotContains(t, buf.String(), "verdict_after_unopened_closer",
		"no closer, nothing discarded — the record must not fire on an ordinary reply")
}

// TD internal/verify/invoke.go:781 (observability): discardedPrefix — the only
// record that a grade was taken from a post-closer fragment with the prefix
// dropped — reached nobody. It was emitted solely via a logger.Debug line that is
// suppressed at the default level, and deliberately NOT written onto the returned
// Verification, so an operator reading verification.json, findings.json or
// report.md could not distinguish a whole-answer grade from a fragment grade —
// the exact gap TD invoke.go:683 filed. The discard must survive into the
// artifact, not just a log channel that is off by default.
func TestInvokeSkeptic_FragmentGradeIsRecordedOnTheVerification(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	// Nothing usable before the closer, so the section is unambiguously the suffix.
	answer := `still weighing whether the guard holds` + "\n" +
		`A reply that resumed mid-thought.` + "\u003c/think\u003e" + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", finalChat(answer), okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	require.Equal(t, verdictConfirmed, v.Verdict, "the committed verdict after the closer is graded")
	assert.Contains(t, v.Notes, "after_unopened_closer",
		"the fragment grade must be recorded on the durable Verification, not only at Debug level")
	assert.Contains(t, v.Notes, "REAL",
		"and the graded reasoning itself stays readable beside the marker")
}
