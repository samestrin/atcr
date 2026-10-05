package verify

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
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

	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"),
		&recordingExecutor{}, finalChat(ambiguousAgentReply()), okDispatcher(), 0)

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
