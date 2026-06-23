package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvidenceSepConstant(t *testing.T) {
	assert.Equal(t, "; ", EvidenceSep)
}

func TestFixAttributionPrefixConstant(t *testing.T) {
	assert.Equal(t, "fix by ", FixAttributionPrefix)
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, HasFixAttribution(tc.evidence, tc.executor))
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, AppendFixAttribution(tc.evidence, tc.executor))
		})
	}
}
