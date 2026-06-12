package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testNonce = "nonce123"

// fakeCompleter records calls per model and returns a scripted result.
type fakeCompleter struct {
	mu    sync.Mutex
	calls map[string]int
	fn    func(inv llmclient.Invocation) (string, error)
}

func newFake(fn func(inv llmclient.Invocation) (string, error)) *fakeCompleter {
	return &fakeCompleter{calls: map[string]int{}, fn: fn}
}

func (f *fakeCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	f.mu.Lock()
	f.calls[inv.Model]++
	f.mu.Unlock()
	return f.fn(inv)
}

func (f *fakeCompleter) count(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[model]
}

func twoAgentSharedTarget(t *testing.T) *Resolution {
	t.Helper()
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "m"},
			"b": {Provider: "p", Model: "m"},
		},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a", "b"}})
	require.NoError(t, err)
	return res
}

func TestRun_SharedTargetInvokedOnce(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := twoAgentSharedTarget(t)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	assert.Equal(t, 1, fake.count("m"), "a shared target must be invoked at most once")
	require.Len(t, rep.Agents, 2)
	for _, ar := range rep.Agents {
		assert.Equal(t, StatusOK, ar.Status)
	}
	assert.Equal(t, 0, rep.ExitCode)
}

func TestRun_MarkerAbsentIsWarning(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := twoAgentSharedTarget(t)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", nil // HTTP 200 but empty content
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 8})
	assert.Equal(t, StatusOKWarning, rep.Agents[0].Status)
	assert.Contains(t, rep.Agents[0].Hint, "max-tokens")
	assert.Equal(t, 0, rep.ExitCode, "a warning still counts as a working path")
}

func TestRun_TokenBudgetAffectsOutcome(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := twoAgentSharedTarget(t)
	// budgetAwareFake simulates a reasoning model that spends the token budget
	// on thinking: it returns the marker only when MaxTokens >= 1024, and
	// empty content when the budget is too small to survive reasoning overhead.
	budgetAwareFake := newFake(func(inv llmclient.Invocation) (string, error) {
		if inv.MaxTokens != nil && *inv.MaxTokens >= 1024 {
			return Marker(testNonce), nil
		}
		return "", nil // budget exhausted on reasoning
	})

	// Default budget (2048) is large enough for the marker to appear.
	repOK := Run(context.Background(), budgetAwareFake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	assert.Equal(t, StatusOK, repOK.Agents[0].Status, "default budget must yield StatusOK")

	// Tiny budget (8) is consumed by reasoning; marker absent → warning.
	repWarn := Run(context.Background(), budgetAwareFake, res, Options{Nonce: testNonce, MaxTokens: 8})
	assert.Equal(t, StatusOKWarning, repWarn.Agents[0].Status, "tiny budget must yield StatusOKWarning")
	assert.Contains(t, repWarn.Agents[0].Hint, "max-tokens")
}

func TestRun_EmptyRosterIsNotSuccess(t *testing.T) {
	// An empty roster (no agents configured) must NOT produce exit 0 —
	// "everything is healthy" is misleading when nothing was tested.
	emptyRes := &Resolution{
		Targets: nil,
		Agents:  nil,
		Paths:   map[string][]string{},
	}
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, emptyRes, Options{Nonce: testNonce})
	assert.NotEqual(t, 0, rep.ExitCode, "empty roster must not exit 0")
	assert.Empty(t, rep.Agents, "empty roster produces no agent results")
}

func TestRun_MissingKeySkipsNetwork(t *testing.T) {
	// ATCR_DOCTOR_KEY deliberately unset.
	res := twoAgentSharedTarget(t)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	assert.Equal(t, 0, fake.count("m"), "missing key must short-circuit before any network call")
	assert.Equal(t, StatusMissingKey, rep.Agents[0].Status)
	assert.Contains(t, rep.Agents[0].Hint, "ATCR_DOCTOR_KEY")
	assert.Equal(t, 1, rep.ExitCode)
}

func TestRun_InvalidBaseURLIsConfigError(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: ""}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m"}},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return "", nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	assert.Equal(t, 0, fake.count("m"))
	assert.Equal(t, StatusInvalidConfig, rep.Agents[0].Status)
	assert.Equal(t, 1, rep.ExitCode)
}

func TestRun_FallbackProvidesWorkingPath(t *testing.T) {
	t.Setenv("K1", "k")
	t.Setenv("K2", "k")
	reg := regWith(
		map[string]registry.Provider{
			"p1": {APIKeyEnv: "K1", BaseURL: "https://a.example/v1"},
			"p2": {APIKeyEnv: "K2", BaseURL: "https://b.example/v1"},
		},
		map[string]registry.AgentConfig{
			"a": {Provider: "p1", Model: "bad", Fallback: "b"},
			"b": {Provider: "p2", Model: "good"},
		},
	)
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a"}})
	require.NoError(t, err)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		if inv.Model == "bad" {
			return "", &llmclient.HTTPStatusError{Status: http.StatusNotFound, Snippet: "model not found"}
		}
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	// Primary 'a' fails (not_found), fallback 'b' works → listed agent has a path → exit 0.
	assert.Equal(t, 0, rep.ExitCode, "fallback supplies a working path for the listed agent")
	statusByAgent := map[string]string{}
	for _, ar := range rep.Agents {
		statusByAgent[ar.Agent] = ar.Status
	}
	assert.Equal(t, StatusNotFound, statusByAgent["a"])
	assert.Equal(t, StatusOK, statusByAgent["b"])
}

func TestRun_NoWorkingPathExits1(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	res := twoAgentSharedTarget(t)
	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", &llmclient.HTTPStatusError{Status: http.StatusUnauthorized, Snippet: "bad key"}
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	assert.Equal(t, StatusAuthFailed, rep.Agents[0].Status)
	assert.Equal(t, 1, rep.ExitCode)
}

func TestClassify_StatusMapping(t *testing.T) {
	tgt := Target{Provider: "p", Model: "m", BaseURL: "https://x/v1", APIKeyEnv: "K"}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"auth401", &llmclient.HTTPStatusError{Status: 401}, StatusAuthFailed},
		{"auth403", &llmclient.HTTPStatusError{Status: 403}, StatusAuthFailed},
		{"notfound", &llmclient.HTTPStatusError{Status: 404}, StatusNotFound},
		{"ratelimited", &llmclient.HTTPStatusError{Status: 429}, StatusRateLimited},
		{"server500", &llmclient.HTTPStatusError{Status: 500}, StatusProviderError},
		{"server503", &llmclient.HTTPStatusError{Status: 503}, StatusProviderError},
		{"timeout", context.DeadlineExceeded, StatusTimeout},
		{"network", fmt.Errorf("dial tcp: connection refused"), StatusNetworkError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify("", tc.err, testNonce, 5, tgt)
			assert.Equal(t, tc.want, got.status)
		})
	}
}

func TestClassify_CanceledIsTimeoutWithoutRaiseHint(t *testing.T) {
	tgt := Target{Provider: "p", Model: "m", BaseURL: "https://x/v1", APIKeyEnv: "K"}
	got := classify("", context.Canceled, testNonce, 5, tgt)
	assert.Equal(t, StatusTimeout, got.status)
	// A cancellation (Ctrl-C) must not advise changing --timeout.
	assert.NotContains(t, got.hint, "raise --timeout")
}

func TestClassify_ErrorBodySnippetSurfaced(t *testing.T) {
	tgt := Target{Provider: "p", Model: "m", BaseURL: "https://x/v1", APIKeyEnv: "K"}
	got := classify("", &llmclient.HTTPStatusError{Status: 404, Snippet: "the model `gpt-x` does not exist"}, testNonce, 5, tgt)
	assert.Equal(t, StatusNotFound, got.status)
	assert.Contains(t, got.detail, "does not exist")
}

func TestRenderJSON_Stable(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusOK, LatencyMS: 12},
	}}
	var b strings.Builder
	require.NoError(t, RenderJSON(&b, rep))
	var parsed struct {
		Agents []struct {
			Agent  string `json:"agent"`
			Status string `json:"status"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal([]byte(b.String()), &parsed))
	require.Len(t, parsed.Agents, 1)
	assert.Equal(t, "a", parsed.Agents[0].Agent)
	assert.Equal(t, StatusOK, parsed.Agents[0].Status)
}

func TestRenderTable_HumanReadable(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "bruce", Provider: "openai", Model: "gpt-4", Status: StatusOK, LatencyMS: 120},
		{Agent: "kai", Provider: "anthropic", Model: "claude", Status: StatusMissingKey, Hint: "set ANTHROPIC_KEY"},
	}}
	var b strings.Builder
	RenderTable(&b, rep)
	out := b.String()
	assert.Contains(t, out, "bruce")
	assert.Contains(t, out, "AGENT")
	assert.Contains(t, out, StatusMissingKey)
	assert.Contains(t, out, "set ANTHROPIC_KEY")
}

// errWriter is a writer that always returns an error, used to trigger tabwriter
// flush failures in tests.
type errWriter struct{ err error }

func (e *errWriter) Write(p []byte) (int, error) { return 0, e.err }

func TestProbe_MalformedBaseURLRejected(t *testing.T) {
	// A fake completer that fails the test if reached — the path/query/fragment
	// checks must short-circuit before any network call is made.
	badCompleter := newFake(func(inv llmclient.Invocation) (string, error) {
		return "", fmt.Errorf("network: dial tcp: unexpected call")
	})
	cases := []struct {
		name    string
		baseURL string
	}{
		{"chat_completions_suffix", "https://api.example.com/v1/chat/completions"},
		{"query_param", "https://api.example.com/v1?stream=true"},
		{"fragment", "https://api.example.com/v1#section"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATCR_PROBE_PATH_KEY", "k")
			tgt := Target{Provider: "p", Model: "m", BaseURL: tc.baseURL, APIKeyEnv: "ATCR_PROBE_PATH_KEY"}
			got := probe(context.Background(), badCompleter, tgt, Options{Nonce: testNonce})
			assert.Equal(t, StatusInvalidConfig, got.status, "base_url %q should be rejected as invalid_config", tc.baseURL)
			assert.Contains(t, got.hint, "base_url")
		})
	}
}

func TestRenderTable_DetailPrefixedWhenHintEmpty(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusNetworkError, Detail: "connection refused"},
	}}
	var b strings.Builder
	RenderTable(&b, rep)
	out := b.String()
	assert.Contains(t, out, "error: connection refused", "Detail should be prefixed with 'error: ' when Hint is empty")
	assert.NotContains(t, out, "\tconnection refused", "undecorated Detail must not appear as-is in the HINT column")
}

func TestRenderTable_HintTakesPrecedenceOverDetail(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusNetworkError, Hint: "check firewall", Detail: "connection refused"},
	}}
	var b strings.Builder
	RenderTable(&b, rep)
	out := b.String()
	assert.Contains(t, out, "check firewall", "Hint should appear when set")
	assert.NotContains(t, out, "connection refused", "Detail must not appear when Hint is set")
}

func TestRenderTableError_EmptySourcePassesThrough(t *testing.T) {
	// The resolver guarantees a non-empty Source for any real pipeline agent;
	// the renderer must not silently substitute 'user' for an empty value
	// (tabwriter replaces tab separators with spaces in output).
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusOK, Source: ""},
	}}
	var b strings.Builder
	require.NoError(t, RenderTableError(&b, rep))
	assert.NotContains(t, b.String(), "user", "empty Source must not be defaulted to 'user'")
}

func TestRenderTableError_SurfacesFlushError(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusOK},
	}}
	err := RenderTableError(&errWriter{err: fmt.Errorf("disk full")}, rep)
	assert.Error(t, err, "RenderTableError should return the Flush error")
}

func TestRenderTableError_NilOnSuccess(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "a", Provider: "p", Model: "m", Status: StatusOK},
	}}
	var b strings.Builder
	assert.NoError(t, RenderTableError(&b, rep), "RenderTableError should return nil on success")
}

func TestClassify_NetworkErrorRedactsAPIKey(t *testing.T) {
	const secret = "super-secret-api-key-xyz"
	t.Setenv("SECRET_REDACT_KEY", secret)
	tgt := Target{Provider: "p", Model: "m", BaseURL: "https://x/v1", APIKeyEnv: "SECRET_REDACT_KEY"}
	// A transport error that accidentally embeds the API key value (e.g. a
	// misconfigured proxy that echoes auth headers in its error message).
	err := fmt.Errorf("request failed: auth key=%s rejected by proxy", secret)
	got := classify("", err, testNonce, 5, tgt)
	assert.Equal(t, StatusNetworkError, got.status)
	assert.NotContains(t, got.detail, secret, "API key must be scrubbed from network-error detail")
	assert.Contains(t, got.detail, "[redacted]")
}

func TestClassify_PromptEchoIsNotOK(t *testing.T) {
	tgt := Target{Provider: "p", Model: "m", BaseURL: "https://x/v1", APIKeyEnv: "K"}
	// An endpoint that echoes the request prompt verbatim contains the marker
	// (because the prompt embeds it), but it did not follow the instruction —
	// a common misconfiguration (wrong route returning the request body).
	got := classify(Prompt(testNonce), nil, testNonce, 5, tgt)
	assert.NotEqual(t, StatusOK, got.status, "a verbatim prompt echo must not classify as ok")
}

func TestBounded_ValidUTF8AtBoundary(t *testing.T) {
	// A two-byte UTF-8 sequence (é = 0xc3 0xa9) straddling the maxDetailBytes
	// boundary must not produce an invalid-UTF-8 output string.
	long := strings.Repeat("a", maxDetailBytes-1) + "\xc3\xa9" // é — 2 bytes; 2nd byte at pos maxDetailBytes
	result := bounded(long)
	assert.LessOrEqual(t, len(result), maxDetailBytes)
	// strings.ToValidUTF8(s, "") replaces invalid bytes with empty string;
	// if result was valid UTF-8 it equals the output unchanged.
	assert.Equal(t, strings.ToValidUTF8(result, ""), result, "bounded() must return valid UTF-8")
}

func TestPromptAndMarker(t *testing.T) {
	assert.Equal(t, "ATCR-OK-"+testNonce, Marker(testNonce))
	assert.Contains(t, Prompt(testNonce), Marker(testNonce))
}

func TestRandomNonce_NonEmptyAndUnique(t *testing.T) {
	n1, err := RandomNonce()
	require.NoError(t, err)
	n2, err := RandomNonce()
	require.NoError(t, err)
	assert.NotEmpty(t, n1)
	assert.NotEqual(t, n1, n2)
}
