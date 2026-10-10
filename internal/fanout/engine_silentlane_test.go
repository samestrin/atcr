package fanout

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
)

// A thinking-declared reviewer that answers NO FINDINGS in a handful of tokens on
// a huge payload is warned about once (Epic 35.16.11.2.2.7 T1): the ronin-backup
// shape from the 35.16.11.2.2 panel run, 10 tokens out on about 158k in. Every
// other clean review stays silent, and the warning never changes the result.
func TestInvokeSlot_SilentLaneWarning(t *testing.T) {
	const msg = `msg="reviewer answered NO FINDINGS in very few tokens on a large payload; the lane may have skipped its review"`
	usage := func(in, out int) llmclient.UsageData {
		return llmclient.UsageData{PromptTokens: in, CompletionTokens: out}
	}

	cases := []struct {
		name          string
		content       string
		usage         llmclient.UsageData
		thinking      string
		thinkingLevel string
		wantWarnings  int
	}{
		{"ronin-backup shape", "NO FINDINGS", usage(158304, 10), registry.ThinkingOn, "", 1},
		{"think-wrapped sentinel", "<think>looked briefly</think>\nNO FINDINGS", usage(158304, 10), registry.ThinkingOn, "", 1},
		{"a level alone declares thinking on", "NO FINDINGS", usage(158304, 10), "", registry.ThinkingLevelLow, 1},
		{"400 tokens out is a real review", "NO FINDINGS", usage(158304, 400), registry.ThinkingOn, "", 0},
		{"50 tokens out is not under the bound", "NO FINDINGS", usage(158304, silentLaneMaxTokensOut), registry.ThinkingOn, "", 0},
		{"5k tokens in is a small payload", "NO FINDINGS", usage(5000, 10), registry.ThinkingOn, "", 0},
		{"10k tokens in is not over the bound", "NO FINDINGS", usage(silentLaneMinTokensIn, 10), registry.ThinkingOn, "", 0},
		{"no usage reported", "NO FINDINGS", usage(0, 0), registry.ThinkingOn, "", 0},
		{"no thinking declaration", "NO FINDINGS", usage(158304, 10), "", "", 0},
		{"thinking off", "NO FINDINGS", usage(158304, 10), registry.ThinkingOff, registry.ThinkingLevelLow, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slot := Slot{Primary: Agent{Name: "ronin-backup", Invocation: llmclient.Invocation{
				Model: "primary", Thinking: tc.thinking, ThinkingLevel: tc.thinkingLevel,
			}}}
			c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
				"primary": {Content: tc.content, Usage: tc.usage},
			}}
			ctx, buf := failoverLogCapture()
			r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

			assert.Equal(t, tc.wantWarnings, strings.Count(buf.String(), msg), buf.String())
			if tc.wantWarnings == 1 {
				assert.Equal(t,
					`level=WARN `+msg+` agent=ronin-backup model=primary tokens_in=158304 tokens_out=10`+"\n",
					buf.String())
			}
			// A warning only: the clean review is recorded exactly as before.
			assert.Equal(t, StatusOK, r.Status)
			assert.NoError(t, r.Err)
			assert.Zero(t, r.ParsedFindingCount())
			assert.False(t, r.UnparseableResponse)
			assert.False(t, r.FallbackUsed)
		})
	}
}
