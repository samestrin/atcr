package benchmark

import (
	"sort"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/samestrin/atcr/reconcile"
)

// MaxOutOfVocabularyRate is the ceiling a benchmark run's out-of-vocabulary rate is
// WARNED against: its only consumer is warnIfVocabularyCeilingExceeded, which writes
// to stderr and deliberately does not change the exit code. The bound is EXCLUSIVE —
// a run sitting exactly on it is reported.
//
// The number moves ONE WAY: tighten it when a further valid run supports tightening,
// NEVER raise it because a run was flagged — a ceiling that yields to the run it is
// judging measures nothing. That rule predates this value and survives it; pinned by
// TestMaxOutOfVocabularyRate_IsDerivedFromTheV1Measurement.
//
// The provenance of 0.05 (derived from the V1 validation run's measured 0.0100, the
// retired 0.20 argument, the n=1 headroom), the read-a-breach-as-a-parser-question
// guidance, and the all-`other` blind spot this number does not see are
// operator-facing narrative: docs/benchmark.md, "The run-result contract".
const MaxOutOfVocabularyRate = 0.05

// ExceedsVocabularyCeiling reports whether a measured rate breaches
// MaxOutOfVocabularyRate.
//
// The comparison lives HERE rather than in each caller so the ceiling's exclusive
// semantics — a run sitting exactly on it is reported — are a property of the
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

// MaxReviewerDriftRate is the PER-REVIEWER out-of-vocabulary rate at or above which
// a single reviewer is named. Like MaxOutOfVocabularyRate the bound is EXCLUSIVE — a
// reviewer sitting exactly on it is reported — and like it the only consumer writes to
// stderr without changing the exit code.
//
// # Why this is NOT MaxOutOfVocabularyRate
//
// Reusing the run-level ceiling here is the obvious move and the wrong one. That number
// is a RUN guard, tightened to 0.05 from a single valid observation (V1's 0.0100), and
// the epic that tightened it recorded the caveat explicitly: n=1, so variance under this
// metric is unmeasured. Applying an n=1-derived bound to individual rows assumes a
// tightness one observation cannot support — and a per-reviewer signal that fires on
// ordinary between-model variation reproduces exactly the defect
// warnIfVocabularyCeilingExceeded's own doc names: a warning printed on every run is a
// warning nobody reads.
//
// 0.50 is a qualitatively different claim — a MAJORITY of this reviewer's own findings
// missed a 32-word enumeration it was handed — rather than a quantitative reading of a
// distribution nobody has measured yet. The case this warning exists for (one reviewer
// at 100% drift hidden under a passing run rate) clears it by a factor of two.
//
// Tighten this once a second valid run makes the spread between models measurable.
// Until then a looser threshold costs a missed moderate drifter, while a tighter one
// costs the signal's credibility on every run.
const MaxReviewerDriftRate = 0.50

// ExceedsReviewerDriftRate reports whether one reviewer's measured rate breaches
// MaxReviewerDriftRate.
//
// The comparison lives HERE, next to the metric PerReviewerVocabulary computes, for the
// same reason ExceedsVocabularyCeiling does: the bound's inclusive semantics are a
// property of the package rather than of whichever operator a given caller happened to
// type. Before this existed the threshold was an unexported cli constant with `>= 0.50`
// inlined at its one call site, so nothing outside package cli could reuse the boundary
// without restating it.
//
// A nil rate is UNMEASURED, not clean, and is never a breach — the same nil-vs-zero
// distinction ReviewerVocabulary.Rate's pointer carries.
func ExceedsReviewerDriftRate(rate *float64) bool {
	return rate != nil && *rate >= MaxReviewerDriftRate
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
//     reconcile.CategoryMerges() deliberately NOT applied. That table is the
//     parse-boundary canonicalization epic's contract; folding it in here would
//     reach into that epic's scope and silently change what this metric means between
//     releases. This choice USED to be what made 0.20 rather than 0.05 the defensible
//     ceiling — V1 then emitted zero merge-table words, retiring that argument without
//     changing the choice itself, and the ceiling is now 0.05 (see MaxOutOfVocabularyRate).
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
// unchanged, but it makes the scalar CONCEALING rather than merely coarse: 300 clean
// findings from one reviewer and 12 drifted from another pool to 0.038, under the
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
// Alignment is by SORT, not by a key join, and that is load-bearing: a key join
// cannot disambiguate two entries sharing a (model, persona) pair. Production never
// presents one — the fold in cli/benchmark_run.go accumulates per realized identity,
// so two lanes realizing the same pair merge into one row upstream of this function —
// but the package API stays correct if a caller passes one: Score's sort is stable,
// both sides preserve the caller's order within a tie, and the two slices stay in
// lockstep even then.
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

	// Mirrors Score's sort exactly — same comparator (modelPersonaLess), same
	// stability — so entry i and Reviewers[i] describe the same row.
	sort.SliceStable(out, func(i, j int) bool {
		return modelPersonaLess(out[i].Model, out[i].Persona, out[j].Model, out[j].Persona)
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
//	error_handling · "error handling" · input_validation · "resource leak" ·
//	"api contract" · "out of scope" · out_of_scope
//
// Those are SPELLINGS of a member, not vocabulary drift, and each one would inflate the
// rate against MaxOutOfVocabularyRate. Separator and hyphenation folding belongs to the
// parse-boundary canonicalization epic and is deliberately out of scope here (folding
// reconcile.CategoryMerges() likewise — see OutOfVocabularyRate's doc).
//
// The feared consequence — that the first real run would trip the warning on a
// normalization artifact rather than on genuine drift — did NOT materialize: V1 emitted
// zero separator/hyphenation variants and zero merge-table words, which is what allowed
// the ceiling to be tightened to 0.05 rather than held loose against an artifact that
// never appeared. The hazard is latent, not retired: a future roster could emit these
// spellings where V1's did not. Diagnose a breach by inspecting the emitted words before
// treating the number as model behaviour — on the only evidence available it has meant
// malformed parser output, not a reviewer ignoring its prompt.
//
// Built per call rather than cached in a package var: reconcile.Categories()
// returns a fresh copy by design, and this runs once per rate computation — twice
// per run result since PerReviewerVocabulary joined OutOfVocabularyRate — not per
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
