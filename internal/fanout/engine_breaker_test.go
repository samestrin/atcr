package fanout

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
)

// provCapture records the provider name it observes on the call context, so a
// test can assert the engine bridged Agent.Provider onto it.
type provCapture struct{ provider string }

func (p *provCapture) Complete(ctx context.Context, _ llmclient.Invocation) (string, error) {
	p.provider = circuitbreaker.ProviderFromContext(ctx)
	return "ok", nil
}

// The engine bridges Agent.Provider onto the request context so llmclient.send
// can key the per-provider breaker without a Provider field on Invocation.
func TestEngine_ThreadsProviderToContext(t *testing.T) {
	pc := &provCapture{}
	e := NewEngine(pc)
	e.Run(context.Background(), []Slot{{
		Primary: Agent{Name: "a", Provider: "openai", Invocation: llmclient.Invocation{Model: "a"}},
	}})
	assert.Equal(t, "openai", pc.provider,
		"engine must thread Agent.Provider onto the call context")
}

// AC6: a CircuitOpenError from the primary is a permanent (non-timeout) failure;
// the slot moves straight to the fallback chain, which answers in its place.
func TestInvokeSlot_CircuitOpenTriggersFallback(t *testing.T) {
	f := newFake()
	f.failFor["primary"] = &llmclient.CircuitOpenError{Provider: "openai"}
	e := NewEngine(f)

	slot := Slot{
		Primary:   Agent{Name: "primary", Provider: "openai", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "fb", Provider: "anthropic", Invocation: llmclient.Invocation{Model: "fb"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	require.Equal(t, StatusOK, r.Status, "fallback should answer when the primary's circuit is open")
	assert.True(t, r.FallbackUsed)
	assert.Equal(t, "primary", r.Agent, "attribution follows the slot, not the substitute")
	assert.Equal(t, "primary", r.FallbackFrom)
}

// A CircuitOpenError classifies as StatusFailed (not StatusTimeout), so a slot
// whose entire chain is circuit-open fails fast and is reported as failed rather
// than mislabelled a timeout.
func TestInvokeSlot_CircuitOpenChainFailsAsFailed(t *testing.T) {
	f := newFake()
	f.failFor["primary"] = &llmclient.CircuitOpenError{Provider: "openai"}
	f.failFor["fb"] = &llmclient.CircuitOpenError{Provider: "openai"}
	e := NewEngine(f)

	slot := Slot{
		Primary:   Agent{Name: "primary", Provider: "openai", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "fb", Provider: "openai", Invocation: llmclient.Invocation{Model: "fb"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	require.Equal(t, StatusFailed, r.Status)
	var coe *llmclient.CircuitOpenError
	require.True(t, errors.As(r.Err, &coe), "chain failure should surface the CircuitOpenError")
}

// A tools-enabled agent with no Provider set must emit a warn-level log so a
// silent breaker bypass is observable in production. Doctor/direct-construction
// paths have Tools=false and are therefore exempt.
func TestInvokeAgent_WarnOnEmptyProviderWithTools(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	e := NewEngine(newFake(), WithLogger(logger))

	a := Agent{
		Name:       "misconfig",
		Tools:      true,
		Invocation: llmclient.Invocation{Model: "misconfig"},
		// Provider intentionally empty — simulates a misconfigured production agent
	}
	e.invokeAgent(context.Background(), a)

	assert.Contains(t, buf.String(), "provider",
		"invokeAgent must warn when a tools-enabled agent has no provider set")
}

// AC2 metric correctness: a circuit-open fail-fast made no provider round-trip,
// so it must not be counted in atcr_api_calls_total.
func TestRecordAgentOutcome_CircuitOpenNotCountedAsAPICall(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	recordAgentOutcome(Result{Status: StatusFailed, Err: &llmclient.CircuitOpenError{Provider: "openai"}})

	if got := metrics.DefaultRegistry.Counter(metrics.NameAPICallsTotal).Value(); got != 0 {
		t.Fatalf("atcr_api_calls_total = %d, want 0 (circuit-open made no request)", got)
	}
	// It is still counted as a failed agent.
	if got := metrics.DefaultRegistry.Counter(metrics.NameAgentsFailed).Value(); got != 1 {
		t.Fatalf("atcr_agents_failed = %d, want 1", got)
	}
}
