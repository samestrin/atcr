# Test Planning Matrix

**Generated:** 2026-07-14
**Plan:** 25.0_sarif_output_integration
**Total ACs:** 9

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - SARIF Formatter Core | 4 | 3 | 1 | 0 | Medium |
| 02 - Severity-to-SARIF-Level Mapping | 1 | 1 | 0 | 0 | Low |
| 03 - SARIF Line/File Anchoring | 2 | 2 | 0 | 0 | Medium |
| 04 - SARIF CI Integration Documentation | 2 | 0 | 0 | 0 | Low |

_Story 04's 2 ACs are documentation-only (Manual/Static: YAML lint + manual review) and do not fall under Unit/Integration/E2E — see Test Coverage Notes._

---

## Detailed AC List

### Story 1: SARIF Formatter Core

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | SARIF Format Constant Registration | Unit | Low | P1 |
| 01-02 | SARIF Base Document Structure | Unit + Golden-File | Medium | P1 |
| 01-03 | SARIF Rules Array (Category Linkage, Structural) | Unit | Medium | P1 |
| 01-04 | CLI Flag Help Text and MCP Parity | Integration + Unit | Low | P2 |

### Story 2: Severity-to-SARIF-Level Mapping

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Severity-to-SARIF-Level Mapping Function | Unit | Low | P1 |

### Story 3: SARIF Line/File Anchoring

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Line-Level Anchoring (URI Pass-Through + Line>0 Region) | Unit | Low | P1 |
| 03-02 | File-Level Fallback Anchoring (Line<=0 Synthesized Region) | Unit | Medium | P1 |

### Story 4: SARIF CI Integration Documentation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | GitHub Code Scanning Upload Example | Manual/Static | Low | P2 |
| 04-02 | GitLab CI SAST Widget Example | Manual/Static | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 7 ACs require unit tests (01-01, 01-02, 01-03, 01-04, 02-01, 03-01, 03-02)
- **Integration Tests:** 1 AC requires integration-level coverage (01-04, CLI command execution)
- **E2E Tests:** 0 ACs require E2E coverage
- **Manual/Static:** 2 ACs are documentation-only, verified via YAML lint and manual review (04-01, 04-02)
- **High Complexity:** 0 ACs marked high complexity
- **Medium Complexity:** 3 ACs (01-02, 01-03, 03-02) — all cluster around the two GitHub-specific display constraints this plan resolves: the base SARIF document/rules structural shape, and the `Line<=0` region-fallback synthesis required for GitHub Code Scanning display.
