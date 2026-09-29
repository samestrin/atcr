package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
//
// The reasoning members carry an assistant turn's own reasoning back into
// tool-loop history, because providers expect it on the next turn. Each holds
// the provider's JSON value as received, under the key it arrived in, and is
// set only by Chat on a reply: user and tool messages never carry one, and
// unset members add nothing to the body. On marshal, encoding/json compacts the
// value and HTML-escapes <, >, and &, so only the bytes can differ, never the
// value.
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`

	ReasoningContent json.RawMessage `json:"reasoning_content,omitempty"`
	Reasoning        json.RawMessage `json:"reasoning,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
	ThinkingBlocks   json.RawMessage `json:"thinking_blocks,omitempty"`
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

	// CallRecords is the per-attempt telemetry for THIS turn's dispatch (one
	// record per HTTP attempt, retries included). Like Usage, it is per-turn and
	// the fanout loop accumulates it across turns. Surfaced only on the success
	// path: a turn that errors returns a nil *ChatResponse, so its records are
	// dropped — the same accepted limitation that already applies to Usage on an
	// errored turn (see the no-choices note below).
	CallRecords []CallRecord

	// Reasoning is this turn's reasoning_content (or reasoning) as text, for
	// readers that report it. What goes back to the model is Message's own
	// reasoning members, not this field.
	Reasoning string
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
	// ResponseFormat is sent on every turn a declared agent makes, tool turns
	// and the forced final alike: a turn that ends on its own is only known to be
	// final from its response, so the field must ride every tool turn; the
	// forced-final no-tools turn (loop.requestFinalAnswer) carries it too.
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	// thinkingFields rides every turn for the same reason as ResponseFormat.
	thinkingFields
}

// chatToolResponse decodes the wire response for a tool-capable turn.
type chatToolResponse struct {
	Choices []struct {
		FinishReason string          `json:"finish_reason"`
		Message      responseMessage `json:"message"`
	} `json:"choices"`
	Usage UsageData `json:"usage"`
}

// responseMessage is a decoded assistant turn: the Message the loop keeps as
// history, plus its reasoning members as received. These fields shadow
// Message's own reasoning members, so decode never fills those: Chat copies
// each one across only after reasoningMember checks its shape.
type responseMessage struct {
	Message
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
	ThinkingBlocks   json.RawMessage `json:"thinking_blocks"`
}

// history is the reply as the loop re-sends it: the Message with each
// reasoning member that has its key's shape. Reasoning and ReasoningContent
// name the same chain of thought under two provider keys (see Client's
// reasoning fallback), so when the two string members are byte-equal only
// reasoning_content is kept — a provider that fills both would otherwise have
// the same reasoning re-sent twice on every later turn. Different values stay
// independent.
func (m responseMessage) history() Message {
	msg := m.Message
	msg.ReasoningContent = reasoningMember(m.ReasoningContent, false)
	msg.Reasoning = reasoningMember(m.Reasoning, false)
	if msg.ReasoningContent != nil && bytes.Equal(msg.Reasoning, msg.ReasoningContent) {
		msg.Reasoning = nil
	}
	msg.ReasoningDetails = reasoningMember(m.ReasoningDetails, true)
	msg.ThinkingBlocks = reasoningMember(m.ThinkingBlocks, true)
	return msg
}

// reasoningMember is a reasoning member as received, kept only when it has its
// key's shape: a string holding a non-whitespace character, or (structured) a
// non-empty array or object. Anything else — null, "", whitespace-only, a
// wrong type, a zero-length container — is absent, so it never reaches a
// request body and never fails the decode. An empty container is dropped as
// absent rather than replayed: an empty thinking_blocks array on a tool-use
// turn is neither the blocks the provider signed nor a meaningful replay, and
// Anthropic rejects a continuation turn whose thinking blocks are missing or
// altered.
func reasoningMember(raw json.RawMessage, structured bool) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	if structured {
		if (raw[0] == '[' || raw[0] == '{') && containerHasElement(raw) {
			return raw
		}
		return nil
	}
	if !stringMemberHasContent(raw) {
		return nil
	}
	return raw
}

// containerHasElement reports whether a JSON array or object holds any
// non-whitespace byte between its delimiters, without decoding: "[]", "{}",
// and whitespace-padded empties like "[ ]" are empty, so a multi-megabyte
// container is scanned, not copied, on a path that only needs an emptiness
// verdict. An object like {"a":null} has non-whitespace bytes and counts as
// populated — emptiness here means no members, not no values.
func containerHasElement(raw json.RawMessage) bool {
	if len(raw) < 2 {
		return false
	}
	for _, b := range raw[1 : len(raw)-1] {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return true
		}
	}
	return false
}

// stringMemberHasContent reports whether raw is a JSON string holding a
// non-whitespace character, without materializing the decoded string: a
// multi-megabyte reasoning member is measured, not copied, on a path that only
// needs an emptiness verdict. Escape-free strings are scanned directly (with no
// backslash the content bytes are the raw bytes between the quotes); an escape
// could stand for whitespace ("\t") or content ("\\"), so those fall back to a
// full decode for the exact answer.
func stringMemberHasContent(raw json.RawMessage) bool {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return false
	}
	content := raw[1 : len(raw)-1]
	if bytes.IndexByte(content, '\\') < 0 {
		for _, b := range content {
			if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
				return true
			}
		}
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != ""
}

// memberText is a string reasoning member's text, "" when absent.
func memberText(raw json.RawMessage) reasoningText {
	var s string
	_ = json.Unmarshal(raw, &s)
	return reasoningText(s)
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
	thinking := newThinkingFields(inv.Thinking, inv.ThinkingLevel, inv.ThinkingStyle, inv.PreserveThinking)
	req := chatToolRequest{
		Model:          inv.Model,
		Messages:       messages,
		Temperature:    temperatureFor(inv.Temperature, thinking),
		MaxTokens:      inv.MaxTokens,
		ResponseFormat: newResponseFormat(inv.ResponseFormat),
		thinkingFields: thinking,
	}
	if len(toolDefs) > 0 {
		req.Tools = toolDefs
		req.ToolChoice = "auto"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	raw, records, err := c.send(ctx, resolveEndpoint(inv.BaseURL), key, body)
	if err != nil {
		// Surface the dispatch's per-attempt telemetry even on error (Epic 4.11):
		// a mid-flight timeout reached the wire, and the fanout loop must still
		// count that attempt — otherwise the AC1 undercount the epic fixed for the
		// single-shot path would persist on the tool-loop path. Only CallRecords is
		// carried here; Usage stays dropped on an errored turn (see the no-choices
		// note below), since token accounting is out of scope for this change.
		return &ChatResponse{CallRecords: records}, err
	}
	var parsed chatToolResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return &ChatResponse{CallRecords: records}, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		// KNOWN LIMITATION (accepted): a provider may return an error-shaped 200
		// with no choices yet still bill for the call and report `usage`. This
		// early return discards parsed.Usage, so cost is understated on
		// billed-but-empty turns. Capturing it would require returning a non-nil
		// ChatResponse alongside this error and teaching the fanout loop to read
		// usage off an errored turn — a cross-package contract change in
		// internal/fanout. Billed-but-empty turns are rare, so the understatement
		// is accepted rather than complicating the error contract. (CallRecords IS
		// surfaced — counting the wire attempt is the point of Epic 4.11.)
		return &ChatResponse{CallRecords: records}, fmt.Errorf("failed to parse response: no choices returned")
	}
	ch := parsed.Choices[0]
	// Guard against truncated or filtered completions that leave an empty turn:
	// a non-standard finish_reason with no content and no tool_calls must not be
	// silently returned as a successful empty review.
	if ch.FinishReason != "stop" && ch.FinishReason != "tool_calls" && ch.FinishReason != "" {
		if (ch.Message.Content == nil || *ch.Message.Content == "") && len(ch.Message.ToolCalls) == 0 {
			return &ChatResponse{CallRecords: records}, fmt.Errorf("provider truncated response (finish_reason=%s): empty content with no tool_calls", ch.FinishReason)
		}
	}
	msg := ch.Message.history()
	if ch.FinishReason == "length" {
		// A length-truncated turn may carry a cut-off structured reasoning value
		// (for Anthropic, a thinking block with no signature). Replaying it would
		// send a value that is neither the blocks the provider signed nor absent
		// — exactly what Anthropic rejects on a continuation turn — so the
		// structured members are cleared before the turn enters history. String
		// reasoning members are kept: text truncation degrades gracefully.
		msg.ThinkingBlocks = nil
		msg.ReasoningDetails = nil
	}
	resp := &ChatResponse{Message: msg, FinishReason: ch.FinishReason, Usage: parsed.Usage, CallRecords: records, Reasoning: reasoningOf(memberText(msg.ReasoningContent), memberText(msg.Reasoning))}
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
