package cli

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---------------------------------------------------------

// recordingObserver captures every observed invocation. The fan-out engine runs
// agents concurrently, so the observer contract is "safe for concurrent use";
// this double honours it and the concurrency test asserts the guarantee holds
// under -race.
type recordingObserver struct {
	mu          sync.Mutex
	got         []ModelInvocation
	panicOnCall bool
}

func (r *recordingObserver) OnModelInvocation(mi ModelInvocation) {
	if r.panicOnCall {
		panic("observer boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, mi)
}

func (r *recordingObserver) calls() []ModelInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ModelInvocation, len(r.got))
	copy(out, r.got)
	return out
}

// chatServer serves one canned chat-completions response. Status 200 with a
// well-formed body is the happy path; a 4xx is the non-retryable failure path
// (5xx would trip the client's retry/backoff loop and slow the suite).
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

// testInvocation builds an Invocation pointed at srv with a resolvable key env.
func testInvocation(t *testing.T, srv *httptest.Server) llmclient.Invocation {
	t.Helper()
	t.Setenv("ATCR_HOOKS_TEST_KEY", "sk-test-not-a-real-key")
	return llmclient.Invocation{
		BaseURL:   srv.URL,
		APIKeyEnv: "ATCR_HOOKS_TEST_KEY",
		Model:     "test/model-a",
		Prompt:    "review this diff",
	}
}

// hookedCtx returns a context carrying obs as the registered observer.
func hookedCtx(obs ModelInvocationObserver, stderr *bytes.Buffer) context.Context {
	return withHooks(context.Background(), Hooks{ModelInvocation: obs}, stderr)
}

// --- AC 01-01: exported entry point, no internal/ in the public surface ----

// TestObservedCompleter_NoObserver_ReturnsUnderlyingClient is the hard guarantee
// behind AC 01-03: with no hook registered the engine must receive the exact
// same *llmclient.Client it receives today, so every type assertion the engine
// makes (MetaCompleter, UsageCompleter, ChatCompleter) resolves identically.
func TestObservedCompleter_NoObserver_ReturnsUnderlyingClient(t *testing.T) {
	client := llmclient.New()

	got := observedCompleter(context.Background(), client)

	require.Same(t, client, got, "no hook registered must yield the bare client, not a wrapper")
}

// TestObservedCompleter_NilObserverInHooks_ReturnsUnderlyingClient covers the
// zero-value Hooks{} case: MainWithHooks(ctx, out, err, Hooks{}) must be
// indistinguishable from Main.
func TestObservedCompleter_NilObserverInHooks_ReturnsUnderlyingClient(t *testing.T) {
	client := llmclient.New()
	ctx := withHooks(context.Background(), Hooks{}, &bytes.Buffer{})

	got := observedCompleter(ctx, client)

	require.Same(t, client, got, "zero-value Hooks must not install a wrapper")
}

// TestObservedCompleter_ObserverRegistered_WrapsClient is the positive case.
func TestObservedCompleter_ObserverRegistered_WrapsClient(t *testing.T) {
	client := llmclient.New()
	obs := &recordingObserver{}

	got := observedCompleter(hookedCtx(obs, &bytes.Buffer{}), client)

	require.NotSame(t, client, got, "a registered observer must install the decorator")
}

// TestObservingCompleter_SatisfiesCompleterFamily guards the subtlest failure
// mode in this design: the engine picks its call path by type assertion
// (MetaCompleter first, then UsageCompleter, then plain Complete; ChatCompleter
// for tool-enabled agents). A decorator that implements only Complete would
// silently downgrade every agent to the no-usage, no-truncation path — the hook
// would still fire, but with zero token counts, making AC 01-02 and the
// downstream audit record structurally wrong rather than visibly broken.
func TestObservingCompleter_SatisfiesCompleterFamily(t *testing.T) {
	c := observedCompleter(hookedCtx(&recordingObserver{}, &bytes.Buffer{}), llmclient.New())

	_, isCompleter := c.(fanout.Completer)
	_, isUsage := c.(fanout.UsageCompleter)
	_, isMeta := c.(fanout.MetaCompleter)
	_, isChat := c.(fanout.ChatCompleter)

	assert.True(t, isCompleter, "decorator must satisfy fanout.Completer")
	assert.True(t, isUsage, "decorator must satisfy fanout.UsageCompleter")
	assert.True(t, isMeta, "decorator must satisfy fanout.MetaCompleter")
	assert.True(t, isChat, "decorator must satisfy fanout.ChatCompleter")
}

// TestHooks_ExportedSurfaceHasNoInternalTypes enforces AC 01-01's core
// requirement structurally: atcr-enterprise is a separate module, so any
// internal/ type appearing in an exported signature makes the hook unusable no
// matter how well documented it is.
func TestHooks_ExportedSurfaceHasNoInternalTypes(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "hooks.go", nil, 0)
	require.NoError(t, err, "cli/hooks.go must exist and parse")

	internalPkgs := map[string]bool{"llmclient": true, "fanout": true, "telemetry": true,
		"log": true, "report": true, "circuitbreaker": true}

	var offenders []string
	checkExpr := func(where string, e ast.Expr) {
		ast.Inspect(e, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok && internalPkgs[ident.Name] {
				offenders = append(offenders, fmt.Sprintf("%s: %s.%s", where, ident.Name, sel.Sel.Name))
			}
			return true
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || !d.Name.IsExported() {
				continue
			}
			checkExpr("func "+d.Name.Name, d.Type)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				checkExpr("type "+ts.Name.Name, ts.Type)
			}
		}
	}

	assert.Empty(t, offenders,
		"exported hook surface must reference no internal/ type — a separate module cannot import them")
}

// --- AC 01-02: payload data ------------------------------------------------

// TestObservingCompleter_FiresOncePerInvocation covers every entry point the
// engine can select. Exactly one observation per model call is the contract the
// downstream audit ledger depends on: a double-fire duplicates a compliance
// record, a missing fire loses one.
func TestObservingCompleter_FiresOncePerInvocation(t *testing.T) {
	tests := []struct {
		name string
		call func(t *testing.T, ctx context.Context, c fanout.Completer, inv llmclient.Invocation)
	}{
		{
			name: "Complete",
			call: func(t *testing.T, ctx context.Context, c fanout.Completer, inv llmclient.Invocation) {
				content, err := c.Complete(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", content)
			},
		},
		{
			name: "CompleteWithUsage",
			call: func(t *testing.T, ctx context.Context, c fanout.Completer, inv llmclient.Invocation) {
				uc, ok := c.(fanout.UsageCompleter)
				require.True(t, ok)
				content, usage, _, err := uc.CompleteWithUsage(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", content)
				require.Equal(t, 11, usage.PromptTokens)
			},
		},
		{
			name: "CompleteWithMeta",
			call: func(t *testing.T, ctx context.Context, c fanout.Completer, inv llmclient.Invocation) {
				mc, ok := c.(fanout.MetaCompleter)
				require.True(t, ok)
				comp, err := mc.CompleteWithMeta(ctx, inv)
				require.NoError(t, err)
				require.Equal(t, "HIGH: a finding", comp.Content)
			},
		},
		{
			name: "Chat",
			call: func(t *testing.T, ctx context.Context, c fanout.Completer, inv llmclient.Invocation) {
				cc, ok := c.(fanout.ChatCompleter)
				require.True(t, ok)
				resp, err := cc.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
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
			ctx := hookedCtx(obs, &bytes.Buffer{})

			c := observedCompleter(ctx, llmclient.New())
			tc.call(t, ctx, c, inv)

			calls := obs.calls()
			require.Len(t, calls, 1, "exactly one observation per model invocation")
			assert.Equal(t, "test/model-a", calls[0].Model)
		})
	}
}

// TestObservingCompleter_CapturesPayloadFields is AC 01-02's substance: the
// observation must carry enough to build a compliance record without the
// consumer reaching into internal/.
func TestObservingCompleter_CapturesPayloadFields(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := circuitbreaker.NewContext(hookedCtx(obs, &bytes.Buffer{}), "openrouter")

	c := observedCompleter(ctx, llmclient.New())
	mc := c.(fanout.MetaCompleter)
	_, err := mc.CompleteWithMeta(ctx, inv)
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

// TestObservingCompleter_ProviderFallsBackToBaseURL covers the doctor-style path
// where no logical provider is attached to the context.
func TestObservingCompleter_ProviderFallsBackToBaseURL(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	_, err := observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, srv.URL, calls[0].Provider,
		"with no logical provider on the context, the endpoint identifies the provider")
}

// TestObservingCompleter_MissingUsageBlock_ZeroTokens: a provider omitting usage
// is graceful degradation, not an error, and must not suppress the observation.
func TestObservingCompleter_MissingUsageBlock_ZeroTokens(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	_, err := observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.Zero(t, calls[0].PromptTokens)
	assert.Zero(t, calls[0].CompletionTokens)
	assert.Equal(t, "ok", calls[0].Response)
}

// TestObservingCompleter_TruncatedResponse_PreservesPartialContent: a
// finish_reason=length response is partial but must still be recorded verbatim,
// flagged as truncated.
func TestObservingCompleter_TruncatedResponse_PreservesPartialContent(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"HIGH: partial"},"finish_reason":"length"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	mc := observedCompleter(ctx, llmclient.New()).(fanout.MetaCompleter)
	_, err := mc.CompleteWithMeta(ctx, inv)
	require.NoError(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Truncated)
	assert.Equal(t, "HIGH: partial", calls[0].Response)
}

// TestObservingCompleter_EmptyPrompt covers the empty-input boundary: an empty
// prompt is still an invocation and still produces a record.
func TestObservingCompleter_EmptyPrompt(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	inv.Prompt = ""
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	_, err := observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1, "an empty prompt is still an observable invocation")
	assert.Empty(t, obs.calls()[0].Prompt)
}

// --- AC 01-02 edge case 3: failed invocations fire exactly once ------------

// TestObservingCompleter_FailedInvocation_FiresOnceWithError is binding on this
// phase (sprint-plan.md Phase 1 note 1): a non-firing-on-failure design makes
// the downstream failed-invocation record impossible to build.
func TestObservingCompleter_FailedInvocation_FiresOnceWithError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	_, err := observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err, "the underlying failure must still surface to the caller")

	calls := obs.calls()
	require.Len(t, calls, 1, "a failed invocation fires the hook exactly once")
	assert.NotEmpty(t, calls[0].Err, "the error must be recorded on the observation")
	assert.Equal(t, "test/model-a", calls[0].Model, "model identity survives the failure path")
}

// TestObservingCompleter_KeyResolutionFailure_FiresOnce covers the failure that
// happens before any HTTP attempt: no wire call, but still one invocation that
// a compliance ledger must account for.
func TestObservingCompleter_KeyResolutionFailure_FiresOnce(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	inv.APIKeyEnv = "ATCR_HOOKS_TEST_UNSET_KEY"
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	_, err := observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	require.Error(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1, "a pre-wire failure is still exactly one invocation")
	assert.NotEmpty(t, calls[0].Err)
	assert.Empty(t, calls[0].Response)
}

// TestObservingCompleter_FailedChatTurn_FiresOnceWithError applies the same
// contract to the multi-turn path.
func TestObservingCompleter_FailedChatTurn_FiresOnceWithError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	cc := observedCompleter(ctx, llmclient.New()).(fanout.ChatCompleter)
	_, err := cc.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
	require.Error(t, err)

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.NotEmpty(t, calls[0].Err)
}

// TestObservingCompleter_ChatToolCallTurn_NoNilDeref: an assistant turn that
// requests tools carries content:null. Dereferencing it blindly panics inside
// the review lifecycle, so the decorator must treat it as an empty response.
func TestObservingCompleter_ChatToolCallTurn_NoNilDeref(t *testing.T) {
	srv := chatServer(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[`+
			`{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"}}]},`+
			`"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	cc := observedCompleter(ctx, llmclient.New()).(fanout.ChatCompleter)
	resp, err := cc.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("hi")}}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	calls := obs.calls()
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].Response, "a content:null tool-call turn records an empty response, not a panic")
}

// TestObservingCompleter_ChatFiresPerTurn: the tool loop calls Chat repeatedly;
// each turn is its own model invocation and its own record.
func TestObservingCompleter_ChatFiresPerTurn(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})

	cc := observedCompleter(ctx, llmclient.New()).(fanout.ChatCompleter)
	for i := 0; i < 3; i++ {
		_, err := cc.Chat(ctx, inv, []llmclient.Message{{Role: "user", Content: strPtr("turn")}}, nil)
		require.NoError(t, err)
	}

	assert.Len(t, obs.calls(), 3, "one observation per chat turn, not one per loop")
}

// --- AC 01-01 error scenario 1: consumer panics are contained --------------

// TestObservingCompleter_PanickingObserver_Recovered is binding on this phase
// (sprint-plan.md Phase 1 note 2). A third-party consumer panic must never
// propagate into cli.Main's lifecycle and change the run's outcome.
func TestObservingCompleter_PanickingObserver_Recovered(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{panicOnCall: true}
	var stderr bytes.Buffer
	ctx := hookedCtx(obs, &stderr)

	var content string
	var err error
	require.NotPanics(t, func() {
		content, err = observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	}, "a panicking consumer hook must be recovered at the seam")

	require.NoError(t, err, "the run's outcome must be unaffected by a broken hook")
	assert.Equal(t, "HIGH: a finding", content, "the model result must reach the caller unchanged")
	assert.Contains(t, stderr.String(), "audit hook",
		"the recovered panic must be reported on stderr, not swallowed silently")
}

// TestObservingCompleter_PanickingObserver_DoesNotMaskUnderlyingError: recovery
// must not convert a genuine model failure into a success.
func TestObservingCompleter_PanickingObserver_DoesNotMaskUnderlyingError(t *testing.T) {
	srv := chatServer(t, http.StatusBadRequest, `{"error":"bad request"}`)
	inv := testInvocation(t, srv)
	var stderr bytes.Buffer
	ctx := hookedCtx(&recordingObserver{panicOnCall: true}, &stderr)

	var err error
	require.NotPanics(t, func() {
		_, err = observedCompleter(ctx, llmclient.New()).Complete(ctx, inv)
	})

	require.Error(t, err, "hook recovery must not swallow the underlying invocation error")
}

// --- concurrency -----------------------------------------------------------

// TestObservingCompleter_ConcurrentInvocations: the engine fans agents out in
// parallel, so the decorator must add no shared mutable state of its own. Run
// under -race this also proves the decorator itself is race-free.
func TestObservingCompleter_ConcurrentInvocations(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := hookedCtx(obs, &bytes.Buffer{})
	c := observedCompleter(ctx, llmclient.New())

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

// --- AC 01-03: default behaviour compatibility -----------------------------

// TestMainWithHooks_ZeroHooks_MatchesMain is AC 01-03's regression gate: with no
// hook registered the new entry point must be byte-for-byte indistinguishable
// from cli.Main on stdout, stderr, and exit code.
func TestMainWithHooks_ZeroHooks_MatchesMain(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "version", args: []string{"atcr", "--version"}},
		{name: "help", args: []string{"atcr", "--help"}},
		{name: "unknown subcommand", args: []string{"atcr", "definitely-not-a-command"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			t.Cleanup(func() { os.Args = origArgs })

			os.Args = tc.args
			var mainOut, mainErr bytes.Buffer
			mainCode := Main(context.Background(), &mainOut, &mainErr)

			os.Args = tc.args
			var hookOut, hookErr bytes.Buffer
			hookCode := MainWithHooks(context.Background(), &hookOut, &hookErr, Hooks{})

			assert.Equal(t, mainCode, hookCode, "exit code must be unchanged")
			assert.Equal(t, mainOut.String(), hookOut.String(), "stdout must be byte-for-byte unchanged")
			assert.Equal(t, mainErr.String(), hookErr.String(), "stderr must be byte-for-byte unchanged")
		})
	}
}

// TestMainWithHooks_RegistersObserverOnContext proves the hooks value actually
// reaches the completer construction seam rather than being accepted and
// dropped — the failure mode a signature-only test would miss.
func TestMainWithHooks_RegistersObserverOnContext(t *testing.T) {
	obs := &recordingObserver{}
	ctx := withHooks(context.Background(), Hooks{ModelInvocation: obs}, &bytes.Buffer{})

	h, ok := hooksFrom(ctx)
	require.True(t, ok, "hooks must be retrievable from the context they were installed on")
	require.Same(t, obs, h.hooks.ModelInvocation)
}

// --- wiring: every fanout completer call site is observed ------------------

// TestHooks_FanoutCallSitesUseObservedCompleter is the structural guard behind
// "every model invocation is observed". The seam sits at the fanout.Completer
// boundary, so any cli file that hands a bare llmclient.New() to fanout bypasses
// the hook entirely — and would do so silently. doctor.go is the one documented
// exception: internal/doctor defines its own local Completer and never routes
// through internal/fanout, so this seam structurally cannot observe it.
func TestHooks_FanoutCallSitesUseObservedCompleter(t *testing.T) {
	const exempt = "doctor.go" // structurally out of reach: own Completer, no fanout import
	entries, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var offenders []string
	for _, path := range entries {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") || base == exempt || base == "hooks.go" {
			continue
		}
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		if bytes.Contains(src, []byte("llmclient.New()")) {
			offenders = append(offenders, base)
		}
	}

	assert.Empty(t, offenders,
		"these files construct a bare completer and bypass the hook seam; route them through newCompleter(ctx)")
}

// TestHooks_DoctorExemptionIsStillAccurate keeps the exemption above honest: if
// doctor ever starts routing through internal/fanout, the exemption silently
// becomes an audit gap, so the assumption is asserted rather than assumed.
func TestHooks_DoctorExemptionIsStillAccurate(t *testing.T) {
	src, err := os.ReadFile("doctor.go")
	require.NoError(t, err)

	assert.NotContains(t, string(src), "fanout.",
		"doctor.go now touches fanout — revisit the hook-coverage exemption in hooks.go")
}

// strPtr is a local helper for the pointer-valued chat message content field.
func strPtr(s string) *string { return &s }
