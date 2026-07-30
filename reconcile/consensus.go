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

// Consensus filter levels (epic 35.9.1). They move ONLY the singleton
// corroboration bar — consensusMinReviewers (the panel-size floor) and
// consensusExempt/trustExempt (the exemption set) are identical at every level,
// deliberately keeping the internal confidence ladder and the exemption rules
// out of the public API.
//
//   - ConsensusStrict is the default and reproduces the hardcoded epic-14.2
//     behavior exactly: every singleton below ConfHigh is sidecarred.
//   - ConsensusLenient raises the kept-bar to ConfMedium, so an uncorroborated
//     MEDIUM singleton survives and only a ConfLow one is sidecarred.
//   - ConsensusOff makes the filter inert, restoring pre-14.2 behavior.
const (
	ConsensusOff     = "off"
	ConsensusLenient = "lenient"
	ConsensusStrict  = "strict"
)

// ConsensusLevels lists the valid levels in documentation order (default
// first), so every surface that must name them in an error or a help string
// reads them from one place instead of re-listing them.
var ConsensusLevels = []string{ConsensusStrict, ConsensusLenient, ConsensusOff}

// NormalizeConsensus canonicalizes a consensus level token and reports whether
// it is in the closed vocabulary. It is case- and whitespace-insensitive
// (following validateGate/ParseSeverity rather than the exact-lowercase
// on_overflow/payload_mode convention), and treats the empty string as the
// unset case: valid, canonicalizing to ConsensusStrict, so an unconfigured
// project keeps today's behavior. Callers phrase their own failure for an
// invalid token — a CLI usage error (exit 2) or an MCP tool error.
func NormalizeConsensus(v string) (string, bool) {
	switch c := strings.ToLower(strings.TrimSpace(v)); c {
	case "":
		return ConsensusStrict, true
	case ConsensusStrict, ConsensusLenient, ConsensusOff:
		return c, true
	default:
		return "", false
	}
}

// consensusFloor maps an effective level to the confidence floor
// consensusSingletonAt tests against, and reports whether the filter runs at
// all. An unrecognized level fails SAFE — it is treated as strict rather than
// disabling the filter — so a value that somehow bypassed boundary validation
// can never silently widen what reaches findings.json.
func consensusFloor(level string) (floor string, enabled bool) {
	switch c, ok := NormalizeConsensus(level); {
	case ok && c == ConsensusOff:
		return "", false
	case ok && c == ConsensusLenient:
		return ConfMedium, true
	default:
		return ConfHigh, true
	}
}

// trustHighThreshold / trustLowThreshold gate the reconcile-time trust prior
// (epic 35.9, consuming scorecard.TrustPriors from epic 35.8): a singleton
// whose sole reviewer's historical corroboration rate is at or above
// trustHighThreshold is exempt from the consensus filter regardless of in-run
// corroboration or PageRank authority (trustExempt); one at or below
// trustLowThreshold is demoted to ConfLow (demoteByTrust). Hardcoded alongside
// consensusMinReviewers, not flags — mirrors epic 35.6's restraint on exposing
// internal filter constants.
const (
	trustHighThreshold = 0.7
	trustLowThreshold  = 0.3
)

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
// also drops a ConfLow untrusted-source singleton (reachable since epic 35.9's
// demoteByTrust demotes low-trust ConfMedium singletons to ConfLow).
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

// trustExempt reports whether a singleton's sole reviewer has a historically
// reliable trust prior (>= trustHighThreshold), exempting it from the
// consensus filter regardless of in-run corroboration or PageRank authority.
// priors is keyed by lowercase reviewer name (scorecard.TrustPriors); the
// lookup lowercases f.Reviewers[0] to match — the same mismatch guarded
// against in internal/verify/select_test.go (TD-013). A finding with other
// than exactly one reviewer, or whose sole reviewer is absent from priors (no
// history, or below scorecard.DefaultTrustMinRuns), is never exempt by this
// predicate; a nil/empty priors map is a complete no-op.
func trustExempt(f Finding, priors map[string]float64) bool {
	if len(priors) == 0 || len(f.Reviewers) != 1 {
		return false
	}
	rate, ok := priors[strings.ToLower(f.Reviewers[0])]
	return ok && rate >= trustHighThreshold
}

// demoteByTrust (epic 35.9) sets Confidence to ConfLow on a singleton whose
// sole reviewer's trust prior is at or below trustLowThreshold, making ConfLow
// reachable at reconcile time for the first time (previously assigned only
// post-verify, by a refuted verdict — see ConfidenceForVerdict). Scoped to
// findings that are currently ConfMedium singletons — never a 2+-reviewer
// finding (already ConfHigh via ConfidenceFor) or one PageRank authority
// already promoted to ConfHigh (promoteByAuthority runs before this in
// Reconcile) — so demotion only ever narrows, never overrides, an existing
// promotion path. A nil/empty priors map is a complete no-op.
func demoteByTrust(m Merged, priors map[string]float64) Merged {
	if len(priors) == 0 || m.Confidence != ConfMedium || len(m.Reviewers) != 1 {
		return m
	}
	if rate, ok := priors[strings.ToLower(m.Reviewers[0])]; ok && rate <= trustLowThreshold {
		m.Confidence = ConfLow
	}
	return m
}

// consensusNoiseCluster wraps a consensus-filtered singleton as a single-finding
// ambiguous cluster — the same shape DBSCAN noise takes, so it is inert in the
// debate and adjudication workflows (both act only on 2-finding gray pairs) yet
// stays in the audit trail and remains recoverable. It shares the helper that
// normalizes merged findings back to the raw per-source wire shape.
func consensusNoiseCluster(f Finding) AmbiguousCluster {
	return singletonAmbiguousCluster(f)
}
