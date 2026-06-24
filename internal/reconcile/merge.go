// merge.go holds ATCR's gray-zone JSON-record merge: collapse a group of
// already-reconciled findings.json records (members a judge ruled "merge" in the
// Epic 6.1 cross-examination apply path) into one record. It operates on the
// ATCR-internal JSONFinding (which carries path-validation fields and a
// Verification block), so it stays in internal/reconcile rather than the
// stdlib-only library (Epic 8.0 Phase 2 Clarification Q2). The shared per-field
// merge rules (severity max + disagreement, longest text, modal category, max
// estimate, confidence) are reused from the library so there is one
// implementation. The package doc lives in disagree.go.

package reconcile

import (
	"sort"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// MergeJSONFindings collapses a group of findings.json records — a gray-zone
// cluster's members the judge ruled "merge" (Epic 6.1) — into one reconciled
// record. It applies the same field rules as the library Merge (severity max with
// a "<lo> vs <hi>" disagreement annotation, longest problem/fix, modal category,
// max est-minutes) but over the already-reconciled JSONFinding shape, where
// REVIEWERS is a list (the merge unions the per-record lists, not a single
// per-source name). The location is the first member's file/line; EVIDENCE is the
// distinct members' evidence joined by " / "; CONFIDENCE follows the unioned
// reviewer count. Verification is combined by mergeVerification (verdict
// precedence confirmed > unverifiable > refuted, skeptic provenance unioned) and
// the Epic 5.0 path-validation fields carry the first member's — the members are
// co-located, so their path status is identical. The caller sets ClusterMerged on
// the result. A zero- or one-member group is returned as-is (the apply path never
// unions fewer than two records, but the helper stays total).
func MergeJSONFindings(group []JSONFinding) JSONFinding {
	if len(group) == 0 {
		return JSONFinding{}
	}
	if len(group) == 1 {
		return group[0]
	}
	sf := make([]reclib.Finding, len(group))
	for i, f := range group {
		sf[i] = reclib.Finding{
			Severity: f.Severity, File: f.File, Line: f.Line,
			Problem: f.Problem, Fix: f.Fix, Category: f.Category,
			EstMinutes: f.EstMinutes, Evidence: f.Evidence,
		}
	}
	maxSev, disagreement := reclib.MergeSeverity(sf)
	// A member is itself a reconciled record that may already carry a wider
	// "<lo> vs <hi>" span than its scalar Severity (e.g. "LOW vs HIGH" while
	// Severity is "HIGH"). MergeSeverity sees only the scalar severities, so fold
	// each member's pre-existing range lower bound back in — otherwise the merged
	// annotation silently narrows to the scalar max/min and understates reviewer
	// tension (TD merge.go:282).
	disagreement = widestDisagreement(maxSev, disagreement, group)
	reviewers := unionReviewers(group)
	merged := JSONFinding{
		Severity:     maxSev,
		File:         group[0].File,
		Line:         group[0].Line,
		Problem:      reclib.LongestField(sf, func(f reclib.Finding) string { return f.Problem }),
		Fix:          reclib.LongestField(sf, func(f reclib.Finding) string { return f.Fix }),
		Category:     reclib.ModalCategory(sf),
		EstMinutes:   reclib.MaxEstMinutes(sf),
		Evidence:     joinEvidence(group),
		Reviewers:    reviewers,
		Confidence:   reclib.ConfidenceFor(len(reviewers)),
		Disagreement: disagreement,
		Verification: mergeVerification(group),
	}
	merged.PathValid, merged.PathWarning, merged.PathSuggestion = mergePathFields(group)
	return merged
}

// widestDisagreement extends a scalar-severity disagreement with each member's
// pre-existing "<lo> vs <hi>" range, so a member that already recorded a wider
// span than its scalar Severity is not narrowed at cluster merge. It returns the
// "<lo> vs <max>" annotation when any source (the scalar disagreement, or a
// member's recorded range) carries a lower bound below maxSev, else the original
// scalar disagreement unchanged.
func widestDisagreement(maxSev, scalarDisagreement string, group []JSONFinding) string {
	maxRank := SeverityRank[maxSev]
	loRank, loSev := maxRank, maxSev
	consider := func(rangeAnnotation string) {
		if rangeAnnotation == "" {
			return
		}
		lo := NormalizeSeverity(strings.SplitN(rangeAnnotation, " vs ", 2)[0])
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

// mergePathFields picks coherent Epic 5.0/5.4 path-validation fields for a cluster
// merge. PathWarning (authoritative, file-existence keyed) and PathSuggestion (set
// only on a candidate-index-corrected member) are each taken as the first non-empty
// across the group, so a sibling's hallucinated-path warning or its correction is
// never lost just because group[0] happened to be a clean/uncorrected member —
// members may even span lines under cross-line drift, so group[0]'s fields are not
// authoritative (TD merge.go:297, merge.go:296). PathValid is auxiliary (see the
// JSONFinding contract) and is kept consistent with the surviving warning: a record
// carrying a warning is not a valid path; with no warning anywhere, group[0]'s
// validated state is preserved.
func mergePathFields(group []JSONFinding) (valid bool, warning, suggestion string) {
	for _, f := range group {
		if warning == "" {
			warning = f.PathWarning
		}
		if suggestion == "" {
			suggestion = f.PathSuggestion
		}
	}
	if warning != "" {
		return false, warning, suggestion
	}
	return group[0].PathValid, "", suggestion
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
	return strings.Join(parts, " / ")
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
