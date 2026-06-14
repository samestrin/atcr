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

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshot constants keep the worktree-branch assertions self-documenting.
const (
	snapshotModeWorktree = "worktree"
	snapshotPrefix       = "atcr-snapshot-"
)

// initToolRepo creates a temp git repo whose head commit changes auth.go (the
// payload) while helper.go stays unchanged (outside the payload), so a tool
// agent can demonstrate reading a file the payload never showed it.
func initToolRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package main\n\n// helper documents the contract for b.\nfunc helper() { a() }\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")
	// Only auth.go changes in the head commit; helper.go is untouched.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n\nfunc a() { b() }\n\nfunc b() {}\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "head")
	head = run("rev-parse", "HEAD")
	return dir, base, head
}

// toolMockProvider scripts a two-turn tool exchange in the OpenAI wire shape:
// the first request (no role:"tool" messages yet) returns an assistant turn with
// read_file + grep tool_calls; once tool results are appended it returns a final
// findings message that cites the evidence read.
func toolMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		hasToolResult := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				hasToolResult = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if !hasToolResult {
			// Turn 1: request two tools — read a file outside the payload and grep
			// for callers. Arguments are JSON-encoded strings (OpenAI/litellm style).
			resp := map[string]any{"choices": []map[string]any{{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []map[string]any{
						{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"helper.go"}`}},
						{"id": "call_2", "type": "function", "function": map[string]any{"name": "grep", "arguments": `{"pattern":"func b"}`}},
					},
				},
			}}}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// Turn 2: final findings, citing the evidence actually read.
		content := "HIGH|auth.go:3|b() is unguarded and helper.go documents no precondition|Add a guard before calling b|correctness|10|read helper.go (outside payload) and grepped func b"
		resp := map[string]any{"choices": []map[string]any{{
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": content},
		}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toolAgentConfig(srvURL string) *ReviewConfig {
	return &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7), Tools: true, SupportsFC: true},
			},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}
}

// Success Criteria #1 (Functional): a tool-enabled agent completes a multi-turn
// review against a fixture repo — reads a file outside the payload, greps for
// callers, and produces findings — with the transcript replaying the session and
// status.json counters reflecting the tool usage.
func TestExecuteReview_ToolAgentEndToEnd(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initToolRepo(t)
	srv := toolMockProvider(t)
	cfg := toolAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 1, res.Summary.Succeeded)

	agentDir := filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta")

	// The agent produced a finding.
	fdata, err := os.ReadFile(filepath.Join(agentDir, "findings.txt"))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(fdata)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1)
	assert.Equal(t, "greta", parsed.Findings[0].Reviewer)

	// status.json counters reflect the multi-turn tool usage.
	sdata, err := os.ReadFile(filepath.Join(agentDir, "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(sdata, &st))
	require.NotNil(t, st.Turns)
	assert.GreaterOrEqual(t, *st.Turns, 2, "at least the tool turn and the final turn")
	require.NotNil(t, st.ToolCalls)
	assert.Equal(t, 2, *st.ToolCalls, "read_file + grep")
	require.NotNil(t, st.ToolBytes)
	assert.Greater(t, *st.ToolBytes, int64(0), "tool results delivered bytes")

	// The transcript replays the full session: one tool_calls event, two
	// tool_result events, then the final message.
	tr, err := tools.ReplayTranscript(filepath.Join(agentDir, "transcript.jsonl"))
	require.NoError(t, err)
	var kinds []string
	for _, e := range tr.Events {
		kinds = append(kinds, e.Event)
	}
	assert.Equal(t, []string{"tool_calls", "tool_result", "tool_result", "final"}, kinds)

	// The read_file result in the transcript carries helper.go's content — proof
	// the agent read a file the payload never contained.
	require.GreaterOrEqual(t, len(tr.Events), 2)
	readResult := string(tr.Events[1].Raw["content"])
	assert.Contains(t, readResult, "helper", "transcript records the file actually read")

	// manifest.json review stage lists the tool agent.
	mdata, err := os.ReadFile(filepath.Join(res.Dir, "manifest.json"))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(mdata, &raw))
	require.Contains(t, raw, "review")
	assert.Contains(t, string(raw["review"]), "greta")

	// AC 03-02 Scenario 5 + AC 03-03 Scenario 4 (worktree branch), end-to-end:
	// although head equals HEAD, PrepareReview has already written the untracked
	// .atcr/reviews/ scaffolding into the repo, so `git status --porcelain` reports
	// a dirty tree and SnapshotFor falls through to the slow path. The review stage
	// on disk therefore records worktree mode, the resolved head_sha, and a worktree
	// path under the OS temp dir whose leaf is the resolved head SHA.
	var review struct {
		SnapshotMode         string `json:"snapshot_mode"`
		HeadSHA              string `json:"head_sha"`
		SnapshotWorktreePath string `json:"snapshot_worktree_path"`
	}
	require.NoError(t, json.Unmarshal(raw["review"], &review))
	assert.Equal(t, snapshotModeWorktree, review.SnapshotMode)
	assert.Equal(t, head, review.HeadSHA)
	assert.Contains(t, review.SnapshotWorktreePath, snapshotPrefix)
	assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),
		"worktree leaf is the resolved head SHA")
}

// budgetTripMockProvider returns read_file tool_calls on every budgeted turn (a
// request carrying tools) and a final findings message on the unbudgeted
// final-answer request (no tools) — so a small max_turns trips mid-exploration.
func budgetTripMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		if len(req.Tools) == 0 {
			// Final-answer request (no tools): return partial findings.
			content := "MEDIUM|auth.go:3|partial review after turn budget|Investigate b()|correctness|10|stopped by max_turns"
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
				"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content},
			}}})
			return
		}
		// Each budgeted turn asks for a distinct read so the loop never short-circuits
		// on a repeated call before the turn budget trips.
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
			"finish_reason": "tool_calls",
			"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []map[string]any{
				{"id": "call_x", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"helper.go"}`}},
			}},
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// AC 02-04 / Success Criteria #2: a tool agent that trips its turn budget mid-run
// records tripped_budgets in the on-disk status.json AND still writes its partial
// findings (partial-success semantics survive the artifact layer).
func TestExecuteReview_BudgetTripRecordedInStatusAndPartialFindings(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initToolRepo(t)
	srv := budgetTripMockProvider(t)
	cfg := toolAgentConfig(srv.URL)
	ac := cfg.Registry.Agents["greta"]
	ac.MaxTurns = ptrInt(2) // trip after two Chat-with-tools turns
	cfg.Registry.Agents["greta"] = ac

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, 1, res.Summary.Succeeded)

	agentDir := filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta")
	sdata, err := os.ReadFile(filepath.Join(agentDir, "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(sdata, &st))
	assert.Contains(t, st.TrippedBudgets, "max_turns", "the turn budget trip is recorded on disk")
	require.NotNil(t, st.Turns)
	assert.Equal(t, 2, *st.Turns)

	// Partial findings survived the trip and reached the artifact layer.
	fdata, err := os.ReadFile(filepath.Join(agentDir, "findings.txt"))
	require.NoError(t, err)
	parsed, err := stream.ParseSource(fdata)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1)
	assert.Equal(t, "MEDIUM", parsed.Findings[0].Severity)
}

// mixedMockProvider serves both an agent's multi-turn Chat (request carries
// tools) and another agent's single-shot Complete (no tools): a tools request
// without a tool result yields one read_file call; everything else (the tool
// agent's second turn, or a single-shot completion) yields a final finding.
func mixedMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		hasToolResult := false
		for _, m := range req.Messages {
			if m.Role == "tool" {
				hasToolResult = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if len(req.Tools) > 0 && !hasToolResult {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
				"finish_reason": "tool_calls",
				"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []map[string]any{
					{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"helper.go"}`}},
				}},
			}}})
			return
		}
		content := "HIGH|auth.go:3|b() unguarded|Guard it|correctness|10|reviewed the change"
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
			"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content},
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// AC 04-04 / Success Criteria: a mixed roster (one tool-loop agent + one 1.x
// single-shot agent) runs in one review; the pool consumes both result shapes
// identically, the tool agent emits tool counters + a transcript, and the
// non-tool agent's status.json stays byte-clean of tool fields.
func TestExecuteReview_MixedRosterReconcilesBoth(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initToolRepo(t)
	srv := mixedMockProvider(t)
	cfg := &ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srv.URL}},
			Agents: map[string]registry.AgentConfig{
				"greta": {Provider: "p", Model: "m-greta", Persona: "greta", Temperature: ptrF(0.7), Tools: true, SupportsFC: true},
				"kai":   {Provider: "p", Model: "m-kai", Persona: "kai", Temperature: ptrF(0.7)}, // 1.x single-shot
			},
		},
		Project:  &registry.ProjectConfig{Agents: []string{"greta", "kai"}},
		Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
	}

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, 2, res.Summary.Succeeded, "both agents complete")

	// Both agents produced a finding the pool merged identically.
	for _, agent := range []string{"greta", "kai"} {
		fdata, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "findings.txt"))
		require.NoErrorf(t, err, "missing %s findings", agent)
		parsed, err := stream.ParseSource(fdata)
		require.NoError(t, err)
		require.Lenf(t, parsed.Findings, 1, "%s finding count", agent)
		assert.Equal(t, agent, parsed.Findings[0].Reviewer)
	}

	// The tool agent recorded counters + a transcript.
	var greta AgentStatus
	gdata, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(gdata, &greta))
	require.NotNil(t, greta.Turns)
	assert.GreaterOrEqual(t, *greta.Turns, 2)
	assert.FileExists(t, filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta", "transcript.jsonl"))

	// The non-tool agent's status.json is byte-clean of tool fields (1.x identical).
	kdata, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "kai", "status.json"))
	require.NoError(t, err)
	for _, f := range []string{"turns", "tool_calls", "tool_bytes", "tools_degraded", "tools_requested"} {
		assert.NotContainsf(t, string(kdata), `"`+f+`"`, "non-tool kai status.json must omit %q", f)
	}
}

// A degraded tool agent (model not function-calling-capable) still completes via
// single-shot, records tools_degraded, and writes no transcript events — the
// mixed-roster/degrade path through the real review flow.
func TestExecuteReview_ToolAgentDegradesWhenIncapable(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initToolRepo(t)
	srv := mockProvider(t) // plain single-shot provider (no tool_calls)
	cfg := toolAgentConfig(srv.URL)
	// Flip the agent to non-function-calling so it must degrade.
	ac := cfg.Registry.Agents["greta"]
	ac.SupportsFC = false
	cfg.Registry.Agents["greta"] = ac

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, 1, res.Summary.Succeeded)

	sdata, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(sdata, &st))
	assert.True(t, st.ToolsDegraded, "an incapable model with tools:true degrades")
	require.NotNil(t, st.Turns)
	assert.Equal(t, 0, *st.Turns, "degrade path runs single-shot, no tool turns")
}
