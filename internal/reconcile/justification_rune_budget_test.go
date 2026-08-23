package reconcile

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// justificationMaxRunes is denominated in RUNES, and extractSection's growth check
// must be too. Comparing the strings.Builder's BYTE length against it instead makes
// the walk break early on non-ASCII prose — em-dashes and smart quotes, which atcr's
// own reviewers write constantly — so the excerpt comes in at a fraction of the
// documented budget and the reviewer's conclusion is discarded with it.
//
// The placeholder-fit guard's half of the same rune conversion is pinned by
// TestExtractSection_PlaceholderIsNeverCutInHalfByTheRuneBudget. This is the growth
// check's half: without it, reverting `runesWritten >= justificationMaxRunes` to
// `b.Len() >= justificationMaxRunes` leaves the whole suite green.
//
// The two cases are the same section in two encodings. Byte-counting cannot tell them
// apart in the ASCII case (bytes == runes there) and truncates the multi-byte one to
// roughly a third, so a single assertion that BOTH fill the budget is what kills it.
func TestExtractSection_BudgetIsCountedInRunesNotBytes(t *testing.T) {
	const linesOfProse = 60
	const runesPerLine = 40

	build := func(fill string) []string {
		lines := []string{"## Findings"}
		for i := 0; i < linesOfProse; i++ {
			lines = append(lines, strings.Repeat(fill, runesPerLine))
		}
		return lines
	}

	cases := []struct {
		name      string
		fill      string
		bytesPerR int
	}{
		{name: "ascii", fill: "a", bytesPerR: 1},
		// U+2014 EM DASH: 3 bytes, 1 rune. b.Len() reaches justificationMaxRunes after
		// a third of the runes the budget actually funds.
		{name: "multibyte", fill: "—", bytesPerR: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := build(tc.fill)
			// Sanity: the section must hold MORE prose than the budget, or neither
			// counting rule would be observable and the assertion would be vacuous.
			if avail := linesOfProse * runesPerLine; avail <= justificationMaxRunes {
				t.Fatalf("fixture too small to exercise the budget: %d runes available, budget %d",
					avail, justificationMaxRunes)
			}

			text, _ := extractSection(lines, linesOfProse/2)

			gotRunes := utf8.RuneCountInString(text)
			if gotRunes < justificationMaxRunes {
				t.Errorf("excerpt spent only %d of its %d-rune budget (%d bytes, %d bytes/rune);\n"+
					"the growth check is counting bytes, so non-ASCII reviewer prose buys a shorter excerpt than ASCII",
					gotRunes, justificationMaxRunes, len(text), tc.bytesPerR)
			}
		})
	}
}
