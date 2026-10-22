package cli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/stretchr/testify/require"
)

// fixtureGit builds `git <args...>` for the test fixture repository at dir. Every
// git fixture in this package goes through it: gitexec.FixtureCommandContext pins
// GIT_DIR, GIT_WORK_TREE and GIT_CEILING_DIRECTORIES to dir, so neither an
// inherited GIT_DIR nor a wrong cwd can retarget the command at the developer's
// checkout. The fixed identity lets commits succeed without any user config.
func fixtureGit(dir string, args ...string) *exec.Cmd {
	cmd := gitexec.FixtureCommandContext(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid",
	)
	return cmd
}

// runFixtureGit runs fixtureGit and fails the test on error.
func runFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := fixtureGit(dir, args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// TestGitFixtures_InheritedGitDirLeavesOuterRepoUntouched is the Epic 45.0
// regression for this package's git fixture helpers. Git exports GIT_DIR into a
// hook's environment, and GIT_DIR beats both `git -C` and cmd.Dir, so a fixture
// run under a hook used to commit into the surrounding repository. Each helper
// below runs with GIT_DIR exported at a throwaway "outer" repo; that repo's HEAD
// and core.bare must not move.
func TestGitFixtures_InheritedGitDirLeavesOuterRepoUntouched(t *testing.T) {
	helpers := map[string]func(t *testing.T){
		"initGitRepo":               initGitRepo,
		"initGitRepoWithChange":     initGitRepoWithChange,
		"initGitRepoWithWideChange": initGitRepoWithWideChange,
		"initBaselineRepo":          initBaselineRepo,
		"gitRun": func(t *testing.T) {
			initBaselineRepo(t)
			require.NoError(t, os.WriteFile("d.txt", []byte("d\n"), 0o644))
			gitRun(t, "add", "d.txt")
			gitRun(t, "commit", "-qm", "add d")
		},
		"gitRevParse": func(t *testing.T) {
			initBaselineRepo(t)
			require.NotEmpty(t, gitRevParse(t, "HEAD"))
		},
		"gitLsFiles": func(t *testing.T) {
			initBaselineRepo(t)
			require.Contains(t, gitLsFiles(t), "a.txt")
		},
		"stageTestDeletion": func(t *testing.T) {
			initGitRepo(t)
			stageTestDeletion(t)
			require.NotEmpty(t, gitSmellOutput(t, "diff", "--cached", "--name-only"))
		},
	}
	for name, helper := range helpers {
		t.Run(name, func(t *testing.T) {
			outer := t.TempDir()
			runFixtureGit(t, outer, "init", "-q")
			runFixtureGit(t, outer, "commit", "--allow-empty", "-qm", "outer")
			head, bare := outerRepoState(t, outer)

			isolate(t)
			t.Setenv("GIT_DIR", outer+"/.git")
			helper(t)

			gotHead, gotBare := outerRepoState(t, outer)
			require.Equal(t, head, gotHead, "%s moved the inherited GIT_DIR's HEAD", name)
			require.Equal(t, bare, gotBare, "%s flipped the inherited GIT_DIR's core.bare", name)
		})
	}
}

// outerRepoState reads the HEAD SHA and core.bare of the repo at dir.
func outerRepoState(t *testing.T, dir string) (head, bare string) {
	t.Helper()
	out, err := gitexec.FixtureCommandContext(context.Background(), dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	b, err := gitexec.FixtureCommandContext(context.Background(), dir, "config", "core.bare").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out)), strings.TrimSpace(string(b))
}
