package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TD row internal/llmclient/thinking.go:47: the "thinking on" predicate — on,
// or a level alone implying on — is implemented three times under one contract
// (the llmclient wire mapper, ThinkingBudgetTokens, and validateThinking).
// ThinkingEnabled is the single exported form all three consume, so the
// layers cannot drift.
func TestThinkingEnabled(t *testing.T) {
	cases := []struct {
		name            string
		thinking, level string
		want            bool
	}{
		{"on no level", ThinkingOn, "", true},
		{"level alone implies on", "", ThinkingLevelMedium, true},
		{"on with level", ThinkingOn, ThinkingLevelHigh, true},
		{"off", ThinkingOff, "", false},
		{"off with level", ThinkingOff, ThinkingLevelMedium, false},
		{"nothing declared", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ThinkingEnabled(tc.thinking, tc.level))
		})
	}
}

// TD row internal/fanout/review.go:2945: "declared" is not "enabled". thinking:
// off is declared (it sends a wire field) but not on; a thinking_style alone is
// neither, because a style with no thinking or level sends nothing. The cache
// key and the model-invocation telemetry gate on this, as doctor does.
func TestThinkingDeclared(t *testing.T) {
	cases := []struct {
		name            string
		thinking, level string
		want            bool
	}{
		{"unset", "", "", false},
		{"style only (the style is not an input: it declares nothing)", "", "", false},
		{"thinking only", ThinkingOn, "", true},
		{"level only", "", ThinkingLevelLow, true},
		{"thinking off", ThinkingOff, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ThinkingDeclared(tc.thinking, tc.level))
		})
	}
}
