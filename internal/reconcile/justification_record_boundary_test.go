package reconcile

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The block-boundary detector and the findings parser read the SAME bytes: fanout
// writes review.md as a byte-identical copy of the model content it hands to
// stream.ParseModelOutput. So any line the detector calls a "record" but the parser
// calls prose is, by construction, narrative — and ending a justification block on it
// silently truncates a reviewer's reasoning. That loss is permanent: localdebt writes
// Justification into an append-only store whose id excludes it, so the first reconcile
// after a truncation is the only one that ever gets to be right.
//
// stream.ParseModelOutput is therefore the ORACLE here, not a second opinion. Pinning
// against the real parser rather than against a restated pattern is what makes the
// detector's doc claim ("cannot drift from the parser") checkable instead of hopeful.
func TestIsFindingRecordStart_AgreesWithTheProducingParser(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		// Real records — column 0, uppercase, pipe adjacent, a location present.
		{"canonical CRITICAL", `CRITICAL|a.go:10|p|f|c|1|e`},
		{"canonical HIGH", `HIGH|a.go:10|p|f|c|1|e`},
		{"canonical MEDIUM", `MEDIUM|a.go:10|p|f|c|1|e`},
		{"canonical LOW", `LOW|a.go:10|p|f|c|1|e`},
		{"minimum three fields", `HIGH|a.go:10|p`},

		// Divergence classes: prose the OLD detector accepted and the parser never did.
		{"lowercase severity", `high|a.go:10|p|f|c|1|e`},
		{"indented and spaced table row", `   High | the goroutine leaks on every retry`},
		{"space before the pipe", `HIGH |a.go:10|p|f`},
		{"indented degenerate", `  high|`},
		{"bare severity, no location", `HIGH|`},
		{"empty location field", `HIGH||p|f`},
		{"leading-pipe markdown table row", `| High | x |`},
		{"markdown table header", `Severity | Impact`},
		{"unknown severity word", `info|x|y`},
		{"severity mid-sentence", `The HIGH|LOW split is described below`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// One line in, one line out: the parser yields a finding iff it considers
			// this line a record.
			parserSaysRecord := len(stream.ParseModelOutput([]byte(c.line))) == 1
			assert.Equal(t, parserSaysRecord, isFindingRecordStart(c.line),
				"line %q: stream.ParseModelOutput and isFindingRecordStart must agree on "+
					"whether this is a finding record — a line the parser calls prose is prose, "+
					"and breaking a justification block on it destroys narrative permanently", c.line)
		})
	}
}

// The three shapes below were MEASURED truncating real justifications. Each asserts
// the whole block survives, byte for byte — a partial excerpt is indistinguishable
// from a reviewer who wrote one sentence, because no truncation marker is emitted.
func TestExtractSection_ProseIsNotABlockBoundary(t *testing.T) {
	cases := []struct {
		name  string
		doc   string
		idx   int
		wants []string // every line that must survive in the excerpt
	}{
		{
			name: "markdown table whose data row starts with a severity word",
			doc: "## Findings\n" +
				"1. **`internal/fanout/engine.go:88`** — the slot is never closed on the retry path.\n" +
				"   Severity | Impact\n" +
				"   High | the goroutine leaks on every retry, so a long run exhausts the pool\n" +
				"   Fix: close the slot in a defer so the retry path cannot skip it.",
			idx:   1,
			wants: []string{"Severity | Impact", "the goroutine leaks on every retry", "Fix: close the slot in a defer"},
		},
		{
			name: "fenced example row the parser itself skips",
			doc: "## Notes\n" +
				"The finding for `internal/auth/token.go:42` is emitted in the standard row shape:\n" +
				"```\n" +
				"HIGH|internal/auth/token.go:42|forged token accepted|verify the signature|security|20|jwt.Parse without Verify\n" +
				"```\n" +
				"which is why the EVIDENCE column carries the call site rather than the diff.",
			idx:   1,
			wants: []string{"standard row shape", "forged token accepted", "EVIDENCE column carries the call site"},
		},
		{
			name: "degenerate severity-prefixed line the parser drops",
			doc: "## X\n" +
				"See `a.go:10` for the leak.\n" +
				"  high|\n" +
				"continued explanation that is now unreachable",
			idx:   1,
			wants: []string{"See `a.go:10` for the leak.", "continued explanation that is now unreachable"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, _ := extractSection(strings.Split(c.doc, "\n"), c.idx)
			for _, w := range c.wants {
				assert.Contains(t, text, w,
					"the excerpt dropped %q — a line the findings parser treats as prose "+
						"must not end the narrative block", w)
			}
		})
	}
}

// The boundary that MUST hold: a genuine record still separates its neighbours, so
// two findings anchored at different lines never receive byte-identical text. This is
// the defect the detector was introduced to fix, and tightening it must not undo that.
func TestExtractSection_GenuineRecordsStillBoundTheirNeighbours(t *testing.T) {
	lines := []string{
		"## Findings",
		"CRITICAL|a.go:10|first problem|first fix|correctness|30|first evidence",
		"HIGH|b.go:20|second problem|second fix|correctness|30|second evidence",
		"LOW|c.go:30|third problem|third fix|correctness|30|third evidence",
	}

	first, _ := extractSection(lines, 1)
	second, _ := extractSection(lines, 2)

	require.NotEqual(t, first, second,
		"consecutive finding records must not fold into one excerpt — that is the "+
			"cross-contamination this boundary exists to prevent")
	assert.NotContains(t, second, "first problem",
		"a record must not absorb the record above it")
	assert.NotContains(t, first, "second problem",
		"a record must not absorb the record below it")
}

// A fence marker toggles state; content between markers is an EXAMPLE, never a record.
// Pinned directly (not only through extractSection) so the parity rule stays legible.
func TestFenceMask_MarksOnlyContentBetweenMarkers(t *testing.T) {
	lines := []string{
		"before",
		"```",
		"HIGH|a.go:10|p|f|c|1|e",
		"```",
		"after",
	}

	mask := fenceMask(lines)

	assert.False(t, mask[0], "prose before the fence is not inside it")
	assert.False(t, mask[1], "the opening marker is not itself inside the fence")
	assert.True(t, mask[2], "content between markers is inside the fence")
	assert.False(t, mask[3], "the closing marker is not inside the fence")
	assert.False(t, mask[4], "prose after the fence is not inside it")
}
