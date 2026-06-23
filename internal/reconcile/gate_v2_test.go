package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCountAtOrAbove_ExcludesRefuted: a refuted finding is never counted, even
// at its own severity (AC 03-05 Scenario 1).
func TestCountAtOrAbove_ExcludesRefuted(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "HIGH", Confidence: "HIGH"}},
		{Finding: Finding{Severity: "HIGH", Confidence: "HIGH"}},
		{Finding: Finding{Severity: "HIGH", Confidence: "HIGH"}},
		{Finding: Finding{Severity: "HIGH", Confidence: "LOW", Verification: &Verification{Verdict: "refuted", Skeptic: "s"}}},
		{Finding: Finding{Severity: "LOW", Confidence: "LOW"}},
	}
	assert.Equal(t, 3, CountAtOrAbove(findings, "HIGH", false), "3 non-refuted HIGH; refuted HIGH excluded")
}

// TestCountAtOrAbove_IncludesConfirmed: confirmed findings count at/above
// threshold (AC 03-05 Scenario 3 — unverifiable counts too, only refuted drops).
func TestCountAtOrAbove_IncludesConfirmed(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "confirmed", Skeptic: "s"}}},
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "unverifiable", Skeptic: "s"}}},
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "refuted", Skeptic: "s"}}},
	}
	assert.Equal(t, 2, CountAtOrAbove(findings, "HIGH", false), "confirmed + unverifiable count; refuted excluded")
}

// TestCountAtOrAbove_V1Finding_NilVerification: a v1 finding (no Verification
// block) counts as non-refuted (AC 03-05 EC1).
func TestCountAtOrAbove_V1Finding_NilVerification(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "HIGH"}},                                           // nil Verification
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: ""}}}, // empty verdict
	}
	assert.Equal(t, 2, CountAtOrAbove(findings, "HIGH", false), "nil and empty-verdict findings both count")
}

// TestCountAtOrAbove_RequireVerified: requireVerified counts only confirmed
// (VERIFIED) findings (AC 05-01 Scenario 2 / EC1 / EC2).
func TestCountAtOrAbove_RequireVerified(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "confirmed"}}},
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "unverifiable"}}},
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "refuted"}}},
		{Finding: Finding{Severity: "HIGH"}}, // v1 nil
	}
	assert.Equal(t, 1, CountAtOrAbove(findings, "HIGH", true), "only the confirmed finding counts when requireVerified")
}

// TestCountAtOrAbove_AllRefuted: all-refuted set returns 0 at any threshold
// (AC 03-05 EC4).
func TestCountAtOrAbove_AllRefuted(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "CRITICAL", Verification: &Verification{Verdict: "refuted"}}},
		{Finding: Finding{Severity: "HIGH", Verification: &Verification{Verdict: "refuted"}}},
		{Finding: Finding{Severity: "LOW", Verification: &Verification{Verdict: "refuted"}}},
	}
	assert.Equal(t, 0, CountAtOrAbove(findings, "LOW", false))
}

// TestCountAtOrAbove_OutOfScopePrecedence: out-of-scope is excluded regardless
// of a confirmed verdict and regardless of requireVerified (AC 05-01 EC4).
func TestCountAtOrAbove_OutOfScopePrecedence(t *testing.T) {
	findings := []Merged{
		{Finding: Finding{Severity: "CRITICAL", Category: CategoryOutOfScope, Verification: &Verification{Verdict: "confirmed"}}},
	}
	assert.Equal(t, 0, CountAtOrAbove(findings, "CRITICAL", false))
	assert.Equal(t, 0, CountAtOrAbove(findings, "CRITICAL", true))
}
