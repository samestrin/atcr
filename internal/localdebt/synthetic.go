package localdebt

import (
	"fmt"
	"path/filepath"
	"strings"
)

// MaxSyntheticRecords caps GenerateSynthetic's count: twenty times the 50,000-record
// measurement store, about a gigabyte on disk, so a mistyped -n fails fast instead
// of filling the disk.
const MaxSyntheticRecords = 1_000_000

const (
	// syntheticComponents is how many internal/pkgNNN directories the synthetic
	// store spreads its findings over, so each component holds ~1% of the rows.
	syntheticComponents = 100

	syntheticFillerSentence = "the caller assumes the value is already validated upstream; "
	syntheticFillerRepeats  = 5
	// syntheticFixFillerLen is how much of syntheticFiller the Fix field carries.
	syntheticFixFillerLen = 120
)

// The Fix slice below is taken at package init, so a filler shorter than
// syntheticFixFillerLen would panic before main. Converting the (constant)
// difference to uint makes that a compile error instead.
const _ = uint(len(syntheticFillerSentence)*syntheticFillerRepeats - syntheticFixFillerLen)

var (
	syntheticSeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}
	syntheticCategories = []string{"error-handling", "api-contract", "concurrency", "performance", "security", "testing"}
	syntheticReviewers  = []string{"security-auditor", "go-reviewer", "perf-analyst"}
	// syntheticFiller pads the free-text fields to the size of a real reconcile
	// finding, so the store weighs what the 35.13 measurement corpus weighed.
	syntheticFiller = strings.Repeat(syntheticFillerSentence, syntheticFillerRepeats)
	syntheticFix    = "validate the input at the boundary and return a typed error; " + syntheticFiller[:syntheticFixFillerLen]
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
//
// The index is unsigned so i < 0 cannot be expressed: with an int, a negative i
// indexed the severity table out of range or stamped a "2026--3-..." timestamp.
// Every uint yields a well-formed record. Each record gets its own Reviewers
// slice, so a caller mutating one cannot reach another.
func SyntheticRecord(i uint) Record {
	month := 1 + i%6
	r := Record{
		SchemaVersion: SchemaVersion,
		RunID:         fmt.Sprintf("2026-%02d-synthetic", month),
		Timestamp:     fmt.Sprintf("2026-%02d-%02dT%02d:%02d:00Z", month, 1+i%28, i%24, i%60),
		Severity:      syntheticSeverities[i%uint(len(syntheticSeverities))],
		File:          fmt.Sprintf("internal/pkg%03d/file%02d.go", i%syntheticComponents, (i/syntheticComponents)%20),
		Line:          1 + int(i/syntheticComponents), // a uint over 100 always fits an int
		Problem:       fmt.Sprintf("synthetic finding %d: %s", i, syntheticFiller),
		Fix:           syntheticFix,
		Category:      syntheticCategories[i%uint(len(syntheticCategories))],
		EstMinutes:    5 + int(i%60),
		Evidence:      fmt.Sprintf("internal/pkg%03d: the unchecked path is reachable from the public entry point", i%syntheticComponents),
		Reviewers:     append([]string(nil), syntheticReviewers...),
		Confidence:    "high",
		Origin:        OriginReview,
	}
	r.StampID()
	return r
}

// GenerateSynthetic appends SyntheticRecord(0) through SyntheticRecord(n-1) to
// the store at dir, one localdebt.Append each — the store's real write path, so
// the shards it produces are exactly what reconcile and `debt add` would leave.
// It refuses a store that already holds shards, so the result is always exactly n
// records and a re-run cannot silently double the corpus. n must lie in
// [0, MaxSyntheticRecords]. Build one with `go run ./cmd/gen-debt-store`.
func GenerateSynthetic(dir string, n int) error {
	if n < 0 || n > MaxSyntheticRecords {
		return fmt.Errorf("synthetic record count %d: expected a non-negative number no greater than %d", n, MaxSyntheticRecords)
	}
	if hasShardFiles(dir) {
		return fmt.Errorf("store %s already holds records; generate into an empty directory", filepath.Base(dir))
	}
	for i := 0; i < n; i++ {
		r := SyntheticRecord(uint(i))
		if err := Append(dir, r); err != nil {
			return fmt.Errorf("synthetic record %d (%s): %w", i, r.ID, err)
		}
	}
	return nil
}
