package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/scorecard"
)

// storeRecord appends a scorecard record to the per-user store resolved under the
// isolated HOME (isolate() must run first). Mirrors how `atcr reconcile` would
// have written it, so the command-under-test reads it back through DefaultDir().
func storeRecord(t *testing.T, rec scorecard.Record) {
	t.Helper()
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	require.NoError(t, scorecard.Append(dir, rec))
}

func reviewerRec(runID, reviewer, model string, raised, corroborated int) scorecard.Record {
	return scorecard.Record{
		SchemaVersion:        scorecard.SchemaVersion,
		RecordType:           scorecard.RecordTypeReviewer,
		RunID:                runID,
		Reviewer:             reviewer,
		Model:                model,
		Role:                 "reviewer",
		FindingsRaised:       raised,
		FindingsCorroborated: corroborated,
		FindingsSolo:         raised - corroborated,
		CorroborationRate:    0.5,
		CostUSD:              0.0234,
		TokensIn:             14200,
		TokensOut:            4000,
		LatencyMS:            3400,
	}
}

func TestScorecardCmd_ResolveByRunID(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-abc123"
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))
	storeRecord(t, reviewerRec(runID, "diana", "gpt-4o", 8, 3))

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
	require.Contains(t, out, "diana")
}

func TestScorecardCmd_TableRendering(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-abc123"
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	for _, col := range []string{"REVIEWER", "MODEL", "RAISED", "CORROBORATED", "SOLO", "CORR%", "COST", "LATENCY"} {
		require.Contains(t, out, col, "table must include column %q", col)
	}
}

func TestScorecardCmd_VerificationColumns(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-ver"
	rec := reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7)
	v, r := 4, 1
	rate := 0.8
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &r
	rec.SurvivedSkepticRate = &rate
	storeRecord(t, rec)

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	for _, col := range []string{"VERIFIED", "REFUTED", "SURV%"} {
		require.Contains(t, out, col, "verification column %q must show when data present", col)
	}
}

func TestScorecardCmd_NoVerificationColumnsWhenAbsent(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-nover"
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "VERIFIED", "verification columns hidden when no record carries verification data")
}

func TestScorecardCmd_ResolveByPath(t *testing.T) {
	isolate(t)
	// A review dir with reconciled/summary.json drives run_id reconstruction:
	// run_id == reconciled_at + "-" + base(reviewDir).
	reviewDir := filepath.Join(".atcr", "reviews", "2026-06-14_abc")
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(reviewDir, "reconciled", "summary.json"),
		[]byte(`{"reconciled_at":"2026-06-14T10:00:00Z"}`), 0o644))

	runID := "2026-06-14T10:00:00Z-2026-06-14_abc"
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))

	code, out := execCmdCapture(t, "scorecard", reviewDir)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
}

func TestScorecardCmd_NoRecordsFound(t *testing.T) {
	isolate(t)
	storeRecord(t, reviewerRec("2026-06-13T08:00:00Z-other", "bruce", "m", 1, 0))

	code, out := execCmdCapture(t, "scorecard", "2026-06-14T10:00:00Z-missing")
	require.Equal(t, 1, code, "no matching records is exit 1")
	require.Contains(t, out, "no scorecard records found")
}

func TestScorecardCmd_CorruptedJSONL(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-corrupt"
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))
	// Append a malformed line to the same month file.
	f, err := os.OpenFile(filepath.Join(dir, "2026-06.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, _ = f.WriteString("{not valid json\n")
	require.NoError(t, f.Close())
	storeRecord(t, reviewerRec(runID, "diana", "gpt-4o", 8, 3))

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
	require.Contains(t, out, "diana", "valid records render despite a corrupt line")
}

func TestScorecardCmd_NoArgs(t *testing.T) {
	isolate(t)
	code, _ := execCmdCapture(t, "scorecard")
	require.Equal(t, 2, code, "missing argument is a usage error")
}

func TestScorecardCmd_InvalidRunID(t *testing.T) {
	isolate(t)
	code, _ := execCmdCapture(t, "scorecard", "garbage-not-a-runid")
	require.Equal(t, 2, code, "a bare id that is not a valid run_id is a usage error")
}
