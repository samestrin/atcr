package reconcile

import "strings"

// EvidenceSep is the delimiter used to join Evidence segments across a pipeline
// (reviewer findings, verification verdicts, executor attribution). Centralising
// it here makes the producer/consumer contract compile-time visible.
const EvidenceSep = "; "

// FixAttributionPrefix is the marker an executor stage appends to a finding's
// Evidence field after generating a fix ("fix by <name>"). Defined here so a
// producing package and a consuming package share the same literal without a
// cross-package import.
const FixAttributionPrefix = "fix by "

// HasFixAttribution reports whether evidence already carries a "fix by <name>"
// token for this executor, matched as a whole EvidenceSep-delimited segment.
// Whole-token matching prevents a name that is a strict prefix of another ("op"
// vs "opus"), or unrelated prose containing "fix by <name>" mid-sentence, from
// being falsely treated as already attributed.
func HasFixAttribution(evidence, name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	attr := FixAttributionPrefix + name
	for _, seg := range strings.Split(evidence, EvidenceSep) {
		if strings.TrimSpace(seg) == attr {
			return true
		}
	}
	return false
}

// AppendFixAttribution appends "fix by <name>" to a finding's Evidence, joining
// with EvidenceSep. It is idempotent: an Evidence already carrying the
// attribution as a delimited token is returned unchanged.
func AppendFixAttribution(evidence, name string) string {
	if strings.TrimSpace(name) == "" {
		return evidence
	}
	if HasFixAttribution(evidence, name) {
		return evidence
	}
	if strings.TrimSpace(evidence) == "" {
		return FixAttributionPrefix + name
	}
	return evidence + EvidenceSep + FixAttributionPrefix + name
}
