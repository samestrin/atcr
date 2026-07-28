package verify

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ported analyzer must reproduce llm-tools' diff-smell verdicts exactly.
// Fixtures below are unified diffs shaped like the ones an executor emits for a
// single finding (one or two file sections).

const dsImplOnly = `diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,3 +10,3 @@
 func pick() int {
-	return 0
+	return 1
 }
`

const dsTestOnly = `diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go
--- a/internal/verify/select_test.go
+++ b/internal/verify/select_test.go
@@ -10,4 +10,3 @@
 func TestPick(t *testing.T) {
-	if pick() != 1 {
-		t.Fatalf("bad")
-	}
+	_ = pick()
 }
`

// dsTestOnlyClean touches ONLY a test file but loses no assertions, so it trips
// test_only and nothing else — the fixture needed to exercise the test_only
// suppression in isolation (dsTestOnly also trips weakened_assertion).
const dsTestOnlyClean = `diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go
--- a/internal/verify/select_test.go
+++ b/internal/verify/select_test.go
@@ -10,3 +10,4 @@
 func TestPick(t *testing.T) {
 	require.Equal(t, 1, pick())
+	require.NotZero(t, pick())
 }
`

const dsWeakenedAssertion = `diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go
--- a/internal/verify/select_test.go
+++ b/internal/verify/select_test.go
@@ -10,4 +10,4 @@
 func TestPick(t *testing.T) {
-	require.Equal(t, 1, pick())
-	require.NoError(t, err)
+	_ = pick()
 }
diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,3 +10,3 @@
 func pick() int {
-	return 0
+	return 1
 }
`

const dsSuppression = `diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,2 +10,3 @@
 func pick() int {
+	//nolint:gosec // trust me
 	return 1
 }
`

const dsStubBody = `diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,2 +10,3 @@
 func pick() int {
+	// TODO: implement properly
 	return 1
 }
`

// dsCryptoPanic is idiomatic Go error handling, not a stub: the panic argument
// is a real message, not a not-implemented literal.
const dsCryptoPanic = `diff --git a/internal/verify/executor.go b/internal/verify/executor.go
--- a/internal/verify/executor.go
+++ b/internal/verify/executor.go
@@ -10,2 +10,3 @@
 func pick() int {
+	panic("crypto/rand: " + err.Error())
 	return 1
 }
`

// dsTrailingTODO defers follow-up work in a trailing comment on a real
// statement — only a TODO/FIXME that IS the whole statement is a stub.
const dsTrailingTODO = `diff --git a/internal/verify/executor.go b/internal/verify/executor.go
--- a/internal/verify/executor.go
+++ b/internal/verify/executor.go
@@ -10,2 +10,3 @@
 func pick() int {
+	panic(err) // TODO: return instead
 	return 1
 }
`

const dsNotImplPanic = `diff --git a/internal/verify/executor.go b/internal/verify/executor.go
--- a/internal/verify/executor.go
+++ b/internal/verify/executor.go
@@ -10,2 +10,3 @@
 func pick() int {
+	panic("not implemented")
 	return 1
 }
`

const dsEmptyCatch = `diff --git a/src/app.js b/src/app.js
--- a/src/app.js
+++ b/src/app.js
@@ -10,2 +10,3 @@
 function pick() {
+  try { risky(); } catch (e) { }
   return 1;
 }
`

// dsEmptyCatchMultiline is the standard formatter-produced shape: the empty
// catch body spans two added lines, so a strictly per-line regex can never see
// the `{ ... }` pair.
const dsEmptyCatchMultiline = `diff --git a/src/app.js b/src/app.js
--- a/src/app.js
+++ b/src/app.js
@@ -10,2 +10,4 @@
 function pick() {
+  try { risky(); } catch (e) {
+  }
   return 1;
 }
`

func TestAnalyzeDiff_Verdicts(t *testing.T) {
	cases := []struct {
		name    string
		diff    string
		verdict string
		types   []string
	}{
		{"impl only is clean", dsImplOnly, smellVerdictClean, nil},
		{"test only is hard", dsTestOnlyClean, smellVerdictHard, []string{smellTestOnly}},
		// dsTestOnly both touches only tests AND drops an assertion, so it trips both
		// HARD detectors at once.
		{"test only plus assertion loss trips both", dsTestOnly, smellVerdictHard,
			[]string{smellTestOnly, smellWeakenedAssertion}},
		{"weakened assertion is hard", dsWeakenedAssertion, smellVerdictHard, []string{smellWeakenedAssertion}},
		{"suppression is soft", dsSuppression, smellVerdictSoftOnly, []string{smellSuppression}},
		{"stub body is soft", dsStubBody, smellVerdictSoftOnly, []string{smellStubBody}},
		{"crypto panic is clean", dsCryptoPanic, smellVerdictClean, nil},
		{"trailing TODO comment is clean", dsTrailingTODO, smellVerdictClean, nil},
		{"not-implemented panic is soft", dsNotImplPanic, smellVerdictSoftOnly, []string{smellStubBody}},
		{"empty catch is soft", dsEmptyCatch, smellVerdictSoftOnly, []string{smellEmptyCatch}},
		{"multi-line empty catch is soft", dsEmptyCatchMultiline, smellVerdictSoftOnly, []string{smellEmptyCatch}},
		{"empty diff is clean", "", smellVerdictClean, nil},
		{"prose is clean", "just change the return value to 1", smellVerdictClean, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := analyzeDiff(tc.diff)
			require.NotNil(t, res)
			assert.Equal(t, tc.verdict, res.Summary.Verdict)
			for _, want := range tc.types {
				assert.Contains(t, res.Summary.ByType, want, "expected smell type %q in %v", want, res.Summary.ByType)
			}
		})
	}
}

// weakened_assertion must fire even though the impl file in the same diff keeps
// test_only from firing — the two detectors are independent.
func TestAnalyzeDiff_WeakenedAssertionIndependentOfTestOnly(t *testing.T) {
	res := analyzeDiff(dsWeakenedAssertion)
	assert.NotContains(t, res.Summary.ByType, smellTestOnly, "impl file present, test_only must not fire")
	assert.Equal(t, 1, res.Summary.ByType[smellWeakenedAssertion])
	assert.Equal(t, smellVerdictHard, res.Summary.Verdict)
}

// isTestPath must be precise: "contest.go" and "latest_results.go" are NOT tests.
func TestIsSmellTestPath(t *testing.T) {
	for _, p := range []string{
		"internal/verify/select_test.go",
		"./internal/verify/select_test.go",
		"src/app.test.ts",
		"src/app.spec.tsx",
		"pkg/tests/helper.go",
		"api/test_thing.py",
		"api/thing_test.py",
		"spec/models/user_spec.rb",
		"src/main/UserTest.java",
		"src/UserTests.cs",
		// Mainstream layouts: .NET test projects, JVM Test dirs, Go golden files.
		"MyProj.Tests/Foo.cs",
		"src/Test/Foo.java",
		"internal/verify/testdata/x.json",
	} {
		assert.True(t, isSmellTestPath(p), "expected test path: %s", p)
	}
	for _, p := range []string{
		"internal/verify/contest.go",
		"internal/verify/latest_results.go",
		"src/protest.ts",
		"internal/verify/select.go",
		// A bare `spec` path segment is a documentation/spec directory, not a
		// test convention — the _spec.rb / .spec.ts filename regexes carry that
		// load.
		"api/spec/openapi.yaml",
		"docs/spec/README.md",
	} {
		assert.False(t, isSmellTestPath(p), "expected NON-test path: %s", p)
	}
}

// File attribution and hunk line numbers must survive parsing so a SOFT smell can
// point at a real location.
func TestAnalyzeDiff_FileAndLineAttribution(t *testing.T) {
	res := analyzeDiff(dsSuppression)
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "internal/verify/select.go", res.Smells[0].File)
	assert.Equal(t, 11, res.Smells[0].Line)
	assert.Equal(t, []string{"internal/verify/select.go"}, res.Files.Impl)
	assert.Empty(t, res.Files.Test)
}

// A pathological hunk-start must not wrap into a garbage (or negative) line
// number — the diff is model-generated, so an absurd header is reachable input.
func TestAnalyzeDiff_HunkStartOverflowIsFailSoft(t *testing.T) {
	res := analyzeDiff("diff --git a/x.go b/x.go\n@@ -1 +99999999999999999999999 @@\n+\t// TODO: later\n")
	require.Len(t, res.Smells, 1)
	assert.Equal(t, 0, res.Smells[0].Line, "overflowing hunk start must degrade to unknown line, not wrap")

	// The largest in-range header still parses exactly.
	res = analyzeDiff("diff --git a/x.go b/x.go\n@@ -1 +999999999 @@\n+\t// TODO: later\n")
	require.Len(t, res.Smells, 1)
	assert.Equal(t, 999999999, res.Smells[0].Line)
}

// Deleting a whole test file is the archetypal reward hack: it must be HARD,
// and it must be HARD *because* test_deleted fired — a verdict assertion alone
// would also pass if the deletion detector were removed entirely (an ordinary
// modification of the same file scores hard + test_only too).
func TestAnalyzeDiff_TestFileDeletionIsHard(t *testing.T) {
	res := analyzeDiff("diff --git a/x_test.go b/x_test.go\ndeleted file mode 100644\n--- a/x_test.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-\trequire.Equal(t, 1, 2)\n")
	assert.Equal(t, smellVerdictHard, res.Summary.Verdict)
	assert.Contains(t, res.Summary.ByType, smellTestDeleted)

	// Negative control: MODIFYING the same test file must not produce
	// test_deleted — only an outright deletion may.
	res = analyzeDiff(dsTestOnly)
	assert.NotContains(t, res.Summary.ByType, smellTestDeleted, "a modified test file is not a deletion")
}

// A CRLF diff (or one wrapped in a markdown fence) must still be recognized and
// parsed — an executor is free to emit either.
func TestAnalyzeDiff_CRLFAndFencedDiffs(t *testing.T) {
	crlf := "diff --git a/x.go b/x.go\r\n--- a/x.go\r\n+++ b/x.go\r\n@@ -1 +1,2 @@\r\n+\t// TODO: x\r\n"
	assert.True(t, looksLikeUnifiedDiff(crlf))
	assert.Equal(t, smellVerdictSoftOnly, analyzeDiff(crlf).Summary.Verdict)

	fenced := "```diff\n" + dsTestOnly + "```\n"
	assert.True(t, looksLikeUnifiedDiff(fenced))
	assert.Equal(t, smellVerdictHard, analyzeDiff(fenced).Summary.Verdict)
}

// A rename-style header with no +++ line still attributes to the b/ path.
func TestAnalyzeDiff_GitHeaderFallbackPath(t *testing.T) {
	res := analyzeDiff("diff --git a/foo/bar.go b/foo/bar.go\n@@ -1,1 +1,2 @@\n+\t// TODO: later\n")
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "foo/bar.go", res.Smells[0].File)
}

// A path containing a space must survive the `diff --git` header parse: the b/
// side is located by its separator, not by whitespace splitting, so the header
// and the +++ line agree on ONE file entry instead of two.
func TestAnalyzeDiff_GitHeaderSpacePath(t *testing.T) {
	res := analyzeDiff("diff --git a/my test_helper.go b/my test_helper.go\n--- a/my test_helper.go\n+++ b/my test_helper.go\n@@ -1,1 +1,2 @@\n+\t// TODO: later\n")
	assert.Len(t, res.Files.Impl, 1, "space path must yield exactly one file entry, got %v", res.Files.Impl)
	assert.Equal(t, "my test_helper.go", res.Files.Impl[0])
}

// looksLikeUnifiedDiff is the gate's pre-filter: free-form fix prose must not be
// mistaken for a diff (clarification Q1 — non-diff fixes pass through as clean).
func TestLooksLikeUnifiedDiff(t *testing.T) {
	for _, s := range []string{dsImplOnly, dsTestOnly, dsEmptyCatch,
		"--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n"} {
		assert.True(t, looksLikeUnifiedDiff(s), "expected diff: %.40s", s)
	}
	for _, s := range []string{
		"",
		"   ",
		"Change the return value from 0 to 1.",
		"```go\nfunc pick() int { return 1 }\n```",
		"func pick() int {\n\treturn 1\n}",
		"+ add a nil check before the deref", // prose starting with '+', no headers
	} {
		assert.False(t, looksLikeUnifiedDiff(s), "expected NON-diff: %q", s)
	}
}

// The evidence string the gate feeds back to the executor must name every smell
// type and stay single-line-safe for prompt interpolation.
func TestSmellFeedback(t *testing.T) {
	res := analyzeDiff(dsWeakenedAssertion)
	fb := smellFeedback(res)
	assert.Contains(t, fb, smellWeakenedAssertion)
	assert.NotContains(t, fb, "\n", "feedback must be flattened for prompt interpolation")

	assert.Equal(t, "", smellFeedback(analyzeDiff(dsImplOnly)), "clean verdict yields no feedback")
	assert.Equal(t, "", smellFeedback(nil))
}

// The feedback is interpolated into the retry prompt and its evidence is verbatim
// model output, so both the per-item length and the item count must be bounded —
// otherwise a crafted fix inflates every retry's token cost.
func TestSmellFeedback_BoundsEvidenceAndItemCount(t *testing.T) {
	long := strings.Repeat("x", maxSmellEvidenceRunes*3)
	fb := smellFeedback(analyzeDiff("diff --git a/x.go b/x.go\n@@ -1 +1,2 @@\n+//nolint " + long + "\n"))
	assert.Contains(t, fb, smellSuppression, "the actionable type name is never truncated")
	assert.Less(t, len(fb), maxSmellEvidenceRunes*2, "evidence must be truncated")
	assert.Contains(t, fb, "...")

	var many strings.Builder
	many.WriteString("diff --git a/x.go b/x.go\n@@ -1 +1,99 @@\n")
	for i := 0; i < maxSmellFeedbackItems*3; i++ {
		many.WriteString("+\t// TODO: item\n")
	}
	fb = smellFeedback(analyzeDiff(many.String()))
	assert.Equal(t, maxSmellFeedbackItems, strings.Count(fb, smellStubBody), "listed items must be capped")
	want := fmt.Sprintf("(+%d more)", maxSmellFeedbackItems*3-maxSmellFeedbackItems)
	assert.Contains(t, fb, want, "the dropped remainder must be reported, never silently truncated")
}

// A line that is merely relocated or reindented within the same file is not a
// NEW shortcut: its text appears verbatim among the file's removed lines, so the
// per-line SOFT fingerprints must not fire on it (TD: diffsmell.go:120 — frequent
// false positives on moves erode trust in the NEEDS_REVIEW marker).
func TestAnalyzeDiff_RelocatedLineIsClean(t *testing.T) {
	relocated := `diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,3 +10,2 @@
 func a() {
-	//nolint:gosec // trust me
 	return 1
 }
@@ -20,2 +20,3 @@
 func b() {
+	//nolint:gosec // trust me
 	return 2
 }
`
	res := analyzeDiff(relocated)
	assert.Equal(t, smellVerdictClean, res.Summary.Verdict)
	assert.Empty(t, res.Smells)

	reindented := `diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,3 +10,3 @@
 func pick() int {
-	// TODO: implement properly
+		// TODO: implement properly
 	return 1
 }
`
	res = analyzeDiff(reindented)
	assert.Equal(t, smellVerdictClean, res.Summary.Verdict)
	assert.Empty(t, res.Smells)
}

// Relocation suppression must not hide a genuinely NEW shortcut added alongside
// the moved line: only the line with no removed-lines twin is reported.
func TestAnalyzeDiff_RelocationDoesNotHideNewSmell(t *testing.T) {
	res := analyzeDiff(`diff --git a/internal/verify/select.go b/internal/verify/select.go
--- a/internal/verify/select.go
+++ b/internal/verify/select.go
@@ -10,3 +10,4 @@
 func pick() int {
-	//nolint:gosec // trust me
+	//nolint:gosec // trust me
+	//nolint:errcheck // new one
 	return 1
 }
`)
	assert.Equal(t, smellVerdictSoftOnly, res.Summary.Verdict)
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "//nolint:errcheck // new one", res.Smells[0].Evidence)
}

// smellTypes returns the deterministic, sorted, deduplicated type list used for
// the FixReview annotation.
func TestSmellTypes(t *testing.T) {
	res := analyzeDiff(dsSuppression + strings.ReplaceAll(dsStubBody, "select.go", "other.go"))
	assert.Equal(t, []string{smellStubBody, smellSuppression}, smellTypes(res))
	assert.Empty(t, smellTypes(analyzeDiff(dsImplOnly)))
	assert.Empty(t, smellTypes(nil))
}
