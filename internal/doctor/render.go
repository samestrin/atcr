package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// RenderJSON writes the report as stable, indented JSON. The schema is the
// AgentResult struct tags; see docs/registry.md for the documented contract.
func RenderJSON(w io.Writer, rep *Report) error {
	// Marshal a wrapper so the top-level shape is a stable object, never null.
	out := struct {
		Agents []AgentResult `json:"agents"`
	}{Agents: rep.Agents}
	if out.Agents == nil {
		out.Agents = []AgentResult{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// RenderTableError writes an aligned human-readable table and returns any
// flush error. Callers that need to detect truncated output should prefer this
// over RenderTable.
func RenderTableError(w io.Writer, rep *Report) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "AGENT\tPROVIDER\tMODEL\tSOURCE\tSTATUS\tLATENCY\tHINT")
	for _, a := range rep.Agents {
		latency := "-"
		if a.LatencyMS > 0 {
			latency = fmt.Sprintf("%dms", a.LatencyMS)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.Agent, a.Provider, a.Model, a.Source, a.Status, latency, diagnostic(a))
	}
	return tw.Flush()
}

// maxTableDetailBytes bounds the upstream detail rendered in the table. The
// probe already clamps detail to 512 bytes; this is the tighter budget for a
// single terminal row, with --json carrying the untruncated text.
const maxTableDetailBytes = 160

// diagnostic builds the HINT cell. The hint is atcr's guess at the cause; the
// detail is the upstream's own account of it. A failing row carries both — the
// guess alone is what let a 403 quota cap read as "check the API key" while the
// captured body said the billing cycle was exhausted. Healthy rows render no
// detail, so the common case stays quiet.
func diagnostic(a AgentResult) string {
	if healthy(a.Status) || a.Detail == "" {
		return a.Hint
	}
	detail := clampRunes(a.Detail, maxTableDetailBytes)
	if len(detail) < len(a.Detail) {
		detail += "… (--json for full text)"
	}
	if a.Hint == "" {
		return "error: " + detail
	}
	return a.Hint + " | detail: " + detail
}

// RenderTable writes an aligned human-readable table: one row per
// effective-roster agent.
func RenderTable(w io.Writer, rep *Report) {
	_ = RenderTableError(w, rep)
}
