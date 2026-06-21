package report

import (
	"bytes"
	"fmt"
	"io"

	"github.com/samestrin/atcr/internal/reconcile"
)

// Contested is the report view of one debated finding's ruling (Epic 6.0). It is
// a lightweight, presentation-only projection of the debate stage's
// reconciled/debate.json item — the report package stays decoupled from the
// debate package; the command layer maps the artifact onto this type.
type Contested struct {
	File              string
	Line              int
	Outcome           string // uphold | overturn | split | unresolved
	OriginalSeverity  string
	SettledSeverity   string
	Judge             string
	Reasoning         string
	Reason            string // unresolved reason (e.g. insufficient_distinct_models)
	ChallengeSurvived bool
	SingleModel       bool
	ClusterDecision   string // merge | separate (gray-zone only)
}

// ContestedReport is the full contested-findings view: the per-item rulings plus
// the count of disputed items that exceeded the debate cap and were not debated
// (disclosed, never silent).
type ContestedReport struct {
	Items    []Contested
	Overflow int
}

// HasContent reports whether the contested view has anything to render.
func (c ContestedReport) HasContent() bool { return len(c.Items) > 0 || c.Overflow > 0 }

// RenderMarkdownWithContested writes the standard markdown report with both the
// disagreement radar and the contested-findings section (Epic 6.0). When the
// contested report is empty the output is byte-identical to
// RenderMarkdownWithDisagreements, so a review with no debate is unchanged.
func RenderMarkdownWithContested(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile, cr ContestedReport) error {
	return renderMarkdownFull(w, findings, df, cr)
}

// writeContestedSection appends the "Contested findings" section: one entry per
// ruling with a one-line rationale, plus the overflow disclosure. It writes
// nothing when the report is empty, so a no-debate review yields byte-identical
// output. All free text is escaped/flattened/truncated (the same injection
// defenses the rest of the report uses); file paths render verbatim in code spans.
func writeContestedSection(b *bytes.Buffer, cr ContestedReport) {
	if !cr.HasContent() {
		return
	}
	fmt.Fprintf(b, "\n## Contested findings\n\nDebated %d finding(s) through cross-examination (proposer/challenger/judge).\n", len(cr.Items))
	for i, c := range cr.Items {
		fmt.Fprintf(b, "\n### %d. %s — %s%s%s\n", i+1, esc(c.Outcome), codeSpan(c.File, c.Line), severityTransition(c), challengeBadge(c))
		switch c.Outcome {
		case "uphold":
			b.WriteString("- Upheld: survived hostile challenge.\n")
		case "split":
			b.WriteString("- Split: real finding, severity settled by the judge.\n")
		case "overturn":
			b.WriteString("- Overturned: refuted, retained but excluded from the gate.\n")
		default:
			if c.Reason != "" {
				fmt.Fprintf(b, "- Unresolved: %s.\n", esc(c.Reason))
			} else {
				b.WriteString("- Unresolved.\n")
			}
		}
		if c.Judge != "" {
			fmt.Fprintf(b, "- Judge: %s%s\n", esc(c.Judge), singleModelNote(c.SingleModel))
		}
		if c.ClusterDecision != "" {
			fmt.Fprintf(b, "- Cluster decision: %s\n", esc(c.ClusterDecision))
		}
		if c.Reasoning != "" {
			fmt.Fprintf(b, "- Rationale: %s\n", escTrunc(c.Reasoning))
		}
	}
	if cr.Overflow > 0 {
		fmt.Fprintf(b, "\n_%d disputed item(s) exceeded the debate cap and were not debated (recorded in debate.json)._\n", cr.Overflow)
	}
}

// severityTransition renders the severity change a split produced, e.g.
// " (HIGH → MEDIUM)", or " (HIGH)" when unchanged/absent.
func severityTransition(c Contested) string {
	if c.Outcome == "split" && c.SettledSeverity != "" && c.SettledSeverity != c.OriginalSeverity {
		return fmt.Sprintf(" (%s → %s)", esc(c.OriginalSeverity), esc(c.SettledSeverity))
	}
	if c.OriginalSeverity != "" {
		return fmt.Sprintf(" (%s)", esc(c.OriginalSeverity))
	}
	return ""
}

// challengeBadge renders the structured ChallengeSurvived marker the `atcr debate`
// help promises. It marks uphold and split entries whose finding survived the
// cross-examination (ChallengeSurvived is set for both, cleared for an overturn), so
// a split survivor is distinguished from a bare split rather than the field being
// dead plumbing the renderer never reads.
func challengeBadge(c Contested) string {
	if c.ChallengeSurvived && (c.Outcome == "uphold" || c.Outcome == "split") {
		return " _(challenge-survived)_"
	}
	return ""
}

// singleModelNote flags a ruling produced under the same-model persona fallback,
// where the independence guarantee is weaker.
func singleModelNote(single bool) string {
	if single {
		return " _(single-model fallback)_"
	}
	return ""
}
