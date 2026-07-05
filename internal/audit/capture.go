package audit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/samestrin/atcr/internal/stream"
)

// poolFindingsRel is the review-directory-relative path to the merged, 8-column
// per-source pool findings file that every review run writes via WritePool. It
// is the single findings artifact guaranteed to exist on every run (reconciled
// findings.json exists only when reconcile runs), so it is the audit summary's
// source of truth — the same source internal/history reads.
var poolFindingsRel = filepath.Join("sources", "pool", "findings.txt")

// RecordReview builds exactly one audit Record for the review at reviewDir and
// appends it to the ledger at auditPath, stamped with the run timestamp ts, the
// pull-request number pr (0 = none), and the resolved base/head SHAs. The
// Findings field summarizes the run's distinct findings counted by severity.
//
// Exactly one record is written per call, unconditionally (Epic 19.1 AC1): a
// missing or empty pool findings file yields an empty severity summary, not a
// skipped write, so the audit trail records every review run — including clean
// ones and runs off the findings path. It returns the number of records
// appended (always 1 on success).
func RecordReview(auditPath, reviewDir string, ts time.Time, pr int, base, head string) (int, error) {
	findings, err := summarize(reviewDir)
	if err != nil {
		return 0, err
	}
	rec := Record{
		Timestamp: ts,
		PR:        pr,
		Base:      base,
		Head:      head,
		Findings:  findings,
	}
	if err := Append(auditPath, []Record{rec}); err != nil {
		return 0, err
	}
	return 1, nil
}

// summarize reads the pool findings for the review at reviewDir and returns the
// count of DISTINCT findings by normalized severity, or nil when there are none.
// A finding caught by N reviewers appears N times in the pool; it is deduped by
// (file,line,problem) keeping the highest severity, so the summary is not
// inflated by reviewer multiplicity (mirrors internal/history.RecordReview so
// the audit summary and the history ledger agree on distinct-finding counts).
//
// A missing pool file is treated as zero findings (nil, nil): an audit write
// must never turn an otherwise-successful review into a failure. Malformed pool
// rows are skipped with a stderr warning, matching the history hook.
func summarize(reviewDir string) (map[string]int, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pool findings: %w", err)
	}

	res, err := stream.ParseSource(data)
	if err != nil {
		return nil, fmt.Errorf("parsing pool findings: %w", err)
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintf(os.Stderr, "atcr: warning: audit: skipped %d malformed pool row(s); they will not appear in the audit summary\n", len(res.Skipped))
	}

	// Dedupe by (file,line,problem) keeping the highest severity per the canonical
	// ranking, so a finding's stored severity is deterministic regardless of pool
	// row order.
	highest := make(map[string]string, len(res.Findings)) // finding key -> highest severity seen
	for _, f := range res.Findings {
		key := f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Problem
		if prev, ok := highest[key]; ok {
			if stream.SeverityRank[stream.NormalizeSeverity(f.Severity)] > stream.SeverityRank[stream.NormalizeSeverity(prev)] {
				highest[key] = f.Severity
			}
			continue
		}
		highest[key] = f.Severity
	}
	if len(highest) == 0 {
		return nil, nil
	}

	counts := make(map[string]int, 4)
	for _, sev := range highest {
		counts[stream.NormalizeSeverity(sev)]++
	}
	return counts, nil
}
