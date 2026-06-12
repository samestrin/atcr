package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureReviewComplete(t *testing.T) {
	dir := t.TempDir()
	// Not fan-out-managed (no manifest.json, e.g. a hand-assembled CLI anchor):
	// nothing to guard.
	require.NoError(t, EnsureReviewComplete(dir, "x"))
	// Manifest present, no pool summary: the fan-out is still running.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"partial":false}`), 0o644))
	err := EnsureReviewComplete(dir, "x")
	require.ErrorIs(t, err, ErrReviewInProgress)
	assert.Contains(t, err.Error(), "still in_progress")
	// Summary written: fan-out complete, guard passes.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":0}`), 0o644))
	require.NoError(t, EnsureReviewComplete(dir, "x"))
}

func TestStatusJSON_RecordsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{
		Agent:        "bruce",
		Status:       StatusOK,
		PayloadMode:  "diff",
		Truncated:    true,
		FilesDropped: []string{"file1.py", "file2.py", "file3.py"},
	}
	require.NoError(t, WriteStatus(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.Truncated)
	assert.Equal(t, []string{"file1.py", "file2.py", "file3.py"}, got.FilesDropped)
	assert.Equal(t, "bruce", got.Agent)
	assert.Equal(t, StatusOK, got.Status)
}

func TestStatusJSON_NoTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{Agent: "greta", Status: StatusOK, FindingsCount: 5, DurationMS: 3200}
	require.NoError(t, WriteStatus(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	assert.False(t, got.Truncated)
	assert.Empty(t, got.FilesDropped)
	assert.Equal(t, 5, got.FindingsCount)
}

// --- Epic 1.1: reserved per-agent status counters (absent in 1.x) ---

// TestStatusJSON_ReservedCountersOmittedIn1x verifies a 1.x status.json omits
// the reserved turns/tool_calls/tool_bytes counters (no agentic loop ran).
func TestStatusJSON_ReservedCountersOmittedIn1x(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{Agent: "bruce", Status: StatusOK, PayloadMode: "diff"}
	require.NoError(t, WriteStatus(path, s))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, key := range []string{"turns", "tool_calls", "tool_bytes"} {
		assert.NotContains(t, string(data), key, "reserved counter absent in 1.x")
	}
	assert.Nil(t, s.Turns)
	assert.Nil(t, s.ToolCalls)
	assert.Nil(t, s.ToolBytes)
}

// TestStatusJSON_ReservedCountersRoundTrip verifies the reserved counters
// serialize and parse back when a future stage populates them.
func TestStatusJSON_ReservedCountersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	turns, calls := 3, 7
	var bytes64 int64 = 2048
	s := &AgentStatus{Agent: "bruce", Status: StatusOK, PayloadMode: "diff", Turns: &turns, ToolCalls: &calls, ToolBytes: &bytes64}
	require.NoError(t, WriteStatus(path, s))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	require.NotNil(t, got.Turns)
	assert.Equal(t, 3, *got.Turns)
	require.NotNil(t, got.ToolCalls)
	assert.Equal(t, 7, *got.ToolCalls)
	require.NotNil(t, got.ToolBytes)
	assert.Equal(t, int64(2048), *got.ToolBytes)
}
