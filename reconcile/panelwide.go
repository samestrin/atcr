package reconcile

import "strings"

// The panel-wide-instruction class (epic 35.16.17).
//
// Every built-in persona carries the predicate-exhaustiveness rule
// (personas/_base.md:12), which tells the reviewer to file an asymmetric
// predicate with CATEGORY invariant, quoting the sibling branch as evidence.
// Nine reviewers given one instruction and pointed at one predicate diff file
// one correlated finding, and ConfidenceFor reads the reviewer count as
// corroboration. A group in this class reached its reviewer count because the
// panel was told to look there, not because reviewers looked independently.
//
// The class is keyed on the category the rule mandates, not on the rule's text,
// so a future panel-wide rule that mandates the same category is covered too.
// CategoryInvariant is a general category, so category alone is not enough: a
// group is in the class only when its members also cite no independent
// corroboration — every member is in one file and quotes near-identical
// evidence. Two reviewers who reach an invariant finding from different files,
// or who quote different code, corroborate each other and stay out of it.
//
// A mixed cluster is classed by its modal category, the one Merge writes on the
// reconciled row (ModalCategory). penny.md lets its variant of the rule file
// correctness instead of invariant, so:
//
//   - modal invariant: every member is tested, whatever its own category. A
//     correctness member quoting the same sibling branch in the same file is the
//     same instruction speaking and does not break the class; a member of any
//     category with distinct evidence or a distinct file does.
//   - modal anything else (correctness included): not in the class. A
//     discounted row therefore always reads invariant, and a cluster where the
//     correctness variant outvotes invariant keeps its reviewer-count
//     confidence. That false negative is accepted over discounting a row whose
//     category does not name the class.
//
// A group with fewer than two distinct reviewers is never in the class: there is
// no agreement to discount, and a singleton's authority promotion
// (promoteByAuthority) is independent signal.

// PanelWideReason is the short reason recorded on a reconciled finding whose
// confidence was discounted because IsPanelWide held for its group. In full:
// reviewers filed one invariant finding in one file with near-identical
// evidence, so their agreement is not independent corroboration. It is kept
// under 80 characters because it is appended to the problem text of the TD row.
const PanelWideReason = "panel-wide instruction: invariant finding, one file, near-identical evidence"

// IsPanelWide reports whether a merge group is in the panel-wide-instruction
// class. It is IsPanelWideQuotes over the group's quotes, the form Merge
// carries on Merged.SourceQuotes.
func IsPanelWide(group []Finding) bool { return IsPanelWideQuotes(sourceQuotes(group)) }

// IsPanelWideQuotes reports whether a set of per-finding quotes is in the
// panel-wide-instruction class: two or more distinct reviewers, a modal
// category of CategoryInvariant, every quote in one file, and every pair of
// quotes near-identical and non-empty. It is the one independence predicate:
// Merge applies it to its group, and an embedder applies it to the union of
// the Merged.SourceQuotes it is combining.
//
// Evidence is compared the way dedupe compares PROBLEM text: the token-set
// Jaccard similarity of the lowercased alphanumeric words must reach
// MergeThreshold for every pair. A member whose evidence has no words quotes
// no sibling branch, so it keeps the group out of the class: a group whose
// members all quote nothing has no shared quote to discount. File paths are
// compared after trimming surrounding space; the line is not compared, because
// the group is already co-located within the cluster window.
func IsPanelWideQuotes(quotes []SourceQuote) bool {
	group := make([]Finding, len(quotes))
	for i, q := range quotes {
		group[i] = Finding{Reviewer: q.Reviewer, File: q.File, Category: q.Category, Evidence: q.Evidence}
	}
	if len(distinctReviewers(group)) < 2 {
		return false
	}
	if ModalCategory(group) != CategoryInvariant {
		return false
	}
	file := strings.TrimSpace(group[0].File)
	toks := make([]map[string]struct{}, len(group))
	for i, f := range group {
		if strings.TrimSpace(f.File) != file {
			return false
		}
		toks[i] = tokenize(f.Evidence)
		if len(toks[i]) == 0 {
			return false
		}
	}
	for i := range toks {
		for j := i + 1; j < len(toks); j++ {
			if rel, _ := classify(toks[i], toks[j]); rel != relMerge {
				return false
			}
		}
	}
	return true
}
