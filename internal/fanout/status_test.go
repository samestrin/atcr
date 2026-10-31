package fanout

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
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

// A stale review is not unrecoverable: its surviving per-agent artifacts are
// exactly what RebuildPool reconstructs the pool aggregate from, and
// EnsureReviewComplete does not gate `--resume` (its only callers are
// cli/reconcile.go and internal/mcp/handlers.go). The rejection must therefore
// name the cheap recovery path, not send the operator to discard a fan-out that
// is mostly on disk already.
func TestEnsureReviewComplete_StaleNamesResumeRecovery(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	err := EnsureReviewComplete(dir, "run-42")
	require.ErrorIs(t, err, ErrReviewStale)
	assert.Contains(t, err.Error(), "--resume", "stale guidance must name the resume path")
	assert.Contains(t, err.Error(), "run-42", "the resume hint must carry the review id the operator has to pass")
	assert.Contains(t, err.Error(), "re-run", "starting over stays available as the explicit fallback")
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

// Both EnsureReviewComplete callers include MCP handlers (internal/mcp/handlers.go
// :336 and :535), and MCP clients have NO resume capability — handlers.go:226 already
// re-messages the sibling explicit-id collision error for exactly that reason ("CLI-only
// flags (--resume/--force); re-message it for MCP clients, which have neither").
//
// The adjacent in_progress message names the MCP tool FIRST ("poll atcr_status (or run
// `atcr status`)"). The stale message inverted that convention for the same callers,
// leading with a CLI-only action. It cannot name an MCP resume — none exists — so the
// action it must surface for those callers is the start-over half, via atcr_review.
func TestEnsureReviewComplete_StaleNamesAnMCPReachableAction(t *testing.T) {
	dir := t.TempDir()
	writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
	err := EnsureReviewComplete(dir, "run-42")
	require.ErrorIs(t, err, ErrReviewStale)

	assert.Contains(t, err.Error(), "atcr_review",
		"an MCP client cannot run `atcr review --resume`; the guidance must name the tool it CAN invoke, "+
			"mirroring how the in_progress sibling names atcr_status")
	// The CLI recovery stays primary — resume is cheaper than starting over.
	assert.Contains(t, err.Error(), "--resume", "the cheap CLI recovery is still named")
	assert.Contains(t, err.Error(), "run-42")
}

// --- Epic 35.16.14.1 T3: live counts and per-persona rows ---

// liveReview scaffolds an in-window review with roster and no summary.json, and
// returns its dir and raw/agent dir.
func liveReview(t *testing.T, roster ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	m := &payload.Manifest{Base: "a", Head: "b", Roster: roster, StartedAt: time.Now().UTC(), TimeoutSecs: 600}
	require.NoError(t, WriteManifest(dir, m))
	raw := filepath.Join(dir, "sources", "pool", poolRawAgentDir)
	require.NoError(t, os.MkdirAll(raw, 0o755))
	return dir, raw
}

func writeAgentStatus(t *testing.T, raw, agent, status string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(raw, agent), 0o755))
	require.NoError(t, WriteStatus(filepath.Join(raw, agent, statusFile), &AgentStatus{Agent: agent, Status: status}))
}

func writeAgentChunks(t *testing.T, raw, agent string, outcomes ...string) {
	t.Helper()
	rec := chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: agent}
	for i, o := range outcomes {
		rec.Bins = append(rec.Bins, chunkRecordBin{Index: i, Files: []string{"f.go"}, Outcome: o})
	}
	require.NoError(t, os.MkdirAll(filepath.Join(raw, agent), 0o755))
	require.NoError(t, writeJSON(filepath.Join(raw, agent, chunksFile), rec))
}

func agentRow(t *testing.T, st *ReviewStatus, name string) AgentProgress {
	t.Helper()
	for _, r := range st.Agents {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no agents row for %q", name)
	return AgentProgress{}
}

// While the fan-out runs, the personas that wrote status.json count as done
// before summary.json exists.
func TestReadReviewStatus_InProgress_IncrementalAgents(t *testing.T) {
	dir, raw := liveReview(t, "agent1", "agent2", "agent3", "agent4")
	writeAgentStatus(t, raw, "agent2", StatusOK)
	writeAgentStatus(t, raw, "agent1", StatusOK)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
	assert.Equal(t, 4, st.AgentCount)
	assert.Equal(t, 2, st.AgentsDone)
	assert.Equal(t, 2, st.AgentsPending)
	assert.Equal(t, []string{"agent1", "agent2"}, st.CompletedAgents)
	assert.Equal(t, StageFanout, st.CurrentStage)
	assert.False(t, st.Partial)

	require.Len(t, st.Agents, 4)
	for i, name := range []string{"agent1", "agent2", "agent3", "agent4"} {
		assert.Equal(t, name, st.Agents[i].Name, "rows follow roster order")
	}
	done := agentRow(t, st, "agent1")
	assert.Equal(t, AgentDone, done.State)
	assert.Equal(t, 1, done.ChunksDone)
	assert.Equal(t, 1, done.ChunksTotal)
	ts, err := time.Parse(time.RFC3339, done.LastActivityAt)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), ts, time.Minute)

	pending := agentRow(t, st, "agent3")
	assert.Equal(t, AgentPending, pending.State)
	assert.Equal(t, 0, pending.ChunksDone)
	assert.Equal(t, 1, pending.ChunksTotal)
	assert.Empty(t, pending.LastActivityAt)
}

// A failed or timed-out persona has finished: it counts as done, is named in
// completed_agents, and its row carries the outcome.
func TestReadReviewStatus_InProgress_FailedAndTimeoutCountAsDone(t *testing.T) {
	dir, raw := liveReview(t, "a", "b", "c")
	writeAgentStatus(t, raw, "a", StatusFailed)
	writeAgentStatus(t, raw, "b", StatusTimeout)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, 2, st.AgentsDone)
	assert.Equal(t, 1, st.AgentsPending)
	assert.Equal(t, []string{"a", "b"}, st.CompletedAgents)
	assert.Equal(t, AgentFailed, agentRow(t, st, "a").State)
	assert.Equal(t, AgentTimeout, agentRow(t, st, "b").State)
	assert.Equal(t, AgentPending, agentRow(t, st, "c").State)
}

// A chunked persona's chunks.json gives its chunk counts and moves its last
// activity, before its status.json exists.
func TestReadReviewStatus_ChunkCountsFromChunksJSON(t *testing.T) {
	dir, raw := liveReview(t, "chunky", "solo")
	writeAgentChunks(t, raw, "chunky", StatusOK, StatusFailed, chunkOutcomePending, chunkOutcomePending, chunkOutcomeDeferred)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	row := agentRow(t, st, "chunky")
	assert.Equal(t, AgentPending, row.State, "no status.json yet")
	assert.Equal(t, 2, row.ChunksDone)
	assert.Equal(t, 5, row.ChunksTotal)
	assert.NotEmpty(t, row.LastActivityAt)
	assert.Equal(t, 0, st.AgentsDone)
	assert.Empty(t, st.CompletedAgents)

	writeAgentChunks(t, raw, "chunky", StatusOK, StatusOK, StatusOK)
	writeAgentStatus(t, raw, "chunky", StatusOK)
	st, err = ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	row = agentRow(t, st, "chunky")
	assert.Equal(t, AgentDone, row.State)
	assert.Equal(t, 3, row.ChunksDone)
	assert.Equal(t, 3, row.ChunksTotal)
}

// last_activity_at is the newer of status.json and chunks.json.
func TestReadReviewStatus_LastActivityIsNewestWrite(t *testing.T) {
	dir, raw := liveReview(t, "p")
	writeAgentChunks(t, raw, "p", StatusOK)
	writeAgentStatus(t, raw, "p", StatusOK)
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filepath.Join(raw, "p", statusFile), older, older))
	require.NoError(t, os.Chtimes(filepath.Join(raw, "p", chunksFile), newer, newer))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01T00:05:00Z", agentRow(t, st, "p").LastActivityAt)

	require.NoError(t, os.Chtimes(filepath.Join(raw, "p", statusFile), newer.Add(time.Minute), newer.Add(time.Minute)))
	st, err = ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-01T00:06:00Z", agentRow(t, st, "p").LastActivityAt)
}

// A corrupt status.json or chunks.json is skipped, never an error for the
// whole read: the persona reads as if the file were absent.
func TestReadReviewStatus_CorruptAgentFilesSkipped(t *testing.T) {
	dir, raw := liveReview(t, "bad-status", "bad-chunks", "wrong-agent", "good")
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "bad-status"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "bad-status", statusFile), []byte("{not json"), 0o644))
	writeAgentStatus(t, raw, "bad-chunks", StatusOK)
	require.NoError(t, os.WriteFile(filepath.Join(raw, "bad-chunks", chunksFile), []byte("[[["), 0o644))
	// A status.json naming another agent is not this persona's record.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "wrong-agent"), 0o755))
	require.NoError(t, WriteStatus(filepath.Join(raw, "wrong-agent", statusFile), &AgentStatus{Agent: "someone-else", Status: StatusOK}))
	writeAgentStatus(t, raw, "good", StatusOK)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
	assert.Equal(t, 2, st.AgentsDone)
	assert.Equal(t, 2, st.AgentsPending)
	assert.Equal(t, []string{"bad-chunks", "good"}, st.CompletedAgents)
	assert.Equal(t, AgentPending, agentRow(t, st, "bad-status").State)
	assert.Equal(t, AgentPending, agentRow(t, st, "wrong-agent").State)
	bc := agentRow(t, st, "bad-chunks")
	assert.Equal(t, AgentDone, bc.State)
	assert.Equal(t, 1, bc.ChunksDone, "a corrupt chunks.json falls back to the single-chunk reading")
	assert.Equal(t, 1, bc.ChunksTotal)
}

// Only roster personas are visited, each at its agentDirName: a stray directory
// under raw/agent is not counted.
func TestReadReviewStatus_OnlyRosterPersonasVisited(t *testing.T) {
	dir, raw := liveReview(t, "greta")
	writeAgentStatus(t, raw, "orphan", StatusOK)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, 0, st.AgentsDone)
	assert.Equal(t, 1, st.AgentsPending)
	assert.Empty(t, st.CompletedAgents)
	require.Len(t, st.Agents, 1)
	assert.Equal(t, "greta", st.Agents[0].Name)
}

// A chunks.json or status.json symlinked outside the review tree is refused by
// pathWithin: its counts and its write time are never read.
func TestReadReviewStatus_SymlinkOutsideTreeRefused(t *testing.T) {
	dir, raw := liveReview(t, "linked", "linkedstatus")
	outside := t.TempDir()

	// A chunks.json pointing outside the tree, with an out-of-tree mtime.
	extRec := chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: "linked",
		Bins: []chunkRecordBin{{Index: 0, Outcome: StatusOK}, {Index: 1, Outcome: StatusOK}, {Index: 2, Outcome: StatusOK}}}
	extChunks := filepath.Join(outside, "chunks.json")
	require.NoError(t, writeJSON(extChunks, extRec))
	when := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	require.NoError(t, os.Chtimes(extChunks, when, when))
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "linked"), 0o755))
	if err := os.Symlink(extChunks, filepath.Join(raw, "linked", chunksFile)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// A status.json pointing outside the tree.
	extStatus := filepath.Join(outside, "status.json")
	require.NoError(t, WriteStatus(extStatus, &AgentStatus{Agent: "linkedstatus", Status: StatusOK}))
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "linkedstatus"), 0o755))
	require.NoError(t, os.Symlink(extStatus, filepath.Join(raw, "linkedstatus", statusFile)))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	row := agentRow(t, st, "linked")
	assert.Equal(t, 0, row.ChunksDone, "an escaping chunks.json is never read")
	assert.Equal(t, 1, row.ChunksTotal)
	assert.Empty(t, row.LastActivityAt, "an escaping file's write time never leaks")

	ls := agentRow(t, st, "linkedstatus")
	assert.Equal(t, AgentPending, ls.State, "an escaping status.json is not a completion record")
	assert.Empty(t, ls.LastActivityAt)
	assert.Equal(t, 0, st.AgentsDone)
	assert.Empty(t, st.CompletedAgents)
}

// Once summary.json lands the counts are its final tally, as before, and the
// rows stay.
func TestReadReviewStatus_SummaryCountsWinOverLiveCounts(t *testing.T) {
	dir, raw := liveReview(t, "a", "b")
	writeAgentStatus(t, raw, "a", StatusOK)
	require.NoError(t, writeJSON(filepath.Join(dir, "sources", "pool", summaryFile),
		PoolSummary{Total: 2, Succeeded: 1, Failed: 1}))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunCompleted, st.Status)
	assert.Equal(t, 2, st.AgentsDone)
	assert.Equal(t, 0, st.AgentsPending)
	assert.Equal(t, []string{"a"}, st.CompletedAgents)
	assert.Len(t, st.Agents, 2)
}

// A raw/agent dir that is itself a symlink out of the review tree is refused
// too: the guard is the review tree, not the directory the files sit in.
func TestReadReviewStatus_SymlinkedRawDirRefused(t *testing.T) {
	dir := t.TempDir()
	m := &payload.Manifest{Base: "a", Head: "b", Roster: []string{"greta"}, StartedAt: time.Now().UTC(), TimeoutSecs: 600}
	require.NoError(t, WriteManifest(dir, m))
	outside := t.TempDir()
	writeAgentStatus(t, outside, "greta", StatusOK)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	if err := os.Symlink(outside, filepath.Join(dir, "sources", "pool", poolRawAgentDir)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, 0, st.AgentsDone)
	assert.Equal(t, AgentPending, agentRow(t, st, "greta").State)
	assert.Empty(t, agentRow(t, st, "greta").LastActivityAt)
}

// A chunks.json that fails the trust checks (an unknown schema_version, a
// record naming another persona, or no bins) is skipped: the row falls back to
// the single-chunk reading, and the file never counts the persona as done.
func TestReadReviewStatus_UntrustedChunksJSONSkipped(t *testing.T) {
	dir, raw := liveReview(t, "old-schema", "other-agent", "no-bins", "done-other")
	write := func(agent string, rec chunkRecord) {
		require.NoError(t, os.MkdirAll(filepath.Join(raw, agent), 0o755))
		require.NoError(t, writeJSON(filepath.Join(raw, agent, chunksFile), rec))
	}
	bins := []chunkRecordBin{{Index: 0, Outcome: StatusOK}, {Index: 1, Outcome: StatusOK}, {Index: 2, Outcome: chunkOutcomePending}}
	write("old-schema", chunkRecord{SchemaVersion: chunksSchemaVersion + 1, Agent: "old-schema", Bins: bins})
	write("other-agent", chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: "someone-else", Bins: bins})
	write("no-bins", chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: "no-bins"})
	// A persona with a valid status.json keeps its done reading over a
	// misattributed chunks.json.
	writeAgentStatus(t, raw, "done-other", StatusOK)
	write("done-other", chunkRecord{SchemaVersion: chunksSchemaVersion, Agent: "someone-else", Bins: bins})

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	for _, name := range []string{"old-schema", "other-agent", "no-bins"} {
		row := agentRow(t, st, name)
		assert.Equal(t, AgentPending, row.State, name)
		assert.Equal(t, 0, row.ChunksDone, name)
		assert.Equal(t, 1, row.ChunksTotal, name)
		assert.Empty(t, row.LastActivityAt, "%s: an untrusted chunks.json's write time is not activity", name)
	}
	row := agentRow(t, st, "done-other")
	assert.Equal(t, AgentDone, row.State)
	assert.Equal(t, 1, row.ChunksDone)
	assert.Equal(t, 1, row.ChunksTotal)
	assert.Equal(t, 1, st.AgentsDone)
	assert.Equal(t, []string{"done-other"}, st.CompletedAgents)
}

// A status.json or chunks.json that is a directory, larger than its read cap,
// or unreadable is skipped: the read still succeeds and the persona reads as if
// the file were absent.
func TestReadReviewStatus_UnreadableAgentFilesSkipped(t *testing.T) {
	prev := maxAgentFileBytes
	maxAgentFileBytes = 4 << 10
	defer func() { maxAgentFileBytes = prev }()

	dir, raw := liveReview(t, "dir-status", "dir-chunks", "big-status", "big-chunks", "noperm", "good")
	// Directories where the files should be.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "dir-status", statusFile), 0o755))
	writeAgentStatus(t, raw, "dir-chunks", StatusOK)
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "dir-chunks", chunksFile), 0o755))
	// A status.json over maxAgentFileBytes, still valid JSON for its persona.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "big-status"), 0o755))
	big := AgentStatus{Agent: "big-status", Status: StatusOK, FilesDropped: []string{strings.Repeat("p", 8<<10)}}
	require.NoError(t, WriteStatus(filepath.Join(raw, "big-status", statusFile), &big))
	fi, err := os.Stat(filepath.Join(raw, "big-status", statusFile))
	require.NoError(t, err)
	require.Greater(t, fi.Size(), maxAgentFileBytes)
	// A chunks.json over maxChunksBytes.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "big-chunks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "big-chunks", chunksFile), make([]byte, maxChunksBytes+1), 0o644))
	// A status.json that stats but cannot be opened.
	writeAgentStatus(t, raw, "noperm", StatusOK)
	noperm := filepath.Join(raw, "noperm", statusFile)
	require.NoError(t, os.Chmod(noperm, 0))
	t.Cleanup(func() { _ = os.Chmod(noperm, 0o644) })
	_, readErr := os.ReadFile(noperm)
	// A short status.json under the cap.
	require.NoError(t, os.MkdirAll(filepath.Join(raw, "good"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(raw, "good", statusFile), []byte(`{"agent":"good","status":"ok"}`), 0o644))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
	for _, name := range []string{"dir-status", "big-status"} {
		row := agentRow(t, st, name)
		assert.Equal(t, AgentPending, row.State, name)
		assert.Empty(t, row.LastActivityAt, name)
	}
	assert.Equal(t, 1, agentRow(t, st, "big-chunks").ChunksTotal)
	assert.Equal(t, 0, agentRow(t, st, "big-chunks").ChunksDone)
	dc := agentRow(t, st, "dir-chunks")
	assert.Equal(t, AgentDone, dc.State, "a directory chunks.json leaves status.json's reading")
	assert.Equal(t, 1, dc.ChunksTotal)
	want := []string{"dir-chunks", "good"}
	if readErr == nil {
		// Permissions do not bind this user (root), so the file reads normally.
		want = []string{"dir-chunks", "good", "noperm"}
	} else {
		assert.Equal(t, AgentPending, agentRow(t, st, "noperm").State, "an unreadable status.json is skipped")
	}
	assert.Equal(t, want, st.CompletedAgents)
	assert.Equal(t, len(want), st.AgentsDone)
}

// A roster name agentDirName rejects ("." or "..") gets no row and reads no
// file, even when a status.json naming it sits where its empty dir name would
// point; the read still succeeds.
func TestReadReviewStatus_RejectedAgentDirNameSkipped(t *testing.T) {
	dir, raw := liveReview(t, "..", ".", "greta")
	require.NoError(t, WriteStatus(filepath.Join(raw, statusFile), &AgentStatus{Agent: "..", Status: StatusOK}))
	writeAgentStatus(t, raw, "greta", StatusOK)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	require.Len(t, st.Agents, 1)
	assert.Equal(t, "greta", st.Agents[0].Name)
	assert.Equal(t, []string{"greta"}, st.CompletedAgents)
	assert.Equal(t, 1, st.AgentsDone)
	assert.Equal(t, 3, st.AgentCount)
	assert.Equal(t, 2, st.AgentsPending)
}

// A roster that repeats a name yields one row and counts the persona once.
func TestReadReviewStatus_DuplicateRosterNameCountedOnce(t *testing.T) {
	dir, raw := liveReview(t, "greta", "kai", "greta")
	writeAgentStatus(t, raw, "greta", StatusOK)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	require.Len(t, st.Agents, 2)
	assert.Equal(t, "greta", st.Agents[0].Name)
	assert.Equal(t, "kai", st.Agents[1].Name)
	assert.Equal(t, []string{"greta"}, st.CompletedAgents)
	assert.Equal(t, 1, st.AgentsDone)
}

// --- Epic 35.16.14.1 T4: stage after the fan-out ---

func writeSummary(t *testing.T, dir string, ps PoolSummary) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, writeJSON(filepath.Join(dir, "sources", "pool", summaryFile), ps))
}

func writeMarker(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", name), []byte("{}\n"), 0o644))
}

func setManifestStages(t *testing.T, dir string, stages ...string) {
	t.Helper()
	m, err := ReadManifest(dir)
	require.NoError(t, err)
	m.Stages = stages
	require.NoError(t, WriteManifest(dir, m))
}

func stageOf(t *testing.T, dir string) string {
	t.Helper()
	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	return st.CurrentStage
}

// The stage is the latest one that finished, walking forward as each marker
// file lands; manifest.stages names verify and debate as well.
func TestReadReviewStatus_StageInference(t *testing.T) {
	t.Run("marker files", func(t *testing.T) {
		dir, raw := liveReview(t, "greta")
		assert.Equal(t, StageFanout, stageOf(t, dir))

		writeAgentStatus(t, raw, "greta", StatusOK)
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1})
		assert.Equal(t, StageReviewed, stageOf(t, dir))

		writeMarker(t, dir, "findings.json")
		assert.Equal(t, StageReconciled, stageOf(t, dir))

		writeMarker(t, dir, "verification.json")
		assert.Equal(t, StageVerified, stageOf(t, dir))

		writeMarker(t, dir, "debate.json")
		assert.Equal(t, StageDebated, stageOf(t, dir))
	})

	t.Run("manifest stages", func(t *testing.T) {
		dir, _ := liveReview(t, "greta")
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1})
		setManifestStages(t, dir, "review")
		assert.Equal(t, StageReviewed, stageOf(t, dir))

		setManifestStages(t, dir, "review", "verify")
		assert.Equal(t, StageVerified, stageOf(t, dir))

		setManifestStages(t, dir, "review", "verify", "debate")
		assert.Equal(t, StageDebated, stageOf(t, dir))
	})

	t.Run("highest wins", func(t *testing.T) {
		// debate.json with no verification.json, and a manifest that lists only
		// verify: the highest stage found is reported, never a lower one.
		dir, _ := liveReview(t, "greta")
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1})
		writeMarker(t, dir, "findings.json")
		writeMarker(t, dir, "debate.json")
		setManifestStages(t, dir, "review", "verify")
		assert.Equal(t, StageDebated, stageOf(t, dir))

		dir2, _ := liveReview(t, "greta")
		writeSummary(t, dir2, PoolSummary{Total: 1, Succeeded: 1})
		writeMarker(t, dir2, "findings.json")
		setManifestStages(t, dir2, "review", "debate")
		assert.Equal(t, StageDebated, stageOf(t, dir2))
	})

	t.Run("marker that is not a file", func(t *testing.T) {
		// A directory named like a marker is not a finished stage.
		dir, _ := liveReview(t, "greta")
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1})
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled", "findings.json"), 0o755))
		assert.Equal(t, StageReviewed, stageOf(t, dir))
	})

	t.Run("marker symlinked outside the review", func(t *testing.T) {
		dir, _ := liveReview(t, "greta")
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1})
		outside := filepath.Join(t.TempDir(), "findings.json")
		require.NoError(t, os.WriteFile(outside, []byte("{}"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "reconciled"), 0o755))
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, "reconciled", "findings.json")))
		assert.Equal(t, StageReviewed, stageOf(t, dir))
	})

	t.Run("failed run", func(t *testing.T) {
		dir, _ := liveReview(t, "greta")
		writeSummary(t, dir, PoolSummary{Total: 1, Failed: 1})
		writeMarker(t, dir, "findings.json")
		st, err := ReadReviewStatus(dir, "x")
		require.NoError(t, err)
		assert.Equal(t, RunFailed, st.Status)
		assert.Equal(t, RunFailed, st.CurrentStage)
	})

	t.Run("interrupted run", func(t *testing.T) {
		dir, _ := liveReview(t, "greta")
		m, err := ReadManifest(dir)
		require.NoError(t, err)
		m.Interrupted = true
		require.NoError(t, WriteManifest(dir, m))
		writeSummary(t, dir, PoolSummary{Total: 1, Succeeded: 1, Partial: true})
		writeMarker(t, dir, "verification.json")
		st, err := ReadReviewStatus(dir, "x")
		require.NoError(t, err)
		assert.Equal(t, RunInterrupted, st.Status)
		assert.Equal(t, RunInterrupted, st.CurrentStage)
	})

	t.Run("stale run", func(t *testing.T) {
		dir := t.TempDir()
		writeManifestOnly(t, dir, `{"base":"a","head":"b","roster":["greta"],"started_at":"2020-01-01T00:00:00Z","timeout_secs":600,"partial":false}`)
		st, err := ReadReviewStatus(dir, "x")
		require.NoError(t, err)
		assert.Equal(t, RunStale, st.Status)
		assert.Equal(t, RunStale, st.CurrentStage)
	})
}

// --- Epic 78 T1: staleness from activity across all bins ---

// staleRun scaffolds a summary-less review that started at start with a 600s
// base timeout, and returns its dir and raw/agent dir.
func staleRun(t *testing.T, start time.Time, roster ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	m := &payload.Manifest{Base: "a", Head: "b", Roster: roster, StartedAt: start, TimeoutSecs: 600}
	require.NoError(t, WriteManifest(dir, m))
	raw := filepath.Join(dir, "sources", "pool", poolRawAgentDir)
	require.NoError(t, os.MkdirAll(raw, 0o755))
	return dir, raw
}

func touchAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(path, at, at))
}

// A live multi-bin run outlives StartedAt + timeout_secs: its bins resolve one
// after another and each one rewrites chunks.json. One persona went quiet past
// the deadline, another resolved a bin a minute ago. The run is still live, so
// it is in_progress, and EnsureReviewComplete says to poll, not to resume.
func TestReadReviewStatus_LiveMultiBinRunIsNotStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour) // far past start + 600s + grace
	withNow(t, now)
	dir, raw := staleRun(t, start, "quiet", "busy")
	writeAgentChunks(t, raw, "quiet", StatusTimeout, chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "quiet", chunksFile), start.Add(time.Minute))
	writeAgentChunks(t, raw, "busy", StatusOK, StatusOK, chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "busy", chunksFile), now.Add(-time.Minute))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "a bin resolved a minute ago: the run is live")
	assert.Equal(t, StageFanout, st.CurrentStage)

	err = EnsureReviewComplete(dir, "run-7")
	require.ErrorIs(t, err, ErrReviewInProgress)
	assert.NotErrorIs(t, err, ErrReviewStale)
	assert.NotContains(t, err.Error(), "--resume", "a live run must not be resumed")
}

// A persona's status.json write is activity too, not only chunks.json.
func TestReadReviewStatus_RecentStatusWriteIsNotStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour)
	withNow(t, now)
	dir, raw := staleRun(t, start, "done", "pending")
	writeAgentStatus(t, raw, "done", StatusOK)
	touchAt(t, filepath.Join(raw, "done", statusFile), now.Add(-5*time.Minute))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
}

// One bin of a chunked persona may run for the base timeout scaled by its chunk
// count (unitDeadline), so a quiet gap longer than the base timeout but inside
// the scaled one is still live.
func TestReadReviewStatus_QuietGapWithinScaledBinDeadlineIsNotStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	last := start.Add(time.Hour)
	withNow(t, last.Add(1000*time.Second)) // > 600+60, < 3*600+60
	dir, raw := staleRun(t, start, "chunky")
	writeAgentChunks(t, raw, "chunky", StatusOK, chunkOutcomePending, chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "chunky", chunksFile), last)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
}

// A run whose newest write is older than the scaled bin deadline, the longest
// requeue wait and grace is dead: stale, and EnsureReviewComplete names resume.
func TestReadReviewStatus_NoActivityPastScaledDeadlineIsStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	last := start.Add(time.Hour)
	withNow(t, last.Add(time.Duration(3*600+staleGraceSecs+1)*time.Second+maxUnitRequeueWait(defaultUnitRetryPolicy())))
	dir, raw := staleRun(t, start, "chunky", "solo")
	writeAgentChunks(t, raw, "chunky", StatusOK, chunkOutcomePending, chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "chunky", chunksFile), last)
	writeAgentStatus(t, raw, "solo", StatusOK)
	touchAt(t, filepath.Join(raw, "solo", statusFile), start.Add(time.Minute))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status)

	err = EnsureReviewComplete(dir, "run-7")
	require.ErrorIs(t, err, ErrReviewStale)
	assert.Contains(t, err.Error(), "--resume")
}

// Exactly at newest write + scaled deadline + longest requeue wait + grace the
// run is not yet stale.
func TestReadReviewStatus_ActivityStaleBoundaryIsExclusive(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	last := start.Add(time.Hour)
	boundary := last.Add(time.Duration(2*600+staleGraceSecs)*time.Second + maxUnitRequeueWait(defaultUnitRetryPolicy()))
	withNow(t, boundary)
	dir, raw := staleRun(t, start, "chunky")
	writeAgentChunks(t, raw, "chunky", StatusOK, chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "chunky", chunksFile), last)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)

	withNow(t, boundary.Add(time.Second))
	st, err = ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status, "one second past the boundary the run is stale")
}

// A unit that failed retryably flushes chunks.json at the requeue, then waits a
// backoff before it is leased again, and the new lease gets a full unit
// deadline. A rate limit that quiets every lane at once leaves no other write
// to move the anchor, so the longest wait (300s for the third rate-limit hop)
// plus one unit deadline must still read live.
func TestReadReviewStatus_RequeuedBinWaitingOutBackoffIsNotStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	requeuedAt := start.Add(time.Hour)
	// Past the unit window (600+60) but inside the longest backoff + unit
	// deadline + grace (300+600+60).
	withNow(t, requeuedAt.Add(900*time.Second))
	dir, raw := staleRun(t, start, "solo", "other")
	writeAgentChunks(t, raw, "solo", chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "solo", chunksFile), requeuedAt)
	// Single-bin personas, so no chunk scaling widens the window past 660s.
	writeAgentStatus(t, raw, "other", StatusOK)
	touchAt(t, filepath.Join(raw, "other", statusFile), requeuedAt.Add(-time.Minute))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "a requeued unit may still be waiting out its backoff")

	err = EnsureReviewComplete(dir, "run-7")
	require.ErrorIs(t, err, ErrReviewInProgress)
	assert.NotContains(t, err.Error(), "--resume", "a live run must not be resumed")
}

// The longest single wait between a requeue and the next lease is the largest
// hop either retry budget allows: the third rate-limit backoff by default.
func TestMaxUnitRequeueWait_DefaultPolicy(t *testing.T) {
	assert.Equal(t, rateLimitBackoff<<(unitMaxAttempts-2), maxUnitRequeueWait(defaultUnitRetryPolicy()))
}

// A hop the summed budget refuses is never waited, and the circuit wait counts
// when it is the longest.
func TestMaxUnitRequeueWait_RespectsBudgets(t *testing.T) {
	p := unitRetryPolicy{
		maxAttempts: 4, budget: 100 * time.Second, backoff: unitBackoff,
		circuitWait: 90 * time.Second, circuitBudget: 30 * time.Minute,
	}
	assert.Equal(t, 90*time.Second, maxUnitRequeueWait(p), "75s fits the budget, 150s does not; the 90s circuit wait is longer")
	p.circuitBudget = 60 * time.Second
	assert.Equal(t, 75*time.Second, maxUnitRequeueWait(p), "a circuit wait over its own budget is never waited")
}

// --- Epic 78 fix F2: a future activity time cannot wedge the stale check ---

// singleBinWindow is the stale window of a staleRun whose personas each have one
// bin: the base timeout, the longest requeue wait and the grace margin.
func singleBinWindow() time.Duration {
	return time.Duration(600+staleGraceSecs)*time.Second + maxUnitRequeueWait(defaultUnitRetryPolicy())
}

// A status.json whose mtime is days ahead (writer clock skew, a restored
// artifact that kept its timestamps) is not believable activity. Taken at face
// value it would push the deadline days out, so a dead review would read
// in_progress until the wall clock caught up and EnsureReviewComplete would
// never return ErrReviewStale. Clamping it to now does not help either: the
// anchor would then be now on every read until the clock passes the mtime.
// It is ignored, so a run that started past its deadline reads stale.
func TestReadReviewStatus_FarFutureActivityDoesNotWedgeStale(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour) // far past start + window
	withNow(t, now)
	dir, raw := staleRun(t, start, "skewed")
	writeAgentStatus(t, raw, "skewed", StatusOK)
	touchAt(t, filepath.Join(raw, "skewed", statusFile), now.Add(72*time.Hour))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status, "a mtime days in the future must not keep a dead run in_progress")

	err = EnsureReviewComplete(dir, "run-7")
	require.ErrorIs(t, err, ErrReviewStale)
}

// A future-dated file does not hide real activity beside it: a persona that
// wrote a minute ago keeps the run in_progress, and once the window passes from
// that write the run reads stale, the far-future mtime notwithstanding.
func TestReadReviewStatus_FarFutureActivityBesideLiveWrite(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour)
	withNow(t, now)
	dir, raw := staleRun(t, start, "skewed", "live")
	writeAgentStatus(t, raw, "skewed", StatusOK)
	touchAt(t, filepath.Join(raw, "skewed", statusFile), now.Add(72*time.Hour))
	writeAgentChunks(t, raw, "live", chunkOutcomePending)
	last := now.Add(-time.Minute)
	touchAt(t, filepath.Join(raw, "live", chunksFile), last)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "a persona wrote a minute ago: the run is live")

	withNow(t, last.Add(singleBinWindow()+time.Second))
	st, err = ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status, "the window has passed since the last believable write")
}

// A mtime a few seconds ahead is ordinary clock slop between the writer and
// the reader, inside the grace margin: it counts as activity at now, so a
// live run that just wrote is not misread as stale.
func TestReadReviewStatus_SlightlyFutureActivityCountsAsNow(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour)
	withNow(t, now)
	dir, raw := staleRun(t, start, "solo")
	writeAgentChunks(t, raw, "solo", chunkOutcomePending)
	touchAt(t, filepath.Join(raw, "solo", chunksFile), now.Add(30*time.Second))

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status)
	require.Len(t, st.Agents, 1)
	assert.Equal(t, now.Format(time.RFC3339), st.Agents[0].LastActivityAt, "slop ahead of now is clamped to now")

	// Just past the grace margin ahead it is no longer believable.
	touchAt(t, filepath.Join(raw, "solo", chunksFile), now.Add((staleGraceSecs+1)*time.Second))
	st, err = ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunStale, st.Status)
	assert.Empty(t, st.Agents[0].LastActivityAt, "an unbelievable write is not reported as activity")
}

// --- Epic 90 T4: the stale window survives an unreadable chunks.json ---

// writeChunkedStatus writes a finished chunked persona's status.json, which
// records its bin count.
func writeChunkedStatus(t *testing.T, raw, agent string, chunks int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(raw, agent), 0o755))
	require.NoError(t, WriteStatus(filepath.Join(raw, agent, statusFile), &AgentStatus{Agent: agent, Status: StatusOK, ChunkCount: chunks}))
}

// A finished chunked persona whose chunks.json is missing, oversize or corrupt
// takes its bin count from status.json's chunk_count, so its row does not
// shrink to one chunk; a valid chunks.json still wins.
func TestReadReviewStatus_StatusChunkCountIsChunksTotalFallback(t *testing.T) {
	dir, raw := liveReview(t, "missing", "oversize", "corrupt", "valid")
	writeChunkedStatus(t, raw, "missing", 4)
	writeChunkedStatus(t, raw, "oversize", 4)
	require.NoError(t, os.WriteFile(filepath.Join(raw, "oversize", chunksFile), make([]byte, maxChunksBytes+1), 0o644))
	writeChunkedStatus(t, raw, "corrupt", 4)
	require.NoError(t, os.WriteFile(filepath.Join(raw, "corrupt", chunksFile), []byte("[[["), 0o644))
	writeChunkedStatus(t, raw, "valid", 4)
	writeAgentChunks(t, raw, "valid", StatusOK, StatusOK, chunkOutcomePending)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	for _, name := range []string{"missing", "oversize", "corrupt"} {
		row := agentRow(t, st, name)
		assert.Equal(t, AgentDone, row.State, name)
		assert.Equal(t, 4, row.ChunksTotal, "%s: status.json's chunk_count is the fallback", name)
		assert.Equal(t, 4, row.ChunksDone, "%s: a finished persona resolved every bin", name)
	}
	row := agentRow(t, st, "valid")
	assert.Equal(t, 3, row.ChunksTotal, "a valid chunks.json wins over status.json")
	assert.Equal(t, 2, row.ChunksDone)
}

// The fallback keeps the stale window wide: a quiet gap past the single-bin
// window but inside the window scaled by status.json's chunk_count is live.
func TestReadReviewStatus_CorruptChunksJSONKeepsScaledStaleWindow(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	last := start.Add(time.Hour)
	withNow(t, last.Add(singleBinWindow()+time.Second))
	dir, raw := staleRun(t, start, "chunky")
	writeChunkedStatus(t, raw, "chunky", 3)
	require.NoError(t, os.WriteFile(filepath.Join(raw, "chunky", chunksFile), []byte("{not json"), 0o644))
	touchAt(t, filepath.Join(raw, "chunky", statusFile), last)

	st, err := ReadReviewStatus(dir, "x")
	require.NoError(t, err)
	assert.Equal(t, RunInProgress, st.Status, "the window is scaled by the 3 bins status.json records")
}

// probeClock runs probe on every backoff wait before the fake clock serves it.
type probeClock struct {
	*fakeUnitClock
	probe func(d time.Duration)
}

func (c probeClock) After(d time.Duration) <-chan time.Time {
	c.probe(d)
	return c.fakeUnitClock.After(d)
}

// circuitOpenOnce fails its first call fast on an open breaker, then reviews.
type circuitOpenOnce struct{ calls atomic.Int32 }

func (c *circuitOpenOnce) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if c.calls.Add(1) == 1 {
		return "", &llmclient.CircuitOpenError{Provider: "p"}
	}
	return baselineChunkFindingCompleter{}.Complete(ctx, inv)
}

// The stale window adds one maxUnitRequeueWait because a requeued unit writes
// chunks.json before it waits. An open breaker's requeue does too: while the
// unit waits out the breaker, chunks.json already holds its bin pending with
// the lease counted, so the wait starts from a fresh write.
func TestReadReviewStatus_OpenBreakerRequeueFlushedChunksJSON(t *testing.T) {
	slots := sixteenBaselineSlots(t)
	poolDir := t.TempDir()
	ledger := newChunkLedger(slog.New(slog.DiscardHandler), poolDir, slots)
	require.NotNil(t, ledger)
	ledger.writePlan()
	agent := slots[0].Primary.Name

	var waits []time.Duration
	// The probe runs on the engine's worker goroutine, where require/FailNow
	// must not be called: it only captures, and the test goroutine asserts.
	var seen []chunkRecord
	var seenErrs []error
	clock := probeClock{fakeUnitClock: newFakeUnitClock(), probe: func(d time.Duration) {
		if d <= 0 {
			return
		}
		waits = append(waits, d)
		var rec chunkRecord
		data, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, agent, chunksFile))
		if err == nil {
			err = json.Unmarshal(data, &rec)
		}
		seen = append(seen, rec)
		seenErrs = append(seenErrs, err)
	}}
	c := &circuitOpenOnce{}
	// Only the first bin runs, so no other bin's write can carry its state to
	// disk: what the wait sees is the requeue's own flush.
	results := NewEngine(c, WithMaxParallel(1), WithSlotStart(ledger.slotStart), WithSlotDone(ledger.slotDone),
		WithUnitRetry(ledger.retryable, ledger.slotRequeue), withUnitClock(clock)).
		Run(context.Background(), slots[:1])

	require.Len(t, results, 1)
	assert.Equal(t, StatusOK, results[0].Status)
	assert.Equal(t, 1, results[0].Requeues)
	require.Equal(t, []time.Duration{circuitOpenBackoff}, waits, "one wait, for the breaker")
	require.Len(t, seen, 1)
	require.NoError(t, seenErrs[0])
	require.NotEmpty(t, seen[0].Bins, "the flushed record holds the planned bins")
	assert.Equal(t, chunkOutcomePending, seen[0].Bins[0].Outcome, "the requeue flushed the bin back to pending before the wait")
	assert.Equal(t, 1, seen[0].Bins[0].Attempts, "the flushed record counts the fail-fast lease")
}
