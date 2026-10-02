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
	// UnresolvedAttempts is how many runs reached this item and left it unresolved,
	// carried forward across runs. Surfaced so an operator sees the countdown
	// ("attempt 2 of 3") BEFORE the run that withholds the item, rather than only
	// learning of the ceiling after its ruling has already vanished.
	UnresolvedAttempts int
}

// Withheld is one disputed item that was NOT debated this run because a prior run
// already left it unresolved at the ceiling. Unlike a Contested ruling it has no
// outcome, so without its own listing the item would go from fully-described (a
// ruling with its reason and per-seat rationale) to a single incremented integer
// (TD cli/report.go:284).
type Withheld struct {
	File               string
	Line               int
	Severity           string
	Problem            string
	Reason             string
	UnresolvedAttempts int
}

// ContestedReport is the full contested-findings view: the per-item rulings plus
// the counts of disputed items that were not debated at all (disclosed, never
// silent). The two causes are counted SEPARATELY because they have opposite
// remedies and opposite permanence — see the fields.
type ContestedReport struct {
	Items []Contested
	// Overflow is the cap-overflow count: items that matched a trigger and exceeded
	// debate.max_items. Raising the cap debates them, so its remedy is a flag change.
	Overflow int
	// Withheld is the attempts-exhausted count: items a prior run left unresolved
	// maxUnresolvedAttempts times. Its remedy is the opposite — withholdExhausted
	// runs BEFORE SelectItems, so NO cap value recovers them. Kept apart from Overflow
	// because a single conflated integer told an operator that raising max_items would
	// debate all seven of them, which is false for every withheld one (TD
	// internal/report/contested.go:98).
	Withheld int
	// WithheldItems lists the withheld items themselves. The count discloses how
	// many; the list preserves WHICH, so an item that disappears from the debated
	// set is still identified rather than reduced to a number (TD cli/report.go:284).
	WithheldItems []Withheld
	// UnresolvedAttemptsCeiling is the attempt ceiling an item must reach to be
	// withheld, so a countdown can render as "attempt N of ceiling" rather than a
	// bare count. Zero means unknown (a caller that did not supply it), in which
	// case the countdown falls back to a bare count.
	UnresolvedAttemptsCeiling int
}

// HasContent reports whether the contested view has anything to render.
func (c ContestedReport) HasContent() bool {
	return len(c.Items) > 0 || c.Overflow > 0 || c.Withheld > 0 || len(c.WithheldItems) > 0
}

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
	// Model independence is the epic's selling point, so a degraded run must be
	// disclosed at the surface a reader scans first: an aggregate count of rulings
	// made under the same-model persona fallback, in addition to the per-item note.
	if single := singleModelCount(cr.Items); single > 0 {
		fmt.Fprintf(b, "\n_%d ruling(s) used the same-model persona fallback — model independence was weaker for these._\n", single)
	}
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
				fmt.Fprintf(b, "- Unresolved: %s.\n", escTrunc(c.Reason))
			} else {
				b.WriteString("- Unresolved.\n")
			}
			// An item left unresolved because distinct models were unavailable has no
			// Judge line to carry singleModelNote, so surface the weakened-independence
			// condition here rather than leaving it invisible.
			if c.Reason == "insufficient_distinct_models" {
				b.WriteString("- Independence: distinct models were unavailable; no ruling under the independence guarantee.\n")
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
		if c.UnresolvedAttempts > 0 {
			fmt.Fprintf(b, "- %s\n", attemptCountdown(c.UnresolvedAttempts, cr.UnresolvedAttemptsCeiling))
		}
	}
	// Rendered SEPARATELY, each with its own remedy, because the two have opposite
	// permanence: a cap overflow is recovered by raising debate.max_items, while a
	// withheld item is not recovered at ANY cap value.
	if cr.Overflow > 0 {
		fmt.Fprintf(b, "\n_%d disputed item(s) were not debated: the debate cap was reached. Raise debate.max_items to debate them (each recorded with its reason in debate.json)._\n", cr.Overflow)
	}
	if cr.Withheld > 0 {
		fmt.Fprintf(b, "\n_%d disputed item(s) were withheld: they reached the unresolved-attempt ceiling in prior runs. Raising the cap will NOT recover them — withholdExhausted runs before selection (each recorded with its reason in debate.json)._\n", cr.Withheld)
	}
	// The withheld items are LISTED below their count. A count alone is how an item
	// goes from fully-described to invisible in one step: it produces no Contested
	// ruling on the run that withholds it, so its diagnosis (the unresolved reason
	// and per-seat rationale it carried for three runs) vanishes and is replaced by
	// +1 on an integer. The listing keeps the item identified across that step
	// (TD cli/report.go:284).
	if len(cr.WithheldItems) > 0 {
		b.WriteString("\n### Withheld items\n")
		for i, w := range cr.WithheldItems {
			fmt.Fprintf(b, "\n%d. %s%s\n", i+1, codeSpan(w.File, w.Line), withheldSeverity(w.Severity))
			if w.Problem != "" {
				fmt.Fprintf(b, "- Problem: %s\n", escTrunc(w.Problem))
			}
			if w.Reason != "" {
				fmt.Fprintf(b, "- Withheld: %s.\n", escTrunc(w.Reason))
			}
			if w.UnresolvedAttempts > 0 {
				fmt.Fprintf(b, "- %s\n", attemptCountdown(w.UnresolvedAttempts, cr.UnresolvedAttemptsCeiling))
			}
		}
	}
}

// attemptCountdown renders an item's carried-forward unresolved attempt count, as
// "attempt N of M" when the ceiling is known and a bare "attempt N" otherwise — so
// an operator sees how close an item is to being withheld before the run that
// finally withholds it. It is deliberately not capped at the ceiling: a count
// above it (a legacy record) still renders honestly.
func attemptCountdown(attempts, ceiling int) string {
	if ceiling > 0 {
		return fmt.Sprintf("Unresolved attempts: attempt %d of %d.", attempts, ceiling)
	}
	return fmt.Sprintf("Unresolved attempts: attempt %d.", attempts)
}

// withheldSeverity renders the withheld item's severity in the same " (SEV)" shape
// a ruling uses, so the two lists read alike.
func withheldSeverity(sev string) string {
	if sev == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", esc(sev))
}

// severityTransition renders the severity change a split produced, e.g.
// " (HIGH → MEDIUM)"; " (HIGH, excluded)" for an overturned (refuted, non-gating)
// finding; or " (HIGH)" for any other live, gating severity.
func severityTransition(c Contested) string {
	if c.Outcome == "split" && c.SettledSeverity != "" && c.SettledSeverity != c.OriginalSeverity {
		return fmt.Sprintf(" (%s → %s)", esc(c.OriginalSeverity), esc(c.SettledSeverity))
	}
	if c.OriginalSeverity == "" {
		return ""
	}
	if c.Outcome == "overturn" {
		// An overturned finding is refuted and excluded from the gate (IsFailing is
		// false). Annotate the tag so it is not read as a live, gating severity
		// identical to an upheld finding's.
		return fmt.Sprintf(" (%s, excluded)", esc(c.OriginalSeverity))
	}
	return fmt.Sprintf(" (%s)", esc(c.OriginalSeverity))
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

// singleModelCount reports how many rulings were produced under the same-model
// persona fallback — the input to the section-level independence disclosure.
func singleModelCount(items []Contested) int {
	n := 0
	for _, c := range items {
		if c.SingleModel {
			n++
		}
	}
	return n
}

// singleModelNote flags a ruling produced under the same-model persona fallback,
// where the independence guarantee is weaker.
func singleModelNote(single bool) string {
	if single {
		return " _(single-model fallback)_"
	}
	return ""
}
