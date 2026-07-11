package payload

import "strings"

// defaultContextWindowTokens is the conservative context window applied to any
// model id absent from the static table below. 32768 is the smallest window
// empirically observed across the roster: in the 19.6 live run the local
// litellm proxy served `dax` a 32,768-token window (its overflow error reported
// "maximum context length is 32768 tokens"). Defaulting unknown or unverified
// models to this small value guarantees we UNDER-fill rather than overflow.
//
// Per the plan's Conservatism NFR an under-filled payload is strictly better
// than a failed agent, and the on_overflow net (F4) catches any residual tail.
// Newly released or locally-aliased models therefore under-fill until Epic 19.7
// (Live Model Resolution) can feed live windows behind this same signature —
// an accepted trade-off (see plan 19.10 F1 Risk Mitigation).
const defaultContextWindowTokens = 32768

// contextWindowTokens is the single source of truth mapping a model id to its
// context-window size in tokens. It is intentionally STATIC (no hot-path
// network call, satisfying the Determinism NFR) and CONSERVATIVE:
//
//   - Keys are model-id strings EXACTLY as they appear in AgentConfig.Model
//     (the same string that keys diffCacheKey and AgentStatus.Model), NOT
//     persona names. The plan's `dax → 32768` / `otto → 144941` shorthand names
//     the reviewer, not the key; this table keys on the model that reviewer
//     runs. Confirmed by the 19.6 run, the local roster reaches models through
//     litellm aliases (e.g. `minimax-m3-mira`, `kimi-k2.7-code`) whose true
//     windows are not recorded in the repo — those aliases are intentionally
//     absent here and resolve to the conservative default, matching the small
//     windows the proxy actually serves (the 19.6 overflow root cause).
//
//   - Windows are conservative floors for models served at their full published
//     window (e.g. via OpenRouter, as the community personas are). Several
//     entries support more than listed; under-listing only under-fills, which
//     is safe. Over-listing would re-introduce the exact overflow this sprint
//     fixes, so it is avoided.
//
// This map is the seam Epic 19.7 will later populate from a live catalog.
var contextWindowTokens = map[string]int{
	// Anthropic Claude — 200k published context.
	"anthropic/claude-opus-4.8": 200000,
	"anthropic/claude-sonnet-5": 200000,
	// Google Gemini 2.5 — 1M published context.
	"google/gemini-2.5-pro":   1000000,
	"google/gemini-2.5-flash": 1000000,
	// ~128k-class models (conservative floor; several publish larger windows).
	"openai/gpt-5.5":            128000,
	"openai/gpt-5.4-mini":       128000,
	"deepseek/deepseek-v4-pro":  128000,
	"moonshotai/kimi-k2.7-code": 128000,
	"qwen/qwen3-coder-plus":     128000,
	"z-ai/glm-5.2":              128000,
}

// ContextWindowTokens returns model's context-window size in tokens. A model id
// not present in the static table receives a conservative default. The function
// never returns zero and never errors, so callers can size a payload against
// the result unconditionally without a nil/zero guard.
//
// The returned value is the model's FULL window; callers reserve the output-cap
// and prompt overhead from it when deriving an effective input budget (F2). It
// is intentionally distinct from the per-chunk diff-line budget MaxContextLines,
// which counts diff lines, not tokens.
func ContextWindowTokens(model string) int {
	if w, ok := contextWindowTokens[strings.TrimSpace(model)]; ok {
		return w
	}
	return defaultContextWindowTokens
}
