package benchmark

import (
	"os"
	"path/filepath"
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
@@ -10,6 +10,7 @@ class A:
 ctx10
 ctx11
-old12
+new12
+extra13
 ctx14
 ctx15
@@ -40,4 +41,5 @@ class B:
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
	assert.Equal(t, []int{12, 13, 42}, m.AddedLines("pkg/one.py"))
	assert.Equal(t, []int{1, 2, 3}, m.AddedLines("pkg/two.py"))
}

func TestParseDiffLineMap_MapsRemovedLinesOnTheBaseSide(t *testing.T) {
	m, err := ParseDiffLineMap([]byte(multiHunkDiff))
	require.NoError(t, err)

	// Hunk 1 starts at BASE line 10. ctx10=10, ctx11=11, old12=12.
	assert.Equal(t, []int{12}, m.RemovedLines("pkg/one.py"))
	assert.Empty(t, m.RemovedLines("pkg/two.py"))
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
	assert.Equal(t, []string{"pkg/one.py", "pkg/two.py"}, m.Files(), "sorted, so callers are deterministic")
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
	assert.Equal(t, []int{1, 2}, m.RemovedLines("pkg/gone.py"))
	assert.Empty(t, m.AddedLines("pkg/gone.py"))
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
	assert.Equal(t, []int{2}, m.AddedLines("f.txt"))
	assert.Equal(t, []int{2}, m.RemovedLines("f.txt"))
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
	assert.Equal(t, []int{7}, m.AddedLines("f.txt"))
	assert.Equal(t, []int{7}, m.RemovedLines("f.txt"))
}

func TestParseDiffLineMap_RejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name, diff, wantErr string
	}{
		{"hunk before any file header", "@@ -1,1 +1,1 @@\n-a\n+b\n", "no file"},
		{"unparseable hunk header", "--- a/f\n+++ b/f\n@@ nonsense @@\n", "hunk header"},
		{"body line before any hunk", "--- a/f\n+++ b/f\n+orphan\n", "hunk"},
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
	assert.Empty(t, m.Files())
}

// The SHIPPED case's outside_diff values must be TRUE of its own diff. This is the
// authoring checklist item FORMAT.md cannot enforce by reading, made mechanical:
// an outside_diff:true finding whose line is an added line is a mis-authored case,
// and it would score the tier's headline metric while measuring nothing.
func TestParseDiffLineMap_ShippedCasesOutsideDiffValuesAreTrue(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(c.Dir, c.Diff))
			require.NoError(t, err)
			lm, err := ParseDiffLineMap(raw)
			require.NoError(t, err)

			for _, f := range c.ExpectedFindings {
				added := lm.IsAddedLine(f.File, f.LineStart)
				if f.IsOutsideDiff() {
					assert.False(t, added,
						"finding %q declares outside_diff:true but %s:%d IS an added line of the case's own diff",
						f.ID, f.File, f.LineStart)
				}
			}
		})
	}
}
