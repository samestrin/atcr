package llmclient

// Thinking values an Invocation carries verbatim from the registry (Epic
// 35.16.11.2.2). They duplicate internal/registry's constants so this package
// stays a leaf; a drift test in internal/registry pins the two sets together.
const (
	ThinkingOn  = "on"
	ThinkingOff = "off"

	ThinkingLevelLow    = "low"
	ThinkingLevelMedium = "medium"
	ThinkingLevelHigh   = "high"
	ThinkingLevelMax    = "max"

	ThinkingStyleQwen            = "qwen"
	ThinkingStyleTemplateKwargs  = "template_kwargs"
	ThinkingStyleReasoningEffort = "reasoning_effort"
	ThinkingStyleAnthropic       = "anthropic"
)

// thinkingFields is the set of request-body members that carry a declared
// thinking setting. It is embedded in both chatRequest and chatToolRequest so
// the two request paths cannot drift. Every member is an omitempty pointer: an
// undeclared agent's zero value adds nothing to the body, and a declared false
// is still sent.
type thinkingFields struct {
	EnableThinking     *bool               `json:"enable_thinking,omitempty"`
	ThinkingBudget     *int                `json:"thinking_budget,omitempty"`
	ChatTemplateKwargs *chatTemplateKwargs `json:"chat_template_kwargs,omitempty"`
	ReasoningEffort    *string             `json:"reasoning_effort,omitempty"`
	Thinking           *anthropicThinking  `json:"thinking,omitempty"`
}

// chatTemplateKwargs is the template_kwargs style's wire object.
type chatTemplateKwargs struct {
	EnableThinking *bool `json:"enable_thinking,omitempty"`
}

// anthropicThinking is the anthropic style's wire object. BudgetTokens is
// omitted when thinking is disabled.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// thinkingBudgets is the one level-to-budget table, read by the qwen
// thinking_budget and anthropic budget_tokens fields.
var thinkingBudgets = map[string]int{
	ThinkingLevelLow:    2048,
	ThinkingLevelMedium: 8192,
	ThinkingLevelHigh:   16384,
	ThinkingLevelMax:    32768,
}

// newThinkingFields maps a declared thinking setting onto its style's wire
// members. A level alone means on. Nothing is sent unless a style and either
// thinking or a level are declared, and an unknown style is never coerced into
// another style's shape. Legality is the registry's job, not this layer's.
func newThinkingFields(thinking, level, style string) thinkingFields {
	if thinking == "" && level == "" {
		return thinkingFields{}
	}
	on := thinking == ThinkingOn || (thinking == "" && level != "")
	var f thinkingFields
	switch style {
	case ThinkingStyleQwen:
		f.EnableThinking = &on
		if b, ok := thinkingBudgets[level]; ok && on {
			f.ThinkingBudget = &b
		}
	case ThinkingStyleTemplateKwargs:
		f.ChatTemplateKwargs = &chatTemplateKwargs{EnableThinking: &on}
	case ThinkingStyleReasoningEffort:
		// The style has no off value and accepts nothing above high.
		if on && level != "" {
			effort := level
			if effort == ThinkingLevelMax {
				effort = ThinkingLevelHigh
			}
			f.ReasoningEffort = &effort
		}
	case ThinkingStyleAnthropic:
		if !on {
			f.Thinking = &anthropicThinking{Type: "disabled"}
			break
		}
		// Anthropic requires a budget when enabled, so on with no level takes medium.
		if level == "" {
			level = ThinkingLevelMedium
		}
		f.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: thinkingBudgets[level]}
	}
	return f
}

// ThinkingBudgetTokens returns the thinking budget the declared setting sends,
// or 0 when it sends none. It reads the same mapping the request body uses.
func ThinkingBudgetTokens(thinking, level, style string) int {
	f := newThinkingFields(thinking, level, style)
	switch {
	case f.ThinkingBudget != nil:
		return *f.ThinkingBudget
	case f.Thinking != nil:
		return f.Thinking.BudgetTokens
	}
	return 0
}
