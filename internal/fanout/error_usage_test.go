package fanout

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
)

// An errored ordinary tool-loop turn still carries the tokens the provider
// billed; they must land in the Result exactly once (Epic 35.26).
func TestToolLoop_ErroredTurnUsageCounted(t *testing.T) {
	t.Parallel()
	cc := &scriptedChat{turns: []chatTurn{
		{toolCalls: []llmclient.ToolCall{toolCall("c1", "read_file", `{"path":"a.go"}`)},
			usage: llmclient.UsageData{PromptTokens: 100, CompletionTokens: 10}},
		{err: errors.New("provider exploded"),
			usage: llmclient.UsageData{PromptTokens: 40, CompletionTokens: 7}},
	}}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 10, 0))
	require.Equal(t, StatusFailed, r.Status)
	assert.Equal(t, 140, r.TokensIn)
	assert.Equal(t, 17, r.TokensOut)
}

// Same contract for the unbudgeted final-answer call after a budget trip.
func TestToolLoop_ErroredFinalAnswerUsageCounted(t *testing.T) {
	t.Parallel()
	turns := toolCallTurns(2)
	turns[0].usage = llmclient.UsageData{PromptTokens: 100, CompletionTokens: 10}
	turns[1].usage = llmclient.UsageData{PromptTokens: 50, CompletionTokens: 5}
	turns = append(turns, chatTurn{err: errors.New("provider exploded"),
		usage: llmclient.UsageData{PromptTokens: 30, CompletionTokens: 3}})
	cc := &scriptedChat{turns: turns}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}

	r := toolEngine(cc, d).invokeAgent(context.Background(), toolAgent("a", 2, 0))
	require.Equal(t, StatusFailed, r.Status)
	assert.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets)
	assert.Equal(t, 180, r.TokensIn)
	assert.Equal(t, 18, r.TokensOut)
}
