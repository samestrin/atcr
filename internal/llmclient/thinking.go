package llmclient

import (
	"slices"

	"github.com/samestrin/atcr/internal/registry"
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
	Thinking           *thinkingObject     `json:"thinking,omitempty"`
	PreserveThinking   *bool               `json:"preserve_thinking,omitempty"`

	// dropTemperature records, on the fields themselves, that this declaration
	// must send no temperature (enabled anthropic thinking — the provider
	// rejects any value but 1). It is set in newThinkingFields' anthropic case
	// and read by temperatureFor, which takes no style parameter: the decision
	// cannot be re-derived inconsistently by a caller passing a mismatched
	// style next to fields built from another one. Unexported, so it never
	// reaches a request body.
	dropTemperature bool
}

// chatTemplateKwargs is the template_kwargs style's wire object.
type chatTemplateKwargs struct {
	EnableThinking *bool `json:"enable_thinking,omitempty"`
}

// thinkingObject is the top-level thinking member, shared by the anthropic and
// glm styles: one JSON key cannot hold two fields, and only one style is ever
// active. BudgetTokens is anthropic's and is omitted when thinking is disabled;
// ClearThinking is glm's preserved-thinking flag.
type thinkingObject struct {
	Type          string `json:"type"`
	BudgetTokens  int    `json:"budget_tokens,omitempty"`
	ClearThinking *bool  `json:"clear_thinking,omitempty"`
}

// newThinkingFields maps a declared thinking setting onto its style's wire
// members. A level alone means on. Nothing is sent unless a style and either
// thinking or a level are declared. preserve renders only under qwen and glm
// with thinking on. Legality is the registry's job, so a value it would reject
// (an unknown style, thinking value, level, or preserve value) is never
// guessed at: nothing is sent.
func newThinkingFields(thinking, level, style, preserve string) thinkingFields {
	// The declared-ness gate is the registry predicate, not a hand-written
	// copy: this check IS the wire rule the cache key and the telemetry echo
	// mirror, so it must share one definition with them or the three can
	// silently disagree (TD internal/llmclient/thinking.go:54).
	if !registry.ThinkingDeclared(thinking, level) {
		return thinkingFields{}
	}
	if thinking != "" && thinking != registry.ThinkingOn && thinking != registry.ThinkingOff {
		return thinkingFields{}
	}
	if level != "" && !slices.Contains(registry.ThinkingLevels(), level) {
		return thinkingFields{}
	}
	if preserve != "" && !slices.Contains(registry.ThinkingValues(), preserve) {
		return thinkingFields{}
	}
	on := registry.ThinkingEnabled(thinking, level)
	var keep *bool
	if on && preserve != "" {
		v := preserve == registry.ThinkingOn
		keep = &v
	}
	budget := registry.ThinkingBudgetTokens(thinking, level, style)
	var f thinkingFields
	switch style {
	case registry.ThinkingStyleQwen:
		f.EnableThinking = &on
		if budget > 0 {
			f.ThinkingBudget = &budget
		}
		f.PreserveThinking = keep
	case registry.ThinkingStyleTemplateKwargs:
		f.ChatTemplateKwargs = &chatTemplateKwargs{EnableThinking: &on}
	case registry.ThinkingStyleReasoningEffort:
		// The style has no off value and accepts nothing above high.
		if on && level != "" {
			effort := level
			if effort == registry.ThinkingLevelMax {
				effort = registry.ThinkingLevelHigh
			}
			f.ReasoningEffort = &effort
		}
	case registry.ThinkingStyleAnthropic:
		if !on {
			f.Thinking = &thinkingObject{Type: "disabled"}
			break
		}
		f.Thinking = &thinkingObject{Type: "enabled", BudgetTokens: budget}
		f.dropTemperature = true
	case registry.ThinkingStyleGLM:
		if !on {
			f.Thinking = &thinkingObject{Type: "disabled"}
			break
		}
		f.Thinking = &thinkingObject{Type: "enabled"}
		if keep != nil {
			clear := !*keep
			f.Thinking.ClearThinking = &clear
		}
	}
	return f
}

// temperatureFor returns the temperature to send alongside f. The fields carry
// the drop decision themselves (see thinkingFields.dropTemperature), so there
// is no style parameter a caller could mismatch. Anthropic rejects extended
// thinking with any temperature but 1, so an enabled anthropic declaration
// sends none and the provider default (1) applies; the registry rejects a
// declared temperature other than 1 for such an agent at load. glm shares the
// thinking member but keeps its temperature.
func temperatureFor(temperature *float64, f thinkingFields) *float64 {
	if f.dropTemperature {
		return nil
	}
	return temperature
}

// SentTemperature returns the temperature inv's request body actually carries,
// which differs from inv.Temperature when the declared thinking drops it (see
// temperatureFor). Observers report this, not the declaration.
func SentTemperature(inv Invocation) *float64 {
	return temperatureFor(inv.Temperature, newThinkingFields(inv.Thinking, inv.ThinkingLevel, inv.ThinkingStyle, inv.PreserveThinking))
}
