# LLM Integration & Tool Loop [IMPORTANT]

## Overview

Skeptics are invoked via the Epic 2.0 tool loop (`invokeToolLoop` at `internal/fanout/loop.go:81`). The loop is model-agnostic (any `ChatCompleter`) and tool-agnostic (any `toolDispatcher`). It drives the Chat → tool_calls → dispatch → repeat cycle with budget/hygiene halts and partial-success semantics.

For Epic 3.0, skeptics reuse the same loop but with a **per-finding scope**: the skeptic sees ONE finding (not the whole review) and tries to disprove it. The prompt framing is "try to prove this wrong" rather than "find problems".

> Source: [codebase-discovery.json:existing_patterns → Tool Loop (invokeToolLoop)]
> Source: [openai.md:Chat Completions API]

---

## Key Concepts

### Tool Loop (invokeToolLoop)

- Location: `internal/fanout/loop.go:81`
- Signature: `func (e *Engine) invokeToolLoop(ctx context.Context, a Agent, cc ChatCompleter, disp toolDispatcher) Result` — the prompt, turn cap, and byte budget come from `a.Invocation.Prompt`, `a.MaxTurns`, and `a.ToolBudgetBytes`.
- Behavior:
  1. Build the initial conversation from `a.Invocation.Prompt`.
  2. Call `cc.Chat` with the conversation and tool definitions.
  3. If the response contains `tool_calls`, dispatch each call via `disp.Execute` and append role `"tool"` results.
  4. Repeat until:
     - The model returns a final answer (no tool calls)
     - A budget trips (`max_turns`, `tool_budget_bytes`, `timeout_secs`)
     - Loop hygiene halts (repeated identical calls, consecutive malformed args)
  5. On a trip/halt, request one final no-tools answer so partial findings are preserved.
  6. Return a `fanout.Result` with status, content, and budget counters.

> Source: [codebase-discovery.json:existing_patterns → Tool Loop]
> Source: [codebase-discovery.json:reusable_components → Tool Loop]

### Skeptic Invocation

- Skeptics are agents with `role: skeptic`, `tools: true`, `supports_function_calling: true`.
- The skeptic receives a **per-finding prompt** constructed by `internal/verify/skeptic.go`.
- The prompt includes:
  - The finding (problem, fix, evidence)
  - The payload context (file paths, code snippets)
  - Verdict envelope spec (output format: `{"verdict": "...", "reasoning": "..."}`)
  - Instruction: "try to prove this finding wrong"
- The skeptic runs the tool loop, using tools to check the actual code (e.g., `read_file`, `grep`).
- Budget: per-finding (not per-review). Reuses the 2.0 loop budgets (`MaxTurns`, `ToolBudgetBytes`).

> Source: [original-requirements.md:Skeptic mechanics]
> Source: [plan.md:Technical Planning Notes]

### Per-Finding Prompt Construction

- Location: `internal/verify/skeptic.go`
- Input: `reconcile.JSONFinding` + payload context
- Output: prompt string
- Structure:
  1. **Role framing:** "You are an adversarial skeptic. Your job is to try to disprove the following finding."
  2. **Finding details:** problem, fix, evidence, severity, confidence
  3. **Code context:** file paths, line numbers, relevant code snippets (from payload)
  4. **Tool access:** "You have access to tools to read files and search the codebase. Use them to verify the evidence."
  5. **Output spec:** "Return a JSON object: `{\"verdict\": \"confirmed|refuted|unverifiable\", \"reasoning\": \"...\"}`. If you cannot determine the verdict, use `unverifiable` and explain why."

> Source: [original-requirements.md:Skeptic mechanics]
> Source: [plan.md:Technical Planning Notes]

### ChatCompleter Interface

- Location: `internal/fanout/engine.go:31`
- Interface: `ChatCompleter` with method `Chat(ctx context.Context, inv llmclient.Invocation, messages []llmclient.Message, toolDefs []llmclient.ToolDef) (*llmclient.ChatResponse, error)`
- Implementation: `*llmclient.Client` (same for reviewers and skeptics)
- The skeptic uses the same `ChatCompleter` as reviewers — no new interface needed.

> Source: [codebase-discovery.json:reusable_components → ChatCompleter Interface]

### Partial Success & Skeptic Failure Handling

- Partial-success semantics: if a budget trips mid-loop, the loop asks for one final no-tools answer so the accumulated evidence is preserved; the returned `fanout.Result` records the tripped budgets.
- For skeptics, a tripped budget → `verdict='unverifiable'` with notes describing the budget that tripped.
- A skeptic failure (timeout, provider error) → `verdict='unverifiable'` — the run **never fails** nor drops a finding due to a single skeptic error.

> Source: [original-requirements.md:Technical Constraints]
> Source: [codebase-discovery.json:architecture_notes]

### Budget Controls

- Per-skeptic budgets reuse the 2.0 loop budgets:
  - `MaxTurns`: max number of tool loop iterations (default: 10)
  - `ToolBudgetBytes`: max total size of tool call payloads (default: 1MB)
  - Time limit: per-finding timeout (default: 60s)
- Budgets are configurable via `verify.budgets` in the registry config.

> Source: [original-requirements.md:Cost controls]
> Source: [plan.md:Technical Planning Notes]

---

## Code Examples

### Tool Loop Invocation (Skeptic)

```go
// Simplified excerpt of internal/fanout/loop.go:invokeToolLoop (line 81).
// The real implementation also handles repeat-detection, malformed args,
// byte budgets, and transcript recording.
func (e *Engine) invokeToolLoop(ctx context.Context, a Agent, cc ChatCompleter, disp toolDispatcher) Result {
    maxTurns := a.MaxTurns
    if maxTurns <= 0 {
        maxTurns = defaultMaxTurns
    }
    prompt := a.Invocation.Prompt
    messages := []llmclient.Message{{Role: "user", Content: &prompt}}

    for turns := 0; ; turns++ {
        if err := ctx.Err(); err != nil {
            // Halt on cancel/timeout and return a classified partial result.
            return partialResult(a, err)
        }

        resp, err := cc.Chat(ctx, a.Invocation, messages, toolDefs)
        if err != nil {
            return classifiedResult(a, err)
        }
        messages = append(messages, resp.Message)

        // Final answer (no tool calls): the skeptic has finished checking.
        if len(resp.Message.ToolCalls) == 0 {
            return okResult(a, derefContent(resp.Message.Content))
        }

        // Budget trip: do not execute this turn's calls; answer them so the
        // conversation stays well-formed, then request a final no-tools answer.
        if turns >= maxTurns {
            return requestFinalAnswer(ctx, a, cc, messages, budgetMaxTurns)
        }

        // Dispatch each tool call and append the role:"tool" results.
        for _, tc := range resp.Message.ToolCalls {
            out, _ := disp.Execute(ctx, tc.Function.Name, llmclient.ToolCallArguments(tc))
            messages = append(messages, llmclient.Message{
                Role:       "tool",
                ToolCallID: tc.ID,
                Content:    &out.Content,
            })
        }
    }
}
```

> Source: [codebase-discovery.json:existing_patterns → Tool Loop]

### Skeptic Prompt Construction

```go
// From internal/verify/skeptic.go (new file)
func buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string {
    var b strings.Builder

    b.WriteString("You are an adversarial skeptic. Your job is to try to disprove the following finding.\n\n")

    b.WriteString("## Finding\n\n")
    b.WriteString(fmt.Sprintf("**Problem:** %s\n", finding.Problem))
    b.WriteString(fmt.Sprintf("**Fix:** %s\n", finding.Fix))
    b.WriteString(fmt.Sprintf("**Evidence:** %s\n", finding.Evidence))
    b.WriteString(fmt.Sprintf("**Severity:** %s\n", finding.Severity))
    b.WriteString(fmt.Sprintf("**Confidence:** %s\n\n", finding.Confidence))

    b.WriteString("## Code Context\n\n")
    for _, file := range entries {
        b.WriteString(fmt.Sprintf("### %s\n", file.Path))
        b.WriteString(fmt.Sprintf("```\n%s```\n\n", file.Body))
    }

    b.WriteString("## Instructions\n\n")
    b.WriteString("You have access to tools to read files and search the codebase. Use them to verify the evidence.\n\n")
    b.WriteString("Return a JSON object:\n")
    b.WriteString("```json\n")
    b.WriteString("{\"verdict\": \"confirmed|refuted|unverifiable\", \"reasoning\": \"...\"}\n")
    b.WriteString("```\n\n")
    b.WriteString("If you cannot determine the verdict, use `unverifiable` and explain why.\n")

    return b.String()
}
```

> Source: [original-requirements.md:Skeptic mechanics]

### Verdict Parsing

```go
// From internal/verify/verdict.go (new file)
func parseVerdict(response string) (*reconcile.Verification, error) {
    // Try to parse JSON
    var result struct {
        Verdict   string `json:"verdict"`
        Reasoning string `json:"reasoning"`
    }
    if err := json.Unmarshal([]byte(response), &result); err != nil {
        // Malformed output → unverifiable with raw text preserved
        return &reconcile.Verification{
            Verdict: "unverifiable",
            Notes:   fmt.Sprintf("malformed_output: %s", response),
        }, nil
    }

    // Validate verdict enum
    switch result.Verdict {
    case "confirmed", "refuted", "unverifiable":
        return &reconcile.Verification{
            Verdict: result.Verdict,
            Notes:   result.Reasoning,
        }, nil
    default:
        // Invalid verdict → unverifiable
        return &reconcile.Verification{
            Verdict: "unverifiable",
            Notes:   fmt.Sprintf("invalid_verdict: %s (raw: %s)", result.Verdict, response),
        }, nil
    }
}
```

> Source: [codebase-discovery.json:existing_patterns → Verification Struct]

---

## Quick Reference

| Concept | Location | Notes |
|---------|----------|-------|
| invokeToolLoop | `internal/fanout/loop.go:81` | Reuse for skeptic invocation |
| Engine struct | `internal/fanout/engine.go:132` | ChatCompleter factory, tool dispatcher |
| ChatCompleter | `internal/fanout/engine.go` | Interface reused for skeptics |
| skeptic.go | `internal/verify/skeptic.go` | New file: prompt construction |
| verdict.go | `internal/verify/verdict.go` | New file: verdict parsing |
| Agent fields | `internal/fanout/engine.go` | `MaxTurns`, `ToolBudgetBytes`, `TimeoutSecs` bound the loop |

---

## Related Documentation

- [Verification Pipeline Architecture](verification-pipeline.md) — core mechanics, confidence v2
- [CLI & MCP Integration](cli-mcp-integration.md) — `atcr verify` subcommand, `atcr_verify` MCP tool
- [Testing & Fixtures](testing-fixtures.md) — skeptic invocation tests, malformed output tests
