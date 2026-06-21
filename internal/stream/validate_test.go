package stream

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidatePath_ExistingFile: a finding whose file exists under root is
// flagged valid with no warning.
func TestValidatePath_ExistingFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/auth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/auth/validate.go"), []byte("package auth\n"), 0o644))

	f := Finding{File: "internal/auth/validate.go", Line: 10}
	ValidatePath(&f, root, nil)

	assert.True(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}

// TestValidatePath_MissingFile: a hallucinated path is flagged invalid with the
// canonical warning (AC2).
func TestValidatePath_MissingFile(t *testing.T) {
	root := t.TempDir()

	f := Finding{File: "internal/auth/validator.go", Line: 10}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_Typo: the real file is validate.go but the finding cites
// validator.go — a typo — so it is invalid (AC5 typo case).
func TestValidatePath_Typo(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/auth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/auth/validate.go"), []byte("package auth\n"), 0o644))

	f := Finding{File: "internal/auth/validator.go", Line: 5}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_WrongDirectory: the file exists under pkg/auth but the
// finding cites internal/auth — wrong directory — so it is invalid (AC5
// wrong-directory case).
func TestValidatePath_WrongDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg/auth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/auth/validator.go"), []byte("package auth\n"), 0o644))

	f := Finding{File: "internal/auth/validator.go", Line: 5}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_EmptyFileLeftUnflagged: a finding with no file path has
// nothing to validate and must never be falsely flagged.
func TestValidatePath_EmptyFileLeftUnflagged(t *testing.T) {
	f := Finding{File: "", Line: 0}
	ValidatePath(&f, t.TempDir(), nil)

	assert.False(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}

// TestValidatePath_EmptyRootDefaultsToCwd: an empty root resolves against "."
// (the package dir during tests), where parser.go exists.
func TestValidatePath_EmptyRootDefaultsToCwd(t *testing.T) {
	f := Finding{File: "parser.go"}
	ValidatePath(&f, "", nil)

	assert.True(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}

// TestValidatePath_EscapesRootIsInvalid: a traversal path is flagged invalid
// even when the target file genuinely exists outside the root — validation must
// never escape the reviewed repo (adversarial: existence oracle).
func TestValidatePath_EscapesRootIsInvalid(t *testing.T) {
	root := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(root), "atcr-outside.go")
	require.NoError(t, os.WriteFile(parentFile, []byte("package x\n"), 0o644))
	t.Cleanup(func() { _ = os.Remove(parentFile) })

	f := Finding{File: "../atcr-outside.go"}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_AbsolutePathNeutralized: an absolute File is re-rooted under
// root (never the literal system path), so a system file outside the repo is
// flagged invalid rather than leaking its existence.
func TestValidatePath_AbsolutePathNeutralized(t *testing.T) {
	root := t.TempDir()
	f := Finding{File: "/etc/hosts"}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_NilSafe: a nil finding pointer is a no-op, not a panic.
func TestValidatePath_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() { ValidatePath(nil, t.TempDir(), nil) })
}

// TestValidatePath_SymlinkEscapeFlagged: a symlinked path segment that resolves
// outside the repo root must NOT be reported as present (Epic 5.4 AC5 — no
// existence oracle). The cleaned path "link/target" has no ".." so it clears the
// lexical guard; only symlink resolution catches the escape.
func TestValidatePath_SymlinkEscapeFlagged(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a sibling temp dir, outside root
	require.NoError(t, os.WriteFile(filepath.Join(outside, "target"), []byte("x\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	f := Finding{File: "link/target"}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid, "a file reached only via a symlink out of the repo must not be valid")
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestEscapesRoot_SeparatorIndependent: the traversal guard must treat a
// filepath.Rel result as an escape regardless of which path separator it carries.
// filepath.Rel emits OS-native separators (backslash on Windows); comparing
// against a hardcoded filepath.Separator means a "..\\foo" form reaching the
// check on a platform with a different separator slips through and a ../foo escape
// passes the guard. The check must normalize before comparing.
func TestEscapesRoot_SeparatorIndependent(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"..", true},
		{"../foo", true},
		{"..\\foo", true}, // backslash form — the cross-platform fragility case
		{"../foo/bar", true},
		{"foo", false},
		{"foo/bar", false},
		{"..foo", false},      // a filename beginning with .., not a traversal
		{"foo/../bar", false}, // a cleaned Rel result never escapes via an interior ..
		{"", false},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, escapesRoot(c.rel), "escapesRoot(%q)", c.rel)
	}
}

// TestValidatePath_SuggestsWrongDirectory: with a candidate index, a wrong-
// directory hallucination yields the real file as PathSuggestion (AC2/AC6).
func TestValidatePath_SuggestsWrongDirectory(t *testing.T) {
	root := gitRepo(t, "pkg/auth/validator.go")
	idx := BuildFileIndex(root)

	f := Finding{File: "internal/auth/validator.go"}
	ValidatePath(&f, root, idx)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
	assert.Equal(t, "pkg/auth/validator.go", f.PathSuggestion)
}

// TestValidatePath_SuggestsTypo: a same-directory typo yields the closest file
// (the 5.0 motivating example) as PathSuggestion (AC4).
func TestValidatePath_SuggestsTypo(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")
	idx := BuildFileIndex(root)

	f := Finding{File: "internal/auth/validator.go"}
	ValidatePath(&f, root, idx)

	assert.False(t, f.PathValid)
	assert.Equal(t, "internal/auth/validate.go", f.PathSuggestion)
}

// TestValidatePath_SuggestsCaseTypo: a case-only difference is flagged invalid
// and suggests the correctly-cased path — even though os.Stat resolves it as
// present on a case-insensitive filesystem (AC3).
func TestValidatePath_SuggestsCaseTypo(t *testing.T) {
	root := gitRepo(t, "internal/auth/parser.go")
	idx := BuildFileIndex(root)

	f := Finding{File: "internal/auth/Parser.go"}
	ValidatePath(&f, root, idx)

	assert.False(t, f.PathValid, "a case-only typo must be flagged even on case-insensitive FS")
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
	assert.Equal(t, "internal/auth/parser.go", f.PathSuggestion)
}

// TestValidatePath_ValidFileNoSuggestion: a correctly-cited tracked file stays
// valid with no suggestion.
func TestValidatePath_ValidFileNoSuggestion(t *testing.T) {
	root := gitRepo(t, "internal/auth/validate.go")
	idx := BuildFileIndex(root)

	f := Finding{File: "internal/auth/validate.go"}
	ValidatePath(&f, root, idx)

	assert.True(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
	assert.Empty(t, f.PathSuggestion)
}

// TestValidatePath_SymlinkEscapeNoSuggestion: AC5 full — a symlink-escaping path
// stays flagged invalid AND emits no suggestion, even with an index present.
func TestValidatePath_SymlinkEscapeNoSuggestion(t *testing.T) {
	root := gitRepo(t, "internal/real.go")
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "known"), []byte("x\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
	idx := BuildFileIndex(root)

	f := Finding{File: "link/known"}
	ValidatePath(&f, root, idx)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
	assert.Empty(t, f.PathSuggestion)
}

// TestValidatePath_IndeterminateEmitsMetric: when existence cannot be proven —
// EvalSymlinks returns a permission/IO error rather than a clean "not found" —
// the finding is left unflagged (never a false "file not found"), but the
// indeterminate branch must no longer be silent. It increments an observability
// counter so a systematic permission problem suppressing all path validation is
// visible in production rather than swallowing every finding without a trace.
//
// The indeterminate result is forced by routing the lookup through a regular
// file standing where a directory segment is expected: EvalSymlinks then fails
// with ENOTDIR, an error os.IsNotExist rejects, so existsContained returns
// existsIndeterminate.
func TestValidatePath_IndeterminateEmitsMetric(t *testing.T) {
	metrics.DefaultRegistry.Reset()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "notadir"), []byte("x\n"), 0o644))

	f := Finding{File: "notadir/child.go", Line: 7}
	ValidatePath(&f, root, nil)

	// An indeterminate result must never masquerade as "file not found".
	assert.False(t, f.PathValid)
	assert.Empty(t, f.PathWarning)

	// ...but it must be counted, so the silent branch becomes observable.
	assert.Equal(t, int64(1), metrics.Counter("atcr_path_validation_indeterminate_total").Value(),
		"an indeterminate path check must increment the observability counter")
}

// TestValidatePath_RootUnresolvedEmitsMetric: when the validation root itself
// cannot be symlink-resolved (a broken symlink, a permission error), the code
// previously fell back to the unresolved path silently — a degraded containment
// check with no signal. The failure must now be observable via a counter, the
// same mechanism the indeterminate branch uses, so a misconfigured root does not
// silently weaken every path check.
//
// The failure is forced with a root that is a symlink to a nonexistent target:
// filepath.EvalSymlinks on it returns an error that is not a clean "not found"
// of the file under it, exercising the root-resolution failure branch.
func TestValidatePath_RootUnresolvedEmitsMetric(t *testing.T) {
	metrics.DefaultRegistry.Reset()

	root := filepath.Join(t.TempDir(), "broken-root")
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "nonexistent-target"), root))

	f := Finding{File: "child.go", Line: 3}
	ValidatePath(&f, root, nil)

	assert.Equal(t, int64(1), metrics.Counter("atcr_path_validation_root_unresolved_total").Value(),
		"an unresolvable validation root must increment the observability counter")
}

// TestValidatePath_NilIndexNoSuggestion: with no index (non-git repo), a missing
// path is flagged invalid but gets no suggestion — graceful degradation to 5.0
// existence-only behavior.
func TestValidatePath_NilIndexNoSuggestion(t *testing.T) {
	root := t.TempDir()

	f := Finding{File: "internal/auth/validator.go"}
	ValidatePath(&f, root, nil)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
	assert.Empty(t, f.PathSuggestion)
}
