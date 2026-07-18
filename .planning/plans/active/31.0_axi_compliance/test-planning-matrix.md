# Test Planning Matrix

**Generated:** 2026-07-18
**Plan:** 31.0_axi_compliance
**Total ACs:** 18

---

## Summary by Story

| Story | ACs | Unit | Integration | Manual | Avg Complexity |
|-------|-----|------|-------------|--------|-----------------|
| 01 — `--axi` Token-Dense Output Mode | 5 | 2 | 3 | 0 | High |
| 02 — Reconcile & Document Exit-Code Contract | 3 | 2 | 0 | 1 | Low-Medium |
| 03 — Pagination & Truncation Guarantees | 4 | 3 | 1 | 0 | Medium |
| 04 — Stderr Isolation & Escape-Sequence Guarantee | 3 | 2 | 1 | 0 | Medium-High |
| 05 — Publish Agentic Consumption Guide | 3 | 0 | 0 | 3 | Low |
| **Total** | **18** | **9** | **5** | **4** | — |

---

## Detailed AC List

### Story 01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | `FormatAXI` Render Dispatch for `atcr report` | Unit (golden-file) | High | P1 |
| 01-02 | AXI Schema Reconciled with `atcr-findings/v1` and TOON Conventions | Unit | Medium | P1 |
| 01-03 | `atcr review --axi` Gates Human-Oriented Live Output | Integration | High | P1 |
| 01-04 | `atcr resume --axi` Parity via Shared Context-Mode Propagation | Integration | Medium | P1 |
| 01-05 | MCP Format-Enum Propagation Decision for `FormatAXI` | Integration | Medium | P2 |

### Story 02: Reconcile and Document the AXI Exit-Code Contract

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | AXI Mode Preserves Existing Exit-Code Semantics | Unit | Medium | P1 |
| 02-02 | New AXI-Introduced Errors Classify Into the Existing Contract | Unit | Low | P2 |
| 02-03 | Document the Exit-Code Reconciliation Decision | Manual | Low | P3 |

### Story 03: AXI Pagination and Truncation Guarantees

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Default Line Cap with Deterministic Truncation | Unit | Medium | P1 |
| 03-02 | `truncated` Flag with Preserved True Total Count | Unit | Medium | P1 |
| 03-03 | `ATCR_AXI_MAX_LINES` Environment Override with Fail-Open Parsing | Unit | Medium | P2 |
| 03-04 | Shared Truncation Wrapper Applied Uniformly Across Both AXI Code Paths | Integration | High | P1 |

### Story 04: AXI Stderr Isolation and Escape-Sequence Guarantee

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Gate Human-Oriented Stdout Writes in review.go and resume.go Under AXI Mode | Unit | High | P1 |
| 04-02 | Pinning Test Guarantees No ANSI/OSC Escape Sequences Reach `--axi` Stdout | Unit | Medium | P1 |
| 04-03 | Non-AXI `review`/`resume` Behavior Remains Unchanged | Integration | Medium | P2 |

### Story 05: Publish the Agentic Consumption Orchestration Guide

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Publish Core Content of `docs/agentic-consumption.md` | Manual | Low | P2 |
| 05-02 | Worked Orchestration Example (Autonomous Sweeper Scenario) | Manual | Low | P3 |
| 05-03 | Additive Cross-Reference from `docs/ci-integration.md` | Manual | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 9 ACs require unit tests (golden-file renderer pinning, exit-code table-driven tests, pagination/truncation logic, env-var parsing, escape-sequence pinning).
- **Integration Tests:** 5 ACs require integration tests (cobra command execution against captured stdout, cross-command truncation-wrapper parity, MCP `handleReport` exercise, non-axi regression coverage).
- **E2E Tests:** 0 ACs require dedicated E2E tests — the plan's integration-level cobra command execution tests substitute for E2E coverage at this scope.
- **Manual/Documentation Review:** 4 ACs are documentation-only (exit-code reconciliation write-up, agentic-consumption guide content, worked orchestration example, CI-integration cross-reference); several note an optional lightweight grep/link-check CI guard but do not require an automated test framework.
- **High Complexity:** 4 ACs marked high complexity (01-01 new format golden-fixture dispatch, 01-03 gating `atcr review`'s live stdout writes, 03-04 shared truncation wrapper across two command entry points, 04-01 gating all stdout write sites) — these carry the most implementation risk and should be sequenced early in the sprint.
