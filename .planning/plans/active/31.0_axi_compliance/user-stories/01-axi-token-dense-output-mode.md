# User Story 1: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`

**Plan:** [31.0: AXI Agent eXperience Interface Compliance](../plan.md)

## User Story

**As an** autonomous coding agent operator running `atcr` as a subprocess in an agentic pipeline
**I want** an `--axi` flag on `atcr review` and `atcr report` that emits a clean, machine-readable TOON/compact-JSON payload on stdout with no ANSI color codes, Markdown tables, or visual dividers
**So that** I can parse `atcr`'s output deterministically without wasting context-window tokens on human-ergonomic formatting or writing brittle regex/Markdown scrapers

## Story Context

- **Background:** `atcr` currently renders findings for humans: `internal/report/render.go` dispatches to `FormatMarkdown`, `FormatJSON`, `FormatChecklist`, and `FormatSarif` for `atcr report`, while `atcr review`'s live progress/summary output (`cmd/atcr/review.go`, `cmd/atcr/resume.go`) writes ANSI-colored, human-oriented text via `cmd.OutOrStdout()`. Neither path currently produces a token-dense payload suitable for an LLM agent to parse cheaply. A related but distinct agent-facing wire format already exists (`# atcr-findings/v1`, documented in `docs/findings-format.md`) — pipe-delimited and versioned — which any new format should stay compatible in spirit with, to avoid fragmenting the "machine format" surface.
- **Assumptions:** `internal/report`'s `Render()` format-enum dispatch pattern (as used for SARIF/checklist) is the correct extension point for `atcr report`; `atcr review`'s live output is a separate code path that must be gated independently behind the same `--axi` flag/mode value; the underlying finding/result data model already contains everything needed to populate a TOON/JSON payload (no new data collection required).
- **Constraints:** Must not alter existing output formats or their golden-file byte-stability tests (`internal/report/render_test.go` `goldenCases`) when `--axi` is not passed. Must not silently repurpose the existing exit-code contract (0=clean/1=gate-failure/2=usage-error/3=auth-error) — that reconciliation is explicitly out of scope for this story (see Story 2). Must follow the TOON spec's tabular-array encoding conventions (see `documentation/toon-format-reference.md`) and stay interoperable with the existing `atcr-findings/v1` precedent rather than inventing an unrelated schema. `--axi` propagating into MCP's `atcr_report` format enum via `report.FormatList()` is acceptable unless explicitly excluded.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `atcr review --axi` and `atcr report --axi` both produce stdout output containing zero ANSI escape sequences, zero Markdown table/heading syntax, and zero decorative dividers, encoded as TOON or compact JSON.
- **Measurable:** A new `FormatAXI` golden-fixture test in `internal/report/render_test.go` passes with byte-stable output; a scripted check (e.g. `grep -P '\x1b\['`) against `--axi` stdout for both commands returns no matches in CI.
- **Achievable:** Extends the existing `internal/report` format-enum dispatch pattern (precedent: SARIF/checklist additions) and threads an axi-mode flag through the same command-context injection point already used for the logger/telemetry client in `PersistentPreRunE`.
- **Relevant:** This is the core deliverable of Epic 31.0 — without a clean token-dense payload, none of the epic's other acceptance criteria (exit codes, pagination, stderr isolation, docs) have a payload to apply to.
- **Time-bound:** Implementable within a single sprint cycle as the first story in the plan's 5-story sequence; unblocks Stories 2-4 which layer exit-code, pagination, and stderr guarantees onto this payload.

## Acceptance Criteria Overview

1. `atcr report --axi` (or equivalent format flag value, e.g. `--format=axi`) renders findings via a new `FormatAXI` case in `internal/report/render.go`'s existing dispatch, producing TOON/compact-JSON output with no ANSI/Markdown artifacts, and ships with its own golden-file fixture.
2. `atcr review --axi` suppresses/replaces its existing human-oriented `cmd.OutOrStdout()` progress and summary writes (in `cmd/atcr/review.go` and `cmd/atcr/resume.go`) with the same token-dense payload shape, rather than leaving that code path unaddressed.
3. The new AXI schema is designed with explicit reference to the existing `atcr-findings/v1` pipe-delimited precedent (`docs/findings-format.md`) and the TOON spec's tabular-array/pipe-delimiter conventions, so the codebase does not end up with two incompatible "machine format" surfaces.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`_

## Technical Considerations

- **Implementation Notes:** Add `FormatAXI` (or similarly named constant) to `internal/report`'s format enum alongside `FormatMarkdown`/`FormatJSON`/`FormatChecklist`/`FormatSarif`, implementing the same `Render()` interface. Thread a boolean/mode value for axi-output through command context using the same injection mechanism already used for the root logger and telemetry client in `PersistentPreRunE`, so both `cmd/atcr/review.go`/`cmd/atcr/resume.go` and the `atcr report` code path can consult it. Gate `atcr review`'s existing `cmd.OutOrStdout()` human-oriented writes behind this mode.
- **Integration Points:** `internal/report/render.go` (format dispatch), `cmd/atcr/review.go` and `cmd/atcr/resume.go` (live progress/summary output), `internal/mcp/tools.go` (derives its format enum from `report.FormatList()` — confirm whether `FormatAXI` should propagate to MCP clients or be explicitly excluded), `docs/findings-format.md` (existing `atcr-findings/v1` precedent to reconcile against).
- **Data Requirements:** No new data collection — the payload is a re-encoding of the finding/result data already available to the existing renderers. Schema shape (tabular TOON array vs. compact JSON object) needs to be finalized against `documentation/toon-format-reference.md` and cross-checked for field-level compatibility with `atcr-findings/v1`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Inventing a TOON/axi schema that duplicates or conflicts with the existing `atcr-findings/v1` agent-facing format fragments the "machine format" surface for downstream consumers | Medium | Review `docs/findings-format.md` and `documentation/toon-format-reference.md` before finalizing field names/encoding; prefer converging on the pipe-delimiter option where the two formats overlap |
| `atcr review`'s live output path is missed and only `atcr report` gets AXI coverage, leaving stdout polluted for the primary agent invocation path | High | Explicitly scope both `cmd/atcr/review.go`/`cmd/atcr/resume.go` and `internal/report/render.go` in this story rather than treating `atcr report` as the sole deliverable |
| New `FormatAXI` case breaks existing golden-file byte-stability tests for other formats, or MCP tool schema propagation introduces an unintended client-facing format option | Low | Add an isolated `FormatAXI` golden fixture without touching existing golden cases; explicitly decide and document whether `FormatAXI` propagates via `report.FormatList()` into `internal/mcp/tools.go`'s enum |

---

**Created:** July 18, 2026 09:03:31AM
**Status:** Draft - Awaiting Acceptance Criteria
