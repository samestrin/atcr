package stream

import (
	"os"
	"path/filepath"
	"testing"

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
	ValidatePath(&f, root)

	assert.True(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}

// TestValidatePath_MissingFile: a hallucinated path is flagged invalid with the
// canonical warning (AC2).
func TestValidatePath_MissingFile(t *testing.T) {
	root := t.TempDir()

	f := Finding{File: "internal/auth/validator.go", Line: 10}
	ValidatePath(&f, root)

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
	ValidatePath(&f, root)

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
	ValidatePath(&f, root)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_EmptyFileLeftUnflagged: a finding with no file path has
// nothing to validate and must never be falsely flagged.
func TestValidatePath_EmptyFileLeftUnflagged(t *testing.T) {
	f := Finding{File: "", Line: 0}
	ValidatePath(&f, t.TempDir())

	assert.False(t, f.PathValid)
	assert.Empty(t, f.PathWarning)
}

// TestValidatePath_EmptyRootDefaultsToCwd: an empty root resolves against "."
// (the package dir during tests), where parser.go exists.
func TestValidatePath_EmptyRootDefaultsToCwd(t *testing.T) {
	f := Finding{File: "parser.go"}
	ValidatePath(&f, "")

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
	ValidatePath(&f, root)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_AbsolutePathNeutralized: an absolute File is re-rooted under
// root (never the literal system path), so a system file outside the repo is
// flagged invalid rather than leaking its existence.
func TestValidatePath_AbsolutePathNeutralized(t *testing.T) {
	root := t.TempDir()
	f := Finding{File: "/etc/hosts"}
	ValidatePath(&f, root)

	assert.False(t, f.PathValid)
	assert.Equal(t, PathNotFoundWarning, f.PathWarning)
}

// TestValidatePath_NilSafe: a nil finding pointer is a no-op, not a panic.
func TestValidatePath_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() { ValidatePath(nil, t.TempDir()) })
}
