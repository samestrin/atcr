package fanout

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/gitexec"
)

// Shared test helpers for the git-plumbing fixtures in this package. Every git
// fixture in the package runs through fanoutGit, so none can retarget at the
// surrounding repository when the suite runs under a hook (Epic 45.0).

// fanoutGit runs git inside dir with a deterministic author/committer identity,
// failing the test on any error. It builds the command with
// gitexec.FixtureCommandContext, which pins GIT_DIR/GIT_WORK_TREE to dir and
// neuters the system/global git config. It returns trimmed combined output.
func fanoutGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitexec.FixtureCommandContext(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
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
