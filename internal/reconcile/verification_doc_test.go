package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVerificationDoc_ParsingIsDefensive pins the claims in docs/verification.md's
// "Parsing is defensive." paragraph that once drifted from the skeptic parser.
//
// The paragraph used to say an out-of-enum verdict "falls back to unverifiable",
// while parseVerdictCore keeps walking past a quoted out-of-enum example and
// grades a committed verdict behind it; `invalid_verdict:` fires only when
// nothing usable follows. It also named only one of the two notes markers and
// never said what a model-typed copy of either is rewritten to.
//
// The marker names are string literals: this package cannot import
// internal/verify (which imports it), and fragmentAfterToken /
// fragmentBeforeToken are unexported. internal/verify's tests pin the same
// literals (think_bare_closer_test.go:854,917,962), so a change to a token's
// name fails there and points back here.
func TestVerificationDoc_ParsingIsDefensive(t *testing.T) {
	line := docLineContaining(t, readDoc(t, "verification.md"), "Parsing is defensive.")

	for _, want := range []string{
		// The parser iterates past a decoy rather than stopping at it.
		"keeps reading past an out-of-enum verdict",
		"grades a usable verdict that follows it",
		"`invalid_verdict:` is reported only when nothing usable follows",
		// Both markers, what each means, and that each leads the notes.
		"`[verdict_after_unopened_closer: N prefix byte(s) discarded]`",
		// N counts the closer itself (invoke.go's discardedPrefix is everything up
		// to the graded text), so 15 bytes of draft plus `</think>` is 23.
		"N is the length of the dropped prefix: the text before the closer plus the closer tag itself",
		"`[verdict_before_unopened_closer: text after the closer ignored]`",
		"leads its notes with",
		// The inert form a model-typed token of either name is rewritten to.
		"`verdict-after-unopened-closer`",
		"`verdict-before-unopened-closer`",
	} {
		assert.Contains(t, line, want)
	}
}
