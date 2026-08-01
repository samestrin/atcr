package reconcile

import "testing"

// Epic 35.9: reconcile-time consumption of the per-reviewer trust prior
// (scorecard.TrustPriors, epic 35.8) — a singleton from a historically reliable
// reviewer survives the epic-14.2 consensus filter without in-run
// corroboration, and a singleton from a historically unreliable reviewer is
// demoted to ConfLow.

func TestTrustExempt_HighTrustSingletonSurvivesConsensusFilter(t *testing.T) {
	// 3-reviewer panel, zero in-run corroboration, zero PageRank authority (no
	// reviewer pair agrees on anything). "trusted" has a prior at the high
	// threshold and must reach findings.json; "stranger" has no prior and is
	// sidecarred as usual.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "trusted"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "stranger"),
		}},
		{Name: "c", Findings: []Finding{
			cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
		}},
	}
	priors := map[string]float64{"trusted": trustHighThreshold}
	res := Reconcile(sources, Options{TrustPriors: priors})

	isTrue(t, hasFinding(res, "foo.go"), "high-trust singleton reaches findings.json")
	isTrue(t, !hasFinding(res, "bar.go"), "a singleton from a reviewer with no prior is still sidecarred")
	eq(t, res.Summary.AuthorityPromoted, 0, "no PageRank authority promotion occurred")
}

// TestTrustExempt_Direct table-drives the exemption predicate directly, the way
// TestDemoteByTrust_Direct does for demotion. It pins the two guards none of the
// end-to-end tests above happen to exercise: a 2-reviewer finding (not a
// singleton, so never exempt) and an empty Reviewers slice. The empty-slice case
// is the one that matters for safety — len(f.Reviewers) != 1 short-circuits
// before f.Reviewers[0] is evaluated, so the index access cannot panic.
//
// That case is reachable end-to-end, not merely defensive: distinctReviewers
// (merge.go) drops empty reviewer names, so a group whose source findings all
// carry Reviewer: "" merges to a ConfMedium finding with Reviewers == [].
func TestTrustExempt_Direct(t *testing.T) {
	priors := map[string]float64{
		"reliable": trustHighThreshold,
		"flaky":    trustLowThreshold,
	}

	cases := []struct {
		name      string
		reviewers []string
		priors    map[string]float64
		want      bool
	}{
		{"a high-trust sole reviewer is exempt", []string{"reliable"}, priors, true},
		{"a mixed-case sole reviewer resolves to its lowercase prior key", []string{"Reliable"}, priors, true},
		{"a low-trust sole reviewer is not exempt", []string{"flaky"}, priors, false},
		{"a sole reviewer absent from priors is not exempt", []string{"stranger"}, priors, false},
		{"nil priors is a complete no-op", []string{"reliable"}, nil, false},
		{"empty priors is a complete no-op", []string{"reliable"}, map[string]float64{}, false},
		{"a 2-reviewer finding is never a singleton, so never exempt", []string{"reliable", "flaky"}, priors, false},
		{"an empty Reviewers slice is guarded before the index access", nil, priors, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eq(t, trustExempt(Finding{Confidence: ConfMedium, Reviewers: tc.reviewers}, tc.priors), tc.want, tc.name)
		})
	}
}

// TestDemoteByTrust_Direct unit-tests the pure demotion predicate directly (the
// same pattern TestConsensusExempt_Predicate uses for consensusExempt): a
// ConfMedium singleton with a low-trust sole reviewer demotes to ConfLow, and
// every guard (no priors, above-threshold rate, multi-reviewer, non-MEDIUM
// confidence) leaves the finding untouched.
func TestDemoteByTrust_Direct(t *testing.T) {
	low := map[string]float64{"flaky": trustLowThreshold}

	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: []string{"flaky"}}}, low).Confidence,
		ConfLow, "a low-trust ConfMedium singleton demotes to ConfLow")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: []string{"flaky"}}}, nil).Confidence,
		ConfMedium, "nil priors is a no-op")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: []string{"reliable"}}},
		map[string]float64{"reliable": trustHighThreshold}).Confidence,
		ConfMedium, "a rate above trustLowThreshold is not demoted")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfHigh, Reviewers: []string{"flaky"}}}, low).Confidence,
		ConfHigh, "a non-ConfMedium finding (already HIGH) is never demoted")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: []string{"flaky", "other"}}}, low).Confidence,
		ConfMedium, "a 2-reviewer finding is never demoted")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: nil}}, low).Confidence,
		ConfMedium, "an empty Reviewers slice is guarded before the index access")
}

// TestTrust_MidRangeRateIsCompleteNoOp pins the neutral zone between the two
// threshold constants: every existing trust test sits exactly ON a boundary
// (trustLowThreshold 0.3 or trustHighThreshold 0.7), so a regression that
// widens a comparison or swaps which constant gates which predicate (e.g.
// trustExempt comparing against trustLowThreshold, or demoteByTrust against
// trustHighThreshold) would pass them all undetected. A mid-range rate must be
// a complete no-op through both predicates.
func TestTrust_MidRangeRateIsCompleteNoOp(t *testing.T) {
	mid := map[string]float64{"mid": (trustLowThreshold + trustHighThreshold) / 2}

	eq(t, trustExempt(Finding{Confidence: ConfMedium, Reviewers: []string{"mid"}}, mid),
		false, "a mid-range rate never exempts from the consensus filter")
	eq(t, demoteByTrust(Merged{Finding{Confidence: ConfMedium, Reviewers: []string{"mid"}}}, mid).Confidence,
		ConfMedium, "a mid-range rate never demotes to ConfLow")
}

// TestDemoteByTrust_LowTrustSingletonDemotedToConfLow proves demotion is
// reachable end-to-end through Reconcile on a real >= consensusMinReviewers
// panel (the same floor the consensus filter itself gates on — see the
// TestDemoteByTrust_Direct guard tests for the panel-size-independent
// predicate). The demoted singleton is given the "security" category so it
// also survives consensusExempt and stays visible in res.Findings — otherwise
// the consensus filter would immediately sidecar it (clearing Confidence in
// the wire shape) and the demotion would be unobservable here, exactly as
// AC2 notes: a ConfLow singleton is sidecarred under strict regardless.
func TestDemoteByTrust_LowTrustSingletonDemotedToConfLow(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "request path is not authorization checked", "security", "flaky"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "stranger"),
		}},
		{Name: "c", Findings: []Finding{
			cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
		}},
	}
	priors := map[string]float64{"flaky": trustLowThreshold}
	res := Reconcile(sources, Options{TrustPriors: priors})

	var found bool
	for _, m := range res.Findings {
		if m.File == "foo.go" {
			found = true
			eq(t, m.Confidence, ConfLow, "low-trust singleton is demoted to ConfLow")
		}
	}
	isTrue(t, found, "a consensus-exempt (security) singleton still reaches findings.json after demotion")
}

func TestTrustPriors_ReviewerKeyLowercased(t *testing.T) {
	// Finding.Reviewer carries the panel's original casing; TrustPriors (epic
	// 35.8) keys lowercase — the lookup must lowercase to match (TD-013's
	// mismatch, guarded the same way select.go does).
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "Archer"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "stranger"),
		}},
		{Name: "c", Findings: []Finding{
			cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
		}},
	}
	priors := map[string]float64{"archer": trustHighThreshold}
	res := Reconcile(sources, Options{TrustPriors: priors})

	isTrue(t, hasFinding(res, "foo.go"), "mixed-case Finding.Reviewer resolves to its lowercase prior key")
}

func TestTrustPriors_AbsentReviewerNoOp(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "unknown"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "stranger"),
		}},
		{Name: "c", Findings: []Finding{
			cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
		}},
	}
	withPriors := Reconcile(sources, Options{TrustPriors: map[string]float64{"someone-else": trustHighThreshold}})
	withoutPriors := Reconcile(sources, Options{})

	eq(t, withPriors.Summary.ConsensusFiltered, withoutPriors.Summary.ConsensusFiltered,
		"a reviewer absent from TrustPriors behaves byte-identically to pre-35.9")
	isTrue(t, !hasFinding(withPriors, "foo.go"), "the unknown-reviewer singleton is still sidecarred")
}

func TestTrustPriors_NilOrEmptyOptionsIsCompleteNoOp(t *testing.T) {
	// Regression guard mirroring epic 13.3's empty-authority-map guarantee: a
	// nil/empty TrustPriors must not perturb reconcile output at all.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("HIGH", "foo.go", 10, "token never expires unchecked here", "correctness", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("HIGH", "foo.go", 10, "token never expires unchecked here", "correctness", "b"),
		}},
		{Name: "c", Findings: []Finding{
			cf("LOW", "bar.go", 20, "unused import lingers in this file", "style", "c"),
		}},
	}
	withNil := Reconcile(sources, Options{TrustPriors: nil})
	withEmpty := Reconcile(sources, Options{TrustPriors: map[string]float64{}})
	baseline := Reconcile(sources, Options{})

	deepEq(t, withNil, baseline, "nil TrustPriors is byte-identical to the zero-value Options")
	deepEq(t, withEmpty, baseline, "empty TrustPriors is byte-identical to the zero-value Options")
}

// TestSummary_TrustPriorsResolvedCountsTheAttachedPriors pins the epic-35.11
// observability gap: ResolveTrustPriors reads a 180d window, so a reviewer with
// no runs inside it drops out of the map and silently loses trust
// exemption/demotion — while the scorecard read discards its diagnostics and
// `atcr personas list --scores` still reads all history, reporting that reviewer
// as healthy. Summary.TrustPriorsResolved is the only signal that divergence
// produces, so it must track len(opts.TrustPriors) exactly: the SIZE of the
// attached map, not how many findings the priors happened to affect.
func TestSummary_TrustPriorsResolvedCountsTheAttachedPriors(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "possible nil deref on this path", "correctness", "trusted"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "stranger"),
		}},
		{Name: "c", Findings: []Finding{
			cf("MEDIUM", "baz.go", 30, "request body is not validated", "correctness", "third"),
		}},
	}

	// Three priors, only one of which matches a reviewer in this run: the count
	// is the map size, so the two non-participating entries still count. A
	// findings-derived count would report 1 here and would not drop when a
	// dormant reviewer ages out of the window — the exact signal this exists for.
	priors := map[string]float64{
		"trusted": trustHighThreshold,
		"dormant": trustHighThreshold,
		"retired": trustLowThreshold,
	}
	eq(t, Reconcile(sources, Options{TrustPriors: priors}).Summary.TrustPriorsResolved, 3,
		"TrustPriorsResolved is len(opts.TrustPriors), including reviewers absent from this run")

	// A reviewer aging out of the window shows up as a smaller count — the drop
	// between runs is the signal.
	aged := map[string]float64{"trusted": trustHighThreshold}
	eq(t, Reconcile(sources, Options{TrustPriors: aged}).Summary.TrustPriorsResolved, 1,
		"a reviewer dropped by the window lowers the count")

	// Nil and empty are both 0: a fresh install, an unresolvable config dir, and
	// an unreadable store all degrade to a nil map by design.
	eq(t, Reconcile(sources, Options{TrustPriors: nil}).Summary.TrustPriorsResolved, 0,
		"nil priors resolve to 0")
	eq(t, Reconcile(sources, Options{TrustPriors: map[string]float64{}}).Summary.TrustPriorsResolved, 0,
		"empty priors resolve to 0")
}

func TestDemoteByTrust_NeverFiresOnAuthorityPromotedFinding(t *testing.T) {
	// Same 3-source shape as TestConsensusFilter_AuthorityPromotedSingletonSurvives:
	// alice earns run-global PageRank authority via two corroborations and her
	// lone MEDIUM finding on bar.go is promoted to ConfHigh. A low trust prior for
	// alice must not demote it back down — demotion is scoped to ConfMedium
	// singletons only, never one PageRank already promoted.
	sources := []Source{
		{Name: "host", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "token never expires unchecked here", "correctness", "alice"),
			cf("MEDIUM", "baz.go", 10, "request body is not validated", "correctness", "alice"),
			cf("MEDIUM", "bar.go", 20, "unused import lingers in this file", "style", "alice"),
		}},
		{Name: "pool", Findings: []Finding{
			cf("MEDIUM", "foo.go", 10, "token never expires unchecked here", "correctness", "bob"),
		}},
		{Name: "extra", Findings: []Finding{
			cf("MEDIUM", "baz.go", 10, "request body is not validated", "correctness", "carol"),
		}},
	}
	priors := map[string]float64{"alice": trustLowThreshold}
	res := Reconcile(sources, Options{TrustPriors: priors})

	eq(t, res.Summary.AuthorityPromoted, 1, "alice's lone finding was promoted by authority")
	for _, m := range res.Findings {
		if m.File == "bar.go" {
			eq(t, m.Confidence, ConfHigh, "authority-promoted singleton is never demoted by a low trust prior")
		}
	}
}

func TestDemoteByTrust_NeverFiresOnMultiReviewerFinding(t *testing.T) {
	// dave and eve co-locate on the same finding, so vote-count alone already
	// yields ConfHigh (len(Reviewers)==2). A low trust prior for both must not
	// demote it — demotion is scoped to singletons only.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("MEDIUM", "multi.go", 40, "possible nil deref on this path", "correctness", "dave"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "multi.go", 40, "possible nil deref on this path", "correctness", "eve"),
		}},
	}
	priors := map[string]float64{"dave": trustLowThreshold, "eve": trustLowThreshold}
	res := Reconcile(sources, Options{TrustPriors: priors})

	require1Finding(t, res, "multi.go")
	for _, m := range res.Findings {
		if m.File == "multi.go" {
			eq(t, m.Confidence, ConfHigh, "a 2-reviewer finding is never demoted by a low trust prior")
		}
	}
}

// require1Finding is a small local helper (not shared with consensus_test.go)
// asserting exactly one merged finding exists at file.
func require1Finding(t *testing.T, res Result, file string) {
	t.Helper()
	n := 0
	for _, m := range res.Findings {
		if m.File == file {
			n++
		}
	}
	eq(t, n, 1, "exactly one merged finding at "+file)
}
