# User Story 6: Document Scorecard Schema and Usage

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer using `atcr`
**I want** clear documentation for the scorecard schema, storage layout, CLI commands, and privacy model
**So that** I can understand what data is collected, where it lives, how to query it, and how to safely contribute to the public Model-Eval Leaderboard (Epic 10.0).

## Story Context

- **Background:** Epic 3.3 introduces a local scorecard store, new CLI commands (`atcr scorecard`, `atcr leaderboard`), and a public-submission export format. Without documentation, developers cannot discover these features, interpret the metrics, or understand the privacy guarantees.
- **Assumptions:** The implementation of Stories 1-5 defines the final schema, CLI flags, and export behavior. Documentation is written after those behaviors are stable.
- **Constraints:** Documentation must be accurate, versioned with `schema_version: 1`, and must not duplicate the CLI `--help` text. It must cover the privacy model because the export feature touches public submission data.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Stories 1-5 (schema, CLI, export, and suppression behavior must be implemented) |

## Success Criteria (SMART Format)

- **Specific:** `docs/scorecard.md` exists and documents: the version 1 per-reviewer record schema (every field, required vs. conditional, aggregation rules), the monthly JSONL storage location (`~/.config/atcr/scorecard/YYYY-MM.jsonl`) and rotation behavior, CLI usage for `atcr scorecard [id-or-path]`, `atcr leaderboard [--since] [--model] [--persona] [--export] [--output]`, the `--no-scorecard` suppression flag, and the privacy/anonymization model for `--export`.
- **Measurable:** Every field in the v1 schema has a description and type. Every CLI flag introduced by Epic 3.3 has a usage example. The privacy section explicitly lists what is stripped and what is preserved in the public submission format.
- **Achievable:** Documentation is prose and examples; no code changes beyond the stories already required.
- **Relevant:** Required by the original epic acceptance criteria and plan success criteria. Enables correct interpretation of scorecard metrics and safe public submission.
- **Time-bound:** Completed within this sprint after Stories 1-5 are stable.

## Acceptance Criteria

| AC | Title | File |
|----|-------|------|
| 06-01 | Scorecard Documentation | [06-01-scorecard-documentation.md](../acceptance-criteria/06-01-scorecard-documentation.md) |

## Original Criteria Mapping

1. Docs: `scorecard.md` (schema, storage location, CLI usage, privacy model). → **[06-01](../acceptance-criteria/06-01-scorecard-documentation.md)**

## Technical Considerations

- **Implementation Notes:** Create `docs/scorecard.md`. Structure: overview, record schema v1 (with JSON example), storage layout and rotation, `atcr scorecard` usage, `atcr leaderboard` usage (table and export), privacy model, and Epic 10.0 submission notes. Keep the CLI `--help` content brief; focus on examples and semantics.
- **Integration Points:** Links to [Story 1](01-auto-emit-scorecard.md) for emission, [Story 2](02-view-single-run-scorecard.md) for single-run CLI, [Story 3](03-view-aggregated-leaderboard.md) for aggregation CLI, [Story 4](04-export-public-leaderboard.md) for export, and [Story 5](05-suppress-emission.md) for suppression.
- **Data Requirements:** Accurate descriptions of all v1 schema fields and the public submission v1 schema fields.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Docs written before implementation stabilizes, then become stale | Medium | Write docs after CLI/export behavior is implemented; include `schema_version: 1` so future schema bumps require doc updates. |
| Privacy model under-documented, leading to accidental public submission of sensitive data | High | Explicit allowlist of preserved fields; examples of stripped data; reference Epic 10.0 privacy requirements. |
| Docs duplicate `--help` output and fall out of sync | Low | Focus on examples, semantics, and schema; let `--help` own flag syntax. |

---

**Created:** June 15, 2026
**Status:** Draft - Awaiting Acceptance Criteria
