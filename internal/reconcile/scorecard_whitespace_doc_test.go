package reconcile

import (
	"strings"
	"testing"
	"unicode"
)

// docs/scorecard.md's blank-identity paragraph states that an identity already empty
// "or whitespace-only, which is the same thing to every reader of the store" is left
// alone and still publishes, and that "A whitespace-only identity is additionally
// named on stderr". Both sentences are FALSE for whitespace that is also a control
// rune — tab, newline, CR, VT, FF and U+0085 are whitespace-only to strings.TrimSpace
// AND control runes to firstNonPrintingRune, and the non-printing-rune check runs
// FIRST in selectPublishableRecordIdentities, so such an identity hard-fails the
// export with a non-zero exit instead of being kept and warned about. The paragraph
// above it cites only U+00AD, U+200B and U+202E as examples, so an operator reading
// the doc has no way to learn that a tab-only model aborts their export.
//
// The guard is BIDIRECTIONAL: the doc needles pin the carve-out sentence, the code
// arms pin the ordering that makes the carve-out true (the non-printing-rune
// hard-fail runs before the blank-after-trimming keep arm, and its predicate is
// IsControl/Cf). A membership probe verifies the carve-out's rune list against
// unicode.IsControl itself, so the doc and the code cannot drift apart silently.
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift
// tests (no Go package lives under docs/), mirroring the sibling doc tests.
func TestScorecardDoc_WhitespaceControlRuneCarveOut(t *testing.T) {
	doc := readRepoFile(t, "../../docs/scorecard.md")
	leaderboard := readRepoFile(t, "../../cli/leaderboard.go")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	docNeedles := []struct {
		name    string
		needle  string
		because string
	}{
		{
			name:    "carve-out named",
			needle:  "whitespace that is also a control rune",
			because: "the paragraph must scope its keep-and-warn claim to whitespace that is NOT a control rune",
		},
		{
			name:    "carve-out routed to the rejection arm",
			needle:  "falls under the control and format rune rejection",
			because: "the operator must learn a tab-only identity aborts the export rather than warn-and-publish",
		},
	}
	for _, n := range docNeedles {
		if !strings.Contains(doc, n.needle) {
			t.Errorf("docs/scorecard.md blank-identity paragraph must carry the carve-out: %s (%s)", n.name, n.because)
		}
	}

	// Behaviour-bearing code arm #1: in cli/leaderboard.go the non-printing-rune
	// hard-fail is the FIRST check inside the field loop, before the
	// blank-after-trimming keep arm — that ordering is what makes the carve-out true.
	codeNeedle := "if r, bad := firstNonPrintingRune(f.value); bad {"
	if !strings.Contains(leaderboard, codeNeedle) {
		t.Errorf("cli/leaderboard.go selectPublishableRecordIdentities must keep the non-printing-rune hard-fail ahead of the keep arm (%s)", codeNeedle)
	}

	// Behaviour-bearing code arm #2: the predicate itself. If IsControl/Cf ever
	// stops covering the runes the doc names, the carve-out becomes false in the
	// other direction (the doc would reject identities the code keeps).
	predicateNeedle := "unicode.IsControl(r) || unicode.Is(unicode.Cf, r)"
	if !strings.Contains(coverage, predicateNeedle) {
		t.Errorf("cli/benchmark_coverage.go firstNonPrintingRune must keep the IsControl/Cf predicate the carve-out is written against (%s)", predicateNeedle)
	}

	// Membership probe: every rune the carve-out names as "whitespace that is also a
	// control rune" IS a control rune (so firstNonPrintingRune really rejects it),
	// and ordinary/NBSP whitespace is NOT (so it really stays in the keep-and-warn
	// arm). If either half fails, the doc's rune list no longer matches
	// unicode.IsControl and the carve-out text is wrong.
	controlWhitespace := []rune{'\t', '\n', '\v', '\f', '\r', '\u0085'}
	for _, r := range controlWhitespace {
		if !unicode.IsControl(r) {
			t.Errorf("rune U+%04X is named by the carve-out as a control rune but unicode.IsControl says otherwise", r)
		}
	}
	for _, r := range []rune{' ', '\u00a0'} {
		if unicode.IsControl(r) {
			t.Errorf("rune U+%04X must stay in the keep-and-warn arm but unicode.IsControl says it is a control rune", r)
		}
	}
}
