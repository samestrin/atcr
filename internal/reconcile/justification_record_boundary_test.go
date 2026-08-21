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
		// The COMPACT leading-pipe row is the one that actually discriminates the
		// column-0 anchor. Its padded sibling above is rejected by the space before
		// the severity word, so it stays false even if the anchor is loosened to
		// tolerate a leading pipe — verified by mutation. Only this shape, where the
		// severity abuts the opening pipe, flips. It is an ordinary unpadded markdown
		// table row, and the parser calls it prose.
		{"compact leading-pipe table row", `|HIGH|a.go:10|p|f|c|1|e`},
		{"compact leading-pipe, minimum fields", `|LOW|a.go:1|p`},
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
// the whole block survives — a partial excerpt is indistinguishable from a reviewer
// who wrote one sentence, because no truncation marker is emitted. FENCED shapes
// survive as one "[quoted example elided]" placeholder per region: the block still
// crosses the fence (the prose below it is asserted), but the quoted content itself
// no longer rides into the excerpt, because a quote of any length absorbed whole
// spends the justificationMaxRunes budget on code and pushes the reviewer's prose
// past the truncation ellipsis.
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
			wants: []string{"standard row shape", "[quoted example elided]", "EVIDENCE column carries the call site"},
		},
		{
			name: "fenced example containing a heading",
			doc: "## Notes\n" +
				"The reviewer quoted the section header it was reading:\n" +
				"```\n" +
				"# Findings\n" +
				"```\n" +
				"and the header is why the paths in this run are repo-relative.",
			idx:   1,
			wants: []string{"quoted the section header", "[quoted example elided]", "paths in this run are repo-relative"},
		},
		{
			name: "fenced example containing a bullet",
			doc: "## Notes\n" +
				"The reviewer quoted the checklist it was handed:\n" +
				"```\n" +
				"- verify the signature before trusting the claims\n" +
				"```\n" +
				"and the checklist is why this finding cites jwt.Parse rather than the handler.",
			idx:   1,
			wants: []string{"quoted the checklist", "[quoted example elided]", "cites jwt.Parse rather than the handler"},
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

	_, mask, _ := fenceMask(lines)

	assert.False(t, mask[0], "prose before the fence is not inside it")
	assert.False(t, mask[1], "the opening marker is not itself inside the fence")
	assert.True(t, mask[2], "content between markers is inside the fence")
	assert.False(t, mask[3], "the closing marker is not inside the fence")
	assert.False(t, mask[4], "prose after the fence is not inside it")
}

// isFenceMarker trims leading spaces and tabs before looking for the backtick run,
// mirroring stream/parser.go:194 exactly. An INDENTED fence is the ordinary shape a model
// emits when it quotes an example inside a numbered list or a nested bullet, so this is
// the common case rather than an exotic one.
//
// Nothing exercised the trim: every fixture in the package opened its fence at column 0,
// so deleting the TrimLeft left the whole suite green. Without it an indented fence is
// invisible, the example row inside it reads as a real record, and extractSection ends the
// narrative on the parser's own counter-example — the exact defect fence-awareness was
// added to prevent.
func TestFenceMask_RecognizesAnIndentedFence(t *testing.T) {
	for _, c := range []struct{ name, indent string }{
		{"two spaces, a nested list item", "  "},
		{"three spaces, under an ordered marker", "   "},
		{"a tab", "\t"},
		{"four spaces", "    "},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines := []string{
				"before",
				c.indent + "```",
				c.indent + "HIGH|a.go:10|p|f|c|1|e",
				c.indent + "```",
				"after",
			}

			_, mask, _ := fenceMask(lines)

			assert.False(t, mask[1], "the opening marker is not itself inside the fence")
			assert.True(t, mask[2],
				"an indented fence still opens a fenced block — parser.go:194 trims the same "+
					"leading space/tab run before testing for the backticks")
			assert.False(t, mask[3], "the closing marker is not inside the fence")
			assert.False(t, mask[4], "prose after the fence is not inside it")
		})
	}
}

// The consequence at the level that matters, pinned through extractSection so the trim is
// protected by what it is FOR and not merely by how it is implemented.
//
// The row inside the fence sits at COLUMN 0 deliberately, and that detail is the whole
// test: an INDENTED row fails recordRe's column-0 anchor on its own, so it survives
// whether or not the fence was recognized, and a fixture built that way would pass with
// the TrimLeft deleted — proving nothing. Only a column-0 row inside an indented fence
// depends on the mask, and it is the natural shape here, since the point of the quote is
// to show the row exactly as emitted.
func TestExtractSection_IndentedFenceStillHidesAColumnZeroExampleRow(t *testing.T) {
	doc := "## Notes\n" +
		"1. The finding for `internal/auth/token.go:42` is emitted in the standard row shape:\n" +
		"   ```\n" +
		"HIGH|internal/auth/token.go:42|forged token accepted|verify the signature|security|20|jwt.Parse\n" +
		"   ```\n" +
		"   which is why the EVIDENCE column carries the call site rather than the diff."

	text, _ := extractSection(strings.Split(doc, "\n"), 1)

	assert.Contains(t, text, "standard row shape")
	assert.Contains(t, text, "EVIDENCE column carries the call site",
		"an indented fence still hides the example inside it; missing the fence makes that "+
			"example read as a real record and truncates the reviewer's reasoning at it, with "+
			"no marker to show it happened")
}

// An UNTERMINATED fence must not poison the rest of the document — in the RELEASED
// view. The strict view keeps the tail masked, byte-parity with the parser.
//
// fenceMask toggles on every marker, so a dangling opener leaves the strict mask true
// to EOF. That is survivable while only recordAt consults it, but headingAt, itemAt
// AND the section walk-up read the released mask: with the tail masked there, every
// boundary shape goes dead, so each finding below the fence gets the WHOLE remaining
// document as its excerpt (byte-identical to its siblings') and resolves its section
// to the last heading ABOVE the fence. A model that opens a fence and forgets to
// close it is an ordinary output defect, not a rare one, and the damage is permanent:
// localdebt persists Justification append-only and Record.StampID hashes
// file/line/problem only, so the wrong text never gets a second chance.
func TestFenceMask_UnterminatedFenceDoesNotMaskTheTail(t *testing.T) {
	lines := []string{
		"# Real Section",
		"```",
		"quoted example",
		"# Fake Heading",
		"- fake bullet",
	}

	strict, released, _ := fenceMask(lines)

	for i, l := range lines {
		assert.False(t, released[i],
			"line %d (%q): a fence that is never closed must mask nothing in the released view — masking to EOF kills every boundary predicate below it", i, l)
	}
	assert.True(t, strict[2] && strict[3] && strict[4],
		"the strict view keeps the tail masked: the parser emits nothing below the dangling opener, so recordAt must not either")
}

// The end-to-end shape of the same defect: three findings under a heading BELOW an
// unclosed fence must keep three distinct excerpts and resolve to the heading that
// really encloses them. Asserted through extractSection because the mask alone does
// not show the consequence the row was filed for.
func TestExtractSection_UnterminatedFenceAboveAFindingsList(t *testing.T) {
	lines := []string{
		"# Real Section",
		"```",
		"an example the model never closed",
		"",
		"## Findings",
		"- first problem at a.go:10",
		"- second problem at a.go:20",
		"- third problem at a.go:30",
	}

	var texts []string
	for _, idx := range []int{5, 6, 7} {
		text, section := extractSection(lines, idx)
		assert.Equal(t, "Findings", section,
			"line %d must resolve to the heading that encloses it, not to the one above the unclosed fence", idx)
		texts = append(texts, text)
	}

	assert.NotEqual(t, texts[0], texts[1], "each finding must keep its own excerpt, not the swallowed tail")
	assert.NotEqual(t, texts[1], texts[2], "each finding must keep its own excerpt, not the swallowed tail")
	assert.Contains(t, texts[0], "first problem")
	assert.NotContains(t, texts[0], "second problem", "an unclosed fence above must not fold the list into one block")
}

// A record-shaped line in the tail of an UNTERMINATED fence is a boundary to nothing.
// The producing parser's bare inFence toggle (stream/parser.go:152-157) skips every
// line below the dangling opener, so the line was never emitted as a record — yet the
// released mask let recordAt read it as one, and extractSection ended the narrative on
// a line the parser never saw. That loss is permanent: localdebt persists
// Justification append-only under an id that excludes it. recordAt must read the
// UN-released (strict) mask — its contract is byte-exact parser parity — while the
// release applies only to headingAt, itemAt and the section walk-up.
func TestExtractSection_RecordShapedLineBelowADanglingFenceIsNotABoundary(t *testing.T) {
	doc := "## Findings\n" +
		"- The verdict rests on the quoted exchange:\n" +
		"```\n" +
		"the model opened a quote it never closed\n" +
		"HIGH|a.go:10|quoted row, not a record|fix|correctness|30|evidence\n" +
		"and the narrative continues after it"
	lines := strings.Split(doc, "\n")

	text, _ := extractSection(lines, 5)

	assert.Contains(t, text, "never closed",
		"a record-shaped line the parser never emitted must not split the narrative")
	assert.Contains(t, text, "continues after it")
}

// A fenced quote the block walk crosses is ELIDED, not absorbed. Masking
// headingAt/itemAt means a quoted diff — every `- removed` line satisfies
// isItemStart — no longer ends the block, so without elision the whole quote rides
// into the excerpt and spends the justificationMaxRunes budget on quoted code,
// pushing the reviewer's own prose past the truncation ellipsis. The budget must
// buy prose on both sides of the fence instead. Consumers make this permanent:
// emit.go hands Justification to localdebt's append-only store, and debt_resolve
// accepts a non-empty Justification as the wontfix rationale — a truncated diff
// hunk cannot stand in for human-typed reasoning.
func TestExtractSection_FencedQuoteInsideABlockIsElided(t *testing.T) {
	doc := "## Findings\n" +
		"- The finding is justified by the before/after diff:\n" +
		"```diff\n" +
		"- removed line\n" +
		"+ added line\n" +
		"```\n" +
		"which is why the fix narrows the predicate"
	lines := strings.Split(doc, "\n")

	text, _ := extractSection(lines, 1)

	assert.Contains(t, text, "before/after diff", "the prose above the fence stays")
	assert.Contains(t, text, "narrows the predicate",
		"the prose BELOW the fence stays — the walk crosses the fence by design")
	assert.NotContains(t, text, "removed line",
		"quoted code is elided, not absorbed into the rune budget")
	assert.Contains(t, text, "elided", "a placeholder marks where the quote went")
}

// The section walk-up's `if fenced[j] { continue }` needs an anchor BELOW a fenced
// heading, with the real heading ABOVE the fence — the one arrangement in which
// skipping fenced lines changes the answer.
//
// Every other fence case in this package puts the anchor at index 1 with the
// heading at index 0, above the fence, so the walk-up stops on the real heading
// before it ever meets a fenced line and the guard is executed-but-not-exercised:
// deleting it leaves the suite green. Here the first heading the walk-up meets is
// quoted example text, so without the guard the excerpt is attributed to "Fake
// Heading" — a section that does not exist in the document — which is the
// mis-attribution the fence-aware walk-up was introduced to fix. The wrong section
// is permanent once written: localdebt persists Justification append-only.
func TestExtractSection_WalkUpSkipsAHeadingInsideAFence(t *testing.T) {
	lines := []string{
		"# Real Section",
		"",
		"```",
		"# Fake Heading",
		"```",
		"",
		"the finding narrative",
	}

	_, section := extractSection(lines, 6)

	assert.Equal(t, "Real Section", section,
		"the section walk-up must skip a heading that only exists inside a fenced example and keep climbing to the real one")
}

// A findings TABLE inside a fence is the single most common review.md shape, and
// an anchor landing on one of its rows makes every line of the resolved block
// fenced: the walk-up stops at the opener and the walk-down at the closer, so the
// whole excerpt collapses to the elision placeholder. Before the elision existed,
// extractSection's text was structurally never content-free — the anchor line
// itself is always non-blank — so matchNarrative's `if text == ""` guard had
// nothing to catch. It does now, and a placeholder-only excerpt must reach it as
// the empty string rather than as 23 bytes of tool-generated text.
//
// The damage is permanent if it escapes: localdebt writes Justification into an
// append-only store whose id EXCLUDES the field (record.go), so no later
// reconcile can replace a content-free value even after the reviewer reformats
// the fence away — and cli/debt_resolve accepts any non-empty Justification as
// the recorded rationale for a terminal wontfix.
func TestExtractSection_PlaceholderOnlyExcerptIsEmptyNotAPlaceholder(t *testing.T) {
	lines := []string{
		"## Review",
		"",
		"```",
		"| internal/x.go:42 | HIGH | something |",
		"```",
	}

	text, section := extractSection(lines, 3)

	assert.Equal(t, "", text,
		"an excerpt made of nothing but elision placeholders carries zero reviewer content and must read as no narrative at all")
	assert.Equal(t, "Review", section,
		"the section walk-up is unaffected — only the text is suppressed")
}

// The guard above only pays off if matchNarrative actually falls through on it:
// ok=false is what leaves Justification and SourceReport unset, which is the
// documented \"no narrative\" state. A placeholder-only text reaching ok=true
// would ALSO stamp a SourceReport pointing at a line INSIDE a quoted example.
func TestMatchNarrative_PlaceholderOnlyExcerptYieldsNoMatch(t *testing.T) {
	narratives := []reviewNarrative{{
		relPath: "review.md",
		leaf:    "alice",
		lines: []string{
			"## Review",
			"",
			"```",
			"| internal/x.go:42 | HIGH | something |",
			"```",
		},
	}}

	_, ok := matchNarrative(narratives, buildAnchorIndex(narratives), "internal/x.go", 42, []string{"alice"})

	assert.False(t, ok,
		"an anchor whose whole block is fenced has no reviewer prose to stamp — the field must stay omitted, not carry a placeholder")
}

// headingAt reads the RELEASED view, and that choice is the half of the fenceMask
// docstring's argument that nothing pinned: swapping it for strict left the suite
// green while itemAt's and recordAt's twins were both killed. Under strict, a
// heading in the tail of an UNTERMINATED fence is not a block boundary, so the walk
// -down absorbs it and keeps going — "a model that opens a fence and forgets to
// close it takes the whole rest of the document with it", and every finding below
// collapses to one byte-identical excerpt. The loss is permanent: localdebt persists
// Justification into an append-only store whose id excludes the field.
func TestExtractSection_HeadingBelowADanglingFenceStillBoundsTheBlock(t *testing.T) {
	lines := []string{
		"# Real Section",
		"",
		"```", // opened and never closed
		"the finding narrative for internal/x.go:42",
		"# Later Heading",
		"prose belonging to the later section",
	}

	text, _ := extractSection(lines, 3)

	assert.Contains(t, text, "the finding narrative",
		"precondition: the anchor's own line is in the excerpt")
	assert.NotContains(t, text, "Later Heading",
		"a heading in the released tail of a dangling fence must bound the block — reading the strict view here lets one unterminated fence swallow the rest of the document")
	assert.NotContains(t, text, "prose belonging to the later section",
		"and everything under that heading with it")
}

// The section walk-up's own `if released[j] { continue }` is the twin read: under
// strict it skips every line in a dangling fence's tail, so an excerpt anchored
// BELOW a heading in that tail is attributed to the last heading ABOVE the opener —
// a section the finding is not in.
func TestExtractSection_SectionResolvesToAHeadingInsideADanglingFenceTail(t *testing.T) {
	lines := []string{
		"# Section Above The Fence",
		"",
		"```", // opened and never closed
		"quoted example text",
		"# Section Below The Opener",
		"",
		"the finding narrative for internal/x.go:42",
	}

	_, section := extractSection(lines, 6)

	assert.Equal(t, "Section Below The Opener", section,
		"a dangling opener releases its tail, so the headings in it are real structure again — attributing the excerpt to the section above the opener names a section the finding is not in")
}

// The elision-membership branch decides whether a fence MARKER is absorbed into the
// placeholder or rendered as reviewer prose, and the whole branch survived mutation:
// `if false` left the suite green, and so did keeping only either disjunct. Nothing
// stood between a clean "[quoted example elided]" and an excerpt with stray ```
// lines in it. These cases cover one per disjunct plus both block boundaries, and
// the empty fence — the shape neither disjunct reaches, because neither of its
// markers has a masked neighbour.
func TestExtractSection_NoFenceMarkerSurvivesIntoTheExcerpt(t *testing.T) {
	cases := []struct {
		name string
		doc  []string
		idx  int
		want string // a fragment of reviewer prose that must survive
	}{
		{
			// Opener above masked content (disjunct 1) and closer below it
			// (disjunct 2), both mid-block with prose on either side.
			name: "fence between prose",
			doc:  []string{"## Findings", "- the finding narrative", "```", "quoted example", "```", "and the conclusion"},
			idx:  1,
			want: "the conclusion",
		},
		{
			// The block STARTS on the opener: the walk-up absorbs it, so j == start
			// and the `j > 0` half of disjunct 2 is not what saves it.
			name: "fence at block start",
			doc:  []string{"## Findings", "```", "quoted example", "```", "prose after the fence"},
			idx:  2,
			want: "prose after the fence",
		},
		{
			// The block ENDS on the closer: j+1 is past `end`, so the `j+1 <
			// len(lines)` half of disjunct 1 is not what saves it.
			name: "fence at block end",
			doc:  []string{"- the finding narrative", "```", "quoted example", "```"},
			idx:  0,
			want: "the finding narrative",
		},
		{
			// An EMPTY fence: neither marker has a masked neighbour, so neither
			// disjunct fires and both fall through to the prose branch. This is the
			// one shape that still violated the rule the elision established.
			name: "empty fence",
			doc:  []string{"## Findings", "", "- the finding narrative", "```", "```", "- a sibling finding"},
			idx:  2,
			want: "the finding narrative",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, _ := extractSection(c.doc, c.idx)

			assert.Contains(t, text, c.want, "precondition: the reviewer's prose must survive")
			assert.NotContains(t, text, "```",
				"a fence marker rendered as reviewer prose is indistinguishable from the reviewer typing backticks, and the value is persisted append-only")
		})
	}
}
