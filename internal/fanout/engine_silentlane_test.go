package fanout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			// The fit signal (Epic 35.16.11.2.2.8 T1) rides the same predicate:
			// counted exactly when the warning fires, never otherwise.
			assert.Equal(t, tc.wantWarnings, r.SilentChunks)
		})
	}
}

// silentLaneResult drives one invokeSlot call the way TestInvokeSlot_SilentLaneWarning
// does and returns the Result.
func silentLaneResult(t *testing.T, content string, in, out int, thinking string) Result {
	t.Helper()
	slot := Slot{Primary: Agent{Name: "pace", Invocation: llmclient.Invocation{
		Model: "primary", Thinking: thinking,
	}}}
	c := &mapMetaCompleter{byModel: map[string]llmclient.Completion{
		"primary": {Content: content, Usage: llmclient.UsageData{PromptTokens: in, CompletionTokens: out}},
	}}
	ctx, _ := failoverLogCapture()
	return NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)
}

// The pace-shaped lane (NO FINDINGS, 10 tokens out on about 158k in, thinking on)
// reaches status.json as silent_chunks 1; a normal clean review and a non-thinking
// clean review leave the field absent. ReviewerOutcome is untouched: all three are
// clean (Epic 35.16.11.2.2.8 T1, AC1).
func TestStatusFor_SilentChunks(t *testing.T) {
	cases := []struct {
		name     string
		r        Result
		want     int
		wantJSON bool
	}{
		{"pace-shaped silent lane", silentLaneResult(t, "NO FINDINGS", 158304, 10, registry.ThinkingOn), 1, true},
		{"normal clean review", silentLaneResult(t, "NO FINDINGS", 158304, 400, registry.ThinkingOn), 0, false},
		{"non-thinking clean review", silentLaneResult(t, "NO FINDINGS", 158304, 3, registry.ThinkingOff), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := statusFor(tc.r, findingsFor(tc.r, nil))
			assert.Equal(t, tc.want, st.SilentChunks)
			raw, err := json.Marshal(st)
			require.NoError(t, err)
			assert.Equal(t, tc.wantJSON, strings.Contains(string(raw), `"silent_chunks":1`), string(raw))
			if !tc.wantJSON {
				assert.NotContains(t, string(raw), "silent_chunks")
			}
			assert.Equal(t, "clean", ReviewerOutcome(st, 0))
		})
	}
}

// mergeResultGroup sums SilentChunks over every chunk, chunk 0 included, beside
// UnparseableChunks, so a persona is wholly silent exactly when the sum reaches
// ChunkCount.
func TestMergeResultGroup_SumsSilentChunks(t *testing.T) {
	silent := silentLaneResult(t, "NO FINDINGS", 158304, 10, registry.ThinkingOn)
	clean := silentLaneResult(t, "NO FINDINGS", 158304, 400, registry.ThinkingOn)

	// ChunkCount is the sizing stamp, not set by this merge, so the group length
	// stands in for it here.
	minority := mergeResultGroup([]Result{silent, clean, silent}, nil)
	assert.Equal(t, 2, minority.SilentChunks, "one productive chunk keeps the persona from being wholly silent (2 < 3)")

	whole := mergeResultGroup([]Result{silent, silent}, nil)
	assert.Equal(t, 2, whole.SilentChunks, "every chunk silent is a wholly silent persona (2 >= 2)")

	none := mergeResultGroup([]Result{clean, clean}, nil)
	assert.Zero(t, none.SilentChunks)
}
