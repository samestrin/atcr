package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/tools"
)

// defaultMaxTurns bounds a tool agent that reaches the loop with MaxTurns unset.
// Registry load applies the same default (DefaultMaxTurns) when tools=true; this
// is the engine-side safety net so the loop can never run unbounded even if an
// Agent is constructed directly (e.g. in a test) without the default applied.
const defaultMaxTurns = 10

// sigHistoryDepth is the number of past turns whose call signatures are kept for
// repeat detection. K=3 catches ABAB and ABCABC oscillation while staying O(K)
// memory.
const sigHistoryDepth = 3

// Loop-control messages. These are static (no per-call allocation) and are
// appended to the conversation to steer a thrashing or budget-exhausted model.
const (
	nudgeMessage = "You already called that tool with those exact arguments and have the result. " +
		"Do not repeat it — use the evidence you already have."
	finalAnswerMessage = "You have reached your exploration budget. Stop calling tools and write your " +
		"final review now, based only on the evidence you have already gathered."
	malformedArgsResult = "error: invalid JSON in tool arguments"
)

// wireToolDefs converts the harness tool definitions into the llmclient wire
// type once per loop. The harness owns the canonical definitions (internal/tools);
// the generic client never imports them, so the engine bridges the two. When exec
// is true (an execution-enabled agent, Epic 11.0), the run_tests/run_script
// definitions are appended so only that agent is told it may execute code; every
// other agent sees the read-only set unchanged.
func wireToolDefs(exec bool) []llmclient.ToolDef {
	defs := tools.Tools()
	if exec {
		defs = append(defs, tools.ExecutionTools()...)
	}
	out := make([]llmclient.ToolDef, len(defs))
	for i, d := range defs {
		out[i] = llmclient.ToolDef{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
	}
	return out
}

// toolLoop carries the mutable state of one agent's multi-turn run so the
// per-turn logic stays out of the driver loop. sigHistory holds the last
// sigHistoryDepth turns' tool-call signatures as a bounded ring (repeat detection
// across ABAB-style oscillation, not just back-to-back repeats); nudgedSigs holds
// signatures we nudged on the previous turn (a reappearance halts); malformedPrev
// records whether the previous turn produced a malformed call (a second
// consecutive one halts).
type toolLoop struct {
	agent    Agent
	cc       ChatCompleter
	disp     toolDispatcher
	maxTurns int
	toolDefs []llmclient.ToolDef
	messages []llmclient.Message
	res      *Result
	start    time.Time

	sigHistory    []map[string]bool
	nudgedSigs    map[string]bool
	malformedPrev bool

	// tr records the per-turn transcript (tool_calls, tool_results, final). nil
	// when transcript recording is disabled; every method on *tools.Transcript is
	// nil-safe, so the loop never guards the calls.
	tr *tools.Transcript
}

// invokeToolLoop drives a tool-enabled agent through the multi-turn exchange:
// send messages + tool definitions, execute any returned tool_calls via the
// dispatcher, append role:"tool" results, and repeat until the model returns a
// final message, a budget trips, loop hygiene halts, or the context is done. A
// trip/halt asks the model for a final answer (one unbudgeted no-tools Chat) so
// partial findings are never discarded (partial-success semantics).
func (e *Engine) invokeToolLoop(ctx context.Context, a Agent, cc ChatCompleter, disp toolDispatcher) Result {
	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	prompt := a.Invocation.Prompt
	var tr *tools.Transcript
	if e.transcript != nil {
		tr = e.transcript(a.Name)
	}
	l := &toolLoop{
		agent:      a,
		cc:         cc,
		disp:       disp,
		maxTurns:   maxTurns,
		toolDefs:   wireToolDefs(a.Exec),
		messages:   []llmclient.Message{{Role: "user", Content: &prompt}},
		res:        &Result{Agent: a.Name, PayloadMode: a.PayloadMode, Truncation: a.Truncation, MinSeverity: a.MinSeverity, MaxFindings: a.MaxFindings, Tools: true, ToolsRequested: true, Model: a.Invocation.Model},
		start:      time.Now(),
		nudgedSigs: map[string]bool{},
		tr:         tr,
	}
	return l.run(ctx)
}

func (l *toolLoop) run(ctx context.Context) Result {
	// Carry this agent's exec eligibility into every dispatch the loop makes. The
	// dispatcher's structural gate (Epic 11.1) admits run_tests/run_script only for
	// an exec-enabled agent and refuses them for every read-only agent, even when
	// the whole pool shares one exec-wired dispatcher. wireToolDefs(a.Exec) already
	// gates the OFFERING; this gates the DISPATCH so the boundary is structural,
	// not merely advisory.
	//
	// l.agent.Exec is fixed for the loop's lifetime: it is set once when the
	// toolLoop is constructed (invokeToolLoop) and never mutated thereafter, so
	// eligibility is derived once here — before the loop — rather than re-read on
	// each iteration. Every turn deliberately shares this one eligibility value;
	// this is an immutability invariant, not a per-iteration computation.
	ctx = tools.WithExecEligibility(ctx, l.agent.Exec)
	// Inject an operator-side refusal sink: when the dispatcher refuses an exec-gated
	// tool a read-only agent named, the refusal otherwise reaches only the model as a
	// tool result. The sink is wired here — fanout may import internal/log, the tools
	// package may not — following the WithExecEligibility context-key pattern. The
	// logger is snapshotted so the closure does not capture the reassigned ctx.
	refusalLog, refusalAgent := log.FromContext(ctx), l.agent.Name
	ctx = tools.WithRefusalLogger(ctx, func(tool string) {
		refusalLog.Warn("exec tool refused: execution eligibility not granted to this agent",
			"tool", tool, "agent", refusalAgent)
	})
	for {
		// Honor cancellation/deadline before each turn so a tripped clock halts
		// the loop with the partial results gathered so far rather than firing
		// another provider request.
		if err := ctx.Err(); err != nil {
			l.res.addTripped(budgetTimeout)
			return l.finalize(classifyStatus(err), err)
		}

		resp, err := l.cc.Chat(ctx, l.agent.Invocation, l.messages, l.toolDefs)
		if err != nil {
			// A Chat error ends the loop. A deadline/cancel records the timeout
			// budget; any other provider error is a plain failure. Partial
			// counters already accumulated in res are preserved either way. An
			// errored turn still carries the per-attempt telemetry for any HTTP
			// attempt that reached the wire (Epic 4.11), so count it before exiting —
			// a mid-flight timeout that did one round-trip must not undercount.
			if resp != nil {
				l.res.addCallRecords(resp.CallRecords)
			}
			status := classifyStatus(err)
			if status == StatusTimeout {
				l.res.addTripped(budgetTimeout)
			}
			return l.finalize(status, err)
		}
		l.res.Turns++
		l.res.addUsage(resp.Usage)
		l.res.addCallRecords(resp.CallRecords)
		l.messages = append(l.messages, resp.Message)

		// Final message (no tool_calls): the model finished within budget.
		if len(resp.Message.ToolCalls) == 0 {
			// A model that reached this loop was declared function-calling-capable
			// (supports_function_calling=true gated entry in invokeAgent). If it never
			// calls a tool on its FIRST turn, that declaration is likely wrong — warn
			// once as a hint (NOT a failure; the loop still returns the answer) so a
			// roster misconfiguration is visible (AC 04-01 Error Scenario 1).
			if l.res.Turns == 1 {
				fmt.Fprintf(os.Stderr, "atcr: warning: agent %s: model %s declared supports_function_calling=true but first response has no tool_calls — possible misconfiguration\n", l.agent.Name, l.agent.Invocation.Model)
			}
			l.res.Content = derefContent(resp.Message.Content)
			// finish_reason=length on the final content turn: the answer is cut off.
			// Surface it so the reviewer truncation-failover policy can react (Epic 19.5).
			l.res.ResponseTruncated = resp.Truncated
			l.tr.RecordFinal(l.res.Turns, l.res.Content)
			return l.finalize(StatusOK, nil)
		}

		// Record the requested tool_calls before deciding whether to execute them,
		// so the transcript is a faithful record even when the turn is skipped by a
		// budget trip below.
		l.tr.RecordToolCalls(l.res.Turns, toolCallRecords(resp.Message.ToolCalls))

		// Model wants tools but there is no budget for another round-trip to feed
		// the results back (Model A, sprint clarification 2026-06-13): do NOT
		// execute this turn's calls; trip max_turns and request a final answer.
		if l.res.Turns >= l.maxTurns {
			l.res.addTripped(budgetMaxTurns)
			// The assistant turn carries tool_calls we will not execute; answer
			// every one so the conversation stays well-formed (OpenAI-compatible
			// providers reject a final request with a dangling tool_call_id,
			// which would discard the partial findings).
			l.answerSkipped(l.res.Turns, resp.Message.ToolCalls, "skipped: turn budget reached; provide your final answer now")
			return l.requestFinalAnswer(ctx)
		}

		halt := l.dispatchTurn(ctx, l.res.Turns, resp.Message.ToolCalls)

		// Cancellation during tool execution halts with partial results and takes
		// precedence over the byte-budget check below (AC 02-02 Error 2).
		if cerr := ctx.Err(); cerr != nil {
			l.res.addTripped(budgetTimeout)
			return l.finalize(classifyStatus(cerr), cerr)
		}
		if halt {
			l.res.addTripped(budgetLoopHygiene)
			return l.requestFinalAnswer(ctx)
		}
		// End-of-turn byte-budget check: the current turn's results were delivered
		// in full; trip only after they are in hand (deferred trip, AC 02-02).
		if l.agent.ToolBudgetBytes > 0 && l.res.ToolBytes > l.agent.ToolBudgetBytes {
			l.res.addTripped(budgetToolBytes)
			return l.requestFinalAnswer(ctx)
		}
	}
}

// dispatchTurn processes one turn's tool calls: it skips/nudges identical
// repeats, rejects malformed arguments before they reach the dispatcher, and
// executes the rest, appending every outcome as a role:"tool" result. It returns
// true when loop hygiene requires halting (a repeat seen in sigHistory, or a
// second consecutive malformed turn). It updates sigHistory/nudgedSigs/malformedPrev
// for the next turn.
func (l *toolLoop) dispatchTurn(ctx context.Context, turn int, calls []llmclient.ToolCall) (halt bool) {
	curSigs := make(map[string]bool, len(calls))
	nextNudged := map[string]bool{}
	malformedThisTurn := false
	recs := make([]tools.ToolResultRecord, 0, len(calls))

	// Every tool_call in the assistant turn must be answered with a role:"tool"
	// result before the next request — providers reject a dangling tool_call_id.
	// Skipped calls (repeats) therefore still get an answer (the nudge), just not
	// a real execution; a separate user-role nudge is NOT used because it would
	// illegally interleave between the assistant tool_calls and its tool results.
	for _, tc := range calls {
		sig := toolSig(tc)
		curSigs[sig] = true

		// A signature we nudged last turn reappearing is a second repeat: answer
		// it (keep the wire well-formed) and halt — the model is thrashing.
		if l.nudgedSigs[sig] {
			l.appendToolResult(tc.ID, nudgeMessage)
			recs = append(recs, textResult(tc, nudgeMessage))
			halt = true
			continue
		}
		// First identical repeat (same call as the immediately previous turn): do
		// not re-execute; answer with the nudge and flag the signature so a
		// reappearance next turn halts.
		if sigInHistory(l.sigHistory, sig) {
			l.appendToolResult(tc.ID, nudgeMessage)
			recs = append(recs, textResult(tc, nudgeMessage))
			nextNudged[sig] = true
			continue
		}

		// Malformed arguments never reach the dispatcher; the model gets an error
		// result and one chance to retry (a second consecutive malformed halts).
		args := llmclient.ToolCallArguments(tc)
		if len(args) > 0 && !json.Valid(args) {
			malformedThisTurn = true
			l.appendToolResult(tc.ID, malformedArgsResult)
			recs = append(recs, textResult(tc, malformedArgsResult))
			continue
		}

		l.res.ToolCalls++
		out, terr := l.disp.Execute(ctx, tc.Function.Name, args)
		if terr != nil {
			// Tool failures (unknown tool, jail violation, file error, recovered
			// panic) are never fatal: relay them to the model as the result.
			msg := "error: " + terr.Error()
			l.appendToolResult(tc.ID, msg)
			recs = append(recs, textResult(tc, msg))
			continue
		}
		l.res.ToolBytes += int64(len(out.Content))
		l.appendToolResult(tc.ID, out.Content)
		recs = append(recs, tools.ToolResultRecord{
			ToolCallID: tc.ID, Name: tc.Function.Name, Content: out.Content,
			Truncated: out.Truncated, OriginalBytes: out.OriginalBytes,
		})
	}

	l.tr.RecordToolResults(turn, recs)
	if malformedThisTurn && l.malformedPrev {
		halt = true
	}
	l.sigHistory = appendSigRing(l.sigHistory, curSigs, sigHistoryDepth)
	l.nudgedSigs = nextNudged
	l.malformedPrev = malformedThisTurn
	return halt
}

// requestFinalAnswer asks the model for its review after a trip/halt with a
// no-tools Chat (unbudgeted — it is not counted as a turn). A failure here keeps
// whatever was gathered: a timeout records the timeout budget, any other error
// fails the agent, but the accumulated counters survive on res.
func (l *toolLoop) requestFinalAnswer(ctx context.Context) Result {
	// If the deadline already passed between the trip check and here, skip the
	// doomed provider round-trip and finalize with the partial result.
	if err := ctx.Err(); err != nil {
		l.res.addTripped(budgetTimeout)
		return l.finalize(classifyStatus(err), err)
	}
	l.appendUser(finalAnswerMessage)
	resp, err := l.cc.Chat(ctx, l.agent.Invocation, l.messages, nil)
	if err != nil {
		// Count the final-answer call's wire attempt(s) even on error (Epic 4.11),
		// mirroring the per-turn path above.
		if resp != nil {
			l.res.addCallRecords(resp.CallRecords)
		}
		status := classifyStatus(err)
		if status == StatusTimeout {
			l.res.addTripped(budgetTimeout)
		}
		return l.finalize(status, err)
	}
	// The final-answer call is unbudgeted as a turn, but its tokens and HTTP
	// attempts are real and must count toward the agent's usage and call telemetry.
	l.res.addUsage(resp.Usage)
	l.res.addCallRecords(resp.CallRecords)
	l.res.Content = derefContent(resp.Message.Content)
	// A truncated forced final-answer is still cut off; surface it (Epic 19.5).
	l.res.ResponseTruncated = resp.Truncated
	l.tr.RecordFinal(l.res.Turns, l.res.Content)
	return l.finalize(StatusOK, nil)
}

func (l *toolLoop) finalize(status string, err error) Result {
	// Close the transcript on every exit path so its buffer is flushed and the
	// file handle released (nil-safe when recording is disabled).
	_ = l.tr.Close()
	l.res.Status = status
	l.res.Err = err
	l.res.DurationMS = time.Since(l.start).Milliseconds()
	return *l.res
}

func (l *toolLoop) appendUser(content string) {
	c := content
	l.messages = append(l.messages, llmclient.Message{Role: "user", Content: &c})
}

func (l *toolLoop) appendToolResult(callID, content string) {
	c := content
	l.messages = append(l.messages, llmclient.Message{Role: "tool", ToolCallID: callID, Content: &c})
}

// answerSkipped appends a role:"tool" result for every call in an assistant turn
// whose tool_calls are intentionally not executed (the max_turns trip). It keeps
// the conversation well-formed so the unbudgeted final-answer request is not
// rejected for a dangling tool_call_id, and records the same answers in the
// transcript so the operator's view matches the model's.
func (l *toolLoop) answerSkipped(turn int, calls []llmclient.ToolCall, note string) {
	recs := make([]tools.ToolResultRecord, 0, len(calls))
	for _, tc := range calls {
		l.appendToolResult(tc.ID, note)
		recs = append(recs, textResult(tc, note))
	}
	l.tr.RecordToolResults(turn, recs)
}

// toolCallRecords converts the wire tool calls into transcript records, copying
// the raw arguments so the transcript shows exactly what the model asked. A
// call's malformed (invalid-JSON) arguments are preserved as a JSON string
// rather than left as invalid RawMessage: an invalid RawMessage would fail the
// whole event's json.Marshal and drop EVERY call in the turn (orphaning the
// tool_result lines), so it is wrapped so the per-call record always serializes.
func toolCallRecords(calls []llmclient.ToolCall) []tools.ToolCallRecord {
	out := make([]tools.ToolCallRecord, len(calls))
	for i, tc := range calls {
		args := llmclient.ToolCallArguments(tc)
		if len(args) > 0 && !json.Valid(args) {
			if enc, err := json.Marshal(string(args)); err == nil {
				args = enc
			} else {
				args = nil // unreachable for a Go string, but never emit invalid JSON
			}
		}
		out[i] = tools.ToolCallRecord{ID: tc.ID, Name: tc.Function.Name, Arguments: args}
	}
	return out
}

// textResult builds a non-truncated transcript result record for a synthetic
// (nudge, error, malformed, skipped) tool answer whose content is plain text.
func textResult(tc llmclient.ToolCall, content string) tools.ToolResultRecord {
	return tools.ToolResultRecord{ToolCallID: tc.ID, Name: tc.Function.Name, Content: content, OriginalBytes: len(content)}
}

// canonicalizeArgs returns a deterministic JSON representation of args, or nil
// if args are not valid JSON. It recursively sorts object keys so semantically
// identical structures produce identical bytes regardless of key order or
// insignificant whitespace.
func canonicalizeArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return args
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return nil
	}
	canon := canonicalizeValue(v)
	out, err := json.Marshal(canon)
	if err != nil {
		return nil
	}
	return out
}

func canonicalizeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := make(map[string]any, len(x))
		for _, k := range keys {
			m[k] = canonicalizeValue(x[k])
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = canonicalizeValue(e)
		}
		return out
	default:
		return x
	}
}

// toolSig is the dedup key for a tool call: name plus normalized, canonicalized
// arguments. A NUL separator avoids collisions between the name and the
// argument JSON. Invalid JSON arguments fall back to the raw bytes so the
// malformed-args path still sees them.
func toolSig(tc llmclient.ToolCall) string {
	args := llmclient.ToolCallArguments(tc)
	if canon := canonicalizeArgs(args); canon != nil {
		args = canon
	}
	return tc.Function.Name + "\x00" + string(args)
}

// sigInHistory reports whether sig appeared in any of the recorded turns.
func sigInHistory(history []map[string]bool, sig string) bool {
	for _, m := range history {
		if m[sig] {
			return true
		}
	}
	return false
}

// appendSigRing appends sigs to history and trims to the most recent k entries.
func appendSigRing(history []map[string]bool, sigs map[string]bool, k int) []map[string]bool {
	history = append(history, sigs)
	if len(history) > k {
		history = history[len(history)-k:]
	}
	return history
}

func derefContent(c *string) string {
	if c == nil {
		return ""
	}
	return *c
}
