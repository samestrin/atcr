# User Story 5: Publish the Agentic Consumption Orchestration Guide

**Plan:** [31.0: AXI Agent eXperience Interface Compliance](../plan.md)

## User Story

**As an** engineer building an autonomous agent or orchestration pipeline that invokes `atcr` as a subprocess (e.g., a sweeper that reviews its own generated code)
**I want** a single, example-driven documentation page that explains how to invoke `--axi` mode, interpret its exit codes, detect pagination truncation, and rely on stdout/stderr separation
**So that** I can safely compose `atcr` into a larger agentic swarm without re-deriving these contracts by reading source code or reverse-engineering behavior from trial and error

## Story Context

- **Background:** The epic's acceptance criteria explicitly require an "Agentic Consumption" documentation section. Plan.md's Expected Outcomes and `files_to_create` name the concrete deliverable: `docs/agentic-consumption.md`. This story is the last of five and is deliberately positioned last because its job is to synthesize the outputs of Stories 1-4 (the `--axi` payload format, the reconciled exit-code contract, pagination/truncation behavior, and stderr/stdout isolation) into one coherent orchestration guide rather than introduce new behavior. Plan.md's Extended Scope Annotations section also calls for a cross-reference from the existing `docs/ci-integration.md` (which already documents the exit-semantics table this story must not restate incorrectly) to the new page, "for discoverability."
- **Assumptions:**
  - Stories 1-4 define the actual behavior (the `--axi` flag/payload format, the exit-code reconciliation, the 500-line default cap with `ATCR_AXI_MAX_LINES` override and `truncated` flag, and the stderr-only diagnostics guarantee); this story documents that behavior, it does not define or change it.
  - The exit-code contract to document is the reconciled 0=clean / 1=gate-failure / 2=usage-error / 3=auth-error scheme from Story 2, not the epic's original (rejected) 0/1/2 proposal — restating the rejected scheme would directly contradict `docs/ci-integration.md` and this plan's own Risk Mitigation section.
  - `docs/ci-integration.md` and `docs/findings-format.md` are the closest existing precedents for structure and tone and should be followed for consistency (existing docs use Markdown with code-fenced examples and short explanatory prose).
  - No source code changes are required or in scope for this story; it is documentation-only.
- **Constraints:**
  - Must not duplicate `docs/ci-integration.md`'s exit-semantics table verbatim as a competing source of truth — reference/link it, and only restate the codes as needed for orchestration context.
  - Must not introduce a new schema or contract of its own; every claim in the doc (payload shape, exit codes, pagination fields, stderr guarantee) must trace back to what Stories 1-4 actually implement, verified against the shipped code/flags rather than the epic's original draft language.
  - The cross-reference addition to `docs/ci-integration.md` must be minimal and additive (a link/section pointer), not a restructuring of that file.
  - This story cannot be finalized until Stories 1-4 have settled their concrete flag names, env var names, and field names (e.g., `truncated`), since the doc must reflect actual shipped behavior, not placeholders.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Stories 1-4 (this story documents their concrete outputs: the `--axi` payload format from Story 1, the reconciled exit-code contract from Story 2, the pagination/truncation contract from Story 3, and the stderr/stdout isolation guarantee from Story 4; should be sequenced last so it reflects final, not draft, behavior) |

## Success Criteria (SMART Format)

- **Specific:** `docs/agentic-consumption.md` exists, covers all five required topics (invoking `--axi` on `atcr review`/`atcr report`, the reconciled exit-code contract, pagination/truncation detection, stderr/stdout separation, and a worked orchestration example), and `docs/ci-integration.md` contains a cross-reference link to it.
- **Measurable:** The doc's exit-code section matches `docs/ci-integration.md`'s exit-semantics table exactly (0/1/2/3, no reintroduction of the epic's rejected internal-error-as-2 scheme); the pagination section names the actual default (500 lines), the actual env var (`ATCR_AXI_MAX_LINES`), and the actual truncation flag (`truncated`) as shipped by Story 3; the worked example is a runnable (or near-runnable) shell/pseudocode snippet showing a subprocess invocation, payload parsing, and exit-code branching.
- **Achievable:** Purely additive documentation work with no new code paths; all source facts are already established by Stories 1-4's implementations by the time this story is executed.
- **Relevant:** Directly satisfies the epic's explicit acceptance criterion for an "Agentic Consumption" docs section, and is the deliverable that makes the other four stories' work discoverable and usable by the epic's target audience (agent/orchestration engineers) rather than only visible in code.
- **Time-bound:** Completed within this plan's sprint, after Stories 1-4 have landed their concrete implementation details, so the doc reflects shipped behavior rather than draft assumptions.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-agentic-consumption-doc-content.md) | Publish Core Content of `docs/agentic-consumption.md` | Integration |
| [05-02](../acceptance-criteria/05-02-worked-orchestration-example.md) | Worked Orchestration Example (Autonomous Sweeper Scenario) | Integration |
| [05-03](../acceptance-criteria/05-03-ci-integration-cross-reference.md) | Additive Cross-Reference from `docs/ci-integration.md` | Integration |

## Original Criteria Overview

1. `docs/agentic-consumption.md` is created and covers: `--axi` invocation on `atcr review`/`atcr report`, the reconciled exit-code contract (0/1/2/3), pagination/truncation (500-line default, `ATCR_AXI_MAX_LINES`, `truncated` flag), and the stderr-only-diagnostics/stdout-only-payload guarantee.
2. The doc includes a worked example modeled on the epic's own motivating scenario — an autonomous sweeper invoking `atcr review --axi` as a subprocess, parsing the resulting payload, and branching on exit code.
3. `docs/ci-integration.md` gains a cross-reference (link or pointer section) to `docs/agentic-consumption.md` for discoverability, without restructuring or duplicating its existing exit-semantics table.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`_

## Technical Considerations

- **Implementation Notes:** Draft the doc only after Stories 1-4's concrete flag/env-var/field names are finalized, to avoid documenting placeholder names that drift from shipped behavior. Structure it to mirror `docs/ci-integration.md` and `docs/findings-format.md` (Markdown, short prose sections, fenced code examples) for consistency with existing docs. Link rather than duplicate the exit-semantics table already in `docs/ci-integration.md`.
- **Integration Points:** New file `docs/agentic-consumption.md`; a minimal additive cross-reference edit to `docs/ci-integration.md`; content grounded in the concrete outputs of Stories 1 (`--axi` flag/payload), 2 (exit-code contract), 3 (pagination), and 4 (stderr isolation); may also reference `docs/findings-format.md`'s existing `atcr-findings/v1` agent-facing format precedent where relevant for context.
- **Data Requirements:** None — documentation only, no schema, code, or persisted-data changes.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The doc is drafted before Stories 1-4 finalize concrete names (flag syntax, env var name, field names), producing documentation that describes placeholder or draft behavior instead of what actually shipped. | Medium — orchestration engineers following the doc would hit mismatches between documented and actual behavior, undermining the epic's trust goal. | Sequence this story last and verify every concrete detail (flag names, env var, field names, exit codes) against the actual shipped implementation from Stories 1-4 before publishing, not against plan.md's draft language. |
| The doc's exit-code section inadvertently restates the epic's original (rejected) 0/1/2="internal/syntax error" scheme instead of the reconciled 0/1/2/3 contract from Story 2, reintroducing the exact divergence the epic's Risk Mitigation section warns against. | High — a published doc with the wrong exit-code contract would actively mislead orchestration engineers and contradict `docs/ci-integration.md`. | Cross-check the exit-code section word-for-word against `docs/ci-integration.md`'s exit-semantics table and Story 2's reconciled contract before publishing; do not draft this section from the epic's original acceptance-criteria text. |
| The cross-reference edit to `docs/ci-integration.md` balloons into an unplanned restructuring of that file, creating scope creep beyond the "for discoverability" intent noted in plan.md's Extended Scope Annotations. | Low | Keep the `docs/ci-integration.md` change to a single additive link/pointer; do not reorder or rewrite existing sections. |

---

**Created:** July 18, 2026 09:03:31AM
**Status:** Draft - Awaiting Acceptance Criteria
