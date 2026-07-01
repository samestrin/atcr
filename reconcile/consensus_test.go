package reconcile

import "testing"

// Epic 14.2 AC2: the reconciler drops uncorroborated singleton findings (single
// reviewer, MEDIUM confidence) to the ambiguous sidecar when the panel is large
// enough (>=3 sources) that a real issue would likely be caught by more than one
// reviewer — unless the finding is security-related, HIGH/CRITICAL severity,
// out-of-scope, or independently confirmed.

// cf builds a fully-specified finding for the consensus-filter tests (fnd only
// sets file/line/problem/reviewer).
func cf(sev, file string, line int, problem, category, reviewer string) Finding {
	return Finding{
		Severity: sev, File: file, Line: line, Problem: problem,
		Fix: "do the fix", Category: category, EstMinutes: 5,
		Evidence: "ev", Reviewer: reviewer,
	}
}

// hasFinding reports whether res.Findings contains a finding at file (any line).
func hasFinding(res Result, file string) bool {
	for _, m := range res.Findings {
		if m.File == file {
			return true
		}
	}
	return false
}

// inAmbiguous reports whether the ambiguous sidecar contains a single-finding
// cluster for file.
func inAmbiguousSingleton(res Result, file string) bool {
	for _, c := range res.Ambiguous {
		if c.File == file && len(c.Findings) == 1 {
			return true
		}
	}
	return false
}

func TestConsensusFilter_DropsStylisticSingletonWithFullPanel(t *testing.T) {
	// 3-source panel. "a" and "b" corroborate one issue (HIGH confidence, kept).
	// "c" reports a lone stylistic LOW finding nobody else saw — an uncorroborated
	// singleton that the consensus filter routes to the sidecar.
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
	res := Reconcile(sources, Options{})

	isTrue(t, hasFinding(res, "foo.go"), "the corroborated consensus finding stays")
	isTrue(t, !hasFinding(res, "bar.go"), "the stylistic singleton is dropped from findings")
	isTrue(t, inAmbiguousSingleton(res, "bar.go"), "the stylistic singleton is routed to the sidecar")
	eq(t, res.Summary.ConsensusFiltered, 1, "one singleton was consensus-filtered")
}

func TestConsensusFilter_InactiveBelowThreeSources(t *testing.T) {
	// A 2-source panel (the documented host + 1 pool workflow). The stylistic
	// singleton must NOT be dropped — requiring consensus here would gut real output.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("HIGH", "foo.go", 10, "token never expires unchecked here", "correctness", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("HIGH", "foo.go", 10, "token never expires unchecked here", "correctness", "b"),
			cf("LOW", "bar.go", 20, "unused import lingers in this file", "style", "b"),
		}},
	}
	res := Reconcile(sources, Options{})

	isTrue(t, hasFinding(res, "bar.go"), "singleton stays when the panel is below 3 sources")
	eq(t, res.Summary.ConsensusFiltered, 0, "the filter is inert below the panel-size floor")
}

func TestConsensusFilter_ExemptsSecurityAndHighSeverity(t *testing.T) {
	// 3-source panel, every finding a singleton (no corroboration). Only the
	// stylistic LOW one is dropped; the security, HIGH, CRITICAL, and out-of-scope
	// singletons are exempt and survive.
	sources := []Source{
		{Name: "a", Findings: []Finding{
			cf("LOW", "style.go", 20, "unused import lingers in this file", "style", "a"),
		}},
		{Name: "b", Findings: []Finding{
			cf("MEDIUM", "sec.go", 30, "request path is not authorization checked", "security", "b"),
			cf("HIGH", "high.go", 40, "off by one drops the last element", "correctness", "b"),
		}},
		{Name: "c", Findings: []Finding{
			cf("CRITICAL", "crit.go", 50, "sql injection in the query builder path", "correctness", "c"),
			cf("MEDIUM", "oos.go", 60, "preexisting smell outside the reviewed change", "out-of-scope", "c"),
		}},
	}
	res := Reconcile(sources, Options{})

	isTrue(t, !hasFinding(res, "style.go"), "the stylistic singleton is dropped")
	isTrue(t, hasFinding(res, "sec.go"), "a security singleton is exempt")
	isTrue(t, hasFinding(res, "high.go"), "a HIGH-severity singleton is exempt")
	isTrue(t, hasFinding(res, "crit.go"), "a CRITICAL-severity singleton is exempt")
	isTrue(t, hasFinding(res, "oos.go"), "an out-of-scope singleton is exempt")
	eq(t, res.Summary.ConsensusFiltered, 1, "only the stylistic singleton was filtered")
}

// TestConsensusExempt_Predicate unit-tests the pure exemption predicate directly,
// including the confirmed-verdict branch that cannot fire through Reconcile (Merge
// nils input Verification), so the branch is exercised and documented.
func TestConsensusExempt_Predicate(t *testing.T) {
	isTrue(t, consensusExempt(Finding{Category: "security"}), "security category exempt")
	isTrue(t, consensusExempt(Finding{Category: "SECURITY"}), "security is case-insensitive")
	isTrue(t, consensusExempt(Finding{Category: "out-of-scope"}), "out-of-scope exempt")
	isTrue(t, consensusExempt(Finding{Severity: "CRITICAL"}), "critical exempt")
	isTrue(t, consensusExempt(Finding{Severity: "high"}), "high exempt (case-insensitive)")
	isTrue(t, consensusExempt(Finding{Verification: &Verification{Verdict: VerdictConfirmed, Skeptic: "s"}}),
		"a confirmed finding is exempt")
	isTrue(t, !consensusExempt(Finding{Severity: "LOW", Category: "style"}),
		"a low-severity stylistic finding is not exempt")
	isTrue(t, !consensusExempt(Finding{Severity: "MEDIUM", Category: "correctness"}),
		"a medium non-security finding is not exempt")
}
