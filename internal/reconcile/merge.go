// merge.go holds the finding-merge rules: collapse a group of duplicate findings
// into one reconciled record (max severity with disagreement annotation, modal
// category, longest problem/fix, evidence concatenation, confidence) plus the
// package-local SeverityRank copy. The package doc lives in disagree.go.

package reconcile

import (
	"math"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// Severity values and their ordering. Higher rank wins a merge.
const (
	SevCritical = "CRITICAL"
	SevHigh     = "HIGH"
	SevMedium   = "MEDIUM"
	SevLow      = "LOW"
)

// SeverityRank is an independent copy of the canonical rank map from
// internal/stream (the single source of truth). Higher rank wins a merge and
// sorts earlier in both the reconcile radar and the report view; unknown
// severities sort last (rank 0). Copied at package init so mutations to one
// package's map cannot corrupt the other; internal lookups read it unqualified
// and external callers keep a stable symbol.
var SeverityRank = func() map[string]int {
	m := make(map[string]int, len(stream.SeverityRank))
	for k, v := range stream.SeverityRank {
		m[k] = v
	}
	return m
}()

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
	maxRank, minRank := -1, math.MaxInt
	var minSev string
	for _, f := range group {
		norm := stream.NormalizeSeverity(f.Severity)
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
		max = stream.NormalizeSeverity(group[0].Severity)
	}
	if len(seen) > 1 {
		disagreement = minSev + " vs " + max
	}
	return max, disagreement
}

// widestDisagreement extends a scalar-severity disagreement with each member's
// pre-existing "<lo> vs <hi>" range, so a member that already recorded a wider
// span than its scalar Severity is not narrowed at cluster merge. It returns the
// "<lo> vs <max>" annotation when any source (the scalar disagreement, or a
// member's recorded range) carries a lower bound below maxSev, else the original
// scalar disagreement unchanged. Members' range lower bounds are read from the
// "<lo> vs <hi>" form spreadFromDisagreement parses; here only the <lo> tier is
// needed, so it is taken directly.
func widestDisagreement(maxSev, scalarDisagreement string, group []JSONFinding) string {
	maxRank := SeverityRank[maxSev]
	loRank, loSev := maxRank, maxSev
	consider := func(rangeAnnotation string) {
		if rangeAnnotation == "" {
			return
		}
		lo := stream.NormalizeSeverity(strings.SplitN(rangeAnnotation, " vs ", 2)[0])
		if r, ok := SeverityRank[lo]; ok && r < loRank {
			loRank, loSev = r, lo
		}
	}
	consider(scalarDisagreement)
	for _, f := range group {
		consider(f.Disagreement)
	}
	if loRank < maxRank {
		return loSev + " vs " + maxSev
	}
	return scalarDisagreement
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
		// Canonicalize (lower+trim) so non-canonical casings like "Out-Of-Scope"
		// or "SECURITY" collapse to the same key the gate, summary, and report
		// all compare against — without this, the gate excludes the finding via
		// normalized match but the summary count and report section miss it via
		// exact-match against CategoryOutOfScope.
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

// MergeJSONFindings collapses a group of findings.json records — a gray-zone
// cluster's members the judge ruled "merge" (Epic 6.1) — into one reconciled
// record. It applies the same field rules as Merge (severity max with a
// "<lo> vs <hi>" disagreement annotation, longest problem/fix, modal category,
// max est-minutes) but over the already-reconciled JSONFinding shape, where
// REVIEWERS is a list (the merge unions the per-record lists, not a single
// per-source name). The location is the first member's file/line; EVIDENCE is the
// distinct members' evidence joined by " / "; CONFIDENCE follows the unioned
// reviewer count. Verification is combined by mergeVerification (verdict
// precedence confirmed > unverifiable > refuted, skeptic provenance unioned) and
// the Epic 5.0 path-validation fields carry the first member's — the members are
// co-located, so their path status is identical. The caller sets ClusterMerged on
// the result. A
// zero- or one-member group is returned as-is (the apply path never unions fewer
// than two records, but the helper stays total).
func MergeJSONFindings(group []JSONFinding) JSONFinding {
	if len(group) == 0 {
		return JSONFinding{}
	}
	if len(group) == 1 {
		return group[0]
	}
	sf := make([]stream.Finding, len(group))
	for i, f := range group {
		sf[i] = stream.Finding{
			Severity: f.Severity, File: f.File, Line: f.Line,
			Problem: f.Problem, Fix: f.Fix, Category: f.Category,
			EstMinutes: f.EstMinutes, Evidence: f.Evidence,
		}
	}
	maxSev, disagreement := mergeSeverity(sf)
	// A member is itself a reconciled record that may already carry a wider
	// "<lo> vs <hi>" span than its scalar Severity (e.g. "LOW vs HIGH" while
	// Severity is "HIGH"). mergeSeverity sees only the scalar severities, so fold
	// each member's pre-existing range lower bound back in — otherwise the merged
	// annotation silently narrows to the scalar max/min and understates reviewer
	// tension (TD merge.go:282).
	disagreement = widestDisagreement(maxSev, disagreement, group)
	reviewers := unionReviewers(group)
	return JSONFinding{
		Severity:       maxSev,
		File:           group[0].File,
		Line:           group[0].Line,
		Problem:        longestField(sf, func(f stream.Finding) string { return f.Problem }),
		Fix:            longestField(sf, func(f stream.Finding) string { return f.Fix }),
		Category:       modalCategory(sf),
		EstMinutes:     maxEstMinutes(sf),
		Evidence:       joinEvidence(group),
		Reviewers:      reviewers,
		Confidence:     confidenceFor(len(reviewers)),
		Disagreement:   disagreement,
		Verification:   mergeVerification(group),
		PathValid:      group[0].PathValid,
		PathWarning:    group[0].PathWarning,
		PathSuggestion: group[0].PathSuggestion,
	}
}

// unionReviewers returns the sorted, deduplicated union of every member record's
// REVIEWERS list (each JSONFinding already carries a reconciled list).
func unionReviewers(group []JSONFinding) []string {
	set := map[string]bool{}
	for _, f := range group {
		for _, r := range f.Reviewers {
			if r != "" {
				set[r] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// joinEvidence concatenates each member's distinct, non-empty EVIDENCE in member
// order, joined by " / ". Duplicates (the same evidence string from co-located
// members) collapse to one so the merged evidence does not double-count.
func joinEvidence(group []JSONFinding) string {
	seen := map[string]bool{}
	parts := make([]string, 0, len(group))
	for _, f := range group {
		if f.Evidence == "" || seen[f.Evidence] {
			continue
		}
		seen[f.Evidence] = true
		parts = append(parts, f.Evidence)
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

// verdictRank orders verify verdicts for the cluster-merge precedence in
// mergeVerification: confirmed (gate-blocking) outranks unverifiable, which
// outranks refuted; an unknown/empty verdict ranks last.
func verdictRank(verdict string) int {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case VerdictConfirmed:
		return 3
	case VerdictUnverifiable:
		return 2
	case VerdictRefuted:
		return 1
	default:
		return 0
	}
}

// mergeVerification combines the member Verification blocks of an inline cluster
// merge into one (Epic 6.1). The judge ruled the members duplicates, so they must
// carry a single verdict: precedence is confirmed > unverifiable > refuted, so a
// confirmed verification of the same underlying issue is never masked by a refuted
// sibling phrasing, and a refuted verdict wins only when no member was confirmed
// or unverifiable. The winning block's Verdict/Notes/ChallengeSurvived are kept
// (ties resolve to the first member, for determinism), and every member's Skeptic
// provenance is unioned (deduped, comma-joined) so no voter is lost. Returns nil
// when no member carried a block.
func mergeVerification(group []JSONFinding) *Verification {
	var chosen *Verification
	var skeptics []string
	seen := map[string]bool{}
	for i := range group {
		v := group[i].Verification
		if v == nil {
			continue
		}
		for _, name := range splitNames(v.Skeptic) {
			if name != "" && !seen[name] {
				seen[name] = true
				skeptics = append(skeptics, name)
			}
		}
		if chosen == nil || verdictRank(v.Verdict) > verdictRank(chosen.Verdict) {
			chosen = v
		}
	}
	if chosen == nil {
		return nil
	}
	out := *chosen // copy so the source finding's block is not mutated
	out.Skeptic = strings.Join(skeptics, ", ")
	return &out
}
