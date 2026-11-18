package verify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyAWSKey is split so the source holds no literal credential.
const verifyAWSKey = "AKIA" + "QYLPMN5HHHFPZAM2"

// secretFinding is one fix-eligible finding whose Problem carries a credential,
// so the skeptic prompt and both executor prompts carry it too.
func secretFinding() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Confidence: ConfidenceVerified, Reviewers: []string{"rev"},
		Problem: "hardcoded key " + verifyAWSKey,
	}}
}

// wireServer is a fake provider recording every request body. Each request is
// answered with a final message carrying reply.
type wireServer struct {
	mu     sync.Mutex
	bodies []string
}

func (s *wireServer) start(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":`+reply+`}}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ATCR_TEST_KEY", "k")
	return srv
}

func (s *wireServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// assertWire checks what reached the provider under policy: warn sends the
// credential unchanged, redact sends a placeholder in its place, block sends
// nothing at all.
func assertWire(t *testing.T, policy string, bodies []string) {
	t.Helper()
	switch policy {
	case "block":
		assert.Empty(t, bodies, "block sends nothing")
	case "redact":
		require.NotEmpty(t, bodies)
		for _, b := range bodies {
			assert.NotContains(t, b, verifyAWSKey)
		}
		assert.Contains(t, bodies[0], payload.SecretPlaceholder)
	default:
		require.NotEmpty(t, bodies)
		assert.Contains(t, bodies[0], verifyAWSKey, "warn sends the prompt unchanged")
	}
}

// Epic 106.3 T1: the skeptic's Invocation carries the registry secret_guard
// policy, warn when the registry sets none, on every Chat call.
func TestRunVerify_SkepticCarriesSecretGuard(t *testing.T) {
	for _, tc := range []struct{ set, want string }{{"", "warn"}, {"redact", "redact"}, {"block", "block"}, {"off", "off"}} {
		t.Run(tc.want, func(t *testing.T) {
			dir := pipelineReview(t, []reconcile.JSONFinding{
				{Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "MEDIUM", Reviewers: []string{"rev"}},
			})
			reg := skepticRegistry()
			reg.SecretGuard = tc.set
			cc := finalChat(`{"verdict":"confirmed","reasoning":"checked the line"}`)
			_, err := runVerify(context.Background(), dir, reg, Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
				return cc, okDispatcher(), nil, nil
			})
			require.NoError(t, err)
			invs := cc.allInvocations()
			require.NotEmpty(t, invs)
			for i, inv := range invs {
				assert.Equal(t, tc.want, inv.SecretGuard, "call %d", i)
			}
		})
	}
}

// Through the real client: a credential in a skeptic prompt is warned about,
// redacted or blocked as the registry policy says. Block sends nothing and
// records the finding unverifiable with a named secret_guard note.
func TestRunVerify_SkepticPromptSecretGuardOnTheWire(t *testing.T) {
	for _, policy := range []string{"warn", "redact", "block"} {
		t.Run(policy, func(t *testing.T) {
			ws := &wireServer{}
			srv := ws.start(t, `"{\"verdict\":\"confirmed\",\"reasoning\":\"checked the line\"}"`)
			dir := pipelineReview(t, []reconcile.JSONFinding{
				{Severity: "HIGH", File: "a.go", Line: 1, Problem: "hardcoded key " + verifyAWSKey, Confidence: "MEDIUM", Reviewers: []string{"rev"}},
			})
			reg := skepticRegistry()
			reg.Providers["p"] = registry.Provider{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY"}
			reg.SecretGuard = policy
			_, err := runVerify(context.Background(), dir, reg, Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
				return llmclient.New(llmclient.WithHTTPClient(srv.Client())), okDispatcher(), nil, nil
			})
			require.NoError(t, err)
			assertWire(t, policy, ws.sent())
			got := readFindings(t, dir)
			require.NotNil(t, got[0].Verification)
			if policy == "block" {
				assert.Equal(t, verdictUnverifiable, got[0].Verification.Verdict)
				assert.Contains(t, got[0].Verification.Notes, "secret_guard=block")
				assert.NotContains(t, got[0].Verification.Notes, verifyAWSKey)
			} else {
				assert.Equal(t, "confirmed", got[0].Verification.Verdict)
			}
		})
	}
}

// The skeptic's failure log names a secret_guard block as its own class, not a
// provider error.
func TestFailureClass_SecretGuardBlocked(t *testing.T) {
	res := fanout.Result{Status: fanout.StatusFailed, Err: errors.Join(errors.New("agent"), llmclient.ErrSecretGuardBlocked)}
	assert.Equal(t, "secret_guard_blocked", failureClass(res))
	assert.Equal(t, "provider_error", failureClass(fanout.Result{Status: fanout.StatusFailed, Err: errors.New("boom")}))
}

// Epic 106.3 T1: the agent-mode executor's Invocation carries the registry
// secret_guard policy, warn when the registry sets none.
func TestGenerateFixes_AgentModeCarriesSecretGuard(t *testing.T) {
	for _, tc := range []struct{ set, want string }{{"", "warn"}, {"redact", "redact"}, {"block", "block"}} {
		t.Run(tc.want, func(t *testing.T) {
			reg := execRegistry("MEDIUM")
			reg.Executor = agentExecConfig()
			reg.SecretGuard = tc.set
			cc := finalChat(`{"fix": "x", "explanation": "y"}`)
			generateFixes(context.Background(), eligibleFinding(), reg.Executor, reg, &recordingExecutor{}, cc, okDispatcher(), 0)
			invs := cc.allInvocations()
			require.NotEmpty(t, invs)
			for i, inv := range invs {
				assert.Equal(t, tc.want, inv.SecretGuard, "call %d", i)
			}
		})
	}
}

// Through the real client: a credential in a tool-loop executor prompt is
// warned about, redacted or blocked as the policy says. Block sends nothing and
// records a named FixWarning through generateFixes' failure path.
func TestGenerateFixes_AgentModePromptSecretGuardOnTheWire(t *testing.T) {
	for _, policy := range []string{"warn", "redact", "block"} {
		t.Run(policy, func(t *testing.T) {
			ws := &wireServer{}
			srv := ws.start(t, `"{\"fix\": \"rotate the key\", \"explanation\": \"y\"}"`)
			reg := execRegistry("MEDIUM")
			reg.Providers[testExecProvider] = registry.Provider{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY"}
			reg.Executor = agentExecConfig()
			reg.SecretGuard = policy
			findings := secretFinding()
			snippet := &recordingExecutor{out: "SNIPPET PATH MUST NOT RUN"}
			generateFixes(context.Background(), findings, reg.Executor, reg, snippet,
				llmclient.New(llmclient.WithHTTPClient(srv.Client())), okDispatcher(), 0)
			assert.Zero(t, snippet.calls)
			assertWire(t, policy, ws.sent())
			if policy == "block" {
				assert.Empty(t, findings[0].Fix)
				assert.Contains(t, findings[0].FixWarning, "secret_guard=block")
				assert.NotContains(t, findings[0].FixWarning, verifyAWSKey)
			} else {
				assert.Equal(t, "rotate the key", findings[0].Fix)
			}
		})
	}
}

// Epic 106.3 T1: the snippet-path executor prompt goes through
// fanout.GuardOutbound before Complete: warn sends it unchanged, redact sends
// the redacted text, block sends nothing and records a named FixWarning.
func TestGenerateFixes_SnippetPromptSecretGuard(t *testing.T) {
	for _, policy := range []string{"", "warn", "redact", "block"} {
		t.Run("policy="+policy, func(t *testing.T) {
			reg := execRegistry("MEDIUM")
			reg.SecretGuard = policy
			findings := secretFinding()
			rec := &recordingExecutor{out: "rotate the key"}
			generateFixes(context.Background(), findings, reg.Executor, reg, rec, nil, okDispatcher(), 0)
			switch policy {
			case "block":
				assert.Zero(t, rec.calls, "block sends nothing")
				assert.Empty(t, findings[0].Fix)
				assert.Contains(t, findings[0].FixWarning, "secret_guard=block")
				assert.NotContains(t, findings[0].FixWarning, verifyAWSKey)
			case "redact":
				require.Equal(t, 1, rec.calls)
				assert.NotContains(t, rec.prompts[0], verifyAWSKey)
				assert.Contains(t, rec.prompts[0], payload.SecretPlaceholder)
				assert.Equal(t, "rotate the key", findings[0].Fix)
			default:
				require.Equal(t, 1, rec.calls)
				assert.Contains(t, rec.prompts[0], verifyAWSKey, "warn (and the empty default) sends the prompt unchanged")
				assert.Equal(t, "rotate the key", findings[0].Fix)
			}
		})
	}
}

// The truncation-aware CompleteWithMeta path is guarded the same way as
// Complete: block never reaches it and redact sends the redacted prompt.
func TestCallExecutor_MetaPathSecretGuard(t *testing.T) {
	prompt := "fix this: " + verifyAWSKey
	blocked := finalChat("unused")
	_, tr, salv, err := callExecutor(context.Background(), blocked, testExecProviderVal(), execConfig("MEDIUM"), prompt, 0, "block")
	require.ErrorIs(t, err, fanout.ErrOutboundBlocked)
	assert.False(t, tr)
	assert.False(t, salv)
	assert.Empty(t, blocked.allInvocations(), "block sends nothing")

	redacted := finalChat("a fix")
	out, _, _, err := callExecutor(context.Background(), redacted, testExecProviderVal(), execConfig("MEDIUM"), prompt, 0, "redact")
	require.NoError(t, err)
	assert.Equal(t, "a fix", out)
	invs := redacted.allInvocations()
	require.Len(t, invs, 1)
	assert.NotContains(t, invs[0].Prompt, verifyAWSKey)
	assert.Contains(t, invs[0].Prompt, payload.SecretPlaceholder)
}
