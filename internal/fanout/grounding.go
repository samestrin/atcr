package fanout

import (
	"strings"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

// groundingTolerance mirrors the reconciler's lineProximity (reconcile/cluster.go):
// a cited line within this many lines of a changed range is treated as grounded,
// absorbing the small line-number drift reviewers routinely introduce.
const groundingTolerance = 3

// groundFindings drops findings that cannot be located in the patch's changed
// lines — the Epic 14.1 anti-hallucination gate. A finding is KEPT when:
//   - it is tagged CATEGORY out-of-scope (a deliberate annotated-not-promoted
//     feature, exempt from the drop); or
//   - no grounding data exists for its file (fail open — never drop when the
//     finding cannot be proven ungrounded, e.g. a non-git range or a file the
//     diff did not report); or
//   - its cited line falls within a changed head range expanded by
//     groundingTolerance; or
//   - its EVIDENCE text matches a changed (added or removed) line in the patch.
//
// Otherwise the finding is dropped as ungrounded. The kept findings are returned
// in their original order along with the count dropped. A nil changed map (the
// ingestion / non-git path, which has no live diff) disables the gate entirely.
func groundFindings(findings []stream.Finding, changed payload.ChangedLines) ([]stream.Finding, int) {
	if changed == nil || len(findings) == 0 {
		return findings, 0
	}
	kept := make([]stream.Finding, 0, len(findings))
	dropped := 0
	for _, f := range findings {
		if isGrounded(f, changed) {
			kept = append(kept, f)
		} else {
			dropped++
		}
	}
	return kept, dropped
}

// isGrounded reports whether a single finding is anchored in the patch.
func isGrounded(f stream.Finding, changed payload.ChangedLines) bool {
	if strings.EqualFold(strings.TrimSpace(f.Category), reclib.CategoryOutOfScope) {
		return true // out-of-scope findings are annotated, not promoted; exempt
	}
	fc, ok := changed[f.File]
	if !ok {
		return true // fail open: no grounding data for this file
	}
	if lineInRanges(f.Line, fc.Ranges) {
		return true
	}
	return evidenceMatches(f.Evidence, fc.ChangedText)
}

// lineInRanges reports whether a 1-based line falls within any changed range
// expanded by groundingTolerance on both ends. A non-positive line (no location
// parsed) is never in range.
func lineInRanges(line int, ranges []payload.LineRange) bool {
	if line <= 0 {
		return false
	}
	for _, r := range ranges {
		if line >= r.Start-groundingTolerance && line <= r.End+groundingTolerance {
			return true
		}
	}
	return false
}

// evidenceMatches is the fuzzy fallback for a finding whose cited line drifted
// out of range: it keeps the finding when its EVIDENCE and a changed line
// contain one another after case-folding and whitespace collapse. Strings
// shorter than 4 normalized characters are ignored so a trivial token ("}",
// "if") cannot ground an arbitrary finding.
func evidenceMatches(evidence string, changed []string) bool {
	ev := collapseSpaces(strings.ToLower(strings.TrimSpace(evidence)))
	if len(ev) < 4 || len(changed) == 0 {
		return false
	}
	for _, c := range changed {
		cl := collapseSpaces(strings.ToLower(c))
		if len(cl) < 4 {
			continue
		}
		if strings.Contains(ev, cl) || strings.Contains(cl, ev) {
			return true
		}
	}
	return false
}

// collapseSpaces folds every run of whitespace to a single space and trims the
// ends, so a quote and its diff line match despite indentation differences.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
