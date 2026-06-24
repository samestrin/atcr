package reconcile

import "testing"

func TestEvidenceSepConstant(t *testing.T) {
	eq(t, EvidenceSep, "; ", "evidence separator")
}

func TestFixAttributionPrefixConstant(t *testing.T) {
	eq(t, FixAttributionPrefix, "fix by ", "fix attribution prefix")
}

func TestHasFixAttribution(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		executor string
		want     bool
	}{
		{"empty_evidence", "", "opus", false},
		{"exact_match", "fix by opus", "opus", true},
		{"joined_match", "Found by bruce; fix by opus", "opus", true},
		{"prefix_not_match", "Found by bruce; fix by opus", "op", false},
		{"prose_not_match", "reviewer suggested a fix by hand", "hand", false},
		{"multi_executor", "Found by bruce; fix by greta; fix by opus", "greta", true},
		{"multi_executor_second", "Found by bruce; fix by greta; fix by opus", "opus", true},
		{"whitespace_trim", "fix by  opus ", "opus", false}, // padded name ≠ plain name
		{"empty_name", "fix by ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq(t, HasFixAttribution(tc.evidence, tc.executor), tc.want, tc.name)
		})
	}
}

func TestAppendFixAttribution(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		executor string
		want     string
	}{
		{"empty_evidence", "", "opus", "fix by opus"},
		{"non_empty_evidence", "Found by bruce", "opus", "Found by bruce; fix by opus"},
		{"idempotent", "Found by bruce; fix by opus", "opus", "Found by bruce; fix by opus"},
		{"different_executor", "Found by bruce; fix by opus", "greta", "Found by bruce; fix by opus; fix by greta"},
		{"prefix_not_suppressed", "Found by bruce; fix by opus", "op", "Found by bruce; fix by opus; fix by op"},
		{"whitespace_only_evidence", "   ", "opus", "fix by opus"},
		{"empty_name_skipped", "Found by bruce", "", "Found by bruce"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq(t, AppendFixAttribution(tc.evidence, tc.executor), tc.want, tc.name)
		})
	}
}
