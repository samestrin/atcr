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
