package verify

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/tools"
)

// chatTurn scripts one Chat response. A non-empty toolCalls makes the turn an
// assistant tool-call turn (content null); otherwise it is a final message
// carrying content. err makes the turn fail; delay blocks until the delay
// elapses or the context is cancelled (timeout simulation).
type chatTurn struct {
	toolCalls []llmclient.ToolCall
	content   string
	err       error
	delay     time.Duration
	truncated bool // final content turn hit finish_reason=length (Epic 19.5)
}

// fakeChatCompleter implements fanout.ChatCompleter (Complete + Chat). Each Chat
// call pops the next scripted turn; calls past the end return a default final
// message so a tripped loop's final-answer request always resolves. Complete
// returns the first turn's content (or empty if no turns) so the single-shot
// degrade path returns a real verdict — tests can assert the outcome, not just
// "unverifiable". Safe for concurrent use.
type fakeChatCompleter struct {
	mu        sync.Mutex
	turns     []chatTurn
	idx       int
	chatCalls int
}

func (f *fakeChatCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.turns) > 0 {
		return f.turns[0].content, nil
	}
	return "", nil
}

func (f *fakeChatCompleter) Chat(ctx context.Context, _ llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	f.mu.Lock()
	call := f.idx
	f.idx++
	f.chatCalls++
	var turn chatTurn
	if call < len(f.turns) {
		turn = f.turns[call]
	} else {
		// Past the script: a final no-tools answer so a budget-trip final request
		// always resolves. Default to a parseable unverifiable verdict.
		turn = chatTurn{content: `{"verdict":"unverifiable","reasoning":"default final answer"}`}
	}
	f.mu.Unlock()

	if turn.delay > 0 {
		select {
		case <-time.After(turn.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if turn.err != nil {
		return nil, turn.err
	}
	msg := llmclient.Message{Role: "assistant"}
	fr := "stop"
	if len(turn.toolCalls) > 0 {
		msg.ToolCalls = turn.toolCalls
		fr = "tool_calls"
	} else {
		c := turn.content
		msg.Content = &c
		if turn.truncated {
			fr = "length"
		}
	}
	return &llmclient.ChatResponse{Message: msg, FinishReason: fr, Truncated: turn.truncated}, nil
}

// finalChat returns a completer that answers with content on its first turn.
func finalChat(content string) *fakeChatCompleter {
	return &fakeChatCompleter{turns: []chatTurn{{content: content}}}
}

// toolCallTurn builds an assistant tool-call turn requesting one tool with empty
// (valid) arguments.
func toolCallTurn(name string) chatTurn {
	return chatTurn{toolCalls: []llmclient.ToolCall{{
		ID:       "call_1",
		Type:     "function",
		Function: llmclient.FunctionCall{Name: name, Arguments: json.RawMessage(`{}`)},
	}}}
}

// fakeDispatcher implements verify.Dispatcher with a fixed result/error and an
// execution counter so tests can assert the tool loop actually dispatched.
type fakeDispatcher struct {
	mu         sync.Mutex
	calls      int
	dispatched []string
	result     tools.ToolResult
	err        error
}

func (d *fakeDispatcher) Execute(_ context.Context, name string, _ json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	d.calls++
	d.dispatched = append(d.dispatched, name)
	d.mu.Unlock()
	return d.result, d.err
}

func (d *fakeDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// sequencedDispatcher returns results in order (the last result repeats once the
// sequence is exhausted), so a test can drive the skeptic's in-loop repro and the
// T3 determinism re-runs with distinct exit codes — e.g. a flaky reproduction.
type sequencedDispatcher struct {
	mu      sync.Mutex
	results []tools.ToolResult
	calls   int
}

func (d *sequencedDispatcher) Execute(_ context.Context, _ string, _ json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.calls
	if i >= len(d.results) {
		i = len(d.results) - 1
	}
	d.calls++
	return d.results[i], nil
}

func (d *fakeDispatcher) toolNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.dispatched))
	copy(out, d.dispatched)
	return out
}

// okDispatcher returns a small successful tool result.
func okDispatcher() *fakeDispatcher {
	return &fakeDispatcher{result: tools.ToolResult{Content: "file contents", OriginalBytes: 13}}
}
