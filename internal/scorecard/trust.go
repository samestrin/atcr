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
// out of the priors map and reverts to the neutral no-history state — the same
// state a brand-new reviewer occupies (reconcile/consensus.go does a plain map
// lookup with no distinct "dormant" handling). Re-widening this constant, not an
// empty-map fallback, is the fix if that ever bites: the map stays non-empty
// while any reviewer is active, so a fallback keyed on emptiness would never
// fire for a single dormant reviewer.
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
	return trustPriorsSince(dir, minRuns, 0, time.Now())
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
func trustPriorsSince(dir string, minRuns int, since time.Duration, now time.Time) (map[string]float64, error) {
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
	for _, row := range Aggregate(unresolvedEraRuns(strictRuns(records))) {
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

	rates := make(map[string]float64, len(byReviewer))
	for name, t := range byReviewer {
		if minRuns > 0 && t.runs < minRuns {
			continue
		}
		rates[name] = ratio(t.corroborated, t.raised)
	}
	return rates, nil
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
// The rule is PREFER-CURRENT, not require-current: when the window holds any
// record stamped with the current definition, only those count; when it holds
// none, the pre-epic records are used as they always were. Both halves matter.
// Requiring the flag would black out every existing reviewer history on upgrade —
// the same stranding strictRuns' empty-means-strict rule exists to avoid — and a
// pre-epic-only store is at least INTERNALLY consistent, which a blend never is.
// The one-time step when the first current-era record lands is the honest cost of
// switching between two coherent measurements; it is not a drift.
//
// Like strictRuns this is scoped to TrustPriors and NOT applied to Aggregate: the
// `atcr scorecard` leaderboard reports what actually happened across all runs.
func unresolvedEraRuns(records []Record) []Record {
	current := make([]Record, 0, len(records))
	for _, r := range records {
		if r.RaisedIncludesUnresolved {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		return current
	}
	return records
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
	priors, _ := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, time.Now())
	return priors
}
