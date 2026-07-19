# Test Planning Matrix

**Generated:** 2026-07-19
**Plan:** 32.0_sandbox_execution_environment
**Total ACs:** 11

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - Route Auto-Fix Validation Through the Sandbox by Default | 3 | 2 | 1 | 0 | Medium |
| 02 - Sandbox Resolution and Preflight Gate for Auto-Fix | 3 | 2 | 1 | 0 | Medium |
| 03 - `--no-sandbox` Opt-Out Flag with CLI Security Warnings | 3 | 3 | 0 | 0 (1 optional E2E-flavored subprocess case) | Medium |
| 04 - Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk | 2 | 0 | 2 (docs-audit suite) | 0 | Low |

---

## Detailed AC List

### Story 01: Route Auto-Fix Validation Through the Sandbox by Default

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Sandbox-Routed Command Dispatch | Unit | Medium | P1 |
| 01-02 | RunResult-to-ValidationResult Translation | Unit | Medium | P1 |
| 01-03 | Zero Behavior Change to the runAutoFix Pipeline | Integration | Medium | P1 |

### Story 02: Sandbox Resolution and Preflight Gate for Auto-Fix

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Resolver Builds and Preflights a Sandbox Backend | Unit | Medium | P1 |
| 02-02 | Inverted Default Posture and SandboxConfig.Validate() Tension | Unit | Medium | P2 |
| 02-03 | Gate Integration — Sandbox Resolution as the Fourth Piece of validateAutoFixBackend | Integration | Medium | P1 |

### Story 03: `--no-sandbox` Opt-Out Flag with CLI Security Warnings

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | `--no-sandbox` Flag Registration and Help Text | Unit | Low | P2 |
| 03-02 | `--no-sandbox` Bypasses Story 2's Resolver/Preflight Gate | Unit (+ integration-flavored gate case) | Medium | P1 |
| 03-03 | Every-Run (Non-Memoized) stderr Security Warning | Unit (+ optional E2E subprocess case) | Medium | P1 |

### Story 04: Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | Auto-Fix Sandboxed-by-Default Posture and `auto_fix:` Config Block Are Documented | Docs Audit (existing suite) + Manual | Low | P2 |
| 04-02 | `--no-sandbox` Risk Is Documented and Cross-Linked, Verified Accurate Against Shipped CLI Behavior | Docs Audit (existing suite) + Manual | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 7 ACs require unit tests (01-01, 01-02, 02-01, 02-02, 03-01, 03-02, 03-03)
- **Integration Tests:** 4 ACs require integration-level coverage (01-03, 02-03, plus the docs-audit suite for 04-01/04-02, which runs as an existing automated Go test suite rather than new test code)
- **E2E Tests:** 0 ACs mandate a full E2E harness; 03-03 notes an optional subprocess-invocation case only if an existing CLI subprocess-test harness is already present in `cmd/atcr`
- **High Complexity:** 0 ACs marked high complexity — this plan is scoped as integration-only wiring of an already-shipped sandbox primitive, keeping every AC at Medium or Low
- **Sequencing note:** Story 04's two ACs are documentation-only and explicitly require a final accuracy pass against Stories 01-03's shipped flag name/warning text/default behavior before merge — flagged in each AC's own risk section, not assumed away here
