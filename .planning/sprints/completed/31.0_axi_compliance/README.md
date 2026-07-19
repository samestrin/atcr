# Sprint 31.0: AXI Agent eXperience Interface Compliance

**Type:** ✨ Feature
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days across 5 phases
**Execution Mode:** Gated 🚧 (adversarial review + phase-boundary gates)
**TDD Mode:** Moderate 🔄

## Overview

Implements a first-class `--axi` (Agent eXperience Interface) output mode for `atcr review` and `atcr report`, replacing human-ergonomic Markdown/ANSI/table output with a token-dense TOON/JSON payload, a reconciled 0/1/2/3 exit-code contract, deterministic pagination/truncation (500-line default cap, `ATCR_AXI_MAX_LINES` override), and strict stdout/stderr isolation — so `atcr` is safely composable as a subprocess inside autonomous agentic workflows.

**Source plan:** [plan/plan.md](plan/plan.md) · **Original request:** [plan/original-requirements.md](plan/original-requirements.md) · **Sprint design:** [plan/sprint-design.md](plan/sprint-design.md)

## Timeline

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Foundation — AXI Schema & Render Dispatch | 2.5 days |
| 2 | Core Integration — Review/Resume Gating, MCP Exclusion, Exit-Code Reconciliation | 2.5 days |
| 3 | Pagination & Truncation Guarantees | 2 days |
| 4 | Stderr Isolation & Escape-Sequence Guarantee | 2 days |
| 5 | Documentation & Validation | 1 day |

## Expected Outcomes

- `FormatAXI` render dispatch in `internal/report`, reconciled with `atcr-findings/v1` and TOON tabular-array/pipe-delimiter conventions
- `atcr review --axi`/`atcr resume --axi` gate all human-oriented `cmd.OutOrStdout()` writes behind shared context-mode propagation
- `FormatAXI` explicitly excluded from the MCP `atcr_report` format enum, documented inline
- AXI mode preserves the existing 0/1/2/3 exit-code contract; new AXI-introduced errors classify into it; reconciliation documented in `docs/ci-integration.md` and `cmd/atcr/main.go`
- Shared `internal/report/pagination.go` line-cap wrapper (default 500, `ATCR_AXI_MAX_LINES` override, `truncated` flag + true total count) applied uniformly to both AXI code paths
- ANSI/OSC escape-sequence pinning test and non-`--axi` regression suite
- `docs/agentic-consumption.md` published with a worked autonomous-sweeper example, cross-referenced from `docs/ci-integration.md`

## Risk Summary (top 3)

1. **Exit-code contract drift** — silently diverging from the existing 0/1/2/3 contract would break downstream CI scripts. Mitigated by Story 2's explicit reconciliation (no new exit code, both `docs/ci-integration.md` and `main.go` comment updated).
2. **`atcr review`'s live-output path missed** — `--axi` covering only `atcr report`'s renderer would leave the primary agent invocation path (`atcr review`) polluted with human text. Mitigated by AC 01-03/01-04/04-01 enumerating every `cmd.OutOrStdout()` call site by line number.
3. **Truncation logic duplicated per-command** — `atcr review --axi` and `atcr report --axi` diverging in cap behavior. Mitigated by AC 03-04's single shared `internal/report/pagination.go` implementation, verified by a cross-command parity test.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — full phase-by-phase TDD task breakdown
- [metadata.md](metadata.md) — tracking metadata
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest
- [plan/](plan/) — copied source plan (user-stories/, acceptance-criteria/, documentation/, sprint-design.md, original-requirements.md)

---

**Next:** `/refine-sprint @.planning/sprints/active/31.0_axi_compliance/`
