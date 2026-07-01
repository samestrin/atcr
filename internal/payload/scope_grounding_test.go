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
// TestScopeRule_DiffDiscardsOnlyWhenGrounded pins the fix for the diff-ingestion
// path: PrepareReviewFromDiff supplies an empty Range so grounding is disabled,
// so the hard-drop discard promise in scopeChangedOnly only applies when
// grounding is active (live git-range reviews). The scope rule must state that
// condition explicitly instead of promising a discard that never happens.
func TestScopeRule_DiffDiscardsOnlyWhenGrounded(t *testing.T) {
	for _, mode := range []PayloadMode{ModeDiff, ModeBlocks} {
		r := ScopeRule(mode)
		if !strings.Contains(r, "grounding is active") && !strings.Contains(r, "git-range") {
			t.Errorf("ScopeRule(%s) must qualify the discard clause with the grounding condition; got %q", mode, r)
		}
	}
}

// TestScopeRule_FilesHardDropsUngrounded pins the fix for files mode: because
// files mode renders the whole head file, reviewers are more likely to cite
// genuine issues on unchanged-but-visible lines. The scope rule must state
// explicitly that such findings are hard-dropped unless tagged out-of-scope,
// matching the parity requirement with scopeChangedOnly.
func TestScopeRule_FilesHardDropsUngrounded(t *testing.T) {
	r := ScopeRule(ModeFiles)
	if !strings.Contains(r, "discarded") {
		t.Errorf("ScopeRule(files) must state that out-of-range findings are discarded; got %q", r)
	}
	if !strings.Contains(r, "out-of-scope") {
		t.Errorf("ScopeRule(files) must mention CATEGORY out-of-scope escape hatch; got %q", r)
	}
	if !strings.Contains(r, "FILE:LINE") {
		t.Errorf("ScopeRule(files) must reference FILE:LINE for the drop rule; got %q", r)
	}
}

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
