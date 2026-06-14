package tools

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedClock pins the transcript timestamp so the ts field is assertable.
func fixedClock(t *testing.T) {
	t.Helper()
	prev := transcriptNow
	transcriptNow = func() time.Time {
		return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { transcriptNow = prev })
}

// captureTranscriptLog swaps the best-effort logger and returns a pointer to the
// accumulated messages so a test can assert an error was logged (not swallowed).
func captureTranscriptLog(t *testing.T) *[]string {
	t.Helper()
	var msgs []string
	prev := transcriptLog
	transcriptLog = func(format string, a ...any) {
		msgs = append(msgs, strings.TrimSpace(fmt.Sprintf(format, a...)))
	}
	t.Cleanup(func() { transcriptLog = prev })
	return &msgs
}

// readLines reads a transcript file into JSON-decoded maps, one per line.
func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []map[string]any
	for _, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(ln), &m), "line not valid JSON: %s", ln)
		out = append(out, m)
	}
	return out
}

// failingWriter returns an error after n successful writes, to exercise the
// best-effort write-failure path.
type failingWriter struct {
	ok  int
	err error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.ok > 0 {
		w.ok--
		return len(p), nil
	}
	return 0, w.err
}

// AC 05-01 Scenario 1: tool_calls event schema.
func TestTranscript_RecordToolCallsEvent(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolCalls(1, []ToolCallRecord{
		{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
		{ID: "call_2", Name: "grep", Arguments: json.RawMessage(`{"pattern":"foo"}`)},
	})
	require.NoError(t, tr.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 1)
	ev := lines[0]
	assert.Equal(t, "tool_calls", ev["event"])
	assert.EqualValues(t, 1, ev["turn"])
	assert.Equal(t, "2026-06-13T12:00:00Z", ev["ts"])
	calls, ok := ev["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, calls, 2)
	c0 := calls[0].(map[string]any)
	assert.Equal(t, "call_1", c0["id"])
	assert.Equal(t, "read_file", c0["name"])
}

// AC 05-01 Scenario 2: tool_result within cap → truncated:false, full content.
func TestTranscript_RecordToolResultWithinCap(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolResults(1, []ToolResultRecord{
		{ToolCallID: "call_1", Name: "read_file", Content: "the result", Truncated: false, OriginalBytes: 10},
	})
	require.NoError(t, tr.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 1)
	ev := lines[0]
	assert.Equal(t, "tool_result", ev["event"])
	assert.Equal(t, "call_1", ev["tool_call_id"])
	assert.Equal(t, "read_file", ev["name"])
	assert.Equal(t, "the result", ev["content"])
	assert.Equal(t, false, ev["truncated"])
	assert.EqualValues(t, 10, ev["original_bytes"])
}

// AC 05-01 Scenario 3: truncated tool_result → truncated:true, original_bytes.
func TestTranscript_RecordTruncatedToolResult(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolResults(2, []ToolResultRecord{
		{ToolCallID: "call_9", Name: "grep", Content: "TRUNC", Truncated: true, OriginalBytes: 50000},
	})
	require.NoError(t, tr.Close())

	ev := readLines(t, path)[0]
	assert.Equal(t, true, ev["truncated"])
	assert.EqualValues(t, 50000, ev["original_bytes"])
	assert.Equal(t, "TRUNC", ev["content"])
}

// AC 05-01 Edge Case 3: empty tool result → content "", original_bytes 0.
func TestTranscript_EmptyToolResult(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolResults(1, []ToolResultRecord{
		{ToolCallID: "call_1", Name: "read_file", Content: "", Truncated: false, OriginalBytes: 0},
	})
	require.NoError(t, tr.Close())

	ev := readLines(t, path)[0]
	assert.Equal(t, "", ev["content"])
	assert.Equal(t, false, ev["truncated"])
	assert.EqualValues(t, 0, ev["original_bytes"])
}

// AC 05-01 Scenario 4: final event is the last line.
func TestTranscript_RecordFinalIsLastLine(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolCalls(1, []ToolCallRecord{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}})
	tr.RecordToolResults(1, []ToolResultRecord{{ToolCallID: "c1", Name: "read_file", Content: "x", OriginalBytes: 1}})
	tr.RecordFinal(4, "the final review")
	require.NoError(t, tr.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 3)
	last := lines[2]
	assert.Equal(t, "final", last["event"])
	assert.EqualValues(t, 4, last["turn"])
	assert.Equal(t, "the final review", last["message"])
}

// AC 05-01 Edge Case 2 / Scenario 5: multi-call turn → one tool_calls event,
// one tool_result per call, in order.
func TestTranscript_MultipleResultsPerTurn(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordToolResults(1, []ToolResultRecord{
		{ToolCallID: "c1", Name: "read_file", Content: "a", OriginalBytes: 1},
		{ToolCallID: "c2", Name: "grep", Content: "b", OriginalBytes: 1},
		{ToolCallID: "c3", Name: "list_files", Content: "c", OriginalBytes: 1},
	})
	require.NoError(t, tr.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 3)
	assert.Equal(t, "c1", lines[0]["tool_call_id"])
	assert.Equal(t, "c2", lines[1]["tool_call_id"])
	assert.Equal(t, "c3", lines[2]["tool_call_id"])
}

// AC 05-02 Scenario 1: append-only across batches; prior lines preserved.
func TestTranscript_AppendOnlyAcrossOpens(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")

	tr1 := OpenTranscript(path, "agent-a")
	tr1.RecordToolCalls(1, []ToolCallRecord{{ID: "c1", Name: "read_file"}})
	tr1.RecordToolResults(1, []ToolResultRecord{{ToolCallID: "c1", Name: "read_file", Content: "x", OriginalBytes: 1}})
	require.NoError(t, tr1.Close())

	// Re-open the same path (O_APPEND) and add turn 2.
	tr2 := OpenTranscript(path, "agent-a")
	tr2.RecordToolCalls(2, []ToolCallRecord{{ID: "c2", Name: "grep"}})
	tr2.RecordFinal(2, "done")
	require.NoError(t, tr2.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 4)
	assert.Equal(t, "tool_calls", lines[0]["event"])
	assert.Equal(t, "final", lines[3]["event"])
}

// AC 05-02 DoD: file opened O_CREATE|O_WRONLY|O_APPEND lands at mode 0644.
func TestTranscript_FileModeAndAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")
	tr.RecordFinal(1, "x")
	require.NoError(t, tr.Close())

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

// AC 05-01 Error Scenario 2: file cannot be created → no-op writer, no panic.
func TestTranscript_OpenFailureIsNoOp(t *testing.T) {
	msgs := captureTranscriptLog(t)
	// A path whose parent does not exist cannot be created.
	bad := filepath.Join(t.TempDir(), "nope", "transcript.jsonl")
	tr := OpenTranscript(bad, "agent-a")
	require.NotNil(t, tr, "OpenTranscript must never return nil")
	// Record* on a disabled writer is a silent no-op (does not panic).
	tr.RecordToolCalls(1, []ToolCallRecord{{ID: "c1", Name: "read_file"}})
	tr.RecordFinal(1, "x")
	require.NoError(t, tr.Close())
	assert.NotEmpty(t, *msgs, "open failure should be logged")
}

// AC 05-01 Error Scenario 1: write failure is logged and not fatal.
func TestTranscript_WriteFailureLoggedNotFatal(t *testing.T) {
	fixedClock(t)
	msgs := captureTranscriptLog(t)
	fw := &failingWriter{ok: 0, err: errors.New("disk full")}
	tr := newTranscript(fw, nil, "agent-a")

	// RecordToolResults flushes at the turn boundary, surfacing the write error.
	tr.RecordToolResults(1, []ToolResultRecord{{ToolCallID: "c1", Name: "read_file", Content: "x", OriginalBytes: 1}})
	_ = tr.Close()
	assert.NotEmpty(t, *msgs, "write failure should be logged")
}

// AC 05-01 Error Scenario 3: a marshal failure (invalid raw JSON in Arguments)
// is logged and skipped; subsequent events still record.
func TestTranscript_MarshalFailureSkipsEventContinues(t *testing.T) {
	fixedClock(t)
	msgs := captureTranscriptLog(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	tr := OpenTranscript(path, "agent-a")

	// Invalid JSON bytes in Arguments make json.Marshal of the event fail.
	tr.RecordToolCalls(1, []ToolCallRecord{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{bad`)}})
	// A subsequent well-formed event must still be written.
	tr.RecordFinal(1, "recovered")
	require.NoError(t, tr.Close())

	lines := readLines(t, path)
	require.Len(t, lines, 1, "the malformed tool_calls event is skipped")
	assert.Equal(t, "final", lines[0]["event"])
	assert.NotEmpty(t, *msgs, "marshal failure should be logged")
}

// AC 05-02 Performance: tool_calls does not flush mid-turn; RecordToolResults
// (turn boundary) flushes. A concurrent reader sees the turn's lines only after
// the boundary flush.
func TestTranscript_FlushesAtTurnBoundary(t *testing.T) {
	fixedClock(t)
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	tr := newTranscript(bufio.NewWriter(f), f, "agent-a")

	tr.RecordToolCalls(1, []ToolCallRecord{{ID: "c1", Name: "read_file"}})
	// Mid-turn: the buffered tool_calls line is not yet on disk.
	mid := readLines(t, path)
	assert.Empty(t, mid, "tool_calls should not flush mid-turn")

	tr.RecordToolResults(1, []ToolResultRecord{{ToolCallID: "c1", Name: "read_file", Content: "x", OriginalBytes: 1}})
	after := readLines(t, path)
	assert.Len(t, after, 2, "turn-boundary flush makes both lines durable")
	require.NoError(t, tr.Close())
}

// A nil *Transcript is a safe no-op on every method (defensive: the loop may hold
// a nil recorder when transcript recording is disabled).
func TestTranscript_NilSafe(t *testing.T) {
	var tr *Transcript
	assert.NotPanics(t, func() {
		tr.RecordToolCalls(1, nil)
		tr.RecordToolResults(1, nil)
		tr.RecordFinal(1, "x")
		_ = tr.Close()
	})
}
