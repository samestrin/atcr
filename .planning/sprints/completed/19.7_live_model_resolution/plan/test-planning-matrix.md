# Test Planning Matrix

**Generated:** 2026-07-08
**Plan:** 19.7_live_model_resolution
**Total ACs:** 22

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01: Catalog Routability Spike & Stable-Channel Heuristic | 2 | 0 | 0 (2 Manual) | 0 | Low |
| 02: Family/Channel Binding & Resolved Lock | 3 | 2 | 1 | 0 | Medium |
| 03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin) | 5 | 5 | 0 | 0 | High |
| 04: Reproducible Upgrade with Before→After Lock Reporting | 3 | 2 | 1 | 0 | Medium |
| 05: `atcr models check` Drift Report | 4 | 3 | 1 | 0 | Medium |
| 06: Major-Bump Re-Validation Gate | 2 | 2 | 0 | 0 | Medium |
| 07: init/quickstart Roster Reconciliation | 3 | 1 | 2 | 0 | Medium |

---

## Detailed AC List

### Story 01: Catalog Routability Spike & Stable-Channel Heuristic

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | `-latest` Alias Routability Confirmed | Manual | Low | P1 |
| 01-02 | `@stable` Channel Heuristic & `z-ai/` Prefix | Manual | Medium | P1 |

### Story 02: Family/Channel Binding & Resolved Lock

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Family/Channel Binding Schema Extension | Unit | Medium | P1 |
| 02-02 | Review Path Reads Locked Slug, Zero Endpoint Calls | Integration | High | P1 |
| 02-03 | Pinned Model Seeds Initial Lock, Zero Migration | Unit | Low | P1 |

### Story 03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Alias Passthrough for 7 Alias-Covered Personas | Unit | Medium | P1 |
| 03-02 | `created`-Timestamp Vendor-Prefix Scan (incl. `z-ai/` correctness) | Unit | High | P1 |
| 03-03 | Explicit Pin Never Floats | Unit | Low | P1 |
| 03-04 | `@stable` Channel Excludes Preview & Expiring | Unit | High | P1 |
| 03-05 | `@latest` Channel Includes Preview | Unit | Medium | P1 |

### Story 04: Reproducible Upgrade with Before→After Lock Reporting

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Upgrade Resolves, Advances Lock, Reports Slug Change | Integration | Medium | P1 |
| 04-02 | Resolution Isolated to Upgrade Path (zero endpoint calls elsewhere) | Unit | High | P1 |
| 04-03 | `--dry-run` Reports Without Writing | Unit | Low | P1 |

### Story 05: `atcr models check` Drift Report

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Command Registration + Human-Readable Drift Report | Integration | Medium | P2 |
| 05-02 | `--json` Machine-Readable Output | Unit | Medium | P2 |
| 05-03 | Exit-Code Contract (0/1/2) | Unit | Low | P2 |
| 05-04 | Deterministic Catalog Snapshot Default (zero live network) | Unit | Medium | P2 |

### Story 06: Major-Bump Re-Validation Gate

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Major-Jump Fixture Gate + Unconditional Verify Flag | Unit | High | P1 |
| 06-02 | Minor-Jump Auto-Lock Regression Guard | Unit | Low | P1 |

### Story 07: init/quickstart Roster Reconciliation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 07-01 | Working, Non-Empty Community Roster Online | Integration | Medium | P2 |
| 07-02 | No Misleading Skip-Warnings | Unit | Low | P2 |
| 07-03 | Shared Reconciliation Point + Backward Compatibility | Integration | Medium | P2 |

---

## Test Coverage Notes

- **Manual Tests:** 2 ACs (both Story 01 — a one-time design spike requiring a real authenticated OpenRouter completion call; not automatable in CI)
- **Unit Tests:** 15 ACs require unit tests
- **Integration Tests:** 5 ACs require integration tests
- **E2E Tests:** 0 ACs require E2E tests
- **High Complexity:** 5 ACs marked high complexity (02-02 zero-endpoint-call review-path guarantee; 03-02 `created`-timestamp vendor-prefix scan incl. `z-ai/` correctness; 03-04 `@stable` preview/expiring exclusion; 04-02 resolution-isolation guarantee; 06-01 major-jump fixture gate + verify flag) — these are the ACs most load-bearing for the epic's core reproducibility and correctness guarantees and warrant the most adversarial test design during implementation.
