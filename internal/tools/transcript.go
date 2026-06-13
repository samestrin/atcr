package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// transcriptNow is the clock for event timestamps; a package var so tests pin it.
var transcriptNow = time.Now

// transcriptLog is the best-effort logger for transcript I/O failures. It is a
// package var so the engine can route it to its structured logger and tests can
// capture it. Failures are logged and swallowed — transcript recording is
// observability, never part of the findings contract (AC 05-01 Error Scenarios).
var transcriptLog = func(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "atcr: warning: "+format+"\n", a...)
}

// ToolCallRecord is one tool call as recorded in a tool_calls transcript event.
// Arguments is the raw JSON the model supplied, recorded verbatim (omitted when
// empty so a no-argument call does not emit an invalid empty value).
type ToolCallRecord struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolResultRecord is one tool result as recorded in a tool_result event.
// Content is exactly what the model received (already truncated to the per-call
// cap); Truncated/OriginalBytes mark when the tool produced more than the cap
// delivered, so the operator sees the model's view and how much was withheld.
type ToolResultRecord struct {
	ToolCallID    string
	Name          string
	Content       string
	Truncated     bool
	OriginalBytes int
}

// transcript event wire shapes. One JSON object per line; the event field
// discriminates the kind, and turn+ts are common to all three.
type tcEvent struct {
	Event     string           `json:"event"`
	Turn      int              `json:"turn"`
	TS        string           `json:"ts"`
	ToolCalls []ToolCallRecord `json:"tool_calls"`
}

type trEvent struct {
	Event         string `json:"event"`
	Turn          int    `json:"turn"`
	TS            string `json:"ts"`
	ToolCallID    string `json:"tool_call_id"`
	Name          string `json:"name"`
	Content       string `json:"content"`
	Truncated     bool   `json:"truncated"`
	OriginalBytes int    `json:"original_bytes"`
}

type finalEvent struct {
	Event   string `json:"event"`
	Turn    int    `json:"turn"`
	TS      string `json:"ts"`
	Message string `json:"message"`
}

// Transcript is a best-effort, append-only JSONL writer for one tool-using
// agent's session under raw/<agent>/transcript.jsonl. It is never nil from
// OpenTranscript: a file that cannot be created yields a disabled writer whose
// Record* methods are silent no-ops, so the agent loop never branches on
// transcript availability. The buffered writer flushes at turn boundaries
// (RecordToolResults/RecordFinal), not per event, so a crashed run still leaves
// well-formed JSONL up to the last flushed turn.
type Transcript struct {
	bw       *bufio.Writer
	closer   io.Closer
	agent    string
	disabled bool
}

// OpenTranscript opens (or creates) the transcript at path in append mode. On
// failure it logs and returns a disabled no-op writer rather than an error, so
// the caller is never forced to handle transcript I/O in the hot loop (AC 05-01
// Error Scenario 2).
func OpenTranscript(path, agent string) *Transcript {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		transcriptLog("agent %s: open transcript %s: %v", agent, path, err)
		return &Transcript{disabled: true, agent: agent}
	}
	return newTranscript(f, f, agent)
}

// newTranscript wraps an arbitrary writer (file in production, a failing writer
// in tests) in a buffered transcript. closer may be nil when the sink owns its
// own lifecycle.
func newTranscript(w io.Writer, closer io.Closer, agent string) *Transcript {
	return &Transcript{bw: bufio.NewWriter(w), closer: closer, agent: agent}
}

// off reports whether this transcript discards writes (nil receiver or a
// disabled writer from a failed open).
func (t *Transcript) off() bool { return t == nil || t.disabled }

// RecordToolCalls appends one tool_calls event for the turn. It does not flush —
// the turn boundary is the following RecordToolResults (or RecordFinal).
func (t *Transcript) RecordToolCalls(turn int, calls []ToolCallRecord) {
	if t.off() {
		return
	}
	t.writeEvent(tcEvent{Event: "tool_calls", Turn: turn, TS: t.ts(), ToolCalls: calls}, "tool_calls", turn)
}

// RecordToolResults appends one tool_result event per result, then flushes
// (turn boundary) so the completed turn is durable for a concurrent reader.
func (t *Transcript) RecordToolResults(turn int, results []ToolResultRecord) {
	if t.off() {
		return
	}
	for _, r := range results {
		t.writeEvent(trEvent{
			Event: "tool_result", Turn: turn, TS: t.ts(),
			ToolCallID: r.ToolCallID, Name: r.Name, Content: r.Content,
			Truncated: r.Truncated, OriginalBytes: r.OriginalBytes,
		}, "tool_result", turn)
	}
	t.flush(turn)
}

// RecordFinal appends the final assistant message and flushes (turn boundary).
func (t *Transcript) RecordFinal(turn int, message string) {
	if t.off() {
		return
	}
	t.writeEvent(finalEvent{Event: "final", Turn: turn, TS: t.ts(), Message: message}, "final", turn)
	t.flush(turn)
}

// Close flushes any buffered events and closes the underlying file. A nil/
// disabled transcript closes cleanly.
func (t *Transcript) Close() error {
	if t.off() {
		return nil
	}
	if err := t.bw.Flush(); err != nil {
		transcriptLog("agent %s: flush transcript on close: %v", t.agent, err)
	}
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}

// writeEvent marshals one event and writes it as a single line. A marshal
// failure (e.g. invalid raw JSON in tool arguments) is logged and the event
// skipped; subsequent events still record (AC 05-01 Error Scenario 3). A write
// failure is logged and swallowed (AC 05-01 Error Scenario 1).
func (t *Transcript) writeEvent(v any, kind string, turn int) {
	data, err := json.Marshal(v)
	if err != nil {
		transcriptLog("agent %s: marshal %s event (turn %d): %v", t.agent, kind, turn, err)
		return
	}
	if _, err := t.bw.Write(append(data, '\n')); err != nil {
		transcriptLog("agent %s: write %s event (turn %d): %v", t.agent, kind, turn, err)
	}
}

// flush writes the buffer to disk at a turn boundary; a flush error is logged
// and swallowed.
func (t *Transcript) flush(turn int) {
	if err := t.bw.Flush(); err != nil {
		transcriptLog("agent %s: flush transcript (turn %d): %v", t.agent, turn, err)
	}
}

// ts renders the current event timestamp in RFC3339 (UTC).
func (t *Transcript) ts() string { return transcriptNow().UTC().Format(time.RFC3339) }
