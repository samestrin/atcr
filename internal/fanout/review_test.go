package fanout

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrF(f float64) *float64 { return &f }

// initRepo creates a temp git repo with a base and head commit that change a Go
// file, returning the dir and the two SHAs.
func initRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() { b() }\n\nfunc b() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// mockProvider returns an httptest server speaking the OpenAI chat-completions
// shape. It returns 500 for any model in failModels, else a findings payload.
func mockProvider(t *testing.T, failModels ...string) *httptest.Server {
	t.Helper()
	fail := map[string]bool{}
	for _, m := range failModels {
		fail[m] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		if fail[req.Model] {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		content := "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func twoAgentConfig(srvURL string) *ReviewConfig {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
			"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)},
		},
	}
	return &ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta", "kai"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{}, // empty → embedded personas
	}
}

func reviewReq(repo, root, base, head string) ReviewRequest {
	return ReviewRequest{
		Repo:       repo,
		Root:       root,
		Range:      ReviewRange{Base: base, Head: head, DetectionMode: "explicit", CommitCount: 1},
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

func TestRunReview_EndToEnd(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "2026-06-10_test", res.ID)
	assert.Equal(t, 2, res.Summary.Succeeded)
	assert.False(t, res.Summary.Partial)

	// Review directory layout.
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(res.Dir, sub))
	}
	assert.FileExists(t, filepath.Join(res.Dir, "payload", "blocks.txt"))

	// Per-agent findings with engine-set REVIEWER.
	for _, agent := range []string{"greta", "kai"} {
		fp := filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "findings.txt")
		data, err := os.ReadFile(fp)
		require.NoError(t, err, "missing %s findings", agent)
		parsed, err := stream.ParseSource(data)
		require.NoError(t, err)
		require.Len(t, parsed.Findings, 1)
		assert.Equal(t, agent, parsed.Findings[0].Reviewer)
		assert.Equal(t, "CRITICAL", parsed.Findings[0].Severity)
	}

	// Manifest provenance + latest pointer.
	mdata, err := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, base, m.Base)
	assert.Equal(t, head, m.Head)
	assert.Equal(t, "explicit", m.DetectionMode)
	assert.ElementsMatch(t, []string{"greta", "kai"}, m.Roster)
	assert.Equal(t, "blocks", m.PerAgentPayload["greta"])

	latest, err := ReadLatest(repo)
	require.NoError(t, err)
	assert.Equal(t, res.ID, latest)
}

func TestRunReview_PartialWhenOneAgentFails(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t, "m-kai") // kai's model 500s; retries exhaust → failed
	cfg := twoAgentConfig(srv.URL)
	// Shrink kai's retry budget so the test is fast.
	res, err := RunReview(context.Background(), llmclient.New(llmclient.WithRetry(0, 0, 1)), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err, "≥1 success means no run-level error")
	assert.Equal(t, 1, res.Summary.Succeeded)
	assert.Equal(t, 1, res.Summary.Failed)
	assert.True(t, res.Summary.Partial)

	// Manifest records the partial flag.
	mdata, _ := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.True(t, m.Partial)
}

func TestRunReview_AllFailReturnsErrorButPreservesArtifacts(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t, "m-greta", "m-kai") // both fail
	cfg := twoAgentConfig(srv.URL)
	res, err := RunReview(context.Background(), llmclient.New(llmclient.WithRetry(0, 0, 1)), cfg, reviewReq(repo, repo, base, head))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAllAgentsFailed)
	require.NotNil(t, res, "result preserved so the caller knows the review dir")
	// Artifacts still on disk for inspection (AC 03-02 Error Scenario 3).
	assert.FileExists(t, filepath.Join(res.Dir, "manifest.json"))
	assert.FileExists(t, filepath.Join(res.Dir, "sources", "pool", "summary.json"))
}

func TestRunReview_EmptyRosterShortCircuits(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := &ReviewConfig{
		Registry: &registry.Registry{},
		Project:  &registry.ProjectConfig{}, // no agents
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
	res, err := RunReview(context.Background(), newFake(), cfg, reviewReq(repo, repo, base, head))
	assert.Nil(t, res)
	assert.ErrorIs(t, err, ErrEmptyRoster)
	// No review dir or latest pointer was created.
	assert.NoDirExists(t, filepath.Join(repo, ".atcr", "reviews"))
}

func TestRunReview_UnknownProviderIsBuildError(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{}, // greta's provider missing
			Agents:    map[string]registry.AgentConfig{"greta": {Provider: "ghost", Model: "m", Persona: "greta"}},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
	_, err := RunReview(context.Background(), newFake(), cfg, reviewReq(repo, repo, base, head))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// AC 06-03: a configured byte budget must surface as Truncated=true with the
// dropped files recorded in each agent's status.json — never silent. This is
// the integration proof that the payload_byte_budget knob actually feeds
// buildPayloads (the budget machinery was previously wired to a hardcoded 0).
func TestRunReview_ConfiguredBudgetRecordsTruncation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte("package main\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base := run("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.go"), []byte("package main\n\nfunc s() {}\n"), 0o644))
	big := "package main\n\nfunc b() {\n" + strings.Repeat("\t_ = \"padding padding padding padding\"\n", 400) + "}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(big), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head := run("rev-parse", "HEAD")

	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)
	cfg.Settings.PayloadByteBudget = 2048 // fits small.go's blocks, not big.go's

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(dir, dir, base, head))
	require.NoError(t, err)

	for _, agent := range []string{"greta", "kai"} {
		data, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "status.json"))
		require.NoError(t, err)
		var st AgentStatus
		require.NoError(t, json.Unmarshal(data, &st))
		assert.True(t, st.Truncated, "agent %s: configured budget must record truncation", agent)
		assert.Contains(t, st.FilesDropped, "big.go", "agent %s: dropped file must be listed", agent)
	}
}

// A range whose only commit is an empty commit has CommitCount > 0 but zero
// changed files, so every payload mode builds empty. PrepareReview must refuse
// before scaffolding — never fire the provider pool at an empty payload — and
// leave no review dir or latest pointer behind.
func TestPrepareReview_OnlyEmptyCommitsRejectedBeforeScaffold(t *testing.T) {
	repo, _, head := initRepo(t)
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("commit", "-q", "--allow-empty", "-m", "empty")
	emptyHead := run("rev-parse", "HEAD")

	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, head, emptyHead)
	_, err := PrepareReview(context.Background(), cfg, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.NoDirExists(t, filepath.Join(repo, ".atcr", "reviews"), "no review dir may be scaffolded for an empty payload")
}

// A client retry of atcr_review with the same explicit id (plausible while the
// first run still shows running) must not launch a second fan-out into the SAME
// review directory — the second PrepareReview must refuse, not scaffold.
func TestPrepareReview_RejectsExistingOverrideID(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	req := reviewReq(repo, repo, base, head)
	req.IDOverride = "custom-review-id"

	_, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	_, err = PrepareReview(context.Background(), cfg, req)
	require.Error(t, err, "second prepare with the same override id must refuse")
	assert.Contains(t, err.Error(), "custom-review-id")
}

// A payloads map missing the agent's effective mode must be an explicit build
// error (like the adjacent unknown-agent/unknown-provider lookups), never a
// silently empty payload that produces a plausible-looking vacuous review.
func TestBuildAgent_MissingPayloadModeErrors(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	_, _, err := buildAgent(cfg, "greta", map[string]modePayload{}, ReviewRange{Base: "a", Head: "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greta")
	assert.Contains(t, err.Error(), "blocks")
}

func TestLoadReviewConfig_DiscoversConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	regYAML := `providers:
  openai:
    api_key_env: OPENAI_API_KEY
    base_url: https://api.openai.com/v1
agents:
  greta:
    provider: openai
    model: gpt-4
`
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(regYAML), 0o644))

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	projYAML := "agents:\n  - greta\npayload_mode: diff\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"), []byte(projYAML), 0o644))

	cfg, err := LoadReviewConfig(root, registry.CLIOverrides{})
	require.NoError(t, err)
	assert.Contains(t, cfg.Registry.Agents, "greta")
	assert.Equal(t, []string{"greta"}, cfg.Project.Agents)
	assert.Equal(t, "diff", cfg.Settings.PayloadMode, "project tier overrides the blocks default")
}
