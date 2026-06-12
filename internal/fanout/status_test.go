package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// --- Epic 1.5: stale inference for dead reviews ---

// withNow swaps the package clock for deterministic stale-window assertions.
func withNow(t *testing.T, fixed time.Time) {
	t.Helper()
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	t.Cleanup(func() { nowFunc = old })
}

// writeManifestOnly writes a raw manifest.json into dir with no summary.json, so
// ReadReviewStatus exercises the absent-completion-signal branch.
func writeManifestOnly(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), []byte(body), 0o644))
}

// A scaffolded review whose summary.json never appeared and whose
// StartedAt + timeout_secs + grace has elapsed is reported stale — an inferred
// terminal state, not eternal in_progress.
func TestReadReviewStatus_StaleWhenTimeoutElapsed(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status)
	assert.Equal(t, 1, st.AgentCount)
}

// Within StartedAt + timeout_secs + grace the same scaffolded review is still
// in_progress — stale must not fire early on a legitimately running fan-out.
func TestReadReviewStatus_InProgressWithinWindow(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	withNow(t, start.Add(120*time.Second)) // 120s in; window is 600 + 60 grace = 660s
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2026-06-12T12:00:00Z","timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
}

// Exactly at the StartedAt + timeout + grace boundary the review is still
// in_progress; stale requires strictly past the deadline (After, not !Before).
func TestReadReviewStatus_StaleBoundaryIsExclusive(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	withNow(t, start.Add(time.Duration(600+staleGraceSecs)*time.Second)) // exactly at the deadline
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2026-06-12T12:00:00Z","timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "at the boundary the run is not yet stale")
}

// An old manifest with no timeout_secs (zero) cannot have its deadline inferred,
// so it keeps reporting in_progress no matter how old — backward compatible
// (Epic 1.5 success criterion: old manifests load and report without stale).
func TestReadReviewStatus_NoTimeoutNeverStale(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
}

// A manifest with a zero StartedAt cannot have a deadline either; stale stays
// off even with a timeout present.
func TestReadReviewStatus_ZeroStartedAtNeverStale(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
}

// A completed review (summary.json present) is never reclassified stale even if
// StartedAt + timeout elapsed long ago: stale is inferred only from an ABSENT
// completion signal, never overriding an observed one.
func TestReadReviewStatus_CompletedNeverStale(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", summaryFile),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":0}`), 0o644))
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunCompleted, st.Status)
}

// A stale (dead) review has no completion signal, so EnsureReviewComplete must
// reject it like an in-progress one — reconciling a dead, possibly partial agent
// set would emit a complete-looking verdict from incomplete data. Unlike
// in_progress, the guidance is to re-run, not poll.
func TestEnsureReviewComplete_RejectsStale(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	err := EnsureReviewComplete(dir, "x")
	require.ErrorIs(t, err, ErrReviewStale)
	assert.Contains(t, err.Error(), "stale")
	assert.Contains(t, err.Error(), "re-run")
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
