# User Story 3: Run a Second Tier Over Skipped Findings

**Plan:** [32.1: Multi-Tier Fix Execution Engine](../plan.md)

## User Story

**As an** atcr operator running BYO-Keys auto-fix
**I want** to point a second, more capable executor at just the findings a first (cheap/local) tier skipped
**So that** I get a two-tier cheap-then-frontier fix workflow end-to-end, without paying frontier-model rates on findings a cheap model could already handle

## Story Context

- **Background:** Stories 1 and 2 add a complexity ceiling (`max_estimated_minutes`, optional `max_severity_for_fix`) to `ExecutorConfig` and teach `generateFixes` (`internal/verify/executor.go`) to skip-and-log any finding whose `EstMinutes`/severity exceeds that ceiling instead of attempting it. This story is the payoff: demonstrate and validate that running atcr a second time, with a second registry config pointed at a higher (or no) ceiling and a more capable model, picks up exactly the findings tier 1 left behind and fixes them — closing out the original epic's AC4 ("a cheap model knocks out LOW complexity bugs, and a second run (or secondary tier) tackles the remaining HIGH complexity bugs").
- **Assumptions:**
  - **Open design question — not resolved by this story:** whether "multi-tier" means (a) a single executor with a ceiling, where a "second tier" is simply a second independently-configured `atcr` run against the same `findings.json` (`Registry.Executor` stays a single `*ExecutorConfig` pointer — additive, low-risk), or (b) a true in-process ordered chain of executors that atcr walks automatically within one run (`Registry.Executor` becomes a slice/ordered list, mirroring the `Primary`/`Fallbacks` shape already used for reviewer agents in `internal/fanout/engine.go`, Epic 19.5 — a larger schema change). The original epic's own AC4 phrasing ("a second run (or secondary tier)") anticipated (a) as acceptable, and Stories 1-2's additive, single-pointer scope assumes (a). This story's acceptance criteria must be written so they hold under (a) as the default, primary path, while not contradicting a (b) implementation if a future clarification session chooses it.
  - Tier 1 and tier 2 runs share the same `findings.json` (or an equivalent finding set) as their common input; tier 2 does not need to regenerate reviewer findings, only re-execute fixes against ones tier 1 skipped.
  - Fix attribution already exists (`generateFixes`'s "not already fix-attributed" skip condition) so tier 2 does not re-attempt findings tier 1 already fixed.
  - Users configure tier 1 and tier 2 as two separate registry configs (e.g., two YAML files or two profiles) with different `ExecutorConfig` ceilings and model bindings.
- **Constraints:**
  - No fanout/reviewer changes required under interpretation (a) — this is scoped to the executor/fix path only.
  - Must not require re-running the reviewer/fanout stage between tier 1 and tier 2; only the fix-execution stage repeats.
  - Must not silently re-attempt or double-charge for findings tier 1 already fixed or already ceiling-skipped-and-then-fixed by tier 2.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (complexity ceiling config), Story 2 (ceiling-aware skip logic in `generateFixes`) |

## Success Criteria (SMART Format)

- **Specific:** Running atcr twice against the same `findings.json` — once with a low-ceiling "cheap" executor config, once with a high/no-ceiling "frontier" executor config — results in tier 1 fixing only findings within its ceiling and skipping the rest, and tier 2 fixing the remainder without re-touching tier 1's already-fixed findings.
- **Measurable:** An integration/end-to-end test demonstrates a fixture `findings.json` with a mix of LOW and HIGH `EstMinutes` findings; after tier 1 + tier 2, 100% of findings are either fixed or explicitly logged as skipped (never silently dropped), and 0 findings are double-processed by both tiers.
- **Achievable:** Builds entirely on the ceiling config (Story 1) and skip logic (Story 2) already in place; no new subsystem, only a documented workflow plus a test proving the composition works.
- **Relevant:** Directly satisfies the original epic's AC4 — the explicit "cheap tier + frontier tier" outcome that motivated the whole plan.
- **Time-bound:** Deliverable within the current sprint, immediately after Stories 1 and 2 land.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-two-tier-run-partitions-every-finding-exactly-once.md) | Two-Tier Run Partitions Every Finding Exactly Once | Integration |
| [03-02](../acceptance-criteria/03-02-fix-attribution-prevents-double-processing-across-tiers.md) | Fix Attribution Prevents Double-Processing Across Tiers | Integration |
| [03-03](../acceptance-criteria/03-03-two-tier-workflow-is-test-verified-and-reproducible.md) | Two-Tier Workflow Is Test-Verified and Reproducible | E2E |

## Original Criteria Overview

1. A two-tier run (tier 1 low-ceiling executor, tier 2 high/no-ceiling executor, both against the same findings set) results in every finding being fixed by exactly one tier or explicitly logged as skipped — never both, never neither, never silently dropped.
2. Fix attribution correctly prevents tier 2 from re-attempting a finding tier 1 already fixed, and correctly allows tier 2 to attempt a finding tier 1 skipped due to the ceiling.
3. The behavior is validated by an automated test (not manual verification only) using a fixture findings set with a mix of complexity levels, and is documented with a worked example so an operator can reproduce the workflow without reading executor source code.

## Technical Considerations

- **Implementation Notes:** Primarily a test/validation and documentation story under interpretation (a) — Stories 1-2 already provide the mechanism (ceiling config + skip-and-log). This story's work is: (1) an integration test in `internal/verify` (or equivalent) that runs `generateFixes` twice in sequence against the same finding set with two different `ExecutorConfig`s and asserts the fixed/skipped partition; (2) a worked example in `examples/registry-with-executor.yaml` and `docs/registry.md` showing two config profiles (cheap-tier, frontier-tier) intended to run back-to-back against the same `findings.json`. If a clarification session instead settles on interpretation (b) before this story is implemented, the story's acceptance criteria (partition correctness, no double-processing, no silent drops) still apply, but the implementation shifts to exercising an in-process ordered chain rather than two separate CLI invocations — flag this fork explicitly at `/design-sprint` time rather than assuming it away.
- **Integration Points:** `internal/verify/executor.go` (`generateFixes` skip chain from Story 2), `internal/registry/config.go` (`ExecutorConfig` ceiling fields from Story 1), fix-attribution tracking (existing), `findings.json` schema (existing, unchanged — `internal/reconcile/emit.go`).
- **Data Requirements:** A fixture `findings.json` (or equivalent in-memory `[]Finding`) with a deliberate mix of `EstMinutes`/severity values spanning below-ceiling, above-ceiling-tier-1-but-below-ceiling-tier-2, and above-both-ceilings cases, to exercise the full partition matrix in tests.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Design ambiguity ((a) vs (b), see Assumptions) resolved differently mid-sprint after this story's AC are locked | Medium | AC are phrased at the outcome level (partition correctness, no double-processing, no silent drops) so they hold under either interpretation; flag the fork explicitly at `/design-sprint` rather than letting it surface as rework later. |
| Fix-attribution logic has an edge case allowing tier 2 to re-attempt or skip-miss a tier-1-fixed finding | High | Dedicated test case asserting attribution state carries correctly between the two runs; assert on the finding's attribution field, not just absence of a crash. |
| Two independently-configured runs is a de facto answer to the "second run" reading of AC4 but may not satisfy a stakeholder who expected in-process chaining | Low | Explicitly document the interpretation used (per Assumptions above) in the worked example and `docs/registry.md`, so the tradeoff is visible rather than assumed. |

---

**Created:** July 20, 2026
**Status:** Draft - Awaiting Acceptance Criteria
