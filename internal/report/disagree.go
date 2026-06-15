package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/samestrin/atcr/internal/reconcile"
)

// RenderDisagreements writes the focused `atcr report --disagreements` view: a
// ranked list of the highest-tension spots in a change, each with the model
// positions side by side. Free text is HTML-escaped and newline-flattened and
// file paths render in backtick code spans, the same injection defenses the main
// report applies.
func RenderDisagreements(w io.Writer, df reconcile.DisagreementsFile) error {
	var b bytes.Buffer
	b.WriteString("# atcr Disagreement Radar\n\n")
	if len(df.Items) == 0 {
		b.WriteString("No disagreements detected.\n")
		_, err := w.Write(b.Bytes())
		return err
	}
	fmt.Fprintf(&b, "%d tension spot(s), highest first.\n", len(df.Items))
	writeRadarItems(&b, df.Items, "## ")
	_, err := w.Write(b.Bytes())
	return err
}

// RenderDisagreementsJSON writes the disagreements radar as indented JSON. The
// DisagreementsFile already carries the schema version and machine-contract
// field names (see TestDisagreementsSchema_StableContract); this is the format
// `atcr report --disagreements --format json` emits.
func RenderDisagreementsJSON(w io.Writer, df reconcile.DisagreementsFile) error {
	data, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// writeRadarSection appends the collapsible "Disagreements" section used inside
// the standard report.md, above the consensus findings. It writes nothing when
// there are no items, so a review with no disagreements produces byte-identical
// report output.
func writeRadarSection(b *bytes.Buffer, df reconcile.DisagreementsFile) {
	if len(df.Items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
	writeRadarItems(b, df.Items, "### ")
}

// writeRadarItems renders each ranked item under the given heading prefix.
func writeRadarItems(b *bytes.Buffer, items []reconcile.DisagreementItem, heading string) {
	for i, it := range items {
		fmt.Fprintf(b, "\n%s%d. %s — %s (%s) · score %s\n",
			heading, i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
		if it.Disagreement != "" {
			fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
		}
		if it.Skeptics != "" {
			fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
		}
		if len(it.Reviewers) > 0 {
			fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinReviewers(it.Reviewers)), it.Independence)
		}
		// escTrunc (500-rune cap) is intentional for display output; the reconcile
		// counterpart (reconcile/disagree.go writeRadarSection) uses esc (no cap) for
		// archival fidelity.
		if it.Problem != "" {
			fmt.Fprintf(b, "- Problem: %s\n", escTrunc(it.Problem))
		}
		if it.Detail != "" {
			fmt.Fprintf(b, "- Detail: %s\n", escTrunc(it.Detail))
		}
		if len(it.Positions) > 0 {
			b.WriteString("- Positions:\n")
			for _, p := range it.Positions {
				fmt.Fprintf(b, "  - %s — %s: %s\n", esc(reviewerOrUnknown(p.Reviewer)), esc(p.Severity), escTrunc(p.Problem))
			}
		}
	}
}

// formatScore renders the ranking score compactly: an integer-valued score drops
// the decimal (6, not 6.0); a fractional score keeps two places.
func formatScore(s float64) string {
	if s == float64(int64(s)) {
		return strconv.FormatInt(int64(s), 10)
	}
	return strconv.FormatFloat(s, 'f', 2, 64)
}

func reviewerOrUnknown(name string) string {
	if name == "" {
		return "(unknown)"
	}
	return name
}
