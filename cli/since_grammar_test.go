package cli

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSinceGrammarIsShared asserts that `--since` means exactly one thing across
// every command that accepts it. Two independent parsers used to back the same
// flag name: history delegated to time.ParseDuration (so "3m" was three MINUTES)
// while scorecard read integer+d/w/m (so "3m" was three MONTHS) — the same input
// silently selecting windows ~43,200x apart. The grammars must now agree on both
// the accepted set and the resulting duration.
func TestSinceGrammarIsShared(t *testing.T) {
	for _, in := range []string{"3m", "48h", "1.5d", "30d", "2w", "90m", "1h30m", "7d"} {
		t.Run(in, func(t *testing.T) {
			h, herr := history.ParseSince(in)
			s, serr := scorecard.ParseSince(in)

			assert.Equal(t, herr == nil, serr == nil,
				"input %q: history accepted=%v but leaderboard accepted=%v — one grammar, one answer", in, herr == nil, serr == nil)
			if herr == nil && serr == nil {
				assert.Equal(t, h, s, "input %q parses to different windows", in)
			}
		})
	}
}

// TestSinceGrammarRejectsClockUnits pins the chosen grammar: window flags take
// calendar-ish units only (d/w/m). Bare h/m/s are rejected outright rather than
// silently reinterpreted, which is what made "3m" ambiguous in the first place.
func TestSinceGrammarRejectsClockUnits(t *testing.T) {
	for _, in := range []string{"48h", "1h30m", "30s", "1.5d"} {
		t.Run(in, func(t *testing.T) {
			_, err := history.ParseSince(in)
			assert.Error(t, err, "%q must be a usage error, not a silently reinterpreted window", in)
		})
	}
}

// TestSinceGrammarMonthsAreMonths is the headline regression: "3m" is three
// 30-day months everywhere, never three minutes.
func TestSinceGrammarMonthsAreMonths(t *testing.T) {
	h, err := history.ParseSince("3m")
	require.NoError(t, err)
	assert.Equal(t, float64(90*24), h.Hours(), "history --since 3m must be 3 months")

	s, err := scorecard.ParseSince("3m")
	require.NoError(t, err)
	assert.Equal(t, float64(90*24), s.Hours(), "leaderboard --since 3m must be 3 months")
}

// TestSinceAllDisablesWindowOnBothCommands asserts the no-window sentinel is
// part of the shared grammar rather than a leaderboard-only extra.
func TestSinceAllDisablesWindowOnBothCommands(t *testing.T) {
	h, herr := history.ParseSince("all")
	require.NoError(t, herr, `history must accept "all"`)
	s, serr := scorecard.ParseSince("all")
	require.NoError(t, serr, `leaderboard must accept "all"`)
	assert.Equal(t, h, s, `"all" must mean the same window on both commands`)
	assert.Greater(t, h.Hours(), float64(100*365*24), `"all" must be an effectively unbounded window`)
}

// TestSinceHelpShowsDefaultOnBothCommands: history registered an empty cobra
// default, so `--help` advertised no window at all while the code silently
// applied 90d. A default the user cannot see is a default they cannot reason
// about.
func TestSinceHelpShowsDefaultOnBothCommands(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want string
	}{
		{"history", `(default "90d")`},
		{"leaderboard", `(default "30d")`},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			_, out := execCmdCapture(t, tc.cmd, "--help")
			require.True(t, strings.Contains(out, "--since"), "help must document --since")
			assert.Contains(t, out, tc.want, "%s --help must show the --since default", tc.cmd)
		})
	}
}
