package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
		// Reserve the built-in output cap, the same number the review lane floors
		// at — an undeclared agent is still promised the provider's own default.
		want := payload.EffectiveByteBudget(sk.Config.Model, &small, payload.DefaultOutputTokens)
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

		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &small, payload.DefaultOutputTokens),
			buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"unlimited is not a smaller number — a declared window must bound it")
	})

	t.Run("a non-positive ceiling is never forwarded", func(t *testing.T) {
		t.Parallel()
		tiny := 1 // prompt overhead alone exhausts it
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &tiny
		sk.Config.ToolBudgetBytes = int64Ptr(4096)

		require.Zero(t, payload.EffectiveByteBudget(sk.Config.Model, &tiny, payload.DefaultOutputTokens),
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
		assert.Greater(t, got, payload.EffectiveByteBudget(sk.Config.Model, &small, payload.DefaultOutputTokens),
			"a declaration BELOW the built-in default must reserve less than the default, not fall back to it")
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

	// Small enough that one oversized read exceeds the derived ceiling, and at the
	// boundary (2*DefaultOutputTokens + prompt overhead) where the half-room
	// reservation cap stops binding — so the ceiling this fixture computes below
	// from EffectiveByteBudget IS the ceiling the lane enforces. Inside the band
	// the cap governs, the two would differ and the oversized read would no longer
	// overrun anything.
	window := 20480

	newSkeptic := func() Skeptic {
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		return sk
	}
	ceiling := payload.EffectiveByteBudget(testSkeptic().Config.Model, &window, payload.DefaultOutputTokens)
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

// TestBuildSkepticAgent_ReservationNeverCostsTheCeilingItself pins the boundary
// the output reservation must not cross: it may SHRINK the derived ceiling, but
// it may not be the reason there is no ceiling at all.
//
// Raising the reservation from derefInt(c.MaxTokens) (0 when undeclared) to
// reservedOutputTokens (floored at payload.DefaultOutputTokens) also moved
// payload.EffectiveByteBudget's zero-return threshold, because effectiveTokens =
// window - outputTokens - promptOverheadTokens. The threshold went from
// `window <= 4096` to `window <= 12288`. An agent declaring a window anywhere in
// that band — legal config, internal/registry/config.go admits 1..10000000 — then
// takes skepticToolBudget's `if ceiling <= 0` arm and gets `declared`, which for
// the dominant roster shape (no tool_budget_bytes) is 0. internal/fanout/loop.go
// reads 0 as UNLIMITED, so the SMALLEST window — the exact case this clamp's own
// doc says it exists for — was the one case it stopped protecting, and it was
// strictly better before: window 8000 derived 13664 bytes with a zero
// reservation.
//
// The reservation is a claim on the window, not a veto over it. Where the window
// cannot afford the full reservation, the claim SHRINKS to what the window can
// fund (capped at half its input room) rather than the ceiling collapsing to
// unlimited — see TestSkepticToolBudget_ReservesOnlyWhatTheWindowCanAfford for
// the derivation that replaced this file's original "derive again with nothing
// reserved" second arm. Where the window genuinely has no input room at all (at
// or below the prompt overhead), there is no ceiling to derive and a POSITIVE
// declared value stands — that case is unchanged and is pinned below too.
func TestBuildSkepticAgent_ReservationNeverCostsTheCeilingItself(t *testing.T) {
	t.Parallel()

	// Every window in the band the raised reservation newly zeroed, plus one on
	// each side of it.
	for _, window := range []int{4097, 8000, 12288, 12289, 20000} {
		window := window
		t.Run(fmt.Sprintf("window %d keeps a real ceiling", window), func(t *testing.T) {
			t.Parallel()
			sk := testSkeptic()
			sk.Config.ContextWindowTokens = &window
			// ToolBudgetBytes deliberately unset: the dominant roster shape, and the
			// one where falling back to `declared` means UNLIMITED.

			require.Positive(t, payload.EffectiveByteBudget(sk.Config.Model, &window, 0),
				"precondition: this window has input room once nothing is reserved, so a zero ceiling can only be the reservation's doing")

			got := buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes
			assert.Positive(t, got,
				"a declared window must still bound the tool loop — forwarding 0 hands the smallest-window skeptic an unlimited read")
		})
	}

	t.Run("a window that can afford the reservation still pays it", func(t *testing.T) {
		t.Parallel()
		window := 128000
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window

		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &window, payload.DefaultOutputTokens),
			buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"the fallback is for windows that cannot afford the reservation, not a retreat from reserving at all")
	})

	t.Run("a window with no input room at all derives nothing", func(t *testing.T) {
		t.Parallel()
		tiny := 1 // below the prompt overhead: no reservation makes this fit
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &tiny
		sk.Config.ToolBudgetBytes = int64Ptr(4096)

		require.Zero(t, payload.EffectiveByteBudget(sk.Config.Model, &tiny, 0),
			"precondition: this window has no room even with nothing reserved, so there is genuinely no ceiling to derive")
		assert.Equal(t, int64(4096), buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"the declared value still stands where no ceiling exists — this arm is not what the fix removes")
	})
}

// TestSkepticToolBudget_ReservesOnlyWhatTheWindowCanAfford pins the derivation
// seam itself: ONE continuous formula, not two discrete arms with an inversion
// between them.
//
// The two-arm shape derived a full-reservation ceiling above the reservation's
// own threshold and an UNRESERVED one below it, so the function was
// non-monotonic in both operands and the lower arm left nothing for the reply.
// Measured on the shipped code: window 12288 derived 28672 bytes while 12289
// derived 3, and at window 12000 a max_tokens of 7903 derived 3 bytes where 7904
// derived 27664 — a ~9000x swing in the direction opposite to the documented
// reservation semantics, off a one-token config change.
//
// The replacement reserves what the window can AFFORD: the agent's resolved
// output cap, capped at half the window's input room. Half rather than
// all-but-one-token, because a reservation that claims the whole room derives a
// 1-token (3-byte) ceiling across the entire band — and a trip on a DERIVED
// ceiling does not void the verdict, so the skeptic would answer from a 3-byte
// view without signalling it.
func TestSkepticToolBudget_ReservesOnlyWhatTheWindowCanAfford(t *testing.T) {
	t.Parallel()

	model := testSkeptic().Config.Model

	// ceilingFor reports the number the tool loop would enforce for a window and
	// an optional max_tokens declaration.
	ceilingFor := func(window int, maxTokens *int) int64 {
		c := testSkeptic().Config
		c.ContextWindowTokens = &window
		c.MaxTokens = maxTokens
		budget, _ := skepticToolBudget(c)
		return budget
	}

	// halfRoom is the reservation cap: half of what the window leaves for input
	// once the fixed prompt overhead is removed. Derived through payload so the
	// overhead has exactly one definition here too.
	halfRoom := func(window int) int { return payload.InputRoomTokens(model, &window) / 2 }

	// toBytes mirrors payload's conservative ~3.5 B/token ratio, pinned by
	// payload's own sizing tests. Used only to state AC2 in the units the window
	// is declared in.
	toBytes := func(tokens int) int64 { return int64(tokens) * 7 / 2 }

	t.Run("the derived ceiling is pinned across the small-window band", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			window int
			want   int64
			why    string
		}{
			{4097, 3, "the window's whole input room really is 1 token — a 3-byte ceiling here is arithmetic, not starvation"},
			{12288, 14336, "roughly half the unreserved fallback this replaces: the exchange that buys reply room"},
			{12289, 14339, "the first arm derived 3 bytes here; the seam is gone"},
			{20479, 28672, "the last window below the cap boundary already reserves the full default"},
			{20480, 28672, "at 2*reserved+overhead the cap stops binding — unchanged from today"},
			{40960, 100352, "well past the boundary: identical to today, which bounds the blast radius"},
		} {
			assert.Equal(t, tc.want, ceilingFor(tc.window, nil),
				"window %d: %s", tc.window, tc.why)
		}
	})

	t.Run("windows at or above the cap boundary derive exactly what they derive today", func(t *testing.T) {
		t.Parallel()
		// 2*DefaultOutputTokens + promptOverhead is where the half-room cap stops
		// binding. Above it the reservation is the full resolved output cap, so the
		// change cannot regress any roster window that already had real room.
		for _, window := range []int{20480, 32768, 40960, 128000, 512000} {
			window := window
			assert.Equal(t, payload.EffectiveByteBudget(model, &window, payload.DefaultOutputTokens),
				ceilingFor(window, nil),
				"window %d must be untouched by the small-window fix", window)
		}
	})

	t.Run("the ceiling never decreases as the window grows", func(t *testing.T) {
		t.Parallel()
		prev := int64(-1)
		for window := 4097; window <= 40960; window++ {
			got := ceilingFor(window, nil)
			require.GreaterOrEqualf(t, got, prev,
				"window %d derived %d after %d derived %d — a larger window must never buy a smaller read",
				window, got, window-1, prev)
			prev = got
		}
	})

	t.Run("the ceiling never increases as the reservation grows", func(t *testing.T) {
		t.Parallel()
		for _, window := range []int{8192, 12288, 20480, 40960, 128000} {
			window := window
			prev := int64(1) << 62
			for reservation := 1; reservation <= 20000; reservation++ {
				got := ceilingFor(window, intPtr(reservation))
				require.LessOrEqualf(t, got, prev,
					"window %d: max_tokens %d derived %d after %d derived %d — more output reserved must never mean more input allowed",
					window, reservation, got, reservation-1, prev)
				prev = got
			}
		}
	})

	t.Run("the reservation never claims more than half the window's input room", func(t *testing.T) {
		t.Parallel()
		for window := 4097; window <= 40960; window++ {
			window := window
			reserved := min(payload.DefaultOutputTokens, halfRoom(window))
			require.Equalf(t, payload.EffectiveByteBudget(model, &window, reserved), ceilingFor(window, nil),
				"window %d must derive from a reservation capped at half its input room (%d)", window, halfRoom(window))
		}
	})

	t.Run("ceiling plus reservation and overhead never exceeds the window", func(t *testing.T) {
		t.Parallel()
		for window := 4097; window <= 20480; window++ {
			overhead := window - payload.InputRoomTokens(model, &window)
			reserved := min(payload.DefaultOutputTokens, halfRoom(window))
			claimed := ceilingFor(window, nil) + toBytes(reserved+overhead)

			// The three shares partition the window exactly, so this can only fail by
			// the ceiling being derived from something other than what was reserved.
			assert.LessOrEqualf(t, claimed, toBytes(window),
				"window %d: ceiling + reservation + overhead must fit inside the window", window)

			// Fitting is not the same as leaving room, and the byte arithmetic above
			// cannot tell them apart — an exact partition satisfies it even when the
			// reply's share is zero. What has to hold is that the DERIVED ceiling is
			// smaller than the one this window would yield with nothing reserved:
			// that difference IS the reply's room, measured on the production value
			// rather than on the test's own expression for it.
			unreserved := payload.EffectiveByteBudget(model, &window, 0)
			if payload.InputRoomTokens(model, &window) <= 1 {
				// The degenerate end of the band: a window whose whole input room is
				// ONE token cannot both be read from and reserved against, so halving
				// floors the reservation to 0 and the ceiling really is 100% of the
				// room. Stated outright rather than hidden inside an assertion that
				// reads as if it were not.
				assert.Equalf(t, unreserved, ceilingFor(window, nil),
					"window %d: one token of input room cannot fund a reservation, so nothing is held back", window)
				continue
			}
			assert.Lessf(t, ceilingFor(window, nil), unreserved,
				"window %d: the ceiling must hold real room back for the reply, not merely fit", window)
		}
	})

	t.Run("a declaration the window chain rejects is halved on the RESOLVED window", func(t *testing.T) {
		t.Parallel()
		// Above ContextWindowTokensCap, so ResolveContextWindow falls through the
		// declaration to the table/default tier. The reservation cap must be taken
		// against the window that is actually resolved, not the raw declaration —
		// EffectiveByteBudget resolves it that way, so a cap computed from the
		// declaration would size the reservation for a window nobody uses.
		overCap := 10000001
		require.NotEqual(t, overCap, payload.ContextWindowTokens(model, &overCap),
			"precondition: this declaration must be rejected by the window chain, or the two readings agree by accident")

		reserved := min(payload.DefaultOutputTokens, halfRoom(overCap))
		assert.Equal(t, payload.EffectiveByteBudget(model, &overCap, reserved), ceilingFor(overCap, nil),
			"both halves of the derivation must read the same resolved window")
	})

	t.Run("the measured inversions no longer occur", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ceilingFor(12000, intPtr(7904)), ceilingFor(12000, intPtr(7903)),
			"a one-token max_tokens change swung the ceiling from 3 bytes to 27664")
		assert.Equal(t, int64(13832), ceilingFor(12000, intPtr(7903)),
			"and the surviving value must be a usable read, not the 3-byte residue")
		assert.Greater(t, ceilingFor(12289, nil), ceilingFor(12288, nil),
			"12288 derived 28672 while 12289 derived 3 — the seam ran backwards")
	})
}

// TestBuildSkepticAgent_NeverForwardsTheEngineUnlimitedSentinel pins the two
// remaining routes by which this lane could hand internal/fanout/loop.go a value
// its `ToolBudgetBytes > 0` guard reads as UNLIMITED.
//
// A declared window is a statement that the loop must be bounded, so no
// declaration may end in an unbounded loop. Returning `declared` when no ceiling
// can be derived left `context_window_tokens: 4096` — legal config, registry
// admits 1..10000000 — unbounded for the dominant roster shape that declares no
// tool_budget_bytes. Separately, a NEGATIVE ToolBudgetBytes survives the
// `declared > 0` test and reaches the engine as UNLIMITED too; load-time
// validation rejects it, but a programmatically built AgentConfig never passes
// through that validation.
func TestBuildSkepticAgent_NeverForwardsTheEngineUnlimitedSentinel(t *testing.T) {
	t.Parallel()

	t.Run("a window at the exact prompt overhead still bounds the loop", func(t *testing.T) {
		t.Parallel()
		window := 4096 // the boundary: input room is exactly zero, so nothing can be derived
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		// ToolBudgetBytes deliberately unset — the shape that turns "no ceiling"
		// into "unlimited".

		require.Zero(t, payload.EffectiveByteBudget(sk.Config.Model, &window, 0),
			"precondition: this window has no input room even with nothing reserved")
		assert.Positive(t, buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"a declared window must never widen the tool loop it exists to bound")
	})

	t.Run("a negative declared budget loses to a derivable ceiling", func(t *testing.T) {
		t.Parallel()
		window := 128000
		sk := testSkeptic()
		sk.Config.ContextWindowTokens = &window
		sk.Config.ToolBudgetBytes = int64Ptr(-1)

		// This pins the OUTCOME, not the clamp: a negative already fails the
		// `declared > 0` test, so the ceiling wins here with or without the clamp.
		// The sub-test below is the one that fails when the clamp is deleted — and
		// even there the clamp changes the VALUE returned, not the engine's reading
		// of it (loop.go guards on `> 0`, so a negative and a 0 are the same
		// unlimited state). What it prevents is this function handing its caller a
		// sentinel the engine has no defined reading for.
		assert.Equal(t, payload.EffectiveByteBudget(sk.Config.Model, &window, payload.DefaultOutputTokens),
			buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"a negative budget must lose to the derived ceiling")
	})

	t.Run("a negative declared budget with no window is clamped to zero", func(t *testing.T) {
		t.Parallel()
		sk := testSkeptic()
		sk.Config.ToolBudgetBytes = int64Ptr(-4096)
		// No window: there is nothing to derive, so the value is forwarded as-is —
		// but it must be normalised to the engine's own sentinel rather than a
		// negative the engine has no defined reading for.

		assert.Zero(t, buildSkepticAgent(sk, "prompt", false).ToolBudgetBytes,
			"an undeclared window derives nothing, so the value is forwarded — but as the engine's own sentinel, not a negative it has no reading for")
	})
}

// TestInvokeSkeptic_FlooredWindowTripYieldsUnverifiable pins what the 1-byte
// floor MEANS to the caller, which is a separate question from what it is.
//
// The floor exists so a declared window at or below the prompt overhead cannot
// reach internal/fanout/loop.go as the engine's UNLIMITED sentinel. But a 1-byte
// ceiling trips on the first tool result by construction, so if that trip were
// classified as a DERIVED trip, tripsVoidTheVerdict would keep the answer and a
// skeptic that read one byte would hand reconcile's gate a live confirmed or
// refuted. That is the same failure — a verdict formed from a starved view, with
// nothing in the record to say so — that ruled out reserving all but one token.
// A window this small cannot fund a trustworthy investigation, so the run is
// unverifiable and says so.
func TestInvokeSkeptic_FlooredWindowTripYieldsUnverifiable(t *testing.T) {
	t.Parallel()

	window := 4096 // exactly the prompt overhead: no input room to derive from
	sk := testSkeptic()
	sk.Config.ContextWindowTokens = &window
	// ToolBudgetBytes deliberately unset: the shape that would otherwise be 0.

	enforced, derived := skepticToolBudget(sk.Config)
	require.Equal(t, minSkepticToolBudget, enforced,
		"the fixture must land on the floor, or nothing below is exercised")
	require.False(t, derived,
		"the floor is not a window-derived ceiling: a trip on it must void the verdict")

	disp := &fakeDispatcher{result: tools.ToolResult{
		Content:       strings.Repeat("x", int(enforced)+1),
		OriginalBytes: int(enforced) + 1,
	}}
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"verdict": "refuted", "reasoning": "answered from a one-byte view"}`},
	}}

	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict,
		"a window that cannot hold one tool result must not produce a verdict the CI gate acts on")
	assert.Contains(t, tripped, "tool_budget_bytes",
		"the trip is reported so an operator can see the window is the cause")
}

// TestInvokeSkeptic_DeclaredCeilingAboveTheDerivedOneIsNotEnforced settles what
// `derived` MEANS when the operator declared a ceiling the derivation overrides.
//
// tripsVoidTheVerdict reads `derived == false` as "the enforced ceiling is the
// operator's, so a trip on it means the run is untrustworthy". A declaration at
// or ABOVE the derived ceiling is never the number enforced — the derived one is
// smaller and wins — so the trip is a derived-ceiling trip and truncates the read
// without voiding the verdict. That is the documented behaviour, and it is the
// only reading consistent with what the engine actually enforced: voiding a
// verdict over a ceiling the operator's declaration never set would blame the
// operator for a bound this lane chose.
func TestInvokeSkeptic_DeclaredCeilingAboveTheDerivedOneIsNotEnforced(t *testing.T) {
	t.Parallel()

	// Inside the band where the half-room cap binds, so the derived ceiling is
	// materially smaller than the declaration below it.
	window := 12288
	sk := testSkeptic()
	sk.Config.ContextWindowTokens = &window
	sk.Config.ToolBudgetBytes = int64Ptr(1 << 20) // far above the derived ceiling

	enforced, derived := skepticToolBudget(sk.Config)
	require.True(t, derived,
		"a declaration above the derived ceiling is not the number enforced, so the trip is a derived one")
	require.Equal(t, int64(14336), enforced,
		"the fixture must sit in the band the half-room cap governs")

	disp := &fakeDispatcher{result: tools.ToolResult{
		Content:       strings.Repeat("x", int(enforced)+1),
		OriginalBytes: int(enforced) + 1,
	}}
	cc := &fakeChatCompleter{turns: []chatTurn{
		toolCallTurn("read_file"),
		{content: `{"verdict": "refuted", "reasoning": "the cited line does not do what the finding claims"}`},
	}}

	v, tripped, err := invokeSkeptic(context.Background(), sk, "prompt", cc, disp, false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictRefuted, v.Verdict,
		"the operator's declaration was never enforced here, so its overrun cannot be what makes the run untrustworthy")
	assert.Contains(t, tripped, "tool_budget_bytes",
		"the trip is still reported for audit — it is the VERDICT that survives, not the silence")
}
