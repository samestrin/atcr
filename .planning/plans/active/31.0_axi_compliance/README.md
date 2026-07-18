# Plan 31.0: AXI Agent eXperience Interface Compliance

## Overview
This plan implements a first-class `--axi` output mode for `atcr` so autonomous agents can consume review findings as token-dense, machine-readable output. It was converted from `.planning/epics/active/31.0_axi_compliance.md` via the `/init-plan` off-ramp because the epic's derived scope (6 tasks / 4 components) exceeded `/execute-epic`'s scope guard (≤6 tasks / ≤2 components). Codebase discovery found that atcr's exit-code contract and stderr-only diagnostics already exist and largely satisfy two of the epic's four acceptance criteria; remaining work centers on the new token-dense format itself, pagination, and documentation.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/31.0_axi_compliance/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/31.0_axi_compliance/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/31.0_axi_compliance/`

## Timeline & Milestones
No fixed deadline was specified in the original epic. Sequence: user stories → acceptance criteria → design sprint → sprint plan → execution, following the standard plan pipeline.

## Resource Requirements
Single Go backend component (`cmd/atcr/`, `internal/report/`, `docs/`); no additional infrastructure, external services, or new third-party dependencies identified.

## Expected Outcomes
- `atcr review --axi` and `atcr report` (via a new format) emit clean, ANSI/Markdown-free, machine-readable output on stdout.
- Stderr/stdout separation and the exit-code contract are verified and documented as stable under `--axi`.
- Large output is paginated (default 500-line cap, `ATCR_AXI_MAX_LINES` override) with an explicit truncation flag.
- A new `docs/agentic-consumption.md` explains how to orchestrate atcr inside larger agent swarms.

## Risk Summary
Primary risks are (1) inadvertently changing the existing, already-documented exit-code contract in a way that breaks downstream CI scripts, and (2) covering only `atcr report`'s renderer while missing `atcr review`'s separate live-progress output path. Both are addressed explicitly in `plan.md`'s Risk Mitigation section.

## Documentation References
- [CLI Command & Output Control Patterns (Cobra)](documentation/cli-command-patterns.md) — **[CRITICAL]**
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)](documentation/exit-code-cli-mcp-precedent.md) — **[CRITICAL]**
- [Existing Agent-Facing Format & Output-Safety Contracts](documentation/agentic-format-precedents.md) — **[IMPORTANT]**
- [MCP Tool Schema & Format-Enum Propagation](documentation/mcp-schema-format-propagation.md) — **[IMPORTANT]**
- [AXI Design Principles (axi.md) — the Epic's Reference Source](documentation/axi-design-principles.md) — **[IMPORTANT]**
- [TOON Format Reference (Token Optimized Object Notation)](documentation/toon-format-reference.md) — **[REFERENCE]**

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Sprint Design](sprint-design.md)
