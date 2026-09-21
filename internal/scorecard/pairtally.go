package scorecard

import (
	"io"
	"sort"
	"strings"
	"time"
)

// The per-pair disagreement surface (sprint 36.0 Story 05).
//
// It answers ONE question, the one the live registry already asks in prose
// beside penny: "penny duplicates pace's remit — if the two never disagree,
// drop one." This file makes that a number.
//
// DISAGREEMENT IS THE EVIDENCE, NOT AGREEMENT. A pair that always agrees is a
// pair carrying one voice twice; a pair that argues is two independent lenses.
// So a LOW rate flags a drop candidate and a HIGH rate protects both members —
// the inverse of how a naive quality score would read the same numbers, and the
// reason AC 05-03 spends an entire criterion on it.
//
// IT IS REPORTING-ONLY (AC 05-04). Nothing here writes, and nothing here feeds
// TrustPriors: the fold reads the same records trustPriorsSince does and
// returns a separate value. A future edit that wires a tally back into an
// individual rate fails TestPairTallies_DoNotAlterIndividualTrustRates.
//
// EVERY UNATTRIBUTABLE SIGNAL IS DISCARDED RATHER THAN SPREAD. That rule is
// applied three times below (a 3+-reviewer split, a duplicate record, a peer
// name that cannot form a key) and it is the same rule trust.go applies to an
// unrecognized consensus level and an out-of-vocabulary category: excluding
// evidence only forgoes data, while apportioning it invents a durable number
// nobody measured.

// PairSignal is one co-reviewer relationship observed on a single run, carried
// on the record of the reviewer it belongs to.
//
// IT EXISTS BECAUSE THE PAIR DATA IS OTHERWISE THROWN AWAY (TD-030). The
// co-reviewer identity lives in EmitInput.Findings[].Reviewers for the duration
// of one Emit call and reaches no persisted field: Record.CategoriesRaised says
// which topics a lens touched, never who else was on the finding, and
// scorecard.Finding is never persisted at all. Without this field the tally
// could only be rebuilt by walking every review directory's
// reconciled/disagreements.json at query time — the second full-store read D2
// rules out, and the second durable store the epic lists as out of scope.
//
// Agreed and Disagreed count FINDINGS, not runs. Peer is stored already
// normalized (lower-cased, trimmed), so the fold never has to re-derive an
// identity the writer could have settled.
type PairSignal struct {
	Peer      string `json:"peer"`
	Agreed    int    `json:"agreed"`
	Disagreed int    `json:"disagreed"`
}

// PairEraCurrent is the pair-signal measurement era this binary writes, and it
// is the whole reason C15 could close TD-030 without a SchemaVersion bump.
//
// An absent pair_signals key is byte-identical on two very different records:
// one written before this field existed, and one whose run genuinely produced
// no pair. omitempty makes them the same bytes. Scored alike, the entire
// pre-4a back-catalogue would read as "these lenses never co-occurred" — which
// is the drop-candidate verdict, applied to every pair in the store.
//
// So the marker is stamped UNCONDITIONALLY on every reviewer record this
// emitter writes, including a clean run with no pairs at all. Its presence, not
// the signal slice's, is what says "this record was measured". That follows
// RaisedDenominator's documented pattern (scorecard.go) rather than
// categoriesRaisedSinceSchema's: a SchemaVersion-keyed constant cannot
// discriminate WITHIN v2, and both eras here are v2.
//
// ABOVE-CURRENT IS EXCLUDED, not clamped, exactly as unresolvedEraRuns excludes
// an above-current RaisedDenominator. A record stamped era 2 was measured under
// a rule this binary does not implement, and a future era-2 record still
// carries schema_version 2, so the store's read gate admits it — nothing but
// this check stops it blending with era-1 evidence. Whoever adds era 2 has to
// come back here and decide the mixing rule rather than inherit a silent blend.
//
// The literal is pinned by TestPairSignals_DoNotBumpTheSchemaVersion, which
// asserts SchemaVersion is still the literal 2.
const PairEraCurrent = 1

// minPairCases is the single minimum-eligible-case floor for the WHOLE pair
// surface (D6). AC 04-05's Edge Case 2 defers to it; Story 4 introduces no
// second floor.
//
// IT IS PROVISIONAL, AND THE MEASUREMENT BEHIND DefaultTrustMinRuns WAS NOT
// REDONE FOR IT. Measured 2026-09-20: there is no scorecard store on this
// machine at all (~/.config/atcr/scorecard/ does not exist), so PairDisagreements
// returns an empty map today and no live pair has ever been observed. The value
// is therefore ADOPTED BY ANALOGY from DefaultTrustMinRuns = 20, whose own
// 2026-07-29 analysis found per-reviewer rates unstable at 10 summed runs and
// converged by 20.
//
// THE ANALOGY IS APPLIED TO THE SAME KIND OF QUANTITY, and that is deliberate:
// PairTally.Cases counts runs on which both members were CO-ELIGIBLE, not runs
// on which they happened to connect. DefaultTrustMinRuns counts a lens's runs
// of opportunity; this counts a pair's. An earlier draft counted only runs with
// a shared finding, which is strictly scarcer than anything the analogy
// describes and would have made 20 a much harsher floor than the same number
// means for a single lens.
//
// WHAT THE ANALOGY STILL CANNOT SHOW: a pair accumulates co-eligible runs more
// slowly than either member accumulates runs, and how much more slowly depends
// on how far apart the two remits sit — plausibly much slower for two narrow
// specialists, which is exactly the population AC 05-03 protects. 20 may still
// be too low (flagging off a thin sample) or too high (stranding every
// specialist pair as insufficient). Nothing here discriminates between those.
//
// RE-MEASUREMENT TRIGGER: redo this against the live store once 30+ distinct
// pairs have any co-eligible case at all, and record the measurement here the
// way DefaultTrustMinRuns' is recorded. Until then no caller may present a
// sufficient/insufficient verdict as an evidence-backed one.
// TestMinPairCases_NotNarrowedWithoutRemeasurement pins the literal so a later
// narrowing cannot ride in without that measurement.
const minPairCases = 20

// dropCandidateMaxRate is the at-or-below disagreement rate at which a pair is
// REPORTED as a drop candidate. Strictly above it, a pair is never flagged (AC
// 05-02 Edge Case 1 — at-or-below, not "near").
//
// IT IS PROVISIONAL FOR THE SAME REASON minPairCases is: no store, no measured
// pair, nothing to derive from. 0.05 encodes a claim that can at least be
// stated plainly — a pair that splits on severity fewer than one time in twenty
// shared findings is carrying one voice twice — but it is a stated claim, not a
// measured threshold, and the epic's own constraint forbids guessing a
// constant. It is shipped explicitly provisional rather than silently.
//
// WHAT THE VALUE CANNOT SHOW: the distribution of real pair rates is unknown,
// so it is not known whether 0.05 separates anything. If genuine duplicates and
// genuine independents both cluster above it, the flag never fires and the
// penny test is decorative; if both cluster below, it fires on every pair. Both
// failures are invisible without the measurement.
//
// RE-MEASUREMENT TRIGGER: once 30+ pairs clear minPairCases, plot their rates
// and set this at the separation the data actually shows — the way
// defaultTrustWindow was set from the 2026-07-31 store rather than from
// intuition. TestDropCandidateMaxRate_NotNarrowedWithoutRemeasurement pins the
// literal, and TestDropCandidate_ExactlyAtThresholdIsFlagged pins the boundary
// itself so the at-or-below comparison cannot be narrowed to a strict one.
//
// REPORTING-ONLY (AC 05-04): crossing this threshold produces a flag on a value
// a human reads. It repoints nothing, disables no lens, and writes no registry.
const dropCandidateMaxRate = 0.05

// PairTally is one persona pair's cross-run disagreement record.
//
// Cases counts RUNS on which both members survived the trust filter chain —
// both got a fair attempt (eligibleOutcomeRuns) and both had their remit in
// play (opportunitySetRuns). It is the pair's OPPORTUNITY, and it is the
// quantity minPairCases is a floor on, so the floor means for a pair what
// DefaultTrustMinRuns means for a lens.
//
// Agreed and Disagreed count FINDINGS across those runs, so the rate is
// computed from the evidence and the floor from the opportunity. A pair can
// therefore be plentiful in findings and still insufficient in cases, which is
// the honest reading: twenty splits inside two runs say little about a pair.
type PairTally struct {
	A             string
	B             string
	Agreed        int
	Disagreed     int
	Cases         int
	Sufficient    bool
	DropCandidate bool
}

// DisagreementRate is the share of the pair's shared findings they split on.
//
// Zero shared findings yields 0, matching ratio()'s zero-denominator
// convention — but a tally with no shared findings is never CONSTRUCTED (see
// pairTallies), so that branch is defensive rather than a reachable reading.
// The distinction matters: a returned 0 always means "measured, never split",
// never "no data". No-data is the absence of the key.
func (p PairTally) DisagreementRate() float64 {
	return ratio(p.Disagreed, p.Agreed+p.Disagreed)
}

// pairKeySep joins the two members of a pair key. It is named because the
// delimiter is an INVARIANT on the member names, not merely a formatting
// choice: a name containing it would make two distinct pairs collide on one
// key. reconcile.distinctReviewers enforces the same kind of invariant for its
// own comma-joined cell, and for the same reason.
const pairKeySep = "|"

// PairKey normalizes two persona names into the one canonical key both
// orderings collapse to: lowercase, alphabetically ordered, joined with "|".
//
// It returns ok == false rather than a key for every input that cannot name a
// real pair, and each rejection closes a distinct silent failure:
//
//   - An EMPTY or whitespace-only member would form "|pace" or "pace|", a key
//     that accumulates every malformed record in the store into one phantom
//     pair. Fail closed, matching soloItem's skip-malformed convention.
//   - A SELF-PAIR ("bruce" with "bruce") would tally a lens against itself,
//     which by construction never disagrees — so it would be flagged as a drop
//     candidate the moment it cleared the floor, recommending that a lens be
//     dropped for duplicating itself.
//   - A MEMBER CONTAINING "|" would merge two different pairs into one tally:
//     PairKey("a|b", "c") and PairKey("a", "b|c") both produce "a|b|c", and
//     their evidence then sums under one key naming neither. Reviewer names are
//     source-derived (a registry entry, a pool summary, a findings cell), so
//     this is not provably unreachable and is rejected rather than assumed away.
func PairKey(a, b string) (string, bool) {
	x := strings.ToLower(strings.TrimSpace(a))
	y := strings.ToLower(strings.TrimSpace(b))
	if x == "" || y == "" || x == y {
		return "", false
	}
	if strings.Contains(x, pairKeySep) || strings.Contains(y, pairKeySep) {
		return "", false
	}
	if x > y {
		x, y = y, x
	}
	return x + pairKeySep + y, true
}

// PairDisagreements reads the scorecard store at dir and returns the per-pair
// disagreement tallies, keyed by PairKey.
//
// It takes NO minRuns. An earlier signature accepted one "for symmetry with
// TrustPriors" and discarded it, which is worse than not offering it: a caller
// passing a floor would have been silently ignored. The pair floor is
// minPairCases and it is not caller-configurable, matching defaultTrustWindow's
// own reasoning about reopening internal filter constants as flags.
//
// The read is best-effort in exactly the way TrustPriors' is: a missing or
// unreadable store yields an empty map and a nil error, never a failure for the
// caller. BE PRECISE ABOUT WHAT "unreadable" COVERS, because the two levels
// behave differently: a whole-file IO failure fails neutral here (empty map),
// while ReadRecords logs and SKIPS an individual malformed or forward-version
// line and reports no error at all. So a partially corrupt month file IS folded
// with those lines missing, and losing a disagreement-bearing run can flip a
// pair toward DropCandidate. That is inherited from the store's read contract
// rather than chosen here, and it is stated so a reader does not take the
// file-level guarantee for a line-level one.
//
// "Durable" here means deterministically re-derivable, NOT cached — read AC
// 05-01 Scenario 3 that way, as the AC itself instructs. There is no cache:
// this re-folds on every call exactly as trustPriorsSince does. Persisting a
// precomputed tally beside the records would be the second durable store the
// epic puts out of scope.
func PairDisagreements(dir string) (map[string]PairTally, error) {
	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	if err != nil {
		return map[string]PairTally{}, nil
	}
	return pairTallies(records), nil
}

// pairEvidence is one pair's counts on one run, before they are summed across
// runs.
type pairEvidence struct {
	agreed    int
	disagreed int
}

// pairTallies folds records into per-pair tallies.
//
// IT RUNS THE SAME FILTER CHAIN trustPriorsSince DOES, and that is the point
// rather than a convenience. A pair signal read off raw records would re-admit
// through the pair surface every run the trust gates just excluded: archer's
// truncated runs, vera's timed-out ones, and every out-of-remit case a narrow
// lens was correctly silent on. Those are the exact exclusions this sprint
// exists to make, so the pair tally has to inherit them.
//
// BOTH MEMBERS MUST SURVIVE THE RUN. Each member carries its own mirrored copy
// of the pair's signal, so the fold could read either. Reading one alone would
// score a relationship with a lens the eligibility gate had just removed from
// that run — crediting a pair for an exchange only one of them was present for.
//
// THE TWO COPIES ARE COMBINED BY MAX PER RUN, NOT SUMMED, AND NOT TAKEN FROM A
// FIXED SIDE. Summing double-counts every shared finding. Taking the
// alphabetically-first member's copy (an earlier draft did) silently discarded
// the whole pair's evidence whenever that member's record carried none — which
// is reachable, not hypothetical, since a reviewer can hold more than one
// record per run. Max is the reconciliation that survives both: the copies are
// written from the same finding set and agree in the ordinary case, so max
// changes nothing there and recovers the evidence when one side is missing it.
// It also absorbs a reviewer appearing twice in one run (two models, or a
// re-emitted RunID), which a sum would double.
//
// A TALLY IS CREATED ONLY WHERE A FINDING WAS SHARED. Co-eligibility alone is
// not a pair (AC 05-01 Edge Case 2, AC 05-03 Edge Case 2): two lenses both in
// play on a case that neither connected on have produced no evidence about each
// other, and recording that as a zero-disagreement entry would be
// indistinguishable from "never disagrees" — which is the drop verdict. Silence
// is not tacit agreement. Cases, by contrast, is counted over CO-ELIGIBILITY,
// because the floor is a question about opportunity rather than about evidence.
//
// COST: one pass over records, then one intersection per pair that actually has
// evidence. It is never O(personas^2 x records) — no step enumerates candidate
// pairs, only observed ones, which for the 13-lens roster is at most ~78.
//
// The input slice is never mutated.
func pairTallies(records []Record) map[string]PairTally {
	unions := opportunityUnions(records)
	kept := opportunitySetRuns(unresolvedEraRuns(mergeRoutedEras(eligibleOutcomeRuns(strictRuns(records)))), unions)

	// runsByPersona answers "was this lens in play on this run", which is what
	// Cases counts. evidence answers "what did this pair say about each other",
	// keyed (pairKey, runID) so the two mirrored copies reconcile per run.
	runsByPersona := map[string]map[string]bool{}
	evidence := map[string]map[string]pairEvidence{}

	for _, r := range kept {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		// PairEra == 0 is a record written before the pair signal existed: its
		// absent slice is not a measured empty set (AC 05-01 Edge Case 3).
		// Above-current is a record measured under a rule this binary does not
		// implement. Both are excluded rather than read as evidence.
		if r.PairEra == 0 || r.PairEra > PairEraCurrent {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(r.Reviewer))
		if name == "" {
			continue
		}
		if runsByPersona[name] == nil {
			runsByPersona[name] = map[string]bool{}
		}
		runsByPersona[name][r.RunID] = true

		for _, s := range r.PairSignals {
			key, ok := PairKey(name, s.Peer)
			if !ok {
				continue // blank, self, or delimiter-bearing peer: no key formed
			}
			if evidence[key] == nil {
				evidence[key] = map[string]pairEvidence{}
			}
			cur := evidence[key][r.RunID]
			// Max, not +=. See the "TWO COPIES" note above.
			if s.Agreed > cur.agreed {
				cur.agreed = s.Agreed
			}
			if s.Disagreed > cur.disagreed {
				cur.disagreed = s.Disagreed
			}
			evidence[key][r.RunID] = cur
		}
	}

	out := map[string]PairTally{}
	for key, byRun := range evidence {
		a, b, _ := strings.Cut(key, pairKeySep)
		t := PairTally{A: a, B: b}
		for runID, e := range byRun {
			// Both members must have survived THIS run, or the evidence is a
			// one-sided account of an exchange the gate already excluded.
			if !runsByPersona[a][runID] || !runsByPersona[b][runID] {
				continue
			}
			t.Agreed += e.agreed
			t.Disagreed += e.disagreed
		}
		if t.Agreed+t.Disagreed == 0 {
			// Never connected on a run they both survived: no evidence about
			// each other, so no entry at all.
			continue
		}
		for runID := range runsByPersona[a] {
			if runsByPersona[b][runID] {
				t.Cases++
			}
		}
		t.Sufficient = t.Cases >= minPairCases
		// Insufficient data is never a drop candidate AND never a confident
		// non-candidate — the caller reads Sufficient to tell those apart.
		t.DropCandidate = t.Sufficient && t.DisagreementRate() <= dropCandidateMaxRate
		out[key] = t
	}
	return out
}

// reviewerPairSignals builds one reviewer's per-peer tallies for a single run,
// from the findings it participated in.
//
// AGREEMENT IS CO-OCCURRENCE ON A MERGED FINDING; DISAGREEMENT IS A SEVERITY
// SPLIT ON ONE. reconcile.Merge stamps Finding.Disagreement ("<lo> vs <hi>")
// whenever a cluster's members did not agree on severity, and
// BuildDisagreements keys KindSeveritySplit off exactly that field. The merged
// Severity is the MAX and so cannot reveal a split on its own, which is why
// Disagreement is threaded beside it rather than instead of it.
//
// A SPLIT IS ONLY ATTRIBUTABLE WHEN THE CLUSTER HELD EXACTLY TWO REVIEWERS, and
// getting this wrong is the defect the 4.2 adversarial pass caught. Disagreement
// is a property of the WHOLE GROUP — reconcile.MergeSeverity sets it when the
// group's severities span more than one value — so on a three-reviewer cluster
// where two said HIGH and one said LOW, charging every pair a split invents two
// disagreements that never happened AND deletes the one real agreement. It
// inflates every multi-reviewer pair's rate, which systematically SUPPRESSES the
// drop-candidate flag this whole surface exists to raise.
//
// The per-reviewer severities are genuinely unrecoverable after a merge —
// reconcile.Position says so in its own comment, which is why Positions is
// populated only for gray-zone clusters. So a 3+-reviewer split contributes
// NOTHING: not a disagreement, because nobody can say between whom, and not an
// agreement either, because the group demonstrably did not agree. Discarding it
// forgoes data; apportioning it would fabricate a durable number.
//
// WHAT THIS DOES NOT COVER, stated rather than left to be discovered: the
// gray_zone half of AC 05-01 Scenario 1. A gray-zone pair is two near-duplicate
// findings DBSCAN left unmerged, so its evidence is the AMBIGUOUS CLUSTER's
// membership — and EmitInput.AmbiguousFindings is flattened to member findings,
// losing exactly the cluster structure a pair needs. Recovering it means a new
// cluster-shaped EmitInput field, and feeding that stream into pair COUNTS
// would re-open TD-034's asymmetry in a new direction (an ambiguous finding
// currently moves no count by design). Filed as TD rather than guessed at here.
// Note the two gaps point the same way: both discard evidence, and the
// drop-candidate flag is the thing that goes un-raised, never wrongly raised.
//
// Peer names are TRIMMED, LOWER-CASED AND DEDUPED per finding, matching
// distinctCount's documented reasoning at the same layer (Emit is exported, so
// a caller's reviewer list is untrusted input): without it, " dax" and "dax" on
// one finding produce two entries that the fold then collapses into one key
// with the counts summed, inflating Agreed for a single shared finding — and an
// inflated Agreed drives the rate DOWN, toward a false drop-candidate flag.
//
// The result is sorted by peer so two byte-identical runs serialize
// byte-identically; unsorted, a diff of the store reports Go's map iteration
// order as churn.
func reviewerPairSignals(name string, findings []Finding) []PairSignal {
	self := strings.ToLower(strings.TrimSpace(name))
	if self == "" {
		return nil
	}
	agreed := map[string]int{}
	disagreed := map[string]int{}

	for _, f := range findings {
		// The participation test uses the same normalized identity the fold
		// keys on. An exact-string test (an earlier draft used one) misses a
		// reviewer whose map key and findings cell differ only in case, and the
		// mirrored copy on the peer's record is then the only witness.
		peers := distinctPeers(f.Reviewers)
		if !peers[self] {
			continue
		}
		split := f.Disagreement != ""
		if split && len(peers) != 2 {
			continue // unattributable; see the note above
		}
		for peer := range peers {
			if _, ok := PairKey(self, peer); !ok {
				continue // blank, self, or delimiter-bearing
			}
			if split {
				disagreed[peer]++
				continue
			}
			agreed[peer]++
		}
	}

	if len(agreed)+len(disagreed) == 0 {
		// nil, not an empty slice: omitempty then omits the key entirely and a
		// no-pair run serializes exactly as a pre-4a record did.
		return nil
	}
	seen := map[string]bool{}
	peers := make([]string, 0, len(agreed)+len(disagreed))
	for _, m := range []map[string]int{agreed, disagreed} {
		for p := range m {
			if !seen[p] {
				seen[p] = true
				peers = append(peers, p)
			}
		}
	}
	sort.Strings(peers)
	out := make([]PairSignal, 0, len(peers))
	for _, p := range peers {
		out = append(out, PairSignal{Peer: p, Agreed: agreed[p], Disagreed: disagreed[p]})
	}
	return out
}

// distinctPeers normalizes one finding's reviewer cell into the identity set the
// pair surface keys on: trimmed, lower-cased, deduped, blanks dropped.
//
// It is deliberately NOT distinctCount's fallback-collapsing notion of
// distinctness. That one asks "how many independent voices" and merges two
// personas served by one fallback model; this one asks "which lenses were on
// this finding", and merging two named lenses into one would delete the pair
// they form. The two answer different questions over the same slice.
func distinctPeers(reviewers []string) map[string]bool {
	out := make(map[string]bool, len(reviewers))
	for _, r := range reviewers {
		n := strings.ToLower(strings.TrimSpace(r))
		if n == "" {
			continue
		}
		out[n] = true
	}
	return out
}
