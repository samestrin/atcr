package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/fanout"
)

// statusAXIStdout is the exact --axi stdout of the completed writeStatusFixture
// review: the ReviewStatus fields as one TOON object (completed_agents absent,
// as its JSON omitempty omits it), then the suggestStatus lines as help[].
func statusAXIStdout() string {
	return "review_id: r\n" +
		"status: completed\n" +
		"agent_count: 2\n" +
		"agents_done: 2\n" +
		"agents_pending: 0\n" +
		"partial: false\n" +
		"current_stage: reviewed\n" +
		"agents[2]{name,state,chunks_done,chunks_total}:\n" +
		"  greta,pending,0,1\n" +
		"  kai,pending,0,1\n" +
		"help[1]: atcr report <id>\n"
}

// statusFixtureJSON is the exact default stdout of the same fixture: the
// indented JSON document `atcr status` printed before --axi existed.
const statusFixtureJSON = `{
  "review_id": "r",
  "status": "completed",
  "agent_count": 2,
  "agents_done": 2,
  "agents_pending": 0,
  "partial": false,
  "current_stage": "reviewed",
  "agents": [
    {
      "name": "greta",
      "state": "pending",
      "chunks_done": 0,
      "chunks_total": 1
    },
    {
      "name": "kai",
      "state": "pending",
      "chunks_done": 0,
      "chunks_total": 1
    }
  ]
}
`

// writePendingStatusFixture lays down a review whose fan-out is still running:
// one persona, a fresh started_at inside the timeout window, no summary.json.
func writePendingStatusFixture(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	started := time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"`+started+`","timeout_secs":600,"partial":false}`), 0o644))
}

// TestStatusCmd_AXI (Epic 104.1 T4): under --axi stdout is exactly the
// ReviewStatus TOON object followed by the help[] block — no JSON — and stderr
// carries no Next: lines.
func TestStatusCmd_AXI(t *testing.T) {
	isolate(t)
	writeStatusFixture(t, ".", "r")
	code, stdout, stderr := execCmdSplit(t, "status", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, statusAXIStdout(), stdout)
	assert.NotContains(t, stderr, "Next:", "--axi carries its next steps on stdout, not stderr")

	var doc map[string]any
	require.Error(t, json.Unmarshal([]byte(stdout), &doc), "stdout is not JSON")
	require.NoError(t, toon.Unmarshal([]byte(stdout), &doc), "stdout decodes as one TOON value")
	assert.Equal(t, "r", doc["review_id"])
	assert.Equal(t, "completed", doc["status"])
	assert.EqualValues(t, 2, doc["agent_count"])
	assert.EqualValues(t, 2, doc["agents_done"])
	assert.EqualValues(t, 0, doc["agents_pending"])
	assert.Equal(t, false, doc["partial"])
	assert.Equal(t, "reviewed", doc["current_stage"])
	assert.NotContains(t, doc, "completed_agents", "an empty list is omitted, as the JSON omitempty omits it")
	assert.Len(t, doc["agents"], 2)
	assert.Len(t, doc["help"], 1)
}

// TestStatusCmd_AXIPendingOffersResume: the Pending gate survives --axi — a
// review with agents still pending gets the review --resume line in help[].
func TestStatusCmd_AXIPendingOffersResume(t *testing.T) {
	isolate(t)
	writePendingStatusFixture(t, "r")
	code, stdout, stderr := execCmdSplit(t, "status", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "review_id: r\n"+
		"status: in_progress\n"+
		"agent_count: 1\n"+
		"agents_done: 0\n"+
		"agents_pending: 1\n"+
		"partial: false\n"+
		"current_stage: fanout\n"+
		"agents[1]{name,state,chunks_done,chunks_total}:\n"+
		"  greta,pending,0,1\n"+
		"help[2]: atcr report <id>,atcr review --resume <id>\n", stdout)
	assert.NotContains(t, stderr, "Next:")
}

// TestStatusCmd_AXIFailedStartCarriesStartError (EST-8137): a review whose
// fan-out never started reports failed with its start_error in the TOON
// payload, the field SKILL.md step 3 reads. The pending fixture above pins the
// other side: a review without one omits the key.
func TestStatusCmd_AXIFailedStartCarriesStartError(t *testing.T) {
	isolate(t)
	dir := filepath.Join(".atcr", "reviews", "r")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	started := time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["greta"],"started_at":"`+started+`","timeout_secs":600,"start_error":"API key env var not set: ATCR_TEST_REVIEW_KEY","partial":false}`), 0o644))
	code, stdout, stderr := execCmdSplit(t, "status", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "review_id: r\n"+
		"status: failed\n"+
		"agent_count: 1\n"+
		"agents_done: 0\n"+
		"agents_pending: 1\n"+
		"partial: false\n"+
		"current_stage: failed\n"+
		"agents[1]{name,state,chunks_done,chunks_total}:\n"+
		"  greta,pending,0,1\n"+
		"start_error: \"API key env var not set: ATCR_TEST_REVIEW_KEY\"\n"+
		"help[2]: atcr report <id>,atcr review --resume <id>\n", stdout)

	var doc map[string]any
	require.NoError(t, toon.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, "API key env var not set: ATCR_TEST_REVIEW_KEY", doc["start_error"])
}

// TestStatusCmd_AXILiveProgress: the additive fields reach the TOON payload —
// completed_agents, per-persona chunk progress, and last_activity_at present
// only for personas that have written (the JSON omitempty rule).
func TestStatusCmd_AXILiveProgress(t *testing.T) {
	isolate(t)
	id := "r"
	dir := filepath.Join(".atcr", "reviews", id)
	raw := filepath.Join(dir, "sources", "pool", "raw", "agent")
	require.NoError(t, os.MkdirAll(raw, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"b","roster":["kai","greta","bruce"],"partial":false}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "kai"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "kai", "status.json"),
		[]byte(`{"agent":"kai","status":"ok"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "greta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "greta", "chunks.json"),
		[]byte(`{"schema_version":1,"agent":"greta","bins":[`+
			`{"index":0,"files":["a.go"],"outcome":"ok"},`+
			`{"index":1,"files":["b.go"],"outcome":"pending"},`+
			`{"index":2,"files":["c.go"],"outcome":"pending"}]}`), 0o644))

	code, stdout, stderr := execCmdSplit(t, "status", "--axi", id)
	require.Equal(t, 0, code, stderr)
	var doc map[string]any
	require.NoError(t, toon.Unmarshal([]byte(stdout), &doc), "stdout decodes as one TOON value: %q", stdout)
	assert.Equal(t, "in_progress", doc["status"])
	assert.Equal(t, "fanout", doc["current_stage"])
	assert.Equal(t, []any{"kai"}, doc["completed_agents"])
	rows, ok := doc["agents"].([]any)
	require.True(t, ok, "agents is an array: %T", doc["agents"])
	require.Len(t, rows, 3)
	kai, _ := rows[0].(map[string]any)
	require.NotNil(t, kai)
	assert.Equal(t, "kai", kai["name"])
	assert.Equal(t, "done", kai["state"])
	assert.EqualValues(t, 1, kai["chunks_done"])
	assert.EqualValues(t, 1, kai["chunks_total"])
	require.Contains(t, kai, "last_activity_at")
	_, err := time.Parse(time.RFC3339, kai["last_activity_at"].(string))
	require.NoError(t, err, "last_activity_at is RFC 3339")
	greta, _ := rows[1].(map[string]any)
	require.NotNil(t, greta)
	assert.EqualValues(t, 1, greta["chunks_done"])
	assert.EqualValues(t, 3, greta["chunks_total"])
	bruce, _ := rows[2].(map[string]any)
	require.NotNil(t, bruce)
	assert.Equal(t, "pending", bruce["state"])
	assert.NotContains(t, bruce, "last_activity_at", "an absent time is omitted, not empty")
	assert.Len(t, doc["help"], 2, "pending review offers resume")
}

// TestStatusCmd_AXILogFormatJSON: under --axi --log-format json no Next:
// record reaches the NDJSON stderr either; stdout is the same payload.
func TestStatusCmd_AXILogFormatJSON(t *testing.T) {
	isolate(t)
	writeStatusFixture(t, ".", "r")
	code, stdout, stderr := execCmdSplit(t, "--log-format", "json", "status", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, statusAXIStdout(), stdout)
	for _, line := range nonEmptyLines(stderr) {
		v := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &v), "stderr stays NDJSON: %q", line)
		msg, _ := v["msg"].(string)
		assert.False(t, strings.HasPrefix(msg, "Next:"), "no Next: record under --axi: %q", msg)
	}
}

// TestStatusCmd_AXILegacyPipeRejected: status has no legacy pipe encoder, so
// --legacy-pipe is a usage error, and ATCR_LEGACY_PIPE=1 changes nothing and
// announces no deprecated encoder.
func TestStatusCmd_AXILegacyPipeRejected(t *testing.T) {
	isolate(t)
	writeStatusFixture(t, ".", "r")
	code, stdout, _ := execCmdSplit(t, "status", "--axi", "--legacy-pipe", "r")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "status", "--axi", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, statusAXIStdout(), stdout)
	assert.NotContains(t, stderr, "deprecated", "no deprecation notice for an encoder this command lacks")
}

// TestStatusCmd_JSONModeUnchanged: without --axi stdout and stderr are the
// bytes status printed before --axi existed — the indented JSON document
// alone on stdout, the human Next: lines on stderr.
func TestStatusCmd_JSONModeUnchanged(t *testing.T) {
	isolate(t)
	writeStatusFixture(t, ".", "r")
	code, stdout, stderr := execCmdSplit(t, "status", "r")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, statusFixtureJSON, stdout)
	assert.True(t, strings.HasSuffix(stderr, "Next: atcr report <id>\n"), "stderr: %q", stderr)
}

// TestStatusCmd_AXIMissingReview: the not-found usage error is unchanged under
// --axi — exit 2, no payload, no help[].
func TestStatusCmd_AXIMissingReview(t *testing.T) {
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	code, stdout, _ := execCmdSplit(t, "status", "--axi", "missing")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
}

// TestStatusAXIPayload_OmitsEmptyFields pins the omitempty mirror directly: a
// status with no stage, no completed personas and an empty roster encodes only
// the six run-level fields — no empty current_stage, completed_agents or agents
// key an agent would pay tokens to read.
func TestStatusAXIPayload_OmitsEmptyFields(t *testing.T) {
	st := &fanout.ReviewStatus{ReviewID: "r", Status: fanout.RunCompleted}
	var buf bytes.Buffer
	require.NoError(t, writeAXIHelp(&buf, statusAXIPayload(st), nil))
	assert.Equal(t, "review_id: r\nstatus: completed\nagent_count: 0\nagents_done: 0\nagents_pending: 0\npartial: false\n", buf.String())
}

// execStatusBrokenStdout runs args through the real root with a stdout that
// fails every write, and returns the command's error.
func execStatusBrokenStdout(t *testing.T, args ...string) error {
	t.Helper()
	var errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(failWriter{})
	root.SetErr(&errBuf)
	return root.ExecuteContext(context.Background())
}

// TestStatusCmd_AXIRenderFault: a broken stdout under --axi is reported as the
// encoder's fault, not swallowed.
func TestStatusCmd_AXIRenderFault(t *testing.T) {
	isolate(t)
	writeStatusFixture(t, ".", "r")
	err := execStatusBrokenStdout(t, "status", "--axi", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axi encoder")
}
