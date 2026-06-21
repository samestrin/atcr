package debate

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
)

// chatTurn scripts one Chat response: a final message carrying content, or an
// error. Mirrors verify's test mock.
type chatTurn struct {
	content string
	err     error
}

// fakeChatCompleter implements fanout.ChatCompleter. Each Chat call pops the next
// scripted turn in order, so three seats sharing one completer consume turns[0..2]
// in seat order. Safe for concurrent use.
type fakeChatCompleter struct {
	mu    sync.Mutex
	turns []chatTurn
	idx   int
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
	var turn chatTurn
	if call < len(f.turns) {
		turn = f.turns[call]
	} else {
		turn = chatTurn{content: "default final answer"}
	}
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if turn.err != nil {
		return nil, turn.err
	}
	c := turn.content
	return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &c}, FinishReason: "stop"}, nil
}

// fakeDispatcher implements debate.Dispatcher with a fixed result.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (d *fakeDispatcher) Execute(_ context.Context, _ string, _ json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return tools.ToolResult{Content: "file contents", OriginalBytes: 13}, nil
}

// fcCast builds a three-seat Cast whose models support function calling, so the
// tool loop runs Chat (and the scripted turns are consumed in seat order).
func fcCast() Cast {
	mk := func(label, agent, model string) Caster {
		return Caster{
			Label:    label,
			Agent:    agent,
			Config:   registry.AgentConfig{Provider: "p", Model: model, SupportsFC: true},
			Provider: registry.Provider{APIKeyEnv: "K", BaseURL: "https://x"},
		}
	}
	return Cast{
		Proposer:   mk(LabelProposer, "alice", "model-a"),
		Challenger: mk(LabelChallenger, "bob", "model-b"),
		Judge:      mk(LabelJudge, "carol", "model-c"),
	}
}
