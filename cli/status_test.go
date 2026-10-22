package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestStatusCmd_StaleJSON verifies the inferred `stale` state (summary.json
// absent, effective timeout elapsed) reaches `atcr status` output unchanged —
// the CLI is a pure pass-through of ReadReviewStatus (Epic 1.5).
func TestStatusCmd_StaleJSON(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_dead"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))

	out, err := runStatusIn(t, root)
	require.NoError(t, err)
	var st struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	assert.Equal(t, "stale", st.Status)
}

func TestStatusCmd_NoReviews(t *testing.T) {
	root := t.TempDir()
	_, err := runStatusIn(t, root)
	require.Error(t, err)
}

// statusJSON is the full `atcr status` document, decoded with the field names
// docs/status.md promises.
type statusJSON struct {
	ReviewID        string   `json:"review_id"`
	Status          string   `json:"status"`
	AgentCount      int      `json:"agent_count"`
	AgentsDone      int      `json:"agents_done"`
	AgentsPending   int      `json:"agents_pending"`
	Partial         bool     `json:"partial"`
	CurrentStage    string   `json:"current_stage"`
	CompletedAgents []string `json:"completed_agents"`
	Agents          []struct {
		Name           string `json:"name"`
		State          string `json:"state"`
		ChunksDone     int    `json:"chunks_done"`
		ChunksTotal    int    `json:"chunks_total"`
		LastActivityAt string `json:"last_activity_at"`
	} `json:"agents"`
}

func decodeStatus(t *testing.T, out string) (statusJSON, map[string]json.RawMessage) {
	t.Helper()
	var st statusJSON
	require.NoError(t, json.Unmarshal([]byte(out), &st))
	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &keys))
	return st, keys
}

// TestStatusCmd_LiveProgressJSON is AC1 at the CLI: a review whose fan-out is
// still running (no summary.json) prints the finished personas, each persona's
// chunk progress and last activity, and the stage fanout.
func TestStatusCmd_LiveProgressJSON(t *testing.T) {
	root := t.TempDir()
	id := "2026-10-22_live"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	raw := filepath.Join(dir, "sources", "pool", "raw", "agent")
	require.NoError(t, os.MkdirAll(raw, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["kai","greta","bruce"],"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))

	// kai finished; greta is a chunked persona with 1 of 3 bins resolved; bruce
	// has written nothing yet.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "kai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "kai", "status.json"),
		[]byte(`{"agent":"kai","status":"ok"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "greta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "greta", "chunks.json"),
		[]byte(`{"schema_version":1,"agent":"greta","bins":[`+
			`{"index":0,"files":["a.go"],"outcome":"ok"},`+
			`{"index":1,"files":["b.go"],"outcome":"pending"},`+
			`{"index":2,"files":["c.go"],"outcome":"pending"}]}`), 0o644))

	out, err := runStatusIn(t, root)
	require.NoError(t, err)
	st, keys := decodeStatus(t, out)

	assert.Equal(t, "in_progress", st.Status)
	assert.Equal(t, 3, st.AgentCount)
	assert.Equal(t, 1, st.AgentsDone)
	assert.Equal(t, 2, st.AgentsPending)
	assert.Equal(t, "fanout", st.CurrentStage)
	assert.Equal(t, []string{"kai"}, st.CompletedAgents)
	for _, k := range []string{"current_stage", "completed_agents", "agents"} {
		assert.Contains(t, keys, k)
	}

	require.Len(t, st.Agents, 3)
	assert.Equal(t, "kai", st.Agents[0].Name, "rows follow roster order")
	assert.Equal(t, "done", st.Agents[0].State)
	assert.Equal(t, 1, st.Agents[0].ChunksDone)
	assert.Equal(t, 1, st.Agents[0].ChunksTotal)
	_, err = time.Parse(time.RFC3339, st.Agents[0].LastActivityAt)
	require.NoError(t, err, "last_activity_at is RFC 3339")

	assert.Equal(t, "greta", st.Agents[1].Name)
	assert.Equal(t, "pending", st.Agents[1].State)
	assert.Equal(t, 1, st.Agents[1].ChunksDone)
	assert.Equal(t, 3, st.Agents[1].ChunksTotal)
	assert.NotEmpty(t, st.Agents[1].LastActivityAt)

	assert.Equal(t, "bruce", st.Agents[2].Name)
	assert.Equal(t, "pending", st.Agents[2].State)
	assert.Equal(t, 0, st.Agents[2].ChunksDone)
	assert.Equal(t, 1, st.Agents[2].ChunksTotal)
	assert.Empty(t, st.Agents[2].LastActivityAt)
	assert.NotContains(t, out, `"last_activity_at": ""`, "an absent time is omitted, not empty")
}

// TestStatusCmd_StageAfterFanoutJSON is AC2 at the CLI: once summary.json is
// present, current_stage names the latest finished stage from the marker files.
func TestStatusCmd_StageAfterFanoutJSON(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_x"
	writeStatusFixture(t, root, id)
	dir := filepath.Join(root, ".atcr", "reviews", id)

	out, err := runStatusIn(t, root)
	require.NoError(t, err)
	st, _ := decodeStatus(t, out)
	assert.Equal(t, "reviewed", st.CurrentStage)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", "findings.json"), []byte(`{}`), 0o644))
	out, err = runStatusIn(t, root)
	require.NoError(t, err)
	st, _ = decodeStatus(t, out)
	assert.Equal(t, "reconciled", st.CurrentStage)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", "verification.json"), []byte(`{}`), 0o644))
	out, err = runStatusIn(t, root, id)
	require.NoError(t, err)
	st, _ = decodeStatus(t, out)
	assert.Equal(t, "verified", st.CurrentStage)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", "debate.json"), []byte(`{}`), 0o644))
	out, err = runStatusIn(t, root, id)
	require.NoError(t, err)
	st, _ = decodeStatus(t, out)
	assert.Equal(t, "debated", st.CurrentStage)

	// AC3: the six existing fields still read as they did once the run ends.
	assert.Equal(t, id, st.ReviewID)
	assert.Equal(t, "completed", st.Status)
	assert.Equal(t, 2, st.AgentCount)
	assert.Equal(t, 2, st.AgentsDone)
	assert.Equal(t, 0, st.AgentsPending)
	assert.False(t, st.Partial)
}

// TestStatusCmd_StaleStageJSON: a stale run reports its terminal status as the
// stage.
func TestStatusCmd_StaleStageJSON(t *testing.T) {
	root := t.TempDir()
	id := "2026-06-10_dead"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))

	out, err := runStatusIn(t, root)
	require.NoError(t, err)
	st, _ := decodeStatus(t, out)
	assert.Equal(t, "stale", st.CurrentStage)
}
