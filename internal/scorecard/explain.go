package scorecard

import (
	"io"
	"time"
)

// The disposition-reason vocabulary is CLOSED and has exactly five members
// (sprint 36.0 C24; grown 3→5 by the TD-041 clarification of 2026-09-22). AC
// 06-05 pinned it closed so cli/personas.go's renderer can rely on a known,
// finite label set; it originally enumerated two members, C24 grew it to three
// for TD-032, and TD-041 grew it to five for the two exclusion causes the walk
// could not name — a non-strict consensus level and a superseded era. The
// property the AC protects is "closed and finite", not any particular
// cardinality, so the renderer's guarantee is unchanged.
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

	// ReasonConsensusNotStrict: the run's consensus level was not strict, so
	// strictRuns dropped the record before any eligibility question was asked.
	// The chain deliberately counts only strict runs (mixing levels lets one
	// exploratory run durably depress the priors), and TD-041's gap was that a
	// lens could not SEE that cause — the record simply vanished.
	ReasonConsensusNotStrict = "consensus-not-strict"

	// ReasonSupersededEra: the record was computed under a raised-denominator
	// definition older than (or unreadable to) the reviewer's newest, so
	// unresolvedEraRuns dropped it rather than blend definitions. Above-current
	// records — a newer atcr's output, a benchmark value, or a corrupt
	// hand-edit, indistinguishable here — are excluded by the same boundary and
	// carry this label too: from this binary's point of view their definition
	// has been superseded either way.
	ReasonSupersededEra = "superseded-era"

	// ReasonNotInOpportunitySet: the lens raised NOTHING on a run whose category
	// union was non-empty, discriminating, and entirely outside this lens's
	// remit, so opportunityDisposition dropped the record. A lens that raised
	// out-of-remit findings is NOT dropped — the gate narrows to raised-nothing
	// lenses (trust.go's opportunityDisposition and its raised-count test,
	// TestOpportunityDisposition_ZeroRaisedContributorIsDropped, are the
	// authority; keep this label coupled to them). This is the label behind "a
	// lens correctly silent on an out-of-remit case is neither credited nor
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

	// ReasonNotOpportunityScoped: the lens has no in-repo persona definition
	// (vera, pace, brad, archer, ronin per sprint-plan C9/C11), so
	// opportunityDisposition answers dispCounted for every one of its records
	// without ever consulting a remit — the lens is never opportunity-scoped,
	// never judged, and never dropped. The record is KEPT and annotated: this
	// label states the scope decision behind that pass-through (epic acceptance
	// criterion 7) instead of leaving the five lenses silently different from
	// the nine grounded ones. It is not an exclusion and must never join
	// ReasonExcludes — the chain did not drop anything.
	ReasonNotOpportunityScoped = "not opportunity-scoped: no in-repo persona definition"
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
	return []string{
		ReasonOutcomeIneligible,
		ReasonConsensusNotStrict,
		ReasonSupersededEra,
		ReasonNotInOpportunitySet,
		ReasonNoRecognizedCategory,
		ReasonNotOpportunityScoped,
	}
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
	case ReasonOutcomeIneligible, ReasonConsensusNotStrict, ReasonSupersededEra, ReasonNotInOpportunitySet:
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
	return detailsFromRecords(records, minRuns), nil
}

// detailsFromRecords is explainTrustPriorsSince's post-read body: the
// link-by-link walk, the per-record disposition notes and the floor, over an
// already-read record slice. Split out so TrustPriorsAndDetails can feed it the
// same records the rates fold reads, instead of re-reading the store.
func detailsFromRecords(records []Record, minRuns int) map[string]PersonaScoreDetail {

	// The chain is walked link by link rather than in one keptForTrust call
	// because the QUESTION here is which link dropped a record, and a single
	// call answers only whether one survived. Each step below invokes the SAME
	// function the production chain does, in the same order.
	//
	// THAT SAMENESS IS NOT SELF-ENFORCING and this comment used to claim it was
	// ("keptForTrust's by construction"). It is not: nothing stops an edit here
	// from dropping a link, and the 5.2.A review proved it by replacing
	// strictRuns(records) with records and watching the whole suite stay green.
	// It is pinned instead by
	// TestExplainTrustPriors_CountedExcludesEveryLinkTheChainDrops, which seeds a
	// non-strict, an ineligible and a superseded-era record and asserts the
	// counted total against keptForTrust's own output rather than against a
	// re-spelled chain expression.
	unions := opportunityUnions(records)
	afterStrict := strictRuns(records)
	afterOutcome := eligibleOutcomeRuns(afterStrict)
	// The chain's era boundary spelled out so the walk can attribute its drops
	// per record against the SAME input the era link sees: mergeRoutedEras and
	// scrubForgedCredit rewrite in place and never drop, so postScrub is the
	// exact population unresolvedEraRuns made its keep/drop decision over.
	postScrub := mergeRoutedEras(scrubForgedCredit(afterOutcome))
	afterEra := unresolvedEraRuns(postScrub)
	newestEra := newestEraByReviewer(postScrub)

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

	// The outcome gate, asked PER RECORD through the same outcomeEligible
	// predicate eligibleOutcomeRuns reads — not by diffing afterStrict against
	// afterOutcome.
	//
	// The diff is the obvious implementation and it is wrong, which the 5.2.A
	// review proved rather than argued. Two records identify identically under
	// any key built from the record's own fields (RunID plus reviewer name is the
	// only candidate — Record carries no unique id, and C15 refused to add one),
	// so when one run holds two records for a reviewer and only one is eligible,
	// the ineligible one's key is still present among the survivors and its
	// exclusion is never noted. Not hypothetical: reconcile.go derives a run id
	// as ReconciledAt + "-" + the review directory's basename, so two reconciles
	// of one directory inside the same second collide.
	//
	// Re-asking the predicate has no such failure mode, is cheaper than building
	// the key set, and is the reason outcomeEligible was extracted at all.
	//
	// The consensus boundary, asked PER RECORD through consensusLevelStrict —
	// the same predicate strictRuns applies. This closes TD-041's first gap: a
	// non-strict run is now NAMED rather than vanishing unexplained. scrub- and
	// merge- links cannot drop a record, so every afterOutcome record absent
	// from afterEra was dropped by the era rule — attributed below through
	// eraSuperseded, again the chain's own predicate.
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if !consensusLevelStrict(r) {
			note(r.Reviewer, ReasonConsensusNotStrict)
		}
	}

	for _, r := range afterStrict {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if !outcomeEligible(r) {
			note(r.Reviewer, ReasonOutcomeIneligible)
		}
	}

	// The era boundary, asked per record over postScrub — the exact input the
	// era link decided over. Closes TD-041's second gap: the older half of an
	// era-spanning reviewer is named instead of silently absent.
	for _, r := range postScrub {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if eraSuperseded(r, newestEra) {
			note(r.Reviewer, ReasonSupersededEra)
		}
	}

	// The opportunity gate, asked per record through the SAME predicate
	// opportunitySetRuns uses. This one cannot be diffed: dispUnscopeable and
	// dispCounted both survive, and telling them apart is the entire point of
	// TD-032's annotation.
	for _, r := range afterEra {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		// The unmapped statement comes FIRST, ahead of the era check: an
		// unmapped lens is never opportunity-scoped regardless of which schema
		// era its records were written under, and the annotation exists so the
		// explanation says so (epic acceptance criterion 7) rather than leaving
		// the five registry-only lenses silently different from the mapped nine.
		if _, mapped := remitFor(r.Reviewer); !mapped {
			note(r.Reviewer, ReasonNotOpportunityScoped)
			count(r.Reviewer)
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

	// The floor reuses the walk's own last link instead of calling
	// keptForTrust(records), which would re-run the whole chain AND
	// opportunityUnions a second time from scratch. The two are identical by
	// construction — keptForTrust is exactly this composition over the same
	// records — and TestExplainTrustPriors_CountedExcludesEveryLinkTheChainDrops
	// pins that by computing keptForTrust independently and comparing.
	//
	// It matters because cli/personas.go calls TrustPriors AND
	// ExplainTrustPriors, so `personas list --scores` was reading the store twice
	// and evaluating the filter chain three times over a store that is already
	// thousands of records and has no rotation.
	return applyExplainFloor(details, opportunitySetRuns(afterEra, unions), minRuns)
}

// applyExplainFloor keeps exactly the personas TrustPriors would key, so the two
// maps a renderer joins are never half-present.
//
// THE FLOOR IS APPLIED UNCONDITIONALLY, including when minRuns <= 0, and that is
// the fix for a real divergence rather than defensive tidying. details is
// accumulated from records the opportunity gate has not run on yet plus the
// outcome-gate notes, so a persona whose every record the chain later dropped
// still has an entry — at Counted 0 with its exclusions. An early return on
// minRuns <= 0 published that entry, and minRuns <= 0 is not a corner: it is the
// ONLY production call, cli/personas.go's ExplainTrustPriors(dir, 0). The 5.2.A
// review proved the consequences end to end — a lens whose every run was
// truncated rendered a row reading "0 counted · 2 excluded (outcome-ineligible)"
// underneath the footer "No scorecard data found", with a non-nil Detail beside
// a nil Rate, falsifying three doc comments and the two tests that forbid a
// fabricated zero.
//
// Membership is therefore "present in Aggregate(kept) with enough runs", which
// is TrustPriors' own rule read off TrustPriors' own inputs.
//
// THE COST IS REAL AND IS NOT A BUG: a lens whose every run was an
// infrastructure failure disappears from this surface entirely, which is exactly
// the lens (archer, vera) epic acceptance criterion 2 is written about. It is
// accepted because TrustPriors says nothing about that lens either — it is
// absent from the priors map and reverts to the neutral baseline — so "n/a" is
// the honest report of a lens atcr has no usable measurement of, and a row
// claiming otherwise beside an empty rate would be worse. Filed as TD-042.
//
// The floor is measured against row.Runs rather than a record count because that
// is the quantity TrustPriors compares to minRuns; counting records instead
// would agree on today's one-record-per-run store and diverge silently the day
// Aggregate groups differently.
//
// ADJUDICATED (2026-09-22 clarification): an ALL-INELIGIBLE lens gets NO row —
// absence is the report, never a fabricated zero. TD-042 records the display
// rule ("n/a" for a mapped lens with zero eligible records, never a numeric
// zero) and C24 closed the disposition-reason vocabulary; inventing a row shape
// here would reopen a settled decision. The skip below is that decision's
// implementation.
func applyExplainFloor(details map[string]PersonaScoreDetail, kept []Record, minRuns int) map[string]PersonaScoreDetail {
	runs := map[string]int{}
	for _, row := range Aggregate(kept) {
		runs[normalizeReviewerName(row.Reviewer)] += row.Runs
	}
	out := make(map[string]PersonaScoreDetail, len(details))
	for name, d := range details {
		n, ok := runs[name]
		if !ok || n < minRuns {
			// !ok covers the every-record-dropped persona at any minRuns; the
			// comparison covers the floor itself. minRuns <= 0 asks for no floor
			// and still requires presence, because TrustPriors requires it too.
			continue
		}
		// Reasons is handed out by value on the struct but the map header is
		// shared, so a caller mutating it would corrupt this result. Copy it,
		// matching the defensive-copy convention RemitCategories already follows.
		d.Reasons = copyReasons(d.Reasons)
		out[name] = d
	}
	return out
}

func copyReasons(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
