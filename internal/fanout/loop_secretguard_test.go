package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loopAWSKey is split so the source holds no literal credential.
const loopAWSKey = "AKIA" + "QYLPMN5HHHFPZAM2"

// invRecorder is a ChatCompleter that answers one tool-call turn per call and
// records the invocation of every Chat call, so a MaxTurns trip reaches
// requestFinalAnswer.
type invRecorder struct {
	mu   sync.Mutex
	invs []llmclient.Invocation
}

func (c *invRecorder) Complete(context.Context, llmclient.Invocation) (string, error) {
	return "", nil
}

func (c *invRecorder) Chat(_ context.Context, inv llmclient.Invocation, _ []llmclient.Message, defs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	c.mu.Lock()
	c.invs = append(c.invs, inv)
	n := len(c.invs)
	c.mu.Unlock()
	if defs == nil {
		return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: strPtr("NO FINDINGS")}, FinishReason: "stop"}, nil
	}
	return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{
		{ID: fmt.Sprintf("c%d", n), Type: "function", Function: llmclient.FunctionCall{Name: "read_file", Arguments: json.RawMessage(fmt.Sprintf(`{"path":"f%d.go"}`, n))}},
	}}, FinishReason: "tool_calls"}, nil
}

// recordLoopInvocations runs a two-turn tool agent whose invocation carries
// policy, so the loop trips max_turns and asks for a final answer, and returns
// the invocation of each Chat call.
func recordLoopInvocations(t *testing.T, policy string) []llmclient.Invocation {
	t.Helper()
	cc := &invRecorder{}
	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "x"}
	a := toolAgent("a", 2, 0)
	a.Invocation.SecretGuard = policy
	r := toolEngine(cc, d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	require.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets, "the loop must reach requestFinalAnswer")
	require.Len(t, cc.invs, 3, "two tool turns plus the final-answer call")
	return cc.invs
}

// Epic 106.2 T2: every tool-loop Chat call, the per-turn ones and the one in
// requestFinalAnswer, carries the agent's secret_guard policy and counts the
// guarded review prompt (message zero) as already guarded. Under warn and
// block a message is scanned once: a successful Chat sent the messages it was
// given, warned about them or found them clean, so the next call counts them
// as guarded too. Call 1 sends only the prompt, so call 2 scans from message
// 1; call 2 sent prompt, turn 1 and its tool result (3), so the final-answer
// call scans only what turn 2 added. Under redact the redacted
// copy never reaches the loop's history, so every call redacts from message 1
// again; off scans nothing and stays at 1.
func TestToolLoop_SecretGuardOnEveryChatCall(t *testing.T) {
	want := map[string][]int{
		secretGuardOff:    {1, 1, 1},
		secretGuardWarn:   {1, 1, 3},
		secretGuardRedact: {1, 1, 1},
		secretGuardBlock:  {1, 1, 3},
	}
	for policy, guarded := range want {
		for i, inv := range recordLoopInvocations(t, policy) {
			assert.Equal(t, policy, inv.SecretGuard, "%s: call %d", policy, i+1)
			assert.Equal(t, guarded[i], inv.AlreadyGuardedMessages, "%s: call %d", policy, i+1)
		}
	}
}

// An agent built without the registry carries no policy; the fanout default
// is warn (secretGuardPolicy), not llmclient's off.
func TestToolLoop_SecretGuardDefaultsToWarn(t *testing.T) {
	for i, inv := range recordLoopInvocations(t, "") {
		assert.Equal(t, secretGuardWarn, inv.SecretGuard, "call %d", i+1)
		assert.Equal(t, []int{1, 1, 3}[i], inv.AlreadyGuardedMessages, "call %d", i+1)
	}
}

// The policy reaches the loop from Settings.SecretGuard, on the primary and on
// a fallback, and an unset setting resolves to warn.
func TestBuildAgents_InvocationCarriesSettingsSecretGuard(t *testing.T) {
	for setting, want := range map[string]string{"": secretGuardWarn, secretGuardOff: secretGuardOff, secretGuardRedact: secretGuardRedact, secretGuardBlock: secretGuardBlock} {
		cfg := toolCfg()
		cfg.Settings.SecretGuard = setting
		payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}
		primary, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)
		assert.Equal(t, want, primary.Invocation.SecretGuard, "primary, setting %q", setting)
		fb, _, err := buildFallbackAgent(context.Background(), cfg, primary, "kai", true, fallbackRefit{})
		require.NoError(t, err)
		assert.Equal(t, want, fb.Invocation.SecretGuard, "fallback, setting %q", setting)
	}
}

// Through the real client: under redact a credential in a tool result is
// redacted on every later request, the final-answer one included, while the
// review prompt, already guarded when it was built, is sent as it is and not
// scanned again.
func TestToolLoop_RedactsToolResultsNotThePrompt(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		turn := len(bodies)
		mu.Unlock()
		if turn <= 2 {
			_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
				`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`, turn, turn)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"NO FINDINGS"}}]}`)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "key = " + loopAWSKey}
	a := toolAgent("a", 2, 0)
	// A prompt the guard let through (warn) still holds the key; the loop must
	// not rescan it.
	a.Invocation = llmclient.Invocation{Model: "m", BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY",
		Prompt: "review this: " + loopAWSKey, SecretGuard: secretGuardRedact}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).invokeAgent(context.Background(), a)
	require.Equal(t, StatusOK, r.Status)
	require.Equal(t, []string{budgetMaxTurns}, r.TrippedBudgets)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 3, "two tool turns plus the final-answer call")
	for i, body := range bodies {
		msgs := wireMessages(t, body)
		assert.Contains(t, string(msgs[0]["content"]), loopAWSKey, "request %d: the guarded prompt is not rescanned", i+1)
		for j, m := range msgs[1:] {
			assert.NotContains(t, string(m["content"]), loopAWSKey, "request %d message %d", i+1, j+1)
		}
		if i > 0 {
			// Message 2 is turn 1's read_file result; turn 2's call trips the
			// budget and is answered with a skip notice, not the file.
			require.Greater(t, len(msgs), 2, "request %d", i+1)
			assert.Equal(t, `"tool"`, string(msgs[2]["role"]), "request %d", i+1)
			assert.Contains(t, string(msgs[2]["content"]), payload.SecretPlaceholder, "request %d: the tool result is redacted", i+1)
		}
	}
	assert.NotContains(t, bodies[2], `"tools"`, "the last request is requestFinalAnswer's")
}

// Through the real client: under block a credential in a tool result stops the
// agent before the turn that would carry it is sent, so advancing the guarded
// count past the clean messages never skips a new one.
func TestToolLoop_BlockStopsOnAToolResultSecret(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		_, _ = fmt.Fprintf(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,`+
			`"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]}}]}`, n, n)
	}))
	defer srv.Close()
	t.Setenv("ATCR_TEST_KEY", "k")

	d := newFakeDispatcher()
	d.byName["read_file"] = tools.ToolResult{Content: "key = " + loopAWSKey}
	a := toolAgent("a", 3, 0)
	a.Invocation = llmclient.Invocation{Model: "m", BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY",
		Prompt: "review this", SecretGuard: secretGuardBlock}

	r := toolEngine(llmclient.New(llmclient.WithHTTPClient(srv.Client())), d).invokeAgent(context.Background(), a)
	assert.Equal(t, StatusFailed, r.Status)
	assert.ErrorIs(t, r.Err, llmclient.ErrSecretGuardBlocked)
	assert.Equal(t, int32(1), hits.Load(), "only the first turn, which holds no credential, is sent")
}
