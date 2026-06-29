package reconcile

import "testing"

// TestReconcile_AuthorityPromotesIsolatedHighAuthorityFinding is the AC3
// behavior: alpha agrees with beta on one issue and with gamma on another, making
// alpha the central (above-baseline-authority) model for the run. alpha's
// otherwise-isolated finding is therefore promoted MEDIUM→HIGH, while beta's
// isolated finding — beta sits below the 1/N baseline — stays MEDIUM.
func TestReconcile_AuthorityPromotesIsolatedHighAuthorityFinding(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "alpha"),
			mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "alpha"),
			mf("HIGH", "c.go", 30, "isolated alpha only finding", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "beta"),
			mf("HIGH", "d.go", 40, "isolated beta only finding", "fix", "security", 15, "e", "beta"),
		}},
		{Name: "gamma", Findings: []Finding{
			mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "gamma"),
		}},
	}
	res := Reconcile(sources, recAt())
	length(t, res.Findings, 4, "two merges (a.go,b.go) + two isolated (c.go,d.go)")

	// All HIGH severity → sorted by file: a.go, b.go, c.go, d.go.
	byFile := map[string]Merged{}
	for _, m := range res.Findings {
		byFile[m.File] = m
	}
	eq(t, byFile["a.go"].Confidence, ConfHigh, "alpha+beta agreement → HIGH")
	eq(t, byFile["b.go"].Confidence, ConfHigh, "alpha+gamma agreement → HIGH")
	eq(t, byFile["c.go"].Confidence, ConfHigh, "isolated finding from high-authority alpha promoted to HIGH")
	eq(t, byFile["d.go"].Confidence, ConfMedium, "isolated finding from below-baseline beta stays MEDIUM")

	// Promotion must not touch the reviewer set — it only adjusts confidence.
	deepEq(t, byFile["c.go"].Reviewers, []string{"alpha"}, "single reviewer unchanged by promotion")
}

// TestReconcile_SymmetricAuthorityDoesNotPromote proves a regular (symmetric)
// agreement graph promotes nothing: in a two-model run both models sit exactly at
// the 1/N baseline, and the strict > comparison fails. This protects every
// existing two-reviewer fixture from silent confidence drift.
func TestReconcile_SymmetricAuthorityDoesNotPromote(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "alpha"),
			mf("HIGH", "c.go", 30, "isolated alpha only finding", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "beta"),
		}},
	}
	res := Reconcile(sources, recAt())
	byFile := map[string]Merged{}
	for _, m := range res.Findings {
		byFile[m.File] = m
	}
	eq(t, byFile["a.go"].Confidence, ConfHigh, "two reviewers → HIGH")
	eq(t, byFile["c.go"].Confidence, ConfMedium, "symmetric authority does not exceed baseline → no promotion")
}

// TestReconcile_NoAgreementLeavesConfidenceUnchanged is the backward-compat
// invariant: with no cross-model agreement anywhere in the run the authority graph
// is empty and confidence is exactly the pre-13.3 vote-count result.
func TestReconcile_NoAgreementLeavesConfidenceUnchanged(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "alpha distinct finding alpha", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "b.go", 20, "beta distinct finding beta", "fix", "security", 15, "e", "beta"),
		}},
	}
	res := Reconcile(sources, recAt())
	length(t, res.Findings, 2, "two isolated findings, no merge")
	for _, m := range res.Findings {
		eq(t, m.Confidence, ConfMedium, "isolated finding with no run agreement stays MEDIUM")
	}
}
