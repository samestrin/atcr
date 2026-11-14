package doctor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gemma4ShortReply is the epic 113 benchmark failure: gemma4:31b answering a
// review in about 4 completion tokens (operator run bench36), the chat-template
// or stop-token fault the floor exists to catch. The object is valid JSON with
// the right key, so before the floor the response_format probe passed it.
const gemma4ShortReply = `{"findings":[]}`

// smokeEndpoint is an OpenAI-shaped fake endpoint: it echoes the marker for the
// marker probe and answers the review-shaped probe with review, reporting
// completionTokens in its usage block the way Ollama, vLLM and llama.cpp do.
func smokeEndpoint(t *testing.T, review string, completionTokens int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		content, tokens := review, completionTokens
		if i := strings.Index(string(body), "ATCR-OK-"); i >= 0 {
			content, tokens = Marker(testNonce), 6
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 210, "completion_tokens": tokens, "total_tokens": 210 + tokens},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runSmoke(t *testing.T, srv *httptest.Server) *Report {
	t.Helper()
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	reg := regWith(
		map[string]registry.Provider{"local": {APIKeyEnv: rfDoctorEnvK, BaseURL: srv.URL}},
		map[string]registry.AgentConfig{"a": {
			Provider: "local", Model: "gemma4:31b",
			ResponseFormat: registry.ResponseFormatJSONObject,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	rep := Run(context.Background(), llmclient.New(), res, Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	return rep
}

// AC2: the review-shaped probe fails a reply under the floor, names the model,
// and fails the exit code so a benchmark pre-flight stops the candidate.
func TestSmokeFloor_FailsTheGemma4FourTokenReplyAndNamesTheModel(t *testing.T) {
	rep := runSmoke(t, smokeEndpoint(t, gemma4ShortReply, 4))
	a := rep.Agents[0]

	assert.Equal(t, StatusShortReply, a.Status)
	assert.False(t, healthy(a.Status), "a reply under the floor is not a working review path")
	assert.Equal(t, 1, rep.ExitCode, "the candidate must stop before scoring")
	assert.Contains(t, a.Hint, "gemma4:31b", "the failure names the model")
	assert.Contains(t, a.Hint, "4 tokens")
	assert.Contains(t, a.Hint, "16-token floor", "the floor is stated")
	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus,
		"the shape was right; the floor is a separate verdict from response_format")
}

// AC2: a normal review reply, one finding for the planted defect, passes.
func TestSmokeFloor_PassesANormalReply(t *testing.T) {
	rep := runSmoke(t, smokeEndpoint(t, oneFinding, 71))
	a := rep.Agents[0]

	assert.Equal(t, StatusOK, a.Status)
	assert.Equal(t, 0, rep.ExitCode)
	assert.Equal(t, ResponseFormatHonored, a.ResponseFormatStatus)
}

// The floor sits exactly at SmokeTokenFloor: one under fails, the floor passes.
func TestSmokeFloor_Boundary(t *testing.T) {
	under := runSmoke(t, smokeEndpoint(t, oneFinding, SmokeTokenFloor-1))
	assert.Equal(t, StatusShortReply, under.Agents[0].Status)

	at := runSmoke(t, smokeEndpoint(t, oneFinding, SmokeTokenFloor))
	assert.Equal(t, StatusOK, at.Agents[0].Status)
}

// usageReply is a fakeCompleter Chat reply carrying a usage block.
func usageReply(content string, usage llmclient.UsageData, truncated bool) func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	return func(llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		return &llmclient.ChatResponse{
			Message:   llmclient.Message{Role: "assistant", Content: &content},
			Truncated: truncated,
			Usage:     usage,
		}, nil
	}
}

// Reasoning tokens are thinking, not the review: a thinking model that spends
// 500 tokens reasoning and answers in 4 is the near-empty review the floor exists
// to catch, so a reported reasoning count is taken off before comparing.
func TestSmokeFloor_SubtractsReportedReasoningTokens(t *testing.T) {
	a, _ := runDeclared(t, false, usageReply(gemma4ShortReply,
		llmclient.UsageData{CompletionTokens: 504, ReasoningTokens: 500, ReasoningTokensReported: true}, false))
	assert.Equal(t, StatusShortReply, a.Status)
	assert.Contains(t, a.Hint, "4 tokens")
}

// A provider that sends no usage block gives no token count; the floor reaches
// no verdict rather than guessing one, and the row is left as it was.
func TestSmokeFloor_SkipsWhenTheProviderReportsNoUsage(t *testing.T) {
	a, _ := runDeclared(t, false, reply(gemma4ShortReply))
	assert.Equal(t, StatusOK, a.Status)
}

// A reply cut off at the budget is short because of the cap, not the model;
// response_format already calls it unverified, and the floor stays out of it.
func TestSmokeFloor_SkipsATruncatedReply(t *testing.T) {
	a, _ := runDeclared(t, false, usageReply(`{"find`, llmclient.UsageData{CompletionTokens: 4}, true))
	assert.Equal(t, StatusOK, a.Status)
	assert.Equal(t, ResponseFormatUnverified, a.ResponseFormatStatus)
}

// The combined (tools) probe can pass on a tool call, which carries no review
// text, so only the plain probe is held to the floor.
func TestSmokeFloor_AppliesOnlyToThePlainProbe(t *testing.T) {
	chat := func(_ llmclient.Invocation, _ []llmclient.Message, tools []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
		if len(tools) > 0 {
			return &llmclient.ChatResponse{
				Message: llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{ID: "1", Type: "function", Function: llmclient.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"probe.go"}`)}}}},
				Usage:   llmclient.UsageData{CompletionTokens: 3},
			}, nil
		}
		return usageReply(oneFinding, llmclient.UsageData{CompletionTokens: 71}, false)(llmclient.Invocation{}, nil, nil)
	}
	a, _ := runDeclared(t, true, chat)
	assert.Equal(t, StatusOK, a.Status)
}

// A short reply on the primary is covered by a working fallback, like any other
// failed status: the exit code reports whether the agent has a working path.
func TestSmokeFloor_AWorkingFallbackCoversAShortPrimary(t *testing.T) {
	short := smokeEndpoint(t, gemma4ShortReply, 4)
	normal := smokeEndpoint(t, oneFinding, 71)
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	reg := regWith(
		map[string]registry.Provider{
			"short":  {APIKeyEnv: rfDoctorEnvK, BaseURL: short.URL},
			"normal": {APIKeyEnv: rfDoctorEnvK, BaseURL: normal.URL},
		},
		map[string]registry.AgentConfig{
			"a":      {Provider: "short", Model: "gemma4:31b", ResponseFormat: registry.ResponseFormatJSONObject, Fallback: "a-back"},
			"a-back": {Provider: "normal", Model: "gemma3:27b", ResponseFormat: registry.ResponseFormatJSONObject},
		},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	rep := Run(context.Background(), llmclient.New(), res, Options{Nonce: testNonce, MaxTokens: 2048})

	assert.Equal(t, 0, rep.ExitCode)
}

// A salvaged marker is judged as a found marker by the zero-budget verdict, which
// rewrites the status of a healthy row. A short_reply row is not healthy, and
// that rewrite must not turn it back into ok_warning while the exit code says 1.
func TestSmokeFloor_SurvivesTheZeroBudgetVerdictOnASalvagedMarker(t *testing.T) {
	t.Setenv(rfDoctorEnvK, rfDoctorKey)
	tiny := 1
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: rfDoctorEnvK, BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {
			Provider: "p", Model: "gemma4:31b", ContextWindowTokens: &tiny,
			ResponseFormat: registry.ResponseFormatJSONObject,
		}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	fake := newFake(nil)
	fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) {
		return llmclient.Completion{Content: Marker(testNonce), Salvaged: true}, nil
	}
	fake.chatFn = usageReply(gemma4ShortReply, llmclient.UsageData{CompletionTokens: 4}, false)

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})

	require.Len(t, rep.Agents, 1)
	assert.Equal(t, 1, rep.ExitCode)
	assert.Equal(t, StatusShortReply, rep.Agents[0].Status, "the row must agree with the exit code")
	assert.Contains(t, rep.Agents[0].Hint, "gemma4:31b")
}
