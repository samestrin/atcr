package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeStatusFixture lays down a review dir with a manifest and pool summary so
// `atcr status` reports a completed run, plus the .atcr/latest pointer.
func writeStatusFixture(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta","kai"],"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", "summary.json"),
		[]byte(`{"total":2,"succeeded":2,"failed":0,"partial":false,"total_findings":3}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
}

func runStatusIn(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(root)
	cmd := newStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestStatusCmd_CompletedJSON(t *testing.T) {
	root := t.TempDir()
	writeStatusFixture(t, root, "2026-06-10_x")
	out, err := runStatusIn(t, root)
	require.NoError(t, err)

	var st struct {
		ReviewID   string `json:"review_id"`
		Status     string `json:"status"`
		AgentCount int    `json:"agent_count"`
		AgentsDone int    `json:"agents_done"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	assert.Equal(t, "2026-06-10_x", st.ReviewID)
	assert.Equal(t, "completed", st.Status)
	assert.Equal(t, 2, st.AgentCount)
	assert.Equal(t, 2, st.AgentsDone)
}

func TestStatusCmd_NoReviews(t *testing.T) {
	root := t.TempDir()
	_, err := runStatusIn(t, root)
	require.Error(t, err)
}
