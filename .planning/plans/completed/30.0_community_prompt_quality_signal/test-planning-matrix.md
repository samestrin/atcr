# Test Planning Matrix

**Generated:** 2026-07-17
**Plan:** 30.0_community_prompt_quality_signal
**Total ACs:** 18

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters | 5 | 5 | 1 | 0 | High |
| 02: Independent Opt-In Gate for Quality-Signal Transmission | 3 | 3 | 1 | 0 | Low |
| 03: Local `--preview` Surface for the Outbound Quality-Signal Payload | 3 | 2 | 1 | 0 | Low |
| 04: Maintainer-Facing Prompt Quality Report | 4 | 4 | 0 | 1 | Medium |
| 05: Document the Quality-Signal Telemetry Contract | 3 | 0 | 0 | 0 | Low |

_Story 05 is documentation-only — its 3 ACs are verified by manual doc-accuracy review + doc-lint, not automated test types._

---

## Detailed AC List

### Story 01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters (Effort: L)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Per-(Persona, Model) Dismissed/Confirmed Aggregation | Unit | Medium | P1 |
| 01-02 | `Model` Field Schema Bump and Attribution-Incomplete Exclusion | Unit + Integration | High | P1 |
| 01-03 | Multi-Persona `Reviewers` Attribution Rule | Unit | Medium | P1 |
| 01-04 | Append-Only Record Fold by ID Before Aggregation | Unit | High | P1 |
| 01-05 | Allowlisted `quality_signal.go` Payload Type with Locking Regression Test | Unit | Medium | P1 |

### Story 02: Independent Opt-In Gate for Quality-Signal Transmission (Effort: S)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Quality Signal Resolves Disabled With No Env Var and No Persisted Config | Unit | Low | P1 |
| 02-02 | Pure Four-Combination Gate, Independent of `telemetryGate`/`resolveSyncCloud` | Unit | Medium | P1 |
| 02-03 | `atcr config set quality_signal <bool>` Persists Atomically, Fails Safe on Corruption | Unit + Integration | Medium | P1 |

### Story 03: Local `--preview` Surface for the Outbound Quality-Signal Payload (Effort: S)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | `--preview` Flag Renders the Exact Outbound JSON Payload | Integration | Medium | P2 |
| 03-02 | `--preview` Never Sends — No Network Call, Independent of Opt-In Gate State | Unit | Medium | P2 |
| 03-03 | Regression Test Locks `--preview` Output to the Real Send's Marshal Path | Unit | Low | P2 |

### Story 04: Maintainer-Facing Prompt Quality Report (Effort: M)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Ranked Per-Persona+Model Quality Report Rendering | Unit | Medium | P2 |
| 04-02 | Content-Free Privacy Guarantee on the Report Render Path | Unit | Medium | P2 |
| 04-03 | Empty-Aggregation "No Data" State | Unit + E2E-lite | Low | P2 |
| 04-04 | Distinct Subcommand Registration Alongside Existing `atcr report` | Unit | Low | P2 |

### Story 05: Document the Quality-Signal Telemetry Contract (Effort: S)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Document the Quality-Signal Payload's Exact Field Allowlist | Manual + doc-lint | Low | P3 |
| 05-02 | Document the Independent Opt-In Mechanism and `--preview` Behavior | Manual + doc-lint | Low | P3 |
| 05-03 | State the Absolute No-Code/No-Finding-Content Guarantee and Restate the Persona-Hash Caveat | Manual | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 14 ACs require unit tests
- **Integration Tests:** 3 ACs require integration tests (each paired with a unit test on the same AC)
- **E2E Tests:** 1 AC includes an E2E-lite CLI exit-code assertion (04-03)
- **Manual/Doc Review:** 3 ACs (Story 05) are documentation-only, verified by manual accuracy review and doc-lint rather than automated tests
- **High Complexity:** 2 ACs marked high complexity (01-02, 01-04) — both concentrated in Story 01's schema-bump and append-only-fold logic, the epic's structurally riskiest area per the codebase discovery's flagged integration gap (no stored model attribution on dismissal records)
