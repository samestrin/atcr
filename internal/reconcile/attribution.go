package reconcile

import "strings"

// EvidenceSep is the delimiter used to join Evidence segments across the
// pipeline (reviewer findings, verification verdicts, executor attribution).
// Centralising it here makes the producer/consumer contract compile-time
// visible: both internal/verify (appendFixAttribution) and
// internal/ghaction (FixAttribution) depend on this literal agreeing.
const EvidenceSep = "; "

// FixAttributionPrefix is the marker the executor stage appends to a
// finding's Evidence field after generating a fix ("fix by <name>"). Defined
// here so the producing package (internal/verify) and the consuming package
// (internal/ghaction) share the same literal without creating a cross-package
// import between leaf packages.
const FixAttributionPrefix = "fix by "

// HasFixAttribution reports whether evidence already carries a "fix by
// <name>" token for this executor, matched as a whole EvidenceSep-delimited
// segment. Whole-token matching prevents a name that is a strict prefix of
// another ("op" vs "opus"), or unrelated prose containing "fix by <name>"
// mid-sentence, from being falsely treated as already attributed.
func HasFixAttribution(evidence, name string) bool {
	attr := FixAttributionPrefix + name
	for _, seg := range strings.Split(evidence, EvidenceSep) {
		if strings.TrimSpace(seg) == attr {
			return true
		}
	}
	return false
}

// AppendFixAttribution appends "fix by <name>" to a finding's Evidence,
// joining with EvidenceSep. It is idempotent: an Evidence already carrying
// the attribution as a delimited token is returned unchanged.
func AppendFixAttribution(evidence, name string) string {
	if HasFixAttribution(evidence, name) {
		return evidence
	}
	if strings.TrimSpace(evidence) == "" {
		return FixAttributionPrefix + name
	}
	return evidence + EvidenceSep + FixAttributionPrefix + name
}
