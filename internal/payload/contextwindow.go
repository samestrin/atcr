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

// contextWindowTokensCap is the payload-layer defense-in-depth ceiling for a
// per-agent declaration. It matches registry.ContextWindowTokensCap (10M tokens)
// so a directly-constructed AgentConfig carrying a value above the registry's
// validateAgent ceiling is clamped to the static table / default, mirroring the
// non-positive-declaration guard below.
//
// The bound is INCLUSIVE, matching the registry's own "must be within 1..%d"
// (config.go), which accepts the cap value exactly — as max_context_lines does at
// its cap. An exclusive bound here made the two ends disagree by one and did it
// SILENTLY: a legal declaration of exactly 10000000 loaded without error and then
// resolved to the static table or the 32768 default, with nothing logged. That is
// the same silent mis-resolution the loader rejects a declared 0 to prevent. A declaration at or above this cap would
// drive effectiveTokens * conservativeBytesPerTokenNum toward int64 overflow in
// EffectiveByteBudget, returning a NEGATIVE byte count and breaking the
// "never a negative byte count" contract.
const contextWindowTokensCap = 10000000

// ContextWindowTokens returns the model's context-window size in tokens, resolved in
// three tiers (Epic 35.16.5.1):
//
//	declared (the agent's own context_window_tokens) -> static table -> default
//
// declared is the operator's per-agent declaration, nil when the agent did not
// make one; it wins over the static table. A model id absent from the table and
// carrying no declaration receives the conservative default. The function never
// returns zero and never errors, so callers can size a payload unconditionally.
//
// A non-positive or above-cap declaration is ignored and the resolver falls
// through to the static table (or the default) — defense-in-depth against a
// directly-constructed AgentConfig bypassing validateAgent. The loader at
// internal/registry/config.go rejects non-positive values at load for a
// different reason: a declared 0 would otherwise be silently ignored by THIS
// guard (the > 0 check fails, falling through to the table), so an operator
// typo like `context_window_tokens: 0` would watch the resolver hand back the
// static-table window (or 32768) without any error. Load-time rejection
// converts that silent mis-resolution into a loud, attributable error.
//
// The returned value is the model's FULL window; callers reserve the output-cap
// and prompt overhead from it when deriving an effective input budget. It is
// distinct from the per-chunk diff-line budget MaxContextLines, which counts
// diff lines, not tokens.
func ContextWindowTokens(model string, declared *int) int {
	if declared != nil && *declared > 0 && *declared <= contextWindowTokensCap {
		return *declared
	}
	if w, ok := contextWindowTokens[strings.TrimSpace(model)]; ok {
		return w
	}
	return defaultContextWindowTokens
}
