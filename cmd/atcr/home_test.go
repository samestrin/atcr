package main

import (
	"bytes"
	"errors"
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
// .atcr/latest yields the no-review state (never an error).
func TestResolveHomeState_NoReviews(t *testing.T) {
	t.Chdir(t.TempDir())
	st := resolveHomeState()
	assert.False(t, st.hasReview, "a repo with no .atcr/latest yields the no-review state")
}

// TestResolveHomeState_HasReview covers the live-state read: resolveHomeState
// resolves .atcr/latest via anchorDir and reads id/status via ReadReviewStatus.
func TestResolveHomeState_HasReview(t *testing.T) {
	root := t.TempDir()
	writeStatusFixture(t, root, "2026-06-10_x")
	t.Chdir(root)

	st := resolveHomeState()
	require.True(t, st.hasReview)
	assert.Equal(t, "2026-06-10_x", st.reviewID)
	assert.Equal(t, "completed", st.status)
}
