package history

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/samestrin/atcr/internal/stream"
)

// poolFindingsRel is the review-directory-relative path to the merged, 8-column
// per-source pool findings file that every review run writes via WritePool. It
// is the single findings artifact guaranteed to exist on every run (reconciled
// findings.json exists only when reconcile runs), so it is the history hook's
// source of truth.
var poolFindingsRel = filepath.Join("sources", "pool", "findings.txt")

// RecordReview reads the pool findings for the review at reviewDir, derives one
// Record per finding (stamped with run time ts), and appends them to the history
// ledger at histPath. It returns the number of records appended.
//
// A missing pool findings file is treated as "nothing to record" (0, nil): a
// history write must never turn an otherwise-successful review into a failure.
// A findings file that parses to zero findings appends nothing and creates no
// ledger file.
func RecordReview(histPath, reviewDir string, ts time.Time) (int, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading pool findings: %w", err)
	}

	res, err := stream.ParseSource(data)
	if err != nil {
		return 0, fmt.Errorf("parsing pool findings: %w", err)
	}

	// The pool findings.txt is the concatenation of every reviewer's rows, so a
	// finding caught by N reviewers appears N times. Dedupe by id within this run
	// so the ledger holds one record per distinct finding per run ("one JSON
	// record per finding", per the plan) and the severity table is not inflated
	// by reviewer multiplicity. The first occurrence wins.
	records := make([]Record, 0, len(res.Findings))
	seen := make(map[string]bool, len(res.Findings))
	for _, f := range res.Findings {
		id := FindingID(f.File, f.Line, f.Problem)
		if seen[id] {
			continue
		}
		seen[id] = true
		records = append(records, Record{
			Timestamp: ts,
			Package:   PackageOf(f.File),
			Severity:  f.Severity,
			ID:        id,
			File:      f.File,
			Category:  f.Category,
		})
	}
	if err := Append(histPath, records); err != nil {
		return 0, err
	}
	return len(records), nil
}
