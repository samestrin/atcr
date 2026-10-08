package verify

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent-mode refusal is a CONTENT-SHAPE decline, and until this change
// postCheck had no way to say so. invokeExecutor's two refusals — its
// HasEnclosingThinkBlock arm (think markup that survived the strip) and its
// executorFixFromAnswer ambiguous arm (a bare </think> with a fix envelope on both
// sides), the two sites that open their warn with agentRefusalPrefix — come back with
// salvaged=false, so they bypassed the salvage arm and landed in the generic
// `if warn != "" {` branch. Two consequences, both tested below:
//
//  1. the refusal was logged as executor_fix_failed, a class the code documents
//     as a provider/transport error, for a reply that arrived intact;
//  2. that branch alone carries no hasAnyFixAttribution guard — unlike the
//     salvage, truncation, empty-completion and decline arms — so a later tier's
//     refusal stamped FixWarning over an earlier tier's generated Fix. That is
//     the "a good Fix never carries a FixWarning" invariant stated at
//     internal/reconcile/emit.go:158.
//
// (TD internal/verify/executor.go:377.)
//
// The reply shape below is the one TestInvokeExecutor_AmbiguousUnopenedCloserRefuses
// already proves reaches the ambiguous return: a lone closer encloses nothing, so
// HasEnclosingThinkBlock passes it through to executorFixFromAnswer.
func ambiguousAgentReply() string {
	return `Reading the file. {"fix":"DRAFT-PATCH","explanation":"draft"}` + "\n" +
		`</think>` + "\n" +
		`{"fix":"REAL-PATCH","explanation":"real"}`
}

// TestGenerateFixes_AgentRefusal_LogsItsOwnClass drives invokeExecutor through
// postCheck — not invokeExecutor alone — because the log class a consumer reads is
// decided in postCheck and nowhere else. The sibling salvage arm earned the same
// split for the same reason (TestGenerateFixes_SnippetSalvaged_LogsItsOwnClass).
func TestGenerateFixes_AgentRefusal_LogsItsOwnClass(t *testing.T) {
	ctx, buf := ceilingCtx()
	findings := eligibleFinding()

	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, finalChat(ambiguousAgentReply()), okDispatcher(), 0)

	out := buf.String()
	assert.Contains(t, out, "executor_agent_refused",
		"a refusal must be disclosed under its own class so an operator can tell it from a dead provider")
	assert.NotContains(t, out, "executor_fix_failed",
		"the reply arrived intact; reporting a content-shape decline as a provider/transport error is the collapse this split undoes")
	assert.NotContains(t, out, "executor_salvaged_reasoning",
		"the salvage class belongs to the snippet-path reasoning salvage and must not be reused for a refusal")
	assert.Contains(t, out, "a.go:1", "the detail must name the finding the refusal cost")
	assert.Empty(t, findings[0].Fix,
		"neither envelope is provably committed, so --auto-fix must have nothing to write")
}

// TestGenerateFixes_AgentRefusal_PreservesPriorTierFix is the cross-tier half.
// The finding already carries a fix an EARLIER tier generated, proved by the
// "fix by sonnet" attribution token — a different executor than this config's
// "opus", so the name-scoped pre-dispatch guard does not skip the finding and the
// refusal really does reach postCheck.
func TestGenerateFixes_AgentRefusal_PreservesPriorTierFix(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified,
		Fix:      "an earlier tier's good fix",
		Evidence: "Found by bruce; fix by sonnet",
	}}

	// TD internal/verify/executor_agent_refusal_test.go:68: every other assertion in
	// this test is an ABSENCE, and all of them hold if the finding is never
	// dispatched at all — mutation-proven, a changed pre-dispatch guard at
	// executor.go silently skipped the finding and the test still passed. Bind a
	// logger and assert the refusal was actually logged, so the test fails when the
	// finding never reaches postCheck.
	ctx, buf := ceilingCtx()
	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, finalChat(ambiguousAgentReply()), okDispatcher(), 0)

	require.Contains(t, buf.String(), "executor_agent_refused",
		"this test's whole premise is that the refusal REACHES postCheck — assert it did")

	f := findings[0]
	assert.Equal(t, "an earlier tier's good fix", f.Fix,
		"a later tier's refusal must not disturb a fix an earlier tier generated")
	assert.Empty(t, f.FixWarning,
		"'a good Fix never carries a FixWarning' (internal/reconcile/emit.go:158) — the refusal must not stamp one over it")
	assert.NotEqual(t, "REAL-PATCH", f.Fix, "the refused patch must never be adopted")
	assert.NotEqual(t, "DRAFT-PATCH", f.Fix, "and least of all the abandoned draft")
}

// TestGenerateFixes_AgentRefusal_WarnsWhenThereIsNoPriorFix is the availability
// half, and the reason the guard is hasAnyFixAttribution rather than f.Fix == "":
// with nothing to protect, the refusal MUST still be disclosed on the finding.
// A guard that silenced this case would trade a wrong warning for a silent one.
func TestGenerateFixes_AgentRefusal_WarnsWhenThereIsNoPriorFix(t *testing.T) {
	findings := eligibleFinding()

	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, finalChat(ambiguousAgentReply()), okDispatcher(), 0)

	f := findings[0]
	assert.Empty(t, f.Fix)
	assert.Contains(t, f.FixWarning, "agent_mode refused",
		"with no prior fix to protect, the refusal is the only record the finding carries")
	assert.Contains(t, f.FixWarning, "both sides",
		"and it must name the reply shape that caused it")
}

// TD internal/verify/executor.go:411: the refusal arm returns before the
// `if truncated` branch and discards the bool, yet invokeExecutor returns
// res.ResponseTruncated on BOTH refusal paths. A reply cut off on
// finish_reason=length carrying the ambiguous-closer shape therefore produced
// exactly one record — class=executor_agent_refused — with nothing saying the
// response was cut off, so the operator was told the model made a shape mistake
// when a token cap was the real cause. A truncated reply is a LIKELY producer of
// unbalanced think markup, so the two causes must both be observable.
func TestGenerateFixes_AgentRefusal_TruncationStaysObservable(t *testing.T) {
	ctx, buf := ceilingCtx()
	findings := eligibleFinding()

	cc := &fakeChatCompleter{turns: []chatTurn{{content: ambiguousAgentReply(), truncated: true}}}
	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, cc, okDispatcher(), 0)

	out := buf.String()
	assert.Contains(t, out, "executor_agent_refused",
		"the refusal class still names the reply shape")
	assert.Contains(t, out, "executor_truncated_fix",
		"the truncation that likely CAUSED the shape must be observable too, not discarded")

	f := findings[0]
	assert.Empty(t, f.Fix, "the refused patch is still not adopted")
	assert.Contains(t, f.FixWarning, "truncat",
		"the operator-facing warning must name the truncation, not present the shape mistake alone")
}

// resumedBlockReply is the HasEnclosingThinkBlock refusal shape: a stripped answer
// that still carries think markup outside a JSON string, produced when a resumed
// run leaves the draft envelope at the FRONT of the answer.
func resumedBlockReply() string {
	return "\u003cthink\u003eplanning\u003c/think\u003e" + "\n" +
		`{"fix":"DRAFT: delete the validation","explanation":"draft, wrong"}` + "\n" +
		"\u003cthink\u003eno wait\u003c/think\u003e" + "\n" +
		`{"fix":"REAL: add a bounds check","explanation":"real answer"}`
}

// TD internal/verify/executor.go:848 (testing): of invokeExecutor's two refusal
// sites, only the executorFixFromAnswer ambiguous arm was pinned. The
// HasEnclosingThinkBlock arm at :848 had ZERO enforcement: rewriting its return
// from agentRefusalPrefix + text to a literal string silently reclassified that
// site as executor_fix_failed (a provider/transport error) and the whole
// internal/verify suite still passed. This drives generateFixes — the only place
// the log class a consumer reads is decided — for THAT shape.
func TestGenerateFixes_ResumedBlockRefusal_LogsItsOwnClass(t *testing.T) {
	ctx, buf := ceilingCtx()
	findings := eligibleFinding()

	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, finalChat(resumedBlockReply()), okDispatcher(), 0)

	out := buf.String()
	assert.Contains(t, out, "executor_agent_refused",
		"the resumed-block refusal must be disclosed under its own class, not as a dead provider")
	assert.NotContains(t, out, "executor_fix_failed",
		"the reply arrived intact; a content-shape decline must not read as a provider/transport error")
	assert.Empty(t, findings[0].Fix,
		"the draft patch must never be adopted, so --auto-fix has nothing to write")
}

// TD internal/verify/executor.go:147: agentRefusalPrefix's doc states a rule with
// no mechanical enforcement — "Any NEW content-shape decline in invokeExecutor
// must open with this constant or it inherits the transport classification." A new
// decline written as a bare string would silently log as executor_fix_failed and
// lose the hasAnyFixAttribution guard, which is exactly the defect this epic fixed.
// The two current producers are correct by grep; the exposure is the next edit.
//
// Pinned rather than commented: every known content-shape decline shape must come
// back from invokeExecutor with a warn opening with agentRefusalPrefix.
func TestInvokeExecutor_ContentShapeDeclinesOpenWithTheRefusalPrefix(t *testing.T) {
	t.Parallel()
	for name, reply := range map[string]string{
		"ambiguous unopened closer": ambiguousAgentReply(),
		"resumed think run":         resumedBlockReply(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
				eligibleFinding()[0], finalChat(reply), okDispatcher(), 0, "")
			assert.Empty(t, fix, "a refused reply yields no patch")
			require.NotEmpty(t, warn, "the refusal must carry a warn")
			assert.True(t, strings.HasPrefix(warn, agentRefusalPrefix),
				"a content-shape decline must open with agentRefusalPrefix or it inherits the transport classification: "+warn)
		})
	}
}

// TD internal/verify/invoke.go:766, executor lane (epic 35.16.11.2.2.4.4 T3): a
// draft fix before a bare </think> followed by NOTHING usable after it. The
// suffix carries no envelope, so the shared rule fell back to the whole answer
// and the first-match parser returned the abandoned draft as the patch --auto-fix
// writes to tracked source. The verify lane can narrow on grade (only `refuted`
// clears the gate); this lane cannot, because every non-empty fix is eligible to
// be written. So any usable prefix + unusable suffix is refused. Driven through
// invokeExecutor and generateFixes, the production call site, for each unusable
// suffix shape.
func TestGenerateFixes_PrefixOnlyDraftFixIsRefused(t *testing.T) {
	for name, suffix := range map[string]string{
		"present-but-empty fix": `{"fix":""}`,
		"prose":                 `Actually, leave the file as it is.`,
		"nothing":               ``,
	} {
		t.Run(name, func(t *testing.T) {
			reply := `{"fix":"DRAFT PATCH"}` + "\n" + closerTag() + "\n" + suffix

			fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
				eligibleFinding()[0], finalChat(reply), okDispatcher(), 0, "")
			assert.Empty(t, fix, "the draft before the closer is not provably committed, so no patch")
			assert.True(t, strings.HasPrefix(warn, agentRefusalPrefix),
				"a content-shape decline must open with agentRefusalPrefix: "+warn)

			ctx, buf := ceilingCtx()
			findings := eligibleFinding()
			generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"),
				&recordingExecutor{}, finalChat(reply), okDispatcher(), 0)
			assert.Contains(t, buf.String(), "executor_agent_refused",
				"the refusal is disclosed under its own class")
			assert.NotEqual(t, "DRAFT PATCH", findings[0].Fix,
				"the abandoned draft must never reach tracked source")
			assert.Empty(t, findings[0].Fix)
		})
	}
}
