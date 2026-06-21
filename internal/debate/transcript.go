package debate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// debateNow is the clock for transcript timestamps; a package var so tests pin it.
var debateNow = time.Now

// transcriptLog is the best-effort logger for transcript I/O failures. Transcript
// recording is observability, never part of the ruling contract — failures are
// logged and swallowed so a debate is never failed by a transcript write error.
var transcriptLog = func(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "atcr: warning: "+format+"\n", a...)
}

// TurnEvent is one debate turn: a seat's statement for the item under debate. The
// per-item transcript is the replayable record of the exchange (proposer →
// challenger → judge), one JSON object per line.
type TurnEvent struct {
	Event     string `json:"event"` // "turn"
	Role      string `json:"role"`  // proposer | challenger | judge
	Agent     string `json:"agent"`
	Model     string `json:"model"`
	Turn      int    `json:"turn"`
	TS        string `json:"ts"`
	Statement string `json:"statement"`
	Status    string `json:"status,omitempty"` // non-OK when the seat halted (timeout/budget/error)
}

// RulingEvent is the judge's parsed ruling, appended after the three turns so the
// transcript carries the settled outcome alongside the exchange that produced it.
type RulingEvent struct {
	Event           string `json:"event"` // "ruling"
	TS              string `json:"ts"`
	Outcome         string `json:"outcome"` // uphold | overturn | split | unresolved
	SettledSeverity string `json:"settled_severity,omitempty"`
	ClusterDecision string `json:"cluster_decision,omitempty"` // merge | separate (gray-zone only)
	Reasoning       string `json:"reasoning,omitempty"`
}

// Transcript is a best-effort, append-only JSONL writer for one debated item's
// exchange under debate/<id>/transcript.jsonl. It is never nil from
// OpenTranscript: a file that cannot be created yields a disabled writer whose
// Record* methods are silent no-ops, so the protocol never branches on transcript
// availability. Each Record flushes so a crashed run leaves well-formed JSONL up
// to the last completed event.
type Transcript struct {
	bw       *bufio.Writer
	closer   io.Closer
	disabled bool
}

// OpenTranscript opens (or creates) the transcript at path in append mode. On
// failure it logs and returns a disabled no-op writer rather than an error.
func OpenTranscript(path string) *Transcript {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		transcriptLog("debate: open transcript %s: %v", path, err)
		return &Transcript{disabled: true}
	}
	return &Transcript{bw: bufio.NewWriter(f), closer: f}
}

// ts returns the current RFC3339 timestamp for an event.
func (t *Transcript) ts() string { return debateNow().UTC().Format(time.RFC3339) }

// RecordTurn appends one turn event and flushes.
func (t *Transcript) RecordTurn(ev TurnEvent) {
	ev.Event = "turn"
	ev.TS = t.ts()
	t.write(ev)
}

// RecordRuling appends the judge's ruling event and flushes.
func (t *Transcript) RecordRuling(ev RulingEvent) {
	ev.Event = "ruling"
	ev.TS = t.ts()
	t.write(ev)
}

// write marshals one event as a single line and flushes. Marshal/write failures
// are logged and swallowed (observability, not the ruling contract).
func (t *Transcript) write(ev any) {
	if t == nil || t.disabled || t.bw == nil {
		return
	}
	line, err := json.Marshal(ev)
	if err != nil {
		transcriptLog("debate: marshal transcript event: %v", err)
		return
	}
	if _, err := t.bw.Write(append(line, '\n')); err != nil {
		transcriptLog("debate: write transcript event: %v", err)
		return
	}
	if err := t.bw.Flush(); err != nil {
		transcriptLog("debate: flush transcript: %v", err)
	}
}

// Close flushes any buffered events and closes the file. A nil or disabled
// transcript closes cleanly.
func (t *Transcript) Close() error {
	if t == nil || t.disabled {
		return nil
	}
	if t.bw != nil {
		if err := t.bw.Flush(); err != nil {
			transcriptLog("debate: flush transcript on close: %v", err)
		}
	}
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}
