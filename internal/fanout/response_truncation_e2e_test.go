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

// These tests exercise the three Epic 19.5 acceptance scenarios end-to-end,
// through the public Engine.Run + WritePool chain (not just invokeSlot), so the
// persisted status.json — the surface the acceptance criteria name — is asserted.
// Scenario (c) (executor truncation) lives in internal/verify/executor_truncation_test.go.

// AC scenario (a): a reviewer response with finish_reason=length and zero parsed
// findings is failed over to the slot's fallback (FallbackUsed=true), and no
// silent clean review is recorded.
func TestE2E_TruncatedZeroFindings_FailsOverAndRecords(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary":  {Content: "I kept thinking but never emitted a finding", Truncated: true},
		"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slots := []Slot{{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}}
	results := e.Run(context.Background(), slots)
	require.Len(t, results, 1)
	assert.Equal(t, StatusOK, results[0].Status, "the fallback rescued the slot")
	assert.True(t, results[0].FallbackUsed, "AC(a): the slot's fallback agent runs")
	assert.Contains(t, results[0].Content, "HIGH|a.go:1")

	pool := filepath.Join(t.TempDir(), "pool")
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	st := readAgentStatus(t, pool, "bruce")
	assert.Equal(t, StatusOK, st.Status)
	assert.True(t, st.FallbackUsed)
}

// AC scenario (b): a reviewer response with finish_reason=length and >=1 parsed
// finding stays StatusOK, keeps its findings, and carries the response_truncated
// marker in status.json.
func TestE2E_TruncatedWithFindings_KeptWithMarkerInStatusJSON(t *testing.T) {
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce\ntrailing thought cut off mid-", Truncated: true},
	}}
	e := NewEngine(c, WithTruncationFailover())
	slots := []Slot{{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}}}}
	results := e.Run(context.Background(), slots)
	require.Len(t, results, 1)
	assert.Equal(t, StatusOK, results[0].Status)
	assert.False(t, results[0].FallbackUsed, "no fallback needed — partial findings landed")

	pool := filepath.Join(t.TempDir(), "pool")
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	st := readAgentStatus(t, pool, "bruce")
	assert.True(t, st.ResponseTruncated, "AC(b): a truncated marker rides the slot record / status.json")
	assert.Equal(t, 1, st.FindingsCount, "AC(b): partial findings are preserved")
}

func readAgentStatus(t *testing.T, pool, agent string) AgentStatus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", agent, "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))
	return st
}
