package benchmark

import "github.com/samestrin/atcr/reconcile"

// MaxOutOfVocabularyRate is the ceiling a benchmark run's out-of-vocabulary rate
// must stay strictly under. It is EXCLUSIVE: a run sitting exactly on it trips the
// guard.
//
// # What 0.20 is, and what it is not
//
// It is NOT derived from the 35.16.2 dry-run's 72.3% (reconcile/category.go:9-12).
// That figure counted findings "the scorer did not recognise", whose denominator
// was the four values internal/benchmarkimport maps ground truth to — not
// membership of this 32-word vocabulary. Worse, the vocabulary was deliberately
// derived as a union that INCLUDES the words that dry-run emitted, so replaying
// the same findings under this metric would score far below 72.3% by
// construction. Treat 72.3% as no baseline at all.
//
// What 0.20 actually buys is headroom for the words reconcile's categoryMerges
// (category.go:143-190) records as meaning a member without being one — `bug`,
// `input`, `clarity`, `cleanliness`, `consistency`, `structure`, `failure`,
// `stability`, `resource`, `resources`. Nothing folds them until epic 35.16.6
// lands parse-boundary canonicalization, so under a bare membership test they all
// read as drift. A tighter ceiling would assert a property of live model
// behaviour that no committed measurement supports and no fixture can prove.
//
// On the taxonomy's own design merits 0.20 is loose: category.go:73 ships `other`
// precisely so a reviewer that read its prompt always has a legal landing spot, so
// every out-of-vocabulary emission is a reviewer ignoring a 32-word enumeration.
// The right move is to TIGHTEN this in 35.16.6 once the post-merge validation run
// supplies the first real number under this metric — never to loosen it when a run
// fails.
const MaxOutOfVocabularyRate = 0.20

// OutOfVocabularyRate is the share of a run's findings whose category is not a
// member of the closed reviewer vocabulary (reconcile.Categories()).
//
// It measures model drift away from the enumeration, which nothing else surfaces:
// a reviewer that invents its own words quietly zeroes its own recall, and the
// resulting low score is indistinguishable from a reviewer that simply found less.
//
// Three definitional choices, each of which changes what the number means:
//
//   - The denominator is FINDINGS, not distinct categories — matching
//     CaseScore.Raised's one-entry-per-finding semantics. A distinct-category
//     denominator would let a single prolific in-vocabulary category mask thirty
//     drifted findings, and inversely make one stray word read as a large share of
//     a small set.
//   - Membership is decided by BARE non-membership after normalize, with
//     reconcile.CategoryMerges() deliberately NOT applied. That table is epic
//     35.16.6's canonicalization contract; folding it in here would reach into that
//     epic's scope and silently change what this metric means between releases.
//     It is also what makes 0.20 rather than 0.05 the defensible ceiling.
//   - A finding with an EMPTY category counts as out of vocabulary. Excluding it
//     would produce a rate that improves when a reviewer stops labelling entirely.
//
// The rate is micro-averaged across the whole run rather than macro-averaged per
// reviewer: it is a property of the run's findings, and a reviewer that raised two
// findings should not weigh as heavily as one that raised eighty.
//
// A run with no findings at all returns nil, NOT 0. A run in which every reviewer
// errored raised nothing to measure, and reporting that as 0.0 would publish the
// most drifted possible run as flawless vocabulary agreement — the exact collapse
// RunResult.OutOfVocabularyRate's pointer exists to prevent. nil means unmeasured;
// a non-nil 0 means measured and clean.
func OutOfVocabularyRate(reviewers []ReviewerScore) *float64 {
	vocabulary := vocabularySet()

	var total, drifted int
	for _, r := range reviewers {
		for _, c := range r.Cases {
			for _, raw := range c.Raised {
				total++
				if !vocabulary[normalize(raw)] {
					drifted++
				}
			}
		}
	}

	if total == 0 {
		return nil
	}
	rate := float64(drifted) / float64(total)
	return &rate
}

// vocabularySet is the closed vocabulary as a lookup set, normalized the same way
// the scorer normalizes a raised category so case and padding never register as
// drift.
//
// Built per call rather than cached in a package var: reconcile.Categories()
// returns a fresh copy by design, and this runs once per run result, not per
// finding. A cached set would trade a real (if small) staleness hazard for an
// allocation nobody is counting.
func vocabularySet() map[string]bool {
	cats := reconcile.Categories()
	out := make(map[string]bool, len(cats))
	for _, c := range cats {
		out[normalize(c)] = true
	}
	return out
}
