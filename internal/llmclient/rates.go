package llmclient

import "strings"

// rateUSDPerMillion is a model's price in USD per one million tokens, split by
// input (prompt) and output (completion) direction.
type rateUSDPerMillion struct {
	input  float64
	output float64
}

// modelRates maps a known model identifier to its USD-per-million-token rates.
//
// Source: provider public pricing as of 2026-06. These values are approximate
// and intended to be maintained as pricing changes; they drive the scorecard's
// cost_usd column. A model absent from this table yields zero cost rather than a
// guess (see ComputeCostUSD), so the scorecard degrades gracefully — the cost
// column shows the unknown-model record as zero instead of a fabricated figure.
var modelRates = map[string]rateUSDPerMillion{
	// Anthropic Claude 4.x family.
	"claude-opus-4-8":   {input: 15.0, output: 75.0},
	"claude-opus-4-7":   {input: 15.0, output: 75.0},
	"claude-opus-4-6":   {input: 15.0, output: 75.0},
	"claude-sonnet-4-6": {input: 3.0, output: 15.0},
	"claude-haiku-4-5":  {input: 1.0, output: 5.0},
	// Common OpenAI-compatible models.
	"gpt-4o":      {input: 2.5, output: 10.0},
	"gpt-4o-mini": {input: 0.15, output: 0.6},
}

// CostUSD returns the USD cost of this usage for the given model. It maps the
// prompt/completion fields to ComputeCostUSD's input/output arguments in exactly
// one place so callers cannot transpose the two counts.
func (u UsageData) CostUSD(model string) float64 {
	return ComputeCostUSD(model, u.PromptTokens, u.CompletionTokens)
}

// normalizeModelKey reduces a provider/gateway-decorated model id to the bare
// key used in modelRates. The same priced model arrives decorated several ways:
// a trailing variant marker (`claude-opus-4-8[1m]`), an OpenRouter-style provider
// prefix (`anthropic/claude-sonnet-4-6`), or a Bedrock-style region.provider
// prefix (`us.anthropic.claude-sonnet-4-6`). The bare id is recovered by dropping
// a trailing `[...]` marker, then everything up to and including the last `/`,
// then a leading `...anthropic.` dotted prefix. An id needing no change — and any
// genuinely unknown id — passes through unaltered, so normalization never
// invents a price for a model that is not in the table.
func normalizeModelKey(model string) string {
	s := model
	if i := strings.IndexByte(s, '['); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "anthropic."); i >= 0 {
		s = s[i+len("anthropic."):]
	}
	return strings.TrimSpace(s)
}

// ComputeCostUSD returns the USD cost of a call given its model and token
// counts. The model id is normalized (provider prefixes and variant suffixes
// stripped) before lookup. Unknown models return 0 (never a panic), and a
// zero-value UsageData (tokensIn == tokensOut == 0) yields 0 for any model.
// Negative token counts are clamped to 0 so a malformed provider response cannot
// produce a negative cost.
func ComputeCostUSD(model string, tokensIn, tokensOut int) float64 {
	rate, ok := modelRates[normalizeModelKey(model)]
	if !ok {
		return 0
	}
	if tokensIn < 0 {
		tokensIn = 0
	}
	if tokensOut < 0 {
		tokensOut = 0
	}
	return float64(tokensIn)/1_000_000*rate.input + float64(tokensOut)/1_000_000*rate.output
}
