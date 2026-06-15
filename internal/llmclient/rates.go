package llmclient

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

// ComputeCostUSD returns the USD cost of a call given its model and token
// counts. Unknown models return 0 (never a panic), and a zero-value UsageData
// (tokensIn == tokensOut == 0) yields 0 for any model. Negative token counts are
// clamped to 0 so a malformed provider response cannot produce a negative cost.
func ComputeCostUSD(model string, tokensIn, tokensOut int) float64 {
	rate, ok := modelRates[model]
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
