package scorecard

import (
	"io"
	"strings"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"
)

// DefaultTrustMinRuns is the conservative minimum-run floor a reviewer must
// clear before its corroboration rate is trusted by a caller that does not
// pick its own minRuns. Live-store analysis (2026-07-29) showed rates were
// still unstable at 10 summed runs but converged by 20; core reviewers already
// run 100+ runs, so this floor does not strand real reviewers.
//
// KNOWN LIMIT — the floor is now applied to a DIFFERENT QUANTITY than the one it
// was measured against, and that has not been re-measured. The 2026-07-29
// analysis counted unfiltered strict runs. opportunitySetRuns then removes every
// run where a lens's remit was not in play, and Aggregate increments Runs per
// record, so a narrow lens now needs proportionally MORE strict runs to clear a
// floor its broader peers clear at 20 — and absence from the priors map is not
// neutral (it disables demoteByTrust as well as trustExempt), so the fairness fix
// can strip trust scoring from exactly the specialists it was written to protect.
//
// It is not corrected here because correcting it means either inventing a
// constant, which this epic's constraints forbid, or re-measuring against a live
// store, and there is no store on the machine this was written on. The exposure
// is nil today for that same reason: an empty store strands nobody. Tracked as
// TD-025 — re-measure the floor against in-remit run counts once a real store
// exists, and record the measurement here the way the one above is recorded.
const DefaultTrustMinRuns = 20

// defaultTrustWindow bounds the reconcile-side trust-prior read (epic 35.11).
// It is deliberately NOT caller-configurable: epic 35.6 restrained exposing
// internal filter constants and epic 35.9 hardcoded trustHighThreshold /
// trustLowThreshold for the same reason, so a --trust-since flag would reopen
// exactly that surface.
//
// 180d is set from measurement, not intuition. The window interacts with
// DefaultTrustMinRuns: a reviewer whose runs INSIDE the window fall below that
// floor is dropped from the priors map, which would silently disable trust
// exemption and demotion on all four RunReconcile paths — a behavior change, not
// a speedup. Against the live store on 2026-07-31 (span 2026-06-26 to
// 2026-07-31, 1260 reviewer records) every one of the 11 reviewers clearing the
// floor held 113-120 strict runs at every candidate window from 30d to 365d, so
// 180d strands nobody with a wide margin. The leaderboard's 30d display default
// is far too aggressive for this purpose and is unrelated.
//
// WHAT THAT MEASUREMENT CANNOT YET SHOW: the store was only ~35 days old, i.e.
// younger than every candidate window, so every window trivially covered all of
// it. The numbers prove 180d is safe TODAY; they do not discriminate between
// window sizes, and no reviewer has yet been observed aging out of one. The
// measurement is worth redoing once the store spans more than 180 days — and
// again once it holds post-35.9.1 history, since strictRuns drops non-strict
// runs and a reviewer used mostly under `--consensus lenient/off` could hold
// fewer than DefaultTrustMinRuns STRICT runs inside the window even while
// running constantly. Every record measured above predates consensus_level and
// therefore counted strict by the legacy default, so that compounding is
// untested.
//
// KNOWN LIMITATION (accepted): a reviewer with no runs in any month file
// overlapping the last 180d — monthOverlapsWindow includes the whole calendar
// month holding the cutoff, so effective retention is 180d plus up to a month,
// roughly 210d in the worst case — drops
// out of the priors map and reverts to the no-history state — the same state a
// brand-new reviewer occupies (reconcile/consensus.go does a plain map lookup
// with no distinct "dormant" handling). That state is not NEUTRAL: absence
// disables demotion as well as exemption, so a dormant phantom-raiser stops
// being demoted. See the note on unresolvedEraRuns below, which says the same
// thing about its own era gap. Re-widening this constant, not an
// empty-map fallback, is the fix if that ever bites: the map stays non-empty
// while any reviewer is active, so a fallback keyed on emptiness would never
// fire for a single dormant reviewer.
//
// The prefer-newest era pass (unresolvedEraRuns) drops a reviewer's whole
// pre-upgrade window the moment its first record under a newer
// raised_denominator lands, which would put the reviewer under
// DefaultTrustMinRuns and silently disable trustExempt and demoteByTrust for it.
// For the 2-to-3 bump that blackout bought nothing — the two eras partition the
// same finding set, so the trust rate is identical either way — and
// mergeRoutedEras now normalises era 3 to era 2 before the pass, removing it. A
// FUTURE denominator bump that genuinely changes the measured set would
// reintroduce the blackout for its own window, and the same question (are the
// two eras arithmetically equivalent for THIS denominator?) has to be answered
// again before extending mergeRoutedEras to it.
//
// Narrowing this value requires redoing the min-runs measurement above
// (TestDefaultTrustWindow_NotNarrowedWithoutRemeasurement pins the constant
// against its own literal, so it can only catch a deliberate narrowing — it
// cannot detect that 180d stopped being generous as the store ages).
// TestDefaultTrustWindow_IsGenerousEnoughForTheMinRunsFloor is its behavioral
// complement: it seeds a literal 20 strict runs at a literal 9-day stride and
// fails if this window or DefaultTrustMinRuns is moved against the other. Both
// literals are deliberate — derived from the constants, the test would contract
// with them and pass at any value.
const defaultTrustWindow = 180 * 24 * time.Hour

// isolatedFindingWeight is the credit a finding earns when exactly one lens
// raised it. Every other finding earns 1/distinctCount(Reviewers), so this is
// the top of that curve.
//
// IT IS PROVISIONAL, and 1.0 is the value that asserts the LEAST. The epic
// forbids guessing a constant, and every value above 1.0 would be a guess about
// how much more a solo find is worth than the 1/N curve already says — a claim
// with no measurement behind it. 1.0 makes the solo case the curve's natural
// maximum and adds nothing on top. The multiplier is applied in reviewerCounts
// regardless, so re-measurement is a one-constant edit rather than a code
// change.
//
// MEASURED 2026-09-21, and the measurement FAILED for lack of data rather than
// returning a number. The derivation needs, per lens, the rate at which its
// SOLO findings later resolved versus the rate at which its corroborated ones
// did; the ratio of those two is the weight. The inputs are
// localdebt.AggregateQualitySignal's counted terminal outcomes. The live debt
// store at .atcr/debt/*.jsonl held 363 records on that date and ZERO of them
// carried any terminal status — Story 01 shipped StatusUnreproducible and
// StatusAttemptsExhausted days earlier and nothing has been closed under the
// new vocabulary yet. There is also no scorecard store on this machine at all
// (~/.config/atcr/scorecard/ does not exist), so the solo-versus-corroborated
// split has no population either. Sample size: 0 on both sides.
//
// WHAT THIS CANNOT SHOW — stated plainly, the way defaultTrustWindow's "the
// store was only ~35 days old" limitation is: nothing here establishes that
// solo findings resolve at a DIFFERENT rate from corroborated ones at all. If
// they resolve at the same rate the correct weight is 1.0 and this value is
// right by accident; if solo findings resolve far more often the weight is well
// above 1.0 and this value systematically under-credits exactly the specialists
// the epic exists to protect. Both are invisible without the measurement, and
// no later phase may cite this constant as evidence-backed.
//
// RE-MEASUREMENT TRIGGER: redo this once the debt ledger holds 50+ records
// carrying a counted terminal status (resolved / wontfix / unreproducible /
// attempts-exhausted) AND the scorecard store holds runs from 20+ distinct
// lenses, then record the measurement here the way DefaultTrustMinRuns' is
// recorded. TestIsolatedFindingWeight_NotNarrowedWithoutRemeasurement pins the
// literal so a later move cannot ride in without that measurement.
const isolatedFindingWeight = 1.0

// Confirmation is one persona's ground-truth outcome counts, folded across every
// model that persona ran under. It is the READ-TIME half of C18's split: the
// scorecard store records how ISOLATED a lens's findings were, and this records
// how often they turned out to be real.
//
// It deliberately mirrors internal/localdebt.QualityRow's counters without
// importing that package. internal/scorecard must not depend on
// internal/localdebt — the lookup is injected as a function value so the
// weighting stays unit-testable with no live debt store (AC 04-01).
//
// The three non-Confirmed counters are kept SEPARATE rather than summed into
// one "not real" total because they say different things about the lens that
// raised the finding, exactly as localdebt.QualityRow documents.
type Confirmation struct {
	// Confirmed counts findings that became a TD row and were RESOLVED — the
	// finding was real and got fixed.
	Confirmed int
	// Dismissed counts findings closed WONTFIX.
	Dismissed int
	// Unreproducible counts findings nobody could reproduce.
	Unreproducible int
	// AttemptsExhausted counts findings whose fix attempts ran out without a
	// resolution.
	AttemptsExhausted int
}

// GroundTruthLookup returns per-persona confirmation counts keyed by LOWERCASE
// persona name, matching trustPriorsSince's own key convention.
//
// It is a function value rather than a direct internal/localdebt call so the
// dependency stays one-way and injectable. A nil lookup, an error, or a persona
// absent from the returned map all mean "no ground truth", which degrades that
// persona to the pre-existing binary corroboration rate — never to an inflated
// score (AC 04-01 Error Scenario 1).
//
// IT TAKES THE SAME WINDOW THE RECORD READ USED, and that argument is not
// decoration. ResolveTrustPriors bounds its scorecard read to defaultTrustWindow
// while an un-windowed lookup would scale that 180-day isolation credit by an
// all-time confirmation ratio — two populations multiplied as though they were
// commensurate. since <= 0 means "no window", matching trustPriorsSince.
type GroundTruthLookup func(since time.Duration, now time.Time) (map[string]Confirmation, error)

// weightedTally is one persona's weighted-credit evidence, summed across every
// model and run that carries the current credit era.
//
// runs is counted separately from trustPriorsSince's own run tally and is not
// redundant with it: that one counts every run that survived the filter chain,
// this one only the era-marked subset the weighted number is actually computed
// from. During the upgrade window those two differ by almost everything, and
// applying a 20-run floor to the first while dividing by the second reports a
// satisfied floor over a sample of one.
type weightedTally struct {
	credit float64
	raised int
	runs   int
}

// weightedCreditByPersona folds records into per-lowercase-persona weighted
// credit and its matching denominator, keyed the way trustPriorsSince keys its
// own tally so a persona that ran under several models sums into one entry.
//
// A RECORD WITHOUT THE CURRENT CREDIT ERA CONTRIBUTES TO NEITHER SIDE. Dropping
// it from the numerator alone would be worse than counting it: its FindingsRaised
// would stay in the denominator and the lens would be charged for findings whose
// credit was never measured. Excluding both is what makes the weighted rate a
// statement about the era it was measured in.
//
// Above-current eras are excluded rather than clamped, matching PairEra's and
// unresolvedEraRuns' treatment — see CreditEraCurrent.
//
// The input slice is never mutated; this reads records and returns a fresh map.
func weightedCreditByPersona(records []Record) map[string]weightedTally {
	out := map[string]weightedTally{}
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if r.CreditEra < 1 || r.CreditEra > CreditEraCurrent {
			continue
		}
		// THE STORE IS NOT A TRUSTED INPUT HERE. It is plain user-writable JSONL
		// that other binaries and eras also write, and this value is a float that
		// lands in the map reconcile compares against its exemption threshold. A
		// hand-edited weighted_credit of 50 on twenty runs yields a prior of 50.0
		// and blanket trustExempt for that lens. Credit is bounded by
		// construction at emit time — at most isolatedFindingWeight per raised
		// finding, never negative — so a record outside that bound was not
		// written by this emitter and is dropped rather than clamped, matching
		// how the era fields treat a value they did not write.
		if r.WeightedCredit < 0 || r.WeightedCredit > float64(r.FindingsRaised)*isolatedFindingWeight {
			continue
		}
		key := strings.ToLower(r.Reviewer)
		t := out[key]
		t.credit += r.WeightedCredit
		t.raised += r.FindingsRaised
		t.runs++
		out[key] = t
	}
	return out
}

// confirmationFactor is the share of a persona's counted TD outcomes that were
// CONFIRMATIONS, i.e. findings that became a debt row and were resolved.
//
// The three non-confirmed counters all sit in the denominator because the epic
// reads them that way: a finding marked wontfix, one nobody could reproduce, and
// one whose fix attempts ran out all failed to prove the reviewer right. They
// are kept apart in Confirmation rather than pre-summed so a later phase can
// weigh them differently without changing the record shape.
//
// ok is false when there is nothing to divide by, which covers an empty row, a
// persona the ledger has never seen, a malformed row carrying negative counts,
// and a row with too few outcomes to read anything from. Every one of those
// means "no ground truth", and the caller's contract for that is to fall back to
// the binary rate — never to scale a score by a fabricated factor.
//
// The negative check is SEPARATE from the total check and both are load-bearing.
// A row of {Confirmed: -5, Dismissed: 30} has a positive total and would yield a
// factor of -0.1667, putting a negative prior into the map reconcile reads.
func confirmationFactor(c Confirmation) (float64, bool) {
	if c.Confirmed < 0 || c.Dismissed < 0 || c.Unreproducible < 0 || c.AttemptsExhausted < 0 {
		return 0, false
	}
	total := c.Confirmed + c.Dismissed + c.Unreproducible + c.AttemptsExhausted
	if total < minConfirmationOutcomes {
		return 0, false
	}
	return float64(c.Confirmed) / float64(total), true
}

// minConfirmationOutcomes is the number of COUNTED terminal debt outcomes a
// persona needs before its confirmation rate is allowed to scale its credit.
//
// It exists because the two halves of the score were otherwise floored very
// differently: the isolation half needs DefaultTrustMinRuns runs before it is
// reported at all, while the confirmation half would multiply that evidence by a
// ratio read off a single row. One wontfix and nothing else gives a factor of
// 0.0, which zeroes the prior of a lens with twenty strict runs behind it; one
// resolved row gives 1.0. Neither is a measurement.
//
// IT IS PROVISIONAL, for exactly the reason minPairCases is and with the same
// honesty: on 2026-09-21 the live ledger at .atcr/debt/*.jsonl held 363 records
// and ZERO carrying any counted terminal status, so there is no distribution to
// set this from. 20 is taken from DefaultTrustMinRuns deliberately — the two
// floors answer the same shape of question ("is there enough of this to read")
// and copying the one that WAS measured is a smaller invention than picking a
// second number.
//
// WHAT IT CANNOT SHOW: whether 20 terminal outcomes per persona is reachable at
// all. If real ledgers never accumulate that many per lens, this floor silently
// disables the confirmation half everywhere and the score is the isolation half
// alone — which is a fallback, not a failure, but it would be invisible.
//
// RE-MEASUREMENT TRIGGER: redo once the ledger holds counted terminal outcomes
// for 10+ distinct personas, and record the measurement here.
// TestMinConfirmationOutcomes_NotNarrowedWithoutRemeasurement pins the literal.
const minConfirmationOutcomes = 20

// TrustPriorsWithGroundTruth is TrustPriors with C18's read-time confirmation
// half supplied by the caller. TrustPriors itself passes no lookup, so its
// numbers and its map[string]float64 shape are byte-identical to before and
// reconcile/consensus.go's trustExempt/demoteByTrust need no call-site change
// (AC 04-04).
//
// gt is a function value rather than a direct internal/localdebt call so the
// dependency stays one-way and the weighting is unit-testable with no live debt
// store. A nil lookup, a lookup that errors, and a persona absent from the
// returned map all degrade that persona to the pre-existing binary corroboration
// rate — see GroundTruthLookup.
//
// This reads ALL HISTORY, matching TrustPriors rather than ResolveTrustPriors:
// the windowing decision belongs to the caller that has one.
//
// DO NOT WIRE THIS INTO ResolveTrustPriors YET, and the reason is a measurement
// that has not been done rather than unfinished plumbing. The value this returns
// is on a DIFFERENT SCALE from the binary corroboration rate every existing
// consumer was calibrated against. reconcile/consensus.go compares the priors
// map against trustHighThreshold (0.7) and trustLowThreshold (0.3), both derived
// from corroborated/raised, where a lens the panel always agrees with scores
// 1.00. Under this function that same lens scores about 1/N — roughly 0.33 on a
// 3-reviewer cluster, before the confirmation factor lowers it further — so
// feeding this map to the unchanged thresholds would push much of the panel
// under trustLowThreshold and demoteByTrust would mark it all ConfLow. That is
// the panel-wide blackout strictRuns and unresolvedEraRuns are both documented
// as refusing to cause, arriving through the front door.
//
// Both thresholds therefore have to be re-derived against weighted evidence
// before any production caller switches to this function, and that derivation
// needs a live scorecard store that does not exist yet (see
// isolatedFindingWeight's own measurement note). Until then TrustPriors and
// ResolveTrustPriors pass a nil lookup and the production path is unchanged.
// Filed as a Phase 5 prerequisite.
func TrustPriorsWithGroundTruth(dir string, minRuns int, gt GroundTruthLookup) (map[string]float64, error) {
	return trustPriorsSince(dir, minRuns, 0, time.Now(), gt)
}

// TrustPriors reads the scorecard store at dir and returns each reviewer's
// corroboration rate (findings corroborated / findings raised), keyed by
// lowercase reviewer name. Aggregate groups by (Reviewer, Model), so a
// reviewer that ran under several models yields several rows; this sums Runs,
// FindingsCorroborated, and FindingsRaised across those rows before applying
// the minRuns floor and recomputing the ratio, so the result is a true
// per-reviewer aggregate rather than whichever model's row sorted last.
//
// Only runs measured under the STRICT consensus level are counted (see
// strictRuns) — including runs at other levels would let one exploratory
// `--consensus off` reconcile durably depress the priors every later strict run
// reads. This also means minRuns is a floor on STRICT runs: a reviewer with 15
// strict and 10 lenient runs has 15 trusted measurements, not 25.
//
// A reviewer whose summed Runs is below minRuns is OMITTED from the map, not
// present with a zero value — callers can distinguish "no history" from
// "measured zero" (a reviewer that cleared the floor but has never raised a
// finding still appears, at rate 0.0, via ratio()'s zero-denominator case).
// minRuns <= 0 applies no floor.
//
// The rates returned are already era-resolved: trustPriorsSince runs the
// prefer-newest raised_denominator pass (unresolvedEraRuns) before aggregating,
// so a caller receives one definition's numbers per reviewer and never needs to
// reason about the era split itself.
//
// A missing, unreadable, or partially readable store (a mid-enumeration IO
// failure on one month file) yields an empty map and a nil error — this is a
// best-effort read, matching ReadAll's "no data yet"
// contract; it never returns an error or panics.
//
// This function reads ALL HISTORY and always will: cli/personas.go calls it
// directly to render per-persona rates over the whole store. The reconcile-side
// window added in epic 35.11 attaches to ResolveTrustPriors, not here.
func TrustPriors(dir string, minRuns int) (map[string]float64, error) {
	// since=0 is "no window", which trustPriorsSince degrades to a plain ReadAll
	// — so this is byte-identical to the pre-35.11 body. A real time.Now() is
	// passed rather than a zero time.Time even though the no-window path never
	// reads it: relying on that short-circuit would make this call correct only
	// by evaluation order, and a zero now would silently read nothing if the
	// order ever changed.
	//
	// The nil lookup is the contract, not a TODO: TrustPriors' documented
	// behaviour is the binary corroboration rate and AC 04-04 pins that it stays
	// so. A caller that has a debt ledger to offer calls
	// TrustPriorsWithGroundTruth instead.
	return trustPriorsSince(dir, minRuns, 0, time.Now(), nil)
}

// trustPriorsSince is TrustPriors' body with the read bounded to the month files
// overlapping [now-since, now]. The window is applied at FILE SELECTION (via
// ReadSince), not as a post-read filter, so month files outside it are never
// opened or parsed — that I/O and parse cost is the whole point. since <= 0
// means "no window" and reads all history, which is how TrustPriors keeps its
// original semantics through this one shared body.
//
// CAUTION when choosing a window: a reviewer whose summed runs inside the window
// fall below minRuns is omitted from the result entirely, so too narrow a window
// silently stops trust exemption and demotion rather than merely speeding up the
// read. See defaultTrustWindow.
func trustPriorsSince(dir string, minRuns int, since time.Duration, now time.Time, gt GroundTruthLookup) (map[string]float64, error) {
	records, err := ReadSince(dir, since, now, ReadOpts{Writer: io.Discard})
	if err != nil {
		// The record slice is truncated at the failed month file. Aggregating
		// it could push a reviewer under minRuns and silently disable trust
		// exemption/demotion, so fail neutral (empty map) rather than compute
		// priors from a partial store — the same "no data" state a missing
		// store yields.
		return map[string]float64{}, nil
	}

	type tally struct{ runs, corroborated, raised int }
	byReviewer := map[string]*tally{}
	// mergeRoutedEras runs BEFORE the prefer-newest pass so eras 2 and 3 arrive as
	// one era and are never split against each other. It also folds the shielded
	// count into FindingsRaised, which is what makes a plain t.raised the full
	// trust denominator below.
	// eligibleOutcomeRuns runs immediately after strictRuns and BEFORE the two
	// era links — see its doc comment; the position is load-bearing, not
	// cosmetic. D5's requirement is that outcome eligibility precedes the two ERA
	// links, which it does.
	//
	// The opportunity set is split across the two ends of the chain and BOTH
	// positions are load-bearing — see opportunityUnions and opportunitySetRuns.
	// The union is taken from the RAW records so no upstream per-record filter can
	// shrink a case's evidence; the filter runs LAST so the era decision is never
	// made from an opportunity-shrunk record set.
	unions := opportunityUnions(records)
	kept := opportunitySetRuns(unresolvedEraRuns(mergeRoutedEras(eligibleOutcomeRuns(strictRuns(records)))), unions)
	// The weighted fold reads the SAME filtered slice Aggregate does, not the raw
	// records: a run the eligibility or opportunity gate just excluded must not
	// re-enter through the weighted numerator. It is a second pass rather than a
	// field on LeaderboardRow because that row is the export/leaderboard shape and
	// D8's whole point is that no non-trust consumer changes.
	weights := weightedCreditByPersona(kept)
	for _, row := range Aggregate(kept) {
		key := strings.ToLower(row.Reviewer)
		t := byReviewer[key]
		if t == nil {
			t = &tally{}
			byReviewer[key] = t
		}
		t.runs += row.Runs
		t.corroborated += row.FindingsCorroborated
		t.raised += row.FindingsRaised
	}

	// One lookup call for the whole fold, not one per persona: the debt ledger is
	// a store read on the far side of the injected function and the per-persona
	// loop below would turn it into an N+1.
	var confirmations map[string]Confirmation
	if gt != nil {
		if c, err := gt(since, now); err == nil {
			confirmations = c
		}
		// An error DISCARDS whatever map came back with it, rather than using a
		// partial one. An adapter over a truncated debt-store read can return
		// both, and a partial ledger is not a smaller true answer — it is a
		// confirmation ratio computed over a subset nobody chose, applied to
		// every persona's credit. Dropping it leaves confirmations nil and every
		// persona reads "no ground truth", which is the same fail-neutral posture
		// the record read above takes when ReadSince fails: degrade toward the
		// pre-existing behaviour rather than fail the caller, because the caller
		// is reconcile deciding whether to exempt a finding and it has no better
		// fallback than the answer it had before this phase.
	}

	rates := make(map[string]float64, len(byReviewer))
	for name, t := range byReviewer {
		if minRuns > 0 && t.runs < minRuns {
			continue
		}
		rates[name] = weightedRate(t.corroborated, t.raised, weights[name], confirmations[name], minRuns)
	}
	return rates, nil
}

// weightedRate is where C18's two halves meet: the ISOLATION half read off the
// records (w) and the CONFIRMATION half read off the debt ledger (c).
//
// It degrades to the pre-existing binary rate — corroborated/raised — whenever
// either half is missing or is measured too thinly to read.
//
// BE PRECISE ABOUT WHAT THAT FALLBACK DOES, because the obvious claim is wrong
// in one direction. It is NOT true that degrading can only lower a score. It
// lowers a solo-heavy specialist, whose binary rate is near 0 and whose weighted
// rate is near 1 — that is the population this sprint exists to promote. But it
// RAISES a corroboration-heavy generalist, whose binary rate is 1.00 and whose
// weighted rate is about 1/N: an unreadable debt store hands exactly that lens
// its maximal prior back. The fallback restores the pre-existing behaviour in
// both directions, no more and no less, and that is the honest description. It
// is accepted because the pre-existing behaviour is the state every consumer was
// calibrated against, not because it is conservative.
//
// The minRuns floor is applied to w.runs, NOT to the caller's own run tally, and
// the two are different sets during the upgrade window. The caller's tally
// counts every run that survived the filter chain; w.runs counts only the
// era-marked subset this number is actually computed from. Checking the first
// while dividing by the second lets nineteen pre-weighting runs plus ONE
// measured run report a satisfied twenty-run floor over a sample of one — and
// since that one run can be a perfect 1.0, it buys blanket trustExempt. This is
// at its worst on the first upgrade, when almost every record is pre-era.
//
// The result is bounded by construction and the bound is worth stating: each
// finding contributes at most isolatedFindingWeight (1.0) to w.credit and
// exactly 1 to w.raised, weightedCreditByPersona drops any record outside that
// bound, and factor is in [0,1] — so the rate cannot exceed 1.0 while
// isolatedFindingWeight stays at its provisional value. A future re-measurement
// above 1.0 would break that, and the caller — reconcile's demoteByTrust — has
// no defence against a rate above 1. Whoever moves the constant has to decide
// the clamp here.
func weightedRate(corroborated, raised int, w weightedTally, c Confirmation, minRuns int) float64 {
	binary := ratio(corroborated, raised)
	if w.raised <= 0 {
		return binary
	}
	if minRuns > 0 && w.runs < minRuns {
		return binary
	}
	factor, ok := confirmationFactor(c)
	if !ok {
		return binary
	}
	return w.credit * factor / float64(w.raised)
}

// mergeRoutedEras rewrites each era-3 record into its exact era-2 equivalent, so
// the prefer-newest pass sees ONE routed era instead of two and a reviewer keeps
// its pre-upgrade window the day its first era-3 record lands.
//
// The rewrite is lossless for the trust rate because the two eras partition the
// SAME finding set. scorecard.go splits in.UnresolvedFindings disjointly into
// chargeable and doc-shielded, so an era-3 record's FindingsRaised +
// FindingsDocShielded is exactly what era 2 reported as FindingsRaised, and
// FindingsCorroborated is untouched by the carve-out. Folding the shielded count
// back in also preserves the anti-gaming property the trust denominator has
// always had: a reviewer cannot launder phantoms out of its prior by anchoring
// them on doc-named tokens, because the shield never reaches this denominator.
//
// THAT PROPERTY IS NO LONGER ABSOLUTE, and the qualification belongs here rather
// than only at the far end. reviewerCategories withholds a doc-shielded
// finding's CATEGORY (AC 03-02 Edge Case 2) while this fold restores its CHARGE,
// so the two now disagree for the trust tally. A lens whose only in-remit
// evidence on a run was doc-shielded contributes nothing to that run's union,
// and opportunitySetRuns then drops the whole record — re-folded charge included
// — whenever some other reviewer raised a discriminating out-of-remit category.
// The laundering route is narrower than before this fold existed, not closed.
// Filed as TD-036.
//
// Era 1 is deliberately NOT merged. It EXCLUDES routed findings from
// FindingsRaised rather than partitioning them, so its denominator covers a
// smaller finding set and blending it in would compare two different quantities —
// the split the prefer-newest pass exists to enforce.
//
// Records ABOVE the current definition are left alone: they are computed under a
// rule this binary does not implement, and normalising one would smuggle it past
// unresolvedEraRuns' exclusion. The test is on the RAW field for that reason —
// raisedDenominatorOf would clamp an above-current value to the current one and
// admit exactly the record that must not be admitted.
//
// The source era is raisedDenominatorRoutedExShield — era 3 BY NAME, never
// RaisedDenominatorCurrent. The equivalence argued above is a proof about the
// 2-to-3 pair; keyed on "whatever is current" it would silently re-point at a
// future era 4 the moment the constant moved, asserting an equivalence nobody
// established. A bump must come back here and prove the new pair.
//
// The rewritten record is left internally CONSISTENT, not merely era-relabelled:
// FindingsSolo and CorroborationRate are recomputed against the merged
// denominator. Nothing on this path reads either, but the value is a Record, and
// the type's other consumers do.
//
// The input slice is never mutated: callers hand in records read from the store
// and must not see them rewritten underneath.
//
// TestMergeRoutedEras_PinsEveryElementOfItsGuard kills a mutation of each element
// below; TestTrustPriors_AboveCurrentRecordsNeverReachThePrior pins the
// consequence of the era arm on the prior itself.
func mergeRoutedEras(records []Record) []Record {
	out := make([]Record, len(records))
	copy(out, records)
	for i := range out {
		if out[i].RecordType != RecordTypeReviewer || out[i].RaisedDenominator != raisedDenominatorRoutedExShield {
			continue
		}
		out[i].FindingsRaised += out[i].FindingsDocShielded
		out[i].FindingsDocShielded = 0
		out[i].RaisedDenominator = raisedDenominatorAllRouted
		out[i].RaisedIncludesUnresolved = true
		// The two fields DERIVED from FindingsRaised move with it, or the record
		// contradicts itself. A doc-shielded finding was routed, so it is
		// uncorroborated by construction and belongs in solo — which makes these
		// exactly the values era 2 reported, the same disjoint partition the
		// equivalence above rests on.
		out[i].FindingsSolo = out[i].FindingsRaised - out[i].FindingsCorroborated
		out[i].CorroborationRate = ratio(out[i].FindingsCorroborated, out[i].FindingsRaised)
	}
	return out
}

// strictRuns keeps only the records measured under the strict consensus level —
// the semantics a corroboration rate has always carried.
//
// FindingsRaised and FindingsCorroborated are computed from the POST-consensus-
// filter finding set (see reviewerCounts), so the same review yields a different
// rate at each level: under lenient or off the uncorroborated singletons strict
// would have sidecarred stay in the set, inflating raised without raising
// corroborated. Mixing those runs in would let one exploratory `--consensus off`
// run durably depress the priors demoteByTrust and trustExempt apply on every
// LATER strict run — a cross-run feedback loop, since a depressed prior demotes
// more findings, which depresses the rate further.
//
// An EMPTY level counts as strict: a store written before epic 35.9.1 has no
// consensus_level, and every one of those runs was strict by construction (the
// levels did not exist). Reading empty as non-strict would strand every existing
// reviewer history in the field.
//
// An UNRECOGNIZED level (only reachable from a hand-edited or corrupted store —
// the emitter always stamps a canonical value) is EXCLUDED rather than read as
// strict. This deliberately inverts consensusFloor's reconcile-time fail-safe,
// because the risk is inverted: there, mistaking a level for non-strict would
// disable the filter, while here, admitting an uninterpretable label could let a
// mislabeled non-strict run depress the priors. Excluding it only forgoes data.
//
// This filter is deliberately scoped to TrustPriors and NOT applied to Aggregate
// itself: the `atcr scorecard` leaderboard reports what actually happened across
// all runs, while the trust prior is a behavioral measurement that is only
// comparable at a fixed level.
func strictRuns(records []Record) []Record {
	kept := make([]Record, 0, len(records))
	for _, r := range records {
		if c, ok := reclib.NormalizeConsensus(r.ConsensusLevel); ok && c == reclib.ConsensusStrict {
			kept = append(kept, r)
		}
	}
	return kept
}

// eligibleOutcomeRuns keeps only the runs where the lens actually got a fair
// attempt, so a durable score measures judgment rather than hosting.
//
// The panel this feeds is deliberately heterogeneous, and its failure history is
// infrastructural rather than editorial: one lens hung on a 1200s proxy timeout
// three times over, another returned truncated-with-zero-findings on every run
// because its host silently capped prompts at 16,384 tokens while answering
// HTTP 200, a third was auth-failed on a billing cap. Without this link all
// three look identical to a lens that read the diff and had nothing to say, and
// every one of them is durably demoted for its wiring.
//
// ELIGIBLE: findings, clean, ungrounded, filtered.
// EXCLUDED: unparseable, truncated, incomplete, failed, unknown.
//
// ungrounded and filtered sit on the eligible side deliberately, and it is the
// one genuinely open call here. Neither is a broken attempt: both are downstream
// of a complete, parseable response whose findings were discarded for cause, so
// they report on judgment. Revisit once a live store exists to measure against.
//
// unknown is excluded rather than inferred. It is the Go zero value, so it means
// both "written before schema 2" and "nobody classified this"; reading it as
// clean would credit a full trust rate to runs no one ever observed.
//
// The membership test is an ALLOWLIST, not a denylist, so a tenth outcome value
// added to the vocabulary is excluded until somebody decides otherwise — the
// fail-neutral direction for a score that persists for 180 days.
//
// Aggregate records pass through untouched, matching unresolvedEraRuns below. An
// aggregate is not a reviewer and carries no outcome, so judging it on one would
// make this the first link in the chain to drop aggregates over a property that
// does not apply to them.
//
// The input slice is never mutated.
//
// WHY IT SITS WHERE IT DOES (immediately after strictRuns, before
// mergeRoutedEras): outcome eligibility is a per-record property of the raw run
// — did this lens get a fair attempt — and is independent of era routing, so an
// ineligible record must be dropped before it can take part in an era decision.
// That is not merely tidy. unresolvedEraRuns is prefer-newest per reviewer: run
// this link after it, and one failed run stamped at a newer era sets that
// reviewer's newest era, and its entire older history is discarded as "the mix"
// before the failure itself is ever discarded. A single timeout would erase a
// lens's whole record. strictRuns stays first, preserving the existing
// cheapest-narrowing-first ordering.
func eligibleOutcomeRuns(records []Record) []Record {
	kept := make([]Record, 0, len(records))
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			kept = append(kept, r) // aggregates pass through untouched
			continue
		}
		switch r.Outcome {
		case outcomeFindings, outcomeClean, outcomeUngrounded, outcomeFiltered:
			kept = append(kept, r)
		}
	}
	return kept
}

// opportunityUnions builds, per RunID, the set of DISCRIMINATING categories any
// reviewer raised on that run. It is deliberately computed from the RAW record
// slice handed to trustPriorsSince — before strictRuns, before the outcome gate
// and before either era link — because the opportunity is a property of the
// CASE, and every one of those links is a PER-RECORD filter that can delete a
// reviewer from a run the rest of the panel still worked.
//
// Taking the union downstream of any of them is the same bug three times over:
//
//   - after strictRuns, one corrupt or hand-edited consensus_level removes that
//     reviewer's categories from the union, and a specialist is then dropped
//     from a run where its remit demonstrably WAS in play;
//   - after eligibleOutcomeRuns, a TRUNCATED reviewer's categories are lost even
//     though internal/fanout records that a truncated reviewer can still have
//     raised findings;
//   - after the era links, the surviving-era subset decides the union.
//
// The filter half is opportunitySetRuns below, and it runs LAST in the chain for
// a separate reason — see its comment.
func opportunityUnions(records []Record) map[string]map[string]struct{} {
	seenByRun := map[string]map[string]struct{}{}
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer || r.SchemaVersion < categoriesRaisedSinceSchema {
			continue
		}
		for _, c := range r.CategoriesRaised {
			if !discriminating(c) {
				continue
			}
			if seenByRun[r.RunID] == nil {
				seenByRun[r.RunID] = map[string]struct{}{}
			}
			seenByRun[r.RunID][c] = struct{}{}
		}
	}
	return seenByRun
}

// opportunitySetRuns keeps a reviewer's record only when that run was an
// opportunity for its lens — when some reviewer on the run raised a category
// inside that lens's remit.
//
// This is the epic's headline property expressed on the chain. A narrow lens is
// SUPPOSED to be silent most of the time; scored against the full corpus it
// looks like a lens that never contributes, while the generalist accumulates
// standing simply by being in scope everywhere. Removing out-of-remit runs from
// the denominator is what makes a specialist's rate comparable to a
// generalist's.
//
// The opportunity is a property of the CASE, so the union is taken per RunID
// across every reviewer's CategoriesRaised. Unioning across the whole store
// instead would make every lens permanently in-remit after one broad run.
//
// THREE KINDS OF RECORD ARE NEVER JUDGED, and each would be a distinct silent
// failure:
//
//   - AGGREGATES pass through, matching eligibleOutcomeRuns and unresolvedEraRuns
//     above. An aggregate is not a reviewer and carries no remit.
//   - PRE-SCHEMA-2 records pass through. Their category set is absent because
//     nothing measured it, and omitempty makes that byte-identical to a measured
//     empty set. Judging them would read the whole unmeasured back-catalogue as
//     "out-of-remit for everyone" and silently shrink every persona's
//     denominator — the cross-era blending unresolvedEraRuns exists to prevent.
//     (They do not in fact reach here today: eligibleOutcomeRuns already drops
//     them for their absent Outcome. The guard is kept because that is an
//     upstream link's behaviour, not this one's contract.)
//   - UNMAPPED personas pass through. vera, pace, brad, archer and ronin have no
//     in-repo definition to ground a remit against (personaRemit's comment), so
//     they are not opportunity-scoped. Dropping them instead would remove trust
//     scoring outright for five of the thirteen live lenses as a SIDE EFFECT of
//     a fairness fix — a regression nothing in this sprint asked for. Unmapped
//     means "not scoped", never "deleted".
//   - RUNS WITH NO DISCRIMINATING CATEGORY AT ALL pass through, every record of
//     them. This is the one that looks like a rule and is really a refusal to
//     guess, and it is NOT the same as the predicate's "a clean case is
//     out-of-remit for everyone". A run reaches an empty union by three routes
//     that the store cannot tell apart: nobody raised anything; everybody raised
//     findings whose CATEGORY word was outside the closed vocabulary and was
//     dropped at the write gate; or every raised value was non-discriminating
//     (see nonDiscriminating in remit.go). The second is not hypothetical —
//     reconcile/category.go records a dry run in which 72.3% of findings used a
//     word the scorer did not recognise. Judging an empty union would durably
//     un-score every lens on such a run for a LABELLING failure, which is the
//     same class of mistake as demoting a lens for its hosting: the exact thing
//     the outcome gate below exists to refuse.
//
// THE UNION AND THE FILTER SIT AT OPPOSITE ENDS OF THE CHAIN, ON PURPOSE.
// opportunityUnions is computed from the RAW records (see its comment: the
// opportunity is a property of the CASE, so no per-record filter may shrink the
// evidence for it). This half — the filter — runs LAST, outside both era links.
//
// WHY THE FILTER RUNS AFTER unresolvedEraRuns. That pass computes each
// reviewer's NEWEST raised-denominator era from the records that reach it, then
// keeps only that era. If the opportunity filter ran first, dropping a
// reviewer's newest-era records as out-of-remit would rewind its era window to
// an older definition — and era 1 EXCLUDES routed phantoms from the denominator
// rather than partitioning them, so the rate goes UP and a phantom-raising lens
// can flip from demoted to exempt. The era must be decided by which definition
// the reviewer actually ran under, never by which of its runs happened to be in
// remit. Running last also keeps the filter clear of mergeRoutedEras, which
// rewrites denominators but never touches RunID, Reviewer or CategoriesRaised,
// so the union stays valid across it.
//
// The input slice is never mutated: callers hand in records read from the store.
func opportunitySetRuns(records []Record, seenByRun map[string]map[string]struct{}) []Record {
	kept := make([]Record, 0, len(records))
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer || r.SchemaVersion < categoriesRaisedSinceSchema {
			kept = append(kept, r)
			continue
		}
		union := seenByRun[r.RunID]
		if len(union) == 0 {
			kept = append(kept, r)
			continue
		}
		// Resolved once per record and reused, rather than calling
		// RemitCategories here and letting InOpportunitySet call it again —
		// every call allocates a defensive copy.
		remit, mapped := RemitCategories(r.Reviewer)
		if !mapped {
			kept = append(kept, r)
			continue
		}
		if intersects(remit, union) {
			kept = append(kept, r)
		}
	}
	return kept
}

// intersects reports whether any member of remit is present in union. It is the
// set-shaped inner half of InOpportunitySet, split out so the chain can hoist
// the remit lookup and reuse one deduped union per run; InOpportunitySet keeps
// the slice-shaped public signature its acceptance criteria pin.
func intersects(remit []string, union map[string]struct{}) bool {
	for _, c := range remit {
		if _, ok := union[c]; ok {
			return true
		}
	}
	return false
}

// The four eligible outcome values, spelled as literals because
// internal/benchmark imports this package and importing it back would close a
// cycle. internal/benchmark/outcome.go stays the vocabulary's single definition.
//
// These four are NOT covered by cli/fanout_outcome_parity_test.go — they are
// unexported, so that test cannot see them. They are pinned only indirectly, by
// this package's independently-written test literals and by
// TestEmitForReconcile_OutOfVocabularyOutcomeIsCoercedToUnknown. Filed as TD:
// the durable fix is an exported vocabulary slice in internal/benchmark that
// every site iterates.
const (
	outcomeFindings   = "findings"
	outcomeClean      = "clean"
	outcomeUngrounded = "ungrounded"
	outcomeFiltered   = "filtered"
)

// unresolvedEraRuns keeps the records of ONE FindingsRaised definition, never a
// mix of both.
//
// It is the second era filter on this path, and it is here for the reason
// strictRuns is: FindingsRaised changed meaning when Epic 35.16.6.5 put the
// Tier-4-routed findings into the denominator, and summing both meanings produces
// a rate that measures neither. A reviewer that raised six corroborated findings
// and four phantoms reports 0.60 under the new definition and 1.00 under the old,
// so a blended window drifts as the old records age out, silently moving
// trustExempt and demoteByTrust with nothing marking the boundary.
//
// The rule is PREFER-NEWEST, not require-current: a reviewer's records are kept
// at the newest definition that reviewer actually has, whatever that is. When it
// holds any record under the current definition, only those count; when it holds
// none, its older records are used as they always were. Both halves matter.
//
// Newest rather than "has the current flag" because FindingsRaised has now
// changed meaning TWICE — Epic 35.16.6.8 took the doc-shielded routings back out
// — so the question has three answers and a bool cannot say which two a window
// is blending. See RaisedDenominator.
// Requiring the flag would black out every existing reviewer history on upgrade —
// the same stranding strictRuns' empty-means-strict rule exists to avoid — and a
// pre-epic-only history is at least INTERNALLY consistent, which a blend never is.
// The one-time step when a reviewer's first current-era record lands is the honest
// cost of switching between two coherent measurements; it is not a drift.
//
// The partition is PER REVIEWER, and that is the whole subtlety. Applied to the
// slice as a whole it asks the wrong question — "has anyone upgraded?" — so the
// first post-upgrade run, which flags only the reviewers on that panel, dropped
// every OTHER reviewer's records entirely. Those reviewers then left byReviewer
// and were absent from the prior map at any minRuns, and absent is not neutral:
// trustExempt reads a missing key as "not exempt" while demoteByTrust reads the
// same missing key as "do not demote", so a low-trust phantom-raiser silently
// stopped being demoted — the reverse of what putting phantoms in the denominator
// was for. A reviewer's era is a property of its own history, so only its own
// records may answer for it.
//
// Like strictRuns this is scoped to TrustPriors and NOT applied to Aggregate: the
// `atcr scorecard` leaderboard reports what actually happened across all runs.
//
// Input order is preserved, and a record whose Reviewer is empty or which is not
// a reviewer record simply forms its own group — Aggregate drops the non-reviewer
// rows immediately after, so grouping them costs nothing and needs no special case
// here.
//
// Three asymmetries are deliberate:
//
//   - strictRuns is NOT per reviewer, and must not be. It classifies each record
//     independently on that record's own consensus_level, so it asks no
//     whole-slice question and no reviewer's records can answer for another's.
//     Only a rule that consults the SET needs a grouping key.
//
//   - The key is the reviewer ALONE, not (reviewer, model), because the rate this
//     protects is per reviewer: trustPriorsSince sums a reviewer's models into one
//     tally, so a finer key would let a current-era model and a pre-epic one blend
//     inside that sum — the mix this exists to forbid. The cost lands on Export,
//     which groups by (reviewer, model): a reviewer that changed models across the
//     upgrade loses the old model's row from the leaderboard. That is narrower
//     than the blend it buys, and it is the direction the filed fix chose.
//
//   - The key is strings.ToLower(Reviewer), matching trustPriorsSince's byReviewer
//     key exactly. Export keys on scrubField(Reviewer) instead, so two identities
//     differing only by a non-printing rune form one export group but two era
//     groups here. Reachable only from a hand-edited store — the export path
//     already rejects non-printing runes in an identity — and matching the
//     consumer this filter is defined for is worth more than matching the other.
//
// Non-empty in, non-empty out — with ONE exception. A reviewer with no current-era
// record keeps all of its records, and one with a current-era record keeps at least
// that record, so no era this pass RECOGNISES can be emptied. Records ABOVE the
// current definition are excluded outright by both loops, so an input made entirely
// of those returns nothing.
//
// That exception is why the callers behind this pass cannot report an empty result
// as a filter miss: PublishedSet and ExportSelected each detect it and raise
// ErrNoCurrentEraRecords instead of ErrNoExportRecords, because "your store was
// written by a newer atcr" and "your filters matched nothing" call for opposite
// operator actions.
func unresolvedEraRuns(records []Record) []Record {
	// The NEWEST definition each reviewer has any record under. Prefer-current
	// generalizes to prefer-newest once there are more than two definitions: the
	// rule was never "has the flag", it was "do not blend", and with three
	// denominators a bool cannot express which two are being blended.
	newest := make(map[string]int, len(records))
	for _, r := range records {
		// Only reviewer records hold a per-reviewer era. An aggregate record is
		// stamped RaisedDenominator = Current under the EMPTY reviewer name, so
		// without this skip it defines the newest era for every empty-name
		// reviewer record that shares the key — and whether those survive then
		// depends on whether the caller dropped aggregates BEFORE this pass
		// (PublishedSet's ApplyFilters does; trustPriorsSince does not).
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		if r.RaisedDenominator > RaisedDenominatorCurrent {
			// Above-current means this binary does not implement the definition
			// the record was computed under — a legitimate record from a NEWER
			// atcr, a benchmark-suite value, or a corrupt hand-edit are
			// indistinguishable here, and all three are EXCLUDED from the era
			// window rather than clamped into the current cohort and blended.
			// Exclusion preserves the clamp's protective intent (a garbage line
			// cannot delete the reviewer's real history) without re-labelling a
			// future era as the current one.
			continue
		}
		k := strings.ToLower(r.Reviewer)
		if d := raisedDenominatorOf(r); d > newest[k] {
			newest[k] = d
		}
	}
	kept := make([]Record, 0, len(records))
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			kept = append(kept, r) // aggregates pass through untouched
			continue
		}
		if r.RaisedDenominator > RaisedDenominatorCurrent {
			continue // above-current: excluded, per the first loop
		}
		// Keep the record when it is computed under the newest definition its own
		// reviewer has. A reviewer with only pre-epic history keeps all of it
		// (its newest IS pre-epic), which is what stops an upgrade from blacking
		// out an existing store. What is dropped is only the older half of a
		// reviewer that spans a change — the mix, which is the one combination
		// measuring neither.
		if raisedDenominatorOf(r) == newest[strings.ToLower(r.Reviewer)] {
			kept = append(kept, r)
		}
	}
	return kept
}

// ResolveTrustPriors resolves the default scorecard store directory and reads
// the priors from it at DefaultTrustMinRuns, degrading to a nil map on any
// failure (an unresolvable user config dir, or the read's own best-effort
// "missing/unreadable store" case) — never an error, never a blocker for the
// caller (epic 35.9 AC5).
//
// Unlike TrustPriors, this read is WINDOWED to defaultTrustWindow (epic 35.11):
// epic 35.9 put it on the primary path of every review and reconcile, so its
// cost is unconditional and grows with the store, which has no rotation. The two
// differ deliberately — TrustPriors serves cli/personas.go, which reports on the
// whole store, while this serves reconcile, which needs a bounded read of recent
// behavior. See defaultTrustWindow for why the window is 180d and what a
// narrower one would silently break.
//
// This is the single helper every reconcile.RunReconcile
// call site (cli/reconcile.go, cli/resume.go, cli/review.go,
// internal/mcp/handlers.go) uses to attach the reviewer trust prior to
// reclib.Options.TrustPriors before calling RunReconcile — NOT called from
// inside internal/reconcile itself, because internal/scorecard already imports
// internal/reconcile (EmitForReconcile takes a reconcile.Result), so the
// reverse import would cycle.
func ResolveTrustPriors() map[string]float64 {
	dir, err := DefaultDir()
	if err != nil {
		return nil
	}
	// The read is documented best-effort and never returns a non-nil error, so
	// the error is discarded (matching cli/personas.go's convention).
	//
	// No ground-truth lookup is passed, so this returns the binary corroboration
	// rate exactly as it did before Phase 4b. internal/scorecard cannot import
	// internal/localdebt, so the adapter has to be supplied by a caller that can
	// see both packages — cli/ and internal/mcp/ both can. Wiring them is Phase
	// 5's job (the explainability + wire-in phase); until then the weighted rate
	// is reachable only through TrustPriorsWithGroundTruth.
	priors, _ := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, time.Now(), nil)
	return priors
}
