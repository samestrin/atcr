# User Story 6: In-Repo Documentation

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Go developer who wants to extend their ATCR review panel
**I want** clear, in-repo guides for installing a community persona and authoring a new one, plus updated registry reference docs that cover the `language` field and skeptic routing behavior
**So that** I can set up domain-specific review coverage and contribute personas without reading source code or asking on a forum

## Story Context

- **Background:** The persona ecosystem (T1–T8) ships new capabilities — bonus personas, language-aware skeptic routing, domain bundles, and a personas CLI — but none of it is documented in `docs/`. A contributor landing on the repo today would have to read Go source to understand the `language` field on `AgentConfig`, the two-partition skeptic routing algorithm, or how to structure a new `.md` persona file. T7-in-repo fills that gap by shipping three deliverables: `docs/personas-install.md` (CLI install/remove/upgrade workflow), `docs/personas-authoring.md` (persona file format and CI fixture requirements), and an update to `docs/registry.md` (new `language` field reference table row and skeptic routing prose). The two example registry files (`examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml`) each get optional commented-out `language` examples on at least one agent definition to show the canonical form.
- **Assumptions:** All in-scope CLI commands (`atcr personas install`, `atcr personas list`, etc.) are shipped by T2 before these docs are finalized. The `language` field canonicalization rules (no leading dot, lowercased, e.g. `["go", "ts"]`) are settled as of the 2026-06-24 technical decisions. The deprecated path `docs/examples/registry.yaml` no longer exists and must not be referenced. Community-repo scaffolding (T3/T4) and the external contribution guide (community half of T7) are descoped; these docs cover in-repo usage only.
- **Constraints:** Documentation must match the implemented behavior exactly — no aspirational or speculative content. The `docs/personas-install.md` guide covers only commands that exist in the binary as of Sprint B. `docs/personas-authoring.md` is scoped to in-repo persona authoring (the six generalists plus the three bonus personas as reference examples); community-repo contribution workflow is out of scope. `docs/registry.md` is updated, not replaced — only the `language` field and skeptic routing sections are added; existing content is preserved.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (T1 — bonus personas `sentinel`/`tracer`/`idiomatic` shipped); Story 2 (T2 — `atcr personas` CLI commands finalized); Story 3 (T8 — `AgentConfig.Language` field and skeptic routing behavior finalized) |

## Success Criteria (SMART Format)

- **Specific:** Three documentation deliverables are committed to `docs/`: `personas-install.md` (covering `atcr personas install`, `remove`, `list`, `search`, `test`, `upgrade`, and `install bundle/<name>`), `personas-authoring.md` (covering persona `.md` file structure, required front-matter fields, and CI fixture format in `personas/testdata/`), and an updated `docs/registry.md` (new `language` field row in the agent fields table, plus a prose section on language-aware skeptic routing). Both example registry files in `examples/` have at least one agent definition with an optional commented-out `language` example using canonical form.
- **Measurable:** A reviewer unfamiliar with the codebase can follow `personas-install.md` from start to finish — installing a community persona, verifying it with `atcr personas test`, and listing installed personas — without opening any Go source file. `personas-authoring.md` contains a complete worked example of a minimal valid persona file and its corresponding fixture. `docs/registry.md` diff is net-additive: zero existing lines removed, only new rows and a new section added.
- **Achievable:** All three deliverables are prose and YAML/Markdown — no code changes required. The content is derived directly from finalized T2 and T8 behavior, which is verified-green before T7-in-repo begins.
- **Relevant:** Without these docs, vertical-market adoption of domain personas stalls because the install workflow and persona authoring contract are undiscoverable. Documentation is the bridge between shipping features and teams actually using them.
- **Time-bound:** Delivered within Sprint B (9.0) after T2, T5, T6, and T8 are green, before the sprint's cumulative adversarial review.

## Acceptance Criteria Overview

1. `docs/personas-install.md` exists and covers the full install/remove/list/search/test/upgrade lifecycle for community personas, including `install bundle/<name>`, with copy-pasteable CLI examples that match the actual command signatures from T2.
2. `docs/personas-authoring.md` exists and documents the persona `.md` file format (required and optional YAML front-matter fields including `language`), the fixture format in `personas/testdata/`, and references the three shipped bonus personas (`sentinel`, `tracer`, `idiomatic`) as worked examples.
3. `docs/registry.md` is updated with a new `language` field row in the agent fields table and a prose subsection explaining language-aware skeptic routing — what canonical form looks like, how the two-partition reorder works, and what happens when no language match exists (silent fallback to the general pool). No existing content is removed.
4. Both `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` include at least one agent definition with a commented-out `language` field example in canonical form (e.g. `# language: [go]`).

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Write `docs/personas-install.md` and `docs/personas-authoring.md` as new files; update `docs/registry.md` in place (add a `language` row to the agent fields table at `docs/registry.md:60-71` and a new subsection after the existing agent fields section). In both example registry files, add a comment block on one reviewer agent (e.g. `bruce`) showing `# language: [go]  # optional: route language-matched findings to this agent first`. Avoid referencing any path that does not exist in the committed tree.
- **Integration Points:** `docs/registry.md` (agent fields table and new skeptic routing prose); `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` (optional `language` comment on agent entries); no code files are touched by this story.
- **Data Requirements:** No schema changes. The `language` field canonical form is `[]string` of lowercased extension strings without a leading dot (e.g. `["go", "ts"]`), as decided 2026-06-24 and implemented by T8. Documentation must reflect this canonical form exactly — no examples using `.go` or `Go`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Docs written before T2/T8 are finalized may describe wrong command signatures or field names | Medium | T7-in-repo is the last task in Sprint B; it is blocked on T2 and T8 being verified-green before prose is written. |
| `docs/personas-authoring.md` describes fixture format that drifts from `personas/testdata/` contents | Low | Reference actual fixture file paths from T1 (`personas/testdata/`) as concrete examples; the authoring guide is reviewed against the committed fixtures before the sprint PR is opened. |
| Example registry files get out of sync with the real agent field set after future sprints | Low | The examples use commented-out `language` lines, so they are syntactically inert — future field additions do not break them. |
| `docs/registry.md` update accidentally removes or reformats existing content | Low | Use a net-additive edit strategy (insert-only); the PR diff is reviewed to confirm zero existing lines are deleted. |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
