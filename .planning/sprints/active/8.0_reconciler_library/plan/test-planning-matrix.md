# Test Planning Matrix

**Generated:** 2026-06-23
**Plan:** 8.0_reconciler_library
**Total ACs:** 25

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 — Reference Implementation Preservation (L) | 6 | 1 | 4 | 1 | Medium |
| 02 — Embeddable Public API Scaffold (L) | 5 | 2 | 3 | 0 | Medium |
| 03 — JSON Format Adapter reconcile-json/v1 (M) | 4 | 3 | 1 | 0 | Low-Medium |
| 04 — OSS Adoption Documentation + Apache 2.0 (M) | 4 | 1 | 2 | 1 | Low |
| 05 — Dual Licensing Path (S) | 3 | 0 | 3 | 0 | Low |
| 06 — Independent Module CI + Leaderboard Citation (M) | 3 | 1 | 0 | 2 | Medium |
| **Total** | **25** | **8** | **13** | **4** | — |

---

## Detailed AC List

### Story 01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Root `go.mod` `replace` directive + stdlib-only nested module consumption | Integration | Medium | P1 |
| 01-02 | Boundary adapter converts `stream.Finding` ↔ `reconcile.Finding`, stamps path-validation fields, preserves `*Verification` identity | Unit | High | P1 |
| 01-03 | Public types / I/O split — `Verification`, `Verdict` constants, `Source`, `Finding` move to library; library has zero `os`/`io` imports | Integration | High | P1 |
| 01-04 | Consumer package import-flip across all 9 consumers; no local re-declarations | Integration | High | P1 |
| 01-05 | Byte-identical fixtures (`findings.json`/`ambiguous.json`/`disagreements.json`) vs pre-extraction baseline | Integration | Medium | P1 |
| 01-06 | Test corpus green + dual CI jobs (root `go test ./...` + `reconcile-module.yml`) required checks | E2E | Medium | P1 |

### Story 02: Embeddable Public API Module Scaffold

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Nested module scaffold with root `replace` directive | Integration | Medium | P1 |
| 02-02 | Lifted-as-is public API surface pinned (`Reconcile(sources, opts) Result`, `Options{ReconciledAt,...}`) | Unit | Medium | P1 |
| 02-03 | Stdlib-only boundary enforcement (denylist grep for deferred clean API) | Integration | High | P1 |
| 02-04 | Type/I/O split + boundary adapter (`internal/reconcile/adapter/adapter.go`) | Integration | High | P1 |
| 02-05 | Severity canonical ownership migration (`NormalizeSeverity`/`SeverityRank`) | Unit | Medium | P2 |

### Story 03: JSON Format Adapter (reconcile-json/v1)

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Decode single object and array of sources → `[]Source` | Unit | Low | P1 |
| 03-02 | Encode `Result` → versioned `reconcile-json/v1` envelope | Unit | Low | P1 |
| 03-03 | Byte stability + `omitempty` on `Disagreement`/`*Verification` | Unit | Medium | P2 |
| 03-04 | Path-validation field isolation + schema independence from `atcr-findings/v1` | Integration | Medium | P2 |

### Story 04: OSS Adoption Documentation and Apache 2.0 License

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | README public API surface (go-doc cross-check) | Integration | Low | P2 |
| 04-02 | README behavior / install / quickstart (runnable mirror) | E2E | Medium | P2 |
| 04-03 | Runnable godoc example (`example_test.go` compiles under `go test`) | Unit | Low | P2 |
| 04-04 | Apache 2.0 `LICENSE` (verbatim full text, module-root discovery) | Integration | Low | P3 |

### Story 05: Commercial License Placeholder for Proprietary Embedding

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | Commercial license placeholder + dual-license pairing | Integration | Low | P3 |
| 05-02 | Contact path + no-enforcement statement | Integration | Low | P3 |
| 05-03 | README licensing section + no-enforcement code scan | Integration | Medium | P3 |

### Story 06: Independent Module CI and Leaderboard Reference Citation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | Tag-push release gate workflow (`reconcile-module.yml`) | E2E | Medium | P2 |
| 06-02 | PR-time module test job closes nested-module boundary gap | E2E | Medium | P2 |
| 06-03 | `docs/scorecard.md` methodology cites standalone reference implementation | Unit | Low | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 8 ACs require unit tests (boundary conversion, API surface pinning, severity migration, JSON decode/encode/byte-stability, godoc example, scorecard citation).
- **Integration Tests:** 13 ACs require integration tests (module scaffold + `replace`, type/I/O split, consumer import-flip, byte-identical fixtures, stdlib-only boundary, JSON schema isolation, README/license discovery).
- **E2E Tests:** 4 ACs require E2E tests (dual CI job gate, README runnable quickstart, tag-push release gate, PR-time boundary-gap job).
- **High Complexity:** 5 ACs marked high complexity — 01-02, 01-03, 01-04 (the core extraction + import flip), 02-03 (stdlib-only boundary), 02-04 (type/I/O split + adapter). These are the lift-as-is-critical path: a regression here breaks AC#3 (zero behavioral change).
- **Open reconciliation flagged in Story 06:** golangci-lint version drift (story targets `v2.12.2`/`@v3`; codebase `ci.yml` uses `v2.6.2`/`@v8`). AC 06-01 requires the tag-push workflow pin a version consistent with `ci.yml`; resolve before implementation.
- **Shared-file overlap:** AC 01-06 and 06-01/06-02 both touch `.github/workflows/reconcile-module.yml` + the dual-job gate, but verify distinct properties (zero-behavioral-change gate vs release-engineering + boundary-gap proof). No conflict — complementary coverage.
