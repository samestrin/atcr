package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		{"impl only is clean", dsImplOnly, VerdictClean, nil},
		{"test only is hard", dsTestOnlyClean, VerdictHard, []string{smellTestOnly}},
		// dsTestOnly both touches only tests AND drops an assertion, so it trips both
		// HARD detectors at once.
		{"test only plus assertion loss trips both", dsTestOnly, VerdictHard,
			[]string{smellTestOnly, smellWeakenedAssertion}},
		{"weakened assertion is hard", dsWeakenedAssertion, VerdictHard, []string{smellWeakenedAssertion}},
		{"suppression is soft", dsSuppression, VerdictSoftOnly, []string{smellSuppression}},
		{"stub body is soft", dsStubBody, VerdictSoftOnly, []string{smellStubBody}},
		{"crypto panic is clean", dsCryptoPanic, VerdictClean, nil},
		{"trailing TODO comment is clean", dsTrailingTODO, VerdictClean, nil},
		{"not-implemented panic is soft", dsNotImplPanic, VerdictSoftOnly, []string{smellStubBody}},
		{"empty catch is soft", dsEmptyCatch, VerdictSoftOnly, []string{smellEmptyCatch}},
		{"multi-line empty catch is soft", dsEmptyCatchMultiline, VerdictSoftOnly, []string{smellEmptyCatch}},
		{"empty diff is clean", "", VerdictClean, nil},
		{"prose is clean", "just change the return value to 1", VerdictClean, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := AnalyzeDiff(tc.diff)
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
	res := AnalyzeDiff(dsWeakenedAssertion)
	assert.NotContains(t, res.Summary.ByType, smellTestOnly, "impl file present, test_only must not fire")
	assert.Equal(t, 1, res.Summary.ByType[smellWeakenedAssertion])
	assert.Equal(t, VerdictHard, res.Summary.Verdict)
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

// File attribution must survive parsing so a SOFT smell points at the right
// file. (Line numbers are deliberately NOT tracked: no production consumer ever
// read them — the quoted evidence line is sufficient to relocate a smell.)
func TestAnalyzeDiff_FileAttribution(t *testing.T) {
	res := AnalyzeDiff(dsSuppression)
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "internal/verify/select.go", res.Smells[0].File)
	assert.Equal(t, []string{"internal/verify/select.go"}, res.Files.Impl)
	assert.Empty(t, res.Files.Test)
}

// Deleting a whole test file is the archetypal reward hack: it must be HARD,
// and it must be HARD *because* test_deleted fired — a verdict assertion alone
// would also pass if the deletion detector were removed entirely (an ordinary
// modification of the same file scores hard + test_only too).
func TestAnalyzeDiff_TestFileDeletionIsHard(t *testing.T) {
	res := AnalyzeDiff("diff --git a/x_test.go b/x_test.go\ndeleted file mode 100644\n--- a/x_test.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-\trequire.Equal(t, 1, 2)\n")
	assert.Equal(t, VerdictHard, res.Summary.Verdict)
	assert.Contains(t, res.Summary.ByType, smellTestDeleted)

	// Negative control: MODIFYING the same test file must not produce
	// test_deleted — only an outright deletion may.
	res = AnalyzeDiff(dsTestOnly)
	assert.NotContains(t, res.Summary.ByType, smellTestDeleted, "a modified test file is not a deletion")
}

// A CRLF diff (or one wrapped in a markdown fence) must still be recognized and
// parsed — an executor is free to emit either.
func TestAnalyzeDiff_CRLFAndFencedDiffs(t *testing.T) {
	crlf := "diff --git a/x.go b/x.go\r\n--- a/x.go\r\n+++ b/x.go\r\n@@ -1 +1,2 @@\r\n+\t// TODO: x\r\n"
	assert.True(t, LooksLikeUnifiedDiff(crlf))
	assert.Equal(t, VerdictSoftOnly, AnalyzeDiff(crlf).Summary.Verdict)

	fenced := "```diff\n" + dsTestOnly + "```\n"
	assert.True(t, LooksLikeUnifiedDiff(fenced))
	assert.Equal(t, VerdictHard, AnalyzeDiff(fenced).Summary.Verdict)
}

// A rename-style header with no +++ line still attributes to the b/ path.
func TestAnalyzeDiff_GitHeaderFallbackPath(t *testing.T) {
	res := AnalyzeDiff("diff --git a/foo/bar.go b/foo/bar.go\n@@ -1,1 +1,2 @@\n+\t// TODO: later\n")
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "foo/bar.go", res.Smells[0].File)
}

// A path containing a space must survive the `diff --git` header parse: the b/
// side is located by its separator, not by whitespace splitting, so the header
// and the +++ line agree on ONE file entry instead of two.
func TestAnalyzeDiff_GitHeaderSpacePath(t *testing.T) {
	res := AnalyzeDiff("diff --git a/my test_helper.go b/my test_helper.go\n--- a/my test_helper.go\n+++ b/my test_helper.go\n@@ -1,1 +1,2 @@\n+\t// TODO: later\n")
	assert.Len(t, res.Files.Impl, 1, "space path must yield exactly one file entry, got %v", res.Files.Impl)
	assert.Equal(t, "my test_helper.go", res.Files.Impl[0])
}

// LooksLikeUnifiedDiff is the gate's pre-filter: free-form fix prose must not be
// mistaken for a diff (clarification Q1 — non-diff fixes pass through as clean).
func TestLooksLikeUnifiedDiff(t *testing.T) {
	for _, s := range []string{dsImplOnly, dsTestOnly, dsEmptyCatch,
		"--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n"} {
		assert.True(t, LooksLikeUnifiedDiff(s), "expected diff: %.40s", s)
	}
	for _, s := range []string{
		"",
		"   ",
		"Change the return value from 0 to 1.",
		"```go\nfunc pick() int { return 1 }\n```",
		"func pick() int {\n\treturn 1\n}",
		"+ add a nil check before the deref", // prose starting with '+', no headers
	} {
		assert.False(t, LooksLikeUnifiedDiff(s), "expected NON-diff: %q", s)
	}
}

// The evidence string the gate feeds back to the executor must name every smell
// type and stay single-line-safe for prompt interpolation.
func TestSmellFeedback(t *testing.T) {
	res := AnalyzeDiff(dsWeakenedAssertion)
	fb := smellFeedback(res)
	assert.Contains(t, fb, smellWeakenedAssertion)
	assert.NotContains(t, fb, "\n", "feedback must be flattened for prompt interpolation")

	assert.Equal(t, "", smellFeedback(AnalyzeDiff(dsImplOnly)), "clean verdict yields no feedback")
	assert.Equal(t, "", smellFeedback(nil))
}

// The feedback is interpolated into the retry prompt and its evidence is verbatim
// model output, so both the per-item length and the item count must be bounded —
// otherwise a crafted fix inflates every retry's token cost.
func TestSmellFeedback_BoundsEvidenceAndItemCount(t *testing.T) {
	long := strings.Repeat("x", maxSmellEvidenceRunes*3)
	fb := smellFeedback(AnalyzeDiff("diff --git a/x.go b/x.go\n@@ -1 +1,2 @@\n+//nolint " + long + "\n"))
	assert.Contains(t, fb, smellSuppression, "the actionable type name is never truncated")
	assert.Less(t, len(fb), maxSmellEvidenceRunes*2, "evidence must be truncated")
	assert.Contains(t, fb, "...")

	var many strings.Builder
	many.WriteString("diff --git a/x.go b/x.go\n@@ -1 +1,99 @@\n")
	for i := 0; i < maxSmellFeedbackItems*3; i++ {
		many.WriteString("+\t// TODO: item\n")
	}
	fb = smellFeedback(AnalyzeDiff(many.String()))
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
	res := AnalyzeDiff(relocated)
	assert.Equal(t, VerdictClean, res.Summary.Verdict)
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
	res = AnalyzeDiff(reindented)
	assert.Equal(t, VerdictClean, res.Summary.Verdict)
	assert.Empty(t, res.Smells)
}

// Relocation suppression must not hide a genuinely NEW shortcut added alongside
// the moved line: only the line with no removed-lines twin is reported.
func TestAnalyzeDiff_RelocationDoesNotHideNewSmell(t *testing.T) {
	res := AnalyzeDiff(`diff --git a/internal/verify/select.go b/internal/verify/select.go
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
	assert.Equal(t, VerdictSoftOnly, res.Summary.Verdict)
	require.Len(t, res.Smells, 1)
	assert.Equal(t, "//nolint:errcheck // new one", res.Smells[0].Evidence)
}

// smellTypes returns the deterministic, sorted, deduplicated type list used for
// the FixReview annotation.
func TestSmellTypes(t *testing.T) {
	res := AnalyzeDiff(dsSuppression + strings.ReplaceAll(dsStubBody, "select.go", "other.go"))
	assert.Equal(t, []string{smellStubBody, smellSuppression}, smellTypes(res))
	assert.Empty(t, smellTypes(AnalyzeDiff(dsImplOnly)))
	assert.Empty(t, smellTypes(nil))
}

// Skipping a test is the most direct way to make a failing test pass, and it is
// unambiguous — nothing legitimate about a fix requires disabling the test that
// proves it. It must be HARD, and it must fire even when the diff also carries a
// real implementation change (which keeps test_only from firing).
const dsTestSkipGo = `diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go
--- a/internal/verify/select_test.go
+++ b/internal/verify/select_test.go
@@ -10,3 +10,4 @@
 func TestPick(t *testing.T) {
+	t.Skip("unrelated flake")
 	require.Equal(t, 1, pick())
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

const dsTestSkipPy = `diff --git a/api/test_thing.py b/api/test_thing.py
--- a/api/test_thing.py
+++ b/api/test_thing.py
@@ -1,3 +1,4 @@
+@pytest.mark.skip(reason="broken")
 def test_thing():
     assert thing() == 1
diff --git a/api/thing.py b/api/thing.py
--- a/api/thing.py
+++ b/api/thing.py
@@ -1,2 +1,2 @@
 def thing():
-    return 0
+    return 1
`

func TestAnalyzeDiff_TestSkipIsHard(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
	}{
		{"go t.Skip", dsTestSkipGo},
		{"pytest mark skip", dsTestSkipPy},
		{"go t.Skipf", strings.Replace(dsTestSkipGo, `t.Skip("unrelated flake")`, `t.Skipf("flaky on %s", runtime.GOOS)`, 1)},
		{"jest it.skip", "diff --git a/src/app.test.ts b/src/app.test.ts\n--- a/src/app.test.ts\n+++ b/src/app.test.ts\n@@ -1 +1,2 @@\n+it.skip('works', () => {})\n" + dsImplOnly},
		{"jest describe.skip", "diff --git a/src/app.test.ts b/src/app.test.ts\n--- a/src/app.test.ts\n+++ b/src/app.test.ts\n@@ -1 +1,2 @@\n+describe.skip('suite', () => {})\n" + dsImplOnly},
		{"jasmine xit", "diff --git a/src/app.spec.js b/src/app.spec.js\n--- a/src/app.spec.js\n+++ b/src/app.spec.js\n@@ -1 +1,2 @@\n+xit('works', () => {})\n" + dsImplOnly},
		{"jasmine xdescribe", "diff --git a/src/app.spec.js b/src/app.spec.js\n--- a/src/app.spec.js\n+++ b/src/app.spec.js\n@@ -1 +1,2 @@\n+xdescribe('suite', () => {})\n" + dsImplOnly},
		{"junit4 Ignore", "diff --git a/src/main/UserTest.java b/src/main/UserTest.java\n--- a/src/main/UserTest.java\n+++ b/src/main/UserTest.java\n@@ -1 +1,2 @@\n+    @Ignore(\"broken\")\n" + dsImplOnly},
		{"junit5 Disabled", "diff --git a/src/main/UserTest.java b/src/main/UserTest.java\n--- a/src/main/UserTest.java\n+++ b/src/main/UserTest.java\n@@ -1 +1,2 @@\n+    @Disabled\n" + dsImplOnly},
		{"rust ignore", "diff --git a/tests/it.rs b/tests/it.rs\n--- a/tests/it.rs\n+++ b/tests/it.rs\n@@ -1 +1,2 @@\n+#[ignore]\n" + dsImplOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := AnalyzeDiff(tc.diff)
			assert.Contains(t, res.Summary.ByType, smellTestSkipped, "skip must be detected in %v", res.Summary.ByType)
			assert.Equal(t, VerdictHard, res.Summary.Verdict)
		})
	}
}

// The skip detector must not fire on non-test files, on REMOVED skip lines
// (re-enabling a test is the opposite of a reward hack), or on identifiers that
// merely contain a skip token.
func TestAnalyzeDiff_TestSkipNegativeControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
	}{
		{"skip token in impl file", "diff --git a/internal/verify/select.go b/internal/verify/select.go\n--- a/internal/verify/select.go\n+++ b/internal/verify/select.go\n@@ -1 +1,2 @@\n+\tt.Skip(\"n/a\")\n"},
		{"removing a skip re-enables the test", "diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n@@ -1,2 +1,1 @@\n-\tt.Skip(\"was flaky\")\n" + dsImplOnly},
		{"identifier containing xit", "diff --git a/src/app.spec.js b/src/app.spec.js\n--- a/src/app.spec.js\n+++ b/src/app.spec.js\n@@ -1 +1,2 @@\n+  prefixit('works', () => { expect(1).toBe(1) })\n" + dsImplOnly},
		{"identifier containing skip", "diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n@@ -1 +1,2 @@\n+\trequire.Equal(t, 1, unit.skipped())\n" + dsImplOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := AnalyzeDiff(tc.diff)
			assert.NotContains(t, res.Summary.ByType, smellTestSkipped, "must not flag: %v", res.Summary.ByType)
		})
	}
}

// Renaming a test file to a non-test path disables it as effectively as deleting
// it — and it is WORSE than a deletion for this analyzer, because the renamed
// file is recorded as an IMPL file, which additionally suppresses test_only for
// everything else in the same diff. Only the `diff --git` header carries the old
// path, so it must be parsed.
const dsTestRenamedAway = `diff --git a/internal/verify/select_test.go b/internal/verify/select_disabled.go
similarity index 100%
rename from internal/verify/select_test.go
rename to internal/verify/select_disabled.go
`

func TestAnalyzeDiff_TestRenamedAwayIsHard(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
	}{
		{"rename to non-test path", dsTestRenamedAway},
		{"rename by suffixing the extension", "diff --git a/x_test.go b/x_test.go.disabled\nsimilarity index 100%\nrename from x_test.go\nrename to x_test.go.disabled\n"},
		{"rename alongside a real impl change", dsTestRenamedAway + dsImplOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := AnalyzeDiff(tc.diff)
			assert.Contains(t, res.Summary.ByType, smellTestRenamedAway, "got %v", res.Summary.ByType)
			assert.Equal(t, VerdictHard, res.Summary.Verdict)
		})
	}
}

// Only a test -> NON-test rename disables anything. Reorganising tests, renaming
// implementation files, and PROMOTING a file into a test must all stay clean.
func TestAnalyzeDiff_RenameNegativeControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
	}{
		{"test to test", "diff --git a/x_test.go b/y_test.go\nsimilarity index 100%\nrename from x_test.go\nrename to y_test.go\n"},
		{"impl to impl", "diff --git a/x.go b/y.go\nsimilarity index 100%\nrename from x.go\nrename to y.go\n"},
		{"impl promoted to test", "diff --git a/x.go b/x_test.go\nsimilarity index 100%\nrename from x.go\nrename to x_test.go\n"},
		{"ordinary edit, no rename", dsImplOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := AnalyzeDiff(tc.diff)
			assert.NotContains(t, res.Summary.ByType, smellTestRenamedAway, "got %v", res.Summary.ByType)
		})
	}
}

// A `+++ `-prefixed line INSIDE a hunk body is added content whose text happens
// to start with `++ ` — not a new-file header. Reading it as a header creates a
// phantom IMPL file (which suppresses test_only, since that requires
// implCount == 0) and rebinds the parser, charging the following removed lines to
// the phantom. One stray added line must not defeat the two HARD detectors.
const dsPlusPlusContentLine = `diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go
--- a/internal/verify/select_test.go
+++ b/internal/verify/select_test.go
@@ -10,4 +10,2 @@
 func TestPick(t *testing.T) {
+++ this text is CONTENT, not a header
-	require.Equal(t, 1, pick())
-	require.NoError(t, err)
 }
`

func TestAnalyzeDiff_PlusPlusContentIsNotAHeader(t *testing.T) {
	res := AnalyzeDiff(dsPlusPlusContentLine)
	assert.Empty(t, res.Files.Impl, "a `+++ ` content line must not create a phantom impl file")
	assert.Equal(t, []string{"internal/verify/select_test.go"}, res.Files.Test)
	assert.Contains(t, res.Summary.ByType, smellTestOnly, "got %v", res.Summary.ByType)
	assert.Contains(t, res.Summary.ByType, smellWeakenedAssertion, "got %v", res.Summary.ByType)
	assert.Equal(t, VerdictHard, res.Summary.Verdict)
}

// A hunk header declaring an absurd line count must not let the body escape the
// scan. Failing CLOSED (an integer wrap to a negative count skips the body) would
// hand a crafted fix a one-line bypass of every detector.
func TestAnalyzeDiff_OverflowingHunkCountFailsOpen(t *testing.T) {
	huge := strings.Repeat("9", 25)
	res := AnalyzeDiff("diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n@@ -1," + huge +
		" +1,2 @@\n+\tt.Skip(\"gone\")\n-\trequire.Equal(t, 1, 2)\n")
	assert.Contains(t, res.Summary.ByType, smellTestSkipped, "got %v", res.Summary.ByType)
	assert.Equal(t, VerdictHard, res.Summary.Verdict)
}

// LooksLikeUnifiedDiff explicitly accepts a HEADERLESS diff (old-file/new-file
// header pairs with no `diff --git` lines), so AnalyzeDiff must handle one. In
// that shape `cur` still points at the PREVIOUS file when a `+++ /dev/null`
// deletion arrives, so binding the deletion only when cur == nil stamps it on the
// wrong file and the deleted test never enters `files` at all.
const dsHeaderlessDeletion = `--- a/internal/verify/impl.go
+++ b/internal/verify/impl.go
@@ -10,3 +10,3 @@
 func pick() int {
-	return 0
+	return 1
 }
--- a/internal/verify/select_test.go
+++ /dev/null
@@ -1,2 +0,0 @@
-	require.Equal(t, 1, pick())
-	require.NoError(t, err)
`

func TestAnalyzeDiff_HeaderlessDeletionBindsToOldPath(t *testing.T) {
	res := AnalyzeDiff(dsHeaderlessDeletion)
	assert.Equal(t, []string{"internal/verify/select_test.go"}, res.Files.Test,
		"the deleted test must be recorded, got test=%v impl=%v", res.Files.Test, res.Files.Impl)
	assert.Equal(t, []string{"internal/verify/impl.go"}, res.Files.Impl)
	assert.Contains(t, res.Summary.ByType, smellTestDeleted, "got %v", res.Summary.ByType)
	assert.Equal(t, VerdictHard, res.Summary.Verdict)

	// The same diff WITH `diff --git` headers already scored hard; the headerless
	// shape must not be the weaker path.
	withHeaders := AnalyzeDiff("diff --git a/internal/verify/impl.go b/internal/verify/impl.go\n" +
		strings.Replace(dsHeaderlessDeletion, "--- a/internal/verify/select_test.go",
			"diff --git a/internal/verify/select_test.go b/internal/verify/select_test.go\n--- a/internal/verify/select_test.go", 1))
	assert.Equal(t, res.Summary.Verdict, withHeaders.Summary.Verdict)
	assert.Contains(t, withHeaders.Summary.ByType, smellTestDeleted)
}

// A malformed `+++ ` header carrying only whitespace is NOT a deletion — only an
// explicit /dev/null is. Treating an empty path as a deletion produces the worst
// kind of false positive: a HARD test_deleted on a diff that deletes nothing, so
// a good fix is rejected, retried, then withheld with a FixWarning accusing the
// model of deleting a test file it never touched.
func TestAnalyzeDiff_WhitespaceOnlyPlusHeaderIsNotADeletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		tail string
	}{
		{"whitespace only", "+++    \n"},
		{"tab only", "+++ \t\n"},
		{"bare marker", "+++ \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The hunk's declared counts are exhausted by its two body lines, so the
			// malformed header lands OUTSIDE the hunk and reaches the header branch —
			// where the empty-path-means-deleted test lives.
			res := AnalyzeDiff("diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n" +
				"@@ -1,1 +1,1 @@\n-\trequire.Equal(t, 1, 2)\n+\trequire.Equal(t, 1, 3)\n" + tc.tail)
			assert.NotContains(t, res.Summary.ByType, smellTestDeleted,
				"nothing was deleted; got %v", res.Summary.ByType)
		})
	}

	// Control: an explicit /dev/null IS still a deletion.
	res := AnalyzeDiff("diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ /dev/null\n@@ -1,1 +0,0 @@\n-\trequire.Equal(t, 1, 2)\n")
	assert.Contains(t, res.Summary.ByType, smellTestDeleted)
}

// A REMOVED line whose content begins with `-- ` (a SQL, Lua, Haskell or Ada
// comment) is content, not an old-file header. Swallowing it drops the removed
// line from the file's tally AND clobbers lastOldPath with garbage.
func TestAnalyzeDiff_RemovedDashDashLineIsContent(t *testing.T) {
	res := AnalyzeDiff("diff --git a/tests/schema.sql b/tests/schema.sql\n" +
		"--- a/tests/schema.sql\n+++ b/tests/schema.sql\n@@ -1,2 +1,1 @@\n" +
		// Diff line = the `-` removal prefix + the SQL comment `-- assert row count`.
		// The `-- assert row count` comment is the ONLY removed line carrying an
		// assertion token, so the smell fires if and only if that line survives the
		// parse. (A second assertion-bearing removed line would mask the defect.)
		"--- assert row count\n-SELECT count(*) FROM t;\n+SELECT 1;\n" + dsImplOnly)
	require.Len(t, res.Files.Test, 1, "test=%v impl=%v", res.Files.Test, res.Files.Impl)
	assert.Contains(t, res.Summary.ByType, smellWeakenedAssertion,
		"the removed `-- assert` line must be counted, got %v", res.Summary.ByType)
	assert.Equal(t, VerdictHard, res.Summary.Verdict)
}

// Fix-generating models append explanatory prose after the diff constantly, and
// a markdown bullet list starts every line with `- `. Parsing that as removed
// content turns a GOOD fix into a false HARD weakened_assertion: the fix is
// withheld, a retry round-trip is burned, and the finding is stamped with a
// FixWarning accusing the model of deleting assertions it never touched.
func TestAnalyzeDiff_TrailingProseIsNotDiffContent(t *testing.T) {
	prose := "\nNotes on this change:\n" +
		"- assert the new value is returned\n" +
		"- expect no regression in the caller\n" +
		"+ follow-up: extend to the batch path\n"

	withProse := AnalyzeDiff(dsImplOnly + dsTestOnlyClean + prose)
	clean := AnalyzeDiff(dsImplOnly + dsTestOnlyClean)

	assert.Equal(t, clean.Summary.Verdict, withProse.Summary.Verdict,
		"trailing prose must not change the verdict; got %v vs %v",
		withProse.Summary.ByType, clean.Summary.ByType)
	assert.NotContains(t, withProse.Summary.ByType, smellWeakenedAssertion,
		"prose bullets are not removed assertions; got %v", withProse.Summary.ByType)
	assert.Equal(t, clean.Files.Impl, withProse.Files.Impl)
	assert.Equal(t, clean.Files.Test, withProse.Files.Test)
}

// smell.File is MODEL-CONTROLLED: it comes verbatim from `+++ b/<anything>` via
// smellHeaderPath, which caps nothing. smellFeedback is interpolated into the
// retry prompt (paid for in tokens on every rejected fix), into
// logPipelineWarning, and — on the double-HARD halt path — verbatim into
// f.FixWarning, which lands in findings.json and the rendered report. The const
// block's claim that bounding evidence and item count suffices only holds if the
// path is bounded too.
func TestSmellFeedback_BoundsModelControlledFilePath(t *testing.T) {
	huge := strings.Repeat("p", 60_000)
	fb := smellFeedback(AnalyzeDiff(
		"diff --git a/" + huge + " b/" + huge + "\n--- a/" + huge + "\n+++ b/" + huge +
			"\n@@ -1 +1,2 @@\n+\t//nolint:gosec\n"))

	assert.Contains(t, fb, smellSuppression, "the actionable type name is never truncated")
	assert.Less(t, len(fb), 4*maxSmellEvidenceRunes,
		"a model-controlled path must not balloon the retry prompt (got %d bytes)", len(fb))

	// The whole feedback string stays bounded even at the item cap, where every
	// item contributes its own path.
	var many strings.Builder
	for i := 0; i < maxSmellFeedbackItems*2; i++ {
		p := fmt.Sprintf("%s%d.go", huge, i)
		many.WriteString("diff --git a/" + p + " b/" + p + "\n--- a/" + p + "\n+++ b/" + p +
			"\n@@ -1 +1,2 @@\n+\t//nolint:gosec\n")
	}
	fb = smellFeedback(AnalyzeDiff(many.String()))
	assert.Less(t, len(fb), maxSmellFeedbackItems*4*maxSmellEvidenceRunes,
		"total feedback must stay bounded across items (got %d bytes)", len(fb))
}

// dsRealGitDiff is a VERBATIM capture of `git diff` — not a hand-written
// fixture. Every other fixture in this file shares one shape: `diff --git`
// present, no index line, hand-written hunk counts that do not match the body,
// nothing after the last hunk. Real executor output carries index lines, file
// modes, a `--- /dev/null` new-file header, ACCURATE hunk counts, and trailing
// context text on the `@@` line. All three parser defects found in this file
// lived in shapes the hand-written fixtures never took, which is why 861 lines of
// tests caught none of them.
const dsRealGitDiff = `diff --git a/helper.go b/helper.go
new file mode 100644
index 0000000..8731fab
--- /dev/null
+++ b/helper.go
@@ -0,0 +1,3 @@
+package p
+
+func helper() {}
diff --git a/select.go b/select.go
index 68df499..d0bb993 100644
--- a/select.go
+++ b/select.go
@@ -1,5 +1,5 @@
 package p
 
 func pick() int {
-	return 0
+	return 1
 }
diff --git a/select_test.go b/select_test.go
index 293485d..11315e7 100644
--- a/select_test.go
+++ b/select_test.go
@@ -3,7 +3,5 @@ package p
 import "testing"
 
 func TestPick(t *testing.T) {
-	if pick() != 1 {
-		t.Fatalf("bad")
-	}
+	_ = pick()
 }
`

// A real capture must parse as accurately as the hand-written fixtures: the
// index/mode/`--- /dev/null` lines must be ignored rather than bound as files,
// the trailing text on the `@@` header must not break the count parse, and the
// assertion loss in the test file must still be caught.
func TestAnalyzeDiff_RealCapturedGitDiff(t *testing.T) {
	res := AnalyzeDiff(dsRealGitDiff)

	assert.ElementsMatch(t, []string{"helper.go", "select.go"}, res.Files.Impl,
		"index/mode/dev-null lines must not become files; got %v", res.Files.Impl)
	assert.Equal(t, []string{"select_test.go"}, res.Files.Test)
	assert.NotContains(t, res.Summary.ByType, smellTestDeleted,
		"a `--- /dev/null` NEW-file header is a creation, not a deletion; got %v", res.Summary.ByType)
	assert.Contains(t, res.Summary.ByType, smellWeakenedAssertion,
		"the removed t.Fatalf assertion must still be caught; got %v", res.Summary.ByType)
	assert.Equal(t, VerdictHard, res.Summary.Verdict)

	// `git diff` hunk counts are ACCURATE, so the body ends exactly where the
	// header says — appending prose must change nothing.
	withProse := AnalyzeDiff(dsRealGitDiff + "\nNotes:\n- assert the new value\n- expect no regression\n")
	assert.Equal(t, res.Summary.ByType, withProse.Summary.ByType)
	assert.Equal(t, res.Files.Impl, withProse.Files.Impl)
}

// --- one-way drift corpus (TD: diffsmell.go:3) ---

// corpusCase is one entry in testdata/diffsmell/corpus.json: a diff file plus the
// verdict and smell types atcr's OWN AnalyzeDiff must produce for it.
type corpusCase struct {
	File    string   `json:"file"`
	Verdict string   `json:"verdict"`
	Types   []string `json:"types"`
	Note    string   `json:"note"`
}

// The corpus is the drift-detection artifact for this port. It is ONE-WAY by
// deliberate choice (see the header comment): it pins atcr's own AnalyzeDiff and
// is NOT automatically verified against llm-tools. A two-way corpus would have to
// vendor the upstream analyzer or shell out to an installed llm-support binary,
// reintroducing exactly the cross-module coupling this port exists to avoid.
//
// Its value is that the fixtures live as real .diff files rather than Go string
// literals, so an upstream change can be replayed against them by hand, and every
// detector plus every parser shape this file has been bitten by stays pinned in
// one reviewable place.
func TestAnalyzeDiff_Corpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "diffsmell", "corpus.json"))
	require.NoError(t, err, "the drift corpus must exist")

	var cases []corpusCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases, "an empty corpus pins nothing")

	seenTypes := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.File, func(t *testing.T) {
			diff, err := os.ReadFile(filepath.Join("testdata", "diffsmell", tc.File))
			require.NoError(t, err, "corpus.json names a diff that does not exist")

			res := AnalyzeDiff(string(diff))
			require.NotNil(t, res)
			assert.Equal(t, tc.Verdict, res.Summary.Verdict, "%s: %s", tc.File, tc.Note)

			var got []string
			for k := range res.Summary.ByType {
				got = append(got, k)
			}
			assert.ElementsMatch(t, tc.Types, got, "%s: %s", tc.File, tc.Note)
		})
		for _, ty := range tc.Types {
			seenTypes[ty] = true
		}
	}

	// Every smell type this port can emit must appear somewhere in the corpus,
	// so adding a detector without a corpus entry fails here rather than silently
	// leaving the new behaviour unpinned.
	for _, ty := range []string{
		smellTestOnly, smellWeakenedAssertion, smellTestDeleted, smellTestSkipped,
		smellTestRenamedAway, smellSuppression, smellEmptyCatch, smellStubBody,
	} {
		assert.True(t, seenTypes[ty], "smell type %q has no corpus entry", ty)
	}
}
