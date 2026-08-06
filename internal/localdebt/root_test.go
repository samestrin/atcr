package localdebt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
)

// writeReviewManifest writes a minimal manifest.json into reviewDir recording root.
// It writes the payload.Manifest struct rather than a hand-rolled JSON literal so
// the test binds to the real field tag: a rename of Manifest.Root's json tag must
// break this, not silently make every manifest-tier case fall through to CWD.
func writeReviewManifest(t *testing.T, reviewDir, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(reviewDir, 0o755))
	data, err := json.Marshal(payload.Manifest{Base: "main", Head: "HEAD", Root: root})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(reviewDir, "manifest.json"), data, 0o644))
}

// repoDir creates a directory carrying a repo-root marker and returns it.
func repoDir(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, marker), 0o755))
	return dir
}

// TestResolveStoreRoot covers AC7(c)/(d)/(e): the ordered explicit > manifest > CWD
// precedence, re-validation of the recorded root, and the no-fall-through rule. One
// case per branch.
//
// The negative cases assert ok == false, which is the signal the caller uses to skip
// persistence entirely. A test asserting only the warning text would pass against a
// resolver that warned and then returned a CWD fallback — the exact
// persist-to-the-wrong-place failure this design forbids — so every failure case
// pins the boolean.
func TestResolveStoreRoot(t *testing.T) {
	t.Run("explicit valid root wins", func(t *testing.T) {
		explicit := t.TempDir()
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, repoDir(t, ".git"))

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{Explicit: explicit, ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.True(t, ok)
		assert.Equal(t, explicit, root, "the explicit tier outranks a perfectly valid manifest root")
		assert.Empty(t, diag.String())
	})

	t.Run("explicit root needs no repo marker", func(t *testing.T) {
		// The caller asserted this root deliberately (and the CLI already validated
		// its existence), so requiring .git/.atcr here would refuse a legitimate root
		// that does not hold a store yet.
		explicit := t.TempDir()

		root, ok := ResolveStoreRoot(RootOpts{Explicit: explicit, AllowCWD: false})

		require.True(t, ok)
		assert.Equal(t, explicit, root)
	})

	t.Run("explicit nonexistent root does not consult the manifest", func(t *testing.T) {
		manifestRoot := repoDir(t, ".git")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)
		missing := filepath.Join(t.TempDir(), "gone")

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{Explicit: missing, ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "a named-but-invalid root is a stop signal, never a hint to try the next tier")
		assert.Empty(t, root)
		assert.NotEqual(t, manifestRoot, root, "the manifest tier must not rescue a bad explicit root")
		assert.Contains(t, diag.String(), "does not exist or is not a directory")
	})

	t.Run("explicit root that is a file is rejected", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "not-a-dir")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{Explicit: f, AllowCWD: true, Diag: &diag})

		assert.False(t, ok)
		assert.Contains(t, diag.String(), "is not a directory")
	})

	t.Run("manifest root with a .git directory is used", func(t *testing.T) {
		manifestRoot := repoDir(t, ".git")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.True(t, ok)
		assert.Equal(t, manifestRoot, root)
		assert.Empty(t, diag.String())
	})

	t.Run("manifest root with ONLY an .atcr directory is rejected", func(t *testing.T) {
		// This case asserted ok == true until TD internal/localdebt/root.go:133. The
		// manifest tier's marker union let an artifacts-only .atcr/ directory
		// self-validate: cli/serve.go hardcodes the MCP engine root to ".", the review
		// path stamps Manifest.Root = absRoot(req.Root), and that directory
		// NECESSARILY contains .atcr/ because atcr just wrote the review into it — so
		// tier 2 re-validated the server's own CWD, the one directory AllowCWD:false
		// exists to refuse. A machine-recorded root must now carry .git specifically.
		manifestRoot := repoDir(t, ".atcr")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "an .atcr-only directory is an artifacts tree, not a repository root")
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "no longer a valid repository root")
	})

	t.Run("the CWD tier still accepts an .atcr marker", func(t *testing.T) {
		// The stricter rule is scoped to the MACHINE-RECORDED tier. The CWD walk and
		// the store's readers (cli/debt.go debtRepoRoot) keep the marker union, or a
		// repo whose only marker is .atcr would split writer from reader again.
		root := repoDir(t, ".atcr")
		t.Chdir(root)

		got, ok := ResolveStoreRoot(RootOpts{AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, resolved(t, root), resolved(t, got))
	})

	t.Run("manifest root with a .git FILE is used (linked worktree)", func(t *testing.T) {
		// A linked worktree and a submodule record their root with a .git FILE, not a
		// directory. Lstat+IsDir would reject both — precisely the setups where a
		// developer is most likely to be reconciling from somewhere other than the
		// main checkout, i.e. where the recorded root matters most.
		manifestRoot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(manifestRoot, ".git"), []byte("gitdir: /elsewhere\n"), 0o600))
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)

		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, manifestRoot, root)
	})

	t.Run("manifest root with a .atcr FILE is rejected", func(t *testing.T) {
		// The .git-as-file allowance is deliberate (worktrees, submodules); extending
		// it to .atcr is not. atcr only ever creates .atcr as a directory, so a stray
		// .atcr file would let an arbitrary directory pass as a repository root —
		// weakening precisely the re-validation the stale-claim design rests on.
		notARepo := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(notARepo, ".atcr"), []byte("junk"), 0o600))
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, notARepo)

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "an .atcr FILE is not a repo-root marker")
		assert.Contains(t, diag.String(), "no longer a valid repository root")
	})

	t.Run("manifest root with a .git SYMLINK is rejected", func(t *testing.T) {
		// TD: the writer used os.Stat (which follows the link) while the CLI's reader
		// walk (cli/debt.go debtRepoRoot) used os.Lstat, so a symlinked .git validated
		// for the writer and was invisible to the reader — the two halves of one store
		// disagreeing on where the repo root is. A link pointing at an arbitrary
		// directory must not pass as a repository root on either side; git never
		// creates one.
		linked := t.TempDir()
		require.NoError(t, os.Symlink(repoDir(t, ".git"), filepath.Join(linked, ".git")))
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, linked)

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "a .git SYMLINK is not a repo-root marker for the writer either")
		assert.Contains(t, diag.String(), "no longer a valid repository root")
	})

	t.Run("stale manifest root does not fall through to CWD", func(t *testing.T) {
		// The copied-artifacts case: the tree carries an absolute path from another
		// machine. AllowCWD is true, so a fall-through would silently succeed here —
		// which is why this case asserts ok == false and not merely a warning.
		gone := filepath.Join(t.TempDir(), "was-a-repo")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, gone)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok)
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "no longer a valid repository root")
	})

	t.Run("manifest root without a repo marker is rejected", func(t *testing.T) {
		// Exists and is a directory, but is not a repo — the relocated-tree case
		// where the old absolute path happens to resolve to something unrelated.
		notARepo := t.TempDir()
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, notARepo)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok)
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "no longer a valid repository root")
	})

	t.Run("manifest root that is a file is rejected", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "root-file")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, f)

		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true})

		assert.False(t, ok)
	})

	t.Run("empty manifest root falls through to CWD", func(t *testing.T) {
		// A MISSING claim is not a stale claim: a pre-field manifest asserted
		// nothing, so the pre-field CWD behavior is exactly right.
		//
		// Chdir into a marker-less dir to pin THAT behavior specifically: tier 3 now
		// walks up for a repo-root marker (TestResolveStoreRoot_CWDTierWalksToRepoRoot),
		// so without this the assertion would be measuring the atcr checkout the test
		// binary happens to run in rather than the fall-through itself.
		t.Chdir(t.TempDir())
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, "")

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.True(t, ok)
		assert.Equal(t, ".", root, "byte-for-byte the pre-manifest DefaultDir(\".\") behavior")
		assert.Empty(t, diag.String())
	})

	t.Run("missing manifest falls through to CWD", func(t *testing.T) {
		t.Chdir(t.TempDir()) // marker-less, as above
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: filepath.Join(t.TempDir(), "no-such-review"), AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, ".", root)
	})

	t.Run("corrupt manifest is an invalid claim: warn and stop", func(t *testing.T) {
		// A manifest that exists but cannot be parsed DID assert something — its
		// invalidity is merely unreadable. Treating it as no claim would silently
		// persist into the CWD, the exact wrong-store write the recorded-root
		// design exists to prevent (TD internal/localdebt/root.go:72).
		review := filepath.Join(t.TempDir(), "r")
		require.NoError(t, os.MkdirAll(review, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(review, "manifest.json"), []byte("{not json"), 0o644))

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "an unreadable manifest is an invalid claim, not a missing one")
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "manifest", "the warning must name the unreadable manifest")
	})

	t.Run("no root and AllowCWD false is a no-persist", func(t *testing.T) {
		// The MCP entry point: its CWD is whatever launched the server, so there is
		// no defensible fallback and the correct answer is to write nothing.
		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{ReviewDir: filepath.Join(t.TempDir(), "none"), AllowCWD: false, Diag: &diag})

		assert.False(t, ok)
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "no repo root recorded in the review manifest")
		assert.Contains(t, diag.String(), "pass repo=<path>", "the warning names the operator's remedy")
	})

	t.Run("whitespace-only explicit root is an invalid claim, not an absent one", func(t *testing.T) {
		// `--repo "$REPO "` with REPO unset produces "   ": the operator NAMED a
		// root, and the resolver acting as if they named nothing is the one
		// silent transition the no-fall-through contract forbids (TD
		// internal/localdebt/root.go:59). Only a genuinely absent Explicit ("")
		// falls through — covered by the manifest-tier cases above.
		manifestRoot := repoDir(t, ".git")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{Explicit: "   ", ReviewDir: review, AllowCWD: true, Diag: &diag})

		assert.False(t, ok, "a supplied-but-blank root is an invalid claim: stop, never fall through")
		assert.Empty(t, root)
		assert.NotEqual(t, manifestRoot, root, "the manifest tier must not rescue a blank explicit root")
		assert.Contains(t, diag.String(), "blank")
	})

	t.Run("nil diag writer does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ResolveStoreRoot(RootOpts{Explicit: filepath.Join(t.TempDir(), "gone")})
		}, "every warning path must tolerate an unset sink; persistence is best-effort")
	})
}

// TestResolveStoreRoot_CWDTierWalksToRepoRoot pins the writer half of the ONE
// resolver the store has (TD internal/localdebt/root.go:89). Tier 3 used to return
// the literal ".", so DefaultDir(".") resolved against the process CWD with no
// repo-root walk — while the reader half (cli/debt.go debtStoreDir → debtRepoRoot)
// DID walk up for a .git/.atcr marker. `atcr reconcile` from a subdirectory wrote
// <cwd>/.atcr/debt while `atcr debt list` from that same cwd read
// <repo-root>/.atcr/debt, so the whole run's backlog was invisible — and the stray
// <cwd>/.atcr it created became a repo-root marker for every later walk.
//
// Both halves now share ONE marker walk (FindRepoRoot), so there is no third copy
// of the marker logic to drift.
func TestResolveStoreRoot_CWDTierWalksToRepoRoot(t *testing.T) {
	t.Run("resolves the repo root when run from a subdirectory", func(t *testing.T) {
		root := repoDir(t, ".git")
		sub := filepath.Join(root, "internal", "deep")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		t.Chdir(sub)

		got, ok := ResolveStoreRoot(RootOpts{AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, resolved(t, root), resolved(t, got),
			"the writer must land in the same store the reader walk finds")
	})

	t.Run("an .atcr directory is a marker for the walk too", func(t *testing.T) {
		root := repoDir(t, ".atcr")
		sub := filepath.Join(root, "pkg")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		t.Chdir(sub)

		got, ok := ResolveStoreRoot(RootOpts{AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, resolved(t, root), resolved(t, got))
	})

	t.Run("no marker anywhere keeps the byte-for-byte CWD behavior", func(t *testing.T) {
		// The suite's isolate() helper chdirs into a bare temp dir with no marker;
		// that case must still resolve exactly as it did before the walk existed.
		t.Chdir(t.TempDir())

		got, ok := ResolveStoreRoot(RootOpts{AllowCWD: true})

		require.True(t, ok)
		assert.Equal(t, ".", got)
	})

	t.Run("AllowCWD false still refuses to guess", func(t *testing.T) {
		root := repoDir(t, ".git")
		t.Chdir(root)

		var diag bytes.Buffer
		got, ok := ResolveStoreRoot(RootOpts{AllowCWD: false, Diag: &diag})

		assert.False(t, ok, "a marker under the MCP server's CWD is not a claim about the reviewed repo")
		assert.Empty(t, got)
	})
}

// resolved evaluates symlinks so a comparison never fails on /var vs /private/var,
// which says nothing about which directory was picked.
func resolved(t *testing.T, path string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return out
}

// TestResolveStoreRoot_RedactsRecordedPath pins the package SECURITY contract on the
// MANIFEST tier (TD internal/localdebt/root.go:77). The recorded root did not come
// from the caller — it was written into an artifact file, possibly on another machine
// — so echoing it verbatim puts an absolute, username-bearing path (/Users/<name>/…)
// into a sink the MCP path routes to bare os.Stderr, which a calling agent captures.
// withLock already reduces its lock path to the base name for exactly this reason
// (lock.go:69-72); these warnings must do the same.
func TestResolveStoreRoot_RedactsRecordedPath(t *testing.T) {
	t.Run("stale recorded root is reduced to its base name", func(t *testing.T) {
		parent := t.TempDir()
		gone := filepath.Join(parent, "was-a-repo")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, gone)

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.False(t, ok)
		assert.Contains(t, diag.String(), "was-a-repo", "the base name still identifies which root went stale")
		assert.NotContains(t, diag.String(), parent, "the absolute parent path must never reach the sink")
	})

	t.Run("unmarked recorded root is reduced to its base name", func(t *testing.T) {
		parent := t.TempDir()
		notARepo := filepath.Join(parent, "not-a-repo")
		require.NoError(t, os.Mkdir(notARepo, 0o755))
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, notARepo)

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.False(t, ok)
		assert.NotContains(t, diag.String(), parent)
	})

	t.Run("unreadable manifest warning carries no absolute path", func(t *testing.T) {
		// The manifest error is an artifact-derived path too: os.Stat's *os.PathError
		// and ReadManifest's wrapped error both embed the full review-dir path.
		parent := t.TempDir()
		review := filepath.Join(parent, "review-artifacts")
		require.NoError(t, os.MkdirAll(review, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(review, "manifest.json"), []byte("{not json"), 0o644))

		var diag bytes.Buffer
		_, ok := ResolveStoreRoot(RootOpts{ReviewDir: review, AllowCWD: true, Diag: &diag})

		require.False(t, ok)
		assert.NotContains(t, diag.String(), parent, "the absolute review-dir path must never reach the sink")
	})
}

// TestResolveStoreRoot_RequireMarker pins the explicit tier's tightening knob
// (TD internal/localdebt/root.go:49): for an entry point whose root is
// model-supplied (MCP), a named-but-unmarked existing directory must be a
// no-persist-with-warning — never a fall-through — while a marked directory
// resolves exactly as before.
func TestResolveStoreRoot_RequireMarker(t *testing.T) {
	t.Run("markerless explicit root is rejected and does not fall through", func(t *testing.T) {
		bare := t.TempDir() // exists, but carries no .git/.atcr marker
		manifestRoot := repoDir(t, ".git")
		review := filepath.Join(t.TempDir(), "r")
		writeReviewManifest(t, review, manifestRoot)

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{Explicit: bare, ReviewDir: review, AllowCWD: true, RequireMarker: true, Diag: &diag})

		assert.False(t, ok, "a model-supplied root with no marker is a stop signal, like any invalid claim")
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "no repository marker")
	})

	t.Run("marked explicit root resolves", func(t *testing.T) {
		marked := repoDir(t, ".git")

		root, ok := ResolveStoreRoot(RootOpts{Explicit: marked, AllowCWD: false, RequireMarker: true})

		require.True(t, ok)
		assert.Equal(t, marked, root)
	})

	t.Run("symlinked explicit root is rejected", func(t *testing.T) {
		// Symmetry with the marker checks, which already use Lstat so a symlinked
		// .git never vouches for a directory (TD internal/mcp/tools.go:79, brad).
		// The root itself was the remaining gap: existingDir stats through the
		// link, so a model-supplied symlink resolved to whatever it pointed at,
		// and a link repointed afterwards would silently move the store.
		real := repoDir(t, ".git")
		link := filepath.Join(t.TempDir(), "repo-link")
		require.NoError(t, os.Symlink(real, link))

		var diag bytes.Buffer
		root, ok := ResolveStoreRoot(RootOpts{Explicit: link, AllowCWD: false, RequireMarker: true, Diag: &diag})

		assert.False(t, ok, "a model-supplied symlinked root is a stop signal, not a redirect")
		assert.Empty(t, root)
		assert.Contains(t, diag.String(), "symlink")
	})

	t.Run("symlinked explicit root still resolves without the knob", func(t *testing.T) {
		// The CLI keeps its permissive tier: an operator who types a path through
		// a symlinked checkout meant that checkout.
		real := repoDir(t, ".git")
		link := filepath.Join(t.TempDir(), "repo-link")
		require.NoError(t, os.Symlink(real, link))

		_, ok := ResolveStoreRoot(RootOpts{Explicit: link, AllowCWD: false})

		assert.True(t, ok, "the operator-typed tier is unchanged by the model-supplied tightening")
	})

	t.Run("markerless explicit root still resolves without the knob", func(t *testing.T) {
		// The CLI stays permissive: its explicit value is operator-typed and has
		// already been through normalizeRepoFlag.
		bare := t.TempDir()

		root, ok := ResolveStoreRoot(RootOpts{Explicit: bare, AllowCWD: false})

		require.True(t, ok)
		assert.Equal(t, bare, root)
	})
}
