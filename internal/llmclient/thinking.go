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

// newThinkingFields maps a declared thinking setting onto its style's wire
// members.
func newThinkingFields(thinking, level, style string) thinkingFields {
	return thinkingFields{}
}

// ThinkingBudgetTokens returns the thinking budget the declared setting sends,
// or 0 when it sends none.
func ThinkingBudgetTokens(thinking, level, style string) int {
	return 0
}
