package benchmark

import (
	"sort"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/reconcile"
)

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
// read as drift.
//
// # How much headroom that actually needs — and why 0.20 may already be too tight
//
// The 35.16.2 dry-run's per-word tail is the one usable measurement (its review at
// .planning/epics/code-reviews/35.16.2_*/claude/2026-08-07_code-review.md §8). Of
// its 213 findings, the merge-table words listed above account for at least:
//
//	input 15 · failure 13 · resource 9 · clarity 5 · consistency 4 ·
//	structure 4 · resources 4   =  54 / 213  ≈  25%
//
// That is a FLOOR, not an estimate: the review enumerated only 12 of the 34 distinct
// categories emitted, and `bug`, `cleanliness`, and `stability` are not among the
// twelve. Replayed under THIS metric those 54 findings are drift, while the tail's
// other big entries (`contract` 28, `state` 21, `coupling` 9, `concurrency` 8,
// `duplication` 7, `naming` 4) are taxonomy members and are not.
//
// So on the only transcript in existence, merge-table words alone would have put the
// rate around 25% — ABOVE this ceiling. Read 0.20 as a deliberately tight fixture
// guard that the first real run may well trip, not as a bound live behaviour has
// been shown to satisfy. If V1 fails here, the finding is that 35.16.6's
// canonicalization is a prerequisite for a meaningful rate — NOT a licence to raise
// the number. Recomputing this share from V1's own output is how the ceiling should
// eventually be set.
//
// On the taxonomy's own design merits 0.20 is loose: category.go:73 ships `other`
// precisely so a reviewer that read its prompt always has a legal landing spot, so
// every out-of-vocabulary emission is a reviewer ignoring a 32-word enumeration.
//
// # Known hole: leaning on `other` entirely reads as flawless agreement
//
// `other` and `out-of-scope` are members of reconcile.Categories(), so they are IN
// vocabulary here. A reviewer or persona that labels EVERY finding `other` therefore
// reports a rate of 0.0 — identical to a reviewer that categorized every finding
// precisely — while conveying no categorical information at all. This is the same
// collapse the nil-vs-0 pointer prevents one level up, and it is currently NOT
// prevented. It interacts with the equivalence relation: `other` is hard-excluded
// from every family, so an all-`other` reviewer scores recall 0.0 AND drift 0.0
// simultaneously — that pairing is the signature to look for.
//
// This is recorded, pinned by TestOutOfVocabularyRate_AllOtherIsAKnownBlindSpot, and
// deliberately NOT fixed here: excluding the routing values would change what this
// metric means, and the choice belongs with the 35.16.6 canonicalization work rather
// than being made silently. Do not read a 0.0 as clean without checking recall.
// The right move is to TIGHTEN this in 35.16.6 once the post-merge validation run
// supplies the first real number under this metric — never to loosen it when a run
// fails.
const MaxOutOfVocabularyRate = 0.20

// ExceedsVocabularyCeiling reports whether a measured rate breaches
// MaxOutOfVocabularyRate.
//
// The comparison lives HERE rather than in each caller so the ceiling's exclusive
// semantics — a run sitting exactly on it trips the guard — are a property of the
// package instead of whichever operator a given test happened to type. Before this
// existed the constant had no non-test consumer at all: a real run measuring 0.72
// wrote the number to JSON and exited 0 with no warning, while the doc above and
// the CHANGELOG both described enforcement that did not exist.
//
// A nil rate is UNMEASURED, not clean, and is never a breach — the same nil-vs-zero
// distinction RunResult.OutOfVocabularyRate's pointer carries.
func ExceedsVocabularyCeiling(rate *float64) bool {
	return rate != nil && *rate >= MaxOutOfVocabularyRate
}

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

// ReviewerVocabulary is one reviewer row's out-of-vocabulary breakdown — the
// per-reviewer detail OutOfVocabularyRate's single scalar structurally cannot carry.
//
// Micro-averaging is the right choice for the run-level number and is deliberately
// unchanged, but it makes the scalar CONCEALING rather than merely coarse: 80 clean
// findings from one reviewer and 12 drifted from another pool to 0.130, under the
// ceiling, and the run reports clean while one of two models ignored the enumeration
// entirely. This type is what names it.
//
// It is a DIAGNOSTIC array on the run result, not a reviewer metric, for the same
// reason OutOfVocabularyRate sits there: scorecard.PublicRecord is the frozen public
// schema shared byte-for-byte with production `leaderboard --export`, and a
// benchmark-only column does not belong in a public submission. BuildSubmission
// accordingly does not carry it forward.
type ReviewerVocabulary struct {
	// Model and Persona are the REALIZED identity, matching the row PublicRecord
	// publishes and Score sorts on — the same identity ReviewerCoverage joins by.
	// The lane (configured agent name) is deliberately NOT carried: since the fold
	// re-keyed on the realized identity, two lanes that realize the same
	// (model, persona) merge into one row, so a single agent name is not
	// well-defined per row.
	Model   string `json:"model"`
	Persona string `json:"persona"`

	// Findings is this reviewer's own denominator and Drifted its numerator, both
	// published alongside Rate rather than left implicit. A rate without its counts
	// cannot be read: 1.0 from one finding and 1.0 from eighty are the same number
	// and very different facts, and the operator warning quotes both for exactly
	// that reason.
	Findings int `json:"findings"`
	Drifted  int `json:"drifted"`

	// Rate is Drifted/Findings — this reviewer's findings pooled the same way the
	// run-level rate pools the run's, NOT a macro-average of its per-case rates.
	//
	// A POINTER for the same nil-vs-zero reason the run-level field is one, applied
	// per row: a reviewer that raised nothing is UNMEASURED, and a failed reviewer
	// reporting 0.0 would publish the most drifted possible row as flawless
	// vocabulary agreement. omitempty drops the key only when the pointer is nil.
	Rate *float64 `json:"rate,omitempty"`

	// RoutingValues counts this reviewer's findings labelled with one of the two
	// ROUTING values — `other` and `out-of-scope` — as opposed to a descriptive
	// category.
	//
	// It exists to close the all-`other` blind spot recorded on
	// MaxOutOfVocabularyRate: both routing values are taxonomy members, so a reviewer
	// that labels EVERY finding `other` reports drift 0.0 — identical to a reviewer
	// that categorized every finding precisely — while conveying no categorical
	// information at all. RoutingValues == Findings alongside Rate 0.0 is that
	// signature; pair it with the recall on the positionally-aligned Reviewers row to
	// confirm (drift 0.0 WITH recall 0.0 means "categorized nothing").
	//
	// This is deliberately a COUNT rather than a redefinition of the rate. Excluding
	// the routing values from the vocabulary would conflate two unrelated failures —
	// category.go:73 ships `other` precisely as the escape hatch that makes the set
	// closed rather than lossy, and reaching for it is a reviewer obeying its prompt,
	// not drifting — and would strand the V1 baseline measured under the current
	// definition. NOT omitempty: a real zero is a measurement (this reviewer routed
	// nothing), and dropping it would make a clean reviewer indistinguishable from
	// one whose row predates the field.
	RoutingValues int `json:"routing_values"`
}

// PerReviewerVocabulary breaks the run's out-of-vocabulary drift down per reviewer.
//
// Membership, normalization, and what counts as drift are IDENTICAL to
// OutOfVocabularyRate — same vocabularySet, same normalize, empty categories still
// count as drift — so an entry's rate is never a differently-defined number wearing
// the same name. Summing the entries' Drifted and Findings reproduces the run-level
// numerator and denominator exactly; the scalar is those two totals divided, and these
// are the same division taken per row.
//
// The returned slice is POSITIONALLY ALIGNED with Score's output for the same input:
// it sorts by the same (model, persona) key with the same stable sort, so entry i
// describes Reviewers[i]. That alignment is what lets a consumer read the all-`other`
// signature — RoutingValues == Findings here, CorroborationRate 0.0 there — without
// this array duplicating a frozen-schema field. Recall is deliberately NOT copied in.
//
// Alignment is by SORT, not by a key join, and that is load-bearing: Score's sort is
// stable precisely because two reviewers can share a (model, persona) pair, and a key
// join cannot disambiguate a collision — which is the case this diagnostic most needs
// to report correctly. Both sides preserve the caller's order within a tie, so the two
// slices stay in lockstep even then.
//
// Every reviewer gets an entry, including one that raised nothing: an absent row would
// silently read as a reviewer that did not exist rather than one that produced nothing.
// A nil/empty input returns nil so the run-result key is omitted entirely.
func PerReviewerVocabulary(reviewers []ReviewerScore) []ReviewerVocabulary {
	if len(reviewers) == 0 {
		return nil
	}

	vocabulary := vocabularySet()
	routing := map[string]bool{
		normalize(reconcile.CategoryOther):      true,
		normalize(reconcile.CategoryOutOfScope): true,
	}

	out := make([]ReviewerVocabulary, 0, len(reviewers))
	for _, r := range reviewers {
		// Scrubbed here for the same reason buildRunResult scrubs before sorting: Score
		// re-scrubs the rows it emits, so an unscrubbed identity would sort into a
		// different position than its counterpart and break the positional alignment
		// this function documents.
		id := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: r.Model, Persona: r.Persona})
		e := ReviewerVocabulary{Model: id.Model, Persona: id.Persona}
		for _, c := range r.Cases {
			for _, raw := range c.Raised {
				n := normalize(raw)
				e.Findings++
				if !vocabulary[n] {
					e.Drifted++
				}
				if routing[n] {
					e.RoutingValues++
				}
			}
		}
		if e.Findings > 0 {
			rate := float64(e.Drifted) / float64(e.Findings)
			e.Rate = &rate
		}
		out = append(out, e)
	}

	// Mirrors Score's sort exactly — same key, same stability — so entry i and
	// Reviewers[i] describe the same row.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Persona < out[j].Persona
	})
	return out
}

// vocabularySet is the closed vocabulary as a lookup set, normalized the same way
// the scorer normalizes a raised category so case and padding never register as
// drift.
//
// # What is NOT folded, and why that inflates the measured rate
//
// normalize (score.go) is ToLower(TrimSpace(s)) and nothing more. SEPARATORS are not
// folded, and 5 of the 32 members are hyphenated — so every separator variant a real
// model emits counts as full drift against this list:
//
//	error_handling · "error handling" · input_validation · "resource leak" · "api contract"
//
// Those are SPELLINGS of a member, not vocabulary drift, and each one inflates the
// rate against MaxOutOfVocabularyRate. Separator and hyphenation folding is epic
// 35.16.6's parse-boundary canonicalization and is deliberately out of scope here
// (folding reconcile.CategoryMerges() likewise — see the const doc above), so the
// first real run may fail the ceiling on a normalization artifact rather than on
// genuine drift. Diagnose a failure by inspecting the emitted words before treating
// the number as model behaviour.
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
