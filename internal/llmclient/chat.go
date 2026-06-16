package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// ToolDef is a function-calling tool definition. It marshals to the OpenAI tool
// envelope ({"type":"function","function":{name,description,parameters}}), the
// lowest-common-denominator wire format across OpenAI-compatible providers. The
// engine converts its harness tool definitions into this wire type so the
// generic client stays decoupled from the tool harness (spike 1.1 #5).
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// MarshalJSON emits the OpenAI tool envelope.
func (d ToolDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"parameters":  d.Parameters,
		},
	})
}

// FunctionCall is the function portion of a tool_call. Arguments is kept as raw
// JSON because providers disagree on its encoding: OpenAI/litellm send a
// JSON-encoded string ("{\"path\":\"x\"}") while some local providers send a raw
// JSON object ({"path":"x"}). ToolCallArguments normalizes both (spike 1.1 #2).
type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// MarshalJSON emits arguments as a JSON-encoded string so the echoed assistant
// turn is wire-canonical for strict OpenAI-compatible validators regardless of
// whether the inbound provider used string-encoded or raw-object form.
func (f FunctionCall) MarshalJSON() ([]byte, error) {
	args := f.Arguments
	if len(args) > 0 && args[0] != '"' {
		// Raw JSON object: encode as a JSON string to match the OpenAI wire form.
		s, err := json.Marshal(string(args))
		if err != nil {
			return nil, fmt.Errorf("encoding function arguments: %w", err)
		}
		args = s
	}
	type alias struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	return json.Marshal(alias{Name: f.Name, Arguments: args})
}

// ToolCall is one model-requested tool invocation.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// Message is one chat-completions message. Content is a pointer so the assistant
// tool-call turn can carry content:null (which OpenAI requires) distinctly from
// an empty string; user/tool messages set it to a real string. ToolCalls is
// present on an assistant turn requesting tools; ToolCallID ties a role:"tool"
// result back to the call that produced it.
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatResponse is the engine-facing result of one Chat turn: the assistant
// message (which may carry tool_calls) and the provider's finish_reason.
// Truncated is true when the provider reported finish_reason "length" (token
// budget exhausted); the content or tool_call arguments may be partial.
type ChatResponse struct {
	Message      Message
	FinishReason string
	Truncated    bool
	// Usage carries the provider-reported token counts for THIS turn only. Zero
	// when the provider omits the `usage` block (additive field; existing
	// callers that ignore it are unaffected).
	//
	// CONTRACT (per-turn-incremental): each Chat() decodes exactly one turn's
	// usage and never accumulates across turns — summing is the caller's job
	// (the fanout loop adds resp.Usage after every turn and the final-answer
	// call). This is correct ONLY for providers that report per-turn-incremental
	// usage. A gateway that reports CUMULATIVE usage on every turn (some
	// Anthropic-via-gateway shims) would be N-counted across a multi-turn loop.
	// If such a gateway comes into scope, detect monotonic-increasing usage and
	// diff successive turns instead of summing; until then the assumption is a
	// documented hard contract, pinned by TestChat_UsageIsPerTurnNotCumulative.
	Usage UsageData
}

// chatToolRequest is the multi-turn request body. Tools (and tool_choice) are
// omitted entirely when no tools are supplied, so a degraded/final-answer call
// is wire-identical to a plain completion.
type chatToolRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
}

// chatToolResponse decodes the wire response for a tool-capable turn.
type chatToolResponse struct {
	Choices []struct {
		FinishReason string  `json:"finish_reason"`
		Message      Message `json:"message"`
	} `json:"choices"`
	Usage UsageData `json:"usage"`
}

// Chat performs one multi-turn chat-completions exchange: it serializes the
// conversation (plus tool definitions when non-empty), sends it through the
// shared retry path, and returns the assistant message and finish reason. The
// caller (the fanout agent loop) owns turn/budget orchestration; Chat is a
// single round-trip. *Client satisfies the fanout ChatCompleter interface via
// this method, so a tool-enabled agent runs the loop while a client lacking it
// (a test fake, say) degrades to single-shot.
func (c *Client) Chat(ctx context.Context, inv Invocation, messages []Message, toolDefs []ToolDef) (*ChatResponse, error) {
	key, err := resolveKey(inv)
	if err != nil {
		return nil, err
	}
	req := chatToolRequest{
		Model:       inv.Model,
		Messages:    messages,
		Temperature: inv.Temperature,
		MaxTokens:   inv.MaxTokens,
	}
	if len(toolDefs) > 0 {
		req.Tools = toolDefs
		req.ToolChoice = "auto"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	raw, err := c.send(ctx, resolveEndpoint(inv.BaseURL), key, body)
	if err != nil {
		return nil, err
	}
	var parsed chatToolResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		// KNOWN LIMITATION (accepted): a provider may return an error-shaped 200
		// with no choices yet still bill for the call and report `usage`. This
		// early return discards parsed.Usage, so cost is understated on
		// billed-but-empty turns. Capturing it would require returning a non-nil
		// ChatResponse alongside this error and teaching the fanout loop to read
		// usage off an errored turn — a cross-package contract change in
		// internal/fanout. Billed-but-empty turns are rare, so the understatement
		// is accepted rather than complicating the error contract.
		return nil, fmt.Errorf("failed to parse response: no choices returned")
	}
	ch := parsed.Choices[0]
	// Guard against truncated or filtered completions that leave an empty turn:
	// a non-standard finish_reason with no content and no tool_calls must not be
	// silently returned as a successful empty review.
	if ch.FinishReason != "stop" && ch.FinishReason != "tool_calls" && ch.FinishReason != "" {
		if (ch.Message.Content == nil || *ch.Message.Content == "") && len(ch.Message.ToolCalls) == 0 {
			return nil, fmt.Errorf("provider truncated response (finish_reason=%s): empty content with no tool_calls", ch.FinishReason)
		}
	}
	resp := &ChatResponse{Message: ch.Message, FinishReason: ch.FinishReason, Usage: parsed.Usage}
	if ch.FinishReason == "length" {
		resp.Truncated = true
	}
	return resp, nil
}

// ToolCallArguments normalizes a tool call's arguments to a raw JSON value,
// tolerating both the OpenAI string-encoded form and the raw-object form some
// local providers emit. A malformed encoding is returned as-is so the caller's
// validity check (json.Valid) surfaces it as a malformed-arguments tool error
// rather than silently dispatching garbage.
func ToolCallArguments(tc ToolCall) json.RawMessage {
	raw := bytes.TrimSpace(tc.Function.Arguments)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return json.RawMessage(s)
		}
	}
	return raw
}
