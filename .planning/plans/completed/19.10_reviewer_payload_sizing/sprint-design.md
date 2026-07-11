# Sprint Design: Per-Model Payload Sizing & Graceful Degradation

**Created:** July 10, 2026
**Plan:** [19.10 Reviewer Payload Sizing](./plan.md)
**Plan Type:** Infrastructure 🏗️ (bugfix characteristics)
**Status:** Design Complete

---

## Original User Request

> Per-Model Payload Sizing & Graceful Degradation for the Multi-Agent Reviewer. The atcr multi-agent reviewer ships a single global byte budget to a heterogeneous roster whose models span 32k→144k-token windows. Confirmed from the 19.6 multi-agent review run: a 101 files / 6,429 insertion diff returned 1 finding from 11 reviewers (5 ok, 3 timeout, 3 failed). Size each reviewer's payload to its own model's token window (reserving the output-token budget), and when a payload still doesn't fit, chunk it to fit using the existing 14.3 chunker made window-aware — degrading gracefully via a configurable `on_overflow` policy rather than silently gutting the review.

**Referenced Resources:**
- [Context-Window Resolver](documentation/context-window-resolver.md) — static per-model context-window table (F1)
- [Per-Agent Budget & Chunking](documentation/per-agent-budget-and-chunking.md) — output-reserved budget and window-aware chunk plan (F2/F3)
- [on_overflow Policy](documentation/on-overflow-policy.md) — degradation ladder and config surface (F4)
- [Cache-Key Correctness](documentation/cache-key-correctness.md) — folding budget/chunk plan into the diff-cache key (F7)
- [Diagnosability Fields](documentation/diagnosability-fields.md) — per-agent `summary.json` fields (F8)
- [Fallback Provenance](documentation/fallback-provenance.md) — fallback model substitution recording (F5)
- [Timeout Scaling](documentation/timeout-scaling.md) — load-scaled timeout for chunked payloads (F6)
- [Config YAML Parsing](documentation/config-yaml-parsing.md) — `gopkg.in/yaml.v3` patterns for `max_sprint_plan_bytes`/`on_overflow` (F9/F4)

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Reviewer Payload Sizing
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 6–8 days
**Phases:** 5
**Pattern:** Foundation → Core Sizing → Overflow & Provenance → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
per-model context window resolver Go
byte to token conversion budget sizing
chunk-to-fit vs shed-to-fit degradation
config precedence pointer Effective resolver pattern
fallback provenance distinct reviewer confidence
```

---

## Complexity Breakdown

- **Architecture:** 1/3 — Explicitly reuses existing primitives (`chunkDiff`, `ApplyByteBudget`) and mirrors established codebase patterns exactly (`CacheMaxBytes`/`ReviewStrategy` config precedence shape, `PathValid` post-merge stamping, `Truncation` non-silent-degradation record). No new architectural pattern is introduced — every task explicitly frames itself as "wiring, not reimplementation."
- **Integration:** 3/3 — Touches 5 internal packages (`internal/payload`, `internal/fanout`, `internal/registry`, `internal/reconcile`, `internal/stream`) plus a new `examples/` script, threading a single provenance/sizing concept (model → window → budget → chunk plan → degradation action) through config precedence, dispatch, caching, and cross-package reconcile confidence math. AC-Live additionally depends on a live external roster (`orchestrator.lan`).
- **Story/Task & Test:** 3/3 — 12 tasks, each with unit + integration test suites (several also add end-to-end `Engine`-driven tests), plus a standalone scripted (non-`go test`) live-audit harness with its own gate logic.
- **Risk/Unknowns:** 2/3 — Most forks were locked during refinement (chunk-to-fit over shed-to-fit, ~3.5 B/token ratio, policy ladder order), reducing design-time risk. Residual unknowns: the exact timeout-scaling curve is an implementation choice (bounded only by monotonicity + clamp), and AC-Live is irreducibly environment-coupled (cannot be verified in CI).

**Time Formula:** Critical-path task-effort sum (S=0.5 dev-day, M=1 dev-day), plus parallel-branch slack absorbed within the critical-path window, plus a cumulative-adversarial-review and live-audit-dry-run buffer.

**Calculation:** Critical path = Task01(S, 0.5) → Task02(M, 1) → Task03(M, 1) → Task04(M, 1) → Task09(S, 0.5) → Task10(S, 0.5) → Task12(M, 1) = **5.5 dev-days**. Parallel branches (Task05 S/0.5, Task06→Task07 S+M/1.5, Task08 S/0.5, Task11 S/0.5) fit inside that window once Task01–04 land, since none of them block or are blocked by the critical path. Add ~0.5–1.5 days for the mandated cumulative adversarial pass and the AC-Live skip-path dry run → **6–8 dev-days total**, consistent with the plan's own 4–6 day estimate at the optimistic (maximal-parallelism) end.

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** standard (not strong)
**Suggested command:** `/create-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 (met: 9/12, 5 phases); gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days (met on all three: 9/12, 5 phases, 6–8 days); strong gated at complexity >= 10/12 (not met: 9/12).

---

## Phase Structure

### Phase 1: Foundation (≈1 day)
- **Task 01 — Per-Model Context-Window Resolver (F1).** Static `model → token window` table + conservative default. No dependencies; unblocks every other sizing task.
- **Task 05 — `on_overflow` Config Schema (F4 config).** Parse/validate/resolve the `on_overflow` policy string through the registry→project precedence chain. No hard dependency on Task 01; runs in parallel as an independent config-layer track.
- **Focus:** establish the two foundational primitives (window lookup, policy config surface) that every downstream task consumes.

### Phase 2: Core Sizing (≈2 days)
- **Task 02 — Per-Agent Effective Input Budget (F2).** Output-reserved, model-derived byte budget; wired into both `ApplyByteBudget` call sites in `internal/fanout/review.go`. Depends on Task 01.
- **Task 03 — Window-Aware Chunking (F3).** Converts Task 02's effective budget into a per-model `maxLines`, wired into the existing `chunkDiff`. Depends on Task 01, Task 02.
- **Task 11 — Configurable Sprint-Plan Limit (F9).** Independent parallel track: `max_sprint_plan_bytes` config key (`internal/registry`) + parameterizing `internal/payload/sprintplan.go`. No hard dependency on F1–F3; shares config files with Task 05 (coordinate merges).
- **Focus:** this phase directly closes the confirmed `dax` boundary-overflow arithmetic (AC1, AC2) and produces the first end-to-end degradation primitive (AC3).

### Phase 3: Overflow & Provenance (≈2 days)
- **Task 04 — `on_overflow` Policy Dispatch (F4 dispatch).** Single dispatch function routing `chunk`/`truncate`/`fallback`/`fail` to the correct primitive. Depends on Task 03.
- **Task 06 — Fallback Provenance, Fanout side (F5 part 1).** Run-level `FallbackCount` tally in `Summary`/`PoolSummary`, plus fixture/e2e proof the bulk path already threads `FallbackUsed`/`FallbackFrom` correctly. No hard dependency; can run in parallel with Task 04/05.
- **Task 07 — Reconcile Fallback-Aware De-Weighting (F5 part 2).** Consumes Task 06's provenance to collapse shared-fallback-model reviewers into one independent voice in the distinct-reviewer CONFIDENCE calculus. Depends on Task 06.
- **Focus:** completes the degradation policy ladder (AC4) and closes the provenance-integrity NFR (AC5) before diagnosability/cache work needs to read from it.

### Phase 4: Integration (≈2 days)
- **Task 08 — Timeout Scaling (F6).** Scales both the per-call (`invokeAgent`) and aggregate (`runEngine`) deadlines from `(base timeout, chunk count)`, clamped to `registry.MaxTimeoutSecs`. Depends on Task 03 (needs the real chunk count Task 03 produces).
- **Task 09 — Cache-Key Correctness (F7).** Folds the per-agent effective budget / chunk-plan into `diffCacheKey`'s tuning token. Depends on Task 02, Task 03.
- **Task 10 — Diagnosability Fields (F8).** Extends `AgentStatus` with `effective_budget`/`resolved_window`/`reserved_output_tokens`/`chunk_count`/`degradation_action`, pure aggregation from Tasks 02/03/04/06's already-computed values. Depends on Task 02, Task 03, Task 04, Task 06.
- **Focus:** this phase is where every prior task's output becomes observable and safe under caching — the three tasks most exposed to "silent regression" risk per the plan's own Risk table (timeout, cache staleness, diagnosability).

### Phase 5: Validation (≈1 day + live-audit buffer)
- **Task 12 — Live Audit Harness (AC-Live).** `examples/19.10-live-audit.sh`: skip-guards on unreachable roster, re-runs the exact confirmed 19.6 range against the real `orchestrator.lan` roster, hard-gates on zero `ContextWindowExceededError` + all five previously-failing agents `status=ok` + findings from ≥2 agents, and prints a before/after evidence table. Depends on Task 01–11 (sequenced last by design).
- **Focus:** full-suite regression (`go test ./...`, `go vet ./...`, lint) plus the standalone live-audit dry run (skip-path verifiable without live access; full-run path requires `orchestrator.lan` connectivity per the plan's Constraints).

---

## Work Decomposition

| # | Task | F-Req | Effort | Depends On | Testable Element |
|---|------|-------|--------|-------------|-------------------|
| 1 | Context-Window Resolver | F1 | S | — | `ContextWindowTokens(model) int`, unit tests (known/unknown/persona-coverage) |
| 2 | Per-Agent Effective Budget | F2 | M | 1 | `EffectiveByteBudget(model, outputTokens) int64`, dax-boundary regression test |
| 3 | Window-Aware Chunking | F3 | M | 1, 2 | chunk-plan helper + `chunkDiff` wiring, lossless-reassembly test |
| 4 | `on_overflow` Policy Dispatch | F4 | M | 3 | `applyOverflowPolicy`, 4-arm + unrecognized-policy tests |
| 5 | `on_overflow` Config Schema | F4 | S | — | `onOverflowValid`, precedence-chain tests |
| 6 | Fallback Provenance — Fanout | F5 | S | — | `Summary.FallbackCount`/`PoolSummary.FallbackCount`, bulk-path fixture + e2e |
| 7 | Reconcile Fallback De-Weighting | F5 | M | 6 | `distinctReviewerCount`, collapsed-independence fixture test |
| 8 | Timeout Scaling | F6 | S | 3 | `scaledTimeoutSecs`, monotonicity + clamp tests |
| 9 | Cache-Key Correctness | F7 | S | 2, 3 | `diffCacheKey` sizing-token fold, collision + backward-compat tests |
| 10 | Diagnosability Fields | F8 | S | 2, 3, 4, 6 | `AgentStatus` 5-field extension, JSON round-trip + omitempty tests |
| 11 | Configurable Sprint-Plan Limit | F9 | S | — | `max_sprint_plan_bytes` precedence chain, caller-supplied-limit tests |
| 12 | Live Audit Harness | AC-Live | M | 1–11 | scripted skip/pass/fail gate, standalone-runnable |

All 12 tasks are pre-existing, fully-specified task files under `tasks/` — this sprint design does not re-scope them, per the skill's "ground decomposition in existing work items" rule for `tasks/`-sourced infrastructure plans.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files in the same package as the code under test (standard Go convention; no separate `test/` tree).

**Test File Placement Examples:**
- `internal/payload/contextwindow_test.go`, `internal/payload/sizing_test.go`, `internal/payload/sprintplan_test.go`
- `internal/fanout/overflow_test.go`, `internal/fanout/cache_test.go`, `internal/fanout/engine_test.go`, `internal/fanout/artifacts_test.go`, `internal/fanout/status_test.go`, `internal/fanout/outcome_test.go`
- `internal/registry/on_overflow_test.go`, `internal/registry/sprintplan_settings_test.go`, `internal/registry/precedence_test.go`
- `internal/reconcile/disagree_test.go`, `internal/reconcile/emit_test.go`, `internal/reconcile/adapter/adapter_test.go`

**Unit/Integration/E2E:**
- **Unit:** table-driven `testing` + `testify` assertions per the coding standard; every task's pure-function helpers (resolver, sizing, chunk-plan, dispatch, scaling, cache-key, precedence) get isolated table-driven coverage.
- **Integration:** several tasks add fixture-based tests that exercise a real `Engine`/`WritePool` pipeline end-to-end (fallback provenance, diagnosability, cache-key collision), following existing precedents (`response_truncation_e2e_test.go`).
- **E2E / Scripted:** Task 12's `examples/19.10-live-audit.sh` is explicitly **not** a `go test` — it is a standalone, env-coupled shell harness (skip-guarded via `atcr doctor`) that re-runs the confirmed 19.6 diff range against the real roster. It must remain fully decoupled from `go test ./...` so CI never blocks on external reachability.

**Test Environment Status:**
- Framework: Go stdlib `testing` + `github.com/stretchr/testify` — established, no new dependency
- Execution: `go test ./...` (project `TEST_COMMAND`) — fully functional today
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (project `COVERAGE_COMMAND`), baseline 80%

---

## Architecture

**Primitives:**
- `payload.FileEntry` / `payload.Truncation` — existing, untouched, reused as-is by the new sizing/chunk-plan helpers
- New: `ContextWindowTokens(model string) int` (F1), `EffectiveByteBudget(model string, outputTokens int) int64` and its chunk-plan sibling (F2/F3), both in `internal/payload/sizing.go`
- New: `OverflowPolicy`/`OverflowResult` (F4, `internal/fanout/overflow.go`) — the single degradation-dispatch primitive

**Module Boundaries:**
- `internal/payload` stays free of any `internal/registry` import (avoids an import cycle) — `ReadSprintPlan`/`ScopeConstraint` take `maxBytes int64` as a caller-supplied parameter (Task 11), not a config lookup.
- `internal/fanout` is the sole consumer that threads model identity, resolved config (`cfg.Settings.*`), and dispatch together — it owns `renderAgent`/`buildFallbackAgent`/`buildSlots`/`runEngine`, all touched across Tasks 02–10.
- `internal/reconcile`'s extracted library boundary (`github.com/samestrin/atcr/reconcile`) is explicitly **not** touched by Task 07 — fallback provenance is stamped only on ATCR-internal `stream.Finding`/`JSONFinding`, mirroring the existing `PathValid`/`PathWarning` precedent.

**External Dependencies:** None new. `gopkg.in/yaml.v3` (already vendored) for the two new config keys; no live tokenizer library — the byte→token ratio is an intentionally conservative static constant (~3.5 B/token).

**Replaceability:** The static context-window table (F1) is designed as a single, clearly-marked source of truth specifically so Epic 19.7 (Live Model Resolution) can later replace it with a live-resolved table behind the same `ContextWindowTokens` signature, with zero call-site changes required.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| Config schema surface (`on_overflow`, `max_sprint_plan_bytes`) | `internal/registry` (config.go, project.go, precedence.go) | Malformed/out-of-range config value causing a silent fallthrough to unintended behavior, or a typo'd key silently ignored | Strict `KnownFields(true)` YAML decoding at every tier; enum validation (`onOverflowValid`) and positive-value validation (`> 0` for byte limit) at load time in both `LoadProjectConfig` and `Registry.validate()`, plus a post-resolution sanity re-check in `ResolveSettings` for directly-constructed `Settings` |
| Fallback-provenance integrity | `internal/fanout` → `internal/reconcile` | An unrecorded model substitution silently inflates the distinct-reviewer CONFIDENCE calculus, misleading downstream severity-gate/reconcile trust decisions | F5 records every substitution at both per-agent (`status.json`) and run (`summary.json`) levels; fail-closed default (missing/malformed status data → treated as non-fallback, never as "assume fallback") |
| Diff-cache correctness | `internal/fanout/cache.go` | A per-agent-sized payload silently served a stale full-payload (or differently-sized) cache hit, corrupting review integrity without any visible error | F7 folds the effective-budget/chunk-plan value into the NUL-separated tuning token; explicit `TestEngine_DifferentSizingMissesCache` regression test verified to fail against pre-F7 code |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|-----------------|--------|----------|
| Per-agent budget + chunk-plan derivation at dispatch | Every reviewer call, N agents × up to 64 chunks/agent | Pure in-memory `O(entries)`, no network/hot-path I/O | Static table lookups + deterministic arithmetic only (Determinism NFR); reuses `chunkDiff` unchanged |
| Serial-lane timeout scaling under chunking | A 32k-window persona fanned into 6+ sequential chunk calls on a slow local backend | No `context deadline exceeded` on a large-but-valid payload (AC6) | `scaledTimeoutSecs(base, chunkTotal)` applied at both the per-call and aggregate deadline seams, monotonic and clamped to `registry.MaxTimeoutSecs` (86400s) |
| Live audit against the real 11-agent roster | 101-file / 6,429-insertion diff, 11 agents, `orchestrator.lan` | Zero `ContextWindowExceededError`; the 5 previously-failing agents all `status=ok` | Skip-guarded (`atcr doctor`) so total unreachability is a no-op, never a CI failure; hard gate only runs when the environment is actually reachable |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Degenerate model windows | A model's window is smaller than `defaultMaxTokens + promptOverhead`; an unmapped model id | `EffectiveByteBudget` returns 0 (never negative/panicking); `ContextWindowTokens` returns the conservative default (never zero) |
| Zero/near-zero chunking | `chunkTotal` of 0 or 1; a pathologically small resolved window feeding the chunk-plan helper | Timeout scaling is a no-op at `chunkTotal <= 1`; chunk-plan `maxLines` clamps to a small positive floor, never triggering `chunkDiff`'s "unlimited" branch unintentionally |
| Zero-fallback / zero-degradation runs | A run where no agent ever falls back or overflows | `fallback_count`, and the F8 diagnosability fields, are explicitly present with zero/absent values per their `omitempty` discipline — never silently omitted when a `0` is the meaningful answer |
| Directly-constructed invalid config | A `Settings` built by directly constructing `proj`/`reg` structs, bypassing file-load validation | Caught by `ResolveSettings`'s post-resolution sanity re-check for both `on_overflow` and `max_sprint_plan_bytes` |
| Config precedence edge cases | Whitespace-only override value at any tier; a typo'd key (`on_overlow`) | Whitespace-only falls through to the next tier (same as existing `ReviewStrategy` behavior); typo'd key rejected by strict decoding |
| Cache-key boundary collision | Two runs, same diff/model/prompt-template, but different effective-budget/chunk-plan regime that happens to render identical prompt text | Sizing token fold-in guarantees distinct keys even in this boundary case |

### Defensive Measures Required

- **Input Validation:** enum validation for `on_overflow` (exactly 4 legal values); `> 0` validation for `max_sprint_plan_bytes` (0 is explicitly NOT a valid "unbounded" sentinel here, unlike `payload_byte_budget`); strict `KnownFields(true)` YAML decoding at every config tier.
- **Error Handling:** `fallback`/`fail` overflow arms return typed/sentinel errors, never silent no-ops or accidental fallthrough to another arm; unrecognized policy strings produce a distinct, clear error.
- **Logging/Audit:** every degradation outcome is recorded in an always-present (non-omitted-when-zero, per the `Truncation`/`TruncatedZeroFindings` precedent) `summary.json`/`status.json` field — `effective_budget`, `resolved_window`, `reserved_output_tokens`, `chunk_count`, `degradation_action`, `fallback_count`.
- **Rate Limiting:** N/A — the equivalent concern (unbounded wall-clock under chunking) is handled by `scaledTimeoutSecs`'s monotonic, clamped scaling rather than a request-rate mechanism.
- **Graceful Degradation:** the `on_overflow` ladder (`chunk` default → `truncate` → `fallback`/`fail` explicit) ensures overflow never hard-fails silently; `chunk` guarantees zero content loss on the default path.

---

## Risks

**Technical:**
- Byte→token ratio too optimistic → residual overflow → mitigated by the conservative ~3.5 B/token ratio (not the codebase's optimistic ~4.1 B/token comment) plus a safety margin; the `on_overflow` net catches any residual tail.
- Chunking a 32k model on a slow backend re-triggers timeouts → mitigated by co-designing Task 08 (timeout scaling) directly against Task 03's real chunk count, not as an afterthought.
- Cache serves a stale full-payload result for a per-agent-sized request → mitigated by Task 09's explicit cache-key fold-in and a regression test verified to catch a reversion.
- Fallback silently corrupts reconcile CONFIDENCE → mitigated by Task 06/07's fail-closed provenance recording and de-weighting, with explicit fixture tests.
- Scope creep into `internal/registry`'s existing timeout/budget resolvers → mitigated by every task explicitly scoping changes to reading already-resolved values and adding only the two new config fields (F4, F9) required.
- Task 05 / Task 11 file-overlap merge risk (both touch `config.go`/`precedence.go`/`project.go`/`.atcr/config.yaml`) → mitigated by physically separating each task's edits near its closest existing precedent field (`ReviewStrategy` for F4, `CacheMaxBytes` for F9), and both diffs being purely additive.

**TDD-Specific:**
- Task 01–03's foundational helpers must land with regression tests that explicitly name the confirmed `dax` boundary arithmetic (`24577 + 8192 > 32768`) so a future refactor cannot silently reintroduce the exact bug this sprint fixes.
- Task 12's live-audit harness has no `go test` coverage by design (env-coupled) — its correctness must be verified by the documented manual dry-run procedure (skip-path locally, full-path on a host with real `orchestrator.lan` connectivity) rather than CI, and this gap should be called out explicitly during `/execute-code-review` rather than treated as a coverage shortfall.

---

**Next:** `/create-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/ --gated`
