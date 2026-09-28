package doctor

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD-020: the marker probe carries the thinking declaration, so an explicit
// --max-tokens at or below an anthropic budget makes Anthropic reject the probe
// (budget_tokens >= max_tokens). The hint names the flag as the cause instead of
// leaving the operator to diagnose a bare 400.
func TestRun_FlagCapAtOrBelowAnthropicBudgetNamesTheFlag(t *testing.T) {
	cases := []struct {
		name     string
		set      bool
		maxTok   int
		err      error
		wantHint []string
		notHint  string
		style    string // default anthropic
		wantThk  string // expected thinking status, when set
	}{
		{name: "flag below budget, 400", set: true, maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "budget_tokens must be less than max_tokens"},
			wantHint: []string{"--max-tokens 4096", "16384"}},
		{name: "flag equal to budget, 400", set: true, maxTok: 16384, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"},
			wantHint: []string{"--max-tokens 16384"}},
		{name: "flag above budget", set: true, maxTok: 32768, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"}, notHint: "--max-tokens"},
		{name: "no flag", maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"}, notHint: "--max-tokens"},
		{name: "auth failure keeps its own hint", set: true, maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 401, Snippet: "bad key"},
			wantHint: []string{"check the API key"}, notHint: "--max-tokens"},
		// Only anthropic shares budget_tokens with max_tokens: a qwen 400 under a
		// low --max-tokens is the declaration being refused, so no flag hint and
		// the control call decides not_honored.
		{name: "qwen, flag below budget, 400", style: registry.ThinkingStyleQwen, set: true, maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"},
			notHint: "--max-tokens", wantThk: ThinkingNotHonored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style := tc.style
			if style == "" {
				style = registry.ThinkingStyleAnthropic
			}
			res := thinkingTarget(t, registry.ThinkingOn, registry.ThinkingLevelHigh, style)
			t.Setenv(rfDoctorEnvK, thinkingKey)
			fake := newFake(markerOK)
			// Only the declared call is rejected; the control call (no declaration,
			// so no budget_tokens) succeeds, as it would against Anthropic.
			n := 0
			fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) {
				n++
				if n == 1 {
					return llmclient.Completion{}, tc.err
				}
				return silent, nil
			}
			rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: tc.maxTok, MaxTokensSet: tc.set})
			require.Len(t, rep.Agents, 1)
			hint := rep.Agents[0].Hint
			for _, want := range tc.wantHint {
				assert.Contains(t, hint, want)
			}
			if tc.notHint != "" {
				assert.NotContains(t, hint, tc.notHint)
			}
			if tc.wantThk != "" {
				assert.Equal(t, tc.wantThk, rep.Agents[0].ThinkingStatus, "detail: %s", rep.Agents[0].ThinkingDetail)
			}
			// The flag caused the rejection, not the provider refusing the
			// declaration, so the control call must not turn it into not_honored.
			if len(tc.wantHint) > 0 && tc.wantHint[0] != "check the API key" {
				assert.NotEqual(t, ThinkingNotHonored, rep.Agents[0].ThinkingStatus, "detail: %s", rep.Agents[0].ThinkingDetail)
			}
		})
	}
}
