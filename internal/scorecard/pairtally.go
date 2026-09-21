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
// Agreed and Disagreed count FINDINGS, not runs. Two reviewers on one merged
// finding agreed that the defect is real; reconcile.Merge stamps Disagreement
// on that same finding when they did not agree on its SEVERITY, which is what
// BuildDisagreements calls a severity_split. That split is the disagreement.
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
// WHAT THAT ANALOGY CANNOT SHOW, and it is not a small gap: a PAIR rate is a
// scarcer quantity than a single reviewer's rate. It needs both members
// eligible and in remit on the same run, so a pair accumulates cases strictly
// slower than either member accumulates runs — plausibly much slower for two
// narrow specialists, which is exactly the population AC 05-03 protects. 20 may
// well be too low (flagging a drop candidate off a thin sample) or too high
// (stranding every specialist pair as insufficient). Nothing here discriminates
// between those.
//
// RE-MEASUREMENT TRIGGER: redo this against the live store once 30+ distinct
// pairs have any eligible case at all, and record the measurement here the way
// DefaultTrustMinRuns' is recorded. Until then no caller may present a
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
// literal until then.
//
// REPORTING-ONLY (AC 05-04): crossing this threshold produces a flag on a value
// a human reads. It repoints nothing, disables no lens, and writes no registry.
const dropCandidateMaxRate = 0.05

// PairTally is one persona pair's cross-run disagreement record.
//
// Cases counts RUNS on which both members survived the trust filter chain —
// both got a fair attempt (eligibleOutcomeRuns) and both had their remit in
// play (opportunitySetRuns). It is the sample size the floor is applied to.
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
func PairKey(a, b string) (string, bool) {
	x := strings.ToLower(strings.TrimSpace(a))
	y := strings.ToLower(strings.TrimSpace(b))
	if x == "" || y == "" || x == y {
		return "", false
	}
	if x > y {
		x, y = y, x
	}
	return x + "|" + y, true
}

// PairDisagreements reads the scorecard store at dir and returns the per-pair
// disagreement tallies, keyed by PairKey.
//
// minRuns is accepted and threaded for symmetry with TrustPriors, so a caller
// holding one floor can pass it to both; the PAIR floor that decides
// sufficiency is minPairCases, per D6.
//
// The read is best-effort in exactly the way TrustPriors' is: a missing,
// unreadable or partially readable store yields an empty map and a nil error,
// never a failure for the caller. A partial read is treated as no data rather
// than folded, because a truncated store can only understate a pair's cases and
// understating cases is what turns a real pair into "insufficient data" — a
// quieter wrong answer than no answer.
//
// "Durable" here means deterministically re-derivable, NOT cached — read AC
// 05-01 Scenario 3 that way, as the AC itself instructs. There is no cache:
// this re-folds on every call exactly as trustPriorsSince does. Persisting a
// precomputed tally beside the records would be the second durable store the
// epic puts out of scope.
func PairDisagreements(dir string, minRuns int) (map[string]PairTally, error) {
	records, err := ReadSince(dir, 0, time.Now(), ReadOpts{Writer: io.Discard})
	if err != nil {
		return map[string]PairTally{}, nil
	}
	_ = minRuns
	return pairTallies(records), nil
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
// BOTH MEMBERS MUST SURVIVE THE RUN, and this is the subtle half. Each member
// carries its own copy of the pair's signal, so the fold could read either one.
// Reading the surviving side alone would score a relationship with a lens the
// eligibility gate had just removed from that run — crediting a pair for an
// exchange only one of them was present for. Requiring both also resolves the
// double-count: the signal is taken once, from the alphabetically first
// member's record, and the second member's copy is used only as proof of
// presence.
//
// A TALLY IS CREATED ONLY WHERE A FINDING WAS SHARED. Co-eligibility alone is
// not a pair (AC 05-01 Edge Case 2, AC 05-03 Edge Case 2): two lenses both in
// play on a case that neither connected on have produced no evidence about each
// other, and recording that as a zero-disagreement entry would be
// indistinguishable from "never disagrees" — which is the drop verdict. Silence
// is not tacit agreement.
//
// The input slice is never mutated.
func pairTallies(records []Record) map[string]PairTally {
	unions := opportunityUnions(records)
	kept := opportunitySetRuns(unresolvedEraRuns(mergeRoutedEras(eligibleOutcomeRuns(strictRuns(records)))), unions)

	// Who survived each run, and what each survivor said about its peers.
	// Presence is keyed lowercase to match every other reviewer key on this
	// path (trustPriorsSince's byReviewer, unresolvedEraRuns' era key).
	present := map[string]map[string]bool{}
	signals := map[string]map[string][]PairSignal{}
	for _, r := range kept {
		if r.RecordType != RecordTypeReviewer || r.PairEra == 0 {
			// PairEra == 0 is a record written before the pair signal existed.
			// Its absent slice is not a measured empty set and must never be
			// folded as one (AC 05-01 Edge Case 3).
			continue
		}
		name := strings.ToLower(strings.TrimSpace(r.Reviewer))
		if name == "" {
			continue
		}
		if present[r.RunID] == nil {
			present[r.RunID] = map[string]bool{}
			signals[r.RunID] = map[string][]PairSignal{}
		}
		present[r.RunID][name] = true
		signals[r.RunID][name] = append(signals[r.RunID][name], r.PairSignals...)
	}

	out := map[string]PairTally{}
	// Cases are counted per (pair, run) rather than per record: a reviewer that
	// ran under two models in one run yields two records, and both name the same
	// peer. Without this the pair would bank one case per record.
	countedCase := map[string]map[string]bool{}

	for runID, byName := range signals {
		for name, sigs := range byName {
			for _, s := range sigs {
				peer := strings.ToLower(strings.TrimSpace(s.Peer))
				key, ok := PairKey(name, peer)
				if !ok {
					continue // blank or self peer: fail closed, no key formed
				}
				if !present[runID][peer] {
					continue // the peer did not survive this run's gates
				}
				// Take the counts ONCE, from the alphabetically first member.
				// The peer's mirrored copy proves presence and nothing else.
				if !strings.HasPrefix(key, name+"|") {
					continue
				}
				t, seen := out[key]
				if !seen {
					a, b, _ := strings.Cut(key, "|")
					t = PairTally{A: a, B: b}
				}
				t.Agreed += s.Agreed
				t.Disagreed += s.Disagreed
				if countedCase[key] == nil {
					countedCase[key] = map[string]bool{}
				}
				if !countedCase[key][runID] {
					countedCase[key][runID] = true
					t.Cases++
				}
				out[key] = t
			}
		}
	}

	for key, t := range out {
		if t.Agreed+t.Disagreed == 0 {
			// Co-eligible but never connected: no evidence about each other.
			delete(out, key)
			continue
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
// WHAT THIS DOES NOT COVER, stated rather than left to be discovered: the
// gray_zone half of AC 05-01 Scenario 1. A gray-zone pair is two near-duplicate
// findings DBSCAN left unmerged, so its evidence is the AMBIGUOUS CLUSTER's
// membership — and EmitInput.AmbiguousFindings is flattened to member findings,
// losing exactly the cluster structure a pair needs. Recovering it means a new
// cluster-shaped EmitInput field, and feeding that stream into pair COUNTS
// would re-open TD-034's asymmetry in a new direction (an ambiguous finding
// currently moves no count by design). Filed as TD rather than guessed at here.
//
// The result is sorted by peer so two byte-identical runs serialize
// byte-identically; unsorted, a diff of the store reports Go's map iteration
// order as churn.
func reviewerPairSignals(name string, findings []Finding) []PairSignal {
	agreed := map[string]int{}
	disagreed := map[string]int{}
	for _, f := range findings {
		if !contains(f.Reviewers, name) {
			continue
		}
		for _, peer := range f.Reviewers {
			if _, ok := PairKey(name, peer); !ok {
				continue // blank peer, or the reviewer itself
			}
			if f.Disagreement != "" {
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
	peers := make([]string, 0, len(agreed)+len(disagreed))
	for p := range agreed {
		peers = append(peers, p)
	}
	for p := range disagreed {
		if _, both := agreed[p]; !both {
			peers = append(peers, p)
		}
	}
	sort.Strings(peers)
	out := make([]PairSignal, 0, len(peers))
	for _, p := range peers {
		out = append(out, PairSignal{Peer: p, Agreed: agreed[p], Disagreed: disagreed[p]})
	}
	return out
}
