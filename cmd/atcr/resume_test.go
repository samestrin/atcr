package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/require"
)

// scaffoldResumeReview creates a minimal review directory (id under
// .atcr/reviews/) with a sources/ tree so resolveResumeDir's completeness check
// passes. Returns the review dir path.
func scaffoldResumeReview(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(fanout.ReviewsRoot("."), id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources"), 0o755))
	return dir
}

func TestResolveResumeDir_LatestAndEmpty(t *testing.T) {
	isolate(t)
	dir := scaffoldResumeReview(t, "2026-06-18_demo")
	require.NoError(t, fanout.WriteLatest(".", "2026-06-18_demo"))

	// Both the literal "latest" and an empty anchor resolve the .atcr/latest pointer.
	for _, anchor := range []string{"latest", ""} {
		got, err := resolveResumeDir(anchor)
		require.NoError(t, err, "anchor %q", anchor)
		require.Equal(t, dir, got, "anchor %q", anchor)
	}
}

func TestResolveResumeDir_ByID(t *testing.T) {
	isolate(t)
	dir := scaffoldResumeReview(t, "2026-06-18_demo")
	got, err := resolveResumeDir("2026-06-18_demo")
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestResolveResumeDir_ExplicitPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources"), 0o755))
	got, err := resolveResumeDir(dir)
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestResolveResumeDir_MissingLatestErrors(t *testing.T) {
	isolate(t)
	_, err := resolveResumeDir("latest")
	require.Error(t, err)
}

func TestResolveResumeDir_UnknownIDErrors(t *testing.T) {
	isolate(t)
	_, err := resolveResumeDir("2026-06-18_nope")
	require.Error(t, err)
}
