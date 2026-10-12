package registry

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.11.2.2.9 AC1: replay_reasoning loads only as off or unset; any
// other value, including the on that thinking and preserve_thinking accept,
// fails load with an error naming the agent. Like response_format it uses
// strict equality, so a bare YAML bool or a near-miss case fails here.
func TestValidateAgent_ReplayReasoning(t *testing.T) {
	const wantErr = `agent 'myagent': invalid replay_reasoning %q: must be "off" or unset`
	cases := []struct {
		name, value string
		ok          bool
	}{
		{"unset", "", true},
		{"off", ReplayReasoningOff, true},
		{"on", "on", false},
		{"bare false", "false", false},
		{"bare true", "true", false},
		{"wrong case", "OFF", false},
		{"other", "never", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureThinkingWarnings(t)
			agent := thinkingAgent("", "", "")
			if tc.value != "" {
				agent += "    replay_reasoning: " + tc.value + "\n"
			}
			reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent)))
			if tc.ok {
				require.NoError(t, err)
				assert.Equal(t, tc.value, reg.Agents["myagent"].ReplayReasoning, "decoded verbatim")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf(wantErr, tc.value))
		})
	}
}

// replay_reasoning is independent of the thinking keys: an agent may opt out
// of replay with or without declaring thinking, under any style.
func TestValidateAgent_ReplayReasoningNeedsNoThinkingKeys(t *testing.T) {
	for _, agent := range []string{
		thinkingAgent(ThinkingOn, "", ThinkingStyleQwen),
		thinkingAgent(ThinkingOff, "", ThinkingStyleGLM),
		thinkingAgent("", ThinkingLevelLow, ThinkingStyleReasoningEffort),
	} {
		captureThinkingWarnings(t)
		_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(agent+"    replay_reasoning: off\n")))
		require.NoError(t, err)
	}
}

// AC2 (registry half): the registry never merges a primary into its fallback,
// so this proves DECODE-LEVEL independence only, as
// TestAgentConfig_PreserveThinkingDecodesIndependentlyOfFallback does for
// preserve_thinking. The lane-wiring proof (buildFallbackAgent sends its own
// value) lives in internal/fanout.
func TestAgentConfig_ReplayReasoningDecodesIndependentlyOfFallback(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, thinkingRegistry(`
  primary:
    provider: p
    model: m
    replay_reasoning: off
    fallback: secondary
  secondary:
    provider: p
    model: m
  own:
    provider: p
    model: m
    replay_reasoning: off
  usesown:
    provider: p
    model: m
    fallback: own
`)))
	require.NoError(t, err)
	assert.Empty(t, reg.Agents["secondary"].ReplayReasoning, "a fallback must not inherit its primary's value")
	assert.Empty(t, reg.Agents["usesown"].ReplayReasoning, "a primary must not inherit its fallback's value")
	assert.Equal(t, ReplayReasoningOff, reg.Agents["primary"].ReplayReasoning)
	assert.Equal(t, ReplayReasoningOff, reg.Agents["own"].ReplayReasoning)
}

// AC1: a community persona may not declare replay_reasoning — whether an
// endpoint tolerates a replayed reasoning member is specific to the endpoint
// each consumer resolves, as for the thinking keys.
func TestRejectMachineLocalFields_ReplayReasoningBanned(t *testing.T) {
	const base = "name: sample\nprovider: openrouter\nmodel: anthropic/claude-opus-4.8\n"
	err := ValidateCommunityPersonaYAML("sample", []byte(base+"replay_reasoning: off\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `community persona "sample" must not declare replay_reasoning:`)
	require.NoError(t, ValidateCommunityPersonaYAML("sample", []byte(base)))
}

// TD row config.go:1480: preserve_thinking: on asks the model to keep its
// earlier reasoning, which only the tool loop's replay sends back, so with
// replay_reasoning: off the pair is a no-op. It still loads, with exactly one
// warning naming the agent; either key alone, or preserve_thinking: off, loads
// with none.
func TestValidateAgent_PreserveThinkingWithoutReplayWarns(t *testing.T) {
	const preserve = "    preserve_thinking: "
	const replayOff = "    replay_reasoning: off\n"
	base := thinkingAgent(ThinkingOn, "", ThinkingStyleQwen)
	cases := []struct {
		name, extra string
		warns       bool
	}{
		{"pair", preserve + ThinkingOn + "\n" + replayOff, true},
		{"preserve on alone", preserve + ThinkingOn + "\n", false},
		{"replay off alone", replayOff, false},
		{"preserve off with replay off", preserve + ThinkingOff + "\n" + replayOff, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureThinkingWarnings(t)
			_, err := LoadRegistry(writeRegistry(t, thinkingRegistry(base+tc.extra)))
			require.NoError(t, err, "the pair warns, it never fails the load")
			if !tc.warns {
				assert.Empty(t, buf.String())
				return
			}
			assert.Equal(t, 1, strings.Count(buf.String(), "\n"), "exactly one warning: %q", buf.String())
			assert.Contains(t, buf.String(), "warning: agent 'myagent': preserve_thinking: on has no effect with replay_reasoning: off")
		})
	}
}
