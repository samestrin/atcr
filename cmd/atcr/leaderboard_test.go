package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/scorecard"
)

// storeLeaderboardRec writes a reviewer record at a given age (days before now)
// under the isolated store, so leaderboard filtering can be exercised end-to-end.
func storeLeaderboardRec(t *testing.T, ageDays int, reviewer, model string) {
	t.Helper()
	ts := time.Now().UTC().AddDate(0, 0, -ageDays).Format(time.RFC3339)
	runID := ts + "-" + reviewer
	storeRecord(t, scorecard.Record{
		SchemaVersion:        scorecard.SchemaVersion,
		RecordType:           scorecard.RecordTypeReviewer,
		RunID:                runID,
		Reviewer:             reviewer,
		Model:                model,
		Role:                 "reviewer",
		FindingsRaised:       10,
		FindingsCorroborated: 6,
		FindingsSolo:         4,
		CorroborationRate:    0.6,
		CostUSD:              0.05,
		LatencyMS:            1200,
	})
}

func TestLeaderboardCmd_TableDisplay(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")
	storeLeaderboardRec(t, 2, "diana", "gpt-4o")

	code, out := execCmdCapture(t, "leaderboard")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
	require.Contains(t, out, "diana")
	for _, col := range []string{"REVIEWER", "MODEL", "RUNS", "CORR%", "COST"} {
		require.Contains(t, out, col, "leaderboard must include column %q", col)
	}
}

func TestLeaderboardCmd_SinceFlag(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 2, "recent", "m")
	storeLeaderboardRec(t, 40, "ancient", "m")

	code, out := execCmdCapture(t, "leaderboard", "--since", "7d")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "recent")
	require.NotContains(t, out, "ancient", "--since 7d excludes the 40-day-old record")
}

func TestLeaderboardCmd_ModelFlag(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")
	storeLeaderboardRec(t, 1, "diana", "gpt-4o")

	code, out := execCmdCapture(t, "leaderboard", "--model", "claude-sonnet-4-6")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
	require.NotContains(t, out, "diana", "--model filters to the matching model only")
}

func TestLeaderboardCmd_EmptyStoreExit0(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "leaderboard")
	require.Equal(t, 0, code, "empty store is graceful, not an error")
	require.Contains(t, out, "No scorecard data found")
}

func TestLeaderboardCmd_NoFilterMatchExit1(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	code, out := execCmdCapture(t, "leaderboard", "--model", "nonexistent-model")
	require.Equal(t, 1, code, "records exist but filters match none → exit 1")
	require.Contains(t, out, "no records match filters")
}

func TestLeaderboardCmd_AllRecordsOlderThanDefaultWindow(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 45, "bruce", "m") // older than the default 30d window

	code, out := execCmdCapture(t, "leaderboard")
	require.Equal(t, 1, code, "data exists but all predates the default window → exit 1")
	require.Contains(t, out, "no records match filters")
	require.Contains(t, out, "window", "no-match message names the active window so hidden data is explained")
}

func TestLeaderboardCmd_InvalidSinceExit1(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "m")

	code, out := execCmdCapture(t, "leaderboard", "--since", "abc")
	require.Equal(t, 1, code, "an invalid --since value is a runtime error (exit 1)")
	require.Contains(t, out, "since")
}

func TestLeaderboardCmd_ExportFlag(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")
	storeLeaderboardRec(t, 2, "diana", "gpt-4o")

	code, out := execCmdCapture(t, "leaderboard", "--export")
	require.Equal(t, 0, code, out)
	// --export emits JSON, not the table: the table header must be absent.
	require.NotContains(t, out, "REVIEWER\t")
	var env struct {
		SubmissionSchema int `json:"submission_schema"`
		Reviewers        []struct {
			Persona string `json:"persona"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), "export stdout must be valid JSON: %s", out)
	require.Equal(t, 1, env.SubmissionSchema)
	require.Len(t, env.Reviewers, 2)
}

func TestLeaderboardCmd_OutputFlag(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	dest := filepath.Join(t.TempDir(), "nested", "deep", "submission.json")
	code, out := execCmdCapture(t, "leaderboard", "--export", "--output", dest)
	require.Equal(t, 0, code, out)
	// --output routes JSON to the file (creating parents), not stdout.
	require.NotContains(t, out, "submission_schema")

	data, err := os.ReadFile(dest)
	require.NoError(t, err, "output file must be created")
	var env struct {
		SubmissionSchema int `json:"submission_schema"`
	}
	require.NoError(t, json.Unmarshal(data, &env))
	require.Equal(t, 1, env.SubmissionSchema)

	info, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "output file must be 0600")
}

func TestLeaderboardCmd_OutputWithoutExportIsUsageError(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	// --output is meaningless for the table view; a bare --output must fail loudly
	// (exit 2) rather than silently leave the expected file unwritten.
	code, out := execCmdCapture(t, "leaderboard", "--output", filepath.Join(t.TempDir(), "x.json"))
	require.Equal(t, 2, code)
	require.Contains(t, out, "--output requires --export")
}

func TestLeaderboardCmd_OutputToDirectoryExit1(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	// --output pointing at an existing directory is a usable-path error (exit 1),
	// not a silent overwrite (AC 04-01 Error Scenario 2).
	dir := t.TempDir()
	code, out := execCmdCapture(t, "leaderboard", "--export", "--output", dir)
	require.Equal(t, 1, code)
	require.Contains(t, out, "directory")
}

// TestLeaderboardCmd_ExportOutputSystemDirRejected pins parity with report/review:
// an --output path under a system directory must be rejected by validation.FilePath
// at the input layer (usage error, exit 2) before any write is attempted.
func TestLeaderboardCmd_ExportOutputSystemDirRejected(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	code, out := execCmdCapture(t, "leaderboard", "--export", "--output", "/etc/atcr-export-test.json")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "system directories")
}

func TestLeaderboardCmd_ExportEmptyStoreExit1(t *testing.T) {
	isolate(t)
	// Unlike the table view (exit 0 on empty store), --export treats no matching
	// records as a failure (exit 1) with a distinct "no data yet" message (not the
	// filter-no-match guidance that advises widening --since).
	code, out := execCmdCapture(t, "leaderboard", "--export")
	require.Equal(t, 1, code)
	require.Contains(t, out, "reconcile", "empty store must guide user toward reconcile")
	require.NotContains(t, out, "no records match the export filters", "empty store must not show filter-no-match message")
}

func TestLeaderboardCmd_ExportNoFilterMatchExit1(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	code, out := execCmdCapture(t, "leaderboard", "--export", "--model", "nonexistent-model")
	require.Equal(t, 1, code)
	require.Contains(t, out, "no records match the export filters")
}

// errWriter is an io.Writer that always fails with a fixed error.
type errWriter struct{ err error }

func (e *errWriter) Write([]byte) (int, error) { return 0, e.err }

// TestRenderLeaderboard_WriteErrorPropagated verifies that renderLeaderboard returns
// the underlying writer's error. The tw.Flush() error path (tabwriter to bytes.Buffer)
// cannot be triggered in isolation because bytes.Buffer never returns an error;
// this test covers the final w.Write path and ensures errors are not discarded.
func TestLeaderboardCmd_SinceAllShowsOldRecords(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 45, "oldreviewer", "m") // older than the default 30d window

	code, out := execCmdCapture(t, "leaderboard", "--since", "all")
	require.Equal(t, 0, code, "--since all must disable the window and show all records: %s", out)
	require.Contains(t, out, "oldreviewer", "record older than 30d must appear with --since all")
}

func TestLeaderboardCmd_SinceAllExportIncludesOldRecords(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 45, "oldreviewer", "m")

	code, out := execCmdCapture(t, "leaderboard", "--export", "--since", "all")
	require.Equal(t, 0, code, "--export --since all must include old records: %s", out)
	var env struct {
		Reviewers []struct {
			Persona string `json:"persona"`
		} `json:"reviewers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), "export must be valid JSON: %s", out)
	require.NotEmpty(t, env.Reviewers, "old records must appear in export with --since all")
}

func TestRenderLeaderboard_WriteErrorPropagated(t *testing.T) {
	rows := []scorecard.LeaderboardRow{
		{Reviewer: "alice", Model: "m", Runs: 1, FindingsRaised: 5, FindingsCorroborated: 3, CorroborationRate: 0.6},
	}
	ew := &errWriter{err: errors.New("disk full")}
	err := renderLeaderboard(ew, rows)
	require.Error(t, err)
	require.Equal(t, "disk full", err.Error())
}

func TestRenderLeaderboard_NoErrorOnSuccess(t *testing.T) {
	rows := []scorecard.LeaderboardRow{
		{Reviewer: "alice", Model: "m", Runs: 1, FindingsRaised: 5, FindingsCorroborated: 3, CorroborationRate: 0.6},
	}
	require.NoError(t, renderLeaderboard(io.Discard, rows))
}

func TestLeaderboardCmd_ExportNoMatchSingleErrorLine(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6") // record exists; filter will exclude it
	// leaderboard --export with no matching filter must emit exactly one error message.
	// Before the fix, the Fprintln in runLeaderboardExport AND err.Error() appended
	// by the test harness produced two separate lines, causing this to fail.
	code, out := execCmdCapture(t, "leaderboard", "--export", "--model", "nonexistent")
	require.Equal(t, 1, code)
	require.NotContains(t, out, "Try widening --since", "duplicate Fprintln must be removed")
	require.Contains(t, out, "no records match the export filters")
}
