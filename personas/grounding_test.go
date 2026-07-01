package personas

import (
	"strings"
	"testing"
)

// TestBase_GroundingDirective pins Epic 14.1 AC1/AC3 into the shared base
// persona: every finding must cite an exact FILE:LINE drawn from the diff, code
// outside the changed lines must not be reported, and an ungrounded finding is
// discarded rather than merely flagged. The out-of-scope category is the only
// sanctioned escape hatch for a pre-existing issue in unchanged code.
func TestBase_GroundingDirective(t *testing.T) {
	base, err := Base()
	if err != nil {
		t.Fatalf("Base(): %v", err)
	}
	low := strings.ToLower(base)
	for _, want := range []string{"discarded", "changed lines", "out-of-scope"} {
		if !strings.Contains(low, want) {
			t.Errorf("base persona template missing grounding directive substring %q", want)
		}
	}
}
