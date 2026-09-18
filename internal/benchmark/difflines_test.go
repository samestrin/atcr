package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiHunkDiff edits one file in two places and adds a second file, so the map
// has to carry per-file state and reset its counters at every hunk header rather
// than running one counter across the whole patch.
const multiHunkDiff = `diff --git a/pkg/one.py b/pkg/one.py
index aaa..bbb 100644
--- a/pkg/one.py
+++ b/pkg/one.py
@@ -10,5 +10,6 @@ class A:
 ctx10
 ctx11
-old12
+new12
+extra13
 ctx14
 ctx15
@@ -40,3 +41,4 @@ class B:
 ctx41
+added42
 ctx43
 ctx44
diff --git a/pkg/two.py b/pkg/two.py
new file mode 100644
index 000..ccc
--- /dev/null
+++ b/pkg/two.py
@@ -0,0 +1,3 @@
+alpha
+beta
+gamma
`

func TestParseDiffLineMap_MapsAddedLinesPerHunk(t *testing.T) {
	m, err := ParseDiffLineMap([]byte(multiHunkDiff))
	require.NoError(t, err)

	// Hunk 1 starts at head line 10. ctx10=10, ctx11=11, new12=12, extra13=13.
	// Hunk 2 starts at head line 41. ctx41=41, added42=42.
	assert.Equal(t, []int{12, 13, 42}, m.addedLines("pkg/one.py"))
	assert.Equal(t, []int{1, 2, 3}, m.addedLines("pkg/two.py"))
}

func TestParseDiffLineMap_MapsRemovedLinesOnTheBaseSide(t *testing.T) {
	m, err := ParseDiffLineMap([]byte(multiHunkDiff))
	require.NoError(t, err)

	// Hunk 1 starts at BASE line 10. ctx10=10, ctx11=11, old12=12.
	assert.Equal(t, []int{12}, m.removedLines("pkg/one.py"))
	assert.Empty(t, m.removedLines("pkg/two.py"))
}

func TestParseDiffLineMap_IsAddedLine(t *testing.T) {
	m, err := ParseDiffLineMap([]byte(multiHunkDiff))
	require.NoError(t, err)

	assert.True(t, m.IsAddedLine("pkg/one.py", 12))
	assert.True(t, m.IsAddedLine("pkg/one.py", 42))
	assert.False(t, m.IsAddedLine("pkg/one.py", 11), "a context line is not an added line")
	assert.False(t, m.IsAddedLine("pkg/one.py", 43), "a context line after the hunk is not an added line")
	assert.False(t, m.IsAddedLine("pkg/absent.py", 12), "a file the diff never touched has no added lines")
}

func TestParseDiffLineMap_Files(t *testing.T) {
	m, err := ParseDiffLineMap([]byte(multiHunkDiff))
	require.NoError(t, err)
	assert.Equal(t, []string{"pkg/one.py", "pkg/two.py"}, m.files(), "sorted, so callers are deterministic")
}

// A deleted file has no head-side path. Its removed lines still belong to the
// base-side path, which is the only name that file ever had.
func TestParseDiffLineMap_DeletedFileKeepsItsBaseSidePath(t *testing.T) {
	const deletion = `diff --git a/pkg/gone.py b/pkg/gone.py
deleted file mode 100644
--- a/pkg/gone.py
+++ /dev/null
@@ -1,2 +0,0 @@
-one
-two
`
	m, err := ParseDiffLineMap([]byte(deletion))
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, m.removedLines("pkg/gone.py"))
	assert.Empty(t, m.addedLines("pkg/gone.py"))
}

// "\ No newline at end of file" is a marker, not content. Counting it as a line
// would shift every subsequent line number in the hunk by one.
func TestParseDiffLineMap_IgnoresNoNewlineMarker(t *testing.T) {
	const noEOL = `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -1,2 +1,2 @@
 keep
-old
\ No newline at end of file
+new
\ No newline at end of file
`
	m, err := ParseDiffLineMap([]byte(noEOL))
	require.NoError(t, err)
	assert.Equal(t, []int{2}, m.addedLines("f.txt"))
	assert.Equal(t, []int{2}, m.removedLines("f.txt"))
}

// A hunk header may omit the count, which means exactly one line.
func TestParseDiffLineMap_HandlesSingleLineHunkHeader(t *testing.T) {
	const single = `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -7 +7 @@
-old
+new
`
	m, err := ParseDiffLineMap([]byte(single))
	require.NoError(t, err)
	assert.Equal(t, []int{7}, m.addedLines("f.txt"))
	assert.Equal(t, []int{7}, m.removedLines("f.txt"))
}

// A REMOVED line whose own content begins with "-- " renders in a unified diff as
// "--- ...", which is byte-identical to the start of a file header. SQL, Lua and
// Haskell comments all look like this, as does `git format-patch`'s signature
// separator.
//
// Matching the header arm first does not merely misclassify that one line: it
// resets the parser's file state mid-hunk, so the whole map comes back EMPTY with
// no error. IsAddedLine is then false everywhere, condition 3 never fires, and an
// outside_diff:true expectation can be satisfied by a report citing an added line.
// That is AC3b defeated silently, on a case that parses and materializes cleanly.
func TestParseDiffLineMap_BodyLineThatLooksLikeAFileHeader(t *testing.T) {
	const sqlComment = `diff --git a/q.sql b/q.sql
--- a/q.sql
+++ b/q.sql
@@ -1,3 +1,3 @@
 SELECT 1;
--- old comment
+++ new comment
 SELECT 2;
`
	m, err := ParseDiffLineMap([]byte(sqlComment))
	require.NoError(t, err)

	assert.Equal(t, []string{"q.sql"}, m.files(), "the file state must survive a body line that looks like a header")
	assert.Equal(t, []int{2}, m.addedLines("q.sql"), `"+++ new comment" is an ADDED line whose content is "++ new comment"`)
	assert.Equal(t, []int{2}, m.removedLines("q.sql"), `"--- old comment" is a REMOVED line whose content is "-- old comment"`)
	assert.True(t, m.IsAddedLine("q.sql", 2), "condition 3 must still be able to fire on this file")
}

// A plain `diff -u` of two files has no "diff --git" separator, so consecutive
// file sections are told apart only by the header pair. The hunk's declared line
// counts are what say where one body ends and the next header begins.
func TestParseDiffLineMap_ConsecutiveFilesWithNoGitSeparator(t *testing.T) {
	const plain = `--- a/one.txt
+++ b/one.txt
@@ -1,2 +1,2 @@
 keep
-old
+new
--- a/two.txt
+++ b/two.txt
@@ -5,1 +5,2 @@
 ctx
+added
`
	m, err := ParseDiffLineMap([]byte(plain))
	require.NoError(t, err)

	assert.Equal(t, []string{"one.txt", "two.txt"}, m.files())
	assert.Equal(t, []int{2}, m.addedLines("one.txt"))
	assert.Equal(t, []int{6}, m.addedLines("two.txt"))
}

// Trailing content after the last hunk is not diff body. `git format-patch` ends
// with a "-- " signature separator, and counting it produces a phantom removed
// line beyond the end of the file.
func TestParseDiffLineMap_IgnoresTrailingContentAfterTheLastHunk(t *testing.T) {
	const withSignature = `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -1,2 +1,2 @@
 keep
-old
+new
--
2.43.0
`
	m, err := ParseDiffLineMap([]byte(withSignature))
	require.NoError(t, err)
	assert.Equal(t, []int{2}, m.removedLines("f.txt"), "the format-patch signature must not become a removed line")
	assert.Equal(t, []int{2}, m.addedLines("f.txt"))
}

// A hunk whose declared counts OVERSHOOT the body it actually carries must not
// silently eat the next file: when the counts are still unconsumed and a line
// arrives that can only be a file header (or a diff --git separator, or any
// other non-body line), the header's leading -/+ would be consumed as removed
// and added CONTENT, the next file would vanish from the map, and its lines
// would be attributed to the previous file. git apply rejects such a diff; the
// parser must reject it too, with a diagnostic — never a one-file map and a nil
// error.
func TestParseDiffLineMap_OverDeclaredHunkThatEatsTheNextFile(t *testing.T) {
	const probe = `--- a/one.txt
+++ b/one.txt
@@ -1,4 +1,4 @@
 ctx1
-old2
+new2
 ctx3
--- a/two.txt
+++ b/two.txt
@@ -5,1 +5,2 @@
 ctx5
+added6
`
	_, err := ParseDiffLineMap([]byte(probe))
	require.Error(t, err, "an over-declared hunk followed by the next file's headers must error, not produce a one-file map")
	assert.Contains(t, err.Error(), "one.txt", "the diagnostic should name the hunk's file")
}

// git's core.quotePath (on by default) C-quotes any path containing non-ASCII
// bytes, so a diff of pkg/e-acute.py carries `--- "a/pkg/e-acute.py"` with the
// quotes and octal escapes intact. The map must key that file under its REAL
// path: files() must return pkg/e-acute.py, and IsAddedLine must be true for its
// added line — otherwise condition 3 fails open for every finding in such a
// file, with no error anywhere.
func TestParseDiffLineMap_UnquotesGitQuotedPaths(t *testing.T) {
	const quoted = `diff --git "a/pkg/e-acute.py" "b/pkg/e-acute.py"
index aaa..bbb 100644
--- "a/pkg/e-acute.py"
+++ "b/pkg/e-acute.py"
@@ -1,1 +1,2 @@
 keep
+ajouté
`
	m, err := ParseDiffLineMap([]byte(quoted))
	require.NoError(t, err)
	assert.Equal(t, []string{"pkg/e-acute.py"}, m.files(), "the C-quoted path must be unquoted before keying")
	assert.True(t, m.IsAddedLine("pkg/e-acute.py", 2), "the added line must be findable under the real path")
}

// A diff emitted with --no-prefix/-p0 carries no a//b/ prefixes — and `a/pkg/a.py`
// is a legal repository path. pathMatches (match.go) strips the prefix only
// conditionally for exactly that reason; the line map must not key such a file
// under the stripped spelling, or IsAddedLine misses every line and condition 3
// fails open for the whole file.
func TestParseDiffLineMap_NoPrefixDiffKeepsARealLeadingASegment(t *testing.T) {
	const noPrefix = `diff --git a/pkg/a.py a/pkg/a.py
--- a/pkg/a.py
+++ a/pkg/a.py
@@ -1,1 +1,2 @@
 ctx
+added
`
	m, err := ParseDiffLineMap([]byte(noPrefix))
	require.NoError(t, err)
	assert.Equal(t, []string{"a/pkg/a.py"}, m.files(), "identical header spellings mean a no-prefix diff: the path is the real repository path")
	assert.True(t, m.IsAddedLine("a/pkg/a.py", 2), "the added line must be keyed under the real path")
	assert.False(t, m.IsAddedLine("pkg/a.py", 2), "the stripped spelling must NOT be a key")
}

// A hunk that declares MORE lines than its body carries, followed by the next
// file's git separator, must error — not silently re-read the separator as
// header text. This exercises the git-style over-declaration arm (the plain
// ---/+++ form is covered by TestParseDiffLineMap_OverDeclaredHunkThatEatsTheNextFile).
//
// NOTE: this test's original prescription asserted "both files appear with
// correct line numbers" — the parser's old fail-open behavior. The
// over-declared-count fix (difflines.go:88) made that input an error by design,
// so the assertion pins the error instead; the coverage intent is unchanged.
func TestParseDiffLineMap_HunkWithOverstatedCounts(t *testing.T) {
	const overstated = `diff --git a/pkg/one.py b/pkg/one.py
--- a/pkg/one.py
+++ b/pkg/one.py
@@ -1,4 +1,4 @@
 ctx1
-old2
+new2
 ctx3
diff --git a/pkg/two.py b/pkg/two.py
new file mode 100644
--- /dev/null
+++ b/pkg/two.py
@@ -0,0 +1,2 @@
+alpha
+beta
`
	_, err := ParseDiffLineMap([]byte(overstated))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pkg/one.py", "the diagnostic names the file whose hunk over-declared")
}

// A case's diff may carry a long minified or generated line. The parser's token
// buffer must absorb it: no error, and the line classified with the correct
// head-side number.
func TestParseDiffLineMap_HandlesAVeryLongLine(t *testing.T) {
	long := strings.Repeat("x", 200*1024)
	diff := "diff --git a/big.txt b/big.txt\n" +
		"--- a/big.txt\n" +
		"+++ b/big.txt\n" +
		"@@ -1,1 +1,2 @@\n" +
		" keep\n" +
		"+" + long + "\n"
	m, err := ParseDiffLineMap([]byte(diff))
	require.NoError(t, err)
	assert.Equal(t, []int{2}, m.addedLines("big.txt"), "the 200 KiB added line is head line 2")
}

// The shipped suite must key every finding's file under the EXACT spelling the
// finding declares. Any key-space disagreement — a C-quoted path, an over-eager
// a/ strip, a parser returning an empty map — silently voids condition 3 for
// that file: every outside_diff:true expectation in it becomes unenforceable
// while the negative-direction test above keeps passing. This is the positive
// half, with a positive pin proving condition 3 is LIVE on shipped data.
func TestParseDiffLineMap_ShippedCasesKeyFindingsUnderTheirExactSpelling(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	// Some shipped findings name files their case's diff never touches (condition
	// 3 is a structural no-op for them), so the count guard is SUITE-wide: across
	// all cases at least one finding must have been checked, or the assertion
	// above proved nothing.
	suiteChecked := 0
	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
			require.NoError(t, err)
			lm, err := ParseDiffLineMap(raw)
			require.NoError(t, err)

			for _, f := range c.ExpectedFindings {
				if !strings.Contains(string(raw), f.File) {
					continue // the diff never touches this file: condition 3 cannot fire for it
				}
				suiteChecked++
				assert.Contains(t, lm.files(), f.File,
					"finding %q names %s, which appears in the case's diff, but the line map keys no file under that exact spelling",
					f.ID, f.File)
			}

		})
	}
	require.Greater(t, suiteChecked, 0, "no shipped finding's file appears in its case's diff; the exact-spelling assertion checked nothing")

	// Positive pin: head line 43 of streamer/cursor.py IS an added line of
	// this case's diff (its added block spans head lines 30-43), so the
	// IsAddedLine lookup condition 3 relies on has something real to reject.
	for _, c := range m.Cases {
		if c.ID != "claim-absent-cursor-fix" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
		require.NoError(t, err)
		lm, err := ParseDiffLineMap(raw)
		require.NoError(t, err)
		assert.True(t, lm.IsAddedLine("streamer/cursor.py", 43),
			"condition 3 must be provably live on shipped data: streamer/cursor.py:43 is an added line")
	}
}

func TestParseDiffLineMap_RejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name, diff, wantErr string
	}{
		{"hunk before any file header", "@@ -1,1 +1,1 @@\n-a\n+b\n", "no file"},
		{"unparseable hunk header", "--- a/f\n+++ b/f\n@@ nonsense @@\n", "hunk header"},
		{"body line before any hunk", "--- a/f\n+++ b/f\n+orphan\n", "hunk"},
		// A negative start is not a diff a reader could act on, and accepting it
		// yields negative line numbers that can never match a reported finding —
		// a silent zero rather than a diagnostic.
		{"negative hunk start", "--- a/f\n+++ b/f\n@@ --5,2 +-3,2 @@\n ctx\n+add\n", "hunk header"},
		{"zero hunk start on a non-empty side", "--- a/f\n+++ b/f\n@@ -0,2 +0,2 @@\n ctx\n+add\n", "hunk header"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDiffLineMap([]byte(tt.diff))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// An empty diff is not an error here — it is a case that changes nothing, which
// the materializer rejects with a far better message than a parser could.
func TestParseDiffLineMap_EmptyDiffIsEmptyNotAnError(t *testing.T) {
	m, err := ParseDiffLineMap(nil)
	require.NoError(t, err)
	assert.Empty(t, m.files())
}

// The SHIPPED case's outside_diff values must be TRUE of its own diff. This is the
// authoring checklist item FORMAT.md cannot enforce by reading, made mechanical:
// an outside_diff:true finding whose line is an added line is a mis-authored case,
// and it would score the tier's headline metric while measuring nothing.
func TestParseDiffLineMap_ShippedCasesOutsideDiffValuesAreTrue(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	checked := 0
	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
			require.NoError(t, err)
			lm, err := ParseDiffLineMap(raw)
			require.NoError(t, err)

			for _, f := range c.ExpectedFindings {
				if !f.IsOutsideDiff() {
					continue
				}
				checked++
				// Every line of the DECLARED RANGE, not just line_start: the range is
				// the defect's body, and outside_diff:true declares that body lives in
				// UNCHANGED code — any added line inside it contaminates the claim, and
				// the REMOVED half is checked too (clause 3 is added OR removed): a
				// cited range deleted by the case's own diff is unwinnable content.
				// (Added lines merely INSIDE the tolerance window are a different,
				// legitimate situation: the change brushing the defect is exactly what
				// condition 3 blocks at match time, and the matcher's window test pins
				// that. The authoring contract is about the defect's own lines.)
				for line := f.LineStart; line <= f.LineEnd; line++ {
					assert.False(t, lm.IsAddedLine(f.File, line),
						"finding %q declares outside_diff:true but %s:%d IS an added line of the case's own diff",
						f.ID, f.File, line)
					assert.False(t, lm.IsRemovedLine(f.File, line),
						"finding %q declares outside_diff:true but %s:%d IS a removed line of the case's own diff",
						f.ID, f.File, line)
				}
			}
		})
	}
	// The guard that keeps this test honest: if a future edit flips the suite's
	// outside_diff values to false, the loop above silently checks nothing. The
	// count must be positive or the invariant was never exercised.
	require.Greater(t, checked, 0,
		"no shipped finding declares outside_diff:true; the condition-3 authoring check verified nothing")
}
