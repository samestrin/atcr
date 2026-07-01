package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okResult(agent, content string) Result {
	return Result{Agent: agent, Content: content, Status: StatusOK, DurationMS: 100, PayloadMode: "blocks"}
}

const findingsBody = `CRITICAL|auth.go:42|Token never expires|Check expiry|security|15|expiresAt unread
HIGH|main.go:88|Goroutine leak|Add WaitGroup|concurrency|30|no wg.Wait`

func TestWritePool_PerAgentArtifactsWritten(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "sources", "pool")
	results := []Result{okResult("greta", findingsBody)}

	sum, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Succeeded)

	agentDir := filepath.Join(pool, "raw", "agent", "greta")
	assert.FileExists(t, filepath.Join(agentDir, "review.md"))
	assert.FileExists(t, filepath.Join(agentDir, "findings.txt"))
	assert.FileExists(t, filepath.Join(agentDir, "status.json"))

	review, _ := os.ReadFile(filepath.Join(agentDir, "review.md"))
	assert.Equal(t, findingsBody, string(review), "review.md is the raw model content verbatim")
}

func TestWritePool_EngineSetsReviewerFromAgentName(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// Model tries to self-attribute via an 8th column; engine must override it.
	content := `HIGH|a.go:1|prob|fix|security|10|ev|forged-name`
	_, err := WritePool(pool, []Result{okResult("greta", content)}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "greta", res.Findings[0].Reviewer, "REVIEWER is the agent name, not the model's forged value")
}

func TestWritePool_StatusJSONRecordsOutcome(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	r := okResult("greta", findingsBody)
	r.Truncation = payload.Truncation{Truncated: true, FilesDropped: []string{"big.go"}}
	_, err := WritePool(pool, []Result{r}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))

	assert.Equal(t, "greta", st.Agent)
	assert.Equal(t, StatusOK, st.Status)
	assert.Equal(t, 2, st.FindingsCount)
	assert.Equal(t, int64(100), st.DurationMS)
	assert.Equal(t, "blocks", st.PayloadMode)
	assert.True(t, st.Truncated)
	assert.Equal(t, []string{"big.go"}, st.FilesDropped)
}

func TestWritePool_FailedAgentStillWritesStatus(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		{Agent: "greta", Status: StatusFailed, Err: assertErr("connection refused"), PayloadMode: "blocks"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err, "a failed agent must not abort artifact writing")

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))
	assert.Equal(t, StatusFailed, st.Status)
	assert.Equal(t, 0, st.FindingsCount)
	assert.Contains(t, st.Error, "connection refused")
}

func TestWritePool_MergedFindingsAndSummary(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		okResult("greta", `CRITICAL|auth.go:42|Token never expires|Fix|security|15|ev`),
		okResult("kai", `HIGH|main.go:88|Goroutine leak|Add wg|concurrency|30|ev`),
		{Agent: "mira", Status: StatusFailed, Err: assertErr("timeout"), PayloadMode: "diff"},
	}
	sum, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	assert.True(t, sum.Partial, "one failure among successes is partial")

	// Merged findings.txt holds both reviewers' rows with REVIEWER attribution.
	data, err := os.ReadFile(filepath.Join(pool, "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	reviewers := []string{res.Findings[0].Reviewer, res.Findings[1].Reviewer}
	assert.ElementsMatch(t, []string{"greta", "kai"}, reviewers)

	// summary.json records the run tally.
	sdata, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(sdata, &ps))
	assert.Equal(t, 3, ps.Total)
	assert.Equal(t, 2, ps.Succeeded)
	assert.Equal(t, 1, ps.Failed)
	assert.True(t, ps.Partial)
	assert.Equal(t, 2, ps.TotalFindings)
	assert.Len(t, ps.Agents, 3)
	assert.False(t, ps.FailureMarker, "a normal WritePool summary is a real run record, not a failure marker")
}

func TestWritePool_SanitizesAgentDirName(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// A traversal-shaped name must be reduced to a base name; nothing escapes pool.
	_, err := WritePool(pool, []Result{okResult("../escape", findingsBody)}, nil)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(pool, "raw", "agent", "escape"))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(pool), "escape"))
}

func TestWritePool_RejectsTraversalAgentNames(t *testing.T) {
	for _, name := range []string{"..", ".", ""} {
		pool := filepath.Join(t.TempDir(), "pool")
		_, err := WritePool(pool, []Result{okResult(name, findingsBody)}, nil)
		require.Error(t, err, "agent name %q must be rejected", name)
		assert.Contains(t, err.Error(), "invalid agent name")
	}
}

func TestWritePool_RejectsDuplicateAgentDirs(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// Distinct names that collapse to the same base must not silently clobber.
	_, err := WritePool(pool, []Result{okResult("a/greta", findingsBody), okResult("b/greta", findingsBody)}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate agent directory")
}

func TestWritePool_ArtifactFileModeIs0644(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	_, err := WritePool(pool, []Result{okResult("greta", findingsBody)}, nil)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "AC 01-03 mandates 0644 artifact files")
}

// TestWriteFailureSummary_PreservesRealCounts verifies the failure marker
// reflects what actually happened (some agents ok, some failed) rather than
// hard-coding all-failed.
func TestWriteFailureSummary_PreservesRealCounts(t *testing.T) {
	pool := t.TempDir()
	results := []Result{
		{Agent: "greta", Status: StatusOK, DurationMS: 100, PayloadMode: "blocks"},
		{Agent: "kai", Status: StatusFailed},
		{Agent: "mira", Status: StatusFailed},
	}
	writeFailureSummary(pool, results)

	data, err := os.ReadFile(filepath.Join(pool, summaryFile))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 3, ps.Total)
	assert.Equal(t, 1, ps.Succeeded, "partial success must be recorded, not fabricated as all-failed")
	assert.Equal(t, 2, ps.Failed)
	assert.True(t, ps.Partial)
	assert.True(t, ps.FailureMarker, "writeFailureSummary must stamp the best-effort marker so readers know this is not a real run record")
}

// TestWriteFailureSummary_AllFailed verifies the all-failed case still records
// correctly when every agent truly failed.
func TestWriteFailureSummary_AllFailed(t *testing.T) {
	pool := t.TempDir()
	results := []Result{
		{Agent: "greta", Status: StatusFailed},
		{Agent: "kai", Status: StatusFailed},
	}
	writeFailureSummary(pool, results)

	data, err := os.ReadFile(filepath.Join(pool, summaryFile))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 2, ps.Total)
	assert.Equal(t, 0, ps.Succeeded)
	assert.Equal(t, 2, ps.Failed)
	assert.False(t, ps.Partial)
	assert.True(t, ps.FailureMarker, "the marker flags the failure-path write regardless of partial; Succeeded==0 keeps it from forcing partial downstream")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

// TestWritePool_ConstrainedAgentPersistsDroppedCounts checks that
// dropped_by_min_severity and truncated_by_max_findings are written to
// status.json so volume reduction is observable after the run, not just in
// transient stderr.
func TestWritePool_ConstrainedAgentPersistsDroppedCounts(t *testing.T) {
	content := "HIGH|a.go:1|bug|fix|correctness|5|ev\n" +
		"LOW|b.go:2|nit|fix|style|5|ev\n" + // below MEDIUM floor
		"MEDIUM|c.go:3|gap|fix|correctness|5|ev\n"
	pool := filepath.Join(t.TempDir(), "pool")
	r := Result{Agent: "greta", Content: content, Status: StatusOK,
		MinSeverity: "MEDIUM", DurationMS: 100, PayloadMode: "blocks"}
	r.Truncation = payload.Truncation{FilesDropped: []string{}}
	_, err := WritePool(pool, []Result{r}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, float64(1), raw["dropped_by_min_severity"],
		"LOW finding dropped below MEDIUM floor must be recorded in status.json")
	assert.Equal(t, float64(0), raw["truncated_by_max_findings"],
		"truncated_by_max_findings must be zero when no max_findings cap applied")
}
