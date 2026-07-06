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
