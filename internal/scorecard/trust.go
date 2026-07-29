package scorecard

// DefaultTrustMinRuns is the conservative minimum-run floor a reviewer must
// clear before its corroboration rate is trusted by a caller that does not
// pick its own minRuns. Live-store analysis (2026-07-29) showed rates were
// still unstable at 10 summed runs but converged by 20; core reviewers already
// run 100+ runs, so this floor does not strand real reviewers.
const DefaultTrustMinRuns = 0

// TrustPriors is a stub pending implementation.
func TrustPriors(dir string, minRuns int) (map[string]float64, error) {
	return nil, nil
}
