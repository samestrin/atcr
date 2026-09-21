package scorecard

import (
	"io"
	"time"
)

// The disposition-reason vocabulary is CLOSED and has exactly three members
// (sprint 36.0 C24). AC 06-05 pinned it closed so cli/personas.go's renderer can
// rely on a known, finite label set; it originally enumerated two members and
// C24 grew it to three for TD-032. The property the AC protects is "closed and
// finite", not "cardinality two", so the renderer's guarantee is unchanged.
//
// READ THE SPLIT BEFORE ADDING A FOURTH. Two of the three name a record that was
// DROPPED from the trust tally. The third names a record that was KEPT. They
// share one vocabulary because both answer the maintainer's question — "why does
// this lens's score rest on these cases?" — and a reader who assumes every label
// is an exclusion will mis-add the counts. ReasonExcludes is the predicate that
// tells them apart; Reasons' own doc comment states the invariant.
const (
	// ReasonOutcomeIneligible: the run's outcome was one of truncated,
	// incomplete, unparseable, failed or unknown, so the lens never got a fair
	// attempt and eligibleOutcomeRuns dropped the record. This is the label that
	// keeps archer's truncation history and vera's timeout history from
	// demoting either lens (epic acceptance criterion 2).
	ReasonOutcomeIneligible = "outcome-ineligible"

	// ReasonNotInOpportunitySet: the run's category union was non-empty and
	// discriminating, and none of its members fell inside this lens's remit, so
	// opportunitySetRuns dropped the record. This is the label behind "a lens
	// correctly silent on an out-of-remit case is neither credited nor
	// penalised" (epic acceptance criterion 1).
	ReasonNotInOpportunitySet = "category-not-in-opportunity-set"

	// ReasonNoRecognizedCategory: the record RAISED findings but contributed no
	// discriminating category to its run's union, so there is no evidence either
	// way about whether its remit was in play. The record is KEPT and charged —
	// it is excluded from opportunity SCOPING, not from the tally (TD-032).
	//
	// Without this the escape is self-exculpating: a lens whose findings carry
	// words the scorer does not recognise contributes nothing to the union, its
	// remit then matches nothing, and opportunitySetRuns deletes the very record
	// demoteByTrust needed. reconcile/category.go records a dry run in which
	// 72.3% of findings used a word the scorer did not recognise, so this is a
	// reachable state and not a theoretical one.
	//
	// It covers BOTH provenances of an empty contribution, because the escape is
	// identical either way: a category outside reclib.Categories() entirely, and
	// a category that is a real member but non-discriminating (invariant, other,
	// out-of-scope — see nonDiscriminating). Naming only the first would leave a
	// phantom-raiser that labels everything `invariant` with the same free pass.
	ReasonNoRecognizedCategory = "no-recognized-category"
)

// ScoreReasons returns the closed disposition-reason vocabulary, in a stable
// order. It is the single enumeration every renderer and test reads, so a fourth
// member is one edit rather than several that can diverge.
//
// TestScoreReasons_IsAClosedThreeMemberVocabulary pins both the membership and
// the count, so growing the set is a deliberate step with a failing test in
// front of it — which is what AC 06-05's "closed vocabulary, not a free-form
// string" actually requires.
func ScoreReasons() []string {
	return []string{ReasonOutcomeIneligible, ReasonNotInOpportunitySet, ReasonNoRecognizedCategory}
}

// ReasonExcludes reports whether a reason label names a record that was DROPPED
// from the trust tally, as opposed to one that was kept and annotated.
//
// It exists because Reasons mixes the two and a caller must not have to
// hard-code which is which — the renderer sums exclusions for its "excluded"
// column and must not fold TD-032's kept records into that total, which would
// report a lens as less-measured than it is.
func ReasonExcludes(reason string) bool {
	switch reason {
	case ReasonOutcomeIneligible, ReasonNotInOpportunitySet:
		return true
	default:
		return false
	}
}

// PersonaScoreDetail is one lens's answer to epic acceptance criterion 7 —
// "scores are explainable per lens: which cases counted, which were excluded and
// why". It is a COMPANION to the priors map, never merged into it: TrustPriors
// keeps returning map[string]float64 so reconcile/consensus.go's trustExempt and
// demoteByTrust need no call-site change (D3/D4).
//
// WHAT IT DOES NOT EXPLAIN, stated here rather than left for a reader to
// discover from a number that does not add up. The trust filter chain has four
// links that drop records, and this surface names only the two this sprint's
// SCORING added (the outcome gate and the opportunity gate). Records dropped by
// strictRuns (not measured at --consensus strict) and by unresolvedEraRuns (an
// older raised-denominator definition than the lens's newest) are era and
// consensus plumbing that predates this sprint, they are in no AC's reason
// vocabulary, and they are counted in NEITHER Counted nor Excluded. So a lens
// that ran forty times can legitimately report Counted 3 + Excluded 5. Filed as
// TD-041.
type PersonaScoreDetail struct {
	// Counted is the number of records that survived the whole chain and fed
	// this lens's rate.
	Counted int

	// Excluded is the number of records the outcome gate or the opportunity gate
	// dropped. It equals the sum of the Reasons entries whose label satisfies
	// ReasonExcludes — and NOT the sum of all Reasons entries, because
	// ReasonNoRecognizedCategory annotates a record that was kept.
	Excluded int

	// Reasons maps a member of ScoreReasons() to the number of this lens's
	// records it applied to. A label with no records is absent rather than
	// present at zero, matching TrustPriors' own absence-not-zero convention, so
	// a renderer can tell "never happened" from "measured zero".
	Reasons map[string]int
}

// ExplainTrustPriors reads the same store TrustPriors reads, over the same
// records and the same filter chain, and returns why each lens's rate rests on
// the cases it does. It takes TrustPriors' exact inputs (D4's one pinned name
// and signature — not TrustPriorsExplained, not any other alias).
//
// Membership matches TrustPriors exactly, and that is a contract rather than a
// coincidence: a lens below minRuns is ABSENT from both maps (AC 06-03's
// absence-not-zero rule), so a renderer joining the two never finds a rate
// without a detail or a detail without a rate. A lens absent from both renders
// "no data", never a fabricated zero.
//
// Like TrustPriors this is best-effort: a missing or unreadable store yields an
// empty map and a nil error.
func ExplainTrustPriors(dir string, minRuns int) (map[string]PersonaScoreDetail, error) {
	return explainTrustPriorsSince(dir, minRuns, 0, time.Now())
}

// explainTrustPriorsSince is ExplainTrustPriors' body with the read bounded the
// way trustPriorsSince bounds its own, so the two surfaces can never disagree
// about which month files they read.
func explainTrustPriorsSince(dir string, minRuns int, since time.Duration, now time.Time) (map[string]PersonaScoreDetail, error) {
	records, err := ReadSince(dir, since, now, ReadOpts{Writer: io.Discard})
	if err != nil {
		// Fail neutral on a truncated store, exactly as trustPriorsSince does:
		// an explanation computed from a partial read would name cases that are
		// missing for an I/O reason and call it a scoring decision.
		return map[string]PersonaScoreDetail{}, nil
	}

	// The chain is walked link by link rather than in one keptForTrust call
	// because the QUESTION here is which link dropped a record, and a single
	// call answers only whether one survived. Each step below invokes the SAME
	// function the production chain does, in the same order, so the survivor set
	// this ends on is keptForTrust's by construction — pinned by
	// TestExplainTrustPriors_WalksExactlyTheProductionChain.
	unions := opportunityUnions(records)
	afterStrict := strictRuns(records)
	afterOutcome := eligibleOutcomeRuns(afterStrict)
	afterEra := unresolvedEraRuns(mergeRoutedEras(scrubForgedCredit(afterOutcome)))

	details := map[string]PersonaScoreDetail{}
	// note records one reason against one persona, creating the Reasons map
	// lazily so a label that never fired stays ABSENT rather than present at
	// zero — the same absence-not-zero rule TrustPriors applies to a below-floor
	// lens, and what lets a renderer tell "never happened" from "measured zero".
	note := func(reviewer, reason string) {
		key := normalizeReviewerName(reviewer)
		d := details[key]
		if d.Reasons == nil {
			d.Reasons = map[string]int{}
		}
		d.Reasons[reason]++
		if ReasonExcludes(reason) {
			d.Excluded++
		}
		details[key] = d
	}
	count := func(reviewer string) {
		key := normalizeReviewerName(reviewer)
		d := details[key]
		d.Counted++
		details[key] = d
	}

	// The outcome gate, diffed against its own input. Records strictRuns already
	// dropped are not in afterStrict and are therefore never attributed — see
	// PersonaScoreDetail's note on what this surface does not explain (TD-041).
	survived := reviewerKeys(afterOutcome)
	for _, r := range afterStrict {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if _, ok := survived[recordKey(r)]; !ok {
			note(r.Reviewer, ReasonOutcomeIneligible)
		}
	}

	// The opportunity gate, asked per record through the SAME predicate
	// opportunitySetRuns uses. This one cannot be diffed: dispUnscopeable and
	// dispInRemit both survive, and telling them apart is the entire point of
	// TD-032's annotation.
	for _, r := range afterEra {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if r.SchemaVersion < categoriesRaisedSinceSchema {
			// Pre-era: carries no CategoriesRaised, so it is never judged on the
			// absence. It counts, unexplained and unannotated.
			count(r.Reviewer)
			continue
		}
		switch opportunityDisposition(r, unions[r.RunID]) {
		case dispOutOfRemit:
			note(r.Reviewer, ReasonNotInOpportunitySet)
		case dispUnscopeable:
			note(r.Reviewer, ReasonNoRecognizedCategory)
			count(r.Reviewer)
		default:
			count(r.Reviewer)
		}
	}

	return applyExplainFloor(details, keptForTrust(records), minRuns), nil
}

// applyExplainFloor drops every persona below the caller's minRuns so
// ExplainTrustPriors' membership matches TrustPriors' exactly.
//
// The floor is computed from Aggregate over the SAME surviving records
// trustPriorsSince aggregates, and against row.Runs rather than a record count,
// because that is the quantity TrustPriors compares to minRuns. Counting
// records instead would agree on today's one-record-per-run store and diverge
// silently the day Aggregate groups differently — and the divergence would show
// up as a persona with a rate and no explanation, or the reverse.
func applyExplainFloor(details map[string]PersonaScoreDetail, kept []Record, minRuns int) map[string]PersonaScoreDetail {
	if minRuns <= 0 {
		return details
	}
	runs := map[string]int{}
	for _, row := range Aggregate(kept) {
		runs[normalizeReviewerName(row.Reviewer)] += row.Runs
	}
	out := make(map[string]PersonaScoreDetail, len(details))
	for name, d := range details {
		if runs[name] < minRuns {
			continue
		}
		out[name] = d
	}
	return out
}

// recordKey identifies one reviewer record for the diff above. RunID is not
// collision-proof (TD-031) and the pair is not unique if a run ever emits two
// records for one reviewer, so a collision merges two records' explanations.
// That is acceptable HERE and only here: this surface reports, it never feeds
// the rate, and the alternative — a synthetic id on the persisted record — is
// the shape change C15 refused.
func recordKey(r Record) string {
	return r.RunID + "\x00" + normalizeReviewerName(r.Reviewer)
}

func reviewerKeys(records []Record) map[string]struct{} {
	keys := make(map[string]struct{}, len(records))
	for _, r := range records {
		if r.RecordType == RecordTypeReviewer {
			keys[recordKey(r)] = struct{}{}
		}
	}
	return keys
}
