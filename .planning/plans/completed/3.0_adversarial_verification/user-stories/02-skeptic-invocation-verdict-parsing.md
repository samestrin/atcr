# User Story 2: Skeptic Invocation & Verdict Parsing

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** verification pipeline consumer (downstream stories: confidence v2, gate integration, report rendering)
**I want** each selected skeptic to be invoked against a single finding via the Epic 2.0 tool loop, and the skeptic's raw response to be parsed into a structured `Verification` envelope with a valid verdict (`confirmed` | `refuted` | `unverifiable`)
**So that** the pipeline produces a per-finding verdict that downstream stages can consume for confidence recomputation, gate counting, and report rendering — without ever dropping a finding due to malformed output or skeptic failure

## Story Context

- **Background:** Story 1 delivers the selection API (`SelectEligibleSkeptics`) which returns `[]AgentConfig` candidates per finding. This story takes those candidates and drives them: construct a per-finding prompt (finding details + code context + verdict envelope spec), invoke the skeptic through the Epic 2.0 tool loop (`fanout.Engine.Run`), and parse the response into the reserved `Verification` struct at `internal/reconcile/emit.go:36`. The tool loop already handles Chat → tool_calls → dispatch → repeat with budget/hygiene halts and partial-success semantics; skeptics reuse it unchanged. The new work is prompt construction (`internal/verify/skeptic.go`), verdict parsing (`internal/verify/verdict.go`), skeptic invocation (`internal/verify/invoke.go`), and vote aggregation (`internal/verify/votes.go`).
- **Assumptions:**
  - `SelectEligibleSkeptics` (Story 1) returns at most `n` eligible skeptics per finding; this story consumes that slice.
  - The `ChatCompleter` interface and the Epic 2.0 tool loop (`fanout.Engine.Run`) are available and unchanged.
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

- **Specific:** (1) `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string` constructs a deterministic per-finding prompt with role framing, finding details (problem, fix, evidence, severity, confidence), code context from payload file entries, tool-access instructions, and the JSON verdict envelope spec (`{"verdict": "...", "reasoning": "..."}`). (2) `invokeSkeptic(ctx context.Context, engine *fanout.Engine, slot fanout.Slot) (*reconcile.Verification, error)` builds a single-skeptic `fanout.Slot` and drives it through `engine.Run`, converting the returned `fanout.Result` into a populated `Verification`. (3) `parseVerdict(response string) (*reconcile.Verification, error)` extracts verdict + reasoning from the raw response, falling back to `unverifiable` for malformed output or invalid verdict enums. (4) `aggregateVerdicts(perSkeptic []*reconcile.Verification) *reconcile.Verification` applies the configured vote count/majority rule: all identical verdicts → that verdict; disagreeing verdicts → `unverifiable` with all reasonings preserved.
- **Measurable:** (1) `go test ./internal/verify/...` passes with >= 95% coverage on `skeptic.go`, `verdict.go`, and `invoke.go`. (2) Verdict parsing tests cover all 7 cases from testing-fixtures.md: confirmed, refuted, unverifiable, malformed JSON, invalid verdict enum, empty response, extra fields. (3) Skeptic invocation tests cover: single skeptic confirms, single skeptic refutes, budget tripped → unverifiable, provider error → unverifiable, malformed output → unverifiable. (4) Vote aggregation tests cover: majority confirms → VERIFIED, majority refutes → LOW, disagreeing skeptics → unverifiable with combined reasoning. (5) `go vet` and existing CI checks remain clean.
- **Achievable:** The tool loop (`fanout.Engine.Run`) and `ChatCompleter` interface are reused unchanged. Prompt construction is string building; verdict parsing is JSON unmarshal + enum validation; vote aggregation is a deterministic fold over per-skeptic verdicts. The `Verification` struct is already reserved. This is integration and parsing work, not new infrastructure.
- **Relevant:** This is the core execution engine of Epic 3.0. Without skeptic invocation and verdict parsing, there is no verification stage — no verdicts to feed confidence v2, no refuted findings to demote, no verified findings to trust. Every downstream story depends on this producing `*Verification` values per finding.
- **Time-bound:** Expected to complete within weeks 1–2 of the 3–4 week epic (immediately after Story 1).

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-skeptic-prompt-construction.md) | Skeptic Prompt Construction | Unit |
| [02-02](../acceptance-criteria/02-02-verdict-parsing.md) | Verdict Parsing | Unit |
| [02-03](../acceptance-criteria/02-03-skeptic-invocation.md) | Skeptic Invocation via Tool Loop | Unit |
| [02-04](../acceptance-criteria/02-04-failure-isolation.md) | Failure Isolation — Finding Never Dropped | Unit |
| [02-05](../acceptance-criteria/02-05-budget-forwarding.md) | Per-Finding Budget Forwarding | Unit |
| [02-06](../acceptance-criteria/02-06-test-coverage.md) | Test Coverage & CI Integration | Unit |
| [02-07](../acceptance-criteria/02-07-verify-min-severity-config.md) | Verify Minimum Severity Registry Config | Unit |

## Original Criteria Overview

1. `buildSkepticPrompt` constructs a deterministic per-finding prompt with adversarial role framing, finding details (problem/fix/evidence/severity/confidence), code context, tool-access instructions, and the JSON verdict envelope spec (`{"verdict": "...", "reasoning": "..."}`).
2. `invokeSkeptic` drives a single skeptic through the Epic 2.0 tool loop via `fanout.Engine.Run` and returns a populated `*reconcile.Verification`; runtime failures are captured as `unverifiable` verdicts rather than propagated errors.
3. `parseVerdict` extracts verdict + reasoning from the raw response, falling back to `unverifiable` with the raw text preserved for malformed output, invalid verdict enums, or empty responses.
4. `aggregateVerdicts` applies the configured vote count/majority rule: all identical per-skeptic verdicts → that verdict; disagreeing verdicts → `unverifiable` with all reasonings preserved.
5. Table-driven unit tests cover all verdict parsing cases, single-skeptic invocation paths, failure isolation, budget forwarding, and vote aggregation.

## Technical Considerations

- **Implementation Notes:**
  - **Prompt construction (`internal/verify/skeptic.go`):** `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string` builds the prompt using `strings.Builder`. Structure: (1) role framing — "You are an adversarial skeptic. Your job is to try to disprove the following finding."; (2) finding details as markdown (problem, fix, evidence, severity, confidence); (3) code context from payload file entries (file path + body in fenced code blocks); (4) tool-access instructions — "You have access to tools to read files and search the codebase. Use them to verify the evidence."; (5) output spec — the JSON envelope `{"verdict": "confirmed|refuted|unverifiable", "reasoning": "..."}` with a note to use `unverifiable` if the verdict cannot be determined. The function is pure (no side effects) and deterministic.
  - **Skeptic invocation (`internal/verify/invoke.go`):** `invokeSkeptic(ctx context.Context, engine *fanout.Engine, slot fanout.Slot) (*reconcile.Verification, error)` constructs a single-skeptic `fanout.Slot` whose Primary `fanout.Agent` carries the per-finding prompt, tool-loop settings (`Tools`, `MaxTurns`, `ToolBudgetBytes`, `SupportsFC`), and skeptic identity. It calls `engine.Run(ctx, []fanout.Slot{slot})` and converts the returned `fanout.Result`: if `Result.Status == OK`, pass `Result.Content` to `parseVerdict`; if `Result.Status` indicates a budget trip or provider error, return `&reconcile.Verification{Verdict: "unverifiable", Notes: <explanation>, Skeptic: <agent.Name>}`. The function never returns an error to the caller — all runtime failures are captured in the `Verification` envelope.
  - **Verdict parsing (`internal/verify/verdict.go`):** `parseVerdict(response string) (*reconcile.Verification, error)` first attempts `json.Unmarshal` into `struct{ Verdict string; Reasoning string }`. On parse failure → `unverifiable` with `Notes: "malformed_output: " + response`. On parse success, validates the verdict enum against `{"confirmed", "refuted", "unverifiable"}`. Invalid enum → `unverifiable` with `Notes: "invalid_verdict: " + verdict + " (raw: " + response + ")"`. Valid enum → `&reconcile.Verification{Verdict: verdict, Notes: reasoning}`. Extra JSON fields are silently ignored (default `json.Unmarshal` behavior). Empty response → `unverifiable` with `Notes: "empty_response"`.
  - **Vote aggregation (`internal/verify/votes.go`):** `aggregateVerdicts(perSkeptic []*reconcile.Verification) *reconcile.Verification` collapses multiple per-skeptic verdicts into one per-finding verdict. If every non-empty verdict is identical, that verdict wins; otherwise the result is `unverifiable` with `Notes` containing each skeptic's reasoning. This is the input shape Story 3 consumes.
  - **Vote count configuration:** The orchestration layer reads `verify.votes` from the registry config (default 1 skeptic per finding) and the `Thorough` option (set by the `--thorough` flag in Story 4) to determine the number of skeptics `n` passed to `SelectEligibleSkeptics`. When `Thorough` is true, `n` is forced to 3 regardless of the config value.
  - **`invokeSkeptic` never propagates errors:** This is a hard constraint. The pipeline must complete verification for all findings even if individual skeptics fail. The `error` return on `invokeSkeptic` is reserved for programming errors (nil context, nil ChatCompleter) — runtime failures (provider error, timeout, malformed output) are captured in the `Verification` envelope.
  - **Transcript recording:** The tool loop already records transcripts to `verify/raw/<skeptic>/transcript.jsonl` (same format as Epic 2.0 reviewer transcripts). This is handled by the loop infrastructure — no new transcript code needed in this story.
- **Integration Points:**
  - `internal/fanout/engine.go` — `Engine.Run` (line 215), `NewEngine`, `WithDispatcher`, `Slot`, and `Agent`: the public tool-loop driver reused for skeptic invocation.
  - `internal/fanout/engine.go` — `ChatCompleter` interface: multi-turn chat contract implemented by the production client and test fakes.
  - `internal/reconcile/emit.go` — `Verification` struct (line 36), `JSONFinding` (line 59): output shape populated by this story.
  - `internal/registry/config.go` — `AgentConfig` struct: skeptic agent configuration (model, max turns, tool budget bytes, timeout).
  - `internal/payload/` — `FileEntry` struct: code context passed to prompt builder.
  - `internal/verify/select.go` (Story 1) — `SelectEligibleSkeptics`: provides skeptic candidates to invoke.
- **Data Requirements:**
  - `registry.yaml` gains an optional `verify` block with `votes` (int, default 1) and per-finding budget fields; this is the only registry schema change in Epic 3.0.
  - `findings.json` and `verification.json` schemas are unchanged by this story.
  - New files created: `internal/verify/skeptic.go` (prompt builder), `internal/verify/verdict.go` (verdict parser), `internal/verify/invoke.go` (skeptic driver), `internal/verify/votes.go` (vote aggregation).
  - New test files: `internal/verify/verdict_test.go`, `internal/verify/verify_test.go`, `internal/verify/votes_test.go`.
  - New test fixtures: `internal/verify/testdata/true-finding.json`, `false-finding.json`, `malformed-response.txt`, `mock-skeptic.go`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Skeptic prompt is too leading or not adversarial enough — skeptic rubber-stamps findings | High — verification provides false confidence | Prompt framing explicitly instructs "try to disprove." Add a test case verifying the prompt contains adversarial framing. Future stories may tune the prompt based on fixture corpus results. |
| LLM produces verdict wrapped in markdown fences or prose instead of bare JSON | Medium — parseVerdict fails to extract JSON | `parseVerdict` should attempt to extract the first JSON object from the response (scan for `{...}`) before falling back to `unverifiable`. Test with fenced and unfenced variants. |
| `invokeSkeptic` silently swallows a programming error (nil pointer, context cancel) making debugging hard | Medium — failures are invisible in CI | The `error` return on `invokeSkeptic` is reserved for programming errors (nil args). Runtime failures are captured in `Verification.Notes`. Add structured logging (with skeptic name, finding ID, error class) at the invocation site so failures are visible in logs even though they don't propagate. |
| Prompt construction includes large file bodies, exceeding the skeptic's context window | Medium — loop fails on first Chat call with context-too-long error | `buildSkepticPrompt` should accept pre-truncated file entries (the caller is responsible for payload context size). Document this contract. Future stories may add per-prompt byte budgets. |
| Tool loop budget trips on every finding, producing all-`unverifiable` verdicts | High — verification stage becomes a no-op | Budget defaults (MaxTurns=10, ToolBudgetBytes=1MB, Timeout=60s) are reused from Epic 2.0 reviewer defaults which are proven adequate. The `trippedBudgets` field in `verification.json` makes this visible. If all findings are unverifiable, the operator can increase budgets via `verify.budgets` in the registry config. |
| Import cycle between `internal/verify` and `internal/fanout` | Medium — build failure | `verify` imports `fanout` (for `Engine.Run` and `ChatCompleter`), but `fanout` must not import `verify`. The engine is the caller, not the callee. Verify with `go build ./...` after initial scaffolding. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Accepted — 6 acceptance criteria defined
