package verify

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The export shim is a translation layer, so these tests assert TRANSLATION
// fidelity — that every field of the package-local smellResult reaches the
// exported shape unchanged — not detection behavior. What counts as a smell is
// already pinned by diffsmell_test.go and the testdata/diffsmell corpus.

// deleteTestDiff removes a whole test file while editing an implementation
// file: the canonical HARD reward hack (test_deleted).
const deleteTestDiff = `diff --git a/internal/foo_test.go b/internal/foo_test.go
--- a/internal/foo_test.go
+++ /dev/null
@@ -1,3 +0,0 @@
-func TestFoo(t *testing.T) {
-	require.Equal(t, 1, Foo())
-}
diff --git a/internal/foo.go b/internal/foo.go
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,1 +1,1 @@
-func Foo() int { return 0 }
+func Foo() int { return 1 }
`

// softOnlyDiff adds a lint suppression to an implementation file: SOFT, no HARD.
const softOnlyDiff = `diff --git a/internal/foo.go b/internal/foo.go
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,1 +1,2 @@
 func Foo() int {
+	//nolint:errcheck
`

// cleanDiff is an ordinary implementation edit with no fingerprints.
const cleanDiff = `diff --git a/internal/foo.go b/internal/foo.go
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,1 +1,1 @@
-func Foo() int { return 0 }
+func Foo() int { return 1 }
`

func TestAnalyzeDiff_ExportedMirrorsInternal(t *testing.T) {
	for _, tc := range []struct {
		name string
		diff string
	}{
		{"hard", deleteTestDiff},
		{"soft", softOnlyDiff},
		{"clean", cleanDiff},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			internal := analyzeDiff(tc.diff)
			got := AnalyzeDiff(tc.diff)
			require.NotNil(t, got)

			require.Equal(t, internal.Summary.Verdict, got.Summary.Verdict)
			require.Equal(t, internal.Summary.Hard, got.Summary.Hard)
			require.Equal(t, internal.Summary.Soft, got.Summary.Soft)
			require.Equal(t, internal.Summary.TestFiles, got.Summary.TestFiles)
			require.Equal(t, internal.Summary.ImplFiles, got.Summary.ImplFiles)
			require.Equal(t, internal.Summary.ByType, got.Summary.ByType)
			require.Equal(t, internal.Files.Test, got.Files.Test)
			require.Equal(t, internal.Files.Impl, got.Files.Impl)

			require.Len(t, got.Smells, len(internal.Smells))
			for i := range internal.Smells {
				require.Equal(t, internal.Smells[i].Type, got.Smells[i].Type)
				require.Equal(t, internal.Smells[i].Severity, got.Smells[i].Severity)
				require.Equal(t, internal.Smells[i].File, got.Smells[i].File)
				require.Equal(t, internal.Smells[i].Evidence, got.Smells[i].Evidence)
			}
		})
	}
}

func TestAnalyzeDiff_HardVerdictOnDeletedTest(t *testing.T) {
	res := AnalyzeDiff(deleteTestDiff)
	require.Equal(t, VerdictHard, res.Summary.Verdict)
	require.Positive(t, res.Summary.Hard)

	var types []string
	for _, s := range res.Smells {
		types = append(types, s.Type)
		if s.Type == smellTestDeleted {
			require.Equal(t, SeverityHard, s.Severity)
		}
	}
	require.Contains(t, types, smellTestDeleted)
}

func TestAnalyzeDiff_SoftOnlyVerdict(t *testing.T) {
	res := AnalyzeDiff(softOnlyDiff)
	require.Equal(t, VerdictSoftOnly, res.Summary.Verdict)
	require.Zero(t, res.Summary.Hard)
	require.Positive(t, res.Summary.Soft)
}

func TestAnalyzeDiff_CleanVerdict(t *testing.T) {
	res := AnalyzeDiff(cleanDiff)
	require.Equal(t, VerdictClean, res.Summary.Verdict)
	require.Empty(t, res.Smells)
}

// A test-only diff raises HARD test_only here even though the fix-selection
// gate suppresses it for test-path findings — the standalone caller has no
// finding to key that exemption on. Pins the documented divergence.
func TestAnalyzeDiff_TestOnlyNotSuppressed(t *testing.T) {
	const testOnly = `diff --git a/internal/foo_test.go b/internal/foo_test.go
--- a/internal/foo_test.go
+++ b/internal/foo_test.go
@@ -1,1 +1,1 @@
-	require.Equal(t, 1, Foo())
+	require.NotNil(t, Foo())
`
	res := AnalyzeDiff(testOnly)
	require.Equal(t, VerdictHard, res.Summary.Verdict)
	require.Contains(t, res.Summary.ByType, smellTestOnly)
}

// Never nil, and the slice/map fields are non-nil so JSON renders [] / {}
// rather than null — a consumer indexing the output must not have to nil-check.
func TestAnalyzeDiff_NeverNilAndMarshalsWithoutNulls(t *testing.T) {
	res := AnalyzeDiff("")
	require.NotNil(t, res)
	require.NotNil(t, res.Files.Test)
	require.NotNil(t, res.Files.Impl)
	require.NotNil(t, res.Smells)
	require.NotNil(t, res.Summary.ByType)

	b, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(b), "null")
}

// The wire contract a downstream consumer pins: upstream diff-smell's key names.
func TestDiffSmellResult_JSONFieldNames(t *testing.T) {
	b, err := json.Marshal(AnalyzeDiff(deleteTestDiff))
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Contains(t, m, "files")
	require.Contains(t, m, "smells")
	require.Contains(t, m, "summary")

	summary := m["summary"].(map[string]any)
	for _, k := range []string{"test_files", "impl_files", "hard", "soft", "by_type", "verdict"} {
		require.Contains(t, summary, k)
	}
	files := m["files"].(map[string]any)
	require.Contains(t, files, "test")
	require.Contains(t, files, "impl")

	smell := m["smells"].([]any)[0].(map[string]any)
	for _, k := range []string{"type", "severity", "file", "evidence"} {
		require.Contains(t, smell, k)
	}
}

func TestLooksLikeUnifiedDiff_Exported(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"git header", deleteTestDiff, true},
		{"hunk header only", "@@ -1,1 +1,1 @@\n-a\n+b", true},
		{"old/new header pair", "--- a/x\n+++ b/x\n", true},
		{"prose with a leading plus", "+ add a nil check to Foo", false},
		{"empty", "", false},
		{"whitespace", "   \n\t\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, LooksLikeUnifiedDiff(tc.in))
			require.Equal(t, looksLikeUnifiedDiff(tc.in), LooksLikeUnifiedDiff(tc.in))
		})
	}
}

// The exported constants must be the same values the analyzer writes, not a
// second copy that could drift.
func TestExportedConstantsMatchInternal(t *testing.T) {
	require.Equal(t, smellVerdictClean, VerdictClean)
	require.Equal(t, smellVerdictSoftOnly, VerdictSoftOnly)
	require.Equal(t, smellVerdictHard, VerdictHard)
	require.Equal(t, smellSeverityHard, SeverityHard)
	require.Equal(t, smellSeveritySoft, SeveritySoft)
}
