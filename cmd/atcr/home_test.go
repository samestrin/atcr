package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
// falls back to the verbatim absolute path (filepath.Rel-plus-fallback).
func TestRelHome(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")
	stubHomeDir(t, home)

	assert.Equal(t, "~"+string(filepath.Separator)+filepath.FromSlash("go/bin/atcr"),
		relHome(filepath.Join(home, "go", "bin", "atcr")),
		"a path under home is rendered with a ~ prefix")
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
	assert.Contains(t, got, "~"+string(filepath.Separator), "exec path is ~-relativized")
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
	assert.Contains(t, noID.String(), "unreadable")
	assert.NotContains(t, noID.String(), "No reviews yet")
}

// TestRunHome_ExecutableFallback covers the LOW independent-review finding: when
// the homeExecutable seam errors, runHome still renders the home view (with the
// "atcr" fallback path) rather than erroring — AC3's never-error guarantee holds
// even on exec-path resolution failure.
func TestRunHome_ExecutableFallback(t *testing.T) {
	t.Chdir(t.TempDir()) // no .atcr/latest -> deterministic no-review path
	origExec := homeExecutable
	t.Cleanup(func() { homeExecutable = origExec })
	homeExecutable = func() (string, error) { return "", errors.New("exec resolution failed") }

	root := newRootCmd()
	root.SetContext(context.Background()) // no axi in context -> text path
	var buf bytes.Buffer
	root.SetOut(&buf)

	require.NoError(t, runHome(root))
	got := buf.String()
	assert.Contains(t, got, "atcr", "the exec fallback renders the fallback name")
	assert.Contains(t, got, "Agent Team Code Review — a review panel, not a reviewer")
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
	wantExec := "~" + string(filepath.Separator) + filepath.FromSlash("go/bin/atcr")
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
