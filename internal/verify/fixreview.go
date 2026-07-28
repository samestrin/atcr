package verify

import "strings"

// fixReviewPrefix is the NEEDS_REVIEW marker that leads every FixReview
// annotation, so the shortcut is greppable in findings.json and unmistakable in
// the rendered report (Epic 35.3).
const fixReviewPrefix = "NEEDS_REVIEW"

// smellRetryInstruction is the trusted, atcr-authored half of the diff-smell
// retry prompt. It lives in the INSTRUCTION section of both prompt builders,
// above the untrusted-data boundary; the evidence half — which quotes the
// model's own rejected diff — goes below it. Keeping the two apart is what lets
// the retry stay within the existing prompt-injection boundary rather than
// widening it (Epic 35.3).
const smellRetryInstruction = "Your previous attempt was rejected: it made the finding go away without fixing it " +
	"(for example by changing only test files, or by removing assertions). " +
	"Fix the underlying problem in the implementation instead. " +
	"Do not delete or weaken tests. If you genuinely cannot fix the root cause, decline rather than retry the same shortcut."

// evaluateFixSmell is the gate's decision function: it scans a candidate fix for
// over-simplification smells and applies the two atcr-specific policies the raw
// analyzer deliberately does not (Epic 35.3 clarifications Q1 and Q2).
//
// It returns nil — "not gateable, pass through" — when the fix is not diff-shaped
// or is pathologically large:
//
//   - Finding.Fix is FREE-FORM. buildFixPrompt asks for "corrected code or a
//     precise change instruction", and validateGoFixSyntax explicitly tolerates
//     prose. Only the --auto-fix path assumes a diff, and it already treats an
//     unparseable fix as a silent skip rather than an error (cli/autofix.go
//     selectAutoFixEntries). Feeding prose to a diff parser would mis-attribute
//     it, so non-diff content is classified clean.
//   - An oversized fix is not scanned, mirroring validateGoFixSyntax's maxFixBytes
//     guard: content that large is not a genuine fix, and the per-added-line regex
//     sweep is the expensive part.
//
// findingFile is the path the finding itself cites. When it is a test file, the
// test_only smell is SUPPRESSED: that detector is pure file classification (test
// lines present, zero impl lines) with no awareness of intent, so a legitimately
// test-scoped finding would be rejected every single time — the false-positive
// mode already recorded against this repo (TD-005). Suppression re-derives the
// verdict, so a co-occurring SOFT smell still surfaces as soft_only rather than
// being masked to clean. weakened_assertion is NOT suppressed: deleting an
// assertion is the reward hack this gate exists to catch, in test files most of all.
func evaluateFixSmell(fix, findingFile string) *smellResult {
	// Defensive: a nil/empty fix is never gateable. Redundant with the
	// looksLikeUnifiedDiff check below (which also rejects ""), but explicit so a
	// future caller change degrades to pass-through rather than a nil dereference.
	if fix == "" {
		return nil
	}
	if len(fix) > maxFixBytes || !looksLikeUnifiedDiff(fix) {
		return nil
	}
	res := analyzeDiff(fix)
	if res != nil && isSmellTestPath(findingFile) {
		dropSmellType(res, smellTestOnly)
	}
	return res
}

// dropSmellType removes every smell of the given type from res and re-derives the
// counts and verdict, so a suppressed HARD smell cannot leave a stale "hard"
// verdict behind and cannot mask a surviving SOFT one.
func dropSmellType(res *smellResult, typ string) {
	if res == nil || res.Summary.ByType[typ] == 0 {
		return
	}
	kept := make([]smell, 0, len(res.Smells))
	for _, s := range res.Smells {
		if s.Type != typ {
			kept = append(kept, s)
		}
	}
	res.Smells = kept
	delete(res.Summary.ByType, typ)
	res.Summary.Hard, res.Summary.Soft = 0, 0
	for _, s := range res.Smells {
		if s.Severity == smellSeverityHard {
			res.Summary.Hard++
		} else {
			res.Summary.Soft++
		}
	}
	switch {
	case res.Summary.Hard > 0:
		res.Summary.Verdict = smellVerdictHard
	case res.Summary.Soft > 0:
		res.Summary.Verdict = smellVerdictSoftOnly
	default:
		res.Summary.Verdict = smellVerdictClean
	}
}

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
