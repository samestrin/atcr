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
		hint := a.Hint
		if hint == "" && a.Detail != "" {
			hint = "error: " + a.Detail
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.Agent, a.Provider, a.Model, a.Source, a.Status, latency, hint)
	}
	return tw.Flush()
}

// RenderTable writes an aligned human-readable table: one row per
// effective-roster agent.
func RenderTable(w io.Writer, rep *Report) {
	_ = RenderTableError(w, rep)
}
