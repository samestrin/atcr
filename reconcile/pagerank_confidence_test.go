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

// TestReconcile_SymmetricTripleDoesNotPromote locks the float boundary: three
// models that all pairwise agree form a vertex-transitive (symmetric) graph whose
// PageRank converges to EXACTLY 1/N for every node, so the strict > baseline test
// promotes nothing. A regression that introduced float drift at this boundary
// would spuriously promote isolated findings in any fully-corroborated run.
func TestReconcile_SymmetricTripleDoesNotPromote(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "alpha"),
			mf("HIGH", "c.go", 30, "shared issue three here", "fix", "security", 15, "e", "alpha"),
			mf("HIGH", "d.go", 40, "isolated alpha only finding", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "beta"),
			mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "beta"),
		}},
		{Name: "gamma", Findings: []Finding{
			mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "gamma"),
			mf("HIGH", "c.go", 30, "shared issue three here", "fix", "security", 15, "e", "gamma"),
		}},
	}
	res := Reconcile(sources, recAt())
	byFile := map[string]Merged{}
	for _, m := range res.Findings {
		byFile[m.File] = m
	}
	eq(t, byFile["d.go"].Confidence, ConfMedium, "symmetric K3 → no node exceeds baseline → no promotion")
}

// TestReconcile_IsolatedFindingFromNeverAgreedModelStaysMedium pins the common
// path: alpha and beta corroborate one issue (so the run HAS agreement and the
// authority map is non-empty), but gamma never agreed with anyone. gamma's
// isolated finding must stay MEDIUM — its reviewer is absent from the authority
// map (zero-value lookup), so promoteByAuthority is a no-op for it. Without this
// test nothing pins the absent-reviewer branch of promoteByAuthority.
func TestReconcile_IsolatedFindingFromNeverAgreedModelStaysMedium(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "beta"),
		}},
		{Name: "gamma", Findings: []Finding{
			mf("HIGH", "c.go", 30, "gamma isolated finding text", "fix", "security", 15, "e", "gamma"),
		}},
	}
	res := Reconcile(sources, recAt())
	byFile := map[string]Merged{}
	for _, m := range res.Findings {
		byFile[m.File] = m
	}
	eq(t, byFile["a.go"].Confidence, ConfHigh, "alpha+beta agreement → HIGH")
	eq(t, byFile["c.go"].Confidence, ConfMedium, "isolated finding from never-agreed gamma stays MEDIUM")
}

// TestReconcile_AuthorityWiredRunIsDeterministic runs Reconcile twice over
// identical input with the authority feature active (alpha is the central model
// whose isolated finding gets promoted) and asserts byte-identical Findings —
// pinning determinism at the full pipeline level, not just the bare pageRank().
func TestReconcile_AuthorityWiredRunIsDeterministic(t *testing.T) {
	build := func() Result {
		sources := []Source{
			{Name: "alpha", Findings: []Finding{
				mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "alpha"),
				mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "alpha"),
				mf("HIGH", "c.go", 30, "isolated alpha only finding", "fix", "security", 15, "e", "alpha"),
			}},
			{Name: "beta", Findings: []Finding{
				mf("HIGH", "a.go", 10, "shared issue one here", "fix", "security", 15, "e", "beta"),
			}},
			{Name: "gamma", Findings: []Finding{
				mf("HIGH", "b.go", 20, "shared issue two here", "fix", "security", 15, "e", "gamma"),
			}},
		}
		return Reconcile(sources, recAt())
	}
	deepEq(t, build().Findings, build().Findings, "authority-wired Reconcile is deterministic")
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

// countAuthorityFlips derives the number of authority-driven MEDIUM→HIGH
// promotions directly from the findings: a single-reviewer finding is MEDIUM by
// the vote-count rule (ConfidenceFor(1)==ConfMedium), so the only way it can end
// HIGH is promoteByAuthority flipping it. This independent recount is the oracle
// the Summary.AuthorityPromoted counter must match (AC2).
func countAuthorityFlips(res Result) int {
	n := 0
	for _, m := range res.Findings {
		if len(m.Reviewers) == 1 && m.Confidence == ConfHigh {
			n++
		}
	}
	return n
}

// TestReconcile_AuthorityPromotedSummaryCountsFlips is the AC2 behavior: the
// Summary.AuthorityPromoted counter equals the number of findings
// promoteByAuthority actually flipped MEDIUM→HIGH in the run. The fixture is the
// asymmetric multi-reviewer case where exactly one isolated finding (alpha's
// c.go) is promoted while beta's isolated finding stays MEDIUM, so the counter
// must read 1 and must agree with the independent recount.
func TestReconcile_AuthorityPromotedSummaryCountsFlips(t *testing.T) {
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
	eq(t, res.Summary.AuthorityPromoted, 1, "exactly one isolated finding (alpha's c.go) promoted MEDIUM→HIGH")
	eq(t, res.Summary.AuthorityPromoted, countAuthorityFlips(res), "counter must match independently recounted flips")
}

// TestReconcile_AuthorityPromotedZeroWhenNoPromotion pins AC4 on the counter
// itself: a symmetric two-model run promotes nothing, so the counter is 0 (no
// spurious counting when confidence assignment is unchanged).
func TestReconcile_AuthorityPromotedZeroWhenNoPromotion(t *testing.T) {
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
	eq(t, res.Summary.AuthorityPromoted, 0, "symmetric authority promotes nothing → counter 0")
	eq(t, res.Summary.AuthorityPromoted, countAuthorityFlips(res), "counter must match independently recounted flips")
}

// TestReconcile_AuthorityPromotedZeroWhenNoAgreement is the backward-compat
// counter invariant: with no cross-model agreement the authority map is empty,
// promotion is a no-op, and the counter stays 0.
func TestReconcile_AuthorityPromotedZeroWhenNoAgreement(t *testing.T) {
	sources := []Source{
		{Name: "alpha", Findings: []Finding{
			mf("HIGH", "a.go", 10, "alpha distinct finding alpha", "fix", "security", 15, "e", "alpha"),
		}},
		{Name: "beta", Findings: []Finding{
			mf("HIGH", "b.go", 20, "beta distinct finding beta", "fix", "security", 15, "e", "beta"),
		}},
	}
	res := Reconcile(sources, recAt())
	eq(t, res.Summary.AuthorityPromoted, 0, "no agreement → empty authority map → counter 0")
}
