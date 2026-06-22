package verify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// agentExecConfig returns an executor config with agent_mode enabled, reusing the
// snippet-path execConfig so the only difference under test is the agent-mode flag.
func agentExecConfig() *registry.ExecutorConfig {
	ex := execConfig("MEDIUM")
	ex.AgentMode = true
	return ex
}

func eligibleFinding() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "stores plaintext password",
			Confidence: ConfidenceVerified, Evidence: "Found by bruce; confidence HIGH"},
	}
}

// AC2: agent_mode=true drives the tool loop and the parsed JSON fix lands on the
// finding with attribution — and the single-shot snippet completer is never used.
func TestGenerateFixes_AgentMode_PopulatesFix(t *testing.T) {
	findings := eligibleFinding()
	cc := finalChat(`{"fix": "hash the password with bcrypt", "explanation": "avoids plaintext storage"}`)
	rec := &recordingExecutor{out: "SNIPPET PATH MUST NOT RUN"}
	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"), rec, cc, okDispatcher(), 0)

	assert.Equal(t, "hash the password with bcrypt", findings[0].Fix)
	assert.Contains(t, findings[0].Evidence, "fix by opus")
	assert.Equal(t, "", findings[0].FixWarning)
	assert.Equal(t, 0, rec.calls, "agent mode must not call the single-shot snippet completer")
}

// AC2: the executor reads via the dispatcher before concluding — a tool-call turn
// followed by a final JSON answer dispatches at least once.
func TestGenerateFixes_AgentMode_ToolLoopThenFix(t *testing.T) {
	findings := eligibleFinding()
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"fix": "guard the nil deref", "explanation": "checked after read"}`},
	}}
	disp := okDispatcher()
	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"), &recordingExecutor{}, cc, disp, 0)

	assert.Equal(t, "guard the nil deref", findings[0].Fix)
	assert.GreaterOrEqual(t, disp.count(), 1, "agent mode should dispatch at least one tool call")
}

// AC4: a tool-loop provider error produces a FixWarning on the finding; the run
// continues and the finding is emitted without a fix (failure isolation).
func TestGenerateFixes_AgentMode_ProviderErrorWarns(t *testing.T) {
	findings := eligibleFinding()
	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"), &recordingExecutor{}, cc, okDispatcher(), 0)

	assert.Equal(t, "", findings[0].Fix, "no fix on a failed tool loop")
	assert.Contains(t, findings[0].FixWarning, "agent_mode failed")
	assert.Contains(t, findings[0].FixWarning, "rate limit exceeded")
}

// AC3: max_tool_calls caps the loop. With a budget of 2 and a completer that only
// ever requests tools (never concludes), the loop trips max_turns and the finding
// gets a FixWarning rather than a fix.
func TestGenerateFixes_AgentMode_MaxToolCallsCapTrips(t *testing.T) {
	findings := eligibleFinding()
	ex := agentExecConfig()
	ex.MaxToolCalls = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), &recordingExecutor{}, cc, okDispatcher(), 0)

	assert.Equal(t, "", findings[0].Fix)
	assert.Contains(t, findings[0].FixWarning, "agent_mode failed")
	assert.Contains(t, findings[0].FixWarning, "max_turns", "the tool-call budget trip must be surfaced")
}

// AC6: agent_mode=true but the dispatcher is unavailable (nil) → fall back to the
// snippet path with a logged warning. The single-shot completer IS used.
func TestGenerateFixes_AgentMode_FallsBackWhenDispatcherNil(t *testing.T) {
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	findings := eligibleFinding()
	rec := &recordingExecutor{out: "snippet-path fix"}
	cc := finalChat(`{"fix": "agent path MUST NOT RUN"}`)
	generateFixes(ctx, findings, agentExecConfig(), execRegistry("MEDIUM"), rec, cc, nil, 0)

	assert.Equal(t, 1, rec.calls, "nil dispatcher must fall back to the single-shot snippet path")
	assert.Equal(t, "snippet-path fix", findings[0].Fix)
	assert.Contains(t, buf.String(), "agent_mode", "the fallback must be logged")
}

// AC6 (companion): agent_mode=true but no ChatCompleter wired (nil) → snippet
// fallback. Mirrors the nil-dispatcher case for the other half of the harness.
func TestGenerateFixes_AgentMode_FallsBackWhenCCNil(t *testing.T) {
	findings := eligibleFinding()
	rec := &recordingExecutor{out: "snippet-path fix"}
	generateFixes(context.Background(), findings, agentExecConfig(), execRegistry("MEDIUM"), rec, nil, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls, "nil ChatCompleter must fall back to the snippet path")
	assert.Equal(t, "snippet-path fix", findings[0].Fix)
}

// AC1: with agent_mode=false the snippet path runs unchanged even when a
// ChatCompleter is wired — the tool loop is never entered. A cc that would error
// if called proves it is untouched.
func TestGenerateFixes_AgentModeOff_SnippetPathUnchanged(t *testing.T) {
	findings := eligibleFinding()
	rec := &recordingExecutor{out: "snippet-path fix"}
	ccThatMustNotRun := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("cc must not be called when agent_mode is off")}}}
	generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), rec, ccThatMustNotRun, okDispatcher(), 0)

	assert.Equal(t, 1, rec.calls)
	assert.Equal(t, "snippet-path fix", findings[0].Fix)
	assert.Equal(t, "", findings[0].FixWarning)
}

// --- invokeExecutor direct unit tests ---

func testExecProviderVal() registry.Provider {
	return registry.Provider{BaseURL: "http://x.invalid", APIKeyEnv: "K"}
}

func TestInvokeExecutor_Success(t *testing.T) {
	fix, warn := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], finalChat(`{"fix": "x", "explanation": "y"}`), okDispatcher(), 0)
	assert.Equal(t, "x", fix)
	assert.Equal(t, "", warn)
}

func TestInvokeExecutor_ProviderErrorReturnsWarn(t *testing.T) {
	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("boom")}}}
	fix, warn := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], cc, okDispatcher(), 0)
	assert.Equal(t, "", fix)
	assert.Contains(t, warn, "agent_mode failed")
	assert.Contains(t, warn, "boom")
}

func TestInvokeExecutor_ParseErrorReturnsWarn(t *testing.T) {
	fix, warn := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], finalChat("I could not find a fix"), okDispatcher(), 0)
	assert.Equal(t, "", fix)
	assert.Contains(t, warn, "agent_mode parse error")
}

// --- parseExecutorResponse unit tests ---

func TestParseExecutorResponse_Valid(t *testing.T) {
	fix, err := parseExecutorResponse(`{"fix": "use a parameterized query", "explanation": "blocks sqli"}`)
	require.NoError(t, err)
	assert.Equal(t, "use a parameterized query", fix)
}

func TestParseExecutorResponse_FencedJSON(t *testing.T) {
	fix, err := parseExecutorResponse("Here is the fix:\n```json\n{\"fix\": \"add bounds check\"}\n```")
	require.NoError(t, err)
	assert.Equal(t, "add bounds check", fix)
}

func TestParseExecutorResponse_MissingFixField(t *testing.T) {
	_, err := parseExecutorResponse(`{"explanation": "no fix key here"}`)
	require.Error(t, err)
}

func TestParseExecutorResponse_EmptyFix(t *testing.T) {
	_, err := parseExecutorResponse(`{"fix": "   "}`)
	require.Error(t, err)
}

func TestParseExecutorResponse_NoJSON(t *testing.T) {
	_, err := parseExecutorResponse("there is no json object here")
	require.Error(t, err)
}

// --- buildExecutorAgent / buildExecutorAgentPrompt unit tests ---

func TestBuildExecutorAgent_ForwardsProviderAndBudget(t *testing.T) {
	ex := agentExecConfig()
	ex.MaxToolCalls = intPtr(7)
	a := buildExecutorAgent(ex, testExecProviderVal(), "the prompt", 0)
	assert.True(t, a.Tools, "agent-mode executor must enable tools")
	assert.True(t, a.SupportsFC, "agent-mode executor must declare function calling so the loop fires")
	assert.Equal(t, 7, a.MaxTurns, "max_tool_calls maps to the agent MaxTurns budget")
	assert.Equal(t, "http://x.invalid", a.Invocation.BaseURL)
	assert.Equal(t, "K", a.Invocation.APIKeyEnv)
	assert.Equal(t, "m-exec", a.Invocation.Model)
	require.NotNil(t, a.Invocation.Temperature)
	assert.Equal(t, 0.0, *a.Invocation.Temperature, "executor fixes default to deterministic temperature")
}

func TestBuildExecutorAgent_DefaultMaxTurnsWhenUnset(t *testing.T) {
	a := buildExecutorAgent(agentExecConfig(), testExecProviderVal(), "p", 0)
	assert.Equal(t, registry.DefaultExecutorMaxToolCalls, a.MaxTurns, "unset max_tool_calls defaults to 10")
}

func TestBuildExecutorAgentPrompt_ContainsFindingAndSchema(t *testing.T) {
	f := reconcile.JSONFinding{Severity: "HIGH", File: "auth.go", Line: 42, Category: "SECURITY",
		Problem: "plaintext password", Fix: "use bcrypt", Evidence: "line 42 stores raw input"}
	p := buildExecutorAgentPrompt(f)
	assert.Contains(t, p, "plaintext password", "the problem must be in the prompt")
	assert.Contains(t, p, "auth.go:42", "the location must be in the prompt")
	assert.Contains(t, p, "use bcrypt", "the reviewer's suggested fix must be carried in")
	assert.Contains(t, p, `"fix"`, "the JSON response schema must be specified")
	assert.Contains(t, strings.ToLower(p), "read", "the prompt must instruct the executor to read the code first")
}
