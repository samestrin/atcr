# Original Requirements

**Date:** 2026-07-16
**Arguments:** `@.planning/epics/active/29.0_anti_slop_persona.md`
**Target:** `.planning/epics/active/29.0_anti_slop_persona.md`

## Purpose

This document is the immutable source-of-truth capture of the original request that seeded this plan. It is preserved verbatim so that later refinement, decomposition, and review steps can always be checked against original intent.

## Content

# Epic Plan 29.0: The "Anti-Slop" Persona & Content Marketing

- **Estimated time**: TBD
- **Tasks/Components**: 3 / 3
- **Execution**: init-plan

## Objective

Create a specialized, human-named persona (`simon`) designed exclusively to hunt down, flag, and strip out AI-generated code bloat (slop). Accompany this persona with a targeted blog post outline to use as a top-of-funnel marketing asset capitalizing on the growing industry frustration with LLM over-engineering.

## Context

A recent article highlighted a team of engineers ("Slopfix") charging $10,000 a week to delete AI-generated code bloat using AI agents. AI coding assistants (like Copilot and ChatGPT) frequently output overly defensive boilerplate, useless tautological comments, unnecessary abstractions (e.g., interfaces for single structs), and overly long/generic variable names.

ATCR's multi-agent architecture is perfectly positioned to solve this. By shipping a "Pre-Cooked Anti-Slop Agent," we give engineering teams a one-command solution (`atcr review --persona simon`) to automatically catch and trim this bloat during CI, saving thousands of dollars in manual review and refactoring time.

## Proposed Solution

1. **Author the Persona:** Create a new persona YAML and prompt template (e.g., `simon`) in the community registry (`personas/community/`). The system instructions must be hyper-focused on identifying:
   - Tautological or "apologetic" AI comments.
   - Unnecessary design patterns (factories, interfaces) applied to simple logic.
   - "Defensive programming" overkill (null checks where type safety exists).
   - Dead or hallucinated code paths.
2. **Fixture Testing:** Write a specific test fixture (`personas/testdata/simon_fixture.patch`) that feeds the persona a bloated, AI-generated PR and asserts that it correctly flags the slop without complaining about actual business logic.
3. **Blog Post Outline:** Author an outline in `.planning/product/content/blog/slopfix-ai-code-bloat.md` that pitches this feature. The narrative should highlight the $10k/week problem and position ATCR's community persona as the free, automated solution.

## Acceptance Criteria

- [ ] A new human-named persona (`simon`) exists in the registry.
- [ ] The persona's prompt strictly targets AI-generated code bloat and over-engineering.
- [ ] A passing fixture test proves the persona successfully identifies slop in a dummy PR.
- [ ] A blog post outline is authored in `.planning/product/content/blog/` covering the "Slopfix" narrative and positioning ATCR as the solution.

## Components Touched

- `personas/community/` (The new YAML and prompt template)
- `personas/testdata/` (The fixture)
- `.planning/product/content/blog/` (The marketing outline)

## Dependencies

- Epic 19.6 (Community Registry Hub) - This persona will live inside the new ecosystem.
- Epic 23.0 (Human Names for Personas) - Enforces the human-naming convention.

## Refinements (2026-07-15)

This section records findings from `/refine-epic` run on 2026-07-15. It is additive — original plan content above is preserved.

### Auto-applied corrections (2)

- ✅ **Task count mismatch:** Plan claims `TBD` task(s); derived `3`. Updated to "Task Count: 3".
- ✅ **Path correction for Community Personas:** Updated the Components Touched and Proposed Solution to explicitly use `personas/community/` so the agent places the files in the correct location.

### Items needing user confirmation (0)

### Advisory observations (1)

- ℹ️ **Scope-guard violation:** Derived TASK_COUNT=3, COMPONENT_COUNT=3 — exceeds the execute-epic skill's ≤6 tasks / ≤2 components limit. This plan will be rejected by the execute-epic skill and should run through `the init-plan skill @.planning/epics/active/29.0_anti_slop_persona.md` for the full sprint pipeline. Refining alone will not unblock /execute-epic.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 3 (limit: 6)
- Derived COMPONENT_COUNT: 3 (limit: 2)
- COMPONENTS_TOUCHED: `personas/community/`, `personas/testdata/`, `.planning/product/content/blog/`
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: false
- Cited references checked: 0
- Codebase search queries (spot-check): ["simon persona", "personas/ testdata/", "blog slopfix"]
- Deep discovery method: keyword
- Deep discovery queries: personas, community registry, slop, AI code bloat
- Deep discovery match count: 3
- Deep discovery snapshot: /Users/samestrin/Documents/GitHub/atcr/.planning/.temp/refine-epic/codebase-discovery.json (temp-only — not committed)
