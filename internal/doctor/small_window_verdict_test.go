package doctor

import (
	"context"
	"testing"

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
