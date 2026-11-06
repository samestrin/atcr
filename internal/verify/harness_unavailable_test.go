package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A verify whose harness cannot be built reports how many findings it sent to
// a skeptic and could not check, and why, so the CLI can fail the run instead
// of printing a normal summary (EST-5102).
func TestRunVerify_HarnessUnavailableIsReported(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
		{Severity: "HIGH", File: "b.go", Line: 2, Problem: "bang", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return nil, nil, nil, ErrNoManifestHead
	})
	require.NoError(t, err)
	assert.Equal(t, 2, res.HarnessUnavailable)
	assert.Contains(t, res.HarnessReason, "no head commit")
}

// Any other harness failure gets a generic reason: the error detail can carry a
// path, which the stage keeps out of every output.
func TestRunVerify_HarnessUnavailableReasonIsGeneric(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return nil, nil, nil, errors.New("snapshot failed: /secret/repo/path")
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.HarnessUnavailable)
	assert.NotEmpty(t, res.HarnessReason)
	assert.NotContains(t, res.HarnessReason, "/secret/repo/path")
}

// A working harness reports nothing.
func TestRunVerify_HarnessAvailableReportsNothing(t *testing.T) {
	dir := pipelineReview(t, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
	})
	res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, scriptedHarness(`{"verdict":"confirmed","reasoning":"checked the line"}`))
	require.NoError(t, err)
	assert.Zero(t, res.HarnessUnavailable)
	assert.Empty(t, res.HarnessReason)
}

// buildDispatcher names a headless manifest with ErrNoManifestHead.
func TestBuildDispatcher_HeadlessManifestIsErrNoManifestHead(t *testing.T) {
	repo := initGitRepo(t)
	rev := filepath.Join(repo, "review")
	require.NoError(t, os.MkdirAll(rev, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rev, manifestFile), []byte(`{"head":"","stages":["review"]}`), 0o644))
	_, _, err := buildDispatcher(repo, rev, nil, nil, 0)
	assert.True(t, errors.Is(err, ErrNoManifestHead), "got %v", err)
}

// The issue's scenario on the verify side: a --scope baseline review of a clean
// tree records HEAD, so verify's harness snapshots it and a skeptic's read_file
// sees the reviewed file's content.
func TestBuildDispatcher_ScopedBaselineReviewSnapshotsHead(t *testing.T) {
	repo := initGitRepo(t) // commits a.go ("package a")
	cfg := &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "http://x.invalid"}},
			Agents:    map[string]registry.AgentConfig{"greta": {Provider: "p", Model: "m", Persona: "greta"}},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
	out := filepath.Join(t.TempDir(), "review")
	prep, err := fanout.PrepareReviewFromRepo(context.Background(), cfg, fanout.ReviewRequest{
		Repo: repo, Root: repo, OutputDir: out, Dir: "a.go", Verify: true,
		Date: "2026-11-06", TimeSuffix: "120000", StartedAt: time.Unix(1000, 0).UTC(),
	})
	require.NoError(t, err)

	disp, cleanup, err := buildDispatcher(repo, prep.Dir, nil, nil, 0)
	require.NoError(t, err, "a clean baseline review gives verify a head to snapshot")
	defer cleanup()
	res, err := disp.Execute(context.Background(), "read_file", []byte(`{"path":"a.go"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "package a")
}
