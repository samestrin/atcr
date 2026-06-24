package reconcile

import "testing"

func TestVerification_Valid_AcceptsEnumValues(t *testing.T) {
	for _, v := range []string{VerdictConfirmed, VerdictRefuted, VerdictUnverifiable} {
		ver := Verification{Verdict: v, Skeptic: "agent"}
		if !ver.Valid() {
			t.Errorf("Valid() must return true for enum verdict %q", v)
		}
	}
}

func TestVerification_Valid_RejectsOutOfEnum(t *testing.T) {
	for _, v := range []string{"", "CONFIRMED", "unknown", "yes", "maybe"} {
		ver := Verification{Verdict: v, Skeptic: "agent"}
		if ver.Valid() {
			t.Errorf("Valid() must return false for non-enum verdict %q", v)
		}
	}
}

func TestVerification_Valid_RequiresNonEmptySkeptic(t *testing.T) {
	ver := Verification{Verdict: VerdictConfirmed, Skeptic: ""}
	if ver.Valid() {
		t.Error("Valid() must return false when Skeptic is empty")
	}
}
