package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHomeDir overrides the homeUserDir seam for the duration of a test so
// ~-relativization is deterministic regardless of the runner's real home.
func stubHomeDir(t *testing.T, dir string) {
	t.Helper()
	orig := homeUserDir
	t.Cleanup(func() { homeUserDir = orig })
	homeUserDir = func() (string, error) { return dir, nil }
}

// TestRelHome covers the ~-relativization idiom (T2): a path under home renders
// with a ~ prefix, the home dir itself renders as ~, and a path outside home
// falls back to the verbatim absolute path (filepath.Rel-plus-fallback). The
// "~" form is pinned as literal forward slashes on every platform — the AXI
// payload's cross-platform contract.
func TestRelHome(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")
	stubHomeDir(t, home)

	assert.Equal(t, "~/go/bin/atcr",
		relHome(filepath.Join(home, "go", "bin", "atcr")),
		"a path under home renders with a ~/ prefix, forward-slash on every platform")
	assert.Equal(t, "~", relHome(home), "the home dir itself renders as ~")

	outside := filepath.FromSlash("/usr/local/bin/atcr")
	assert.Equal(t, outside, relHome(outside),
		"a path outside home falls back to the verbatim absolute path")
}

// TestRelHome_NoHomeDir covers the fallback when the home directory cannot be
// resolved: the path is returned verbatim, never a broken ~ substitution.
func TestRelHome_NoHomeDir(t *testing.T) {
	orig := homeUserDir
	t.Cleanup(func() { homeUserDir = orig })
	homeUserDir = func() (string, error) { return "", errors.New("no home") }

	p := filepath.FromSlash("/usr/local/bin/atcr")
	assert.Equal(t, p, relHome(p), "when home can't be resolved, path is returned verbatim")
}

// TestRelHome_EmptyHomeDir pins the other unresolvable-home shape: homeUserDir
// succeeds but returns an empty string — relHome must treat that like the error
// case and return the path verbatim, never a broken "~/..." substitution.
func TestRelHome_EmptyHomeDir(t *testing.T) {
	stubHomeDir(t, "")

	p := filepath.FromSlash("/usr/local/bin/atcr")
	assert.Equal(t, p, relHome(p), "an empty home dir is unresolvable: path is returned verbatim")
}

// TestRenderHomeView_HasReview covers the happy path: exec path (~-relativized),
// one-line description, and the current review's id/status.
func TestRenderHomeView_HasReview(t *testing.T) {
	stubHomeDir(t, filepath.FromSlash("/home/testuser"))
	var buf bytes.Buffer
	require.NoError(t, renderHomeView(&buf,
		filepath.FromSlash("/home/testuser/go/bin/atcr"),
		"Agent Team Code Review — a review panel, not a reviewer",
		homeState{hasReview: true, reviewID: "2026-06-10_x", status: "completed"}))

	got := buf.String()
	assert.Contains(t, got, "~/", "exec path is ~-relativized, forward-slash on every platform")
	assert.Contains(t, got, "Agent Team Code Review — a review panel, not a reviewer")
	assert.Contains(t, got, "2026-06-10_x", "the current review id is shown")
	assert.Contains(t, got, "completed", "the current review status is shown")
}

// TestRenderHomeView_NoReview covers AC3: the explicit no-reviews-yet state with
// a run-`atcr review` hint and no stale "Latest review" line.
func TestRenderHomeView_NoReview(t *testing.T) {
	stubHomeDir(t, filepath.FromSlash("/home/testuser"))
	var buf bytes.Buffer
	require.NoError(t, renderHomeView(&buf,
		filepath.FromSlash("/home/testuser/go/bin/atcr"),
		"Agent Team Code Review — a review panel, not a reviewer",
		homeState{hasReview: false}))

	got := buf.String()
	assert.Contains(t, got, "No reviews yet", "no-review state is explicit")
	assert.Contains(t, got, "atcr review", "no-review state hints the review command")
	assert.NotContains(t, got, "Latest review:", "no stale review line in the no-review state")
}

// TestResolveHomeState_NoReviews covers AC3's resolution: a repo with no
// .atcr/latest (os.ErrNotExist) yields the no-review first-run state, never an
// error and never the "unavailable" degrade state.
func TestResolveHomeState_NoReviews(t *testing.T) {
	t.Chdir(t.TempDir())
	st := resolveHomeState(context.Background())
	assert.False(t, st.hasReview, "a repo with no .atcr/latest yields the no-review state")
	assert.False(t, st.unavailable, "a truly-absent pointer is first-run, not unavailable")
}

// TestResolveHomeState_HasReview covers the live-state read: resolveHomeState
// resolves .atcr/latest via anchorDir and reads id/status via ReadReviewStatus.
func TestResolveHomeState_HasReview(t *testing.T) {
	root := t.TempDir()
	writeStatusFixture(t, root, "2026-06-10_x")
	t.Chdir(root)

	st := resolveHomeState(context.Background())
	require.True(t, st.hasReview)
	assert.Equal(t, "2026-06-10_x", st.reviewID)
	assert.Equal(t, "completed", st.status)
}

// TestResolveHomeState_StalePointer covers the degrade path flagged by
// independent review: a .atcr/latest pointer that names a review whose directory
// can't be read (pruned/corrupt) is the honest "unavailable" state carrying the
// known id — NOT silently masked as first-run.
func TestResolveHomeState_StalePointer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte("2026-06-10_gone\n"), 0o644))
	t.Chdir(root)

	st := resolveHomeState(context.Background())
	assert.False(t, st.hasReview, "a pointer to an unreadable review is not a readable review")
	assert.True(t, st.unavailable, "a stale/unreadable pointer is the unavailable state, not first-run")
	assert.Equal(t, "2026-06-10_gone", st.reviewID, "the known id is preserved for an honest message")
}

// TestResolveHomeState_CorruptPointer covers the other degrade cause: an empty
// (or malformed) .atcr/latest is a non-ErrNotExist anchor error, reported as
// unavailable with no id — distinct from the true first-run state.
func TestResolveHomeState_CorruptPointer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte("   \n"), 0o644))
	t.Chdir(root)

	st := resolveHomeState(context.Background())
	assert.False(t, st.hasReview)
	assert.True(t, st.unavailable, "an empty/corrupt pointer is unavailable, not first-run")
	assert.Empty(t, st.reviewID, "no usable id from a corrupt pointer")
}

// TestResolveHomeState_UnreadableAnchor covers the non-ErrNotExist anchorDir
// failure path directly: .atcr/latest exists but cannot be read as a file (here
// it is a directory, so os.ReadFile fails with EISDIR rather than ErrNotExist).
// The honest state is unavailable with no id — never the first-run guidance.
func TestResolveHomeState_UnreadableAnchor(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr", "latest"), 0o755))
	t.Chdir(root)

	st := resolveHomeState(context.Background())
	assert.False(t, st.hasReview)
	assert.True(t, st.unavailable, "an unreadable .atcr/latest (non-ErrNotExist anchor error) is unavailable, not first-run")
	assert.Empty(t, st.reviewID, "no usable id from an unreadable anchor")
}

// TestResolveHomeState_DanglingPointer covers the broken-pointer honesty gap:
// .atcr/latest as a symlink whose target is gone makes os.ReadFile report
// ErrNotExist (it follows the link), yet the pointer itself exists — the honest
// state is unavailable, NOT the misleading "No reviews yet" first-run line.
func TestResolveHomeState_DanglingPointer(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, ".atcr", "reviews", "2026-06-10_gone"),
		filepath.Join(root, ".atcr", "latest")))
	t.Chdir(root)

	st := resolveHomeState(context.Background())
	assert.False(t, st.hasReview, "a dangling pointer is not a readable review")
	assert.True(t, st.unavailable, "a dangling .atcr/latest symlink is a present-but-broken pointer: unavailable, not first-run")
	assert.Empty(t, st.reviewID, "no usable id from a dangling pointer")
}

// TestRenderHomeView_Unavailable covers the honest degrade rendering for both the
// known-id (stale pointer) and unknown-id (corrupt pointer) cases — never the
// misleading "No reviews yet" line.
func TestRenderHomeView_Unavailable(t *testing.T) {
	stubHomeDir(t, filepath.FromSlash("/home/testuser"))
	const desc = "Agent Team Code Review — a review panel, not a reviewer"

	var withID bytes.Buffer
	require.NoError(t, renderHomeView(&withID, filepath.FromSlash("/home/testuser/go/bin/atcr"), desc,
		homeState{unavailable: true, reviewID: "2026-06-10_gone"}))
	assert.Contains(t, withID.String(), "2026-06-10_gone")
	assert.Contains(t, withID.String(), "unavailable")
	assert.NotContains(t, withID.String(), "No reviews yet")

	var noID bytes.Buffer
	require.NoError(t, renderHomeView(&noID, filepath.FromSlash("/home/testuser/go/bin/atcr"), desc,
		homeState{unavailable: true}))
	assert.Contains(t, noID.String(),
		".atcr/latest pointer file is corrupted or unreadable — run `atcr review` to regenerate it.",
		"the empty-id unavailable message names the pointer file and the single remedy")
	assert.NotContains(t, noID.String(), "No reviews yet")
}

// pinArgs overrides os.Args for the duration of a test so the exec-path
// fallback chain is deterministic regardless of the test binary's own argv.
func pinArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

// TestRunHome_ExecutableFallback covers the LOW independent-review finding: when
// the homeExecutable seam errors, runHome still renders the home view rather
// than erroring — AC3's never-error guarantee holds even on exec-path
// resolution failure. os.Args is pinned so the fallback chain is deterministic:
// with Args[0] = "atcr" the rendered exec path is the invocation name.
func TestRunHome_ExecutableFallback(t *testing.T) {
	t.Chdir(t.TempDir()) // no .atcr/latest -> deterministic no-review path
	origExec := homeExecutable
	t.Cleanup(func() { homeExecutable = origExec })
	homeExecutable = func() (string, error) { return "", errors.New("exec resolution failed") }
	pinArgs(t, []string{"atcr"})

	root := newRootCmd()
	root.SetContext(context.Background()) // no axi in context -> text path
	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, runHome(root))
	got := buf.String()
	firstLine, _, _ := strings.Cut(got, "\n")
	assert.Equal(t, "atcr", firstLine, "the exec fallback renders the os.Args[0] invocation name")
	assert.Contains(t, got, "Agent Team Code Review — a review panel, not a reviewer")
}

// TestRunHome_ExecutableFallbackUsesArgsZero covers the renamed-binary case:
// when os.Executable fails, the fallback exec path is os.Args[0] — the name the
// binary was actually invoked as — not a hardcoded "atcr", which would render a
// misleading exec_path for a renamed or wrapped binary.
func TestRunHome_ExecutableFallbackUsesArgsZero(t *testing.T) {
	t.Chdir(t.TempDir()) // no .atcr/latest -> deterministic no-review path
	origExec := homeExecutable
	t.Cleanup(func() { homeExecutable = origExec })
	homeExecutable = func() (string, error) { return "", errors.New("exec resolution failed") }
	pinArgs(t, []string{"/usr/local/bin/renamed-atcr"})

	root := newRootCmd()
	root.SetContext(context.Background())
	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, runHome(root))
	firstLine, _, _ := strings.Cut(buf.String(), "\n")
	assert.Equal(t, "/usr/local/bin/renamed-atcr", firstLine,
		"the fallback exec path is os.Args[0], not a hardcoded binary name")
}

// TestRunHome_ExecutableFallbackEmptyArgs pins the last-ditch branch: with no
// argv at all (exotic embedding), the fallback is the command's own name —
// injected from cmd.Name(), not a hardcoded literal.
func TestRunHome_ExecutableFallbackEmptyArgs(t *testing.T) {
	t.Chdir(t.TempDir()) // no .atcr/latest -> deterministic no-review path
	origExec := homeExecutable
	t.Cleanup(func() { homeExecutable = origExec })
	homeExecutable = func() (string, error) { return "", errors.New("exec resolution failed") }
	pinArgs(t, []string{})

	root := newRootCmd()
	root.SetContext(context.Background())
	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, runHome(root))
	firstLine, _, _ := strings.Cut(buf.String(), "\n")
	assert.Equal(t, root.Name(), firstLine, "with empty argv the fallback is cmd.Name()")
}

// TestRootCmd_BareAXIRendersHomeViewPayload covers AC4/T3: bare `atcr --axi`
// renders the home view as the token-dense TOON payload through the shared AXI
// context plumbing (root local --axi flag -> PersistentPreRunE -> axiFromContext),
// exit 0 — not the human text and not help.
func TestRootCmd_BareAXIRendersHomeViewPayload(t *testing.T) {
	out, err := execute(t, "--axi")
	require.NoError(t, err)
	assert.Contains(t, out, "home[1", "bare atcr --axi emits the home-view TOON payload")
	assert.NotContains(t, out, "Usage:", "axi home view is not help text")
}

// TestRootCmd_BareNonAXIIsTextNotPayload pins that WITHOUT --axi the home view is
// human text, not the TOON payload — the same data, two renderers, one dispatch.
func TestRootCmd_BareNonAXIIsTextNotPayload(t *testing.T) {
	out, err := execute(t)
	require.NoError(t, err)
	assert.NotContains(t, out, "home[1", "without --axi the home view is human text, not the TOON payload")
}

// TestHomeView_GoldenNonAXI is the AC5 snapshot: it pins the exact non-axi
// home-view output byte-for-byte, for both the has-review and no-review states,
// with the executable path and home dir stubbed for determinism.
func TestHomeView_GoldenNonAXI(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")
	stubHomeDir(t, home)
	execPath := filepath.Join(home, "go", "bin", "atcr")
	wantExec := "~/go/bin/atcr" // pinned forward-slash: the cross-platform AXI contract
	const desc = "Agent Team Code Review — a review panel, not a reviewer"

	t.Run("has review", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, renderHomeView(&buf, execPath, desc,
			homeState{hasReview: true, reviewID: "2026-06-10_x", status: "completed"}))
		want := wantExec + "\n" + desc + "\nLatest review: 2026-06-10_x (completed)\n"
		assert.Equal(t, want, buf.String())
	})

	t.Run("no review", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, renderHomeView(&buf, execPath, desc, homeState{hasReview: false}))
		want := wantExec + "\n" + desc + "\nNo reviews yet — run `atcr review` to start one.\n"
		assert.Equal(t, want, buf.String())
	})
}
