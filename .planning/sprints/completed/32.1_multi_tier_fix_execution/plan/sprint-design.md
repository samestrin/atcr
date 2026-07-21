# Sprint Design: Multi-Tier Fix Execution Engine

**Created:** July 20, 2026 12:40:47PM
**Plan:** [Multi-Tier Fix Execution Engine](/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/32.1_multi_tier_fix_execution/)
**Plan Type:** Feature
**Status:** Design Complete

---

## Original User Request

> Evolve the ATCR "Fixer" from a naive, single-model executor into a highly configurable, multi-tier execution engine. This allows users to route simple fixes to fast/cheap local models (like Ollama/Llama 3) while reserving expensive frontier models (like GPT-4o or Gemini 1.5) for complex architectural bugs.

**Referenced Resources:** None — `/find-documentation` found no specifications directly matching this plan's scope (threshold 0.7); `documentation/source.md` lists zero sources.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Multi-Tier Fix Execution
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 4
**Pattern:** Foundation → Core Routing → Two-Tier Integration → Documentation & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
executor complexity ceiling routing
generateFixes skip chain conditions
effective value resolver pattern Go
pipeline warning class logging
two-tier fix attribution testing
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - Extends 4 existing `ExecutorConfig` field precedents (`MinSeverity`, `TimeoutSecs`, `Temperature`, `MaxToolCalls`) with the identical named-constant + `EffectiveXxx()` resolver convention; no new architectural pattern introduced.
- **Integration:** 2/3 - Coordinates registry config validation, the verify-package execution engine, pipeline logging/reconcile output, docs/examples, and cross-run fix-attribution state — 3+ components that must stay consistent.
- **Story/Task & Test:** 3/3 - 5 user stories / 12 acceptance criteria spanning unit, integration, and E2E coverage; Story 3's cross-tier partition/attribution logic is explicitly flagged High complexity in `test-planning-matrix.md`.
- **Risk/Unknowns:** 2/3 - An open design question (single-executor-plus-ceiling vs. a true in-process multi-executor chain) is flagged repeatedly across `plan.md`, `README.md`, and the user stories as needing resolution; `EstMinutes` is also a best-effort, model-emitted signal, not a correctness guarantee.

**Time Formula:** COMPLEX bracket (7-9/12 → 8-12 days), refined against story effort estimates (S, M, M, S, S) with one High-complexity integration story (Story 3).
**Calculation:** Foundation 2d + Core Routing 3d + Two-Tier Integration 3d + Documentation & Validation 2d = 10 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strongly gated)
**Suggested command:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/32.1_multi_tier_fix_execution/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Design Decision: Interpretation Locked

**This is the single open design question the plan repeatedly flags as needing resolution before phase structure is fixed.** Resolving it here unblocks `/create-sprint`.

**Decision:** Interpretation (a) — a single `ExecutorConfig` gains a complexity ceiling; "tier 2" is a second, independently-configured `atcr` run against the same `findings.json`. `Registry.Executor` stays a single `*ExecutorConfig` pointer. No in-process ordered executor chain (interpretation (b)) is built in this plan.

**Rationale:**
- The original epic's own AC4 language — "a second run (or secondary tier)" — already anticipated (a) as acceptable.
- Codebase discovery confirms `Registry.Executor` is a single pointer today (`internal/registry/config.go:463-466`); converting it to a slice is a schema-breaking-adjacent change with no requirement forcing it.
- All 5 user stories and all 12 acceptance criteria are already written and scoped against (a) — Story 3 explicitly notes its ACs "hold under either interpretation" but its Technical Considerations, test plan, and worked-example scope assume (a) as the implementation.
- (b) has a precedent in the codebase (`internal/fanout/engine.go`'s Primary/Fallbacks chain, Epic 19.5) that could be borrowed later if a true in-process chain is ever required — deferring that as future scope, not blocking this plan, keeps this sprint additive and low-risk.

**Impact on phase structure:** No `Registry.Executor` schema redesign phase is needed. Phase 3 (Two-Tier Integration) validates the two-independent-runs workflow via test + worked example rather than building executor-chain orchestration.

---

## Phase Structure

### Phase 1: Foundation — Config Surface & Validation (2 days)
**Items:** Story 1 (Configure a Complexity Ceiling), Story 4 (Validate Ceiling Configuration)
**Focus:** Add `MaxEstimatedMinutes *int` and `MaxSeverityForFix string` to `ExecutorConfig`; add their `EffectiveMaxEstimatedMinutes()` / `EffectiveMaxSeverityForFix()` resolvers; add `validateExecutor` range checks (new `MaxExecutorEstimatedMinutes` constant) plus the floor/ceiling cross-field contradiction check. Config-only — nothing consumes these fields yet.

### Phase 2: Core Routing — Skip Chain & Self-Gating (3 days)
**Items:** Story 2 (Skip Over-Ceiling Findings Safely)
**Focus:** Add a `withinComplexityCeiling` predicate in `internal/verify/severity.go`; wire it into `generateFixes`'s existing pre-dispatch skip chain (`internal/verify/executor.go:104-232`) as a fourth condition; add the `executor_ceiling_skip` `logPipelineWarning` class and `FixWarning` message; add the self-gating decline branch so an executor that judges a dispatched fix too complex declines through the identical skip-and-log contract rather than returning a partial fix.

### Phase 3: Two-Tier Integration & Verification (3 days)
**Items:** Story 3 (Run a Second Tier Over Skipped Findings)
**Focus:** Integration/E2E test running `generateFixes` twice against the same fixture finding set with two different `ExecutorConfig`s (low-ceiling then high/no-ceiling), asserting every finding is fixed by exactly one tier or explicitly skipped-and-logged — never both, never neither. Dedicated assertion on fix-attribution state to prove tier 2 never re-attempts a tier-1-fixed finding.

### Phase 4: Documentation & Validation (2 days)
**Items:** Story 5 (Document the Multi-Tier Workflow)
**Focus:** `docs/registry.md` executor field table gains `max_estimated_minutes`/`max_severity_for_fix` rows; `docs/findings-format.md`'s `EST_MINUTES` description cross-references the new routing consumer; `examples/registry-with-executor.yaml` gains a worked cheap-tier + frontier-tier example, validated by loading it through atcr's registry loader (dry-run, zero load errors). Sprint-wide Definition of Done validation.

---

## Work Decomposition

### Phase 1 — Story 1: Configure a Complexity Ceiling on the Executor
- **Testable elements:** `ExecutorConfig` struct fields exist and parse (01-01); `EffectiveMaxEstimatedMinutes()`/`EffectiveMaxSeverityForFix()` return correct defaults when unset (01-02)
- **Test type:** Unit (`internal/registry/executor_config_test.go`)
- **AC links:** [01-01](acceptance-criteria/01-01-executorconfig-exposes-complexity-ceiling-fields.md), [01-02](acceptance-criteria/01-02-effective-value-resolvers-return-correct-defaults.md)

### Phase 1 — Story 4: Validate Ceiling Configuration
- **Testable elements:** Non-positive/over-cap `max_estimated_minutes` rejected; invalid `max_severity_for_fix` rejected; floor-above-ceiling contradiction rejected (04-01, 04-02)
- **Test type:** Unit (`internal/registry/executor_config_test.go`)
- **AC links:** [04-01](acceptance-criteria/04-01-numeric-and-severity-ceiling-values-are-range-validated.md), [04-02](acceptance-criteria/04-02-floor-ceiling-contradiction-is-rejected-at-load-time.md)

### Phase 2 — Story 2: Skip Over-Ceiling Findings Safely
- **Testable elements:** Ceiling-exceeding findings skipped before dispatch with `FixWarning` + `executor_ceiling_skip` log (02-01); self-gating decline never surfaces as `Fix` content (02-02); existing confidence/severity/attribution skip chain and failure branches unaffected (02-03)
- **Test type:** Unit + Integration (`internal/verify/executor_test.go`)
- **AC links:** [02-01](acceptance-criteria/02-01-ceiling-exceeding-findings-are-skipped-before-dispatch.md), [02-02](acceptance-criteria/02-02-self-gating-decline-never-presents-a-partial-fix-as-complete.md), [02-03](acceptance-criteria/02-03-existing-skip-chain-and-failure-branches-remain-unaffected.md)

### Phase 3 — Story 3: Run a Second Tier Over Skipped Findings
- **Testable elements:** Two-tier run partitions every finding exactly once (03-01); fix attribution prevents double-processing across tiers (03-02); workflow is test-verified and reproducible via fixture (03-03)
- **Test type:** Integration + E2E (`internal/verify` package)
- **AC links:** [03-01](acceptance-criteria/03-01-two-tier-run-partitions-every-finding-exactly-once.md), [03-02](acceptance-criteria/03-02-fix-attribution-prevents-double-processing-across-tiers.md), [03-03](acceptance-criteria/03-03-two-tier-workflow-is-test-verified-and-reproducible.md)

### Phase 4 — Story 5: Document the Multi-Tier Workflow
- **Testable elements:** Field table + prose documented in `docs/registry.md`/`docs/findings-format.md` (05-01, manual review); worked two-tier example is valid, loadable YAML (05-02)
- **Test type:** Manual + Integration (dry-run config load)
- **AC links:** [05-01](acceptance-criteria/05-01-ceiling-fields-documented-in-registry-and-findings-format-docs.md), [05-02](acceptance-criteria/05-02-worked-two-tier-example-is-valid-and-runnable.md)

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Same-package `_test.go` files (Go convention) — `internal/registry/executor_config_test.go`, `internal/verify/executor_test.go`, `internal/verify/severity_test.go` (new).

**Test File Placement Examples:**
- `internal/registry/executor_config_test.go` — new tests: `TestExecutor_MaxEstimatedMinutesOutOfRangeRejected`, `TestExecutor_MaxSeverityForFixInvalidRejected`, `TestExecutor_MaxSeverityForFixBelowMinSeverityRejected`, `TestExecutor_EffectiveMaxEstimatedMinutesDefault`
- `internal/verify/executor_test.go` — new tests: `TestGenerateFixes_SkipsAboveComplexityCeiling`, `TestGenerateFixes_SelfGatingDeclineNotPartialFix`, `TestGenerateFixes_TwoTierPartitionsFindingsExactlyOnce`
- `internal/verify/severity_test.go` — new test for `withinComplexityCeiling` predicate in isolation

**Unit/Integration/E2E:**
- Unit (6 ACs: 01-01, 01-02, 02-01, 02-02, 04-01, 04-02) — table-driven, `go test ./...`, no external dependencies
- Integration (4 ACs: 02-03, 03-01, 03-02, 05-02) — exercise `generateFixes` end-to-end against fixture finding sets and, for 05-02, the real registry YAML loader
- E2E (1 AC: 03-03) — full two-tier workflow reproduced against a fixture `findings.json` with a deliberate mix of below-ceiling / tier-1-only / tier-2-only / above-both-ceilings findings
- Manual (1 AC: 05-01) — documentation review, no automated assertion

**Test Environment Status:**
- Framework: `go test` (standard library `testing`, `stretchr/testify` where already in use)
- Execution: `go test ./...` per `.planning/.config/config.yaml`; coverage via `go test -coverprofile=coverage.out ./...` (baseline 80%)
- Coverage Tools: Existing project coverage baseline (80%) applies; no new tooling required

---

## Architecture

**Primitives:**
- `ExecutorConfig` (`internal/registry/config.go:206-225`) — the executor's declarative capability/eligibility surface
- `Finding.EstMinutes` / `JSONFinding.EstMinutes` (`internal/stream/parser.go:53`, `internal/reconcile/emit.go:69`) — the existing, already-populated complexity signal this plan routes on (no new signal invented)
- `FixWarning` (`internal/reconcile/emit.go:136`) — the existing per-finding skip-visibility carrier

**Module Boundaries:**
- `internal/registry` owns config schema, defaults, and validation (`ExecutorConfig`, `validateExecutor`, `EffectiveXxx()` resolvers) — black box to callers, who read only via the effective-value methods
- `internal/verify` owns the routing/execution decision (`generateFixes`, `meetsSeverityFloor`, new `withinComplexityCeiling`) — consumes `internal/registry`'s resolved config, never re-derives defaults itself
- `internal/verify/pipeline.go`'s `logPipelineWarning` is the single classed-warning sink; this plan adds one new class string, no new plumbing

**External Dependencies:** None new — internal routing/config logic against the existing standard library and already-vendored dependencies (per `plan.md`'s Recommended Packages: none identified).

**Replaceability:** A new ceiling field can be added or removed without touching `generateFixes`'s dispatch mechanics; the ceiling check is a pure predicate (`withinComplexityCeiling`) that could be swapped or extended (e.g. a future weighted score) without changing the skip-chain's control flow.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| `atcr.yaml` ceiling field parsing | `internal/registry/config.go` (`ExecutorConfig`, `validateExecutor`) | Malformed/malicious YAML values (negative, absurdly large, non-canonical severity strings) causing a panic, silent misconfiguration, or a permanently-ineligible executor | Named-constant range checks (`MaxExecutorEstimatedMinutes`), `reclib.NormalizeSeverity` validation against the canonical severity set, accumulated (never short-circuited) `errs`, explicit floor/ceiling contradiction check |
| Skip/decline reason strings reaching logs and `findings.json` | `internal/verify/executor.go`, `internal/reconcile/emit.go` (`FixWarning`) | Finding `File`/`Line` or model-generated decline text propagating unsanitized into logs/report output | Reuse the existing `logPipelineWarning` contract (path-bearing detail at Debug, class at Warn) unchanged; no new untrusted-content sink introduced |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Per-finding pre-dispatch skip chain in `generateFixes` | One evaluation per finding per run, inside the existing bounded worker pool (`reg.Verify.MaxParallel`) | No measurable added latency vs. current confidence/severity/attribution checks | `withinComplexityCeiling` is an O(1) integer comparison, evaluated pre-dispatch (before any goroutine/API call is spawned) — adds no I/O and no new allocation on the hot path |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| `EstMinutes` zero/unset value | A finding with `EstMinutes == 0` (non-numeric model output parses as 0 per `docs/findings-format.md`) | Treated as "no estimate provided," not as "trivially cheap" (always passes) or "unknown" (always skipped) — explicit test case per Story 2's Potential Risks |
| Ceiling field nil vs. explicit zero | `MaxEstimatedMinutes == nil` (unset) vs. a hypothetical explicit `0` | Pointer type distinguishes unset (no ceiling / unlimited) from an explicit value, matching the `TimeoutSecs`/`MaxToolCalls` convention |
| Floor/ceiling contradiction | `max_severity_for_fix` configured below `min_severity_for_fix` | Rejected at config-load time with a clear error (Story 4), never silently producing a permanently-ineligible executor discovered only at runtime |
| Self-gating decline mid-dispatch | Executor determines complexity mid-attempt (not pre-dispatch) that a fix is beyond its capability | Declines via the identical `FixWarning` + `logPipelineWarning` skip contract as a pre-dispatch ceiling skip — never returns partial/incomplete `Fix` content |
| Cross-tier fix-attribution | Tier 2 run encounters a finding tier 1 already fixed, vs. one tier 1 skipped | Existing "not already fix-attributed" skip condition prevents re-attempt on tier-1-fixed findings; ceiling-skipped findings remain eligible for tier 2 — dedicated test asserts on attribution state, not just absence of a crash (Story 3 Potential Risks) |
| Every tier's ceiling set too low | Operator misconfigures both tiers so a finding is skipped by all of them | Must remain visible via non-empty `FixWarning` in `findings.json`/report output — never misreadable as a false "clean" run (plan Risk 3) |

### Defensive Measures Required

- **Input Validation:** All new `ExecutorConfig` fields validated in `validateExecutor` with named-constant range checks and cross-field (floor vs. ceiling) contradiction detection, matching the existing accumulate-don't-short-circuit convention.
- **Error Handling:** Ceiling-skip and self-gating-decline paths never surface as silent `continue` — both must set `FixWarning` and emit a `logPipelineWarning("executor_ceiling_skip", ...)` call, distinct from `executor_fix_failed` (a provider/transport error, not a deliberate decline).
- **Logging/Audit:** Reuse `logPipelineWarning` (`internal/verify/pipeline.go:41`) unchanged — one new class string only; no new logging sink.
- **Rate Limiting:** Not applicable — this plan adds no new external calls; the ceiling check reduces API calls (skips before dispatch) rather than adding any.
- **Graceful Degradation:** A ceiling-skipped or self-declined finding is always left visible and re-attemptable by a later tier — never dropped, never presented as fixed when it is not.

---

## Risks

**Technical:**
- Design ambiguity between single-executor+ceiling and true multi-executor chain → **Resolved above** (Design Decision section) in favor of the additive, lower-risk interpretation already assumed by all 5 stories.
- `EstMinutes` is a best-effort, model-emitted integer that can be under/over-estimated → Treat the ceiling as a soft routing hint layered on existing confidence/severity gates, not a correctness guarantee on its own (already documented in Story 1/2 Assumptions).
- Cross-tier fix-attribution edge cases (Story 3, flagged High complexity in `test-planning-matrix.md`) → Dedicated integration test asserting on attribution state explicitly, not just absence of a crash.

**TDD-Specific:**
- Story 4 (validation hardening) depends on Story 1's fields existing first → Sequenced together in Phase 1 so RED/GREEN cycles for both land before Phase 2 begins.
- Story 5's worked example depends on Story 3's mechanism being finalized → Sequenced last (Phase 4); if the Design Decision above is disturbed later, the example must be updated before Story 5 is considered done.
- Phase 2's self-gating branch must not leave a finding with both a stale `Fix` and a decline warning → Follow `generateFixes`'s existing documented invariant that the valid-syntax branch clears `FixWarning` unconditionally; decline branch must return before any `f.Fix` assignment (Story 2 Potential Risks).

---

**Next:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/32.1_multi_tier_fix_execution/`
