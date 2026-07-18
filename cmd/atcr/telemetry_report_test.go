package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runQualityReportCmd drives newQualityReportCmd()'s RunE directly with args,
// capturing stdout. It reads the isolated .atcr/debt store under the test's
// working directory, so callers must isolate(t) (and optionally seedQualityRecord)
// first. It mirrors runPreview's direct-RunE style but needs no telemetry client —
// the report never sends.
func runQualityReportCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newQualityReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.ParseFlags(args); err != nil {
		return out.String(), err
	}
	// Run first, THEN read the buffer: a `return out.String(), cmd.RunE(...)` would
	// evaluate out.String() before RunE writes to it (left-to-right operand order).
	err := cmd.RunE(cmd, cmd.Flags().Args())
	return out.String(), err
}

// rankFixture is the shared three-pair fixture: distinct dismissal rates 0.9, 0.5,
// 0.05, supplied out of rank order so the render must sort it.
func rankFixture() []localdebt.QualityRow {
	return []localdebt.QualityRow{
		{Persona: "beta", Model: "gpt-4", DismissedCount: 1, ConfirmedCount: 19},  // 0.05
		{Persona: "alpha", Model: "gpt-4", DismissedCount: 9, ConfirmedCount: 1},  // 0.90
		{Persona: "alpha", Model: "claude", DismissedCount: 1, ConfirmedCount: 1}, // 0.50
	}
}

// TestQualityReport_RankedByDismissalRateDescending covers AC 04-01 Scenario 1/3:
// the markdown table lists rows sorted by dismissal rate descending with the exact
// per-row cell formatting, and the heading names the ranking basis.
func TestQualityReport_RankedByDismissalRateDescending(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderQualityReport(&buf, rankFixture(), "md"))
	out := buf.String()

	assert.Contains(t, strings.ToLower(out), "dismissal rate", "heading/columns must name the ranking basis")

	iHigh := strings.Index(out, "| alpha | gpt-4 | 9 | 1 | 90.0% |")
	iMid := strings.Index(out, "| alpha | claude | 1 | 1 | 50.0% |")
	iLow := strings.Index(out, "| beta | gpt-4 | 1 | 19 | 5.0% |")
	require.GreaterOrEqual(t, iHigh, 0, "high-dismissal row must render with exact cells")
	require.GreaterOrEqual(t, iMid, 0, "mid-dismissal row must render with exact cells")
	require.GreaterOrEqual(t, iLow, 0, "low-dismissal row must render with exact cells")
	assert.Less(t, iHigh, iMid, "0.90 row must precede 0.50 row")
	assert.Less(t, iMid, iLow, "0.50 row must precede 0.05 row")
}

// TestQualityReport_JSONFormatMatchesMDRankOrder covers AC 04-01 Scenario 2: JSON
// renders the same rows in the same rank order, fields limited to persona, model,
// dismissed/confirmed counts, and dismissal rate.
func TestQualityReport_JSONFormatMatchesMDRankOrder(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderQualityReport(&buf, rankFixture(), "json"))

	var got []qualityReportRow
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 3)

	assert.Equal(t, qualityReportRow{Persona: "alpha", Model: "gpt-4", DismissedCount: 9, ConfirmedCount: 1, DismissalRate: 0.9}, got[0])
	assert.Equal(t, qualityReportRow{Persona: "alpha", Model: "claude", DismissedCount: 1, ConfirmedCount: 1, DismissalRate: 0.5}, got[1])
	assert.Equal(t, qualityReportRow{Persona: "beta", Model: "gpt-4", DismissedCount: 1, ConfirmedCount: 19, DismissalRate: 0.05}, got[2])

	// The JSON object must expose exactly the five allowlisted keys — no leaked field.
	var raw []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &raw))
	require.Len(t, raw, 3)
	for _, m := range raw {
		assert.Len(t, m, 5, "each row exposes exactly persona, model, dismissed_count, confirmed_count, dismissal_rate")
		for _, k := range []string{"persona", "model", "dismissed_count", "confirmed_count", "dismissal_rate"} {
			_, ok := m[k]
			assert.True(t, ok, "row must carry key %q", k)
		}
	}
}

// TestQualityReport_TiedRatesTieBreakDeterministic covers AC 04-01 EC2: equal
// dismissal rates tie-break by persona then model ascending, byte-for-byte stable.
func TestQualityReport_TiedRatesTieBreakDeterministic(t *testing.T) {
	rows := []localdebt.QualityRow{
		{Persona: "zeta", Model: "m1", DismissedCount: 1, ConfirmedCount: 1},
		{Persona: "alpha", Model: "m2", DismissedCount: 1, ConfirmedCount: 1},
		{Persona: "alpha", Model: "m1", DismissedCount: 2, ConfirmedCount: 2},
	}
	var b1, b2 bytes.Buffer
	require.NoError(t, renderQualityReport(&b1, rows, "md"))
	require.NoError(t, renderQualityReport(&b2, rows, "md"))
	assert.Equal(t, b1.String(), b2.String(), "identical input must render byte-identically")

	out := b1.String()
	i1 := strings.Index(out, "| alpha | m1 |")
	i2 := strings.Index(out, "| alpha | m2 |")
	i3 := strings.Index(out, "| zeta | m1 |")
	require.GreaterOrEqual(t, i1, 0)
	require.GreaterOrEqual(t, i2, 0)
	require.GreaterOrEqual(t, i3, 0)
	assert.Less(t, i1, i2, "alpha/m1 before alpha/m2 (model ascending tie-break)")
	assert.Less(t, i2, i3, "alpha/* before zeta/* (persona ascending tie-break)")
}

// TestQualityReport_UnsupportedFormatUsageError covers AC 04-01 Error Scenario 1:
// a bad --format is a usage error (exit 2) raised BEFORE any data read — proven by
// a corrupt store that would exit 1 if reached still yielding exit 2.
func TestQualityReport_UnsupportedFormatUsageError(t *testing.T) {
	isolate(t)
	corruptDebtStore(t)

	_, err := runQualityReportCmd(t, "--format", "xml")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "unknown format is a usage error (exit 2), validated before the read")
}

// TestQualityReport_UnderlyingReadErrorExitsOne covers AC 04-01 Error Scenario 2: a
// present-but-unreadable store surfaces a wrapped error at exit 1 (not a panic).
func TestQualityReport_UnderlyingReadErrorExitsOne(t *testing.T) {
	isolate(t)
	corruptDebtStore(t)

	require.NotPanics(t, func() {
		_, err := runQualityReportCmd(t, "--format", "md")
		require.Error(t, err)
		assert.Equal(t, 1, exitCode(err), "a read failure is exit 1, distinct from the exit-2 usage class")
	})
}

// TestQualityReport_EmptyAggregationNoDataMessage_MD covers AC 04-03: an empty
// store renders a clean human-readable no-data message at exit 0.
func TestQualityReport_EmptyAggregationNoDataMessage_MD(t *testing.T) {
	isolate(t)
	out, err := runQualityReportCmd(t, "--format", "md")
	require.NoError(t, err)
	assert.Contains(t, out, "No quality-signal data", "empty md render must be a clear no-data state")
}

// TestQualityReport_EmptyAggregationNoDataMessage_JSON covers AC 04-03: empty JSON
// is a well-formed [], never null.
func TestQualityReport_EmptyAggregationNoDataMessage_JSON(t *testing.T) {
	isolate(t)
	out, err := runQualityReportCmd(t, "--format", "json")
	require.NoError(t, err)
	assert.Equal(t, "[]", strings.TrimSpace(out), "empty JSON must be [] not null")
	assert.NotContains(t, out, "null")
}

// TestQualityReport_EmptyDoesNotConflateWithReadFailure covers AC 04-03: the
// no-data state (exit 0) and a genuine read failure (exit 1) are distinct.
func TestQualityReport_EmptyDoesNotConflateWithReadFailure(t *testing.T) {
	t.Run("empty store exits zero", func(t *testing.T) {
		isolate(t)
		_, err := runQualityReportCmd(t, "--format", "json")
		require.NoError(t, err)
	})
	t.Run("unreadable store exits one", func(t *testing.T) {
		isolate(t)
		corruptDebtStore(t)
		_, err := runQualityReportCmd(t, "--format", "json")
		require.Error(t, err)
		assert.Equal(t, 1, exitCode(err))
	})
}

// TestQualityReport_SubsequentRunWithDataRendersFullTable covers AC 04-01: after
// seeding terminal records the report renders the aggregated row.
func TestQualityReport_SubsequentRunWithDataRendersFullTable(t *testing.T) {
	isolate(t)

	first, err := runQualityReportCmd(t, "--format", "md")
	require.NoError(t, err)
	assert.Contains(t, first, "No quality-signal data", "before any data, the no-data state renders")

	seedQualityRecord(t, "alpha", "gpt-4", "wontfix", "a.go")
	seedQualityRecord(t, "alpha", "gpt-4", "resolved", "b.go")

	out, err := runQualityReportCmd(t, "--format", "md")
	require.NoError(t, err)
	assert.Contains(t, out, "| alpha | gpt-4 | 1 | 1 | 50.0% |", "one dismissed + one confirmed → rate 50%")
}

// TestQualityReport_MarkdownCellsEscapePipeAndNewline locks the 4.2.A defense-in-
// depth fix: a persona/model carrying a pipe or newline is escaped so it cannot
// break the markdown table structure (persona/model are content-free, so this is
// hygiene, not a privacy leak).
func TestQualityReport_MarkdownCellsEscapePipeAndNewline(t *testing.T) {
	rows := []localdebt.QualityRow{
		{Persona: "a|b", Model: "m\n1", DismissedCount: 1, ConfirmedCount: 1},
	}
	var buf bytes.Buffer
	require.NoError(t, renderQualityReport(&buf, rows, "md"))
	out := buf.String()

	// Exactly one data row (5 columns → the row line has 6 pipes from the template
	// plus the escaped literal rendered as "\|", which is not a column separator).
	assert.Contains(t, out, `| a\|b | m 1 | 1 | 1 | 50.0% |`, "pipe escaped, newline flattened to a space")
	assert.NotContains(t, out, "m\n1", "a raw newline must never reach a table cell")
}

// corruptDebtStore makes DefaultDir(".") a regular FILE so os.ReadDir fails with a
// non-ENOENT error, forcing localdebt.ReadAll to return an error (the present-but-
// unreadable store case). Its parent is created first.
func corruptDebtStore(t *testing.T) {
	t.Helper()
	debtDir := localdebt.DefaultDir(".")
	require.NoError(t, os.MkdirAll(filepath.Dir(debtDir), 0o755))
	require.NoError(t, os.WriteFile(debtDir, []byte("not a directory"), 0o644))
}

// TestQualityReport_DirFlagReadsExplicitStoreWithoutChdir pins the --dir parity
// with `debt resolve`/`debt compact` (TD cmd/atcr/telemetry_report.go:62):
// quality-report must accept --dir pointing at an explicit fixture store so
// md/json parity tests (and scripts driving all three commands) need no chdir.
// The isolated CWD store stays empty, so rendered rows can only come from --dir.
func TestQualityReport_DirFlagReadsExplicitStoreWithoutChdir(t *testing.T) {
	isolate(t)

	dir := filepath.Join(t.TempDir(), "debt")
	seedQualityRecordAt(t, dir, "alpha", "gpt-4", "wontfix", "a.go")
	seedQualityRecordAt(t, dir, "alpha", "gpt-4", "resolved", "b.go")

	out, err := runQualityReportCmd(t, "--dir", dir, "--format", "md")
	require.NoError(t, err)
	assert.Contains(t, out, "| alpha | gpt-4 | 1 | 1 | 50.0% |",
		"--dir must point the report at the explicit fixture store, not DefaultDir(\".\")")
}

// seedQualityRecordAt is seedQualityRecord's explicit-dir counterpart: it appends
// one terminal local-debt record to the given store dir so --dir tests can seed a
// fixture store outside the chdir'd DefaultDir("."). Distinct files per call keep
// StampID distinct (records sharing an ID would fold together and undercount).
func seedQualityRecordAt(t *testing.T, dir, persona, model, status, file string) {
	t.Helper()
	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-07-01T10:00:00Z-seed",
		Timestamp:     "2026-07-01T10:00:00Z",
		Severity:      "LOW",
		File:          file,
		Line:          1,
		Problem:       "problem-" + file,
		Reviewers:     []string{persona},
		Model:         model,
		Status:        status,
	}
	rec.StampID()
	require.NoError(t, localdebt.Append(dir, rec))
}
