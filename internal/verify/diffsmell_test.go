package verify

import (
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

const dsEmptyCatch = `diff --git a/src/app.js b/src/app.js
--- a/src/app.js
+++ b/src/app.js
@@ -10,2 +10,3 @@
 function pick() {
+  try { risky(); } catch (e) { }
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
		{"test only is hard", dsTestOnly, smellVerdictHard, []string{smellTestOnly}},
		{"weakened assertion is hard", dsWeakenedAssertion, smellVerdictHard, []string{smellWeakenedAssertion}},
		{"suppression is soft", dsSuppression, smellVerdictSoftOnly, []string{smellSuppression}},
		{"stub body is soft", dsStubBody, smellVerdictSoftOnly, []string{smellStubBody}},
		{"empty catch is soft", dsEmptyCatch, smellVerdictSoftOnly, []string{smellEmptyCatch}},
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
	} {
		assert.True(t, isSmellTestPath(p), "expected test path: %s", p)
	}
	for _, p := range []string{
		"internal/verify/contest.go",
		"internal/verify/latest_results.go",
		"src/protest.ts",
		"internal/verify/select.go",
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

// Deleting a whole test file is the archetypal reward hack: it must be HARD.
func TestAnalyzeDiff_TestFileDeletionIsHard(t *testing.T) {
	res := analyzeDiff("diff --git a/x_test.go b/x_test.go\ndeleted file mode 100644\n--- a/x_test.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-\trequire.Equal(t, 1, 2)\n")
	assert.Equal(t, smellVerdictHard, res.Summary.Verdict)
	assert.Contains(t, res.Summary.ByType, smellTestOnly)
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

// smellTypes returns the deterministic, sorted, deduplicated type list used for
// the FixReview annotation.
func TestSmellTypes(t *testing.T) {
	res := analyzeDiff(dsSuppression + strings.Replace(dsStubBody, "select.go", "other.go", -1))
	assert.Equal(t, []string{smellStubBody, smellSuppression}, smellTypes(res))
	assert.Empty(t, smellTypes(analyzeDiff(dsImplOnly)))
	assert.Empty(t, smellTypes(nil))
}
