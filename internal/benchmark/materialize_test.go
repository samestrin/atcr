package benchmark

import (
	"context"
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "pkg", "example.py"), []byte("one\ntwo\nthree\n"), 0o600))
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
	got := gitOut(t, mc.Root, "log", "-1", "--format=%B", mc.HeadSHA)
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

// The SHIPPED case must materialize. A fixture-only suite would stay green while
// benchmarks/repo-state-v1/ drifted out of applying.
func TestMaterializeCase_ShippedCaseMaterializes(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	for _, c := range m.Cases {
		t.Run(c.ID, func(t *testing.T) {
			mc, err := MaterializeCase(context.Background(), c, t.TempDir())
			require.NoError(t, err, "shipped case %q must materialize", c.ID)
			// Every expected finding names a file that must exist in the HEAD state.
			// A case citing a file the change deleted, or never created, is
			// unwinnable — the exact silent failure the tier exists to detect.
			for _, f := range c.ExpectedFindings {
				_, err := os.Stat(filepath.Join(mc.Root, filepath.FromSlash(f.File)))
				assert.NoError(t, err, "expected finding %q cites %q, which must exist in the head state", f.ID, f.File)
			}
		})
	}
}
