package reconcile

import "strings"

// consensusMinReviewers is the panel-size floor for the epic-14.2 consensus filter,
// measured in DISTINCT REVIEWERS that contributed findings (see panelReviewers) —
// NOT the number of source directories. Below it the filter is inert: a 2-reviewer
// panel (the documented single-API-key host + 1 pool persona workflow) makes almost
// every finding a singleton, so requiring corroboration there would drop real issues
// wholesale. At or above it a genuine problem is likely to be caught by more than
// one reviewer, so an uncorroborated singleton is more plausibly a hallucination
// than a rare true positive.
const consensusMinReviewers = 3

// categorySecurity is the literal finding category exempt from the consensus filter.
// securityRelated recognizes synonyms and substrings as well, so a single reviewer
// labeling a genuine vulnerability "vulnerability", "auth", or "injection" is not
// silently dropped because the exemption predicate was overly literal.
const categorySecurity = "security"

// securityRelated reports whether a category signals a security concern after
// ModalCategory-style canonicalization (lower+trim). It matches the literal token
// "security" plus common synonyms/substrings reviewers actually emit.
func securityRelated(category string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	if c == categorySecurity {
		return true
	}
	return strings.Contains(c, "vuln") || strings.Contains(c, "auth") || strings.Contains(c, "inject")
}

// panelReviewers counts the distinct non-empty reviewers that contributed findings
// across all sources — the true panel size the consensus filter gates on. It is
// deliberately NOT len(sources): ATCR's discovery flattens every pool persona into a
// single "pool" source (sources/pool/raw/agent/<name>/findings.txt), distinguishing
// personas only by the per-finding Reviewer column, so a full multi-persona panel is
// just two source directories (host + pool) but many reviewers. Gating on len(sources)
// would leave the filter permanently inert for the exact multi-persona pool the epic
// targets; gating on the distinct-reviewer count is what makes it fire. Reviewers
// that produced no findings (an empty/skipped source) do not count, so a degraded or
// dead source cannot inflate the panel to the threshold.
func panelReviewers(sources []Source) int {
	seen := map[string]struct{}{}
	for _, s := range sources {
		for _, f := range s.Findings {
			if f.Reviewer != "" {
				seen[f.Reviewer] = struct{}{}
			}
		}
	}
	return len(seen)
}

// consensusSingleton reports whether a reconciled finding is an uncorroborated
// singleton — the drop candidate for the consensus filter. "Uncorroborated" is
// confidence below HIGH: ConfidenceFor gives MEDIUM to a finding with fewer than two
// distinct reviewers, and any finding the authority graph (epic 13.3) or the verify
// stage promoted to HIGH/VERIFIED is corroborated and never dropped. Keying on
// confidence rather than len(Reviewers) preserves authority promotion for free and
// also drops a (reserved, currently unused) ConfLow untrusted-source singleton.
func consensusSingleton(m Merged) bool {
	return !ConfidenceAtOrAbove(m.Confidence, ConfHigh)
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
	cat := strings.ToLower(strings.TrimSpace(f.Category))
	if securityRelated(cat) || cat == CategoryOutOfScope {
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
// stays in the audit trail and remains recoverable. It shares the helper that
// normalizes merged findings back to the raw per-source wire shape.
func consensusNoiseCluster(f Finding) AmbiguousCluster {
	return singletonAmbiguousCluster(f)
}
