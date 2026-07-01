package payload

import (
	"strings"
	"testing"
)

// TestScopeRule_DiffDiscardsUngrounded pins Epic 14.1 AC3 onto the changed-only
// scope rule: the reviewer is told that a finding outside the changed lines is
// discarded (the new hard-drop grounding, not the old "flagged during
// reconciliation" soft signal) and that out-of-scope is the sanctioned escape
// hatch. The pre-existing "changed regions" / "Stay on the diff" phrasing that
// other tests depend on must survive.
func TestScopeRule_DiffDiscardsUngrounded(t *testing.T) {
	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		r := ScopeRule(mode)
		for _, want := range []string{"discarded", "out-of-scope"} {
			if !strings.Contains(r, want) {
				t.Errorf("ScopeRule(%s) missing grounding substring %q; got %q", mode, want, r)
			}
		}
		if !strings.Contains(r, "changed regions") || !strings.Contains(r, "Stay on the diff") {
			t.Errorf("ScopeRule(%s) lost pre-existing phrasing: %q", mode, r)
		}
	}
}
