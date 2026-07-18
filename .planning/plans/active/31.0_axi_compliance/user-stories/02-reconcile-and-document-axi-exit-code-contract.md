# User Story 2: Reconcile and Document the AXI Exit-Code Contract

**Plan:** [31.0: AXI Agent eXperience Interface Compliance](../plan.md)

## User Story

**As a** CI/orchestration engineer wiring `atcr` into a larger agentic swarm or pipeline
**I want** `atcr review --axi` (and other AXI-aware subcommands) to exit with the same deterministic, already-documented codes atcr uses today (`0`=clean, `1`=gate-failure, `2`=usage-error, `3`=auth-error)
**So that** existing CI scripts and new agent orchestrators can branch on exit code alone, without needing to special-case `--axi` invocations or track two competing exit-code schemes

## Story Context

- **Background:** The epic's original proposal (`0`=success, `1`=actionable findings, `2`=internal/syntax error) conflicts with the exit-code contract atcr already ships and CI scripts already depend on: `0`=clean, `1`=gate-failure, `2`=usage-error, `3`=auth-error, implemented via `exitFailure`/`exitUsage`/`exitAuth` constants, the `codedError` type (`ExitCode() int`), and the single `exitCode(err)` dispatch function in `cmd/atcr/main.go:126-165`, and documented in `docs/ci-integration.md`'s exit-semantics table. `atcr verify` (Epic 3.0) independently arrived at the identical `0`/`1`/`2` = success/gate-failure/usage-error mapping when it was designed, reinforcing this as the codebase's settled convention rather than an accident. Plan.md (line 24, line 68, and the Risk Mitigation section) explicitly directs this plan to reconcile with the existing contract rather than silently diverging from it, and identifies exit-code reconciliation as Extended Scope requiring its own user story.
- **Assumptions:**
  - The existing 0/1/2/3 contract is correct and stable; this story does not change what any existing code means for non-AXI invocations.
  - `--axi` mode is purely an output-format concern; it does not need a wider or narrower set of exit codes than plain-text mode.
  - "Internal/syntax error" cases the epic's original proposal wanted a dedicated code for (e.g., invalid YAML, model API failure) already map naturally onto the existing contract's `exitUsage` (`2`, for cases the operator can fix, such as invalid config/YAML) or `exitFailure` (`1`, for cases where the review itself could not complete) — no new code is required.
  - `atcr review`, `atcr report`, and `atcr reconcile --fail-on` are the AXI-relevant entry points; `atcr verify` already conforms to this contract and needs no change, only confirmation.
- **Constraints:**
  - Do not add a parallel exit-code mechanism, a second `exitCode`-like function, or a `--axi`-specific override path — all AXI exit-code resolution must flow through the single existing `exitCode(err)` dispatch point at `cmd/atcr/main.go:156`.
  - Do not repurpose code `2` for "internal/syntax error" as the epic's literal text proposed; that would break the usage-error meaning CI scripts already depend on.
  - Must not alter the numeric meaning of any existing code (`0`, `1`, `2`, `3`) for non-AXI invocations — this is a documentation/verification story, not a breaking change.
  - `docs/ci-integration.md`'s exit-semantics table is the canonical human-facing reference and must stay in sync with `cmd/atcr/main.go`'s comment block.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None (can proceed in parallel with the `--axi` flag/formatter work in Story 1, since this story only verifies and documents exit-code behavior rather than building the payload format) |

## Success Criteria (SMART Format)

- **Specific:** `atcr review --axi`, `atcr report --axi` (or equivalent), and `atcr reconcile --fail-on --axi` exit with `0` on a clean result, `1` when findings/gate-failure occur, `2` on usage/configuration error, and `3` on auth failure — identical to their non-`--axi` behavior — verified via the single `exitCode(err)` dispatch point in `cmd/atcr/main.go:156`, with no second exit-code mechanism introduced.
- **Measurable:** A test (or set of tests) exercises all four exit codes under `--axi` mode and asserts the numeric value returned by the process for each scenario; `docs/ci-integration.md`'s exit-semantics table and `cmd/atcr/main.go`'s exit-code comment block both explicitly state the reconciliation decision (that AXI mode reuses the existing contract, and that the epic's original 2=internal-error proposal was rejected in favor of mapping such cases to the existing `usageError`/`exitFailure` codes).
- **Achievable:** No new exit-code infrastructure is required — the `codedError`/`exitCode()` pattern already exists and generalizes to error paths introduced by AXI output handling (e.g., a malformed `ATCR_AXI_MAX_LINES` value maps to `usageError`).
- **Relevant:** This is the exact conflict plan.md flags as the epic's most significant deviation risk; without an explicit, documented reconciliation, `--axi` could ship with silently inconsistent exit semantics that break the CI scripts and agent orchestrators the whole epic exists to serve.
- **Time-bound:** Completed within this plan's sprint, verified before `atcr review --axi`/`atcr report --axi` are marked feature-complete in Story 1.

## Acceptance Criteria Overview

1. `--axi` mode does not change the numeric meaning or trigger conditions of exit codes `0`, `1`, `2`, or `3` for any covered subcommand (`atcr review`, `atcr report`, `atcr reconcile --fail-on`), confirmed against `atcr verify`'s independently-arrived-at identical mapping as cross-validation.
2. Any new error condition introduced specifically by `--axi` handling (e.g., invalid pagination env var, unsupported format combination) is explicitly classified into the existing contract (`usageError`/exit `2` for operator-fixable config problems, `exitFailure`/exit `1` otherwise) via the existing `codedError` pattern — not a new exit code.
3. `docs/ci-integration.md`'s exit-semantics table and the `cmd/atcr/main.go` exit-code comment block are updated to state, in writing, that `--axi` reuses the existing 0/1/2/3 contract and that the epic's original "2=internal/syntax error" proposal was deliberately not adopted, with the reasoning documented inline.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`_

## Technical Considerations

- **Implementation Notes:** This is primarily a verification + documentation story, not new exit-code plumbing. Confirm every AXI-mode code path that can fail (flag parsing, pagination env var parsing, format rendering) resolves its error through `usageError()`/`authError()`/plain `error` (defaulting to `exitFailure`) exactly as non-AXI paths already do, then route it through the existing `exitCode(err)` function at `cmd/atcr/main.go:156` — no `--axi`-specific branch in that function.
- **Integration Points:** `cmd/atcr/main.go` (`exitFailure`/`exitUsage`/`exitAuth` constants, `codedError`, `exitCode()`); `cmd/atcr/review.go` and the `atcr report`/`atcr reconcile --fail-on` command paths where errors originate; `docs/ci-integration.md`'s exit-semantics table; `atcr verify`'s exit-code table as precedent to cite in documentation (see `documentation/exit-code-cli-mcp-precedent.md`).
- **Data Requirements:** None — no schema or persisted data changes; this story is process/error-classification and documentation only.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A new AXI-specific error path (e.g., bad `ATCR_AXI_MAX_LINES` value) is left unwrapped and falls through to the generic `exitFailure` (1) instead of the more precise `exitUsage` (2), misclassifying operator-fixable config errors as review-outcome failures. | Medium — CI scripts branching on exit code 2 vs 1 could misinterpret a config typo as "findings present." | Explicitly enumerate every new AXI-introduced error source during implementation and wrap each with `usageError()` where the cause is operator-fixable configuration, following the existing pattern used elsewhere in `cmd/atcr/main.go`. |
| Documentation drifts from code: `docs/ci-integration.md`'s table is updated but `cmd/atcr/main.go`'s comment block is not (or vice versa), reintroducing the exact "silent divergence" risk plan.md warns against. | Medium — future contributors could reintroduce a competing exit-code scheme believing none was previously reconciled. | Update both locations in the same change, and cross-reference `documentation/exit-code-cli-mcp-precedent.md` and `atcr verify`'s exit-code table as the cited precedent in both places. |
| A future contributor, unaware of this reconciliation decision, re-attempts to implement the epic's literal 0/1/2="internal/syntax error" scheme for a new AXI feature, reintroducing the conflict this story resolves. | Low — would require also missing the plan.md Extended Scope Annotations and this story's documentation. | Record the reconciliation decision explicitly in both `docs/ci-integration.md` and the `cmd/atcr/main.go` comment block (not only in planning docs), so it is visible at the point future contributors would touch exit-code logic. |

---

**Created:** July 18, 2026 09:03:31AM
**Status:** Draft - Awaiting Acceptance Criteria
