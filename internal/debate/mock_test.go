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
// error. Mirrors verify's test mock. meta, when set, scripts the single-shot
// CompleteWithMeta reply instead (a truncated, reasoning-salvaged completion),
// so a test can reproduce the degraded single-shot path a thinking-on
// supports_function_calling:false seat takes.
type chatTurn struct {
	content string
	err     error
	meta    *llmclient.Completion
}

// fakeChatCompleter implements fanout.ChatCompleter. Each Chat call pops the next
// scripted turn in order, so three seats sharing one completer consume turns[0..2]
// in seat order. Every Chat call's Invocation is recorded in call order, so a test
// can inspect what each seat actually sent. Complete (the single-shot non-FC path)
// records its Invocation the same way, so a test can also inspect what a seat that
// skipped the tool loop actually sent. Safe for concurrent use.
type fakeChatCompleter struct {
	mu    sync.Mutex
	turns []chatTurn
	idx   int
	invs  []llmclient.Invocation
}

// invocations returns a copy of the Invocation each Chat call received, in call
// order.
func (f *fakeChatCompleter) invocations() []llmclient.Invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llmclient.Invocation(nil), f.invs...)
}

func (f *fakeChatCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invs = append(f.invs, inv)
	if len(f.turns) > 0 {
		return f.turns[0].content, nil
	}
	return "", nil
}

// CompleteWithMeta serves the single-shot path the engine picks first when the
// completer is a MetaCompleter. It pops the same scripted turn the Chat path
// would (by idx), so turns stay in seat order across mixed single-shot and
// tool-loop seats. A turn with meta set returns that completion verbatim
// (including its Truncated marker); otherwise it mirrors Complete.
func (f *fakeChatCompleter) CompleteWithMeta(_ context.Context, inv llmclient.Invocation) (llmclient.Completion, error) {
	f.mu.Lock()
	f.invs = append(f.invs, inv)
	call := f.idx
	f.idx++
	var turn chatTurn
	if call < len(f.turns) {
		turn = f.turns[call]
	}
	f.mu.Unlock()
	if turn.err != nil {
		return llmclient.Completion{}, turn.err
	}
	if turn.meta != nil {
		return *turn.meta, nil
	}
	return llmclient.Completion{Content: turn.content}, nil
}

func (f *fakeChatCompleter) Chat(ctx context.Context, inv llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	f.mu.Lock()
	call := f.idx
	f.idx++
	f.invs = append(f.invs, inv)
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
