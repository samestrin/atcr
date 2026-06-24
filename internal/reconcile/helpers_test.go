package reconcile

import "github.com/samestrin/atcr/internal/stream"

// mf builds a per-source ATCR stream.Finding with the merge-relevant fields. The
// pure-logic merge/cluster/dedupe tests moved into the reconcile library (Epic
// 8.0); this helper backs the ATCR-internal tests that still build stream.Finding
// inputs (Source discovery, emit, summary, golden-corpus, disagreement radar).
func mf(sev, file string, line int, problem, fix, category string, est int, evidence, reviewer string) stream.Finding {
	return stream.Finding{
		Severity: sev, File: file, Line: line, Problem: problem, Fix: fix,
		Category: category, EstMinutes: est, Evidence: evidence, Reviewer: reviewer,
	}
}

// mfL builds the same finding as the library Finding type, for constructing
// AmbiguousCluster.Findings (which the extracted library types as []reconcile.Finding).
func mfL(sev, file string, line int, problem, fix, category string, est int, evidence, reviewer string) Finding {
	// Inline the stream.Finding -> reconcile.Finding field map so lib.go's
	// toLibFinding helper can be removed (TD-006).
	src := mf(sev, file, line, problem, fix, category, est, evidence, reviewer)
	return Finding{
		Severity:   src.Severity,
		File:       src.File,
		Line:       src.Line,
		Problem:    src.Problem,
		Fix:        src.Fix,
		Category:   src.Category,
		EstMinutes: src.EstMinutes,
		Evidence:   src.Evidence,
		Reviewer:   src.Reviewer,
		Reviewers:  src.Reviewers,
		Confidence: src.Confidence,
	}
}
