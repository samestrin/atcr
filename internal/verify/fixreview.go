package verify

import "strings"

// fixReviewPrefix is the NEEDS_REVIEW marker that leads every FixReview
// annotation, so the shortcut is greppable in findings.json and unmistakable in
// the rendered report (Epic 35.3).
const fixReviewPrefix = "NEEDS_REVIEW"

// buildFixReview renders the annotation stamped on a fix that the diff-smell gate
// ACCEPTED despite SOFT smells (suppression / empty_catch / stub_body). It names
// every smell type found so a human reviewer knows which shortcut was taken.
//
// It returns "" for a nil or smell-free result, so a clean fix is never annotated.
// Only the type list is included — never the raw evidence line — because the
// evidence is verbatim model-generated diff text, whereas the type list is a
// closed, trusted vocabulary; the location detail already lives in the finding.
func buildFixReview(res *smellResult) string {
	types := smellTypes(res)
	if len(types) == 0 {
		return ""
	}
	return fixReviewPrefix + ": fix accepted with over-simplification smell(s): " + strings.Join(types, ", ")
}
