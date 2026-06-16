package main

import (
	"encoding/json"
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
		SchemaVersion int `json:"schema_version"`
		Records       []struct {
			Reviewer string `json:"reviewer"`
		} `json:"records"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), "export stdout must be valid JSON: %s", out)
	require.Equal(t, 1, env.SchemaVersion)
	require.Len(t, env.Records, 2)
}

func TestLeaderboardCmd_OutputFlag(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	dest := filepath.Join(t.TempDir(), "nested", "deep", "submission.json")
	code, out := execCmdCapture(t, "leaderboard", "--export", "--output", dest)
	require.Equal(t, 0, code, out)
	// --output routes JSON to the file (creating parents), not stdout.
	require.NotContains(t, out, "schema_version")

	data, err := os.ReadFile(dest)
	require.NoError(t, err, "output file must be created")
	var env struct {
		SchemaVersion int `json:"schema_version"`
	}
	require.NoError(t, json.Unmarshal(data, &env))
	require.Equal(t, 1, env.SchemaVersion)

	info, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "output file must be 0600")
}

func TestLeaderboardCmd_ExportEmptyStoreExit1(t *testing.T) {
	isolate(t)
	// Unlike the table view (exit 0 on empty store), --export treats no matching
	// records as a failure (exit 1) with the canonical no-match message.
	code, out := execCmdCapture(t, "leaderboard", "--export")
	require.Equal(t, 1, code)
	require.Contains(t, out, "No records match the specified filters. Try widening --since or removing filters.")
}

func TestLeaderboardCmd_ExportNoFilterMatchExit1(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")

	code, out := execCmdCapture(t, "leaderboard", "--export", "--model", "nonexistent-model")
	require.Equal(t, 1, code)
	require.Contains(t, out, "No records match the specified filters. Try widening --since or removing filters.")
}
