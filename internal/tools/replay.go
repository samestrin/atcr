package tools

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// replayLog is the best-effort warning sink for malformed/unknown transcript
// lines. A package var so tests can capture it; defaults to stderr.
var replayLog = func(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "atcr: warning: "+format+"\n", a...)
}

// knownReplayEvents is the set of event kinds a transcript may contain. A line
// carrying any other kind is from a newer engine version and is skipped, not
// fatal (AC 05-02 Error Scenario 3).
var knownReplayEvents = map[string]bool{"tool_calls": true, "tool_result": true, "final": true}

// maxReplayLine bounds a single transcript line. A tool_result content is capped
// at the per-call byte cap (~64 KiB) plus envelope; 1 MiB is a generous ceiling
// that keeps a corrupt giant line from allocating without bound.
const maxReplayLine = 1 << 20

// ReplayEvent is one parsed transcript event. Raw carries the whole line so a
// caller can read event-specific fields (tool_calls, content, message) without
// the replay harness committing to every schema.
type ReplayEvent struct {
	Event string
	Turn  int
	Raw   map[string]json.RawMessage
}

// ReplayResult summarizes a replay pass: the events in file order, how many
// non-blank lines were seen, and how many were skipped (malformed, missing the
// event field, or an unknown event kind).
type ReplayResult struct {
	Events     []ReplayEvent
	TotalLines int
	Skipped    int
}

// ReplayTranscript reads transcript.jsonl and returns events in source order as
// a flat []ReplayEvent with each event's raw JSON map. It is a resilient event
// reader, NOT a Chat-sequence reconstructor: it does not pair tool_calls with
// tool_results, rebuild message lists, or reconstruct the original Chat arguments.
// Malformed lines, missing event fields, and unknown event kinds are logged and
// skipped so a partial (crash-truncated) or forward-version transcript never
// errors the harness (AC 05-02 Error Scenarios 1–3).
func ReplayTranscript(path string) (ReplayResult, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReplayResult{}, nil // an agent that failed before the first event leaves no file
		}
		return ReplayResult{}, err
	}
	defer func() { _ = f.Close() }()

	var res ReplayResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxReplayLine)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(trimSpaceBytes(raw)) == 0 {
			continue // blank line: ignored, not counted
		}
		res.TotalLines++

		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			replayLog("transcript %s line %d: malformed JSON, skipped: %v", path, line, err)
			res.Skipped++
			continue
		}
		evtRaw, ok := obj["event"]
		if !ok {
			replayLog("transcript %s line %d: missing required field \"event\", skipped", path, line)
			res.Skipped++
			continue
		}
		var evt string
		if err := json.Unmarshal(evtRaw, &evt); err != nil || !knownReplayEvents[evt] {
			replayLog("transcript %s line %d: unknown event %q, skipped", path, line, evt)
			res.Skipped++
			continue
		}
		turn := 0
		if tr, ok := obj["turn"]; ok {
			if err := json.Unmarshal(tr, &turn); err != nil {
				replayLog("transcript %s line %d: malformed turn field, defaulting to 0: %v", path, line, err)
			}
		}
		res.Events = append(res.Events, ReplayEvent{Event: evt, Turn: turn, Raw: obj})
	}
	if err := sc.Err(); err != nil {
		// A scan error (e.g. a line over the cap) ends the read but the events
		// gathered so far are returned — best-effort, like a partial transcript.
		replayLog("transcript %s: read ended early: %v", path, err)
	}
	return res, nil
}

// trimSpaceBytes reports the input with leading/trailing ASCII whitespace
// removed, used only to detect blank lines without allocating a string.
func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpace(b[start]) {
		start++
	}
	for end > start && isSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
