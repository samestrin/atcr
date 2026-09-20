package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/benchmark"
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

// reviewerRec is the cli-side stand-in for a record the emitter wrote. Outcome
// is stamped for the reason sprint 36.0 added the field: trustPriorsSince scores
// only runs the lens got a fair attempt at, so a record carrying no outcome is
// excluded and any trust-rate assertion built on one silently measures nothing.
func reviewerRec(runID, reviewer, model string, raised, corroborated int) scorecard.Record {
	return scorecard.Record{
		SchemaVersion:        scorecard.SchemaVersion,
		Outcome:              benchmark.OutcomeFindings,
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

func TestScorecardCmd_BareMonthPrefixIsUsageError(t *testing.T) {
	isolate(t)
	code, _ := execCmdCapture(t, "scorecard", "2026-06")
	require.Equal(t, 2, code, "a bare YYYY-MM with no timestamp is a malformed run_id, not an empty run")
}

func TestScorecardCmd_SummaryUnreadableNoPathLeak(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	isolate(t)
	reviewDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(reviewDir, "reconciled"), 0o755))
	summaryPath := filepath.Join(reviewDir, "reconciled", "summary.json")
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{"reconciled_at":"2026-06-14T10:00:00Z"}`), 0o644))
	require.NoError(t, os.Chmod(summaryPath, 0o000))
	defer func() { _ = os.Chmod(summaryPath, 0o644) }() // restore for t.TempDir cleanup

	code, out := execCmdCapture(t, "scorecard", reviewDir)
	require.Equal(t, 1, code, "unreadable summary.json is a real failure, not a usage error")
	require.NotContains(t, out, summaryPath, "resolved absolute path of summary.json must not appear in error message")
}

func TestScorecardCmd_SlashBearingRunIDFallsBackToRunID(t *testing.T) {
	isolate(t)
	// A run_id-shaped arg that also contains a slash trips looksLikePath, sending
	// it down the review-dir path; that lookup finds no reconciled/summary.json. It
	// must then fall back to treating the arg as a run_id (it satisfies IsRunID)
	// rather than failing with a confusing "no reconciled/summary.json" usage error.
	runID := "2026-06-14T10:00:00Z-a/b"
	storeRecord(t, reviewerRec(runID, "bruce", "claude-sonnet-4-6", 12, 7))

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
}

func TestFormatPercent_ClampsOutOfRange(t *testing.T) {
	// A corrupt record may carry a rate outside [0,1]; formatPercent must clamp to
	// match the export path's clampRate rather than rendering nonsense like 500% or
	// a negative percentage.
	require.Equal(t, "100%", formatPercent(5.0), "above-range rate clamps to 100%")
	require.Equal(t, "0%", formatPercent(-0.5), "below-range rate clamps to 0%")
	require.Equal(t, "100%", formatPercent(1.0), "upper boundary unchanged")
	require.Equal(t, "0%", formatPercent(0.0), "lower boundary unchanged")
	require.Equal(t, "58%", formatPercent(0.58), "in-range rate rounds normally")
}

// TestRenderScorecard_ShowsDocShieldedOnlyWhenNonzero is the per-run twin of the
// leaderboard column: the same carve-out hides the same quantity here, and this
// table is the one an operator reads immediately after a review.
func TestRenderScorecard_ShowsDocShieldedOnlyWhenNonzero(t *testing.T) {
	base := scorecard.Record{
		SchemaVersion: scorecard.SchemaVersion, RecordType: scorecard.RecordTypeReviewer,
		RunID: "2026-09-02T00:00:00Z-a", Reviewer: "bruce", Model: "m",
		FindingsRaised: 6, FindingsCorroborated: 6, FindingsSolo: 0, CorroborationRate: 1,
	}

	t.Run("absent when every record is zero", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, renderScorecard(&buf, []scorecard.Record{base}))
		assert.NotContains(t, buf.String(), "DOC-SHIELDED")
	})

	t.Run("present with the count when any record is nonzero", func(t *testing.T) {
		shielded := base
		shielded.Reviewer = "greta"
		shielded.FindingsDocShielded = 4
		var buf bytes.Buffer
		require.NoError(t, renderScorecard(&buf, []scorecard.Record{base, shielded}))
		out := buf.String()
		require.Contains(t, out, "DOC-SHIELDED")
		assert.Regexp(t, `(?m)^greta\b.*\s4\s*$`, out, "greta's row must carry its shielded count")
		assert.Regexp(t, `(?m)^bruce\b.*\s0\s*$`, out, "bruce shows 0 rather than a blank")
	})
}

// TestScorecardCmd_AllTruncatedReviewerRendersNotMeasured pins the one surface a
// human reads against the omission the emitter exists to produce.
//
// internal/scorecard omits survived_skeptic_rate when no countable verdict
// survived — a published 0.0 is indistinguishable from a reviewer whose findings
// were ALL refuted, the strongest negative signal the metric carries. The table
// then gated SURV% on FindingsVerified, which Emit always sets under
// hasVerification, so the not-measured rendering was unreachable and `survived :=
// 0.0` printed 0%. The record says "not measured" and the table said "0%".
func TestScorecardCmd_AllTruncatedReviewerRendersNotMeasured(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-trunc"
	rec := reviewerRec(runID, "bruce", "claude-sonnet-4-6", 1, 0)
	// The all-truncated shape: verification ran, both counts present at zero, and
	// the rate deliberately absent.
	v, r := 0, 0
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &r
	rec.SurvivedSkepticRate = nil
	storeRecord(t, rec)

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "SURV%", "verification ran, so the columns still show")
	assert.Equal(t, "-", survCell(t, out, "bruce"),
		"an absent rate must render as not-measured — printing 0% republishes the ambiguity the emitter dropped the key to remove")
}

// TestScorecardCmd_RefutedReviewerStillRendersZeroPercent is the other half: a
// rate that is genuinely 0.0 is a real measurement and must still print. Without
// this, "render - when absent" could be satisfied by never printing 0% at all.
func TestScorecardCmd_RefutedReviewerStillRendersZeroPercent(t *testing.T) {
	isolate(t)
	runID := "2026-06-14T10:00:00Z-refuted"
	rec := reviewerRec(runID, "bruce", "claude-sonnet-4-6", 1, 0)
	v, r := 0, 3
	rate := 0.0
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &r
	rec.SurvivedSkepticRate = &rate
	storeRecord(t, rec)

	code, out := execCmdCapture(t, "scorecard", runID)
	require.Equal(t, 0, code, out)
	assert.Equal(t, "0%", survCell(t, out, "bruce"),
		"every one of this reviewer's findings was refuted — that IS a measured 0% and must not be hidden")
}

// survCell returns the SURV% cell of the named reviewer's row. Asserting on the
// whole table cannot work here: CORR% renders "50%", which contains "0%", so a
// substring check on the output passes and fails for the wrong reasons.
func survCell(t *testing.T, out, reviewer string) string {
	t.Helper()
	var header, row []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "REVIEWER" {
			header = f
		}
		if f[0] == reviewer {
			row = f
		}
	}
	require.NotNil(t, header, "no header row in output:\n%s", out)
	require.NotNil(t, row, "no row for reviewer %q in output:\n%s", reviewer, out)
	require.Equal(t, len(header), len(row), "header/row column mismatch:\n%s", out)
	for i, h := range header {
		if h == "SURV%" {
			return row[i]
		}
	}
	t.Fatalf("no SURV%% column in output:\n%s", out)
	return ""
}
