# User Story 5: Suppress Emission

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer running `atcr reconcile`
**I want** to pass `--no-scorecard` to skip scorecard writing
**So that** I can run reconcile without polluting the local store (e.g., test runs, dry runs, debugging sessions).

## Story Context

- **Background:** Story 1 introduces automatic scorecard emission at the end of every `atcr reconcile` run. While this is the correct default behavior for production runs, developers frequently run reconcile in non-production contexts — testing persona changes, debugging finding logic, dry-running against sample repos, or iterating on prompts. Each of these runs would append records to the JSONL store, making aggregated leaderboard data noisy and unreliable. A suppression flag gives developers control over when data enters the persistent store.
- **Assumptions:** The `--no-scorecard` flag is parsed at the CLI level before reconcile execution begins. The scorecard emission hook (from Story 1) checks a single boolean gate before writing. The flag does not affect any other reconcile behavior — analysis, corroboration, and summary output proceed normally.
- **Constraints:** The flag must be honored 100% of the time — even a single leaked record during a `--no-scorecard` run undermines trust in the store. Must not alter reconcile's exit code or output format. Must be discoverable via `--help`.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (Auto-emit Scorecard) — the emission path that this flag suppresses |

## Success Criteria (SMART Format)

- **Specific:** When `atcr reconcile --no-scorecard` is executed, zero scorecard records are written to `~/.config/atcr/scorecard/` for that run. All other reconcile behavior (analysis, corroboration, summary output, exit code) is identical to a run without the flag.
- **Measurable:** Integration test runs reconcile with `--no-scorecard` against a test fixture and asserts: (a) zero new lines appended to the monthly JSONL file, (b) reconcile exit code is 0, (c) reconcile summary output is unchanged compared to a run without the flag. A second test confirms that without the flag, records ARE written (regression guard).
- **Achievable:** Single boolean flag threaded from CLI parser to the scorecard emission hook. No changes to reconcile logic, analysis, or output.
- **Relevant:** Protects the integrity of the scorecard store. Without suppression, developers will avoid running reconcile for testing purposes — or worse, forget they ran test runs and draw incorrect conclusions from leaderboard data.
- **Time-bound:** Implemented and verified within this sprint.

## Acceptance Criteria

| AC | Title | File |
|----|-------|------|
| 01 | CLI Flag Registration & Help Text | [05-01-cli-flag-registration.md](../acceptance-criteria/05-01-cli-flag-registration.md) |
| 02 | Suppression Gate — Zero Records Written | [05-02-suppression-gate.md](../acceptance-criteria/05-02-suppression-gate.md) |
| 03 | Default Behavior Preserved & No Side Effects | [05-03-no-side-effects.md](../acceptance-criteria/05-03-no-side-effects.md) |

### Overview

1. `atcr reconcile --no-scorecard` completes normally but writes zero scorecard records.
2. `atcr reconcile` (without the flag) continues to emit scorecard records as defined in Story 1.
3. The flag appears in `atcr reconcile --help` output with a clear description.
4. The flag does not affect reconcile's exit code, stdout/stderr output, or summary.json content.
5. Reconcile summary output does not print any scorecard-related message (success or failure) when `--no-scorecard` is passed — emission is silently skipped.

## Technical Considerations

- **Implementation Notes:** Add `--no-scorecard` boolean flag to the reconcile CLI command definition. Thread the flag value through to the reconcile execution context (or pass as a parameter to the post-completion hook). In the scorecard emission path (Story 1), check the flag as the first condition — if true, return early without opening the JSONL file. No partial writes, no logging of "scorecard suppressed" messages. Keep the change minimal: one flag, one early-return guard.
- **Integration Points:** CLI flag parser (reconcile subcommand). Scorecard emission hook from Story 1 (the single point where records are written). `--help` text generation.
- **Data Requirements:** No new data structures. The flag is a runtime boolean with no persistence — it is not stored in config, not written to summary.json, not included in scorecard records.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Flag is silently ignored due to a code path that bypasses the guard | High | Integration test that asserts zero records written with `--no-scorecard`. Add a second test that asserts records ARE written without the flag (regression guard). |
| Developer typos the flag (e.g., `--no-scorecards`, `--skip-scorecard`) and records are emitted anyway | Low | Use exact flag name from plan (`--no-scorecard`). Document in `--help`. Consider adding common aliases if the CLI framework supports them. |
| Flag suppresses scorecard but reconcile still fails — error output mentions scorecard confusingly | Low | Scorecard emission is a post-completion step. If reconcile fails before reaching that step, no records would be written regardless. Ensure error paths do not reference scorecard status. |
| Future scorecard consumers (leaderboard, export) need to handle gaps from suppressed runs | Low | JSONL is already append-only and sparse by nature. Leaderboard aggregation (Story 3) naturally handles missing runs — no special logic needed. |

---

**Created:** June 15, 2026 10:47:26AM
**Status:** AC Generated
