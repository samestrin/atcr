package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
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

// An interrupted review (manifest.interrupted=true) with at least one successful
// agent reports "interrupted", NOT "completed": the run was cut short by a signal,
// so the partial result set must be distinguishable from a clean completion
// (epic 4.1 AC4). Interrupted takes precedence over the succeeded/failed tally.
func TestReadReviewStatus_InterruptedWithPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta","stevie"],"started_at":"2026-06-17T12:00:00Z","timeout_secs":600,"interrupted":true,"partial":true}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", summaryFile),
		[]byte(`{"total":2,"succeeded":1,"failed":1,"partial":true,"total_findings":3}`), 0o644))
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInterrupted, st.Status, "AC4: a signal-interrupted run reports interrupted, not completed")
	assert.True(t, st.Partial, "an interrupted partial run is still partial")
	assert.Equal(t, 2, st.AgentsDone, "both agents were processed (1 ok + 1 failed)")
	assert.Equal(t, 0, st.AgentsPending)
}

// An interrupted review where every agent failed still reports "interrupted"
// (the user cut it short) rather than "failed": the run never got a fair chance
// to complete, so the cause-of-incompleteness is the interrupt, not failure.
func TestReadReviewStatus_InterruptedOverridesFailed(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2026-06-17T12:00:00Z","timeout_secs":600,"interrupted":true,"partial":false}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", summaryFile),
		[]byte(`{"total":1,"succeeded":0,"failed":1,"partial":false,"total_findings":0}`), 0o644))
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInterrupted, st.Status, "interrupted overrides failed when the run was signal-cancelled")
}

// A normal completed review (no interrupted flag) is unaffected — interrupted is
// off by default and an absent flag never reclassifies a clean completion.
func TestReadReviewStatus_NotInterruptedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2026-06-17T12:00:00Z","timeout_secs":600,"partial":false}`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sources", "pool", summaryFile),
		[]byte(`{"total":1,"succeeded":1,"failed":0,"partial":false,"total_findings":0}`), 0o644))
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunCompleted, st.Status, "a manifest without interrupted reports completed as before")
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

// TestReadReviewStatus_NonErrNotExistPastDeadlineIsStale verifies that a
// non-ErrNotExist read error on summary.json (e.g. permission denied, "is a
// directory") combined with an elapsed deadline reports stale rather than
// masking the condition as an eternal in_progress.
func TestReadReviewStatus_NonErrNotExistPastDeadlineIsStale(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	// Place a directory where summary.json should be; os.ReadFile returns an
	// "is a directory" error which is NOT os.ErrNotExist.
	require.NoError(t, os.MkdirAll(filepath.Join(poolDir, summaryFile), 0o755))
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status, "non-ErrNotExist read error past deadline must report stale, not in_progress")
}

// TestReadReviewStatus_NonErrNotExistBeforeDeadlineIsInProgress verifies that a
// non-ErrNotExist read error within the deadline window keeps reporting
// in_progress (the fan-out may still be running even if the dir is transiently
// unreadable).
func TestReadReviewStatus_NonErrNotExistBeforeDeadlineIsInProgress(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(filepath.Join(poolDir, summaryFile), 0o755))
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	withNow(t, start.Add(120*time.Second)) // 120s in; window is 600 + 60 grace = 660s
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2026-06-12T12:00:00Z","timeout_secs":600,"partial":false}`)
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "non-ErrNotExist read error within window must stay in_progress")
}

// TestReadReviewStatus_ConcurrentWritesNeverTornRead pins the Task 4 read-pair
// invariant: readers running concurrently with the finalization writes (the
// writer rewrites summary.json then the manifest, the way ExecuteReview does)
// must never observe a torn pair, a corrupt parse, or an invalid state. Run
// under -race, it also proves there is no data race between the atomic file
// writes and the reads.
func TestReadReviewStatus_ConcurrentWritesNeverTornRead(t *testing.T) {
	dir := t.TempDir()
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))

	// In-progress manifest, no summary yet (recent start → within the window, so
	// stale never fires and the only valid states are in_progress / completed).
	m := &payload.Manifest{Base: "a", Head: "b", Roster: []string{"greta", "kai"}, StartedAt: time.Now().UTC(), TimeoutSecs: 600}
	require.NoError(t, WriteManifest(dir, m))

	var wg sync.WaitGroup
	var bad int32 // count of invalid observations

	// Writer: finalize repeatedly, racing the readers — summary.json (completion
	// signal) then the finalized manifest, mirroring ExecuteReview's order.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = writeJSON(filepath.Join(poolDir, summaryFile),
				PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, TotalFindings: 1})
			fm := *m
			fm.CompletedAt = time.Now().UTC()
			_ = WriteManifest(dir, &fm)
		}
	}()

	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				st, err := ReadReviewStatus(dir, "x")
				if err != nil {
					atomic.AddInt32(&bad, 1)
					continue
				}
				switch st.Status {
				case RunInProgress: // summary not yet visible: pending == roster
					if st.AgentCount != 2 || st.AgentsPending != 2 || st.AgentsDone != 0 {
						atomic.AddInt32(&bad, 1)
					}
				case RunCompleted: // summary visible: completed tally
					if st.AgentCount != 2 || st.AgentsDone != 2 || st.AgentsPending != 0 {
						atomic.AddInt32(&bad, 1)
					}
				default: // stale/failed are impossible within the window with succeeded>0
					atomic.AddInt32(&bad, 1)
				}
			}
		}()
	}
	wg.Wait()
	assert.Zero(t, atomic.LoadInt32(&bad), "no read may observe a torn pair, corrupt parse, or invalid state")
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

// TestStatusJSON_ResponseTruncatedAlwaysPresent verifies that response_truncated
// is serialized even when false, matching the Epic 2.2 counters so consumers can
// distinguish a false value from a pre-19.5 status.json that predates the field.
func TestStatusJSON_ResponseTruncatedAlwaysPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{Agent: "greta", Status: StatusOK, ResponseTruncated: false}
	require.NoError(t, WriteStatus(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"response_truncated": false`, "false must be present for field-presence detection")

	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	assert.False(t, got.ResponseTruncated)
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

// --- Epic 19.10 F8: per-agent diagnosability fields ---

// TestStatusJSON_DiagnosabilityFieldsRoundTrip verifies the five diagnosability
// fields serialize and parse back when a per-model-sized run populates them (AC8).
func TestStatusJSON_DiagnosabilityFieldsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{
		Agent:                "dax",
		Status:               StatusOK,
		PayloadMode:          "diff",
		EffectiveBudget:      114688,
		ResolvedWindow:       32768,
		ReservedOutputTokens: 8192,
		ChunkCount:           6,
		DegradationAction:    "chunk",
	}
	require.NoError(t, WriteStatus(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Field names land in the JSON exactly as the omitempty tags declare them.
	for _, key := range []string{
		`"effective_budget": 114688`,
		`"resolved_window": 32768`,
		`"reserved_output_tokens": 8192`,
		`"chunk_count": 6`,
		`"degradation_action": "chunk"`,
	} {
		assert.Contains(t, string(data), key)
	}

	var got AgentStatus
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, int64(114688), got.EffectiveBudget)
	assert.Equal(t, 32768, got.ResolvedWindow)
	assert.Equal(t, 8192, got.ReservedOutputTokens)
	assert.Equal(t, 6, got.ChunkCount)
	assert.Equal(t, "chunk", got.DegradationAction)
}

// TestStatusJSON_DiagnosabilityFieldsOmittedWhenUnsized verifies an agent that
// never went through per-model sizing (a bare/pre-19.10 result) emits none of the
// five new keys, keeping status.json byte-identical to the pre-F8 shape.
func TestStatusJSON_DiagnosabilityFieldsOmittedWhenUnsized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	s := &AgentStatus{Agent: "greta", Status: StatusOK, PayloadMode: "diff"}
	require.NoError(t, WriteStatus(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, key := range []string{
		"effective_budget", "resolved_window", "reserved_output_tokens",
		"chunk_count", "degradation_action",
	} {
		assert.NotContains(t, string(data), key, "diagnosability field %q must be absent for an unsized agent", key)
	}
}
