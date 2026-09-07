package verify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

func intPtr(i int) *int       { return &i }
func int64Ptr(i int64) *int64 { return &i }

func testSkeptic() Skeptic {
	return Skeptic{
		Name:     "skeptic-1",
		Config:   registry.AgentConfig{Provider: "p", Model: "skeptic-model", Role: registry.RoleSkeptic, SupportsFC: true},
		Provider: registry.Provider{APIKeyEnv: "TEST_KEY", BaseURL: "http://localhost/v1"},
	}
}

// TestInvokeSkeptic_DegradesWhenNotFC verifies SupportsFC is forwarded: a skeptic
// whose model lacks function calling degrades to single-shot rather than being
// forced into the tool loop. The fake's Complete returns a real verdict so the
// single-shot path produces a confirmed outcome — proving the degrade happened
// and the tool loop was skipped (dispatcher call count == 0).
func TestInvokeSkeptic_DegradesWhenNotFC(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.SupportsFC = false
	disp := okDispatcher()
	v, _, err := invokeSkeptic(context.Background(), sk, "prompt", finalChat(`{"verdict":"confirmed"}`), disp, false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, "confirmed", v.Verdict, "degrade path should return the single-shot verdict, not unverifiable")
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.Equal(t, 0, disp.count(), "tool loop must not be entered on degrade — dispatcher never called")
}

// TestBuildSkepticAgent_ForwardsProviderAndBudgets locks the provider routing and
// budget forwarding onto the constructed Agent/Invocation (the HIGH fix from 2.2.A).
func TestBuildSkepticAgent_ForwardsProviderAndBudgets(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(7)
	sk.Config.ToolBudgetBytes = int64Ptr(4096)
	sk.Config.TimeoutSecs = intPtr(30)
	sk.Config.MaxRetries = intPtr(4)
	sk.Config.InitialBackoffMs = intPtr(200)
	a := buildSkepticAgent(sk, "the prompt", false)
	assert.True(t, a.Tools)
	assert.True(t, a.SupportsFC)
	assert.Equal(t, 7, a.MaxTurns)
	assert.Equal(t, int64(4096), a.ToolBudgetBytes)
	assert.Equal(t, 30, a.TimeoutSecs)
	assert.Equal(t, 4, a.MaxRetries, "skeptic max_retries forwarded (Epic 4.6)")
	assert.Equal(t, 200, a.InitialBackoffMs, "skeptic initial_backoff_ms forwarded (Epic 4.6)")
	assert.Equal(t, "http://localhost/v1", a.Invocation.BaseURL)
	assert.Equal(t, "TEST_KEY", a.Invocation.APIKeyEnv)
	assert.Equal(t, "skeptic-model", a.Invocation.Model)
	assert.Equal(t, "the prompt", a.Invocation.Prompt)
}

func TestInvokeSkeptic_Confirms(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "confirmed", "reasoning": "evidence valid"}`)
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "evidence valid", v.Notes)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_Refutes(t *testing.T) {
	t.Parallel()
	cc := finalChat(`{"verdict": "refuted", "reasoning": "false positive"}`)
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictRefuted, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_UsesToolsThenConcludes(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"verdict": "confirmed", "reasoning": "verified via file read"}`},
	}}
	disp := okDispatcher()
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, disp, false)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "verified via file read", v.Notes)
	assert.GreaterOrEqual(t, disp.count(), 1, "tool loop should have dispatched at least once")
}

func TestInvokeSkeptic_ProviderError(t *testing.T) {
	t.Parallel()
	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err) // runtime failure NOT propagated
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
	assert.Contains(t, v.Notes, "rate limit exceeded")
}

// TestInvokeSkeptic_UsesLogger verifies invokeSkeptic routes a skeptic failure
// through the injected context logger (not os.Stderr): the failure summary is
// emitted at Warn with the skeptic name and class, observable in the buffer.
func TestInvokeSkeptic_UsesLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := log.NewContext(context.Background(), logger)

	cc := &fakeChatCompleter{turns: []chatTurn{{err: errors.New("rate limit exceeded")}}}
	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	require.Equal(t, verdictUnverifiable, v.Verdict)

	out := buf.String()
	assert.Contains(t, out, "skeptic failed",
		"a skeptic failure must be logged through the injected context logger")
	assert.Contains(t, out, "skeptic=skeptic-1")
	assert.Contains(t, out, "class=provider_error")
}

func TestInvokeSkeptic_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the first turn
	cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"verdict":"confirmed"}`}}}
	v, _, err := invokeSkeptic(ctx, testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_MalformedOutput(t *testing.T) {
	t.Parallel()
	cc := finalChat("I don't know")
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "malformed_output")
	assert.Equal(t, "skeptic-1", v.Skeptic)
}

func TestInvokeSkeptic_EmptyResponse(t *testing.T) {
	t.Parallel()
	cc := finalChat("")
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "empty_response")
}

func TestInvokeSkeptic_BudgetTripMaxTurns(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	v, _, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "max_turns")
}

func TestInvokeSkeptic_BudgetTripToolBytes(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.ToolBudgetBytes = int64Ptr(10)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file")}}
	disp := &fakeDispatcher{result: tools.ToolResult{Content: "this content is definitely more than ten bytes", OriginalBytes: 46}}
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "tool_budget_bytes")
	assert.Contains(t, tripped, "tool_budget_bytes", "tripped budgets must be surfaced separately from Notes")
}

func TestInvokeSkeptic_BudgetTripTimeout(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.TimeoutSecs = intPtr(1)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), {delay: 2 * time.Second, content: `{"verdict":"confirmed"}`}}}
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, tripped, "timeout_secs", "timeout budget trip must be surfaced structurally")
}

// TestInvokeSkeptic_SurfacesTrippedBudgets locks AC1: a budget trip is returned
// as a separate []string, not only folded into the free-text Notes, so the caller
// can populate VerificationResult.TrippedBudgets structurally.
func TestInvokeSkeptic_SurfacesTrippedBudgets(t *testing.T) {
	t.Parallel()
	sk := testSkeptic()
	sk.Config.MaxTurns = intPtr(2)
	cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}
	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, tripped, "max_turns", "tripped budgets must be surfaced separately from Notes")
}

// TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict: a verdict reached without a
// trip returns an empty tripped-budgets slice (the field never carries noise).
func TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict(t *testing.T) {
	t.Parallel()
	v, tripped, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt",
		finalChat(`{"verdict":"confirmed","reasoning":"ok"}`), okDispatcher(), false)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Empty(t, tripped, "a clean verdict trips no budgets")
}

func TestInvokeSkeptic_NilContext(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(nil, testSkeptic(), "prompt", finalChat("{}"), okDispatcher(), false) //nolint:staticcheck // intentional nil-ctx guard test
	require.Error(t, err)
}

func TestInvokeSkeptic_NilChatCompleter(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", nil, okDispatcher(), false)
	require.Error(t, err)
}

func TestInvokeSkeptic_NilDispatcher(t *testing.T) {
	t.Parallel()
	_, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat("{}"), nil, false)
	require.Error(t, err)
}

// TestInvokeSkeptic_ForwardsDeclaredMaxTokens pins that an agent's max_tokens
// declaration reaches the skeptic REQUEST, not merely the Agent literal.
//
// llmclient.Invocation carries MaxTokens and its own doc warns that a reasoning
// model spends the budget on chain-of-thought before emitting visible content, but
// the skeptic Invocation forwarded every other per-agent budget (MaxTurns,
// ToolBudgetBytes, MaxRetries, InitialBackoffMs) and omitted this one — so the
// provider default applied and the declaration was silently inert. Under a low
// provider default the skeptic finishes mid-reasoning and returns no verdict,
// which the engine records as unverifiable while the run still reports success:
// the same silent-loss mode the review fan-out already fixed with resolveMaxTokens.
// Measured 2026-09-06 through litellm on a TRIVIAL 7-line snippet: glm-5.3-flash
// emitted 5,885 chars of reasoning, minimax-m3 13,618 and 3,270 output tokens.
//
// The undeclared row is load-bearing, not filler. Only the DECLARATION is
// forwarded — no built-in default is imposed here, unlike the review fan-out's
// third tier — so an undeclared skeptic keeps the provider default it has today
// and this fix cannot newly truncate one. Changing that is a separate decision on
// separate evidence.
func TestInvokeSkeptic_ForwardsDeclaredMaxTokens(t *testing.T) {
	t.Parallel()

	t.Run("declared", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.MaxTokens = intPtr(24000)
		cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"verdict":"confirmed"}`}}}

		_, _, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
		require.NoError(t, err)

		got := cc.lastInvocation().MaxTokens
		require.NotNil(t, got, "the declaration must reach the request body, not stop at the Agent literal")
		assert.Equal(t, 24000, *got)
	})

	t.Run("undeclared keeps the provider default", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		cc := &fakeChatCompleter{turns: []chatTurn{{content: `{"verdict":"confirmed"}`}}}

		_, _, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
		require.NoError(t, err)

		assert.Nil(t, cc.lastInvocation().MaxTokens,
			"no declaration means no cap is sent: this fix removes an omission, it does not impose a new default")
	})
}

// TestBuildSkepticAgent_ClampsToolBudgetToDeclaredWindow pins that a skeptic's
// context_window_tokens declaration reaches the ONE budget in this lane it can
// bound: the tool-output ceiling.
//
// A skeptic reads real files through the tool loop, so its input grows with tool
// output — but ToolBudgetBytes is a flat per-agent number, not window-derived, so
// an agent declared at 32768 tokens and one declared at 512000 got the same tool
// budget and the small one could be walked past its window by a few large reads.
// The declaration was inert here (docs/registry.md said as much).
//
// Only a DECLARED window clamps. An undeclared agent keeps exactly today's
// behaviour, so this cannot newly starve a roster nobody has sized.
func TestBuildSkepticAgent_ClampsToolBudgetToDeclaredWindow(t *testing.T) {
	t.Parallel()

	small := 32768
	large := 512000

	t.Run("a small declared window shrinks the effective tool budget", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &small
		sk.Config.ToolBudgetBytes = int64Ptr(4 << 20) // 4 MiB: far past a 32k window

		got := buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes
		want := payload.EffectiveByteBudget(sk.Config.Model, &small, 0)
		require.Positive(t, want, "the fixture must leave real input room, or the clamp below proves nothing")
		assert.Equal(t, want, got, "a declared window bounds what the tool loop may pour into it")
		assert.Less(t, got, int64(4<<20), "the flat per-agent number must lose to the smaller window-derived ceiling")
	})

	t.Run("a larger window leaves a smaller declared budget alone", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &large
		sk.Config.ToolBudgetBytes = int64Ptr(4096)

		assert.Equal(t, int64(4096), buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"the clamp is a ceiling, never a floor: an operator asking for less still gets less")
	})

	t.Run("an undeclared window keeps today's behaviour", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ToolBudgetBytes = int64Ptr(4 << 20)

		assert.Equal(t, int64(4<<20), buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"no declaration, no clamp — nothing is derived from a window nobody stated")
	})

	t.Run("an unlimited budget is clamped like any other", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &small
		// ToolBudgetBytes unset: the engine reads 0 as UNLIMITED, which is exactly
		// the state a declared window contradicts.

		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &small, 0),
			buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"unlimited is not a smaller number — a declared window must bound it")
	})

	t.Run("a non-positive ceiling is never forwarded", func(t *testing.T) {
		t.Parallel()
		tiny := 1 // prompt overhead alone exhausts it
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &tiny
		sk.Config.ToolBudgetBytes = int64Ptr(4096)

		require.Zero(t, payload.EffectiveByteBudget(sk.Config.Model, &tiny, 0),
			"the fixture must actually produce a zero ceiling, or the guard below is untested")
		assert.Equal(t, int64(4096), buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"forwarding a derived 0 would mean UNLIMITED to the engine — the exact inversion of the clamp")
	})

	t.Run("the output cap is reserved out of the window", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &small
		sk.Config.MaxTokens = intPtr(8000)

		got := buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes
		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &small, 8000), got)
		assert.Less(t, got, payload.EffectiveByteBudget(sk.Config.Model, &small, 0),
			"tokens promised to the response are not available to tool output")
	})
}

// TestInvokeSkeptic_DerivedToolBudgetTripDoesNotVoidTheVerdict pins the boundary
// the window-derived tool budget must not cross: it may TRUNCATE what a skeptic
// reads, but it may not VOID what the skeptic concluded.
//
// The clamp gave every window-declaring roster agent a positive tool ceiling
// where derefInt64 previously returned 0 (unlimited) — 29 of 29 agents in the
// shipped registry declare context_window_tokens and none declares
// tool_budget_bytes. Under invoke.go's collapse, ANY tripped budget rewrites the
// model's answer to "unverifiable", and reconcile/gate.go excludes only
// "refuted" from the CI gate. So a skeptic that reads a few large files and
// correctly refutes a false-positive HIGH finding would newly BLOCK the gate —
// on a budget the operator never configured.
//
// A DECLARED budget keeps its enforcement semantics: an operator who asks for a
// ceiling is asking for the trip to mean something.
func TestInvokeSkeptic_DerivedToolBudgetTripDoesNotVoidTheVerdict(t *testing.T) {
	t.Parallel()

	// A window small enough that one oversized read exceeds the derived ceiling.
	window := 5000

	newSkeptic := func() Skeptic {
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		return sk
	}
	ceiling := payload.EffectiveByteBudget(testSkeptic().Config.Model, &window, 0)
	require.Positive(t, ceiling, "the fixture must derive a real ceiling, or nothing below is exercised")

	// One tool result that overruns the ceiling, then a real final answer — the
	// loop trips at end-of-turn and calls requestFinalAnswer, so the model DOES
	// speak before the collapse decides whether to listen.
	overrunDispatcher := func() *fakeDispatcher {
		return &fakeDispatcher{result: tools.ToolResult{
			Content:       strings.Repeat("x", int(ceiling)+1),
			OriginalBytes: int(ceiling) + 1,
		}}
	}
	refutingTurns := func() *fakeChatCompleter {
		return &fakeChatCompleter{turns: []chatTurn{
			toolCallTurn("read_file"),
			{content: `{"verdict": "refuted", "reasoning": "the cited line does not do what the finding claims"}`},
		}}
	}

	t.Run("a derived ceiling truncates but does not overrule the skeptic", func(t *testing.T) {
		t.Parallel()
		sk := newSkeptic()
		// ToolBudgetBytes deliberately unset: the ceiling is entirely derived, so
		// there is no operator intent for the trip to enforce.

		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", refutingTurns(), overrunDispatcher(), false)
		require.NoError(t, err)
		require.NotNil(t, v)
		assert.Equal(t, verdictRefuted, v.Verdict,
			"a budget the operator never configured must not rewrite a real verdict into one that blocks the gate")
		assert.Contains(t, tripped, "tool_budget_bytes",
			"the trip is still reported for audit — it is the VERDICT that must survive, not the silence")
	})

	t.Run("a declared ceiling keeps its enforcement semantics", func(t *testing.T) {
		t.Parallel()
		sk := newSkeptic()
		sk.Config.ToolBudgetBytes = int64Ptr(ceiling / 2) // smaller than the ceiling: the declaration wins

		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", refutingTurns(), overrunDispatcher(), false)
		require.NoError(t, err)
		require.NotNil(t, v)
		assert.Equal(t, verdictUnverifiable, v.Verdict,
			"an operator who declares a tool budget is asking for the trip to mean the run is untrustworthy")
		assert.Contains(t, tripped, "tool_budget_bytes")
	})

	t.Run("a trip on any other budget still voids the verdict", func(t *testing.T) {
		t.Parallel()
		sk := newSkeptic()
		sk.Config.MaxTurns = intPtr(2)
		cc := &fakeChatCompleter{turns: []chatTurn{toolCallTurn("read_file"), toolCallTurn("read_file")}}

		v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, okDispatcher(), false)
		require.NoError(t, err)
		require.NotNil(t, v)
		assert.Equal(t, verdictUnverifiable, v.Verdict,
			"the exemption is scoped to the derived tool budget alone — max_turns still halts a run")
		assert.Contains(t, tripped, "max_turns")
	})
}

// TestBuildSkepticAgent_ReservesTheSameOutputCapAsTheReviewLane pins the second
// half of skepticToolBudget's own argument.
//
// The derivation's doc claims it is "the same one the review fan-out sizes
// payloads with, so the window resolution chain ... and the output reservation
// have exactly one definition". The window chain was shared; the output
// reservation was not. The review lane resolves declaration → the built-in
// payload.DefaultOutputTokens (fanout.resolveMaxTokens FLOORS at it); this lane
// passed derefInt(c.MaxTokens), which is 0 when nothing is declared.
//
// The undeclared case is the DOMINANT one — 23 of the 29 window-declaring roster
// agents declare no max_tokens — and it is also the case where reserving nothing
// is least defensible: when the declaration is nil the lane forwards nil to
// llmclient.Invocation.MaxTokens, which omits the field so the PROVIDER's own
// default applies. The ceiling then reserves zero output tokens while the
// provider reserves an unknown, non-zero amount — defeating the clamp's own
// stated premise that tokens promised to the response are not available to tool
// output.
func TestBuildSkepticAgent_ReservesTheSameOutputCapAsTheReviewLane(t *testing.T) {
	t.Parallel()

	// Large enough that the built-in reservation still leaves real input room —
	// the point is that the ceiling SHRINKS, not that it collapses.
	window := 128000

	t.Run("an undeclared max_tokens still reserves the built-in default", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		// MaxTokens deliberately nil: the provider will apply its own default, so
		// the ceiling must reserve something rather than pretend output is free.

		got := buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes
		want := payload.EffectiveByteBudget(sk.Config.Model, &window, payload.DefaultOutputTokens)
		require.Positive(t, want, "the fixture must leave input room, or the assertion below proves nothing")
		assert.Equal(t, want, got,
			"an undeclared agent must reserve the same output cap the review lane floors at")
		assert.Less(t, got, payload.EffectiveByteBudget(sk.Config.Model, &window, 0),
			"reserving nothing was the bug: it hands tool output room the response will take back")
	})

	t.Run("a declared max_tokens still wins over the default", func(t *testing.T) {
		t.Parallel()
		declared := 16384
		require.NotEqual(t, payload.DefaultOutputTokens, declared,
			"precondition: the declaration must differ from the constant, or this proves nothing")
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		sk.Config.MaxTokens = &declared

		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &window, declared),
			buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"the declaration is the cap the provider will honour, so it is the cap to reserve")
	})

	t.Run("an undeclared window reserves nothing, because it clamps nothing", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ToolBudgetBytes = int64Ptr(4 << 20)

		assert.Equal(t, int64(4<<20), buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"no declared window, no derivation — the reservation never enters the picture")
	})
}
