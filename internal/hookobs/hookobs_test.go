package hookobs

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingObserver captures every observed invocation. The fan-out engine runs
// agents concurrently, so the Observer contract is "safe for concurrent use";
// this double honours it and TestWrap_ConcurrentInvocations proves the
// decorator itself is race-free under -race.
type recordingObserver struct {
	mu          sync.Mutex
	got         []Invocation
	panicOnCall bool
}

func (r *recordingObserver) OnModelInvocation(mi Invocation) {
	if r.panicOnCall {
		panic("observer boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, mi)
}

func (r *recordingObserver) calls() []Invocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Invocation, len(r.got))
	copy(out, r.got)
	return out
}

// chatServer serves one canned chat-completions response. A 4xx is the
// non-retryable failure path; a 5xx would trip the client's retry/backoff loop
// and slow the suite.
func chatServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const okCompletion = `{"choices":[{"message":{"role":"assistant","content":"HIGH: a finding"},` +
	`"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`

func testInvocation(t *testing.T, srv *httptest.Server) llmclient.Invocation {
	t.Helper()
	t.Setenv("ATCR_HOOKOBS_TEST_KEY", "sk-test-not-a-real-key")
	return llmclient.Invocation{
		BaseURL:   srv.URL,
		APIKeyEnv: "ATCR_HOOKOBS_TEST_KEY",
		Model:     "test/model-a",
		Prompt:    "review this diff",
	}
}

func observedCtx(obs Observer, stderr *bytes.Buffer) context.Context {
	return NewContext(context.Background(), obs, stderr)
}

func strPtr(s string) *string { return &s }

// --- Wrap: installation and the default path ------------------------------

// TestWrap_NoObserver_ReturnsUnderlyingClient is the hard guarantee behind the
// "no behaviour change when hooks are unset" contract: the engine must receive
// the exact same *llmclient.Client, so every capability type-assertion it makes
// resolves identically.
func TestWrap_NoObserver_ReturnsUnderlyingClient(t *testing.T) {
	client := llmclient.New()

	got := Wrap(context.Background(), client)

	require.Same(t, client, got, "no observer registered must yield the bare client, not a wrapper")
}

// TestWrap_NilObserver_ReturnsUnderlyingClient covers a context built from a
// zero Hooks: the carrier is present but empty.
func TestWrap_NilObserver_ReturnsUnderlyingClient(t *testing.T) {
	client := llmclient.New()
	ctx := NewContext(context.Background(), nil, &bytes.Buffer{})

	got := Wrap(ctx, client)

	require.Same(t, client, got, "a nil observer must not install a wrapper")
}

func TestWrap_ObserverRegistered_WrapsClient(t *testing.T) {
	client := llmclient.New()

	got := Wrap(observedCtx(&recordingObserver{}, &bytes.Buffer{}), client)

	require.NotSame(t, client, got, "a registered observer must install the decorator")
	require.IsType(t, &observingClient{}, got)
}

// --- payload -------------------------------------------------------------

// TestWrap_FiresOncePerInvocation covers every entry point the engine can
// select. Exactly one observation per model call is the contract a compliance
// ledger depends on: a double-fire duplicates a record, a missing fire loses one.
func TestWrap_FiresOncePerInvocation(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T, ctx context.Context, c Client, inv llmclient.Invocation)
	}{
		{
			name: "Complete",
			call: func(t *testing.T, ctx context.Context, c Client, inv llmclient.Invocation) {
				content, err := c.Complete(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", content)
			},
		},
		{
			name: "CompleteWithUsage",
			call: func(t *testing.T, ctx context.Context, c Client, inv llmclient.Invocation) {
				content, usage, _, err := c.CompleteWithUsage(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", content)
				require.Equal(t, 11, usage.PromptTokens)
			},
		},
		{
			name: "CompleteWithMeta",
			call: func(t *testing.T, ctx context.Context, c Client, inv llmclient.Invocation) {
				comp, err := c.CompleteWithMeta(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", comp.Content)
			},
		},
		{
			name: "Chat",
			call: func(t *testing.T, ctx context.Context, c Client, inv llmclient.Invocation) {
				resp, err := c.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
				require.NoError(t, err)
				require.NotNil(t, resp)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := chatServer(t, http.StatusOK, okCompletion)
			inv := testInvocation(t, srv)
			obs := &recordingObserver{}
			ctx := observedCtx(obs, &bytes.Buffer{})

			tc.call(t, ctx, Wrap(ctx, llmclient.New()), inv)

			calls := obs.calls()
			require.Len(t, calls, 1, "exactly one observation per model invocation")
			assert.Equal(t, "test/model-a", calls[0].Model)
		})
	}
}

func TestWrap_CapturesPayloadFields(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := circuitbreaker.NewContext(observedCtx(obs, &bytes.Buffer{}), "openrouter")

	_, err := Wrap(ctx, llmclient.New()).CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1)
	got := calls[0]

	assert.Equal(t, "test/model-a", got.Model)
	assert.Equal(t, "openrouter", got.Provider, "logical provider comes from the engine's context")
	assert.Equal(t, srv.URL, got.BaseURL)
	assert.Equal(t, "review this diff", got.Prompt)
	assert.Equal(t, "HIGH: a finding", got.Response)
	assert.Equal(t, 11, got.PromptTokens)
	assert.Equal(t, 7, got.CompletionTokens)
	assert.False(t, got.Truncated)
	assert.Empty(t, got.Err)
	assert.False(t, got.StartedAt.IsZero(), "audit records need a per-invocation timestamp")
	assert.Positive(t, got.Duration, "duration must be measured, not left zero")
}

func TestWrap_ProviderFallsBackToBaseURL(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Equal(t, srv.URL, obs.calls()[0].Provider,
		"with no logical provider attached, the endpoint identifies the provider")
}

// TestWrap_ScrubsCredentialsFromEndpoint: llmclient strips userinfo before
// building a request so credentials cannot surface in transport errors. An
// observation is a durable audit record, so it must not reintroduce them.
func TestWrap_ScrubsCredentialsFromEndpoint(t *testing.T) {
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})
	inv := llmclient.Invocation{
		BaseURL:   "https://svcacct:hunter2@gateway.example/v1",
		APIKeyEnv: "ATCR_HOOKOBS_TEST_UNSET_KEY",
		Model:     "test/model-a",
	}

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err, "unset key env: the call fails before any network attempt")

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.NotContains(t, calls[0].BaseURL, "hunter2", "the password must never reach an audit record")
	assert.NotContains(t, calls[0].Provider, "hunter2", "the provider fallback must be scrubbed too")
	assert.Contains(t, calls[0].BaseURL, "gateway.example", "provider identity must survive the scrub")
}

// TestWrap_UnparseableBaseURLPreserved: identity is more useful than a blank.
func TestWrap_UnparseableBaseURLPreserved(t *testing.T) {
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})
	inv := llmclient.Invocation{
		BaseURL:   "://not a url",
		APIKeyEnv: "ATCR_HOOKOBS_TEST_UNSET_KEY",
		Model:     "test/model-a",
	}

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Equal(t, "://not a url", obs.calls()[0].BaseURL)
}

func TestWrap_MissingUsageBlock_ZeroTokens(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Zero(t, obs.calls()[0].PromptTokens)
	assert.Zero(t, obs.calls()[0].CompletionTokens)
	assert.Equal(t, "ok", obs.calls()[0].Response)
}

func TestWrap_TruncatedResponse_PreservesPartialContent(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"HIGH: partial"},"finish_reason":"length"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.True(t, obs.calls()[0].Truncated)
	assert.Equal(t, "length", obs.calls()[0].FinishReason)
	assert.Equal(t, "HIGH: partial", obs.calls()[0].Response)
}

func TestWrap_EmptyPrompt(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	inv.Prompt = ""
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1, "an empty prompt is still an observable invocation")
	assert.Empty(t, obs.calls()[0].Prompt)
}

// --- failure paths fire exactly once --------------------------------------

// TestWrap_FailedInvocation_FiresOnceWithError: a non-firing-on-failure design
// makes a failed-invocation audit record impossible to build.
func TestWrap_FailedInvocation_FiresOnceWithError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err, "the underlying failure must still surface to the caller")

	calls := obs.calls()
	require.Len(t, calls, 1, "a failed invocation fires exactly once")
	assert.NotEmpty(t, calls[0].Err)
	assert.Equal(t, "test/model-a", calls[0].Model, "model identity survives the failure path")
}

// TestWrap_KeyResolutionFailure_FiresOnce covers the failure before any HTTP
// attempt: no wire call, but still one invocation a ledger must account for.
func TestWrap_KeyResolutionFailure_FiresOnce(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	inv.APIKeyEnv = "ATCR_HOOKOBS_TEST_UNSET_KEY"
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err)

	require.Len(t, obs.calls(), 1, "a pre-wire failure is still exactly one invocation")
	assert.NotEmpty(t, obs.calls()[0].Err)
	assert.Empty(t, obs.calls()[0].Response)
}

func TestWrap_FailedChatTurn_FiresOnceWithError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Chat(ctx, inv,
		[]llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
	require.Error(t, err)

	require.Len(t, obs.calls(), 1)
	assert.NotEmpty(t, obs.calls()[0].Err)
}

// TestWrap_ChatToolCallTurn_NoNilDeref: an assistant turn requesting tools
// carries content:null. Dereferencing it blindly panics inside the review
// lifecycle, so it must be flattened to an empty response.
func TestWrap_ChatToolCallTurn_NoNilDeref(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[`+
			`{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}]},`+
			`"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	resp, err := Wrap(ctx, llmclient.New()).Chat(ctx, inv,
		[]llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, obs.calls(), 1)
	assert.Empty(t, obs.calls()[0].Response,
		"a content:null tool-call turn records an empty response, not a panic")
	assert.Equal(t, "tool_calls", obs.calls()[0].FinishReason)
}

// TestWrap_ChatRecordsMessageHistory covers the multi-turn payload, including a
// nil-content assistant turn in the history being flattened rather than
// dereferenced.
func TestWrap_ChatRecordsMessageHistory(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	history := []llmclient.Message{
		{Role: "user", Content: strPtr("find bugs")},
		{Role: "assistant", Content: nil},
		{Role: "tool", Content: strPtr("file contents"), ToolCallID: "call_1"},
	}
	_, err := Wrap(ctx, llmclient.New()).Chat(ctx, inv, history, nil)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	got := obs.calls()[0].Messages
	require.Len(t, got, 3)
	assert.Equal(t, Message{Role: "user", Content: "find bugs"}, got[0])
	assert.Equal(t, Message{Role: "assistant", Content: ""}, got[1], "content:null flattens to empty")
	assert.Equal(t, Message{Role: "tool", Content: "file contents", ToolCallID: "call_1"}, got[2],
		"a tool result must keep the id linking it back to the call that produced it")
}

// TestWrap_ChatRecordsRequestedToolCalls: a tool-enabled agent's response is
// frequently a tool call with no text, so a record carrying only Response would
// show an empty exchange and lose which tool ran with which arguments.
func TestWrap_ChatRecordsRequestedToolCalls(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[`+
			`{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}]},`+
			`"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).Chat(ctx, inv,
		[]llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	require.Len(t, obs.calls()[0].ResponseToolCalls, 1)
	assert.Equal(t, ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		obs.calls()[0].ResponseToolCalls[0])
}

// TestWrap_RecordsCallIdentity: without run/agent/stage an observer receives an
// interleaved flat stream it cannot group into a per-run compliance record.
func TestWrap_RecordsCallIdentity(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := WithCall(observedCtx(obs, &bytes.Buffer{}),
		Call{RunID: "2026-07-25_feat-x", AgentName: "security-reviewer", Stage: "review"})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Equal(t, "2026-07-25_feat-x", obs.calls()[0].RunID)
	assert.Equal(t, "security-reviewer", obs.calls()[0].AgentName)
	assert.Equal(t, "review", obs.calls()[0].Stage)
}

// TestWithCall_MergesAcrossLayers: cli contributes RunID/Stage, the engine
// contributes AgentName, and verify/debate refine Stage. A later layer must not
// erase an earlier layer's contribution, whatever the order.
func TestWithCall_MergesAcrossLayers(t *testing.T) {
	ctx := WithCall(context.Background(), Call{RunID: "run-1", Stage: "review"})
	ctx = WithCall(ctx, Call{AgentName: "agent-a"})
	ctx = WithCall(ctx, Call{Stage: "verify"})

	got := CallFrom(ctx)

	assert.Equal(t, Call{RunID: "run-1", AgentName: "agent-a", Stage: "verify"}, got,
		"empty fields must not overwrite; non-empty must win")
}

func TestWithCall_NilContext(t *testing.T) {
	assert.NotPanics(t, func() { WithCall(nil, Call{RunID: "x"}) }) //nolint:staticcheck // nil ctx is the case under test
	assert.Equal(t, Call{}, CallFrom(nil))                          //nolint:staticcheck // SA1012: nil ctx is the case under test
}

// TestWrap_NilContext: cobra hands a nil context to a command that was never
// executed, a condition the previous bare llmclient.New() call sites were
// immune to.
func TestWrap_NilContext(t *testing.T) {
	client := llmclient.New()
	var got Client
	require.NotPanics(t, func() { got = Wrap(nil, client) }) //nolint:staticcheck // nil ctx is the case under test
	assert.Same(t, client, got)
}

// TestWrap_RecordsSequence gives a consumer a total order over a stream that
// concurrency and equal timestamps otherwise leave ambiguous.
func TestWrap_RecordsSequence(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})
	c := Wrap(ctx, llmclient.New())

	for i := 0; i < 3; i++ {
		_, err := c.Complete(ctx, inv)
		require.NoError(t, err)
	}

	calls := obs.calls()
	require.Len(t, calls, 3)
	assert.Less(t, calls[0].Seq, calls[1].Seq, "sequence must be monotonic")
	assert.Less(t, calls[1].Seq, calls[2].Seq)
}

// TestWrap_RecordsCostAndRequestParams: the rate table lives in an internal
// package, so a separate module cannot compute cost itself — it has to arrive
// on the payload or be forked downstream.
func TestWrap_RecordsCostAndRequestParams(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	temp := 0.2
	maxTok := 4096
	inv.Temperature = &temp
	inv.MaxTokens = &maxTok
	inv.Model = "anthropic/claude-sonnet-4.5"
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	got := obs.calls()[0]
	require.NotNil(t, got.Temperature)
	assert.InDelta(t, 0.2, *got.Temperature, 1e-9)
	require.NotNil(t, got.MaxTokens)
	assert.Equal(t, 4096, *got.MaxTokens)
	assert.Positive(t, got.Attempts, "at least one wire attempt must be reported")
	assert.Equal(t, llmclient.ComputeCostUSD("anthropic/claude-sonnet-4.5", 11, 7), got.CostUSD)
}

// TestWrap_UnknownModelCostIsZero: an unpriced model must report zero rather
// than a fabricated figure.
func TestWrap_UnknownModelCostIsZero(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Zero(t, obs.calls()[0].CostUSD, "an unpriced model must not report an invented cost")
}

// TestWrap_ChatFiresPerTurn: the tool loop calls Chat repeatedly; each turn is
// its own model invocation and its own record.
func TestWrap_ChatFiresPerTurn(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})
	c := Wrap(ctx, llmclient.New())

	for i := 0; i < 3; i++ {
		_, err := c.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("turn")}}, nil)
		require.NoError(t, err)
	}

	assert.Len(t, obs.calls(), 3, "one observation per chat turn, not one per loop")
}

// --- consumer panics are contained ----------------------------------------

// TestWrap_PanickingObserver_Recovered: a third-party consumer panic must never
// propagate into the CLI lifecycle and change the run's outcome.
func TestWrap_PanickingObserver_Recovered(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	var stderr bytes.Buffer
	ctx := observedCtx(&recordingObserver{panicOnCall: true}, &stderr)

	var content string
	var err error
	require.NotPanics(t, func() {
		content, err = Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	}, "a panicking consumer hook must be recovered at the seam")

	require.NoError(t, err, "the run's outcome must be unaffected by a broken hook")
	assert.Equal(t, "HIGH: a finding", content, "the model result must reach the caller unchanged")
	assert.Contains(t, stderr.String(), "audit hook",
		"the recovered panic must be reported on stderr, not swallowed silently")
}

// TestWrap_PanickingObserver_NilStderrDoesNotRepanic: the report happens inside
// the deferred recover, so a nil writer there would replace the panic just
// contained and kill the process — the outcome the recovery exists to prevent.
func TestWrap_PanickingObserver_NilStderrDoesNotRepanic(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	ctx := NewContext(context.Background(), &recordingObserver{panicOnCall: true}, nil)

	var err error
	require.NotPanics(t, func() {
		_, err = Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	}, "a nil stderr must not turn a contained hook panic into a process kill")
	require.NoError(t, err)
}

// TestWrap_PanickingObserver_DoesNotMaskUnderlyingError: recovery must not
// convert a genuine model failure into a success.
func TestWrap_PanickingObserver_DoesNotMaskUnderlyingError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	var stderr bytes.Buffer
	ctx := observedCtx(&recordingObserver{panicOnCall: true}, &stderr)

	var err error
	require.NotPanics(t, func() {
		_, err = Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	})

	require.Error(t, err, "hook recovery must not swallow the underlying invocation error")
}

// --- concurrency -----------------------------------------------------------

// TestWrap_ConcurrentInvocations: the engine fans agents out in parallel, so
// the decorator must add no shared mutable state of its own. Under -race this
// also proves the decorator is race-free.
func TestWrap_ConcurrentInvocations(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})
	c := Wrap(ctx, llmclient.New())

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.Complete(ctx, inv)
		}()
	}
	wg.Wait()

	assert.Len(t, obs.calls(), n, "every concurrent invocation must be observed exactly once")
}

// TestWrap_ConcurrentPanickingObserver: N goroutines each panicking means N
// concurrent writes to the shared stderr writer. Under -race this proves the
// panic-path write is serialized.
func TestWrap_ConcurrentPanickingObserver(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	var stderr bytes.Buffer
	ctx := observedCtx(&recordingObserver{panicOnCall: true}, &stderr)
	c := Wrap(ctx, llmclient.New())

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.Complete(ctx, inv)
		}()
	}
	wg.Wait()

	assert.Equal(t, n, bytes.Count(stderr.Bytes(), []byte("audit hook")),
		"every recovered panic must be reported exactly once, without interleaving")
}

// --- merge rules across layers --------------------------------------------

// TestWithCall_RunIDIsOutermostWins is the regression test for a correlation
// split that only appears on a supported flag combination. cli stamps the
// derived review id; verify and debate stamp the review directory's basename.
// Those coincide on the default layout, but `--output-dir out/foo` keeps the
// operator's path while the id stays the derived ReviewID — so an inner
// overwrite would file one run's review-stage and verify-stage records under
// two different ids, which is exactly the correlation failure RunID exists to
// prevent. The default path masks it, so it needs its own test.
func TestWithCall_RunIDIsOutermostWins(t *testing.T) {
	ctx := WithCall(context.Background(), Call{RunID: "2026-07-25_main", Stage: "review"})
	ctx = WithCall(ctx, Call{RunID: "foo", Stage: "verify"}) // inner layer, dir basename

	got := CallFrom(ctx)

	assert.Equal(t, "2026-07-25_main", got.RunID,
		"an inner layer must not resplit one run's records under a second id")
	assert.Equal(t, "verify", got.Stage, "the inner layer must still refine the stage")
}

// TestWithCall_InnerRunIDAppliesWhenNoOuter covers standalone `atcr verify` /
// `atcr debate`, where no outer layer supplied an id.
func TestWithCall_InnerRunIDAppliesWhenNoOuter(t *testing.T) {
	ctx := WithCall(context.Background(), Call{RunID: "2026-07-25_main", Stage: "verify"})

	assert.Equal(t, "2026-07-25_main", CallFrom(ctx).RunID)
}

// TestWithCall_StageChainRetainsRunID walks the real review→verify→autofix
// chain and asserts the whole chain stays correlated under one id.
func TestWithCall_StageChainRetainsRunID(t *testing.T) {
	ctx := WithCall(context.Background(), Call{RunID: "run-1", Stage: "review"})
	ctx = WithCall(ctx, Call{AgentName: "security-reviewer"})
	verifyCtx := WithCall(ctx, Call{RunID: "some-dir", Stage: "verify"})
	fixCtx := WithCall(verifyCtx, Call{Stage: "autofix"})

	for name, c := range map[string]Call{"review": CallFrom(ctx), "verify": CallFrom(verifyCtx), "autofix": CallFrom(fixCtx)} {
		assert.Equal(t, "run-1", c.RunID, "%s stage lost the run correlation", name)
	}
	assert.Equal(t, "verify", CallFrom(verifyCtx).Stage)
	assert.Equal(t, "autofix", CallFrom(fixCtx).Stage)
}

// --- observer isolation ----------------------------------------------------

// TestWrap_SamplingParamsAreSnapshotNotAliased: for fan-out agents these
// pointers belong to the loaded registry config and are shared by every
// invocation of that agent for the process's lifetime. Handing the raw pointer
// to an observer would let it write through and reconfigure every subsequent
// model call, breaking the guarantee that an observer cannot alter the run it
// is observing.
func TestWrap_SamplingParamsAreSnapshotNotAliased(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	sharedTemp := 0.2
	sharedMax := 4096
	inv.Temperature = &sharedTemp
	inv.MaxTokens = &sharedMax
	obs := &recordingObserver{}
	ctx := observedCtx(obs, &bytes.Buffer{})

	_, err := Wrap(ctx, llmclient.New()).CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	got := obs.calls()[0]
	require.NotNil(t, got.Temperature)
	require.NotNil(t, got.MaxTokens)
	assert.NotSame(t, &sharedTemp, got.Temperature, "must be a snapshot, not an alias into shared config")
	assert.NotSame(t, &sharedMax, got.MaxTokens)

	// A hostile (or merely careless) observer writing through the pointer must
	// not reconfigure the caller's config.
	*got.Temperature = 1.9
	*got.MaxTokens = 1
	assert.InDelta(t, 0.2, sharedTemp, 1e-9, "an observer must not be able to alter the run's sampling config")
	assert.Equal(t, 4096, sharedMax)
}

// TestWrap_AttemptsReportedOnEveryPath: Attempts is documented without
// qualification, so a path reporting 0 would read as "no transmission
// occurred" in the ledger the field was added for.
func TestWrap_AttemptsReportedOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(c Client, ctx context.Context, inv llmclient.Invocation) error
	}{
		{"Complete", func(c Client, ctx context.Context, inv llmclient.Invocation) error {
			_, err := c.Complete(ctx, inv)
			return err
		}},
		{"CompleteWithUsage", func(c Client, ctx context.Context, inv llmclient.Invocation) error {
			_, _, _, err := c.CompleteWithUsage(ctx, inv)
			return err
		}},
		{"CompleteWithMeta", func(c Client, ctx context.Context, inv llmclient.Invocation) error {
			_, err := c.CompleteWithMeta(ctx, inv)
			return err
		}},
		{"Chat", func(c Client, ctx context.Context, inv llmclient.Invocation) error {
			_, err := c.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := chatServer(t, http.StatusOK, okCompletion)
			inv := testInvocation(t, srv)
			obs := &recordingObserver{}
			ctx := observedCtx(obs, &bytes.Buffer{})

			require.NoError(t, tc.call(Wrap(ctx, llmclient.New()), ctx, inv))

			require.Len(t, obs.calls(), 1)
			assert.Equal(t, 1, obs.calls()[0].Attempts, "one wire attempt must be reported on every path")
		})
	}
}

// TestWrap_ConcurrentPanicsAcrossDecorators: Wrap is called at several
// independent sites (cli, verify's pipeline and its fix executor, debate), each
// producing its own decorator around the SAME caller-supplied stderr. Under
// `atcr serve` two parallel verify/debate tool calls do exactly that, so the
// panic-path lock has to be shared through the context rather than owned by
// each decorator. Under -race this fails if it is not.
func TestWrap_ConcurrentPanicsAcrossDecorators(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	var stderr bytes.Buffer
	ctx := observedCtx(&recordingObserver{panicOnCall: true}, &stderr)

	// Two decorators from one NewContext, as the real call sites produce.
	a := Wrap(ctx, llmclient.New())
	b := Wrap(ctx, llmclient.New())
	require.NotSame(t, a, b, "the sites build independent decorators")

	const n = 8
	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() { defer wg.Done(); _, _ = a.Complete(ctx, inv) }()
		go func() { defer wg.Done(); _, _ = b.Complete(ctx, inv) }()
	}
	wg.Wait()

	assert.Equal(t, 2*n, bytes.Count(stderr.Bytes(), []byte("audit hook")),
		"every recovered panic must be reported exactly once across all decorators")
}
