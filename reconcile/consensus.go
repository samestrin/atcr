package reconcile

import "strings"

// consensusMinSources is the panel-size floor for the epic-14.2 consensus filter.
// Below it the filter is inert: a 2-source panel (the documented single-API-key
// host + 1 pool agent workflow) makes almost every finding a singleton, so
// requiring corroboration there would drop real issues wholesale. At or above it a
// genuine problem is likely to be caught by more than one reviewer, so an
// uncorroborated singleton is more plausibly a hallucination than a rare true
// positive.
const consensusMinSources = 3

// categorySecurity is the finding category exempt from the consensus filter. It is
// compared case-insensitively (lower+trim) to mirror ModalCategory's
// canonicalization, so "Security"/"SECURITY" all match.
const categorySecurity = "security"

// consensusSingleton reports whether a reconciled finding is an uncorroborated
// singleton — the drop candidate for the consensus filter. Confidence == ConfMedium
// is exactly "fewer than two distinct reviewers AND not authority-promoted" (epic
// 13.3): a single-reviewer finding the authority graph raised to HIGH is NOT a
// singleton and is never dropped. Keying on Confidence rather than len(Reviewers)
// therefore preserves authority promotion for free.
func consensusSingleton(m Merged) bool {
	return m.Confidence == ConfMedium
}

// consensusExempt reports whether a singleton is too costly to drop as a probable
// hallucination and must survive to findings.json regardless of corroboration. A
// finding is exempt when it is security-related, HIGH/CRITICAL severity, an
// out-of-scope annotation (whose own AC 06-04 report path must not be disturbed),
// or carries an independently-confirmed verdict.
//
// NOTE: Verification is nil at reconcile time in the current pipeline — Merge does
// not propagate input verdicts, and the verify stage stamps them post-reconcile —
// so the confirmed-verdict branch cannot fire through Reconcile today. It is kept
// as a defensive, forward-looking guard so the predicate stays correct if a caller
// ever filters over already-verified findings; it is unit-tested directly.
func consensusExempt(f Finding) bool {
	switch strings.ToLower(strings.TrimSpace(f.Category)) {
	case categorySecurity, CategoryOutOfScope:
		return true
	}
	switch NormalizeSeverity(f.Severity) {
	case SevCritical, SevHigh:
		return true
	}
	if f.Verification != nil && f.Verification.Verdict == VerdictConfirmed {
		return true
	}
	return false
}

// consensusNoiseCluster wraps a consensus-filtered singleton as a single-finding
// ambiguous cluster — the same shape DBSCAN noise takes, so it is inert in the
// debate and adjudication workflows (both act only on 2-finding gray pairs) yet
// stays in the audit trail and remains recoverable. Similarity is 0 (no
// corroboration), and the id is the stable single-problem content handle.
func consensusNoiseCluster(f Finding) AmbiguousCluster {
	return AmbiguousCluster{
		ID:       AmbiguousID(f.File, f.Line, f.Problem, f.Problem),
		File:     f.File,
		Line:     f.Line,
		Findings: []Finding{f},
	}
}
