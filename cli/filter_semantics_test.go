package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// debt list's filter flags each document their matching mode in --help:
// severity/status/origin are exact, category is a case-insensitive substring,
// component is a path-segment prefix.
func TestDebtList_FilterHelpStatesMatchingMode(t *testing.T) {
	_, help := execCmdCapture(t, "debt", "list", "--help")
	for _, flag := range []string{"severity", "status", "origin"} {
		assert.Regexp(t, `--`+flag+` string\s+filter by `+flag+` \(exact`, help,
			"--%s help must state exact matching", flag)
	}
	assert.Contains(t, help, "substring match", "--category keeps its documented substring mode")
	assert.Contains(t, help, "path prefix", "--component keeps its documented prefix mode")
}

// leaderboard --model matches like personas search --model: a case-insensitive
// substring, so an unambiguous fragment selects the model (previously it
// required the full exact id — the two commands disagreed).
func TestLeaderboard_ModelFilterSubstringCaseInsensitive(t *testing.T) {
	isolate(t)
	storeLeaderboardRec(t, 1, "bruce", "claude-sonnet-4-6")
	storeLeaderboardRec(t, 1, "diana", "gpt-4o")

	code, out := execCmdCapture(t, "leaderboard", "--model", "SONNET")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "bruce")
	require.NotContains(t, out, "diana", "a case-insensitive substring --model must exclude non-matching models")
}
