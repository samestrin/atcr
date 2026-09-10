package fanout

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shared test helpers for the git-plumbing fixtures in this package. The
// exec.Command("git", ...) + GIT_AUTHOR_NAME env-scrubbing pattern had drifted
// into one private copy per test file; this file is the one shared pair new
// tests must call instead of adding a ninth. Older duplicates elsewhere in the
// package are a separate cleanup and are intentionally left as they are.

// fanoutGit runs git inside dir with a deterministic author/committer identity
// and the system/global git config neutered, failing the test on any error. It
// returns trimmed combined output.
func fanoutGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// fanoutRepo builds a two-commit repository whose head commit asserts a claim,
// and returns the repo dir plus the base and head SHAs.
func fanoutRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 0 }\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the cursor file")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 1 }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "drain.go"), []byte("package p\n\nfunc Drain() int { return Begin() }\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "preserve the cursor across a cold drain\n\n- Begin() no longer returns a wiped offset\n- Drain() keeps the previous offset\n")
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}
