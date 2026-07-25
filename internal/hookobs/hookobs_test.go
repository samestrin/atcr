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
	assert.Equal(t, Message{Role: "tool", Content: "file contents"}, got[2])
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
