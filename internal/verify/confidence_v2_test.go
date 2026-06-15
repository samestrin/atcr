package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfidenceV2 covers the full verdict x v1-confidence matrix (AC 03-01).
// confidenceV2 is a pure mapping: confirmed promotes to VERIFIED regardless of
// the v1 level, refuted demotes to LOW, and every other verdict (unverifiable,
// empty, unknown) passes the v1 confidence through unchanged.
func TestConfidenceV2(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		verdict  string
		expected string
	}{
		{"confirmed/HIGH", "HIGH", "confirmed", "VERIFIED"},
		{"confirmed/MEDIUM", "MEDIUM", "confirmed", "VERIFIED"},
		{"confirmed/LOW", "LOW", "confirmed", "VERIFIED"},
		{"refuted/HIGH", "HIGH", "refuted", "LOW"},
		{"refuted/MEDIUM", "MEDIUM", "refuted", "LOW"},
		{"refuted/LOW", "LOW", "refuted", "LOW"},
		{"unverifiable/HIGH", "HIGH", "unverifiable", "HIGH"},
		{"unverifiable/MEDIUM", "MEDIUM", "unverifiable", "MEDIUM"},
		{"unverifiable/LOW", "LOW", "unverifiable", "LOW"},
		{"empty/HIGH", "HIGH", "", "HIGH"},
		{"empty/MEDIUM", "MEDIUM", "", "MEDIUM"},
		{"empty/LOW", "LOW", "", "LOW"},
		{"unknown/HIGH", "HIGH", "garbage", "HIGH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, confidenceV2(tt.v1, tt.verdict))
		})
	}
}

// TestConfidenceV2_VerifiedConstant pins the exported tier constant so callers
// (report rendering, gate) compare against a single source of truth.
func TestConfidenceV2_VerifiedConstant(t *testing.T) {
	assert.Equal(t, "VERIFIED", ConfidenceVerified)
}

// TestConfidenceV2_CaseInsensitiveVerdict guards against a skeptic emitting a
// non-canonical casing slipping past the mapping (3.3 robustness).
func TestConfidenceV2_CaseInsensitiveVerdict(t *testing.T) {
	assert.Equal(t, "VERIFIED", confidenceV2("MEDIUM", "Confirmed"))
	assert.Equal(t, "LOW", confidenceV2("HIGH", "REFUTED"))
}
