package verify

import (
	reclib "github.com/samestrin/atcr/reconcile"
	"strings"
)

// aggregateVerdicts collapses the per-skeptic verdicts for one finding into a
// single finding-level verdict using a strict-majority rule:
//
//   - A single valid verdict passes through unchanged.
//   - When exactly one verdict has the strict maximum vote count, it wins
//     (unanimous and clear-majority cases). Its Notes carry the reasonings of the
//     skeptics who voted for it; the aggregate Skeptic names every voter.
//   - Any tie for the maximum — including a head-to-head disagreement or a
//     three-way split — yields "unverifiable", preserving EVERY skeptic's
//     reasoning so a human can see the disagreement.
//   - An empty slice (or one with no valid verdicts) yields "unverifiable" — there
//     is nothing to trust, but the finding is never dropped.
//
// nil entries and entries whose Verdict is not a known enum value are ignored
// (defensive: a malformed per-skeptic verdict should already be "unverifiable",
// but a stray nil must not panic the fold).
func aggregateVerdicts(perSkeptic []*reclib.Verification) *reclib.Verification {
	valid := make([]*reclib.Verification, 0, len(perSkeptic))
	for _, v := range perSkeptic {
		if v == nil {
			continue
		}
		switch v.Verdict {
		case verdictConfirmed, verdictRefuted, verdictUnverifiable:
			valid = append(valid, v)
		}
	}

	if len(valid) == 0 {
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "no_skeptic_verdicts"}
	}
	if len(valid) == 1 {
		// Pass-through: copy so callers never alias a per-skeptic struct.
		c := *valid[0]
		return &c
	}

	counts := map[string]int{}
	for _, v := range valid {
		counts[v.Verdict]++
	}
	winner, top, tie := "", 0, false
	for verdict, n := range counts {
		switch {
		case n > top:
			winner, top, tie = verdict, n, false
		case n == top:
			tie = true
		}
	}

	if tie {
		return &reclib.Verification{
			Verdict:   verdictUnverifiable,
			Skeptic:   joinSkeptics(valid),
			Notes:     combineReasonings(valid),
			Truncated: anyTruncated(filterByVerdict(valid, verdictUnverifiable)),
		}
	}
	winners := filterByVerdict(valid, winner)
	return &reclib.Verification{
		Verdict:   winner,
		Skeptic:   joinSkeptics(winners),
		Notes:     combineReasonings(winners),
		Truncated: anyTruncated(winners),
	}
}

// anyTruncated reports whether any of vs reached its verdict from a shortened
// read. It is applied to the SAME set winningAttribution credits tripped budgets
// from — the skeptics whose verdict the record actually reports — so the caveat
// on a finding and the budgets on its audit row cannot disagree about who is
// being described. On a decisive vote that is the winners; on a tie it is the
// participants who were themselves unverifiable, because a tie's unverifiable
// comes from the tie and annotating it with a losing voter's truncation would
// describe a verdict the record does not report.
func anyTruncated(vs []*reclib.Verification) bool {
	for _, v := range vs {
		if v != nil && v.Truncated {
			return true
		}
	}
	return false
}

func filterByVerdict(vs []*reclib.Verification, verdict string) []*reclib.Verification {
	out := make([]*reclib.Verification, 0, len(vs))
	for _, v := range vs {
		if v.Verdict == verdict {
			out = append(out, v)
		}
	}
	return out
}

// combineReasonings joins each verdict's "skeptic: notes" line so a human can read
// every reasoning behind the aggregate. Entries with empty Notes still contribute
// their verdict so silence is visible.
func combineReasonings(vs []*reclib.Verification) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		label := v.Skeptic
		if label == "" {
			label = v.Verdict
		}
		parts = append(parts, label+": "+v.Notes)
	}
	return strings.Join(parts, " | ")
}

func joinSkeptics(vs []*reclib.Verification) string {
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		if v.Skeptic != "" {
			names = append(names, v.Skeptic)
		}
	}
	return strings.Join(names, ", ")
}
