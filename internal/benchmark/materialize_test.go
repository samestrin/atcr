package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// materializableCase writes a case whose diff genuinely applies to its base tree,
// so a test can exercise materialization rather than the load-time guards.
func materializableCase(t *testing.T) RepoStateCase {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "apply-case")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "base", "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "base", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "pkg", "example.py"), []byte("one\ntwo\nthree\n"), 0o600))
	// An executable base file: the +x bit is part of the tree object, and the
	// materialized commit must carry mode 100755 for it (see the mode test).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "bin", "run.sh"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "commit-message.txt"),
		[]byte("feat: change two\n\nBody line.\n\nSigned-off-by: A Dev <dev@example.com>\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "change.diff"), []byte(
		"diff --git a/pkg/example.py b/pkg/example.py\n"+
			"--- a/pkg/example.py\n"+
			"+++ b/pkg/example.py\n"+
			"@@ -1,3 +1,3 @@\n"+
			" one\n"+
			"-two\n"+
			"+TWO\n"+
			" three\n"), 0o600))
	return RepoStateCase{
		ID: "apply-case", Format: FormatRepoStateV1, BaseTree: "base",
		CommitMessage: "commit-message.txt", Diff: "change.diff", Dir: dir,
		ExpectedFindings: []ExpectedFinding{{ID: "f", File: "pkg/example.py", LineStart: 2, LineEnd: 2,
			OutsideDiff: boolPtr(false), Category: "correctness", Summary: "s"}},
	}
}

func boolPtr(b bool) *bool { return &b }

func gitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := gitexec.CommandContextFn(context.Background(), append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	require.NoError(t, err, "git %s", strings.Join(args, " "))
	return string(out)
}

// T2's headline: a case becomes a working tree a review RANGE can be computed
// against. Two commits, not one — a single squashed commit has no base..head.
// The doc says dest must be an EXISTING EMPTY directory; the function must
// enforce its own contract. A stray pre-existing file in dest is otherwise swept
// into the BASE commit by git add -A — silently, with no error — so the base
// tree, the base..head range and the working tree disagree.
func TestMaterializeCase_RejectsANonEmptyDest(t *testing.T) {
	c := materializableCase(t)
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "LEFTOVER.txt"), []byte("x"), 0o600))

	_, err := MaterializeCase(context.Background(), c, dest)
	require.Error(t, err, "a non-empty dest must be refused, not committed")
	assert.Contains(t, err.Error(), c.ID, "the error names the case")
	assert.Contains(t, err.Error(), "LEFTOVER.txt", "the error names what was in the way")
}

func TestMaterializeCase_RejectsAMissingDest(t *testing.T) {
	c := materializableCase(t)
	_, err := MaterializeCase(context.Background(), c, filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err, "dest must already exist; the caller owns its lifetime")
	assert.Contains(t, err.Error(), c.ID)
}

// runGit formats args[0] into its error, so a call with NO args panics with an
// index-out-of-range instead of returning an error.
// A failing git apply folds git's stderr into the returned error verbatim. A
// diff failing across many files emits several KiB of per-file error lines, and
// every byte lands in the operator's stderr on a near-miss. The captured output
// must be bounded: the FIRST lines carry the diagnosis, the tail is noise.
func TestRunGit_TruncatesCapturedOutputInErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "diff --git a/missing-%d.txt b/missing-%d.txt\n", i, i)
		fmt.Fprintf(&sb, "--- a/missing-%d.txt\n", i)
		fmt.Fprintf(&sb, "+++ b/missing-%d.txt\n", i)
		fmt.Fprintf(&sb, "@@ -1,1 +1,1 @@\n")
		fmt.Fprintf(&sb, "-gone%d\n", i)
		fmt.Fprintf(&sb, "+here%d\n", i)
	}
	bigDiff := filepath.Join(dir, "bad.diff")
	require.NoError(t, os.WriteFile(bigDiff, []byte(sb.String()), 0o600))

	err := runGit(context.Background(), dir, "apply", "--whitespace=nowarn", "--", bigDiff)
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 2248,
		"the folded git output must be bounded (2 KiB cap + slack), got %d bytes", len(err.Error()))
}

func TestRunGit_ReportsAnErrorInsteadOfPanickingOnNoArgs(t *testing.T) {
	dir := t.TempDir()
	require.NotPanics(t, func() {
		err := runGit(context.Background(), dir)
		require.Error(t, err, "a call with no subcommand must error, not index out of range")
	})
}

// A base tree carrying its own .gitignore makes `git add -A` silently DROP the
// files it matches: they are copied into the working tree (what expected findings'
// line numbers index) but never enter the commit, so the base..head range and
// the working tree disagree. The materializer must refuse the metadata entries
// git's add/commit machinery consumes, not silently honor them.
func TestMaterializeCase_RejectsGitMetadataInBaseTree(t *testing.T) {
	c := materializableCase(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.Dir, "base", ".gitignore"), []byte("*.log\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(c.Dir, "base", "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(c.Dir, "base", "pkg", "debug.log"), []byte("x"), 0o600))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err, "a base tree with a .gitignore must be refused, not committed with files silently dropped")
	assert.Contains(t, err.Error(), ".gitignore")
}

// A base-tree ROOT that is itself a symlink must be refused with the symlink
// diagnostic. WalkDir Lstats the root, sees a non-directory, never descends, and
// the walk ends with zero files — which, if the root were classified only after
// the rel=="." early return, surfaces as the misleading "base tree is empty"
// error instead of naming the actual defect.
func TestMaterializeCase_RejectsASymlinkedBaseTreeRoot(t *testing.T) {
	c := materializableCase(t)
	real := filepath.Join(t.TempDir(), "real-base")
	require.NoError(t, os.MkdirAll(filepath.Join(real, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "pkg", "example.py"), []byte("one\n"), 0o600))
	// Point the case's base tree at the symlink; materializableCase wrote real
	// dirs, so replace the base dir with a link to one.
	require.NoError(t, os.RemoveAll(filepath.Join(c.Dir, "base")))
	require.NoError(t, os.Symlink(real, filepath.Join(c.Dir, "base")))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink", "the root must be classified as a symlink, not as an empty tree")
	assert.NotContains(t, err.Error(), "empty")
}

// copyBaseTree ignores the context it is transitively given, so a cancelled run
// still copies the ENTIRE base tree — bounded only by RAM and disk — before the
// failure finally surfaces at git init. A cancelled context must abort the copy
// itself, leaving dest untouched.
func TestMaterializeCase_ACanceledContextAbortsBeforeCopying(t *testing.T) {
	c := materializableCase(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dest := t.TempDir()

	_, err := MaterializeCase(ctx, c, dest)
	require.Error(t, err)
	entries, rdErr := os.ReadDir(dest)
	require.NoError(t, rdErr)
	assert.Empty(t, entries, "a cancelled context must abort the copy, not materialize the tree anyway")
}

// The executable bit is PART OF THE TREE OBJECT — a regression in the mode
// copy changes the commit SHA the determinism test pins, in a way that test
// cannot see (it compares two runs of the same broken code). The fixture ships
// an executable file and the materialized repository must carry mode 100755.
func TestMaterializeCase_PreservesTheExecutableBit(t *testing.T) {
	c := materializableCase(t)
	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	stage := gitOut(t, mc.Root, "ls-files", "--stage", "bin/run.sh")
	assert.Contains(t, stage, "100755", "the executable bit must survive into the git tree: got %q", stage)
}

// GIT_CONFIG_GLOBAL=/dev/null does NOT disable git's default excludes file,
// which is read from $XDG_CONFIG_HOME/git/ignore independently of any config
// (gitexec only appends the two env pins; the rest of the environment, XDG
// included, flows through). A host with a personal ignore file would silently
// drop matching base-tree files from the commit — making the tree, the range
// and both SHAs HOST-DEPENDENT, falsifying the fixed-SHA contract.
func TestMaterializeCase_IgnoresTheHostExcludeFile(t *testing.T) {
	xdg := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "git", "ignore"), []byte("*.log\n"), 0o600))
	t.Setenv("XDG_CONFIG_HOME", xdg)

	c := materializableCase(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.Dir, "base", "pkg", "debug.log"), []byte("noise"), 0o600))

	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	tree := gitOut(t, mc.Root, "ls-tree", "-r", "--name-only", "HEAD")
	assert.Contains(t, tree, "pkg/debug.log", "the committed tree must not depend on the host's ignore file")
}

func TestMaterializeCase_ProducesAReviewableRange(t *testing.T) {
	c := materializableCase(t)
	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	assert.NotEmpty(t, mc.BaseSHA)
	assert.NotEmpty(t, mc.HeadSHA)
	assert.NotEqual(t, mc.BaseSHA, mc.HeadSHA, "base and head must be distinct commits")

	diff := gitOut(t, mc.Root, "diff", "--name-only", mc.BaseSHA, mc.HeadSHA)
	assert.Equal(t, "pkg/example.py\n", diff, "base..head must be exactly the case's change")
}

// The diff is the only thing that changes the tree, so the head working tree must
// show the applied change and the base commit must not.
func TestMaterializeCase_AppliesTheDiff(t *testing.T) {
	c := materializableCase(t)
	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	head, err := os.ReadFile(filepath.Join(mc.Root, "pkg", "example.py"))
	require.NoError(t, err)
	assert.Equal(t, "one\nTWO\nthree\n", string(head), "the working tree is the HEAD state")

	base := gitOut(t, mc.Root, "show", mc.BaseSHA+":pkg/example.py")
	assert.Equal(t, "one\ntwo\nthree\n", base, "the base commit is the pre-change tree")
}

// commit-message.txt is the case's CLAIM SOURCE — the thing the claim ledger reads
// to decide whether the change delivers what it says. If materialization reformats
// it, the tier measures a message no author wrote.
func TestMaterializeCase_CommitMessageIsVerbatim(t *testing.T) {
	c := materializableCase(t)
	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Join(c.Dir, c.CommitMessage))
	require.NoError(t, err)
	// %B is the raw body; --format adds one terminating newline per commit, so
	// exactly one is stripped to recover the stored bytes. Stripping ALL trailing
	// newlines would hide the defect this test exists for — git's default
	// --cleanup drops trailing blank lines, and a case whose message ends in one
	// would then compare equal to a message git had already rewritten.
	got := strings.TrimSuffix(gitOut(t, mc.Root, "log", "-1", "--format=%B", mc.HeadSHA), "\n")
	assert.Equal(t, string(want), got, "the head commit message must be the case file, byte for byte")
}

// Two materializations of the same case must produce the same SHAs. The suite's
// whole value is comparing runs, and a wall-clock commit date would make every
// materialization a different repository.
func TestMaterializeCase_IsDeterministic(t *testing.T) {
	c := materializableCase(t)
	first, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)
	second, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, first.BaseSHA, second.BaseSHA)
	assert.Equal(t, first.HeadSHA, second.HeadSHA)
}

// AC7 at the materializing layer. The loader already refuses a symlinked
// base_tree, but MaterializeCase copies the tree's CONTENTS, and a symlink nested
// inside a legitimate base/ is a second escape the loader's top-level check never
// looks at.
func TestMaterializeCase_RejectsSymlinkInsideBaseTree(t *testing.T) {
	c := materializableCase(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(c.Dir, "base", "link.txt")))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

// A nested .git would make the materialized repository depend on whatever history
// an author happened to copy in. The loader checks the top level; the copy walks
// every directory, so it checks every level.
func TestMaterializeCase_RejectsNestedGitDir(t *testing.T) {
	c := materializableCase(t)
	require.NoError(t, os.MkdirAll(filepath.Join(c.Dir, "base", "vendor", ".git"), 0o755))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".git")
}

// A diff that does not apply must fail LOUDLY here. Left to run time the case
// would review an unchanged tree and score every reviewer zero — indistinguishable
// from a genuinely hard case, which is the failure mode this tier exists to detect.
func TestMaterializeCase_RejectsADiffThatDoesNotApply(t *testing.T) {
	c := materializableCase(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.Dir, "change.diff"), []byte(
		"diff --git a/pkg/absent.py b/pkg/absent.py\n"+
			"--- a/pkg/absent.py\n"+
			"+++ b/pkg/absent.py\n"+
			"@@ -1,1 +1,1 @@\n"+
			"-gone\n"+
			"+here\n"), 0o600))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply")
}

// AC7 by the third route: the DIFF is author-controlled too, and a patch whose
// paths climb out of the repository would write wherever it liked. The guard is
// git apply's own refusal, so this test pins that we rely on it deliberately
// rather than by luck — if a future git relaxed it, this fails rather than
// silently granting a case write access to the host.
func TestMaterializeCase_RejectsADiffThatEscapesTheTree(t *testing.T) {
	c := materializableCase(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.Dir, "change.diff"), []byte(
		"diff --git a/../escape.txt b/../escape.txt\n"+
			"new file mode 100644\n"+
			"--- /dev/null\n"+
			"+++ b/../escape.txt\n"+
			"@@ -0,0 +1 @@\n"+
			"+owned\n"), 0o600))

	dest := t.TempDir()
	_, err := MaterializeCase(context.Background(), c, dest)
	require.Error(t, err, "a diff writing outside the repository must be refused")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "escape.txt"))
}

// An empty base tree makes `git commit` fail with git's own "nothing to commit",
// which tells an author nothing about their case. It is also always a broken case:
// this tier exists to plant defects reachable through UNCHANGED code, and an empty
// base tree has none.
func TestMaterializeCase_RejectsAnEmptyBaseTree(t *testing.T) {
	c := materializableCase(t)
	require.NoError(t, os.RemoveAll(filepath.Join(c.Dir, "base")))
	require.NoError(t, os.MkdirAll(filepath.Join(c.Dir, "base"), 0o755))

	_, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// The SHIPPED case must materialize. A fixture-only suite would stay green while
// benchmarks/repo-state-v1/ drifted out of applying.
func TestMaterializeCase_ShippedCaseMaterializes(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			mc, err := MaterializeCase(context.Background(), c, t.TempDir())
			require.NoError(t, err, "shipped case %q must materialize", c.ID)
			for _, f := range c.ExpectedFindings {
				// Every expected finding names a file that must exist in the HEAD state.
				// A case citing a file the change deleted, or never created, is
				// unwinnable — the exact silent failure the tier exists to detect.
				headPath := filepath.Join(mc.Root, filepath.FromSlash(f.File))
				_, err := os.Stat(headPath)
				assert.NoError(t, err, "expected finding %q cites %q, which must exist in the head state", f.ID, f.File)
				// And the cited LINE RANGE must exist in that head file: expected
				// findings' line numbers are head-state, so a range beyond the file's
				// actual length (or a non-1-based start) makes the case unwinnable —
				// every reviewer scores zero and it reads as merely hard. Findings'
				// line numbers are head-state, so the count comes from the
				// MATERIALIZED file, not the base tree.
				head, rerr := os.ReadFile(headPath)
				if rerr != nil {
					continue // already reported by the Stat assertion above
				}
				lineCount := len(strings.Split(string(head), "\n"))
				// A trailing newline yields a final empty split element; a file that
				// ends in a newline has no line there, so drop it.
				if lineCount > 0 && strings.HasSuffix(string(head), "\n") {
					lineCount--
				}
				assert.GreaterOrEqual(t, f.LineStart, 1,
					"case %q finding %q cites %s line_start %d; head-state lines are 1-based", c.ID, f.ID, f.File, f.LineStart)
				assert.LessOrEqual(t, f.LineEnd, lineCount,
					"case %q finding %q cites %s line_end %d but the materialized head file has only %d line(s); the case is unwinnable",
					c.ID, f.ID, f.File, f.LineEnd, lineCount)
			}
		})
	}
}
