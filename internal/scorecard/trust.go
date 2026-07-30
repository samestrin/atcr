package scorecard

import (
	"io"
	"strings"
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
	for _, row := range Aggregate(records) {
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

// ResolveTrustPriors is a stub pending implementation (RED).
func ResolveTrustPriors() map[string]float64 {
	return nil
}
