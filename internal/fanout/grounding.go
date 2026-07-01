package fanout

import (
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

// groundingTolerance mirrors the reconciler's lineProximity (reconcile/cluster.go):
// a cited line within this many lines of a changed range is treated as grounded,
// absorbing the small line-number drift reviewers routinely introduce.
const groundingTolerance = 3

// groundFindings drops findings that cannot be located in the patch's changed
// lines — the Epic 14.1 anti-hallucination gate. STUB (RED).
func groundFindings(findings []stream.Finding, changed payload.ChangedLines) ([]stream.Finding, int) {
	return findings, 0
}
