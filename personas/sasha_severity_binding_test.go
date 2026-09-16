package personas

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSashaPredicateRule_GatesCriticalOnDemonstratedReachability pins the one
// security-specific hazard the shared predicate-exhaustiveness lens creates once
// it is voiced as sasha.
//
// sasha's rubric grants CRITICAL for "directly exploitable injection, auth
// bypass, or secret leak on a reachable path". A bullet that tells sasha the
// sibling arm IS the way in hands that rubric its own predicate pre-satisfied,
// before any evidence is gathered — so a bare field asymmetry between two guard
// arms arrives already qualified for the top severity, and the 3-call tool
// budget plus "report the finding anyway at reduced confidence" then push it out
// the door. otto.md carries the same lens purely descriptively, which is what
// proves the asserted consequence was never forced by the format. The bullet
// must bind CRITICAL to a reachable path the reviewer can actually show.
func TestSashaPredicateRule_GatesCriticalOnDemonstratedReachability(t *testing.T) {
	body, err := fs.ReadFile(files, "sasha.md")
	require.NoError(t, err, "reading embedded sasha.md")

	ruleLine, err := predicateRuleLineUnderFocus(string(body))
	require.NoError(t, err, "sasha.md must carry the predicate-exhaustiveness rule under ## Focus")

	require.Containsf(t, ruleLine, "CRITICAL only",
		"sasha's predicate-exhaustiveness bullet does not bind its top severity: %q — the lens "+
			"points sasha at a validation or authorization guard and sasha's rubric grants "+
			"CRITICAL on a reachable path, so the bullet itself must require a reachable path "+
			"the reviewer can demonstrate before that severity is available",
		ruleLine)
}
