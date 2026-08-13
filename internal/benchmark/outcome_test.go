package benchmark

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ValidOutcome is the checkpoint-resume trust boundary: it admits exactly the
// Outcome* wire values plus OutcomeUnknown (genuine absence), and nothing else. Two
// adversarial cases matter most: an arbitrary string (which would become an
// arbitrary key in the published outcomes tally) and the literal "unknown" — the
// TALLY label, never a stored value — whose acceptance would make a fabricated
// outcome indistinguishable from genuine absence.
func TestValidOutcome(t *testing.T) {
	for _, v := range []string{
		OutcomeUnknown, OutcomeFindings, OutcomeClean,
		OutcomeUnparseable, OutcomeTruncated, OutcomeIncomplete, OutcomeFailed,
	} {
		assert.True(t, ValidOutcome(v), "stored wire value %q must be valid", v)
	}
	for _, v := range []string{
		OutcomeUnknownLabel, // the tally label is not a storable value
		"fabricated", "CLEAN", "unknown ", "n/a",
	} {
		assert.False(t, ValidOutcome(v), "%q is not a storable outcome", v)
	}
}
