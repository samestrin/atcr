package benchmark

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func exp(id, file string, start, end, tol int, outside bool) ExpectedFinding {
	t := tol
	o := outside
	return ExpectedFinding{
		ID: id, File: file, LineStart: start, LineEnd: end,
		LineTolerance: &t, OutsideDiff: &o, Category: "correctness", Summary: "s",
	}
}

// matchedIDs is the outcome under test in almost every case below: WHICH expected
// findings were hit. Comparing the id set rather than the whole slice keeps a
// failure readable.
func matchedIDs(ms []FindingMatch) []string {
	var out []string
	for _, m := range ms {
		if m.Matched {
			out = append(out, m.Expected.ID)
		}
	}
	return out
}

// An empty line map stands in for "the diff is irrelevant to this test" — only an
// outside_diff:true expectation consults it.
func noDiff(t *testing.T) DiffLineMap {
	t.Helper()
	lm, err := ParseDiffLineMap(nil)
	require.NoError(t, err)
	return lm
}

// AC3 condition 2, both directions in one test: inside the window matches, well
// outside it does not. 40 lines away is the plan's own example.
func TestMatchFindings_LineToleranceWindow(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}

	for _, tt := range []struct {
		name string
		line int
		want bool
	}{
		{"exact", 50, true},
		{"lower edge of window", 47, true},
		{"upper edge of window", 53, true},
		{"one past the lower edge", 46, false},
		{"one past the upper edge", 54, false},
		{"forty lines away", 90, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: tt.line}}, noDiff(t))
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0].Matched)
		})
	}
}

// The window is measured from the RANGE, not from a single line: line_start minus
// tolerance through line_end plus tolerance.
func TestMatchFindings_WindowSpansTheWholeRange(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 20, 30, 2, false)}
	for _, line := range []int{18, 25, 32} {
		got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: line}}, noDiff(t))
		assert.True(t, got[0].Matched, "line %d is inside [18,32]", line)
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 33}}, noDiff(t))
	assert.False(t, got[0].Matched, "line 33 is outside [18,32]")
}

// The tolerance is PER FINDING and data-driven. A hardcoded ±3 would pass the test
// above and fail this one.
func TestMatchFindings_ToleranceIsPerFinding(t *testing.T) {
	wide := []ExpectedFinding{exp("wide", "pkg/a.py", 50, 50, 10, false)}
	narrow := []ExpectedFinding{exp("narrow", "pkg/a.py", 50, 50, 0, false)}
	report := []ReportedFinding{{File: "pkg/a.py", Line: 58}}

	assert.True(t, MatchFindings(wide, report, noDiff(t))[0].Matched)
	assert.False(t, MatchFindings(narrow, report, noDiff(t))[0].Matched)
	assert.True(t, MatchFindings(narrow, []ReportedFinding{{File: "pkg/a.py", Line: 50}}, noDiff(t))[0].Matched,
		"tolerance 0 still matches the exact line")
}

// AC3 condition 1: the file must be equal, exactly. A right-line finding in the
// wrong file is not the same defect.
func TestMatchFindings_FileMustMatchExactly(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}
	for _, file := range []string{"pkg/b.py", "other/pkg/a.py", "a.py", "pkg/A.py"} {
		got := MatchFindings(expected, []ReportedFinding{{File: file, Line: 50}}, noDiff(t))
		assert.False(t, got[0].Matched, "file %q must not match pkg/a.py", file)
	}
}

// A reviewer often copies the path straight out of the diff header, prefix and
// all. The grounding gate already accepts those spellings — it strips them for its
// own lookup and never rewrites the finding — so a citation that CLEARED the gate
// arrives here with its prefix intact. Rejecting it would make the tier's headline
// metric turn on a cosmetic path habit.
func TestMatchFindings_ToleratesDiffArtifactPathPrefixes(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}
	for _, cited := range []string{"pkg/a.py", "b/pkg/a.py", "a/pkg/a.py", "./pkg/a.py", "/pkg/a.py", " pkg/a.py "} {
		got := MatchFindings(expected, []ReportedFinding{{File: cited, Line: 50}}, noDiff(t))
		assert.True(t, got[0].Matched, "a citation spelled %q must reach pkg/a.py", cited)
	}
}

// The a//b/ strip is CONDITIONAL, not unconditional. `a/b.py` is a legal
// repository path, and eating its first segment would make an exactly-correct
// citation fail to match the file it names.
func TestMatchFindings_DoesNotEatARealLeadingASegment(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "a/b.py", 50, 50, 3, false)}

	got := MatchFindings(expected, []ReportedFinding{{File: "a/b.py", Line: 50}}, noDiff(t))
	assert.True(t, got[0].Matched, "the verbatim path must match itself")

	got = MatchFindings(expected, []ReportedFinding{{File: "b.py", Line: 50}}, noDiff(t))
	assert.False(t, got[0].Matched, "a different file must not match by accident of the strip")
}

// N reports of one defect score as ONE hit. Without this a reviewer that repeated
// itself would out-score one that reported cleanly.
func TestMatchFindings_ManyReportsOfOneDefectScoreOnce(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}
	reported := []ReportedFinding{
		{File: "pkg/a.py", Line: 49},
		{File: "pkg/a.py", Line: 50},
		{File: "pkg/a.py", Line: 51},
	}
	got := MatchFindings(expected, reported, noDiff(t))
	require.Len(t, got, 1)
	assert.True(t, got[0].Matched)
	assert.Equal(t, []string{"f"}, matchedIDs(got))
}

// A reported finding matches AT MOST ONE expected finding. Two expectations whose
// windows overlap must not both be credited to a single report.
func TestMatchFindings_OneReportSatisfiesAtMostOneExpected(t *testing.T) {
	expected := []ExpectedFinding{
		exp("near", "pkg/a.py", 50, 50, 5, false),
		exp("far", "pkg/a.py", 54, 54, 5, false),
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 51}}, noDiff(t))
	assert.Equal(t, []string{"near"}, matchedIDs(got), "51 is nearer 50 than 54, and cannot satisfy both")
}

// Ties resolve to the NEAREST range midpoint — measured from the midpoint, not
// from line_start, so a wide range and a narrow one compete fairly.
func TestMatchFindings_TieResolvesToNearestMidpoint(t *testing.T) {
	expected := []ExpectedFinding{
		exp("wide-range", "pkg/a.py", 40, 60, 5, false),   // midpoint 50
		exp("narrow-range", "pkg/a.py", 44, 44, 5, false), // midpoint 44
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 49}}, noDiff(t))
	assert.Equal(t, []string{"wide-range"}, matchedIDs(got), "49 is 1 from midpoint 50 and 5 from midpoint 44")
}

// An EXACT midpoint tie breaks alphabetically by id, so the outcome is a property
// of the data and not of map or slice order.
func TestMatchFindings_ExactTieBreaksAlphabeticallyByID(t *testing.T) {
	expected := []ExpectedFinding{
		exp("zulu", "pkg/a.py", 50, 50, 5, false),
		exp("alpha", "pkg/a.py", 50, 50, 5, false),
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 50}}, noDiff(t))
	assert.Equal(t, []string{"alpha"}, matchedIDs(got))
}

// Matching must be ORDER-INDEPENDENT: the same reports in a different order
// produce the same outcome, or two runs of one reviewer are not comparable.
func TestMatchFindings_IsOrderIndependent(t *testing.T) {
	expected := []ExpectedFinding{
		exp("a-one", "pkg/a.py", 10, 10, 3, false),
		exp("b-two", "pkg/a.py", 12, 12, 3, false),
	}
	forward := []ReportedFinding{{File: "pkg/a.py", Line: 10}, {File: "pkg/a.py", Line: 12}}
	backward := []ReportedFinding{{File: "pkg/a.py", Line: 12}, {File: "pkg/a.py", Line: 10}}

	assert.Equal(t, matchedIDs(MatchFindings(expected, forward, noDiff(t))),
		matchedIDs(MatchFindings(expected, backward, noDiff(t))))
	assert.Equal(t, []string{"a-one", "b-two"}, matchedIDs(MatchFindings(expected, backward, noDiff(t))))
}

// Results come back one per expected finding, in the case's own order, so a caller
// can report per-finding outcomes without re-joining on id.
func TestMatchFindings_ReturnsOneRowPerExpectedInCaseOrder(t *testing.T) {
	expected := []ExpectedFinding{
		exp("first", "pkg/a.py", 10, 10, 3, false),
		exp("second", "pkg/b.py", 20, 20, 3, false),
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/b.py", Line: 20}}, noDiff(t))
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Expected.ID)
	assert.False(t, got[0].Matched)
	assert.Equal(t, "second", got[1].Expected.ID)
	assert.True(t, got[1].Matched)
	assert.Equal(t, 0, got[1].ReportedIndex, "the matching report is identified, not just counted")
	assert.Equal(t, -1, got[0].ReportedIndex, "an unmatched expectation names no report")
}

// A non-positive reported line carries no position, so it cannot settle a
// positional expectation. Crediting it would make a file-level finding match every
// expectation in that file.
func TestMatchFindings_IgnoresFindingsWithNoLine(t *testing.T) {
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 0}}, noDiff(t))
	assert.False(t, got[0].Matched)
}

// ---- AC3b: condition 3, the measurement that makes outside_diff real ----

// diffAddingLine50 is a diff whose only added line is head line 50 of pkg/a.py.
func diffAddingLine50(t *testing.T) DiffLineMap {
	t.Helper()
	lm, err := ParseDiffLineMap([]byte(
		"diff --git a/pkg/a.py b/pkg/a.py\n" +
			"--- a/pkg/a.py\n" +
			"+++ b/pkg/a.py\n" +
			"@@ -48,3 +48,4 @@\n" +
			" ctx48\n" +
			" ctx49\n" +
			"+added50\n" +
			" ctx51\n"))
	require.NoError(t, err)
	return lm
}

// The headline of AC3b. Same report, same window, two expectations that differ
// ONLY in outside_diff — and they must come out differently, or outside_diff is a
// label rather than a measurement.
func TestMatchFindings_AddedLineCannotSatisfyAnOutsideDiffExpectation(t *testing.T) {
	report := []ReportedFinding{{File: "pkg/a.py", Line: 50}}
	lm := diffAddingLine50(t)

	outside := MatchFindings([]ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, true)}, report, lm)
	assert.False(t, outside[0].Matched,
		"citing an ADDED line must not satisfy an outside_diff:true expectation")

	inside := MatchFindings([]ExpectedFinding{exp("f", "pkg/a.py", 50, 50, 3, false)}, report, lm)
	assert.True(t, inside[0].Matched,
		"the same report DOES satisfy an outside_diff:false expectation")
}

// Condition 3 exists because the tolerance window BRUSHES the change. A settling
// line three away from an added line is inside the window, and a reviewer citing
// the added line instead must not be credited — while one citing the settling line
// itself must be.
func TestMatchFindings_OutsideDiffStillMatchesAnUntouchedLineInTheWindow(t *testing.T) {
	lm := diffAddingLine50(t)
	expected := []ExpectedFinding{exp("f", "pkg/a.py", 52, 52, 3, true)}

	assert.False(t, MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 50}}, lm)[0].Matched,
		"line 50 is added; inside the window is not enough")
	assert.True(t, MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 52}}, lm)[0].Matched,
		"line 52 is untouched and inside the window")
	assert.True(t, MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 49}}, lm)[0].Matched,
		"line 49 is a context line, untouched, and inside the window")
}

// A report blocked by condition 3 must stay AVAILABLE to another expectation. If
// the blocked pairing consumed the report, one mis-aimed citation would silently
// cost a second, correct finding.
func TestMatchFindings_ACondition3BlockDoesNotConsumeTheReport(t *testing.T) {
	lm := diffAddingLine50(t)
	expected := []ExpectedFinding{
		exp("a-outside", "pkg/a.py", 50, 50, 3, true),
		exp("b-inside", "pkg/a.py", 50, 50, 3, false),
	}
	got := MatchFindings(expected, []ReportedFinding{{File: "pkg/a.py", Line: 50}}, lm)
	assert.Equal(t, []string{"b-inside"}, matchedIDs(got))
}

// Nearest-midpoint-wins is a GREEDY rule, and greedy is not maximum-cardinality.
// Here a pairing that scores BOTH expectations exists — give line 49 to `a-loose`
// and line 50 to `b-strict` — but the rule hands line 50 to `a-loose`, because
// that pairing is distance 0 and wins the id tie alphabetically. `b-strict` has a
// tolerance of 0, so line 49 cannot settle it, and it goes unmatched.
//
// That is FORMAT.md's rule, not a bug in it: "when several are in range, the one
// whose range midpoint is nearest wins" describes exactly this. A maximizing
// matcher would contradict the stated tie-break and make a reviewer's score depend
// on a global optimization nobody can reproduce by reading their own report.
// Pinned so the tradeoff stays a decision rather than an accident.
func TestMatchFindings_NearestMidpointIsGreedyNotMaximumCardinality(t *testing.T) {
	expected := []ExpectedFinding{
		exp("a-loose", "pkg/a.py", 50, 50, 1, false),  // window [49,51]
		exp("b-strict", "pkg/a.py", 50, 50, 0, false), // window [50,50]
	}
	reported := []ReportedFinding{
		{File: "pkg/a.py", Line: 50}, // settles either
		{File: "pkg/a.py", Line: 49}, // settles a-loose only
	}
	got := MatchFindings(expected, reported, noDiff(t))
	assert.Equal(t, []string{"a-loose"}, matchedIDs(got),
		"greedy takes the distance-0 pairing for a-loose and leaves b-strict, whose "+
			"tolerance of 0 the remaining report cannot reach")
}

func TestMatchFindings_NoExpectationsOrNoReports(t *testing.T) {
	assert.Empty(t, MatchFindings(nil, []ReportedFinding{{File: "pkg/a.py", Line: 1}}, noDiff(t)))

	got := MatchFindings([]ExpectedFinding{exp("f", "pkg/a.py", 1, 1, 3, false)}, nil, noDiff(t))
	require.Len(t, got, 1)
	assert.False(t, got[0].Matched)
}
