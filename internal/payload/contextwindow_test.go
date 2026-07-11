package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContextWindowTokens_KnownModels asserts each seeded model id resolves to
// its listed window. The table covers a small/default-window model and a large
// (>144k) window model per AC1: a 32k-window model (any unlisted id, resolving
// to the conservative default) and a 144k+ window model (Claude at 200k).
func TestContextWindowTokens_KnownModels(t *testing.T) {
	cases := map[string]int{
		"anthropic/claude-opus-4.8": 200000,
		"anthropic/claude-sonnet-5": 200000,
		"google/gemini-2.5-pro":     1000000,
		"google/gemini-2.5-flash":   1000000,
		"openai/gpt-5.5":            128000,
		"openai/gpt-5.4-mini":       128000,
		"deepseek/deepseek-v4-pro":  128000,
		"moonshotai/kimi-k2.7-code": 128000,
		"qwen/qwen3-coder-plus":     128000,
		"z-ai/glm-5.2":              128000,
	}
	for model, want := range cases {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, want, ContextWindowTokens(model))
		})
	}
}

// TestContextWindowTokens_SmallAndLargeWindow is the explicit AC1 pairing: a
// 32k-window model and a large-window (>144k) model resolve to distinct,
// correctly-ordered windows so downstream per-agent sizing (F2) can chunk a
// small-window model more than a large one.
func TestContextWindowTokens_SmallAndLargeWindow(t *testing.T) {
	small := ContextWindowTokens("some-local-32k-alias") // unlisted → default
	large := ContextWindowTokens("anthropic/claude-opus-4.8")
	assert.Equal(t, 32768, small)
	assert.Equal(t, 200000, large)
	assert.Greater(t, large, small)
}

// TestContextWindowTokens_UnknownDefaults asserts an unmapped model id returns
// the conservative default and never zero — including the local litellm aliases
// the 19.6 roster actually used, which are intentionally absent from the table.
func TestContextWindowTokens_UnknownDefaults(t *testing.T) {
	unknown := []string{
		"totally-unknown-model",
		"minimax-m3-mira",                   // 19.6 local alias (bruce/mira)
		"kimi-k2.7-code",                    // 19.6 local alias (kai)
		"glm-5.2",                           // 19.6 local alias (archer); note: hosted "z-ai/glm-5.2" IS listed
		"NVIDIA-Nemotron-3-Super-120B-A12B", // 19.6 local alias (pace)
		"",
	}
	for _, m := range unknown {
		t.Run(m, func(t *testing.T) {
			got := ContextWindowTokens(m)
			assert.Equal(t, defaultContextWindowTokens, got)
			assert.NotZero(t, got)
		})
	}
}

// TestContextWindowTokens_TrimsWhitespace asserts keys resolve after trimming,
// matching the trimmed-string handling used across the config precedence chain.
func TestContextWindowTokens_TrimsWhitespace(t *testing.T) {
	assert.Equal(t, 200000, ContextWindowTokens("  anthropic/claude-opus-4.8  "))
}

// TestContextWindowTokens_AllPersonasCoveredOrDefault is the AC1 regression
// guard: every model id a roster persona can be configured with is either
// present in the static table or receives the conservative default — never
// zero, never an error. Uses a stable fixture of the roster's known model ids
// (the community-persona hosted ids plus the confirmed 19.6 local aliases) so
// the guard is deterministic and not coupled to on-disk config discovery.
func TestContextWindowTokens_AllPersonasCoveredOrDefault(t *testing.T) {
	rosterModels := []string{
		// Hosted (OpenRouter) ids from personas/community/*.yaml.
		"anthropic/claude-opus-4.8",
		"anthropic/claude-sonnet-5",
		"deepseek/deepseek-v4-pro",
		"google/gemini-2.5-flash",
		"google/gemini-2.5-pro",
		"moonshotai/kimi-k2.7-code",
		"openai/gpt-5.4-mini",
		"openai/gpt-5.5",
		"qwen/qwen3-coder-plus",
		"z-ai/glm-5.2",
		// Confirmed local litellm aliases from the 19.6 run.
		"minimax-m3-mira",
		"kimi-k2.7-code",
		"glm-5.2",
		"NVIDIA-Nemotron-3-Super-120B-A12B",
	}
	for _, m := range rosterModels {
		t.Run(m, func(t *testing.T) {
			w := ContextWindowTokens(m)
			assert.GreaterOrEqual(t, w, defaultContextWindowTokens,
				"every roster model must resolve to at least the conservative default, never zero")
		})
	}
}
