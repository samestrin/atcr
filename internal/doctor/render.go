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
	_, _ = fmt.Fprintln(tw, "AGENT\tPROVIDER\tMODEL\tSOURCE\tWINDOW\tSTATUS\tLATENCY\tHINT")
	for _, a := range rep.Agents {
		latency := "-"
		if a.LatencyMS > 0 {
			latency = fmt.Sprintf("%dms", a.LatencyMS)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.Agent, a.Provider, a.Model, a.Source, window(a), a.Status, latency, diagnostic(a))
	}
	return tw.Flush()
}

// window builds the WINDOW cell: the resolved context window and the tier it
// came from, e.g. "262144 (declaration)".
//
// The tier is rendered alongside the number rather than instead of it because
// neither answers the operator's question alone. The number cannot show whether
// a declaration took effect — a declaration that was ignored and a static-table
// hit that happens to match look identical. The tier cannot show whether the
// value is plausible for the model. Seeing "512000 (declaration)" next to a
// model the table pins at 128000 is what makes an over-declaration diagnosable
// here instead of as an opaque provider HTTP 400 mid-run.
// The probed output cap rides in this same cell rather than in a column of its own.
// The two numbers are one story — the cap is reserved out of the window when the payload
// is sized — and the ok_warning hint tells the operator to change the cap, so a table
// that shows the window but not the cap names a knob it refuses to display. Rendered with
// its tier for the same reason the window is: an agent declaring exactly doctor's default
// is otherwise indistinguishable from one declaring nothing.
//
// Omitted when no call was placed (cap 0), so a short-circuited probe does not report a
// cap it never applied.
func window(a AgentResult) string {
	if a.ContextWindowTokens <= 0 {
		return "-"
	}
	w := fmt.Sprintf("%d", a.ContextWindowTokens)
	if a.WindowSource != "" {
		w = fmt.Sprintf("%d (%s)", a.ContextWindowTokens, a.WindowSource)
	}
	// A non-positive MaxTokens means the probe applied NO cap, so there is no probed
	// cap to render — but that must not swallow review's cap too. It used to: the
	// early return here preceded the review-cap suffix below, so a row with an
	// uncapped probe and a FIRING zero-budget verdict showed neither cap while its
	// hint quoted review's, and docs/registry.md states this cell carries the hint's
	// operand. The probed cap stays absent; review's is appended whenever the row is
	// healthy enough for a hint to speak for it (see the suffix gate below).
	//
	// No tier-less form for the probed cap: probe() sets the cap and its source
	// together, so a cap without a source cannot occur. A fallback for it would
	// render exactly the declaration-vs-default ambiguity MaxTokensSource exists to
	// remove.
	cell := w
	if a.MaxTokens > 0 {
		cell = fmt.Sprintf("%s / cap %d (%s)", w, a.MaxTokens, a.MaxTokensSource)
	}
	// Review's cap rides along when it differs from the probed one: the ok_warning
	// hint quotes REVIEW's cap (doctor's --max-tokens caps the probe only), so a
	// cell showing just the probe's cap names a knob whose current value it refuses
	// to display — the defect the probed-cap render fixed, one tier over.
	//
	// But only where SOME cap was in play. On a row that placed no call at all
	// (missing_key/invalid_config short-circuits before the budget resolves, leaving
	// MaxTokens 0 while run.go assigns ReviewMaxTokens unconditionally) the suffix
	// would be the cell's ONLY cap, and a reader who knows the documented
	// `<window> / cap N (tier)` shape reads that two-part form as the cap the probe
	// used. healthy() keeps the case the early return used to swallow: a FIRING
	// zero-budget verdict is ok_warning, its hint quotes review's cap, and that cap
	// must still be shown.
	if (a.MaxTokens > 0 || healthy(a.Status)) && a.ReviewMaxTokens > 0 && a.ReviewMaxTokens != a.MaxTokens {
		cell = fmt.Sprintf("%s / review cap %d", cell, a.ReviewMaxTokens)
	}
	return cell
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
