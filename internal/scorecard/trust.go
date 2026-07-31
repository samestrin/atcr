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
// A missing or unreadable store directory yields an empty map and a nil
// error — this is a best-effort read, matching ReadAll's "no data yet"
// contract; it never returns an error or panics.
func TrustPriors(dir string, minRuns int) (map[string]float64, error) {
	records, _ := ReadAll(dir, ReadOpts{Writer: io.Discard})

	type tally struct{ runs, corroborated, raised int }
	byReviewer := map[string]*tally{}
	for _, row := range Aggregate(strictRuns(records)) {
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

// ResolveTrustPriors resolves the default scorecard store directory and reads
// TrustPriors from it at DefaultTrustMinRuns, degrading to a nil map on any
// failure (an unresolvable user config dir, or TrustPriors' own best-effort
// "missing/unreadable store" case) — never an error, never a blocker for the
// caller (epic 35.9 AC5). This is the single helper every reconcile.RunReconcile
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
	// TrustPriors is documented best-effort and never returns a non-nil error,
	// so the error is discarded (matching cli/personas.go's convention).
	priors, _ := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, time.Now())
	return priors
}

// defaultTrustWindow bounds the reconcile-side trust-prior read (epic 35.11 T2).
const defaultTrustWindow = 180 * 24 * time.Hour

// trustPriorsSince is the windowed aggregation. RED stub: ignores the window.
func trustPriorsSince(dir string, minRuns int, since time.Duration, now time.Time) (map[string]float64, error) {
	return TrustPriors(dir, minRuns)
}
