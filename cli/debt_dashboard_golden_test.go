package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// updateDashboardGolden rewrites the dashboard golden files from the current
// render. Only ever run it against a render known to be correct: the goldens were
// captured from the pre-35.21 full-record read, and they are what pins the
// summary-path render as byte-identical to it (epic 35.21 AC3).
var updateDashboardGolden = flag.Bool("update-dashboard-golden", false, "rewrite cli/testdata/debt_dashboard_*.golden.md")

// debtDashboardGoldenCorpus is the shared corpus the byte-identity goldens are
// rendered from. It covers every rendered surface the summary path must keep
// identical: all six status buckets, an off-enum severity (the (unknown) row),
// secret-shaped tokens in the location and the problem (redaction), control
// bytes and raw HTML (sanitization), a pipe in the location, an empty and a
// malformed timestamp (the histogram's unknown bucket), a severity/date tie
// broken by location, and one id with history across shards — opened, resolved,
// then re-detected — so the fold, not the first occurrence, decides what renders.
func debtDashboardGoldenCorpus() []localdebt.Record {
	mk := func(runID, ts, status, sev, file string, line, est int, problem string) localdebt.Record {
		r := localdebt.Record{
			SchemaVersion: localdebt.SchemaVersion, RunID: runID, Timestamp: ts,
			Status: status, Severity: sev, File: file, Line: line, EstMinutes: est,
			Problem: problem, Fix: "f", Category: "c",
		}
		r.StampID()
		return r
	}
	recs := append([]localdebt.Record{}, debtSampleRecords()...)
	recs = append(recs,
		mk("2026-05-run", "2026-05-02T09:00:00Z", "", "HIGH", "internal/keys/sk-live0000SECRET111/app.go", 4, 20,
			"leaked sk-live0000SECRET222 in the header"),
		mk("2026-05-run", "2026-05-02T09:00:00Z", "", "HIGH", "internal/a.go", 3, 5,
			"erase\x1b[1Athis\x9bK and <img src=x onerror=alert(1)>   tail"),
		mk("2026-05-run", "2026-05-02T09:00:00Z", "", "HIGH", "internal/x | y.go", 1, 7, "pipe | in file"),
		mk("2026-05-run", "2026-05-02T09:00:00Z", "", "BLOCKER", "odd/sev/a.go", 1, 1, "off-enum severity"),
		mk("2026-05-run", "2026-05-02T09:00:00Z", "", "", "odd/sev/b.go", 2, 1, "empty severity"),
		mk("2026-07-run", "", "", "MEDIUM", "see the design doc", 0, 0, "no timestamp, prose file"),
		mk("2026-07-run", "2026-7x", "", "LOW", "main.go", 9, 3, "malformed timestamp"),
		mk("2026-07-run", "2026-07-01T00:00:00Z", localdebt.StatusWontfix, "CRITICAL", "a/d.go", 4, 0, "wontfix"),
		mk("2026-07-run", "2026-07-01T00:00:00Z", localdebt.StatusUnreproducible, "HIGH", "a/e.go", 5, 0, "unreproducible"),
		mk("2026-07-run", "2026-07-01T00:00:00Z", localdebt.StatusAttemptsExhausted, "HIGH", "a/f.go", 6, 45, "attempts exhausted"),
		mk("2026-07-run", "2026-07-01T00:00:00Z", localdebt.StatusDeferred, "LOW", "a/g.go", 7, 2, "deferred"),
	)

	// One id with history: opened in May, resolved in June, re-detected in July.
	// FoldRecords and FoldSummaries both keep the July open occurrence; a render
	// that showed the May text or counted it resolved would differ here.
	hist := mk("2026-05-run", "2026-05-20T09:00:00Z", "", "CRITICAL", "internal/hist/h.go", 12, 90, "first sighting")
	resolved := hist
	resolved.RunID, resolved.Timestamp, resolved.Status = "2026-06-run", "2026-06-20T09:00:00Z", localdebt.StatusResolved
	reopened := hist
	reopened.RunID, reopened.Timestamp, reopened.Problem = "2026-07-run", "2026-07-02T09:00:00Z", "re-detected text"
	recs = append(recs, hist, resolved, reopened)
	return recs
}

// TestDebtDashboard_GoldenRenderIsByteIdentical is epic 35.21 AC3: the dashboard
// read through the real store and the production command renders exactly the
// bytes the pre-35.21 full-record read rendered, at every --top shape: the
// suppressed list, a list shorter than the backlog, and one longer than it.
func TestDebtDashboard_GoldenRenderIsByteIdentical(t *testing.T) {
	dir := writeLocalDebt(t, debtDashboardGoldenCorpus()...)
	for _, top := range []int{0, 1, 3, 50} {
		t.Run(fmt.Sprintf("top=%d", top), func(t *testing.T) {
			got, err := runDebt(t, "dashboard", "--dir", dir, "--top", fmt.Sprint(top))
			require.NoError(t, err)

			path := filepath.Join("testdata", fmt.Sprintf("debt_dashboard_top%d.golden.md", top))
			if *updateDashboardGolden {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), got, "the dashboard render drifted from the pre-35.21 golden")
		})
	}
}
