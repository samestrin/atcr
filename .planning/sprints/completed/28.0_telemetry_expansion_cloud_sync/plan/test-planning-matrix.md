# Test Planning Matrix

**Generated:** 2026-07-15
**Plan:** 28.0_telemetry_expansion_cloud_sync
**Total ACs:** 19

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - Anonymous Usage Telemetry Ping | 4 | 4 | 0 | 0 | Medium |
| 02 - Telemetry Opt-Out | 4 | 4 | 3 | 0 | Medium |
| 03 - Persona ID Hashing for the Persona Leaderboard | 4 | 3 | 1 | 0 | High |
| 04 - `--sync-cloud` Authenticated Push | 4 | 4 | 2 | 0 | High |
| 05 - Telemetry Privacy Documentation | 3 | 3 | 0 | 0 | Low |

---

## Detailed AC List

### Story 01: Anonymous Usage Telemetry Ping

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Fire-and-Forget Telemetry Send | Unit | Medium | P1 |
| 01-02 | Bounded, Non-Blocking Timeout | Unit | High | P1 |
| 01-03 | Panic-Safe, Fail-Open Behavior | Unit | High | P1 |
| 01-04 | Schema-Constrained Payload (No Source Code or File Paths) | Unit | Medium | P2 |

### Story 02: Telemetry Opt-Out

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | `ATCR_TELEMETRY=0` Disables Telemetry Process-Wide | Unit + Integration | Medium | P1 |
| 02-02 | `atcr config set telemetry <bool>` Persists Opt-Out to `.atcr/config.yaml` | Unit + Integration | Medium | P2 |
| 02-03 | Env Var and Config Opt-Outs Are OR'd, Never Overridden | Unit + Integration | High | P2 |
| 02-04 | `docs/telemetry.md` Documents the Opt-Out and `docs_audit_test.go` Coverage Passes | Unit | Low | P2 |

### Story 03: Persona ID Hashing for the Persona Leaderboard

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Deterministic Hashed-Persona-ID Function | Unit | Medium | P1 |
| 03-02 | Dedicated Telemetry Persona Schema Type | Unit | Medium | P2 |
| 03-03 | Existing Leaderboard Export Path Byte-for-Byte Regression | Integration | High | P1 |
| 03-04 | Hash Determinism, Uniqueness, and Non-Reversibility Unit Tests | Unit | Medium | P2 |

### Story 04: `--sync-cloud` Authenticated Push

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | `--sync-cloud` Flag Registration | Unit | Low | P2 |
| 04-02 | Successful Authenticated Cloud Push | Unit + Integration | High | P1 |
| 04-03 | Missing `ATCR_API_KEY` Dedicated Exit Code | Unit | Medium | P1 |
| 04-04 | Invalid/Rejected `ATCR_API_KEY` Dedicated Exit Code | Unit + Integration | High | P2 |

### Story 05: Telemetry Privacy Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | `docs/telemetry.md` Content Coverage and `docs/README.md` Index Link | Unit | Low | P3 |
| 05-02 | `docs/scorecard.md` Privacy Model Section Updated for Telemetry/Cloud-Sync | Unit | Low | P3 |
| 05-03 | `docs_audit_test.go` Flag/Index Coverage Passes with Finalized Docs | Unit | Low | P3 |

---

## Test Coverage Notes

- **Unit Tests:** 18 ACs require unit tests (all except 03-03, which is integration-only)
- **Integration Tests:** 6 ACs require integration coverage (02-01, 02-02, 02-03, 03-03, 04-02, 04-04) — mostly around opt-out precedence, cloud-sync auth, and the leaderboard-export regression
- **E2E Tests:** 0 ACs require E2E coverage — this plan is CLI/library-level with no UI surface
- **High Complexity:** 6 ACs marked high complexity (01-02, 01-03, 02-03, 03-03, 04-02, 04-04) — concentrated around goroutine/panic safety, opt-out precedence logic, the privacy-critical export regression, and cloud-sync auth
