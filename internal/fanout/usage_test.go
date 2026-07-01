package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usageCompleter is a Completer that also reports token usage (UsageCompleter),
// so the single-shot path captures model + tokens onto the Result.
type usageCompleter struct {
	usage   llmclient.UsageData
	records []llmclient.CallRecord
}

func (u *usageCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	return "review by " + inv.Model, nil
}

func (u *usageCompleter) CompleteWithUsage(ctx context.Context, inv llmclient.Invocation) (string, llmclient.UsageData, []llmclient.CallRecord, error) {
	return "review by " + inv.Model, u.usage, u.records, nil
}

func TestSingleShot_CapturesModelAndUsage(t *testing.T) {
	e := NewEngine(&usageCompleter{usage: llmclient.UsageData{PromptTokens: 1200, CompletionTokens: 340}})
	r := e.invokeAgent(context.Background(), Agent{
		Name:       "bruce",
		Invocation: llmclient.Invocation{Model: "claude-sonnet-4-6"},
	})
	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, "claude-sonnet-4-6", r.Model)
	assert.Equal(t, 1200, r.TokensIn)
	assert.Equal(t, 340, r.TokensOut)
}

func TestSingleShot_PlainCompleterReportsZeroUsage(t *testing.T) {
	// fakeCompleter implements only Complete (no usage); usage stays zero.
	e := NewEngine(newFake())
	r := e.invokeAgent(context.Background(), Agent{
		Name:       "bruce",
		Invocation: llmclient.Invocation{Model: "m"},
	})
	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, r.TokensIn)
	assert.Equal(t, 0, r.TokensOut)
}

func TestStatusFor_PersistsUsageWhenPresent(t *testing.T) {
	st := statusFor(Result{
		Agent:     "bruce",
		Status:    StatusOK,
		Model:     "claude-sonnet-4-6",
		TokensIn:  1200,
		TokensOut: 340,
	}, findingsResult{})
	assert.Equal(t, "claude-sonnet-4-6", st.Model)
	assert.Equal(t, 1200, st.TokensIn)
	assert.Equal(t, 340, st.TokensOut)
}

func TestStatusFor_OmitsUsageWhenZero(t *testing.T) {
	// A zero-usage result keeps status.json byte-identical to the pre-3.3 shape:
	// the omitempty fields must be absent from the serialized JSON.
	st := statusFor(Result{Agent: "bruce", Status: StatusOK, Model: "m"}, findingsResult{})
	assert.Empty(t, st.Model, "model not recorded without usage")
	data, err := json.Marshal(st)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.NotContains(t, m, "model")
	assert.NotContains(t, m, "tokens_in")
	assert.NotContains(t, m, "tokens_out")
}

func TestResult_AddUsageAccumulates(t *testing.T) {
	// Multi-turn semantics (TD-002): usage sums across turns, not last-turn-only.
	r := &Result{}
	r.addUsage(llmclient.UsageData{PromptTokens: 100, CompletionTokens: 20})
	r.addUsage(llmclient.UsageData{PromptTokens: 250, CompletionTokens: 60})
	r.addUsage(llmclient.UsageData{PromptTokens: 50, CompletionTokens: 10})
	assert.Equal(t, 400, r.TokensIn)
	assert.Equal(t, 90, r.TokensOut)
}

func TestReadPoolSummary_RoundTrip(t *testing.T) {
	// WritePool persists AgentStatus usage; ReadPoolSummary reads it back so the
	// scorecard emitter can source per-reviewer model/tokens.
	dir := t.TempDir()
	pool := filepath.Join(dir, "sources", "pool")
	results := []Result{
		{Agent: "bruce", Status: StatusOK, Content: "x", Model: "claude-sonnet-4-6", TokensIn: 1200, TokensOut: 340, DurationMS: 900},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	ps, err := ReadPoolSummary(dir)
	require.NoError(t, err)
	require.Len(t, ps.Agents, 1)
	assert.Equal(t, "claude-sonnet-4-6", ps.Agents[0].Model)
	assert.Equal(t, 1200, ps.Agents[0].TokensIn)
	assert.Equal(t, 340, ps.Agents[0].TokensOut)
	assert.EqualValues(t, 900, ps.Agents[0].DurationMS)

	// status.json missing entirely → raw os.ErrNotExist for callers to degrade on.
	_, err = ReadPoolSummary(t.TempDir())
	assert.ErrorIs(t, err, os.ErrNotExist)
}
