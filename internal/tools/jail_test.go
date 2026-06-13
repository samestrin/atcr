package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkFixture(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
}

// TestNewJail_CanonicalizesRoot is the TD-002 regression: on macOS t.TempDir()
// lives under /var -> /private/var (a symlink), so a naive prefix check would
// false-reject a legitimate in-root file. The constructor must EvalSymlinks the
// root.
func TestNewJail_CanonicalizesRoot(t *testing.T) {
	root := t.TempDir()
	mkFixture(t, root, "main.go")
	j, err := NewJail(root)
	require.NoError(t, err)
	got, err := j.Resolve("main.go")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(got, "main.go"))
	assert.True(t, strings.HasPrefix(got, j.Root()+string(os.PathSeparator)))
}

func TestJail_Resolve(t *testing.T) {
	root := t.TempDir()
	mkFixture(t, root, "src/main.go")
	mkFixture(t, root, "README.md")
	mkFixture(t, root, ".gitignore")
	mkFixture(t, root, ".github/workflows/ci.yml")
	mkFixture(t, root, ".git/config")
	mkFixture(t, root, ".git/objects/ab/cdef1234")
	mkFixture(t, root, "foo.git/bar")

	j, err := NewJail(root)
	require.NoError(t, err)

	tests := []struct{ name, path, wantReason string }{
		{"valid nested", "src/main.go", ""},
		{"valid root file", "README.md", ""},
		{"internal dotdot stays in root", "src/sub/../../src/main.go", ""},
		{"gitignore allowed", ".gitignore", ""},
		{"github allowed", ".github/workflows/ci.yml", ""},
		{"foo.git allowed", "foo.git/bar", ""},
		{"absolute rejected", "/etc/passwd", "absolute path not allowed"},
		{"dotdot escape", "../../secrets", "path escapes snapshot root"},
		{"git config", ".git/config", "access to .git directory not allowed"},
		{"git objects", ".git/objects/ab/cdef1234", "access to .git directory not allowed"},
		{"empty", "", "empty path not allowed"},
		{"nul byte", "src/main\x00.go", "path contains NUL byte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := j.Resolve(tt.path)
			if tt.wantReason == "" {
				require.NoError(t, err)
				assert.True(t, got == j.Root() || strings.HasPrefix(got, j.Root()+string(os.PathSeparator)))
				return
			}
			require.Error(t, err)
			var je *JailError
			require.ErrorAs(t, err, &je)
			assert.Equal(t, tt.path, je.Path)
			assert.Contains(t, je.Reason, tt.wantReason)
		})
	}
}

func TestJail_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on Windows")
	}
	root := t.TempDir()
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(root, "link")))
	require.NoError(t, os.Symlink("..", filepath.Join(root, "escape")))
	j, err := NewJail(root)
	require.NoError(t, err)

	_, err = j.Resolve("link")
	var je *JailError
	require.ErrorAs(t, err, &je)
	assert.Contains(t, je.Reason, "escapes snapshot root")

	_, err = j.Resolve("escape/secrets")
	require.ErrorAs(t, err, &je)
	assert.Contains(t, je.Reason, "escapes snapshot root")
}

func TestJailError_FormatAndErrorsAs(t *testing.T) {
	e := &JailError{Path: "x", Reason: "empty path not allowed"}
	assert.Equal(t, "path jail: empty path not allowed: x", e.Error())
}
