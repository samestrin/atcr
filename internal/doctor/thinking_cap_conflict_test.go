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
	}{
		{name: "flag below budget, 400", set: true, maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "budget_tokens must be less than max_tokens"},
			wantHint: []string{"--max-tokens 4096", "16384"}},
		{name: "flag equal to budget, 400", set: true, maxTok: 16384, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"},
			wantHint: []string{"--max-tokens 16384"}},
		{name: "flag above budget", set: true, maxTok: 32768, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"}, notHint: "--max-tokens"},
		{name: "no flag", maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 400, Snippet: "x"}, notHint: "--max-tokens"},
		{name: "auth failure keeps its own hint", set: true, maxTok: 4096, err: &llmclient.HTTPStatusError{Status: 401, Snippet: "bad key"},
			wantHint: []string{"check the API key"}, notHint: "--max-tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := thinkingTarget(t, registry.ThinkingOn, registry.ThinkingLevelHigh, registry.ThinkingStyleAnthropic)
			t.Setenv(rfDoctorEnvK, thinkingKey)
			fake := newFake(markerOK)
			fake.metaFn = func(llmclient.Invocation) (llmclient.Completion, error) { return llmclient.Completion{}, tc.err }
			rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: tc.maxTok, MaxTokensSet: tc.set})
			require.Len(t, rep.Agents, 1)
			hint := rep.Agents[0].Hint
			for _, want := range tc.wantHint {
				assert.Contains(t, hint, want)
			}
			if tc.notHint != "" {
				assert.NotContains(t, hint, tc.notHint)
			}
		})
	}
}
