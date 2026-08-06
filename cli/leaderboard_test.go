package cli

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

// unreadableMonthFileFor makes the month file holding a record of the given age
// unreadable and returns its name, so a test can prove the leaderboard never
// opened it: an opened file at mode 0o000 fails the read, and the leaderboard
// surfaces that failure as an error. Nothing else can distinguish "not read" from
// "read and ignored" — a record simply missing from the output would also be
// explained by the in-memory filter, which is exactly what this epic is not about.
func unreadableMonthFileFor(t *testing.T, ageDays int) string {
	t.Helper()
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	name := time.Now().UTC().AddDate(0, 0, -ageDays).Format("2006-01") + ".jsonl"
	path := filepath.Join(dir, name)
	require.FileExists(t, path, "the month file must exist before it is locked")
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	return name
}

// TestLeaderboardCmd_OutOfWindowMonthFileNeverOpened is the epic's core assertion:
// a --since window selects month FILES before opening them, so a month outside the
// window is never read. The out-of-window file is locked at 0o000 — an all-history
// read fails on it (see the sibling test below), so a clean exit 0 proves the file
// was skipped at selection rather than read and filtered away afterwards.
func TestLeaderboardCmd_OutOfWindowMonthFileNeverOpened(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 0o000 does not block reads when running as root")
	}
	isolate(t)
	storeLeaderboardRec(t, 1, "recent", "m")
	storeLeaderboardRec(t, 400, "ancient", "m") // ~13 months back: its own month file
	locked := unreadableMonthFileFor(t, 400)

	code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
	require.Equal(t, 0, code, "the out-of-window month file must never be opened: %s", out)
	require.Contains(t, out, "recent")
	require.NotContains(t, out, locked, "no diagnostic may name a file the window excluded")
	require.NotContains(t, out, "failed to read scorecard store")
}

// TestLeaderboardCmd_SinceAllStillOpensEveryMonthFile is the control for the test
// above: with the window disabled, the same locked file IS opened and its failure
// surfaces. Without this pair, the assertion above would also pass if the window
// silently swallowed read errors.
func TestLeaderboardCmd_SinceAllStillOpensEveryMonthFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 0o000 does not block reads when running as root")
	}
	isolate(t)
	storeLeaderboardRec(t, 1, "recent", "m")
	storeLeaderboardRec(t, 400, "ancient", "m")
	unreadableMonthFileFor(t, 400)

	code, out := execCmdCapture(t, "leaderboard", "--since", "all")
	require.Equal(t, 1, code, "an all-history read still opens every month file: %s", out)
	require.Contains(t, out, "failed to read scorecard store")
}

// TestLeaderboardCmd_WindowedReadKeepsEmptyStoreAndNoMatchDistinct pins the two
// states a naive windowed read collapses: with records outside the window never
// entering the result set, a populated store looks identical to an empty one. They
// have different exit codes and different messages, and both run the same command
// path, so they are asserted together.
func TestLeaderboardCmd_WindowedReadKeepsEmptyStoreAndNoMatchDistinct(t *testing.T) {
	t.Run("empty store exits 0", func(t *testing.T) {
		isolate(t)
		code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
		require.Equal(t, 0, code, "no data at all is a graceful empty state: %s", out)
		require.Contains(t, out, "No scorecard data found")
	})

	t.Run("all data outside the window exits 1", func(t *testing.T) {
		isolate(t)
		storeLeaderboardRec(t, 400, "ancient", "m") // months outside a 30d window

		code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
		require.Equal(t, 1, code, "data exists but none is in the window: %s", out)
		require.Contains(t, out, "no records match filters")
		require.NotContains(t, out, "No scorecard data found", "a populated store is never the empty-store state")
	})
}

// TestLeaderboardCmd_InWindowMonthButOutsideDayCutoffExcluded pins that the
// windowed read narrows I/O without replacing the filter. ReadSince selects whole
// calendar months, so a record from earlier in the cutoff's own month is read;
// only the day-precision ApplyFilters boundary excludes it. Skipped near the start
// of a month, where a 7d window has no same-month-but-older day to test.
func TestLeaderboardCmd_InWindowMonthButOutsideDayCutoffExcluded(t *testing.T) {
	isolate(t)
	now := time.Now().UTC()
	if now.Day() < 12 {
		t.Skip("needs a day-of-month with room for an older same-month record outside a 7d window")
	}
	storeLeaderboardRec(t, 1, "recent", "m")
	storeLeaderboardRec(t, 10, "sameMonthButOlder", "m")

	code, out := execCmdCapture(t, "leaderboard", "--since", "7d")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "recent")
	require.NotContains(t, out, "sameMonthButOlder",
		"the month file is read whole, but the day-precision --since boundary still applies")
}

// TestLeaderboardCmd_SinceZeroShowsOldRecords pins the second no-window sentinel.
// "all" and "0" both map to an empty Since (leaderboard.go), which ApplyFilters
// treats as "no window" — and which the windowed read must turn back into an
// all-history read rather than into a zero-length window that hides every record.
func TestLeaderboardCmd_SinceZeroShowsOldRecords(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 45, "oldreviewer", "m") // older than the default 30d window

	code, out := execCmdCapture(t, "leaderboard", "--since", "0")
	require.Equal(t, 0, code, "--since 0 must disable the window and show all records: %s", out)
	require.Contains(t, out, "oldreviewer", "record older than 30d must appear with --since 0")
}

// TestLeaderboardCmd_InvalidSinceEmptyStoreExit0 pins the precedence between the
// empty-store check and the --since parse: the store is read BEFORE the window
// value is validated, so an empty store reports its graceful exit-0 state and the
// invalid value is never surfaced. Sizing a window from --since ahead of the read
// must not turn this into the exit-1 invalid-since error.
func TestLeaderboardCmd_InvalidSinceEmptyStoreExit0(t *testing.T) {
	isolate(t)

	code, out := execCmdCapture(t, "leaderboard", "--since", "abc")
	require.Equal(t, 0, code, "an empty store is graceful even with an invalid --since: %s", out)
	require.Contains(t, out, "No scorecard data found")
	require.NotContains(t, out, "invalid --since", "the empty-store state takes precedence over the window value")
}

// TestLeaderboardCmd_ExportInvalidSinceEmptyStoreReportsEmptyStore pins the same
// precedence on the export path, where the empty-store failure carries its own
// distinct message. --export never needs a window, so no window parsing may run
// ahead of this check either.
func TestLeaderboardCmd_ExportInvalidSinceEmptyStoreReportsEmptyStore(t *testing.T) {
	isolate(t)

	code, out := execCmdCapture(t, "leaderboard", "--export", "--since", "abc")
	require.Equal(t, 1, code, "an empty store fails the export: %s", out)
	require.Contains(t, out, "no scorecard data yet")
	require.NotContains(t, out, "invalid --since", "the empty-store export error takes precedence")
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
