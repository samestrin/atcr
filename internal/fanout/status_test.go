package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
