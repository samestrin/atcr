# User Story 6: Persona Guidance & Documentation

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** reviewer operator configuring tool-enabled agents
**I want** persona templates to contain tool-aware guidance, an evidence-citation rule, and up-to-date documentation that explains active registry fields and payload-as-starting-point semantics
**So that** agents use the tool loop effectively, produce findings grounded in evidence the agent actually read, and operators understand how to configure budgets and anticipate costs

## Story Context

- **Background:** Epic 2.0 introduces a multi-turn agent loop, a path-jailed sandbox, and per-agent budgets. Those mechanisms are useless if the persona prompt does not tell the agent how to use them: when to verify a suspicion, how to budget exploration, what to read before reporting, and how to cite evidence. This story owns the persona content and the operator-facing documentation that makes the feature discoverable and configurable.
- **Assumptions:**
  - `PayloadContext.ToolsEnabled bool` is implemented by [User Story 1: Agent Loop Execution](01-agent-loop-execution.md), so persona templates can branch on it.
  - The registry fields `tools`, `max_turns`, and `tool_budget_bytes` are parsed and validated (reserved in Epic 1.1) and activated by [User Story 2: Budget Enforcement](02-budget-enforcement.md) and [User Story 4: Graceful Degradation](04-graceful-degradation.md).
  - Existing persona files follow the project resolution chain documented in `docs/registry.md`.
- **Constraints:**
  - No new third-party dependencies — persona rendering and documentation use existing tooling.
  - Tool-aware guidance must not widen review scope by accident; the existing 1.0 scope rule (findings target the changed range unless explicitly out-of-scope-flagged) remains in force.
  - Documentation updates must align with the actual implementation in Stories 1-5; no speculative features.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (`PayloadContext.ToolsEnabled`); User Story 2 (registry defaults/validation); User Story 4 (`supports_function_calling` registry field) |

## Success Criteria (SMART Format)

- **Specific:** Every shipped persona template that is intended for tool use contains an `{{if .ToolsEnabled}}` section with concrete guidance; the evidence-citation rule is stated in the same section; `docs/registry.md` documents `tools`, `max_turns`, and `tool_budget_bytes` as active fields with defaults and validation rules; `docs/payload-modes.md` explains that the payload is the starting point, not the universe, for tool agents; the README includes cost guidance (3-10× provider calls per tool agent).
- **Measurable:** 100% of tool-capable personas render without error when `ToolsEnabled` is true or false; documentation pages mention each active field at least once; README includes the 3-10× cost multiplier statement; existing persona render tests remain green.
- **Achievable:** The changes are template conditionals, prose edits, and documentation updates — no new engine code beyond what Stories 1-5 already provide.
- **Relevant:** Without this story, agents either ignore the tool loop or use it inefficiently, and operators cannot configure or budget for it. This directly supports the Epic 2.0 objective of "bounded agents that explore the repository through read-only, path-jailed tools."
- **Time-bound:** Completed within the Epic 2.0 sprint sequence, after Stories 1, 2, and 4 reach implementation.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [06-01](../acceptance-criteria/06-01-tool-enabled-persona-guidance.md) | Tool-Enabled Persona Guidance Sections | Unit + Integration |
| [06-02](../acceptance-criteria/06-02-evidence-citation-rule.md) | Evidence-Citation Rule & Scope Guard | Unit + Integration |
| [06-03](../acceptance-criteria/06-03-registry-documentation-activation.md) | Registry Documentation Activation | Documentation |
| [06-04](../acceptance-criteria/06-04-payload-modes-readme-cost-guidance.md) | Payload-Modes Semantics & README Cost Guidance | Documentation |

## Original Criteria Overview

1. Persona templates use `{{if .ToolsEnabled}}` conditional sections to render tool-aware guidance only when the agent is configured with `tools: true`.
2. Tool-aware guidance tells the agent to: verify suspicions before reporting, prefer reading the enclosing file over guessing, and budget exploration within the turn and byte limits.
3. Evidence-citation rule: every finding that relies on tool-gathered evidence must cite the file path and line numbers the agent actually read; unsupported claims are rejected or flagged low-confidence.
4. Scope rule: tools widen *evidence gathering*, not *review scope* — findings still target the changed range unless explicitly tagged `out-of-scope`.
5. `docs/registry.md` updates the reserved-fields table to active status for `tools`, `max_turns`, and `tool_budget_bytes`; documents defaults (`max_turns=10` when tools=true), validation (`max_turns > 0`, `tool_budget_bytes >= 0`), and per-model `supports_function_calling` opt-in.
6. `docs/payload-modes.md` adds a "Tool agents" subsection explaining that the payload is the starting point of the review, not the universe, and that the agent may read additional files through the path-jailed toolset.
7. `README.md` adds a cost-guidance paragraph stating that tool agents typically consume 3-10× the provider calls of a single-shot reviewer and pointing to the budget fields.

## Technical Considerations

- **Implementation Notes:**
  - Update embedded/default persona markdown files (location depends on project layout, commonly `internal/personas/` or embedded assets) to wrap tool guidance in `{{if .ToolsEnabled}}...{{end}}`.
  - Keep the non-tool section unchanged so single-shot agents continue to receive the same 1.0 prompt.
  - Add at least one render test in `internal/payload/personas_render_test.go` that exercises `ToolsEnabled: true` and asserts the tool guidance is present.
  - Update `docs/registry.md` reserved-fields table (currently labeled "Reserved fields (parsed and validated, inert in 1.x)") to reflect Epic 2.0 activation: change the table header/description, mark `tools`, `max_turns`, `tool_budget_bytes` as active, and add a `supports_function_calling` row under the provider/model section.
  - Update `docs/payload-modes.md` with a "Tool agents" or "Payload as starting point" subsection near the scope rules.
  - Update `README.md` (or create a new "Cost guidance" section) with the 3-10× multiplier and a link to `docs/registry.md`.
- **Integration Points:**
  - `internal/payload/template.go` — `PayloadContext.ToolsEnabled` already exists from Story 1; this story consumes it.
  - `internal/payload/personas_render_test.go` — add `ToolsEnabled` render test cases.
  - `docs/registry.md` — owned by this story; Stories 2 and 4 implement the fields it documents.
  - `docs/payload-modes.md` — owned by this story; Story 1 and Story 3 implement the semantics it describes.
  - `README.md` — owned by this story; cross-references Stories 1-5.
- **Data Requirements:**
  - No new structs or schemas; this story edits templates and markdown.
  - `PayloadContext.ToolsEnabled` must be set from `AgentConfig.Tools` before render (Story 1).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Tool guidance is too verbose and consumes too much of the context window | Medium | Keep the conditional section concise; focus on principles (verify, cite, stay in scope) rather than enumerating every tool |
| Tool guidance accidentally widens review scope | High | Explicitly restate the existing scope rule in the same section; require `out-of-scope` tag for pre-existing issues |
| Documentation drifts from implementation as Stories 1-5 evolve | Medium | Mark this story as dependent on Stories 1, 2, and 4; review docs in final acceptance pass |
| Operators misunderstand the 3-10× multiplier as a guarantee rather than a rule of thumb | Low | Phrase as "typically 3-10×" and link to budget fields that cap worst-case cost |
| Existing personas not under version control make updates hard to track | Low | Update only embedded/shipped personas; user-level persona overrides remain the user's responsibility |

---

**Created:** June 13, 2026
**Status:** AC Generated - Ready for Implementation
