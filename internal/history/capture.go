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

// PoolFindingsRel is the review-directory-relative path to the merged, 8-column
// per-source pool findings file that every review run writes via WritePool. It
// is the single findings artifact guaranteed to exist on every run (reconciled
// findings.json exists only when reconcile runs), so it is the history hook's
// source of truth.
var PoolFindingsRel = filepath.Join("sources", "pool", "findings.txt")

// RecordReview reads the pool findings for the review at reviewDir, derives one
// Record per finding (stamped with run time ts), and appends them to the history
// ledger at histPath. It returns the number of records appended.
//
// A missing pool findings file is treated as "nothing to record" (0, nil): a
// history write must never turn an otherwise-successful review into a failure.
// A findings file that parses to zero findings appends nothing and creates no
// ledger file.
func RecordReview(histPath, reviewDir string, ts time.Time) (int, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, PoolFindingsRel))
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

	records := make([]Record, 0, len(res.Findings))
	for _, f := range res.Findings {
		records = append(records, Record{
			Timestamp: ts,
			Package:   PackageOf(f.File),
			Severity:  f.Severity,
			ID:        FindingID(f.File, f.Line, f.Problem),
			File:      f.File,
			Category:  f.Category,
		})
	}
	if err := Append(histPath, records); err != nil {
		return 0, err
	}
	return len(records), nil
}
