package reconcile

import (
	"math"
	"sort"
	"strings"
)

// Severity values and their ordering. Higher rank (see SeverityRank) wins a merge.
const (
	SevCritical = "CRITICAL"
	SevHigh     = "HIGH"
	SevMedium   = "MEDIUM"
	SevLow      = "LOW"
)

// Confidence values. HIGH = 2+ distinct reviewers, MEDIUM = single reviewer,
// LOW = reserved for untrusted sources (unused in v1).
const (
	ConfHigh   = "HIGH"
	ConfMedium = "MEDIUM"
	ConfLow    = "LOW"
)

// CategoryOutOfScope tags a finding as outside the reviewed change (a
// pre-existing issue in files mode, out-of-range in diff/blocks mode). Such
// findings are annotated rather than promoted: kept in the artifacts, counted in
// summaries, listed in a separate report section, and excluded from a severity
// gate.
const CategoryOutOfScope = "out-of-scope"

// Merged is one reconciled finding: the library Finding (with Reviewers,
// Confidence, the disagreement annotation, and any Verification block populated)
// is the whole record. It is a thin wrapper so callers can speak of a "merged
// finding" distinctly from a per-source input finding while sharing one struct.
type Merged struct {
	Finding
}

// Merge collapses a group of duplicate findings into one reconciled finding per
// the merge rules: REVIEWERS comma-joined+deduped+sorted; SEVERITY = max with a
// "<lo> vs <hi>" disagreement when lower severities are present; PROBLEM/FIX =
// longest; CATEGORY = modal (alpha tiebreak); EST_MINUTES = max; EVIDENCE =
// reviewer-prefixed concatenation; CONFIDENCE from the distinct-reviewer count.
// The location is the first finding's file/line (the group is co-located within
// the cluster window).
//
// Input Verification blocks are intentionally NOT propagated: Verification is
// stamped post-reconcile by the caller after the verify stage resolves verdicts.
func Merge(group []Finding) Merged {
	if len(group) == 0 {
		return Merged{} // defensive: callers never pass an empty group
	}
	reviewers := distinctReviewers(group)
	maxSev, disagreement := MergeSeverity(group)

	m := Finding{
		Severity:     maxSev,
		File:         group[0].File,
		Line:         group[0].Line,
		Problem:      LongestField(group, func(f Finding) string { return f.Problem }),
		Fix:          LongestField(group, func(f Finding) string { return f.Fix }),
		Category:     ModalCategory(group),
		EstMinutes:   MaxEstMinutes(group),
		Evidence:     mergeEvidence(group),
		Reviewers:    reviewers,
		Confidence:   ConfidenceFor(len(reviewers)),
		Disagreement: disagreement,
	}
	return Merged{Finding: m}
}

// distinctReviewers returns the sorted, deduplicated reviewer names in a group.
// Each input finding carries a single per-source Reviewer.
func distinctReviewers(group []Finding) []string {
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

// MergeSeverity returns the max severity and, when the group spans more than one
// distinct known severity, the "<lo> vs <hi>" disagreement annotation. It is a
// building block shared by Merge and embedders that merge already-reconciled
// records.
func MergeSeverity(group []Finding) (max, disagreement string) {
	seen := map[string]bool{}
	maxRank, minRank := -1, math.MaxInt
	var minSev string
	for _, f := range group {
		norm := NormalizeSeverity(f.Severity)
		rank, ok := SeverityRank[norm]
		if !ok {
			continue // unknown severity ignored for max/min
		}
		// Track the normalized form so a mixed-case duplicate (e.g. "critical"
		// and "CRITICAL") counts as one severity, not a spurious disagreement,
		// and the returned max/min are canonical.
		seen[norm] = true
		if rank > maxRank {
			maxRank, max = rank, norm
		}
		if rank < minRank {
			minRank, minSev = rank, norm
		}
	}
	if max == "" {
		// No known severity in the group: fall back to the first value normalized
		// so casing is consistent with every known-severity path.
		max = NormalizeSeverity(group[0].Severity)
	}
	if len(seen) > 1 {
		disagreement = minSev + " vs " + max
	}
	return max, disagreement
}

// LongestField returns the longest value of sel across the group (ties keep the
// first seen, for determinism). Shared building block.
func LongestField(group []Finding, sel func(Finding) string) string {
	best := ""
	for _, f := range group {
		if v := sel(f); len(v) > len(best) {
			best = v
		}
	}
	return best
}

// ModalCategory returns the most frequent CATEGORY, breaking ties
// alphabetically. Iteration is over sorted keys for determinism, and a non-empty
// category is preferred over the empty string on a tie so a single
// empty-category finding cannot hijack the modal result.
//
// out-of-scope is fail-closed: it wins only when EVERY finding in the group
// carries it. Any other value present (even one reviewer's) excludes
// out-of-scope from the vote, so a duplicate-majority cannot silently drop the
// real category and un-gate the finding. Shared building block.
func ModalCategory(group []Finding) string {
	counts := map[string]int{}
	allOutOfScope := true
	for _, f := range group {
		// Canonicalize (lower+trim) so non-canonical casings like "Out-Of-Scope"
		// or "SECURITY" collapse to the same key the gate, summary, and report
		// all compare against.
		cat := strings.ToLower(strings.TrimSpace(f.Category))
		counts[cat]++
		if cat != CategoryOutOfScope {
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

// MaxEstMinutes returns the most pessimistic estimate in the group. Shared
// building block.
func MaxEstMinutes(group []Finding) int {
	max := group[0].EstMinutes
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
func mergeEvidence(group []Finding) string {
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
	return strings.Join(parts, " / ")
}

// ConfidenceFor maps the distinct-reviewer count to a confidence level. Shared
// building block.
func ConfidenceFor(reviewerCount int) string {
	if reviewerCount >= 2 {
		return ConfHigh
	}
	return ConfMedium
}
