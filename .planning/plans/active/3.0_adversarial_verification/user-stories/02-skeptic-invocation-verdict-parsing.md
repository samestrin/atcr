# User Story 2: Skeptic Invocation & Verdict Parsing

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** verification pipeline consumer (downstream stories: confidence v2, gate integration, report rendering)
**I want** each selected skeptic to be invoked against a single finding via the Epic 2.0 tool loop, and the skeptic's raw response to be parsed into a structured `Verification` envelope with a valid verdict (`confirmed` | `refuted` | `unverifiable`)
**So that** the pipeline produces a per-finding verdict that downstream stages can consume for confidence recomputation, gate counting, and report rendering — without ever dropping a finding due to malformed output or skeptic failure

## Story Context

- **Background:** Story 1 delivers the selection API (`SelectEligibleSkeptics`) which returns `[]AgentConfig` candidates per finding. This story takes those candidates and drives them: construct a per-finding prompt (finding details + code context + verdict envelope spec), invoke the skeptic through `invokeToolLoop` at `internal/fanout/loop.go:81`, and parse the response into the reserved `Verification` struct at `internal/reconcile/emit.go:36`. The tool loop already handles Chat → tool_calls → dispatch → repeat with budget/hygiene halts and partial-success semantics; skeptics reuse it unchanged. The new work is prompt construction (`internal/verify/skeptic.go`) and verdict parsing (`internal/verify/verdict.go`).
- **Assumptions:**
  - `SelectEligibleSkeptics` (Story 1) returns at most `n` eligible skeptics per finding; this story consumes that slice.
  - The `ChatCompleter` interface and `invokeToolLoop` are available and unchanged from Epic 2.0.
  - The `Verification` struct (`Verdict`, `Skeptic`, `Notes`) is reserved and ready to populate.
  - Skeptic agents in the registry have `tools: true` and `supports_function_calling: true`.
- **Constraints:**
  - A skeptic failure (provider error, timeout, malformed output) must **never** cause a finding to be dropped. The verdict becomes `unverifiable` with an explanatory `Notes` field.
  - Verdict parsing must handle: valid JSON with valid verdict, valid JSON with invalid verdict, malformed JSON, empty response, response with extra fields.
  - The per-finding prompt must be deterministic (same input → same prompt) for reproducibility and testability.
  - All new code must be unit-tested with table-driven tests matching existing patterns.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (Skeptic Selection & Role Plumbing) — needs `SelectEligibleSkeptics` to provide candidates |

## Success Criteria (SMART Format)

- **Specific:** (1) `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string` constructs a deterministic per-finding prompt with role framing, finding details (problem, fix, evidence, severity, confidence), code context from payload file entries, tool-access instructions, and the JSON verdict envelope spec (`{"verdict": "...", "reasoning": "..."}`). (2) `invokeSkeptic(ctx context.Context, skeptic registry.AgentConfig, prompt string, cc fanout.ChatCompleter, disp fanout.ToolDispatcher) (*reconcile.Verification, error)` drives the skeptic through `invokeToolLoop` and returns a populated `Verification`. (3) `parseVerdict(response string) (*reconcile.Verification, error)` extracts verdict + reasoning from the raw response, falling back to `unverifiable` for malformed output or invalid verdict enums.
- **Measurable:** (1) `go test ./internal/verify/...` passes with >= 95% coverage on `skeptic.go` and `verdict.go`. (2) Verdict parsing tests cover all 7 cases from testing-fixtures.md: confirmed, refuted, unverifiable, malformed JSON, invalid verdict enum, empty response, extra fields. (3) Skeptic invocation tests cover: single skeptic confirms, single skeptic refutes, budget tripped → unverifiable, provider error → unverifiable, malformed output → unverifiable. (4) `go vet` and existing CI checks remain clean.
- **Achievable:** The tool loop (`invokeToolLoop`) and `ChatCompleter` interface are reused unchanged. Prompt construction is string building; verdict parsing is JSON unmarshal + enum validation. The `Verification` struct is already reserved. This is integration and parsing work, not new infrastructure.
- **Relevant:** This is the core execution engine of Epic 3.0. Without skeptic invocation and verdict parsing, there is no verification stage — no verdicts to feed confidence v2, no refuted findings to demote, no verified findings to trust. Every downstream story depends on this producing `*Verification` values per finding.
- **Time-bound:** Expected to complete within weeks 1–2 of the 3–4 week epic (immediately after Story 1).

## Acceptance Criteria Overview

1. `buildSkepticPrompt` produces a deterministic prompt containing: role framing ("adversarial skeptic, try to disprove"), finding details (problem, fix, evidence, severity, confidence), code context from `[]payload.FileEntry`, tool-access instructions, and the JSON verdict envelope spec.
2. `invokeSkeptic` drives the selected skeptic through `invokeToolLoop` with the constructed prompt and returns a `*reconcile.Verification`. On provider error or timeout, returns `verdict="unverifiable"` with an explanatory `Notes` — does not propagate the error.
3. `parseVerdict` handles all 7 test cases: valid JSON with `confirmed`/`refuted`/`unverifiable` verdicts → populated `Verification`; malformed JSON → `unverifiable` with raw text in `Notes`; invalid verdict enum → `unverifiable` with the invalid value preserved; empty response → `unverifiable`; extra fields → ignored.
4. A skeptic that produces no parseable verdict (timeout, provider error, empty response, malformed output) results in `verdict="unverifiable"` — the finding is never dropped nor the run aborted.
5. Per-finding budgets are respected: `MaxTurns`, `ToolBudgetBytes`, and per-finding timeout are forwarded to the tool loop. A tripped budget produces `verdict="unverifiable"` with `trippedBudgets` recorded.
6. Table-driven unit tests in `internal/verify/verdict_test.go` and `internal/verify/verify_test.go` cover all cases above with >= 95% coverage on `skeptic.go` and `verdict.go`.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`_

## Technical Considerations

- **Implementation Notes:**
  - **Prompt construction (`internal/verify/skeptic.go`):** `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string` builds the prompt using `strings.Builder`. Structure: (1) role framing — "You are an adversarial skeptic. Your job is to try to disprove the following finding."; (2) finding details as markdown (problem, fix, evidence, severity, confidence); (3) code context from payload file entries (file path + body in fenced code blocks); (4) tool-access instructions — "You have access to tools to read files and search the codebase. Use them to verify the evidence."; (5) output spec — the JSON envelope `{"verdict": "confirmed|refuted|unverifiable", "reasoning": "..."}` with a note to use `unverifiable` if the verdict cannot be determined. The function is pure (no side effects) and deterministic.
  - **Skeptic invocation (`internal/verify/invoke.go`):** `invokeSkeptic(ctx context.Context, skeptic registry.AgentConfig, prompt string, cc fanout.ChatCompleter, disp fanout.ToolDispatcher) (*reconcile.Verification, error)` constructs an `Agent` from the `AgentConfig` (prompt, max turns, tool budget bytes, timeout), then calls `engine.invokeToolLoop(ctx, agent, cc, disp)`. The returned `fanout.Result` is converted: if `Result.Status == OK`, pass `Result.Content` to `parseVerdict`; if `Result.Status` indicates a budget trip or error, return `&reconcile.Verification{Verdict: "unverifiable", Notes: <explanation>, Skeptic: skeptic.Name}`. The function never returns an error to the caller — all failures are captured in the `Verification` envelope.
  - **Verdict parsing (`internal/verify/verdict.go`):** `parseVerdict(response string) (*reconcile.Verification, error)` first attempts `json.Unmarshal` into `struct{ Verdict string; Reasoning string }`. On parse failure → `unverifiable` with `Notes: "malformed_output: " + response`. On parse success, validates the verdict enum against `{"confirmed", "refuted", "unverifiable"}`. Invalid enum → `unverifiable` with `Notes: "invalid_verdict: " + verdict + " (raw: " + response + ")"`. Valid enum → `&reconcile.Verification{Verdict: verdict, Notes: reasoning}`. Extra JSON fields are silently ignored (default `json.Unmarshal` behavior). Empty response → `unverifiable` with `Notes: "empty_response"`.
  - **`invokeSkeptic` never propagates errors:** This is a hard constraint. The pipeline must complete verification for all findings even if individual skeptics fail. The `error` return on `invokeSkeptic` is reserved for programming errors (nil context, nil ChatCompleter) — runtime failures (provider error, timeout, malformed output) are captured in the `Verification` envelope.
  - **Transcript recording:** The tool loop already records transcripts to `verify/raw/<skeptic>/transcript.jsonl` (same format as Epic 2.0 reviewer transcripts). This is handled by the loop infrastructure — no new transcript code needed in this story.
- **Integration Points:**
  - `internal/fanout/loop.go` — `invokeToolLoop` (line 81): the loop driver reused for skeptic invocation.
  - `internal/fanout/engine.go` — `ChatCompleter` interface (line 31), `Engine` struct (line 132): factory for ChatCompleter from agent config.
  - `internal/reconcile/emit.go` — `Verification` struct (line 36), `JSONFinding` (line 59): output shape populated by this story.
  - `internal/registry/config.go` — `AgentConfig` struct: skeptic agent configuration (model, max turns, tool budget bytes, timeout).
  - `internal/payload/` — `FileEntry` struct: code context passed to prompt builder.
  - `internal/verify/select.go` (Story 1) — `SelectEligibleSkeptics`: provides skeptic candidates to invoke.
- **Data Requirements:**
  - No schema changes to `registry.yaml`, `findings.json`, or `verification.json`.
  - New files created: `internal/verify/skeptic.go` (prompt builder), `internal/verify/verdict.go` (verdict parser), `internal/verify/invoke.go` (skeptic driver).
  - New test files: `internal/verify/verdict_test.go`, `internal/verify/verify_test.go`.
  - New test fixtures: `internal/verify/testdata/true-finding.json`, `false-finding.json`, `malformed-response.txt`, `mock-skeptic.go`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Skeptic prompt is too leading or not adversarial enough — skeptic rubber-stamps findings | High — verification provides false confidence | Prompt framing explicitly instructs "try to disprove." Add a test case verifying the prompt contains adversarial framing. Future stories may tune the prompt based on fixture corpus results. |
| LLM produces verdict wrapped in markdown fences or prose instead of bare JSON | Medium — parseVerdict fails to extract JSON | `parseVerdict` should attempt to extract the first JSON object from the response (scan for `{...}`) before falling back to `unverifiable`. Test with fenced and unfenced variants. |
| `invokeSkeptic` silently swallows a programming error (nil pointer, context cancel) making debugging hard | Medium — failures are invisible in CI | The `error` return on `invokeSkeptic` is reserved for programming errors (nil args). Runtime failures are captured in `Verification.Notes`. Add structured logging (with skeptic name, finding ID, error class) at the invocation site so failures are visible in logs even though they don't propagate. |
| Prompt construction includes large file bodies, exceeding the skeptic's context window | Medium — loop fails on first Chat call with context-too-long error | `buildSkepticPrompt` should accept pre-truncated file entries (the caller is responsible for payload context size). Document this contract. Future stories may add per-prompt byte budgets. |
| Tool loop budget trips on every finding, producing all-`unverifiable` verdicts | High — verification stage becomes a no-op | Budget defaults (MaxTurns=10, ToolBudgetBytes=1MB, Timeout=60s) are reused from Epic 2.0 reviewer defaults which are proven adequate. The `trippedBudgets` field in `verification.json` makes this visible. If all findings are unverifiable, the operator can increase budgets via `verify.budgets` in the registry config. |
| Import cycle between `internal/verify` and `internal/fanout` | Medium — build failure | `verify` imports `fanout` (for `invokeToolLoop`, `ChatCompleter`), but `fanout` must not import `verify`. The `Engine.invokeToolLoop` is called by `verify`, not the reverse. Verify with `go build ./...` after initial scaffolding. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Draft - Awaiting Acceptance Criteria
