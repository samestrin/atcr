package reconcile_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/scorecard"
)

// writeSourceFindings writes a v1-header findings.txt at sourcesDir/relPath.
// Duplicated (not imported) from the internal reconcile_test-package helper of
// the same name: this file lives in the external reconcile_test package so it
// can import internal/scorecard without an import cycle (internal/scorecard
// imports internal/reconcile), so it cannot reach the unexported package-level
// helper.
func writeSourceFindings(t *testing.T, sourcesDir, relPath, body string) {
	t.Helper()
	full := filepath.Join(sourcesDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte("# atcr-findings/v1\n"+body), 0o644))
}

// TestRunReconcile_AppliesScorecardTrustPriors (epic 35.9 AC1): a reviewer with
// >= DefaultTrustMinRuns scorecard history and a corroboration rate at the
// trust-exempt threshold survives the consensus filter on an otherwise
// uncorroborated singleton, with zero in-run corroboration and zero PageRank
// authority — proving RunReconcile actually resolves and attaches
// scorecard.TrustPriors, not just tolerates its absence.
func TestRunReconcile_AppliesScorecardTrustPriors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())

	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	for i := 0; i < scorecard.DefaultTrustMinRuns; i++ {
		require.NoError(t, scorecard.Append(dir, scorecard.Record{
			SchemaVersion:        1,
			RecordType:           scorecard.RecordTypeReviewer,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-r%02d", i),
			Reviewer:             "trusted",
			Model:                "m",
			Role:                 "reviewer",
			FindingsRaised:       1,
			FindingsCorroborated: 1, // rate 1.0, at/above trustHighThreshold
		}))
	}

	reviewDir := t.TempDir()
	// 3-reviewer panel (the consensus filter's activation floor); "trusted"'s
	// singleton has no in-run corroboration and no PageRank authority (no
	// reviewer pair agrees on anything).
	writeSourceFindings(t, filepath.Join(reviewDir, "sources"), "a/findings.txt",
		"MEDIUM|foo.go:10|possible nil deref on this path|fix|correctness|10|ev|trusted\n")
	writeSourceFindings(t, filepath.Join(reviewDir, "sources"), "b/findings.txt",
		"MEDIUM|bar.go:20|unused import lingers in this file|fix|style|10|ev|stranger\n")
	writeSourceFindings(t, filepath.Join(reviewDir, "sources"), "c/findings.txt",
		"MEDIUM|baz.go:30|request body is not validated|fix|correctness|10|ev|third\n")

	res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reconcile.Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, err)

	var trustedSurvived bool
	for _, m := range res.Findings {
		if m.File == "foo.go" {
			trustedSurvived = true
		}
	}
	assert.True(t, trustedSurvived,
		"a reviewer with a high scorecard-measured trust prior survives the consensus filter without in-run corroboration")
}
