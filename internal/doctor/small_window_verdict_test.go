package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_SmallDeclaredWindowWarnsAboutVerificationCollapse extends the
// zeroBudget pattern to the skeptic lane's own floor: a DECLARED
// context_window_tokens at or below the prompt overhead derives no tool ceiling
// at all, so every verification that agent reviews yields unverifiable (notes
// window_below_prompt_overhead) — and reconcile's CI gate does not exclude
// unverifiable. The value is legal config nothing rejects at load, and the
// probe cannot catch it (the nonce prompt is trivial), so doctor is the surface
// that has to say so before a run spends its budget on guaranteed-unverifiable
// findings. The zeroBudget verdict already fires for these windows, but its
// hint speaks for the review fan-out's payload sizing and says nothing about
// the verification lane.
func TestRun_SmallDeclaredWindowWarnsAboutVerificationCollapse(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	const window = 4096 // exactly the prompt overhead: input room 0
	cw := window

	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &cw}},
	)
	res, err := ResolveWithCap(reg, &registry.ProjectConfig{Agents: []string{"a"}}, 0)
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	require.Equal(t, "declaration", got.WindowSource,
		"precondition: the warning is about an operator declaration, not a table/default row")
	require.Equal(t, window, got.ContextWindowTokens, "precondition: the declared window reached the report")

	assert.Equal(t, StatusOKWarning, got.Status,
		"a window that cannot fund one tool result quietly fails every check for that agent — doctor must not report ok")
	assert.Contains(t, got.Hint, "DECLARED context_window_tokens",
		"the warning must name the operator's own declaration as the operand")
	assert.Contains(t, got.Hint, "unverifiable",
		"the verification-lane consequence (every check unverifiable) is what the payload-side hint never said")
	assert.Contains(t, got.Hint, "window_below_prompt_overhead",
		"the notes token the skeptic lane emits must match the hint, so an operator can connect the two")
	assert.Contains(t, got.Hint, zeroBudgetRemedy,
		"the remedy is the same declaration knob the zeroBudget verdict points at")
}

// TestSmallWindowClause_Guards pins the pure clause's contract for callers
// other than Run: only a DECLARATION tier, only a healthy probe, and only a
// window that genuinely has no input room fire it. Run composes the clause as
// an append to zeroBudgetVerdict's hint, which always fires for these windows.
func TestSmallWindowClause_Guards(t *testing.T) {
	t.Parallel()

	clause, ok := smallWindowClause("m", 4096, 0, payload.WindowSourceDeclaration, StatusOK)
	require.True(t, ok, "a declared at-overhead window is exactly the case the warning exists for")
	assert.Contains(t, clause, "4096")
	assert.Contains(t, clause, "unverifiable")
	assert.Contains(t, clause, "window_below_prompt_overhead")

	_, ok = smallWindowClause("m", 4096, 0, payload.WindowSourceTable, StatusOK)
	assert.False(t, ok, "a table row is the sizing layer's claim, not an operator statement — no warning")

	_, ok = smallWindowClause("m", 4096, 0, payload.WindowSourceDefault, StatusOK)
	assert.False(t, ok, "the default tier is never an operator statement — no warning")

	// Clarification Q1 (2026-09-07) widened the clause from "at or below the
	// prompt overhead" to "cannot derive a trustworthy tool ceiling". Having
	// input room is no longer enough: 8192 derives 7168 bytes and 4097 derives 3,
	// both far below one tool result, so both collapse every verification for the
	// agent and both must be warned about. The predicate is asked of the verify
	// lane itself so this warning cannot describe a narrower band than the code
	// enforces.
	_, ok = smallWindowClause("m", 8192, 0, payload.WindowSourceDeclaration, StatusOK)
	assert.True(t, ok, "a window with input room but no usable ceiling still collapses verification")

	_, ok = smallWindowClause("m", 4097, 0, payload.WindowSourceDeclaration, StatusOK)
	assert.True(t, ok, "one token of input room derives 3 bytes — the agent verifies nothing")

	_, ok = smallWindowClause("m", 32768, 0, payload.WindowSourceDeclaration, StatusOK)
	assert.False(t, ok, "a window that funds a real read is exactly what the warning must stay silent about")

	_, ok = smallWindowClause("m", 4096, 0, payload.WindowSourceDeclaration, "auth_failed")
	assert.False(t, ok, "an unhealthy probe has a louder problem the warning must not overwrite")

	_, ok = smallWindowClause("m", 0, 0, payload.WindowSourceDeclaration, StatusOK)
	assert.False(t, ok, "window 0 means the window did not resolve, not a zero-token window")
}

// TestRun_UnderivableCeilingWindowWarnsWithoutZeroBudget covers the band that
// clarification Q1 (2026-09-07) moved into the same collapse.
//
// The skeptic lane now refuses any derived tool ceiling below one real tool
// result, so EVERY declared window up to 31012 tokens floors and yields
// unverifiable for every finding — not just the at-or-below-overhead windows
// this file originally covered. Two things follow, and both are asserted here.
//
// First, doctor has to warn across the whole widened band, or the mitigation
// that TD row internal/verify/invoke.go:366 was closed on ("the operator hears
// it before the run") is false for most of the range that now collapses.
//
// Second, the composition changes. zeroBudgetVerdict fires only while
// window <= maxTokens + prompt overhead (12288 at the review default), so above
// that there is no payload hint to append to — the clause has to stand as the
// hint on its own rather than being glued onto an empty string.
func TestRun_UnderivableCeilingWindowWarnsWithoutZeroBudget(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	// 20480: above the zeroBudget threshold (its input budget is a healthy 28672
	// bytes) but still below one tool result, so verification collapses while the
	// payload side sees nothing wrong.
	const window = 20480
	cw := window

	require.Positive(t, payload.EffectiveByteBudget("m", &cw, payload.DefaultOutputTokens),
		"precondition: zeroBudgetVerdict must NOT fire here, or this is the case the other test already covers")

	reg := regWith(
		map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_DOCTOR_KEY", BaseURL: "https://api.example/v1"}},
		map[string]registry.AgentConfig{"a": {Provider: "p", Model: "m", ContextWindowTokens: &cw}},
	)
	res, err := ResolveWithCap(reg, &registry.ProjectConfig{Agents: []string{"a"}}, 0)
	require.NoError(t, err)

	fake := newFake(func(inv llmclient.Invocation) (string, error) {
		return Marker(testNonce), nil
	})

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce})

	require.Len(t, rep.Agents, 1)
	got := rep.Agents[0]
	require.Equal(t, "declaration", got.WindowSource, "precondition: an operator declaration")

	assert.Equal(t, StatusOKWarning, got.Status,
		"a window that cannot derive a trustworthy ceiling fails every verification for that agent")
	assert.Contains(t, got.Hint, "unverifiable",
		"the verification-lane consequence must be named for the whole widened band, not only at-overhead windows")
	assert.Contains(t, got.Hint, "raise (or drop) the declaration",
		"the clause carries its own remedy — zeroBudgetRemedy belongs to the payload hint, which does not fire here")
	assert.NotContains(t, got.Hint, "Separately:",
		"there is no payload hint to append to at this window — the clause must stand on its own")
	assert.Equal(t, strings.TrimSpace(got.Hint), got.Hint,
		"a hint glued onto an empty string leaks its separator whitespace")
}
