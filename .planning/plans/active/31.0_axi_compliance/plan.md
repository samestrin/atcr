# Plan 31.0: AXI Agent eXperience Interface Compliance

## Plan Overview
**Last Modified:** 2026-07-18 (refined via `/refine-plan --deep`)
**Plan Type:** feature
**Plan Goal:** Add a first-class `--axi` (Agent eXperience Interface) output mode to `atcr` so autonomous agents can consume review findings as token-dense, machine-readable output instead of human-formatted Markdown/ANSI. The mode must layer onto atcr's *existing* exit-code contract and stderr-only diagnostics rather than replacing them, and must ship with pagination guarantees and an agentic-orchestration doc.
**Target Users:** Autonomous coding agents and CI orchestrators invoking `atcr` programmatically (primary); human engineers building agentic pipelines around atcr (secondary).
**Framework/Technology:** Go, Cobra CLI, existing `internal/report` multi-format renderer (md/json/checklist/sarif).

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/*.md`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/*.md`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`

## Feature Analysis Summary
Codebase discovery found that two of the epic's four acceptance criteria are already substantially satisfied by existing infrastructure: `cmd/atcr/main.go` already centralizes a coded exit-code contract (`exitFailure=1`, `exitUsage=2`, `exitAuth=3`, `codedError`/`exitCode()`), documented in `docs/ci-integration.md`; and `internal/log` already enforces stderr-only diagnostics, with stdout reserved for command output (and the MCP protocol stream in `atcr serve`), documented in `docs/logging.md`. `internal/report/render.go` already dispatches findings to multiple output formats (`FormatMarkdown`, `FormatJSON`, `FormatChecklist`, `FormatSarif`) for `atcr report`, which is the closest existing analog to the new token-dense format and the natural extension point. The epic's `internal/cli/` and `internal/formatters/` component paths do not exist in the codebase; the real equivalents are `cmd/atcr/` and `internal/report/`. `atcr review`'s live progress/summary output (`cmd/atcr/review.go`, via `cmd.OutOrStdout()`) is a separate code path from `atcr report`'s findings rendering and both need coverage for full AXI compliance.

## Technical Planning Notes
- Reconcile, don't replace, the exit-code contract: `atcr review`/`atcr reconcile --fail-on` already use 0=clean / 1=gate-failure / 2=usage-error / 3=auth-error. The epic's proposed 0/1/2 scheme is close but reassigns `2` from "usage error" to "internal/syntax error" — this plan must define the reconciled contract explicitly rather than silently diverging from what CI scripts already depend on.
- Extend `internal/report`'s existing format-enum pattern (`FormatMarkdown`, `FormatJSON`, ...) with a new token-dense format rather than building a parallel writer stack, for consistency with the SARIF/checklist formats already living there.
- `atcr review`'s human-readable progress/summary output is a distinct code path from `atcr report`'s findings rendering (`cmd.OutOrStdout()` calls in `cmd/atcr/review.go` vs. `internal/report.Render`); `--axi` needs to suppress/replace both, not just the final report.
- Stderr isolation (AC2) and exit-code determinism (AC3) are largely pre-existing invariants — implementation effort should focus on verifying they hold under `--axi` rather than building them from scratch.
- A prior epic (7.2) already consolidated divergent Markdown renderers into one shared renderer in `internal/reconcile` — any new AXI/TOON formatter should follow that shared-renderer precedent instead of adding a third divergent rendering path.
- An existing lightweight agent-facing format precedent (`# atcr-findings/v1`, used in `.atcr/reviews/<id>/sources/pool/`) is documented in `docs/findings-format.md` (with skill-side copies in `skill/findings-format.md` and `skill/host-review.md`) and worth reviewing before finalizing a new TOON schema.

## Implementation Strategy
Implement the `--axi` flag as a cross-cutting output-mode value threaded through command context (mirroring how the root logger and telemetry client are already injected in `PersistentPreRunE`), so both `atcr review`'s live summary output and `atcr report`'s findings rendering can consult it. Extend `internal/report/render.go` with a new token-dense format implementation reusing the existing `Render()` dispatch, and gate `atcr review`'s human-oriented `cmd.OutOrStdout()` writes behind the same axi-mode check. Implement pagination/truncation (default 500-line cap, `ATCR_AXI_MAX_LINES` override) as a wrapping writer or post-processing step applied uniformly to axi-mode output. Reconcile the exit-code contract explicitly in code and docs rather than introducing a second scheme. Close with a new `docs/agentic-consumption.md` page and a cross-reference from `docs/ci-integration.md`.

## Documentation References
- [CLI Command & Output Control Patterns (Cobra)](documentation/cli-command-patterns.md) — **[CRITICAL]**
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)](documentation/exit-code-cli-mcp-precedent.md) — **[CRITICAL]**
- [Existing Agent-Facing Format & Output-Safety Contracts](documentation/agentic-format-precedents.md) — **[IMPORTANT]**
- [MCP Tool Schema & Format-Enum Propagation](documentation/mcp-schema-format-propagation.md) — **[IMPORTANT]**
- [TOON Format Reference (Token Optimized Object Notation)](documentation/toon-format-reference.md) — **[REFERENCE]**

## Recommended Packages
No high-ROI external packages identified. The TOON/token-dense format is a bespoke text encoding best implemented directly (as `internal/report`'s existing JSON/SARIF/Markdown formatters already are), consistent with the codebase's established pattern of hand-rolled formatters over the standard library rather than third-party encoding dependencies.

## User Story Themes

**Persona 1 — Autonomous coding agent operator:** Runs `atcr review --axi` as a subprocess, needs stdout to be a clean, directly-parseable payload with no ANSI/Markdown noise, and needs the payload to respect a token budget via pagination.

**Persona 2 — CI/orchestration engineer:** Wires `atcr` into a larger agentic swarm/pipeline, relies on deterministic exit codes to branch orchestration logic, and needs documentation describing how to compose atcr safely with other tools.

Estimated story themes (5 stories):
1. `--axi` flag produces a clean TOON/compact-JSON payload on stdout, stripped of ANSI/Markdown, for `atcr review` and `atcr report`.
2. Exit-code contract is reconciled and documented as deterministic under `--axi` (0/1/2, preserving the existing `3` auth code).
3. Pagination/truncation: default 500-line cap with a `truncated` flag, overridable via `ATCR_AXI_MAX_LINES`.
4. Stderr isolation verified/enforced under `--axi`: progress/diagnostic logs never leak onto stdout.
5. `docs/agentic-consumption.md` published, explaining orchestration patterns for swarms invoking atcr.

## Planning Success Criteria
- `atcr review --axi` (and `atcr report --axi`/equivalent — Extended Scope, see below) emits a clean, machine-readable payload on stdout with no ANSI codes, Markdown tables, table padding, or visual dividers.
- Stderr carries only diagnostic/progress logs; stdout carries only the final payload, verified for both `atcr review` and `atcr report`.
- Exit codes deterministically and unambiguously reflect the review outcome, with the reconciled 0/1/2(/3) contract documented in one place.
- Large finding sets/diffs are paginated at a documented default (500 lines) with a `truncated` flag, overridable via `ATCR_AXI_MAX_LINES`.
- `docs/agentic-consumption.md` exists and is cross-referenced from `docs/ci-integration.md` (cross-reference is Extended Scope, see below).

## Extended Scope Annotations
The following plan elements go beyond the literal text of `original-requirements.md`. They are preserved deliberately as valuable technical prerequisites or enhancements, and annotated here so reviewers understand their origin.

- **`atcr report` AXI coverage (Extended Scope):** The original request scopes `--axi` to `atcr review`. This plan extends the token-dense payload and stdout/stderr guarantees to `atcr report` as well, because `internal/report/render.go` is the natural extension point for a new output format and agents may consume either invocation path.
- **Exit-code contract reconciliation (Extended Scope):** The original proposes a 0/1/2 scheme that reassigns `2` to "internal/syntax error". This plan deliberately reconciles with the existing documented contract (`0=clean / 1=gate-failure / 2=usage-error / 3=auth-error`, per `docs/ci-integration.md`) rather than silently reassigning codes that downstream CI scripts already depend on. Any change to the existing codes must be explicitly scoped by a user story.
- **`docs/ci-integration.md` cross-reference (Extended Scope):** Original AC4 asks only for an "Agentic Consumption" docs section; the cross-link from the CI integration doc is added for discoverability.

## Risk Mitigation
- **Risk: Silently changing the existing exit-code contract breaks downstream CI scripts.** Mitigation: treat exit-code reconciliation as an explicit design decision recorded in this plan's documentation, not an implicit side effect of `--axi`; keep the existing 0/1/2/3 codes stable unless a user story explicitly scopes a change.
- **Risk: `--axi` only covers `atcr report` and misses `atcr review`'s live progress/summary output, leaving stdout polluted for the primary invocation path.** Mitigation: explicitly scope both code paths (`cmd/atcr/review.go` and `internal/report/render.go`) in the work breakdown, not just the renderer.
- **Risk: Inventing a new TOON schema duplicates the existing `atcr-findings/v1` agent-facing format precedent, fragmenting the "machine format" surface.** Mitigation: review `docs/findings-format.md` and the `atcr-findings/v1` format during design before finalizing the new schema.

## Next Steps
1. `/find-documentation @.planning/plans/active/31.0_axi_compliance/`
2. `/create-documentation @.planning/plans/active/31.0_axi_compliance/`
3. `/create-user-stories @.planning/plans/active/31.0_axi_compliance/`
4. `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`
5. `/design-sprint @.planning/plans/active/31.0_axi_compliance/`
6. `/create-sprint @.planning/plans/active/31.0_axi_compliance/`
