package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The degraded-input branches of the two new render helpers were both uncovered.
// cli/report.go always supplies debate.MaxUnresolvedAttempts, so the only caller
// that can reach attemptCountdown's bare-count arm is a FUTURE one — which is
// precisely the caller the fallback was written for, and precisely why nothing
// proved it renders anything sensible (TD internal/report/contested.go:180).
func TestAttemptCountdown_FallsBackToABareCountWhenTheCeilingIsUnknown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Unresolved attempts: attempt 2 of 3.", attemptCountdown(2, 3),
		"with the ceiling known the countdown says how close the item is to being withheld")

	got := attemptCountdown(2, 0)
	assert.Equal(t, "Unresolved attempts: attempt 2.", got,
		"a caller that did not supply the ceiling gets a bare count")
	assert.NotContains(t, got, " of ",
		"rendering 'of 0' would tell the operator the item is already past a ceiling of zero")

	// Deliberately NOT capped at the ceiling: a legacy record above it renders
	// honestly rather than being clamped into a lie.
	assert.Equal(t, "Unresolved attempts: attempt 5 of 3.", attemptCountdown(5, 3),
		"a count above the ceiling is a real legacy record and must render as it is")
}

// withheldSeverity's empty arm exists so a severity-less withheld item does not
// print a stray " ()" beside its path. Uncovered, so nothing stopped the empty
// string from rendering as an empty pair of parens in report.md.
func TestWithheldSeverity_EmptyRendersNothing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", withheldSeverity(""),
		"a withheld item with no severity must render no parenthetical at all")
	assert.Equal(t, " (HIGH)", withheldSeverity("HIGH"),
		"and a real severity keeps the same ' (SEV)' shape a ruling uses, so the two lists read alike")
}

// Both fallbacks together, through the renderer an operator actually reads: a
// withheld item carrying neither a severity nor a known ceiling must still produce
// a clean line. This is the shape that would have shipped a stray " ()" and an
// "of 0".
func TestWriteContestedSection_WithheldItemWithNoSeverityAndNoCeilingRendersCleanly(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writeContestedSection(&b, ContestedReport{
		Withheld: 1,
		WithheldItems: []Withheld{{
			File: "a.go", Line: 10, Problem: "nil deref", Reason: "unresolved_attempts_exhausted",
			UnresolvedAttempts: 3,
		}},
		// UnresolvedAttemptsCeiling deliberately left at 0 — the degraded caller.
	})
	out := b.String()

	require.Contains(t, out, "a.go", "the withheld item must still be identified")
	assert.NotContains(t, out, " ()",
		"an empty severity must not render as an empty parenthetical")
	assert.NotContains(t, out, "of 0",
		"an unknown ceiling must not render as a ceiling of zero")
	assert.Contains(t, out, "Unresolved attempts: attempt 3.",
		"the bare-count fallback is what the degraded caller gets")
	// The line must not end up with a dangling space before the newline either.
	for _, line := range strings.Split(out, "\n") {
		assert.Equal(t, strings.TrimRight(line, " "), line, "no trailing space on any rendered line")
	}
}
