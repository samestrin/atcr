package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/scorecard"
)

// exportTestCmd is a bare command whose stdout is discarded, so a success-path export
// does not spray its JSON envelope across the test log. runLeaderboardExport writes
// through cmd.OutOrStdout(), which defaults to os.Stdout when unset.
func exportTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(io.Discard)
	return c
}

// storeLeaderboardRec writes a reviewer record at a given age (days before now)
// under the isolated store, so leaderboard filtering can be exercised end-to-end.
func storeLeaderboardRec(t *testing.T, ageDays int, reviewer, model string) {
	t.Helper()
	storeLeaderboardRecAt(t, time.Now().UTC().AddDate(0, 0, -ageDays), reviewer, model)
}

// storeLeaderboardRecAt is storeLeaderboardRec with an explicit timestamp, for a
// test that must place a record relative to a window boundary rather than at a
// whole number of days back. The timestamp drives both the run_id (which the
// --since filter parses) and the month file it lands in.
func storeLeaderboardRecAt(t *testing.T, at time.Time, reviewer, model string) {
	t.Helper()
	ts := at.UTC().Format(time.RFC3339)
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
	require.Equal(t, 2, env.SubmissionSchema)
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
	require.Equal(t, 2, env.SubmissionSchema)

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

// unreadableMonthFileFor replaces the month file for the given instant with a
// bound Unix domain socket (symlinked into the store) and returns its name, so a
// test can prove the leaderboard never opened it. os.Open on a socket fails
// regardless of the effective uid, unlike mode 0o000 which is ignored by root.
// The instant must be the same one passed to the store write, so the helper
// targets the same month stem the writer used. The socket is bound under /tmp
// because Unix domain socket paths have a small length budget and the isolated
// scorecard dir is usually too deep.
func unreadableMonthFileFor(t *testing.T, at time.Time) string {
	t.Helper()
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	name := at.UTC().Format("2006-01") + ".jsonl"
	target := filepath.Join(dir, name)
	require.FileExists(t, target, "the month file must exist before it is replaced")

	sockDir, err := os.MkdirTemp("/tmp", "atcr-*")
	require.NoError(t, err, "creating short socket directory")
	sockPath := filepath.Join(sockDir, "sock")
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err, "creating unix socket fixture")

	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Symlink(sockPath, target))

	t.Cleanup(func() {
		_ = l.Close()
		_ = os.Remove(target)
		_ = os.Remove(sockPath)
		_ = os.Remove(sockDir)
	})
	return name
}

// TestLeaderboardCmd_OutOfWindowMonthFileNeverOpened is the epic's core assertion:
// a --since window selects month FILES before opening them, so a month outside the
// window is never read. The out-of-window file is locked at 0o000 — an all-history
// read fails on it (see the sibling test below), so a clean exit 0 proves the file
// was skipped at selection rather than read and filtered away afterwards.
func TestLeaderboardCmd_OutOfWindowMonthFileNeverOpened(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "recent", "m")
	ancient := time.Now().UTC().AddDate(0, 0, -400)
	storeLeaderboardRecAt(t, ancient, "ancient", "m") // ~13 months back: its own month file
	locked := unreadableMonthFileFor(t, ancient)

	code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
	require.Equal(t, 0, code, "the out-of-window month file must never be opened: %s", out)
	require.Contains(t, out, "recent")
	require.NotContains(t, out, locked, "no diagnostic may name a file the window excluded")
	require.NotContains(t, out, "failed to read scorecard store")
}

// TestLeaderboardCmd_SinceAllStillOpensEveryMonthFile is the control for the test
// above: with the window disabled, the same unreadable file IS opened and its
// failure surfaces. Without this pair, the assertion above would also pass if the
// window silently swallowed read errors.
func TestLeaderboardCmd_SinceAllStillOpensEveryMonthFile(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "recent", "m")
	ancient := time.Now().UTC().AddDate(0, 0, -400)
	storeLeaderboardRecAt(t, ancient, "ancient", "m")
	unreadableMonthFileFor(t, ancient)

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

// TestLeaderboardCmd_EmptyWindowProbeSurfacesReadFailure covers the failure mode
// the empty-store probe introduces: the window legitimately finds nothing, so the
// probe reads the whole store — and that read can fail. An unreadable month file
// must surface as a read error, never be reported as the graceful "no data yet"
// state, which would tell a user with a broken store that they have no history.
func TestLeaderboardCmd_EmptyWindowProbeSurfacesReadFailure(t *testing.T) {
	isolate(t)
	ancient := time.Now().UTC().AddDate(0, 0, -400)
	storeLeaderboardRecAt(t, ancient, "ancient", "m") // out of window: the probe alone reads it
	unreadableMonthFileFor(t, ancient)

	code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
	require.Equal(t, 1, code, "an unreadable store is a failure, not an empty store: %s", out)
	require.Contains(t, out, "failed to read scorecard store")
	require.NotContains(t, out, "No scorecard data found", "a store that cannot be read is not a store with no data")
}

// TestLeaderboardCmd_OutOfWindowLockedFileDependsOnWindowState keeps the two
// divergent outcomes for the same locked out-of-window month file in one place.
// With a populated window the file is never opened and the command succeeds; with
// an empty window the probe must read the whole store and surfaces the failure.
func TestLeaderboardCmd_OutOfWindowLockedFileDependsOnWindowState(t *testing.T) {
	cases := []struct {
		name        string
		storeRecent bool
		wantCode    int
		wantContain string
		wantNot     []string
	}{
		{
			name:        "populated window ignores locked out-of-window file",
			storeRecent: true,
			wantCode:    0,
			wantContain: "recent",
			wantNot:     []string{"failed to read scorecard store"},
		},
		{
			name:        "empty window probe surfaces locked out-of-window file",
			storeRecent: false,
			wantCode:    1,
			wantContain: "failed to read scorecard store",
			wantNot:     []string{"No scorecard data found"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			if tc.storeRecent {
				storeLeaderboardRec(t, 1, "recent", "m")
			}
			ancient := time.Now().UTC().AddDate(0, 0, -400)
			storeLeaderboardRecAt(t, ancient, "ancient", "m")
			unreadableMonthFileFor(t, ancient)

			code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
			require.Equal(t, tc.wantCode, code, out)
			require.Contains(t, out, tc.wantContain)
			for _, s := range tc.wantNot {
				require.NotContains(t, out, s)
			}
		})
	}
}

// TestLeaderboardCmd_FutureRecordVisibility keeps the accepted asymmetry of a
// file-selecting window: a lone future-stamped record is read by the empty-window
// probe and shown, while a future-stamped record alongside an in-window record is
// excluded by monthOverlapsWindow's fail-closed upper edge.
func TestLeaderboardCmd_FutureRecordVisibility(t *testing.T) {
	cases := []struct {
		name        string
		storeRecent bool
		wantCode    int
		wantContain string
		wantNot     []string
	}{
		{
			name:        "lone future record is shown via the empty-window probe",
			storeRecent: false,
			wantCode:    0,
			wantContain: "future",
			wantNot:     []string{"no records match filters"},
		},
		{
			name:        "future record is hidden when an in-window record exists",
			storeRecent: true,
			wantCode:    0,
			wantContain: "recent",
			wantNot:     []string{"future"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			future := time.Now().UTC().AddDate(0, 2, 0)
			storeLeaderboardRecAt(t, future, "future", "m")
			if tc.storeRecent {
				storeLeaderboardRec(t, 1, "recent", "m")
			}

			code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
			require.Equal(t, tc.wantCode, code, out)
			require.Contains(t, out, tc.wantContain)
			for _, s := range tc.wantNot {
				require.NotContains(t, out, s)
			}
		})
	}
}

// TestLeaderboardCmd_MalformedInWindowFileDiagnosticOnce pins that the empty-store
// probe does not re-emit diagnostics the windowed read already printed. A file
// that decodes to zero records but is non-empty is opened once by ReadSince and
// once by the probe's ReadAll; the probe must discard its diagnostics writer so
// the user sees exactly one warning.
func TestLeaderboardCmd_MalformedInWindowFileDiagnosticOnce(t *testing.T) {
	isolate(t)
	dir, err := scorecard.DefaultDir()
	require.NoError(t, err)
	name := time.Now().UTC().Format("2006-01") + ".jsonl"
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("{not valid json\n"), 0o600))

	code, out := execCmdCapture(t, "leaderboard", "--since", "30d")
	require.Equal(t, 0, code, "a store with only malformed data is the empty-store state: %s", out)
	count := strings.Count(out, scorecard.MsgMalformedSkip)
	require.Equal(t, 1, count, "malformed-file diagnostic must be emitted exactly once, got %d:\n%s", count, out)
}

// TestLeaderboardCmd_InWindowMonthFileButOutsideDayCutoffExcluded pins that the
// windowed read narrows I/O without replacing the filter: ReadSince selects whole
// calendar months, so the month CONTAINING the cutoff is read entirely, and only
// the day-precision ApplyFilters boundary excludes the records inside it that
// predate the cutoff. Both halves matter — if the excluded record's file were
// never opened, this would pass for the wrong reason and prove nothing about the
// two composing.
//
// The older record is placed just before the cutoff inside the cutoff's own month
// rather than at a fixed age, so the month file is guaranteed in-window on every
// calendar day. A fixed age only lands there on days late enough in the month,
// which would leave this contract unexercised for most of a month's CI runs.
func TestLeaderboardCmd_InWindowMonthFileButOutsideDayCutoffExcluded(t *testing.T) {
	isolate(t)
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -1) // matches the --since 1d below

	// A minute before the cutoff is in the cutoff's month except when the cutoff
	// itself sits in that month's first minute; the month's own start instant is
	// then the latest qualifying timestamp.
	older := cutoff.Add(-time.Minute)
	if older.Month() != cutoff.Month() {
		older = time.Date(cutoff.Year(), cutoff.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	if !older.Before(cutoff) {
		t.Skip("cutoff landed exactly on its month's first instant, leaving no room before it")
	}

	storeLeaderboardRecAt(t, now, "recent", "m")
	storeLeaderboardRecAt(t, older, "sameMonthFileButOlder", "m")
	require.Equal(t, cutoff.Format("2006-01"), older.Format("2006-01"),
		"the excluded record must live in the cutoff's month file, which the window always opens")

	code, out := execCmdCapture(t, "leaderboard", "--since", "1d")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "recent")
	require.NotContains(t, out, "sameMonthFileButOlder",
		"the month file is read whole, but the day-precision --since boundary still applies")
}

// TestLeaderboardCmd_ExportStillReadsAllHistory pins the export path's exemption
// from the window. Windowing --export would be invisible to every other export
// test (they use an empty store or --since all, both zero-window), yet it would
// swap the no-match failure for the empty-store one on a store whose data all
// predates the window — the two export errors the command deliberately keeps
// distinct. Deleting the window computation's `!export` guard must fail here.
func TestLeaderboardCmd_ExportStillReadsAllHistory(t *testing.T) {
	isolate(t)
	// ~13 months back, so the record's month file is outside the 30d window on
	// every calendar day. A 45-day-old record would share the cutoff's month
	// whenever the cutoff falls late enough in it, and a windowed export would
	// then still read it — leaving the exemption unproven for part of the month.
	storeLeaderboardRec(t, 400, "ancient", "m")

	code, out := execCmdCapture(t, "leaderboard", "--export", "--since", "30d")
	require.Equal(t, 1, code, "no record survives the export filters: %s", out)
	require.Contains(t, out, "no records match the export filters",
		"the record was read and then filtered out, not missed by a windowed read")
	require.NotContains(t, out, "no scorecard data yet",
		"a windowed export would misreport a populated store as empty")
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

// The production envelope's version moved to 2 for a BENCHMARK-side reason, and
// board acceptance of version 2 is an explicitly unverified coordination item. A
// production submitter reads this flag's help and nothing else, so the bump has to be
// visible here — otherwise the only notice lives in a doc they have no reason to open.
func TestLeaderboardExportHelpNamesTheSchemaVersion(t *testing.T) {
	_, stdout, stderr := execCmdSplit(t, "leaderboard", "--help")
	help := stdout + stderr

	require.Contains(t, help, "--export", "precondition: the flag is documented in help")
	require.Contains(t, help, fmt.Sprintf("submission_schema %d", scorecard.SubmissionSchema),
		"a production submitter must learn the envelope version changed — "+
			"asserted against the constant, not a literal, so the next bump cannot leave a stale pair")
	require.Contains(t, help, "pinned to",
		"and that a board pinned to an older version needs updating")
}

// `benchmark export` hard-rejects a run-result whose reviewer identity carries a Cc/Cf
// rune, but `leaderboard --export` is the SIBLING producer into the same public
// envelope, through the same scrubField, with no such guard — so the invariant
// "no invisible rune survives into the published document" held for one producer and
// not the other. This is a divergence epic 35.16.6.2 created, not a pre-existing
// regression: before it, neither producer checked.
func TestRunLeaderboardExport_RejectsNonPrintingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rec    scorecard.Record
		offend string
	}{
		{
			name: "model carrying a zero-width space",
			rec: scorecard.Record{
				SchemaVersion: 1, RecordType: scorecard.RecordTypeReviewer, RunID: "2026-08-29T00:00:00Z-a",
				Reviewer: "greta", Model: "claude-son\u200Bnet", FindingsRaised: 3, FindingsCorroborated: 2,
			},
			offend: "claude-son\u200Bnet",
		},
		{
			// Reviewer is the field Export scrubs into the envelope's `persona`.
			name: "reviewer carrying a bidi override",
			rec: scorecard.Record{
				SchemaVersion: 1, RecordType: scorecard.RecordTypeReviewer, RunID: "2026-08-29T00:00:00Z-b",
				Reviewer: "gre\u202Eta", Model: "claude-sonnet", FindingsRaised: 3, FindingsCorroborated: 2,
			},
			offend: "gre\u202Eta",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runLeaderboardExport(exportTestCmd(), []scorecard.Record{tc.rec}, scorecard.FilterOpts{}, "")
			require.Error(t, err, "an identity the public scrub cannot carry intact must not publish")
			require.Contains(t, err.Error(), "non-printing rune")
			// %q, not %s: rendering a bidi override raw would reorder the operator's own
			// terminal with the very defect being reported.
			require.Contains(t, err.Error(), fmt.Sprintf("%q", tc.offend))
			// The run_id locator comes from the same untrusted store record, so it is
			// escaped too — a raw one would let a crafted id reorder the report itself.
			require.Contains(t, err.Error(), fmt.Sprintf("%q", tc.rec.RunID))
		})
	}
}

// The guard checks what would actually PUBLISH, not what the store happens to hold.
// Export filters internally, so a pre-filter check would hard-fail an export whose
// envelope was clean — a rejection the operator could clear only by deleting unrelated
// history.
func TestRunLeaderboardExport_IgnoresNonPrintingIdentityInFilteredOutRecords(t *testing.T) {
	recs := []scorecard.Record{
		{
			SchemaVersion: 1, RecordType: scorecard.RecordTypeReviewer, RunID: "2026-08-29T00:00:00Z-a",
			Reviewer: "greta", Model: "claude-sonnet", FindingsRaised: 3, FindingsCorroborated: 2,
		},
		{
			SchemaVersion: 1, RecordType: scorecard.RecordTypeReviewer, RunID: "2026-08-29T00:00:00Z-b",
			Reviewer: "bruce", Model: "gpt-5\u200B-mini", FindingsRaised: 3, FindingsCorroborated: 1,
		},
	}
	// --model selects only the clean record; the offending one never reaches the envelope.
	err := runLeaderboardExport(exportTestCmd(), recs, scorecard.FilterOpts{Model: "claude-sonnet"}, "")
	require.NoError(t, err, "a record the filters exclude never publishes, so it must not fail the export")
}
