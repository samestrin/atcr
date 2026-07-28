package verify

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

// truncatingExecutor is a snippet-path executorCompleter whose CompleteWithMeta
// reports a finish_reason=length truncation with non-empty (rambling) content —
// the runaway fixer scenario the fix must NOT silently accept.
type truncatingExecutor struct {
	content string
}

func (t *truncatingExecutor) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return t.content, nil
}

func (t *truncatingExecutor) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: t.content, Truncated: true}, nil
}

func truncFinding() reconcile.JSONFinding {
	return reconcile.JSONFinding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified}
}

// Snippet path (AC scenario c): a truncated fix must be flagged non-silently and
// NOT presented as a usable patch — no silent no-op success.
func TestGenerateFixes_SnippetTruncated_FlagsNoUsablePatch(t *testing.T) {
	findings := []reconcile.JSONFinding{truncFinding()}
	// Prose-ish content that would otherwise pass the syntax guard and be accepted
	// as a clean fix; truncation must override that.
	rec := &truncatingExecutor{content: "change the query to use a parameterized statement instead of str"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	f := findings[0]
	assert.Empty(t, f.Fix, "a truncated fix must NOT be presented as a usable patch")
	assert.Contains(t, f.FixWarning, "truncated", "the truncation must be surfaced non-silently")
}

// Agent-mode path: a truncated agent-mode fix (parseable, so no parse warn masks
// it) must also be flagged and dropped.
func TestGenerateFixes_AgentModeTruncated_FlagsNoUsablePatch(t *testing.T) {
	findings := []reconcile.JSONFinding{truncFinding()}
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"fix":"partial patch that got cut off"}`, truncated: true}}}
	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"), &recordingExecutor{}, cc, okDispatcher(), 0)
	f := findings[0]
	assert.Empty(t, f.Fix, "a truncated agent-mode fix must NOT be presented as a usable patch")
	assert.Contains(t, f.FixWarning, "truncated")
}

// Regression: a NON-truncated snippet fix still lands normally (the truncation
// guard does not disturb the clean path).
func TestGenerateFixes_SnippetNotTruncated_StillLands(t *testing.T) {
	findings := []reconcile.JSONFinding{truncFinding()}
	rec := &truncatingExecutor{content: "use a parameterized query"}
	// Override to a clean (non-truncated) completer by using recordingExecutor.
	_ = rec
	clean := &recordingExecutor{out: "use a parameterized query"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), clean, nil, okDispatcher(), 0)
	f := findings[0]
	assert.Equal(t, "use a parameterized query", f.Fix)
	assert.Empty(t, f.FixWarning)
}

// truncatingErrorExecutor returns both an error and truncated=true, mirroring a
// provider that stops on finish_reason=length with an empty/partial response.
type truncatingErrorExecutor struct {
	content string
	err     error
}

// A later tier's truncated response must NOT stamp a FixWarning over a finding an
// EARLIER tier already fixed — the same prior-tier-success guard the ceiling
// skips and the smell-gate halt apply. The discriminator is any executor
// attribution in Evidence ("fix by <name>"): an attributed Fix is generated,
// whereas a bare Fix is the reviewer's own suggestion (TD: executor.go:340).
func TestGenerateFixes_SnippetTruncated_PreservesPriorTierFix(t *testing.T) {
	f := truncFinding()
	f.Fix = "an earlier tier's good fix"
	f.Evidence = "Found by bruce; fix by sonnet" // sonnet = a different tier than execConfig's opus
	findings := []reconcile.JSONFinding{f}
	rec := &truncatingExecutor{content: "change the query to use a parameterized statement instead of str"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	got := findings[0]
	assert.Equal(t, "an earlier tier's good fix", got.Fix, "a prior tier's fix must survive")
	assert.Empty(t, got.FixWarning, "a truncated later-tier response must not warn over an earlier tier's generated fix")
}

// The documented reviewer-suggestion contract is preserved: a non-empty Fix with
// NO executor attribution is the reviewer's own suggestion, and the truncation
// warning still rides alongside it.
func TestGenerateFixes_SnippetTruncated_WarnsOverReviewerSuggestion(t *testing.T) {
	f := truncFinding()
	f.Fix = "reviewer's suggested fix"
	f.Evidence = "Found by bruce"
	findings := []reconcile.JSONFinding{f}
	rec := &truncatingExecutor{content: "change the query to use a parameterized statement instead of str"}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	got := findings[0]
	assert.Equal(t, "reviewer's suggested fix", got.Fix)
	assert.Contains(t, got.FixWarning, "truncated", "the reviewer-suggestion contract keeps the warning")
}

func (t *truncatingErrorExecutor) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return t.content, t.err
}

func (t *truncatingErrorExecutor) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: t.content, Truncated: true}, t.err
}

// Regression: when the snippet path returns an error alongside truncated=true,
// the truncation flag must not be discarded in favor of the generic error warning.
func TestGenerateFixes_SnippetTruncatedWithError_FlagsNoUsablePatch(t *testing.T) {
	findings := []reconcile.JSONFinding{truncFinding()}
	rec := &truncatingErrorExecutor{content: "", err: context.DeadlineExceeded}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)
	f := findings[0]
	assert.Empty(t, f.Fix, "a truncated fix must NOT be presented as a usable patch")
	assert.Contains(t, f.FixWarning, "truncated", "truncation must take priority over the generic error warning")
}
