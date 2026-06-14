package reconcile

import (
	"sort"

	"github.com/samestrin/atcr/internal/stream"
)

// Severity values and their ordering. Higher rank wins a merge.
const (
	SevCritical = "CRITICAL"
	SevHigh     = "HIGH"
	SevMedium   = "MEDIUM"
	SevLow      = "LOW"
)

var severityRank = map[string]int{SevCritical: 4, SevHigh: 3, SevMedium: 2, SevLow: 1}

// Confidence values. HIGH = 2+ distinct reviewers, MEDIUM = single reviewer,
// LOW = reserved for untrusted sources (unused in v1).
const (
	ConfHigh   = "HIGH"
	ConfMedium = "MEDIUM"
	ConfLow    = "LOW"
)

// CategoryOutOfScope tags a finding as outside the reviewed change (a
// pre-existing issue in files mode, out-of-range in diff/blocks mode). Such
// findings are annotated rather than promoted (AC 06-04): kept in the
// artifacts, counted in summary.json, listed in a separate report section, and
// excluded from the severity gate (CountAtOrAbove).
const CategoryOutOfScope = "out-of-scope"

// Merged is one reconciled finding: a stream.Finding (with Reviewers + Confidence
// set) plus the severity-disagreement annotation when reviewers disagreed.
type Merged struct {
	stream.Finding
	Disagreement string // "<lo> vs <hi>" when the group spans multiple severities, else ""

	// Verification is the skeptic verdict block populated during the verify
	// re-emit (Epic 3.0); nil for a v1 finding or one below the min-severity
	// floor. The gate reads Verdict directly: a refuted finding is excluded, and
	// under requireVerified only a confirmed finding counts.
	Verification *Verification
}

// Merge collapses a group of duplicate findings into one reconciled finding per
// the merge rules (AC 01-05 / reconciler.md): REVIEWERS comma-joined+deduped+
// sorted; SEVERITY = max with a "<lo> vs <hi>" disagreement when lower
// severities are present; PROBLEM/FIX = longest; CATEGORY = modal (alpha
// tiebreak); EST_MINUTES = max; EVIDENCE = reviewer-prefixed concatenation;
// CONFIDENCE from the distinct-reviewer count. The location is the first
// finding's file/line (the group is co-located within the cluster window).
func Merge(group []stream.Finding) Merged {
	if len(group) == 0 {
		return Merged{} // defensive: callers never pass an empty group
	}
	reviewers := distinctReviewers(group)
	maxSev, disagreement := mergeSeverity(group)

	m := stream.Finding{
		Severity:   maxSev,
		File:       group[0].File,
		Line:       group[0].Line,
		Problem:    longestField(group, func(f stream.Finding) string { return f.Problem }),
		Fix:        longestField(group, func(f stream.Finding) string { return f.Fix }),
		Category:   modalCategory(group),
		EstMinutes: maxEstMinutes(group),
		Evidence:   mergeEvidence(group),
		Reviewers:  reviewers,
		Confidence: confidenceFor(len(reviewers)),
	}
	return Merged{Finding: m, Disagreement: disagreement}
}

// distinctReviewers returns the sorted, deduplicated reviewer names in a group.
// Each input finding carries a single per-source Reviewer.
func distinctReviewers(group []stream.Finding) []string {
	set := map[string]bool{}
	for _, f := range group {
		if f.Reviewer != "" {
			set[f.Reviewer] = true
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// mergeSeverity returns the max severity and, when the group spans more than one
// distinct known severity, the "<lo> vs <hi>" disagreement annotation.
func mergeSeverity(group []stream.Finding) (max, disagreement string) {
	seen := map[string]bool{}
	maxRank, minRank := -1, 1<<31
	var minSev string
	for _, f := range group {
		rank, ok := severityRank[f.Severity]
		if !ok {
			continue // unknown severity ignored for max/min
		}
		seen[f.Severity] = true
		if rank > maxRank {
			maxRank, max = rank, f.Severity
		}
		if rank < minRank {
			minRank, minSev = rank, f.Severity
		}
	}
	if max == "" {
		// No known severity in the group: fall back to the first value verbatim.
		max = group[0].Severity
	}
	if len(seen) > 1 {
		disagreement = minSev + " vs " + max
	}
	return max, disagreement
}

// longestField returns the longest value of sel across the group (ties keep the
// first seen, for determinism).
func longestField(group []stream.Finding, sel func(stream.Finding) string) string {
	best := ""
	for _, f := range group {
		if v := sel(f); len(v) > len(best) {
			best = v
		}
	}
	return best
}

// modalCategory returns the most frequent CATEGORY, breaking ties
// alphabetically. Iteration is over sorted keys for determinism, and a non-empty
// category is preferred over the empty string on a tie so a single
// empty-category finding cannot hijack the modal result.
//
// out-of-scope is fail-closed: it wins only when EVERY finding in the group
// carries it. Any other value present (even one reviewer's) excludes
// out-of-scope from the vote, so a duplicate-majority cannot silently drop the
// real category and un-gate the finding.
func modalCategory(group []stream.Finding) string {
	counts := map[string]int{}
	allOutOfScope := true
	for _, f := range group {
		counts[f.Category]++
		if f.Category != CategoryOutOfScope {
			allOutOfScope = false
		}
	}
	if allOutOfScope {
		return CategoryOutOfScope
	}
	delete(counts, CategoryOutOfScope)
	cats := make([]string, 0, len(counts))
	for cat := range counts {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	best, bestCount := "", -1
	for _, cat := range cats {
		c := counts[cat]
		switch {
		case c > bestCount:
			best, bestCount = cat, c
		case c == bestCount && best == "" && cat != "":
			best = cat // prefer a real category over the empty string on a tie
		}
	}
	return best
}

// maxEstMinutes returns the most pessimistic estimate in the group.
func maxEstMinutes(group []stream.Finding) int {
	max := 0
	for _, f := range group {
		if f.EstMinutes > max {
			max = f.EstMinutes
		}
	}
	return max
}

// mergeEvidence concatenates each finding's evidence, reviewer-prefixed when
// more than one reviewer contributed, joined by " / " (the writer flattens any
// newline to a space, so a flat separator keeps the txt row stable). A single
// reviewer's evidence passes through unprefixed.
func mergeEvidence(group []stream.Finding) string {
	if len(group) == 1 {
		return group[0].Evidence
	}
	parts := make([]string, 0, len(group))
	for _, f := range group {
		if f.Evidence == "" {
			continue
		}
		if f.Reviewer != "" {
			parts = append(parts, "["+f.Reviewer+"] "+f.Evidence)
		} else {
			parts = append(parts, f.Evidence)
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " / "
		}
		out += p
	}
	return out
}

// confidenceFor maps the distinct-reviewer count to a confidence level.
func confidenceFor(reviewerCount int) string {
	if reviewerCount >= 2 {
		return ConfHigh
	}
	return ConfMedium
}
