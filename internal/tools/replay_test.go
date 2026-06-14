package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureReplayLog swaps the replay warning logger so a test can assert that
// malformed/skipped lines were reported (not silently dropped).
func captureReplayLog(t *testing.T) *[]string {
	t.Helper()
	var msgs []string
	prev := replayLog
	replayLog = func(format string, a ...any) {
		msgs = append(msgs, format)
	}
	t.Cleanup(func() { replayLog = prev })
	return &msgs
}

func writeTranscriptLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644))
	return path
}

// AC 05-02 Scenario 3: replay reconstructs the event sequence in order.
func TestReplay_ReconstructsSequence(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"event":"tool_calls","turn":1,"ts":"2026-06-13T12:00:00Z","tool_calls":[{"id":"c1","name":"read_file"}]}`,
		`{"event":"tool_result","turn":1,"ts":"2026-06-13T12:00:00Z","tool_call_id":"c1","name":"read_file","content":"x","truncated":false,"original_bytes":1}`,
		`{"event":"final","turn":1,"ts":"2026-06-13T12:00:00Z","message":"done"}`,
	)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	require.Len(t, res.Events, 3)
	assert.Equal(t, "tool_calls", res.Events[0].Event)
	assert.Equal(t, "tool_result", res.Events[1].Event)
	assert.Equal(t, "final", res.Events[2].Event)
	assert.Equal(t, 1, res.Events[2].Turn)
	assert.Equal(t, 3, res.TotalLines)
	assert.Equal(t, 0, res.Skipped)
}

// AC 05-02 Scenario 2 / Edge 1: a partial (crash-truncated) transcript replays
// the complete lines and ignores a trailing partial line.
func TestReplay_PartialTranscriptValid(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"event":"tool_calls","turn":1,"ts":"2026-06-13T12:00:00Z","tool_calls":[{"id":"c1","name":"read_file"}]}`,
		`{"event":"tool_result","turn":1,"ts":"2026-06-13T12:00:00Z","tool_call_id":"c1","name":"read_file","content":"x"}`,
		`{"event":"tool_calls","turn":2,"ts":"2026-06-13T12:00:00Z","tool_c`, // crash mid-line
	)
	msgs := captureReplayLog(t)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	assert.Len(t, res.Events, 2, "two complete events replayed")
	assert.Equal(t, 1, res.Skipped, "the partial line is skipped")
	assert.NotEmpty(t, *msgs)
}

// AC 05-02 Error Scenario 1: malformed JSON line is logged and skipped; valid
// lines around it still replay.
func TestReplay_MalformedLineSkipped(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"event":"tool_calls","turn":1,"ts":"t","tool_calls":[]}`,
		`{not json at all}`,
		`{"event":"final","turn":1,"ts":"t","message":"ok"}`,
	)
	msgs := captureReplayLog(t)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	assert.Len(t, res.Events, 2)
	assert.Equal(t, 1, res.Skipped)
	assert.NotEmpty(t, *msgs)
}

// AC 05-02 Error Scenario 2: a valid JSON line missing the required event field
// is logged and skipped.
func TestReplay_MissingEventFieldSkipped(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"turn":1,"ts":"t","message":"no event field"}`,
		`{"event":"final","turn":1,"ts":"t","message":"ok"}`,
	)
	msgs := captureReplayLog(t)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "final", res.Events[0].Event)
	assert.Equal(t, 1, res.Skipped)
	assert.NotEmpty(t, *msgs)
}

// AC 05-02 Error Scenario 3: an unknown (future) event type is logged and
// skipped; known types still replay.
func TestReplay_UnknownEventTypeSkipped(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"event":"future_thing","turn":1,"ts":"t"}`,
		`{"event":"tool_calls","turn":1,"ts":"t","tool_calls":[]}`,
	)
	msgs := captureReplayLog(t)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "tool_calls", res.Events[0].Event)
	assert.Equal(t, 1, res.Skipped)
	assert.NotEmpty(t, *msgs)
}

// AC 05-02 Edge Case 4: an empty (0-byte) transcript replays as an empty session.
func TestReplay_EmptyFile(t *testing.T) {
	path := writeTranscriptLines(t, "")
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	assert.Empty(t, res.Events)
}

// A nonexistent transcript replays as an empty session (no error): an agent that
// failed before the first event leaves no file (AC 05-02 Edge Case 4).
func TestReplay_AbsentFile(t *testing.T) {
	res, err := ReplayTranscript(filepath.Join(t.TempDir(), "absent.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, res.Events)
}

// A malformed turn field (non-integer) must log a warning rather than silently
// collapsing the event to turn 0, unlike every other field error.
func TestReplay_MalformedTurnFieldLogsWarning(t *testing.T) {
	msgs := captureReplayLog(t)
	path := writeTranscriptLines(t,
		`{"event":"tool_calls","turn":"not-an-int","ts":"t","tool_calls":[]}`,
	)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	// Event is still included (resilient), but a warning must be logged.
	require.Len(t, res.Events, 1, "event with malformed turn must not be skipped")
	assert.Equal(t, 0, res.Events[0].Turn, "turn defaults to 0 when unparseable")
	assert.NotEmpty(t, *msgs, "malformed turn field must produce a warning log")
}

// Blank lines between events are ignored without counting as skips.
func TestReplay_BlankLinesIgnored(t *testing.T) {
	path := writeTranscriptLines(t,
		`{"event":"final","turn":1,"ts":"t","message":"ok"}`,
		``,
		``,
	)
	res, err := ReplayTranscript(path)
	require.NoError(t, err)
	require.Len(t, res.Events, 1)
	assert.Equal(t, 0, res.Skipped)
}
