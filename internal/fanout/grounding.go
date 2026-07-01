package fanout

import (
	"strings"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

const (
	// groundingTolerance mirrors the reconciler's lineProximity (reconcile/cluster.go):
	// a cited line within this many lines of a changed range is treated as
	// grounded, absorbing the small line-number drift reviewers routinely introduce.
	groundingTolerance = 3
	// evidenceMinMatch is the minimum normalized length a substring match must
	// span before the evidence fallback trusts it, so ubiquitous boilerplate
	// ("if err != nil {") cannot ground an arbitrary hallucinated finding.
	evidenceMinMatch = 12
)

// groundFindings drops findings that cannot be located in the patch's changed
// lines — the Epic 14.1 anti-hallucination gate. A nil changed map (the
// ingestion / non-git path, which has no live diff) disables the gate entirely
// (keep all). With a non-nil map, each finding is judged by isGrounded and the
// ungrounded ones are dropped. Kept findings retain their original order; the
// drop count is returned for the per-agent stderr tally.
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

// isGrounded reports whether a finding is anchored in the patch. Precedence:
//   - CATEGORY out-of-scope is exempt (a deliberate annotated-not-promoted
//     feature; the reconciler segregates these, never promotes them).
//   - A file the patch did NOT touch is ungrounded and dropped — this is the
//     fabricated-file hallucination class (a model inventing a path it never saw
//     change), the primary vector Epic 14.1 targets.
//   - A file changed only in binary/mode (no line data) fails open: there is
//     nothing to check the finding against, so it cannot be proven ungrounded.
//   - A file-level finding (no specific line) on a changed file is kept: the file
//     itself is in scope.
//   - Otherwise the cited line must fall within a changed range ±groundingTolerance,
//     or the EVIDENCE must match a changed line.
func isGrounded(f stream.Finding, changed payload.ChangedLines) bool {
	if strings.EqualFold(strings.TrimSpace(f.Category), reclib.CategoryOutOfScope) {
		return true
	}
	fc, ok := changed[normalizeFindingPath(f.File)]
	if !ok {
		return false // file not in the patch: ungrounded
	}
	if len(fc.Ranges) == 0 && len(fc.ChangedText) == 0 {
		return true // binary/mode-only change: no lines to check, fail open
	}
	if f.Line <= 0 {
		return true // file-level finding on a file the patch changed
	}
	if lineInRanges(f.Line, fc.Ranges) {
		return true
	}
	return evidenceMatches(f.Evidence, fc.ChangedText)
}

// normalizeFindingPath strips the diff-artifact prefixes a model sometimes copies
// into a FILE column (a/, b/, ./, a leading /) so a cited path matches the
// repo-root-relative head-path keys of the changed map. Best-effort: a form it
// cannot normalize (an absolute path elsewhere) simply misses the map, and the
// finding is treated as ungrounded.
func normalizeFindingPath(file string) string {
	p := strings.TrimSpace(file)
	p = strings.TrimPrefix(p, "./")
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		p = p[2:]
	}
	return strings.TrimPrefix(p, "/")
}

// lineInRanges reports whether a 1-based line falls within any changed range
// expanded by groundingTolerance on both ends. A non-positive line is never in
// range (handled as a file-level finding by the caller).
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
// out of range: it keeps the finding when its EVIDENCE and a changed line contain
// one another (case-folded, whitespace-collapsed) over at least evidenceMinMatch
// characters, so a real quote grounds the finding while short boilerplate cannot.
func evidenceMatches(evidence string, changed []string) bool {
	ev := collapseSpaces(strings.ToLower(strings.TrimSpace(evidence)))
	if len(ev) < evidenceMinMatch || len(changed) == 0 {
		return false
	}
	for _, c := range changed {
		cl := collapseSpaces(strings.ToLower(c))
		if len(cl) < evidenceMinMatch {
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
