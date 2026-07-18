# Sprint Design: AXI (Agent eXperience Interface) Compliance

**Created:** July 18, 2026 10:07:42AM
**Plan:** [31.0: AXI Agent eXperience Interface Compliance](plan.md)
**Plan Type:** Feature
**Status:** Design Complete

---

## Original User Request

> Implement an `--axi` (Agent eXperience Interface) output mode to make `atcr` a foundational tool for *other* autonomous agents. By stripping human-ergonomic formatting (Markdown, ANSI colors, ASCII tables) in favor of token-dense TOON/JSON output, predictable pagination, and strict exit codes, `atcr` becomes safely composable in agentic workflows (e.g., an autonomous sweeper invoking `atcr` to review its own generated code).

**Referenced Resources:**
- [CLI Command & Output Control Patterns (Cobra)](documentation/cli-command-patterns.md) — Cobra's `PersistentFlags()`, `PersistentPreRunE` context injection, `cmd.OutOrStdout()`/`SetErr()` output routing, and the single-point `exitCode()` resolution pattern already used by atcr; the mechanism `--axi` mode threading must reuse.
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)](documentation/exit-code-cli-mcp-precedent.md) — documents the existing 0/1/2/3 contract and the `atcr verify` precedent for folding a new capability into it without a second scheme; also the `report.FormatList()` → MCP enum auto-propagation precedent (Sprint 25.0/TD-003).
- [Existing Agent-Facing Format & Output-Safety Contracts](documentation/agentic-format-precedents.md) — the `atcr-findings/v1` versioned wire format, the `truncated`/`files_dropped` deterministic-truncation naming precedent, the `sanitizeDisplay` control-character idiom (and its `osc8()` counterexample), and the golden-file byte-stability test gate every format must register with.
- [MCP Tool Schema & Format-Enum Propagation](documentation/mcp-schema-format-propagation.md) — the `jsonschema-go`/MCP SDK reflection chain that auto-propagates a new `FormatAXI` constant into the `atcr_report` MCP tool's enum/description unless explicitly excluded.
- [AXI Design Principles (axi.md) — the Epic's Reference Source](documentation/axi-design-principles.md) — the 10-principle source the epic cites; Principle 1 mandates TOON by name, Principle 5 requires a definitive empty state (in tension with TOON's empty-object rule), Principle 6 introduces a *third* exit-code contract to reconcile, Principle 2/4 create schema-width and aggregate tensions with the 8-9-column findings shape.
- [TOON Format Reference (Token Optimized Object Notation)](documentation/toon-format-reference.md) — the normative tabular-array (`key[N]{...}:`), alternative-delimiter (pipe), quoting, and escape-sequence rules a TOON encoder must implement.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** AXI Compliance
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 5
**Pattern:** Foundation → Core Integration → Pagination/Truncation → Stderr Isolation → Documentation & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
CLI output format-enum dispatch pattern Go
context propagation via Cobra PersistentPreRunE
deterministic never-silent truncation pattern
exit-code contract reconciliation CI scripts
stdout stderr isolation for agent-facing tooling
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - Introduces a genuinely new cross-cutting concept (an axi-mode value threaded through command context, alongside the existing logger/telemetry client) and a new pagination/truncation layer (`internal/report/pagination.go`), but both build directly on established atcr patterns (format-enum dispatch, `PersistentPreRunE` context injection) rather than overhauling them.
- **Integration:** 2/3 - Touches 3+ internal integration points that must stay in sync: `internal/report` (renderer + pagination), `cmd/atcr` (context injection, `review.go`/`resume.go` gating, `main.go` flag/env registration), and `internal/mcp` (format-enum propagation decision), plus a documentation cross-reference into `docs/ci-integration.md`.
- **Story/Task & Test:** 3/3 - 5 user stories, 18 acceptance criteria, spanning golden-file unit tests, table-driven env-var/exit-code tests, cobra-level integration tests capturing stdout, and an escape-sequence pinning test — test-planning-matrix.md rates 4 of the 18 ACs "High" complexity.
- **Risk/Unknowns:** 2/3 - Several concrete design decisions remain open for this sprint to resolve (TOON vs. compact JSON, full-width vs. subset schema fields, MCP include/exclude for `FormatAXI`, whether `--axi`-mode structured errors ride stdout or stderr) — each is well-scoped by the plan's documentation but still requires a judgment call recorded in code/docs, not a known quantity.

**Time Formula:** COMPLEX (7-9/12) → 8-12 day range; sum of phase estimates below
**Calculation:** Phase 1 (2.5d) + Phase 2 (2.5d) + Phase 3 (2d) + Phase 4 (2d) + Phase 5 (1d) = 10 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strong — score is 9/12, strong threshold is ≥10)
**Suggested command:** `/create-sprint @.planning/plans/active/31.0_axi_compliance/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Foundation — AXI Schema & Render Dispatch (2.5 days)
**Items:** Story 1 (AC 01-01, 01-02)
**Focus:** Design and land the `FormatAXI` schema decision (TOON vs. compact JSON, full-width vs. subset fields, pipe-delimiter convergence with `atcr-findings/v1`) and implement the `internal/report` render-dispatch extension with its golden-file fixture. This is the foundational payload every later phase wraps (pagination), gates (stdout isolation), or documents (Story 5) — it must land first and be schema-stable before Phases 2-4 build on it.

### Phase 2: Core Integration — Review/Resume Gating, MCP Decision, Exit-Code Reconciliation (2.5 days)
**Items:** Story 1 (AC 01-03, 01-04, 01-05), Story 2 (AC 02-01, 02-02, 02-03)
**Focus:** Thread the axi-mode value through `PersistentPreRunE` context injection (mirroring the logger/telemetry precedent); gate `atcr review`'s and `atcr resume`'s human-oriented `cmd.OutOrStdout()` writes behind it; decide and document MCP `FormatAXI` enum propagation; and reconcile/document the exit-code contract (confirming `--axi` introduces no second exit-code mechanism and no repurposing of code `2`). These two threads (output gating, exit-code reconciliation) are independent of each other but both depend on Phase 1's flag/context plumbing existing.

### Phase 3: Pagination & Truncation Guarantees (2 days)
**Items:** Story 3 (AC 03-01, 03-02, 03-03, 03-04)
**Focus:** Implement the shared `internal/report/pagination.go` line-cap wrapper (default 500 lines, deterministic row-boundary truncation), the `truncated` flag reusing the `internal/fanout/status.go` naming precedent, the `ATCR_AXI_MAX_LINES` fail-open env-var override, and wire the same wrapper into both `atcr review --axi`'s live path and `atcr report --axi`'s batch path so neither command reimplements truncation independently.

### Phase 4: Stderr Isolation & Escape-Sequence Guarantee (2 days)
**Items:** Story 4 (AC 04-01, 04-02, 04-03)
**Focus:** Audit and gate every named `cmd.OutOrStdout()` call site in `review.go`/`resume.go` (including both `writeReviewSummary` callers and the `AllComplete()` short-circuit branch) behind axi mode; add the ANSI/OSC escape-sequence pinning test (using `osc8()` as the known-bad regression fixture); and verify non-`--axi` invocations are byte-identical to pre-sprint behavior.

### Phase 5: Documentation & Validation (1 day)
**Items:** Story 5 (AC 05-01, 05-02, 05-03) + cumulative regression sweep
**Focus:** Publish `docs/agentic-consumption.md` (invocation, reconciled exit codes, pagination, stderr isolation, worked sweeper example) verified against Phases 1-4's actual shipped flag/env-var/field names — not draft language — plus the additive cross-reference from `docs/ci-integration.md`. Close with a full non-`--axi` regression pass and golden-file re-verification across all formats.

---

## Work Decomposition

### Story 1 — `--axi` Token-Dense Output Mode for `atcr review` and `atcr report` (Priority: High, Effort: L)
Testable elements:
- [01-01](acceptance-criteria/01-01-axi-format-render-dispatch.md) `FormatAXI` render dispatch in `internal/report/render.go` + golden fixture — Unit
- [01-02](acceptance-criteria/01-02-axi-schema-toon-findings-v1-compatibility.md) Schema reconciled with `atcr-findings/v1` and TOON conventions — Unit
- [01-03](acceptance-criteria/01-03-review-axi-mode-output-gating.md) `atcr review --axi` gates human-oriented live output — Integration
- [01-04](acceptance-criteria/01-04-resume-context-axi-mode-propagation.md) `atcr resume --axi` parity via shared context-mode propagation — Integration
- [01-05](acceptance-criteria/01-05-mcp-axi-format-enum-decision.md) MCP format-enum propagation decision for `FormatAXI` — Unit/Integration

### Story 2 — Reconcile and Document the AXI Exit-Code Contract (Priority: High, Effort: S)
Testable elements:
- [02-01](acceptance-criteria/02-01-axi-exit-code-parity.md) AXI mode preserves existing exit-code semantics (0/1/2/3) — Unit/Integration
- [02-02](acceptance-criteria/02-02-new-axi-error-classification.md) New AXI-introduced errors classify into the existing contract — Unit
- [02-03](acceptance-criteria/02-03-document-exit-code-reconciliation.md) Document the exit-code reconciliation decision — Manual

### Story 3 — AXI Pagination and Truncation Guarantees (Priority: High, Effort: M, depends on Story 1)
Testable elements:
- [03-01](acceptance-criteria/03-01-default-line-cap-deterministic-truncation.md) Default 500-line cap with deterministic truncation — Unit
- [03-02](acceptance-criteria/03-02-truncated-flag-and-true-total-count.md) `truncated` flag with preserved true total count — Unit
- [03-03](acceptance-criteria/03-03-axi-max-lines-env-override.md) `ATCR_AXI_MAX_LINES` env override with fail-open parsing — Unit/Integration
- [03-04](acceptance-criteria/03-04-shared-truncation-wrapper-across-commands.md) Shared truncation wrapper applied uniformly across both AXI code paths — Integration

### Story 4 — AXI Stderr Isolation and Escape-Sequence Guarantee (Priority: High, Effort: M, depends on Story 1)
Testable elements:
- [04-01](acceptance-criteria/04-01-review-resume-stdout-gating.md) Gate human-oriented stdout writes in `review.go`/`resume.go` under AXI mode — Unit/Integration
- [04-02](acceptance-criteria/04-02-escape-sequence-pinning-test.md) Pinning test guarantees no ANSI/OSC escape sequences reach `--axi` stdout — Unit
- [04-03](acceptance-criteria/04-03-non-axi-regression-protection.md) Non-AXI `review`/`resume` behavior remains unchanged — Unit/Integration

### Story 5 — Publish the Agentic Consumption Orchestration Guide (Priority: Medium, Effort: S, depends on Stories 1-4)
Testable elements:
- [05-01](acceptance-criteria/05-01-agentic-consumption-doc-content.md) Publish core content of `docs/agentic-consumption.md` — Integration
- [05-02](acceptance-criteria/05-02-worked-orchestration-example.md) Worked orchestration example (autonomous sweeper scenario) — Integration
- [05-03](acceptance-criteria/05-03-ci-integration-cross-reference.md) Additive cross-reference from `docs/ci-integration.md` — Integration

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated with source (`*_test.go` alongside implementation files in the same package), matching all 368 existing Go test files in the repo.
**Test File Placement Examples:**
- `internal/report/render_test.go` — extend `goldenCases` table with a new `{"axi", FormatAXI, "report.axi", nil}` entry; new fixture at `internal/report/testdata/report.axi`
- `internal/report/pagination_test.go` (new) — line-cap/truncation/env-override table-driven tests
- `cmd/atcr/main_test.go` — extend exit-code table-driven tests for `--axi` invocations; add `axiMaxLinesFromEnv()` unit tests
- `cmd/atcr/review_test.go` / `cmd/atcr/resume_test.go` — extend with captured-stdout assertions for both `--axi` and non-`--axi` paths
- `cmd/atcr/models_test.go` (or new `cmd/atcr/axi_escape_test.go`) — ANSI/OSC escape-sequence pinning test in the `TestDriftLine_StripsControlChars` style
- `internal/mcp/tools_test.go` — extend schema-enum/description drift assertions for the `FormatAXI` include/exclude decision

**Unit/Integration/E2E:** Per test-planning-matrix.md: 9 unit ACs, 5 integration ACs, 0 dedicated E2E ACs (cobra-level command execution against captured stdout substitutes for E2E at this scope), 4 manual/documentation ACs. Coverage target: maintain the project's existing 80% baseline (`coverage_baseline: 80` in config.yaml); golden-file byte-comparison for the new format; `t.Setenv` for isolated `ATCR_AXI_MAX_LINES` test cases.

**Test Environment Status:**
- Framework: `go test` (standard library `testing`) + `testify` (`assert`/`require`) — confirmed present via 368 existing `*_test.go` files
- Execution: `go test ./...` (project standard command, config.yaml)
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (project standard command, config.yaml)

---

## Architecture

**Primitives:**
- `FormatAXI` — new format-enum constant in `internal/report`, alongside `FormatMarkdown`/`FormatJSON`/`FormatChecklist`/`FormatSarif`
- AXI-mode context value — a boolean/mode value threaded via `PersistentPreRunE` into `cmd.Context()`, read through a `FromContext`-style accessor (mirrors `log.FromContext`/`telemetry.FromContext`)
- `internal/report/pagination.go` (new file) — the shared line-cap wrapper/post-processor: takes rendered AXI output, returns capped output + `truncated bool` + preserved true element count

**Module Boundaries:**
- `internal/report` — owns the `FormatAXI` renderer (schema encoding) and the pagination/truncation wrapper; both are pure functions over already-collected findings data, no new I/O
- `cmd/atcr` — owns context injection (`main.go`), flag/env registration (`--axi` persistent flag, `axiMaxLinesFromEnv()`), and gating of existing human-oriented stdout writes in `review.go`/`resume.go`/`review_summary.go`
- `internal/mcp` — owns the deliberate include/exclude decision for `FormatAXI` propagating into the `atcr_report` tool's JSON Schema enum

**External Dependencies:** None new. Go standard library only (`bytes`, `strings`, `unicode/utf8`, `strconv`, `os`, `context`) — consistent with plan.md's "hand-rolled formatters over third-party dependencies" stance; the third-party Go TOON implementation referenced in `documentation/toon-format-reference.md` is evaluated, not adopted by default.

**Replaceability:** The format-enum dispatch pattern (`Render()`'s switch) already provides a clean swap/extension point — `FormatAXI` is added the same way `FormatSarif` was in Sprint 25.0. The pagination wrapper is a single shared function consumed by two call sites (`cmd/atcr/report.go`, `cmd/atcr/review.go`), so it can be modified or replaced without touching either command's control flow.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| Escape-sequence injection via untrusted finding text | AXI payload encoding (`renderAXI`), review-summary payload | A reviewer-agent-controlled or repo-content-controlled string (finding `Problem`/`Fix`/`Evidence` fields) contains a raw `\x1b[`/`\x1b]` CSI/OSC sequence | TOON's 5-escape-only rule structurally rejects raw control bytes (must quote/escape); pinning test (AC 04-02) with `osc8()` as a positive-control fixture; reuse `sanitizeDisplay`-style stripping if the encoder path doesn't already reject them |
| `ATCR_AXI_MAX_LINES` env-var parsing | `axiMaxLinesFromEnv()` in `cmd/atcr/main.go` | Malformed, blank, zero, negative, or extremely large values | Fail-open to default 500 with exactly one stderr warning, never a fatal error/panic; `strconv.Atoi` bounds-checks overflow |
| MCP format-enum exposure | `internal/mcp/tools.go`/`handlers.go` | `FormatAXI` auto-propagates into `atcr_report`'s JSON Schema enum via `report.FormatList()` unless explicitly filtered | Explicit include/exclude decision recorded in code comments (AC 01-05); double-layer defense (JSON Schema enum + `report.ValidFormat()` backstop) unchanged |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| AXI payload rendering | Up to thousands of findings per review/report run | <200ms for 1,000 findings (matches existing `renderJSON`/`renderMarkdown` performance class) | Pure in-memory single-pass encoding, no re-rendering |
| Line-cap truncation | Payloads from 0 to tens of thousands of lines | O(n) single pass, negligible overhead vs. untruncated rendering | Truncate at a fixed row-boundary cut point; no backtracking or re-parsing |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Zero-findings payload | `atcr review --axi`/`atcr report --axi` against a clean result | Well-formed explicit empty-state payload (e.g. `findings[0]:` + run metadata), never zero-byte stdout — axi.md Principle 5 overrides TOON's empty-object default |
| Truncation boundary | Payload exactly at 500 lines vs. 501 lines | Exactly-at-cap is NOT truncated (inclusive boundary); one-over triggers truncation at exactly the cap |
| Malformed `ATCR_AXI_MAX_LINES` | Non-numeric, blank, zero, negative | Fail open to default 500, exactly one stderr warning, exit code unaffected (never `usageError`) |
| Interrupted review (SIGINT/SIGTERM) under `--axi` | `reportInterrupt` path fires mid-fan-out | Interrupt-path output is also axi-gated — must not become an unguarded escape hatch |
| `AllComplete()` resume short-circuit | `atcr resume --axi` where all agents already completed | The early-return branch's human line is also axi-gated, not missed because it bypasses the main summary call |
| Mixed `--axi`/non-`--axi` across review→resume | Review started without `--axi`, resumed with `--axi` (or reverse) | Each invocation's own flags govern its own stdout independently; axi mode is not persisted to the on-disk manifest |

### Defensive Measures Required

- **Input Validation:** Free-text finding fields (`Problem`/`Fix`/`Evidence`/`Verification.Notes`) quoted/escaped per TOON's must-quote rules before encoding; `ATCR_AXI_MAX_LINES` validated with fail-open semantics, never fatal.
- **Error Handling:** No new exit-code mechanism — every new AXI error source flows through the existing `usageError()`/`authError()`/unwrapped-`exitFailure` classification via the single `exitCode(err)` dispatch point (`cmd/atcr/main.go:156`).
- **Logging/Audit:** Diagnostic/progress output remains stderr-only via existing `internal/log`/`setupLogger()`; no change to that invariant, only to what is additionally gated on the stdout side.
- **Rate Limiting:** N/A — local CLI rendering, no external service calls introduced.
- **Graceful Degradation:** Truncation is never a hard failure — a payload exceeding the cap degrades to a capped-but-flagged output, never an error or crash.

---

## Risks

**Technical:**
- Risk: Silently changing the existing exit-code contract breaks downstream CI scripts. → Mitigation: Story 2 treats reconciliation as an explicit, documented decision (both `docs/ci-integration.md` and the `main.go` comment block), keeping 0/1/2/3 stable; no new exit code introduced.
- Risk: `--axi` covers only `atcr report`'s renderer and misses `atcr review`'s live-output path, leaving the primary agent invocation path polluted. → Mitigation: AC 01-03/01-04/04-01 explicitly enumerate every `cmd.OutOrStdout()` call site in `review.go`/`resume.go` by line number.
- Risk: Inventing a TOON/axi schema that duplicates or conflicts with `atcr-findings/v1`, fragmenting the machine-format surface. → Mitigation: AC 01-02 requires explicit reconciliation (pipe-delimiter convergence, superset field-mapping) before the schema is finalized.
- Risk: Truncation logic duplicated per-command, causing `atcr review --axi` and `atcr report --axi` to diverge in cap behavior. → Mitigation: AC 03-04 requires one shared `internal/report/pagination.go` implementation consumed by both call sites, verified by a cross-command parity test.

**TDD-Specific:**
- Risk: New `FormatAXI` golden fixture accidentally perturbs existing golden files (md/json/checklist/sarif). → Mitigation: add the new `goldenCases` entry in isolation; run the full `TestRender_GoldenFiles` suite before/after to confirm no existing fixture's bytes changed.
- Risk: MCP schema-enum test (`internal/mcp/tools_test.go`) starts failing once `FormatAXI` is added to `report.FormatList()`, if the include/exclude decision (AC 01-05) isn't made explicitly first. → Mitigation: sequence AC 01-05's decision before or alongside AC 01-01's enum addition, and update `tools_test.go`'s expected enum/description in the same change.
- Risk: Non-`--axi` regression in `review.go`/`resume.go` after gating changes land (Phase 4) without a keep-green pass. → Mitigation: AC 04-03 is a dedicated regression-protection story sequenced immediately after AC 04-01's gating changes, re-running all pre-existing stdout-content assertions unmodified.

---

**Next:** `/create-sprint @.planning/plans/active/31.0_axi_compliance/ --gated`
