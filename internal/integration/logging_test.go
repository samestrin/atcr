// Package integration holds end-to-end tests that exercise the structured
// logging, request correlation, secret/path redaction, and error
// classification wiring (Epic 4.0) across package boundaries — driving a real
// review run against a mocked LLM provider and asserting on captured log
// output. These tests live in their own package so they consume only the
// exported APIs of internal/fanout, internal/llmclient, internal/log,
// internal/errors, and internal/mcp, the same way the cmd/atcr and MCP layers do.
package integration

import (
	"bytes"
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

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atcrerrors "github.com/samestrin/atcr/internal/errors"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/mcp"
	"github.com/samestrin/atcr/internal/registry"
)

func ptrF(f float64) *float64 { return &f }

// initRepo creates a temp git repo with a base and head commit that change a Go
// file, returning the repo dir and the two SHAs. Mirrors the fanout test
// fixture but uses only exported APIs.
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
// shape, returning a single CRITICAL finding for every agent.
func mockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func twoAgentConfig(srvURL string) *fanout.ReviewConfig {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7)},
			"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)},
		},
	}
	return &fanout.ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta", "kai"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
}

func reviewReq(repo, base, head string) fanout.ReviewRequest {
	return fanout.ReviewRequest{
		Repo:       repo,
		Root:       repo,
		Range:      fanout.ReviewRange{Base: base, Head: head, DetectionMode: "explicit", CommitCount: 1},
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

// runCorrelatedReview drives a full review exactly as cmd/atcr/review.go does:
// it constructs the root logger over buf, runs PrepareReview, attaches the
// review_id and a sink-level Redactor (rooted at the absolute repo path) to the
// context logger, then runs ExecuteReview. It returns the resolved review id and
// the correlated context so a test can probe the wired sink directly.
func runCorrelatedReview(t *testing.T, buf *bytes.Buffer, level, format string) (string, context.Context, *fanout.ReviewResult) {
	t.Helper()
	repo, base, head := initRepo(t)
	srv := mockProvider(t)

	logger, err := log.New(level, format, buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	prep, err := fanout.PrepareReview(ctx, twoAgentConfig(srv.URL), reviewReq(repo, base, head))
	require.NoError(t, err)

	// Mirror cmd/atcr/review.go: attach review_id (AC9), then enforce sink-level
	// redaction rooted at the absolute repo path (AC5/AC6).
	ctx = log.NewContext(ctx, log.WithReviewID(log.FromContext(ctx), prep.ID))
	redactRoot := prep.Repo
	if abs, aerr := filepath.Abs(redactRoot); aerr == nil {
		redactRoot = abs
	}
	ctx = log.NewContext(ctx, log.WithRedactor(log.FromContext(ctx), log.NewRedactor(redactRoot)))

	res, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, 2, res.Summary.Succeeded, "both mocked agents should succeed")
	return prep.ID, ctx, res
}

// TestIntegration_ReviewRun_DebugOutputHasCorrelation verifies that a debug-level
// review run tags every emitted log line with review_id (AC9) and that agent
// invocation lines additionally carry agent_name (AC10).
func TestIntegration_ReviewRun_DebugOutputHasCorrelation(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	var buf bytes.Buffer
	id, _, _ := runCorrelatedReview(t, &buf, "debug", "text")

	out := buf.String()
	require.NotEmpty(t, out, "debug review run must emit log lines")
	require.Contains(t, out, "invoking agent", "engine must log per-agent debug lines")

	sawAgentLine := false
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		assert.Contains(t, line, log.AttrReviewID+"="+id,
			"every log line during a review must carry review_id (AC9): %q", line)
		if strings.Contains(line, "invoking agent") {
			sawAgentLine = true
			assert.Regexp(t, log.AttrAgentName+"=(greta|kai)", line,
				"agent invocation lines must carry agent_name (AC10): %q", line)
		}
	}
	assert.True(t, sawAgentLine, "expected at least one agent invocation line")
}

// TestIntegration_ReviewRun_JSONFormat verifies --log-format=json emits
// newline-delimited JSON with level and msg keys on every line (AC2).
func TestIntegration_ReviewRun_JSONFormat(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	var buf bytes.Buffer
	runCorrelatedReview(t, &buf, "debug", "json")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "each log line must be valid JSON: %q", line)
		assert.Contains(t, rec, "level", "JSON log line must have a level key (AC2)")
		assert.Contains(t, rec, "msg", "JSON log line must have a msg key (AC2)")
	}
}

// TestIntegration_ReviewRun_NoSecretLeak verifies a known API-key value never
// appears in log output, even when a call site logs it directly — redaction is
// enforced at the sink for the whole review (AC5).
func TestIntegration_ReviewRun_NoSecretLeak(t *testing.T) {
	const key = "sk-canary-leak-value-987654"
	t.Setenv("ATCR_TEST_KEY", key)
	var buf bytes.Buffer
	_, ctx, _ := runCorrelatedReview(t, &buf, "debug", "text")

	// Prove the wired sink scrubs a secret a careless call site logs directly.
	log.FromContext(ctx).Error("provider rejected credential",
		"authorization", "Bearer "+key, "raw", key)

	out := buf.String()
	assert.NotContains(t, out, key, "API key value must not appear in log output at any level (AC5)")
	assert.Contains(t, out, "[redacted]", "the sink must have scrubbed the secret-shaped value")
}

// TestIntegration_ReviewRun_NoAbsolutePathLeak verifies absolute repo paths are
// rendered relative to the review root in log output (AC6).
func TestIntegration_ReviewRun_NoAbsolutePathLeak(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	var buf bytes.Buffer
	_, ctx, res := runCorrelatedReview(t, &buf, "debug", "text")

	root, err := filepath.Abs(res.Dir)
	require.NoError(t, err)
	// The review root is the repo; res.Dir lives under it. Walk up to the repo
	// root so the relativization assertion targets the redactor's configured root.
	repoRoot := root
	for !fileExists(t, filepath.Join(repoRoot, "auth.go")) {
		parent := filepath.Dir(repoRoot)
		require.NotEqual(t, repoRoot, parent, "could not locate repo root above %s", root)
		repoRoot = parent
	}
	abs := filepath.Join(repoRoot, "internal", "secret.go")
	log.FromContext(ctx).Info("loaded " + abs)

	out := buf.String()
	assert.NotContains(t, out, repoRoot+string(filepath.Separator),
		"absolute paths under the review root must be relativized (AC6)")
	assert.Contains(t, out, filepath.Join("internal", "secret.go"),
		"path must render relative to the review root")
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// fakeCompleter is a stub LLM completer for the MCP stdout test.
type fakeCompleter struct{}

func (fakeCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "CRITICAL|auth.go:1|x|y|security|10|z", nil
}

// TestIntegration_MCPMode_StdoutClean verifies that in MCP serve mode stdout
// carries only the protocol stream: the shared logger writes diagnostics to its
// configured sink (stderr), and no tool call leaks to os.Stdout (AC3). The
// in-memory transport never writes to os.Stdout, so any byte captured there is a
// stdout-discipline violation.
func TestIntegration_MCPMode_StdoutClean(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })

	var logBuf bytes.Buffer
	logger, err := log.New("debug", "text", &logBuf)
	require.NoError(t, err)

	srv, err := mcp.NewServer(t.TempDir(), fakeCompleter{}, logger)
	require.NoError(t, err)

	ctx := context.Background()
	clientT, serverT := mcpsdk.NewInMemoryTransports()
	_, err = srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	c := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := c.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	_, _ = cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: mcp.ToolStatus, Arguments: map[string]any{}})
	_, _ = cs.ListTools(ctx, nil)

	require.NoError(t, w.Close())
	leaked, _ := io.ReadAll(r)
	assert.Empty(t, string(leaked), "MCP serve mode: stdout must be protocol-only, no log leak (AC3)")
}

// TestIntegration_LLMClient_ErrorClassification verifies a transient (503)
// provider failure surfaces as a retryable ClassifiedError (AC11, AC12).
func TestIntegration_LLMClient_ErrorClassification(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "sk-secret-value-123")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := llmclient.New(llmclient.WithHTTPClient(srv.Client()), llmclient.WithRetry(2, time.Millisecond, 1.5))
	_, err := c.Complete(context.Background(), llmclient.Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "ATCR_TEST_KEY", Model: "m1", Prompt: "review",
	})
	require.Error(t, err)
	assert.True(t, atcrerrors.IsRetryable(err), "503 must classify as retryable transient (AC12)")
}

// TestIntegration_LLMClient_PermanentError verifies a permanent (404) provider
// failure surfaces as a non-retryable ClassifiedError (AC11, AC12).
func TestIntegration_LLMClient_PermanentError(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "sk-secret-value-123")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := llmclient.New(llmclient.WithHTTPClient(srv.Client()), llmclient.WithRetry(2, time.Millisecond, 1.5))
	_, err := c.Complete(context.Background(), llmclient.Invocation{
		BaseURL: srv.URL + "/v1", APIKeyEnv: "ATCR_TEST_KEY", Model: "m1", Prompt: "review",
	})
	require.Error(t, err)
	assert.False(t, atcrerrors.IsRetryable(err), "404 must classify as non-retryable permanent (AC12)")
}
