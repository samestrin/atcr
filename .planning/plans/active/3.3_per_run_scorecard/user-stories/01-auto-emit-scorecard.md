# User Story 1: Auto-emit Scorecard

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer running `atcr reconcile`
**I want** a per-reviewer scorecard record written automatically after each run
**So that** quality trends are tracked without extra steps, enabling data-driven reviewer and model selection over time.

## Story Context

- **Background:** Every `atcr reconcile` run already computes per-reviewer quality signal — findings raised, corroboration rates, and post-verify survived-skeptic outcomes. Currently this signal is computed and then discarded when the process exits. Epic 3.3 introduces persistence of this signal as the foundation for quality trending (Epic 3.3 internal) and the public Model-Eval Leaderboard (Epic 10.0).
- **Assumptions:** Reconcile output (per-reviewer findings, corroboration, verification status) is already available in-memory at the end of a run. A local `~/.config/atcr/scorecard/` directory can be created on first write. JSONL is an appropriate append-only format for small, frequent writes.
- **Constraints:** No new analysis or computation — only persistence of existing signals. Must not slow down reconcile noticeably. Must be suppressible via `--no-scorecard`. Schema must be versioned (`schema_version: 1`) so Epic 10.0 can evolve the public submission format independently.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Epic 3.0 reconcile output (findings, corroboration, `survived_skeptic` signal) |

## Success Criteria (SMART Format)

- **Specific:** After every `atcr reconcile` run, one JSONL record per reviewer (plus one aggregate record) is appended to `~/.config/atcr/scorecard/YYYY-MM.jsonl` containing reviewer identity, model, findings raised/corroborated/solo, corroboration rate, verification counts (when `verification.json` is present), survived-skeptic rate, cost, token counts, and latency.
- **Measurable:** 100% of reconcile runs produce at least N+1 records (N = number of reviewers + 1 aggregate). Records pass JSON schema validation. `--no-scorecard` suppresses emission with zero records written.
- **Achievable:** Uses existing in-memory reconcile data; no new LLM calls or analysis. File I/O is limited to a single append per run.
- **Relevant:** Establishes the data pipeline that makes `atcr leaderboard` (Story 2/3) and Epic 10.0 public submission possible. Without persisted records, no trending or comparison is possible.
- **Time-bound:** Implemented and verified within this sprint.

## Acceptance Criteria Overview

1. A scorecard JSONL file is created/updated in `~/.config/atcr/scorecard/` after each reconcile run.
2. Each record matches the versioned schema (`schema_version: 1`) with all required fields populated from reconcile output.
3. Verification-dependent fields (`findings_verified`, `findings_refuted`, `survived_skeptic_rate`) are omitted when `verification.json` is absent and populated when present.
4. `--no-scorecard` flag suppresses all scorecard writes for that run.
5. An aggregate record summarizing the full run is appended alongside per-reviewer records.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/3.3_per_run_scorecard/`_

## Technical Considerations

- **Implementation Notes:** Hook into reconcile's post-completion path. Build record objects from existing in-memory data structures (reviewer findings, corroboration results, verification status, token/cost/latency tracking). Open JSONL file in append mode, write one JSON object per line, close. Create directory and file if they don't exist. Use atomic append (write + flush) to avoid corruption on crash.
- **Integration Points:** Reconcile completion handler (where summary.json is written). Verification module (to detect `verification.json` presence). CLI flag parser (for `--no-scorecard`).
- **Data Requirements:** Schema version 1 record shape as defined in plan. Per-reviewer: `reviewer`, `model`, `role`, `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms`. Conditional: `findings_verified`, `findings_refuted`, `survived_skeptic_rate`. Run-level: `run_id`, `schema_version`, timestamp.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| File write fails (permissions, disk full) silently drops scorecard data | Medium | Log warning on write failure; do not fail the reconcile run. Surface error in reconcile summary output. |
| Concurrent reconcile runs corrupt JSONL file | Low | Use file locking (flock) or append-only writes with fsync. Each record is a single line — partial writes are recoverable. |
| Schema evolves and old records become hard to query | Low | `schema_version` field on every record. Query tools (leaderboard) handle version negotiation. |
| `--no-scorecard` flag not recognized, records emitted anyway | Low | Integration test that runs reconcile with flag and asserts zero records written. |

---

**Created:** June 15, 2026 10:47:26AM
**Status:** Draft - Awaiting Acceptance Criteria
