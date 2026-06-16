## Metadata

- **Last Modified:** 2026-06-15
- **Plan Type:** feature

## Plan Overview

**Plan Type:** feature
**Plan Goal:** Emit a normalized per-reviewer eval record alongside each reconcile run, accumulate records into a local monthly JSONL store, and expose them via `atcr scorecard` (single run) and `atcr leaderboard` (aggregated view). This is the monitoring foundation for the quality pipeline and the data prerequisite for the public Model-Eval Leaderboard (Epic 10.0).
**Target Users:** Developer/reviewer running atcr CLI
**Framework/Technology:** Go, cobra CLI, text/tabwriter, os+bufio JSONL, encoding/json

## Objectives

- Capture the per-reviewer quality signal already produced by `atcr reconcile` as a structured, versioned record.
- Accumulate those records in a local, append-only monthly JSONL store under `~/.config/atcr/scorecard/`.
- Surface single-run results via `atcr scorecard [id-or-path]` as a formatted table.
- Aggregate and rank results across runs via `atcr leaderboard`, with `--since`, `--model`, and `--persona` filters.
- Provide a versioned, anonymized public-submission export via `atcr leaderboard --export` for Epic 10.0.
- Resolve the hard prerequisite in `internal/llmclient` so cost and token fields are populated instead of always empty.

## Planning Deliverables

### User Stories

- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 6 stories

### Acceptance Criteria

- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Generated

## Feature Analysis Summary

This plan adds a persistence and projection layer over the existing reconcile pipeline. Every `atcr reconcile` run already computes the quality signal (per-reviewer attribution, corroboration counts, verification verdicts) — this epic captures that signal as structured JSONL records and surfaces them via two new CLI commands. The record schema is versioned (`schema_version: 1`) to serve as the submission format for Epic 10.0's public leaderboard. A hard prerequisite — `internal/llmclient` usage parsing — must be resolved first; without it, cost/token columns are always empty.

## Technical Planning Notes

- **New package:** `internal/scorecard/` with 4 files: `scorecard.go` (record schema + emitter), `store.go` (JSONL read/query), `aggregate.go` (leaderboard ranking + filters), `export.go` (versioned public submission JSON).
- **Hook point:** `cmd/atcr/reconcile.go:runReconcile` — call `scorecard.Emit()` after `reconcile.RunReconcile` succeeds, before `gateFindings`. Best-effort: errors logged, never returned.
- **Storage:** `~/.config/atcr/scorecard/YYYY-MM.jsonl` — monthly rotation, append-only, local-only.
- **JSONL pattern:** Follow `internal/tools/transcript.go` — `os.OpenFile` with `O_CREATE|O_WRONLY|O_APPEND`, `bufio.Writer`, disabled no-op on error.
- **Schema versioning:** `schema_version` field in every record; future changes increment version, old records remain readable.
- **Aggregate record:** Each run appends one record per reviewer plus one aggregate record for the run, matching the storage model described in the original requirements.

## Implementation Strategy

**Phase 1 — Hard prerequisite (Task 1):** Decode `usage` from provider responses in `internal/llmclient/client.go` (`chatResponse`) and `internal/llmclient/chat.go` (`chatToolResponse`). Surface `tokens_in`, `tokens_out` via `Complete()` and `Chat()` return values. Compute `cost_usd` from a per-model rate table. This unblocks cost/token columns in scorecard records.

**Phase 2 — Core emitter (Tasks 2-3):** Build `internal/scorecard/scorecard.go` — record schema, `Emit(reviewDir, reconcile.Result, emitOpts)` function. Computes per-reviewer metrics from `Findings []Merged` (count findings where reviewer appears, count where `len(Reviewers) >= 2` for corroboration, check `Verification.Verdict` for confirmed/refuted). Writes one JSONL record per reviewer plus one aggregate record per run. Integrate into `cmd/atcr/reconcile.go` after `RunReconcile` succeeds.

**Phase 3 — CLI commands (Tasks 4-5):** Build `cmd/atcr/scorecard.go` (`atcr scorecard [id-or-path]` — reads reconciled findings, displays per-reviewer table via `text/tabwriter`) and `cmd/atcr/leaderboard.go` (`atcr leaderboard [--since, --model, --persona]` — reads monthly JSONL files, aggregates, ranks by corroboration rate and cost-per-corroborated-finding). Register both in `cmd/atcr/main.go`.

**Phase 4 — Export + flags (Tasks 6-7):** Build `internal/scorecard/export.go` (versioned public submission JSON, schema v1, anonymization pass — no provider keys, no repo content, no PII). Add `--no-scorecard` flag to `atcr reconcile`. Add `--export` flag to `atcr leaderboard`.

**Phase 5 — Docs + tests (Task 8):** `docs/scorecard.md` (schema, storage, CLI usage, privacy model). Unit tests per AC using testify. Integration test: reconcile → emit → read back.

## Recommended Packages

No new packages required. Existing dependencies cover all needs: `text/tabwriter` (std) for table output, `os`+`bufio` for JSONL append, cobra for CLI, encoding/json for serialization.

## User Story Themes

1. **Auto-emit scorecard:** As a developer running `atcr reconcile`, I want a per-reviewer scorecard record written automatically after each run, so that quality trends are tracked without extra steps.
2. **View single-run scorecard:** As a developer, I want to run `atcr scorecard [id-or-path]` to see a formatted table of per-reviewer metrics for a specific run, so I can compare reviewer performance at a glance.
3. **View aggregated leaderboard:** As a developer, I want to run `atcr leaderboard [--since, --model, --persona]` to see ranked reviewer performance across runs, so I can make data-driven model/persona selections.
4. **Export for public leaderboard:** As a developer, I want to run `atcr leaderboard --export` to emit the versioned anonymized submission JSON, so I can contribute to the public Model-Eval Leaderboard (Epic 10.0).
5. **Suppress emission:** As a developer, I want to pass `--no-scorecard` to `atcr reconcile` to skip scorecard writing, so I can run reconcile without polluting the local store (e.g., test runs, dry runs).

## Planning Success Criteria

- `atcr reconcile` writes a per-reviewer scorecard record to `~/.config/atcr/scorecard/YYYY-MM.jsonl` after each run.
- Record fields: schema_version, run_id, reviewer, model, role, findings_raised, findings_corroborated, findings_solo, corroboration_rate, cost_usd, tokens_in, tokens_out, latency_ms.
- When `reconciled/verification.json` is present, record also includes findings_verified, findings_refuted, survived_skeptic_rate.
- `atcr scorecard [id-or-path]` displays a formatted table for a single run.
- `atcr leaderboard` aggregates across stored records and ranks by corroboration rate; `--since` filters by date range.
- `atcr leaderboard --export` emits the versioned public submission JSON (no PII, no repo content, no provider keys).
- `--no-scorecard` suppresses writing to the local store.
- Schema is versioned; a future schema change increments `schema_version` and old records remain readable.
- Docs: scorecard.md (schema, storage location, CLI usage, privacy model).

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Scorecard JSONL grows unbounded | Monthly rotation; document `~/.config/atcr/scorecard/` location; user can delete old months |
| Export format becomes public API locked too early | Version from day one (`schema_version` field); document v1 is experimental until Epic 10.0 stabilizes it |
| Cost/token fields unavailable from some providers | Fields are `omitempty`; missing data degrades gracefully (shown as `—` in table). **Hard prerequisite:** llmclient must parse `usage` first — without it, columns are always empty, not just degraded. |

## Next Steps

1. `/find-documentation @.planning/plans/active/3.3_per_run_scorecard/`
2. `/create-documentation @.planning/plans/active/3.3_per_run_scorecard/`
3. `/create-user-stories @.planning/plans/active/3.3_per_run_scorecard/`
4. `/create-acceptance-criteria @.planning/plans/active/3.3_per_run_scorecard/`
5. `/design-sprint @.planning/plans/active/3.3_per_run_scorecard/`
6. `/create-sprint @.planning/plans/active/3.3_per_run_scorecard/`
