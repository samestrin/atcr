# User Story 4: CLI Command & MCP Tool

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** developer running CI gates or using atcr programmatically via MCP
**I want** `atcr verify` as a standalone CLI subcommand, `atcr review --verify` to chain the full pipeline, and an `atcr_verify` MCP tool for programmatic access
**So that** I can run adversarial verification through the same interfaces I already use for review and reconcile — no new workflows to learn, and the verification pipeline is accessible from both human-driven CLI and automated MCP clients

## Story Context

- **Background:** Stories 1–3 deliver the backend infrastructure: skeptic selection (`SelectEligibleSkeptics`), skeptic invocation (`invokeSkeptic`), verdict parsing, confidence v2 recomputation, and artifact re-emission (`verification.json`, re-emitted `findings.json`, updated `manifest.json`, summary verdict counts). This story exposes that pipeline through the user-facing interfaces. The CLI follows the established Cobra pattern: each subcommand is a separate file in `cmd/atcr/` (e.g., `review.go`, `reconcile.go`), registered at `cmd/atcr/main.go:97`. The MCP tool follows the handler pattern: registration in `internal/mcp/server.go:buildServer()` at line 57, handler in `internal/mcp/handlers.go` following `handleReconcile` at line 159. The existing `--reconcile` flag on `atcr review` provides the chaining pattern to mirror.
- **Assumptions:**
  - Stories 1–3 are complete: `internal/verify` provides `Verify(findings, reg, opts) (*Result, error)` which returns a result with `VerdictCounts`, `GateStatus`, and an `Emit(reviewDir)` method that writes all artifacts.
  - The `reconciled/findings.json` input is loadable via `reconcile.ReadReconciledFindings(reviewDir)` at `internal/reconcile/emit.go:145`.
  - The registry is loadable via `registry.Load(registryPath)`.
  - The `--reconcile` flag on `atcr review` already chains review → reconcile; `--verify` mirrors this to chain review → reconcile → verify.
- **Constraints:**
  - The `atcr verify` subcommand must follow the exact same Cobra pattern as `atcr reconcile` — same flag parsing style, same error handling, same output format.
  - The `atcr_verify` MCP tool must follow the exact same handler pattern as `atcr_reconcile` — same input/output shape conventions, same error wrapping.
  - The `--verify` flag on `atcr review` must not break the existing `--reconcile` flag; both can be used independently or together (though `--verify` implies `--reconcile`).
  - All new code must be unit-tested with table-driven tests matching existing patterns.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Skeptic Selection & Role Plumbing), Story 2 (Skeptic Invocation & Verdict Parsing), Story 3 (Confidence v2 & Re-emit) — this story is the user-facing layer over the backend pipeline |

## Success Criteria (SMART Format)

- **Specific:** (1) `cmd/atcr/verify.go` defines `atcr verify [id-or-path]` with flags `--fresh`, `--thorough`, `--min-severity`, registered in `cmd/atcr/main.go:97` via `AddCommand`. (2) `cmd/atcr/review.go` gains a `--verify` flag that chains review → reconcile → verify in one run. (3) `internal/mcp/server.go:buildServer()` registers `atcr_verify` with handler `handleVerify` in `internal/mcp/handlers.go`. (4) All three entry points call `internal/verify.Verify(findings, reg, opts)` and emit artifacts via `result.Emit(reviewDir)`.
- **Measurable:** (1) `go build ./cmd/atcr/...` succeeds; `atcr verify --help` prints usage with all three flags. (2) `atcr verify <path>` on a fixture review directory produces `verification.json`, re-emitted `findings.json`, updated `manifest.json` with `"verify"` in stages, and updated `summary.json` with `verdictCounts`. (3) `atcr review --verify <diff>` chains all three stages and produces the same artifacts as running `atcr review --reconcile` then `atcr verify` separately. (4) `atcr_verify` MCP tool returns structured result with verdict counts and gate status. (5) `go vet ./...` and existing CI checks remain clean. (6) Integration tests cover: CLI invocation, MCP handler invocation, `--verify` chaining, flag combinations (`--fresh`, `--thorough`, `--min-severity`).
- **Achievable:** This is a thin user-facing layer over Stories 1–3. The Cobra pattern and MCP handler pattern are already established; this story copies and adapts them.
- **Relevant:** Without CLI and MCP entry points, the verification pipeline is inaccessible to users. This story is the deliverable — it makes Epic 3.0 usable.
- **Time-bound:** Expected to complete within week 3 of the 3–4 week epic (immediately after Story 3).

## Acceptance Criteria Overview

1. `atcr verify [id-or-path]` subcommand exists in `cmd/atcr/verify.go`, registered in `main.go`, with `--fresh`, `--thorough`, and `--min-severity` flags. It loads the registry, loads reconciled findings, calls `verify.Verify`, and emits all artifacts.
2. `atcr review --verify` chains review → reconcile → verify in one run, mirroring the existing `--reconcile` chaining behavior. `--verify` implies `--reconcile` (verification requires reconciled input).
3. `atcr_verify` MCP tool is registered in `buildServer()` with handler `handleVerify` in `internal/mcp/handlers.go`. It accepts the same parameters as the CLI flags and returns a structured result with verdict counts and gate status.
4. All three entry points (CLI verify, CLI review --verify, MCP atcr_verify) produce identical artifacts for the same input: `verification.json`, re-emitted `findings.json` with verification blocks, `manifest.json` with `"verify"` stage, `summary.json` with `verdictCounts`.
5. Error handling follows established patterns: missing reconciled findings produces a clear error message suggesting `atcr reconcile` first; registry load failures propagate; skeptic invocation failures produce `unverifiable` verdicts (never crash the run).
6. Integration tests cover: CLI invocation with all flag combinations, MCP handler invocation, `--verify` chaining, missing-input error path, and idempotent re-runs.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`_

## Technical Considerations

- **Implementation Notes:**
  - **`cmd/atcr/verify.go`:** New file defining `verifyCmd` as a `cobra.Command`. The command accepts an optional positional argument (review directory path or review ID, following the `atcr reconcile` pattern). Flags: `--fresh` (bool, default false), `--thorough` (bool, default false), `--min-severity` (string, default "MEDIUM"). The `RunE` function: (1) resolves the review directory from the positional arg, (2) loads the registry via `registry.Load(registryPath)`, (3) loads reconciled findings via `reconcile.ReadReconciledFindings(reviewDir)`, (4) constructs `verify.Options{Fresh, Thorough, MinSeverity}`, (5) calls `verify.Verify(findings, reg, opts)`, (6) calls `result.Emit(reviewDir)` to write all artifacts, (7) prints a summary to stdout (verdict counts, gate status).
  - **`cmd/atcr/main.go`:** Add `rootCmd.AddCommand(verifyCmd)` at the `AddCommand` call (line 97).
  - **`cmd/atcr/review.go`:** Add `--verify` bool flag. When set, after the review and reconcile stages complete, call the same verify logic as `atcr verify`. `--verify` implies `--reconcile` — if `--verify` is set but `--reconcile` is not explicitly set, force reconcile on. The chaining order is: review → reconcile → verify, with each stage's output feeding the next.
  - **`internal/mcp/handlers.go`:** Add `handleVerify` following the `handleReconcile` pattern (line 159). The handler: (1) parses `VerifyArgs` from the MCP request (fields: `Path string`, `Fresh bool`, `Thorough bool`, `MinSeverity string`, `RegistryPath string`), (2) loads registry and reconciled findings, (3) calls `verify.Verify`, (4) emits artifacts, (5) returns `*VerifyResult` with `VerdictCounts` (confirmed/refuted/unverifiable) and `GateStatus` (pass/fail based on `--fail-on` threshold if provided). The `VerifyResult` struct is defined locally in this file.
  - **`internal/mcp/server.go`:** Register `atcr_verify` in `buildServer()` (line 57) via `registerTool(r, &mcpsdk.Tool{Name: ToolVerify, Description: "Run adversarial verification on reconciled findings"}, e.handleVerify)`. Add the `ToolVerify` constant alongside existing tool name constants.
  - **Gate integration:** The CLI and MCP entry points both support `--fail-on` and `--require-verified` flags (following existing patterns from `atcr review`). These flags are passed through to the gate counter which was updated in Story 3 to exclude refuted findings and support `--require-verified`. The MCP handler accepts these as optional parameters in `VerifyArgs`.
  - **Output format:** The CLI `atcr verify` prints a human-readable summary to stdout: verdict counts (confirmed/refuted/unverifiable), number of findings processed, duration. The MCP tool returns a structured `VerifyResult` JSON object. Both follow the output conventions of `atcr reconcile`.
- **Integration Points:**
  - `cmd/atcr/main.go:97` — `AddCommand` registration point.
  - `cmd/atcr/review.go` — `--verify` flag addition and chaining logic.
  - `cmd/atcr/verify.go` — new file, Cobra command definition.
  - `internal/mcp/server.go:57` — `buildServer()` tool registration.
  - `internal/mcp/handlers.go` — `handleVerify` handler, `VerifyArgs`/`VerifyResult` structs, `ToolVerify` constant.
  - `internal/verify.Verify` (Story 2/3) — the backend pipeline function called by all three entry points.
  - `internal/reconcile.ReadReconciledFindings` (`internal/reconcile/emit.go:145`) — input loader.
  - `internal/reconcile/gate.go:57` — `CountAtOrAbove` (updated in Story 3 to exclude refuted findings).
  - `internal/mcp/handlers.go:339` — `failingFindings` (updated in Story 3 for MCP gate path).
- **Data Requirements:**
  - No new schema changes. All artifacts (`verification.json`, `findings.json`, `manifest.json`, `summary.json`) are written by Story 3's emission functions. This story only triggers those writes.
  - `VerifyArgs` MCP input schema: `path` (string, required), `fresh` (bool, optional, default false), `thorough` (bool, optional, default false), `minSeverity` (string, optional, default "MEDIUM"), `registryPath` (string, optional), `failOn` (string, optional), `requireVerified` (bool, optional).
  - `VerifyResult` MCP output schema: `verdictCounts` (object: confirmed, refuted, unverifiable integers), `findingsProcessed` (int), `durationMs` (int64), `gateStatus` (object: pass/fail, failingCount — only present if `failOn` provided).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `--verify` flag on `atcr review` conflicts with existing `--reconcile` flag behavior | Medium — unexpected behavior when both flags are used | `--verify` implies `--reconcile` — document this clearly. If both are set, `--verify` takes precedence for the reconcile step (no double reconcile). Unit test all flag combinations. |
| MCP tool registration conflicts with existing tool names | Medium — server startup failure | Use the existing `registerTool` wrapper which detects duplicates and fails fast. Add `ToolVerify` constant alongside existing constants. Test server startup with all tools registered. |
| `atcr verify` run before `atcr reconcile` produces confusing error | Medium — poor user experience | Check for `reconciled/findings.json` before calling `verify.Verify`. If missing, return a clear error: "no reconciled findings found — run `atcr reconcile` first". Follow the pattern used by `atcr reconcile` when review output is missing. |
| CLI and MCP entry points diverge in behavior (e.g., different flag handling) | Medium — inconsistent results | Both entry points call the same `verify.Verify` function with the same `Options` struct. The only difference is argument parsing (CLI flags vs MCP JSON). Integration tests verify identical artifacts for the same input via both paths. |
| `handleVerify` MCP handler does not propagate gate status correctly | Medium — CI gates cannot use MCP path | The handler calls the same `CountAtOrAbove` / `failingFindings` functions (updated in Story 3) as the CLI path. Integration test verifies gate status matches between CLI and MCP for the same input. |
| Review directory path resolution differs between CLI and MCP | Low — artifacts written to wrong location | Both paths use the same `resolveReviewDir` helper (existing in `cmd/atcr/review.go` or equivalent). The MCP handler accepts an explicit `path` parameter. Unit test path resolution for both. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Draft - Awaiting Acceptance Criteria
