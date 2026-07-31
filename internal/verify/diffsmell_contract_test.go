package verify

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// diffsmell_contract_test.go guards the PUBLIC shape of the diff-smell result —
// the JSON a consumer of `atcr verify diff --json` pins to. diffsmell_test.go
// and the testdata/diffsmell corpus own the question of what counts as a smell;
// this file owns only the question of what the answer looks like on the wire.

// contractDiff is the worked example documented in docs/diff-smell.md. It is
// built to exercise all three line cases in one document: a hard smell WITH a
// line (test_skipped), a count-derived smell with NO line (weakened_assertion),
// and a soft added-line smell with a line in a second file (suppression).
const contractDiff = `diff --git a/pkg/auth_test.go b/pkg/auth_test.go
--- a/pkg/auth_test.go
+++ b/pkg/auth_test.go
@@ -10,2 +10,3 @@ func TestAuth(t *testing.T) {
 	setup()
-	require.Equal(t, want, got)
+	require.NotNil(t, got)
+	t.Skip("flaky")
diff --git a/pkg/auth.go b/pkg/auth.go
--- a/pkg/auth.go
+++ b/pkg/auth.go
@@ -20,1 +20,2 @@
 func Auth() error {
+	//nolint:errcheck
`

// goldenContractJSON is the exact marshaled form of AnalyzeDiff(contractDiff).
//
// THIS IS A PUBLIC CONTRACT, NOT AN IMPLEMENTATION DETAIL. A consumer that pins
// to `atcr verify diff --json` depends on these key names, the closed verdict
// set, and the omitempty behavior of `line`. If this test fails because a key
// was renamed, a field removed, or the verdict vocabulary changed, that is a
// BREAKING change: it needs a CHANGELOG "Breaking" entry and a matching update
// to docs/diff-smell.md. Adding a new smell TYPE is additive and will not fail
// this test — consumers key on `severity` and `summary.verdict`, both closed
// sets. Do not "fix" this test by pasting in new output without deciding which
// of those two cases you are in.
const goldenContractJSON = `{
  "files": {
    "test": [
      "pkg/auth_test.go"
    ],
    "impl": [
      "pkg/auth.go"
    ]
  },
  "smells": [
    {
      "type": "test_skipped",
      "severity": "hard",
      "file": "pkg/auth_test.go",
      "line": 12,
      "evidence": "t.Skip(\"flaky\")"
    },
    {
      "type": "weakened_assertion",
      "severity": "soft",
      "file": "pkg/auth_test.go",
      "evidence": "test replaced assertion(s) one-for-one; verify they were not weakened"
    },
    {
      "type": "suppression",
      "severity": "soft",
      "file": "pkg/auth.go",
      "line": 21,
      "evidence": "//nolint:errcheck"
    }
  ],
  "summary": {
    "test_files": 1,
    "impl_files": 1,
    "hard": 1,
    "soft": 2,
    "by_type": {
      "suppression": 1,
      "test_skipped": 1,
      "weakened_assertion": 1
    },
    "verdict": "hard"
  }
}`

func TestSmellResult_GoldenJSON(t *testing.T) {
	b, err := json.MarshalIndent(AnalyzeDiff(contractDiff), "", "  ")
	require.NoError(t, err)
	require.Equal(t, goldenContractJSON, string(b),
		"the diff-smell JSON shape is a PUBLIC CONTRACT consumed by `atcr verify diff --json`. "+
			"A renamed key, a removed field, or a changed verdict vocabulary is BREAKING: it needs a "+
			"CHANGELOG \"Breaking\" entry and a docs/diff-smell.md update. Adding a new smell type is "+
			"additive and should not have reached this assertion. The docs half is enforced "+
			"mechanically by TestDiffSmellDocExampleMatchesAnalyzer (cli), which runs this analyzer "+
			"over the doc's own ```diff fence and compares it to the doc's own ```json fence.")
}

// The verdict vocabulary is closed. A consumer branches on exactly these three.
func TestSmellResult_VerdictVocabularyIsClosed(t *testing.T) {
	require.Equal(t, "clean", VerdictClean)
	require.Equal(t, "soft_only", VerdictSoftOnly)
	require.Equal(t, "hard", VerdictHard)
	require.Equal(t, "hard", SeverityHard)
	require.Equal(t, "soft", SeveritySoft)

	for _, tc := range []struct{ diff, want string }{
		{contractDiff, VerdictHard},
		{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,2 @@\n func A() {\n+\t//nolint:all\n", VerdictSoftOnly},
		{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-func A() int { return 0 }\n+func A() int { return 1 }\n", VerdictClean},
		{"", VerdictClean},
	} {
		require.Equal(t, tc.want, AnalyzeDiff(tc.diff).Summary.Verdict)
	}
}

// AC7: an added-line smell reports the real new-file line, including in a
// SECOND hunk — the case a naive "count lines from the top of the file"
// implementation gets wrong.
func TestAnalyzeDiff_LineOnAddedLineSmellAcrossHunks(t *testing.T) {
	const twoHunks = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -5,1 +5,2 @@
 first()
+	//nolint:first
@@ -80,3 +100,4 @@
 ctx1()
 ctx2()
+	//nolint:third
 ctx3()
`
	res := AnalyzeDiff(twoHunks)
	require.Len(t, res.Smells, 2)

	// Hunk 1 starts at new line 5: context occupies 5, the addition is 6.
	require.Equal(t, smellSuppression, res.Smells[0].Type)
	require.Equal(t, 6, res.Smells[0].Line)

	// Hunk 2 starts at new line 100 — deliberately different from its old-side
	// start (80), so reading the wrong capture group shows up here.
	require.Equal(t, smellSuppression, res.Smells[1].Type)
	require.Equal(t, 102, res.Smells[1].Line)
}

// A removed line consumes no NEW-file position; only added and context lines do.
func TestAnalyzeDiff_LineSkipsRemovedLines(t *testing.T) {
	const d = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,2 @@
 keep()
-drop1()
-drop2()
+	//nolint:after-removals
`
	res := AnalyzeDiff(d)
	require.Len(t, res.Smells, 1)
	// context at 1, then the addition at 2 — the two removals do not advance it.
	require.Equal(t, 2, res.Smells[0].Line)
}

// AC7: count-derived and file-level smells carry no line, and omitempty drops
// the key entirely rather than emitting a misleading 0.
func TestAnalyzeDiff_LineOmittedForFileLevelSmells(t *testing.T) {
	const testOnly = `diff --git a/a_test.go b/a_test.go
--- a/a_test.go
+++ b/a_test.go
@@ -1,1 +1,1 @@
-	require.Equal(t, 1, A())
+	require.NotNil(t, A())
`
	res := AnalyzeDiff(testOnly)
	require.NotEmpty(t, res.Smells)
	for _, s := range res.Smells {
		require.Zero(t, s.Line, "%s is not an added-line smell and must carry no line", s.Type)
	}

	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"line"`,
		"omitempty must drop the key for smells with no line, not emit 0")
}

// A malformed hunk header yields NO line rather than numbering this hunk's
// additions from the previous hunk's position — a confidently wrong file:line
// is worse than an absent one.
func TestAnalyzeDiff_MalformedHunkHeaderYieldsNoLine(t *testing.T) {
	const d = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
 ok()
+	//nolint:real
@@ garbage @@
+	//nolint:orphan
`
	res := AnalyzeDiff(d)
	require.NotEmpty(t, res.Smells)
	require.Equal(t, 2, res.Smells[0].Line)
	// The orphan line is outside any parseable hunk, so it is not diff content at
	// all and raises nothing — the header switch never re-entered a hunk.
	require.Len(t, res.Smells, 1)
}

// A hunk header whose new-side start digits overflow an int yields NO line
// rather than numbering this hunk's additions from the previous hunk's
// position — a confidently wrong file:line is worse than an absent one. The
// overflow is the ONLY way the start parse can fail once the strict
// hunk-counts shape matched, so this is the case that pins the `newLine = 0`
// reset: without it the second smell below would carry the first hunk's
// carried-over position instead of zero.
func TestAnalyzeDiff_OverflowHunkStartYieldsNoLine(t *testing.T) {
	const d = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
 ok()
+	//nolint:first
@@ -1,1 +99999999999999999999,2 @@
+	//nolint:overflow
 ctx()
`
	res := AnalyzeDiff(d)
	require.Len(t, res.Smells, 2)
	require.Equal(t, 2, res.Smells[0].Line)
	require.Zero(t, res.Smells[1].Line,
		"an overflowing +start must yield no line, not a carried-over one")

	b, err := json.Marshal(res.Smells[1])
	require.NoError(t, err)
	require.NotContains(t, string(b), `"line"`,
		"omitempty must drop the key for a smell with no line, not emit 0")
}

// Every collection is materialized, so the JSON form never carries null and a
// consumer indexing a field need not nil-check one that is merely empty.
func TestSmellResult_EmptyCollectionsAreNotNull(t *testing.T) {
	res := AnalyzeDiff("")
	require.NotNil(t, res)
	require.NotNil(t, res.Files.Test)
	require.NotNil(t, res.Files.Impl)
	require.NotNil(t, res.Smells)
	require.NotNil(t, res.Summary.ByType)

	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(b), "null")
	require.JSONEq(t,
		`{"files":{"test":[],"impl":[]},"smells":[],"summary":{"test_files":0,"impl_files":0,"hard":0,"soft":0,"by_type":{},"verdict":"clean"}}`,
		string(b))
}

func TestLooksLikeUnifiedDiff_Exported(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"git header", contractDiff, true},
		{"hunk header only", "@@ -1,1 +1,1 @@\n-a\n+b", true},
		{"old/new header pair", "--- a/x\n+++ b/x\n", true},
		{"prose with a leading plus", "+ add a nil check to Foo", false},
		{"empty", "", false},
		{"whitespace", "   \n\t\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, LooksLikeUnifiedDiff(tc.in))
		})
	}
}

// TestAnalyzeDiff_EmptyCatchReportsTheCatchLineOnce pins the two defects the
// consecutive-line join introduced once Line became a reported field.
//
// The join exists because formatters split `catch (e) {` and `}` across two added
// lines, and Go's \s matches the newline — so the pair reveals what a per-line
// scan cannot. But it reported a.line, the FIRST line of the pair, even when the
// match came entirely from the second: measured on the fixture below, two smells
// were emitted, one at a.js:10 with evidence "doSomething();" (a line containing
// no catch at all) and a duplicate at a.js:11.
//
// The evidence mismatch predates the Line field, but reporting Line turned it into
// a confident, WRONG file:line on a documented public contract that downstream
// gates are told to paste into technical-debt rows.
func TestAnalyzeDiff_EmptyCatchReportsTheCatchLineOnce(t *testing.T) {
	diff := "diff --git a/a.js b/a.js\n--- a/a.js\n+++ b/a.js\n@@ -10,0 +10,2 @@\n" +
		"+  doSomething();\n" +
		"+  try { risky(); } catch (e) {}\n"

	var catches []Smell
	for _, s := range AnalyzeDiff(diff).Smells {
		if s.Type == smellEmptyCatch {
			catches = append(catches, s)
		}
	}
	require.Len(t, catches, 1, "exactly one empty_catch, not one per line of the joined pair: %+v", catches)
	require.Equal(t, 11, catches[0].Line, "the line reported must be the one that contains `catch`")
	require.Contains(t, catches[0].Evidence, "catch", "the evidence must be the offending line, not its predecessor")
}

// TestAnalyzeDiff_EmptyCatchJoinRequiresAdjacency pins that the pair join only
// applies to genuinely consecutive lines. fc.added spans HUNKS, so without an
// adjacency check the join can splice lines hundreds apart: a `} catch (e) {` at
// new line 2 joined with a `}` from a hunk starting at new line 500 matched the
// regex and produced a smell for code that never existed.
func TestAnalyzeDiff_EmptyCatchJoinRequiresAdjacency(t *testing.T) {
	diff := "diff --git a/b.js b/b.js\n--- a/b.js\n+++ b/b.js\n@@ -1,0 +2,1 @@\n" +
		"+  } catch (e) {\n" +
		"@@ -500,0 +500,1 @@\n" +
		"+  }\n"

	for _, s := range AnalyzeDiff(diff).Smells {
		require.NotEqual(t, smellEmptyCatch, s.Type,
			"lines %d apart must not be joined into an empty-catch pair", 498)
	}
}

// TestAnalyzeDiff_EmptyCatchSplitAcrossAdjacentLines is the negative control: the
// formatter-produced split shape — the reason the join exists — must still fire,
// and must report the line carrying `catch`.
func TestAnalyzeDiff_EmptyCatchSplitAcrossAdjacentLines(t *testing.T) {
	diff := "diff --git a/c.js b/c.js\n--- a/c.js\n+++ b/c.js\n@@ -10,0 +10,2 @@\n" +
		"+  } catch (e) {\n" +
		"+  }\n"

	var catches []Smell
	for _, s := range AnalyzeDiff(diff).Smells {
		if s.Type == smellEmptyCatch {
			catches = append(catches, s)
		}
	}
	require.Len(t, catches, 1, "the split empty catch must still be detected exactly once")
	require.Equal(t, 10, catches[0].Line, "the `catch` keyword is on the first line of this pair")
	require.Contains(t, catches[0].Evidence, "catch")
}
