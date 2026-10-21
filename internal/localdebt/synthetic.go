package localdebt

import (
	"fmt"
	"strings"
)

// syntheticComponents is how many internal/pkgNNN directories the synthetic store
// spreads its findings over, so each component holds ~1% of the rows.
const syntheticComponents = 100

var (
	syntheticSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}
	syntheticCategories = []string{"error-handling", "api-contract", "concurrency", "performance", "security", "testing"}
	syntheticReviewers  = []string{"security-auditor", "go-reviewer", "perf-analyst"}
	// syntheticFiller pads the free-text fields to the size of a real reconcile
	// finding, so the store weighs what the 35.13 measurement corpus weighed.
	syntheticFiller = strings.Repeat("the caller assumes the value is already validated upstream; ", 5)
)

// SyntheticRecord returns the i-th record of the synthetic debt store, a pure
// function of i: the same index always yields the same record, on any machine,
// so peak-RSS numbers taken over the store at two commits measure the code and
// not the corpus (epic 35.21 AC7).
//
// Every record is open and distinct. Severities cycle through the four known
// values (a quarter CRITICAL), files spread across syntheticComponents
// directories (~1% each), and run ids across the first six months of 2026 — the
// selectivities the 35.13 `debt list` measurements were taken at.
func SyntheticRecord(i int) Record {
	month := 1 + i%6
	r := Record{
		SchemaVersion: SchemaVersion,
		RunID:         fmt.Sprintf("2026-%02d-synthetic", month),
		Timestamp:     fmt.Sprintf("2026-%02d-%02dT%02d:%02d:00Z", month, 1+i%28, i%24, i%60),
		Severity:      syntheticSeverities[i%len(syntheticSeverities)],
		File:          fmt.Sprintf("internal/pkg%03d/file%02d.go", i%syntheticComponents, (i/syntheticComponents)%20),
		Line:          1 + i/syntheticComponents,
		Problem:       fmt.Sprintf("synthetic finding %d: %s", i, syntheticFiller),
		Fix:           "validate the input at the boundary and return a typed error; " + syntheticFiller[:120],
		Category:      syntheticCategories[i%len(syntheticCategories)],
		EstMinutes:    5 + i%60,
		Evidence:      fmt.Sprintf("internal/pkg%03d: the unchecked path is reachable from the public entry point", i%syntheticComponents),
		Reviewers:     syntheticReviewers,
		Confidence:    "high",
		Origin:        OriginReview,
	}
	r.StampID()
	return r
}

// GenerateSynthetic appends SyntheticRecord(0) through SyntheticRecord(n-1) to
// the store at dir, one localdebt.Append each — the store's real write path, so
// the shards it produces are exactly what reconcile and `debt add` would leave.
// It appends to whatever dir already holds; point it at an empty directory for a
// store of exactly n records. Build one with `go run ./cmd/gen-debt-store`.
func GenerateSynthetic(dir string, n int) error {
	if n < 0 {
		return fmt.Errorf("synthetic record count %d: expected a non-negative number", n)
	}
	for i := 0; i < n; i++ {
		r := SyntheticRecord(i)
		if err := Append(dir, r); err != nil {
			return fmt.Errorf("synthetic record %d (%s): %w", i, r.ID, err)
		}
	}
	return nil
}
