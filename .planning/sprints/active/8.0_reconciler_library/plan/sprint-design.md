# Sprint Design: Reconciler Library Module Extraction

**Created:** June 23, 2026 02:00:01PM
**Plan:** [8.0: Reconciler Library Module Extraction](.planning/plans/active/8.0_reconciler_library/plan.md)
**Plan Type:** Feature ✨
**Status:** Design Complete

---

## Original User Request

> Extract ATCR's deterministic reconciler from `internal/reconcile` into a standalone Go module
> (`github.com/samestrin/atcr/reconcile`) with a clean public API, one format adapter, and a clear
> licensing path. This turns the core architectural moat into a separable asset that other tools can
> embed and that can generate licensing revenue without running SaaS.

**Referenced Resources:**

- [Epic 8.0 Reconciler Library](original-requirements.md)
  - **Summary:** Full epic specification for extracting the reconciler as a nested Go module with a root `replace` directive, lift-as-is public API, JSON adapter, dual licensing, and independent CI.
  - **Key Points:**
    - Lift existing API as-is (`Reconcile(sources []Source, opts Options) Result`); clean-API reshape deferred to follow-on epic.
    - Nested module at `./reconcile/` with own `go.mod`; root `replace github.com/samestrin/atcr/reconcile => ./reconcile`.
    - Phase-0 boundary: `emit.go`/`discover.go` type/IO split; `Verification` becomes public; levenshtein stays in `internal/stream`.

- [Go Module & Standard Library](documentation/go-module-stdlib.md)
  - **Summary:** Go 1.25 toolchain docs covering nested module creation and the `replace` directive.
  - **Key Points:** `go.mod` with `module github.com/samestrin/atcr/reconcile`, no `require` block (stdlib-only), `replace` in root `go.mod`.

- [Reconciler Public API & Verification Interface](documentation/reconciler-api-verification.md)
  - **Summary:** Lifted-as-is public API contract and public/private boundary decisions ratified 2026-06-23.
  - **Key Points:** `Verification` becomes public library type; `gate.go` imports it unchanged; `Merged.Verification` pointer-identity preserved.

- [JSON Format Adapter (reconcile-json/v1)](documentation/json-adapter.md)
  - **Summary:** `encoding/json` adapter with independently-versioned `reconcile-json/v1` schema (not `atcr-findings/v1`).
  - **Key Points:** Decode accepts single object or array; encode produces versioned envelope with `omitempty` on `disagreement`/`verification`; no path-validation fields in external schema.

- [Testing with testify](documentation/testing-testify.md)
  - **Summary:** Test conventions including runnable godoc `Example` function in `example_test.go`.
  - **Key Points:** Table-driven tests; testify confined to `*_test.go`; `example_test.go` must compile without external deps.

- [Linting & CI/CD](documentation/linting-ci.md)
  - **Summary:** golangci-lint config and dual-coverage module CI wiring.
  - **Key Points:** golangci-lint pinned to `v2.12.2` (resolve version drift against `ci.yml` before implementation); `[gauntlet]` self-hosted runner; Go 1.25 setup reused.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Reconciler Library Extraction
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 5
**Pattern:** Foundation → Core Extraction → Consumer Flip → Adapter & Docs → CI & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go nested module extraction replace directive pattern
mechanical move refactor import flip stdlib-only
boundary adapter stream type conversion Go packages
deterministic output byte-identical fixture corpus oracle
dual-license Apache commercial placeholder Go library
```

---

## Complexity Breakdown

- **Architecture:** 2/3 — New nested module scaffold + `replace` directive + public/private type split across `emit.go`/`discover.go`; new structural pattern for this repo but not a full rewrite.
- **Integration:** 2/3 — 9 ATCR consumer packages need import-flip, nested module `go.mod` boundary blocks root `go test ./...`, two new CI workflows, external JSON adapter, `docs/scorecard.md` edit.
- **Story/Task & Test:** 3/3 — 6 stories, 25 ACs (8 unit / 13 integration / 4 E2E), byte-identical fixture oracle split across two module test runs, table-driven corpus tests and round-trip adapter tests.
- **Risk/Unknown:** 2/3 — `emit.go`/`discover.go` type/IO split explicitly flagged error-prone; consumer scope exceeded original enumeration (9 packages including `internal/scorecard` and `internal/registry`); golangci-lint version drift risk.

**Time Formula:** base(COMPLEX 8-12 days) anchored by story effort (2L + 3M + 1S = 2×3 + 3×1.5 + 1×0.5 = 11 raw days) × mechanical-move discount (0.9) ≈ 10 days
**Calculation:** 11 × 0.9 ≈ 10 days across 5 phases

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** standard (complexity 9/12 — gated strongly suggested, not mandatory)
**Suggested command:** `/create-sprint @.planning/plans/active/8.0_reconciler_library/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 ✓; gated triggered by complexity >= 8/12 ✓, phases >= 5 ✓, or duration > 5 days ✓; strong gated at complexity >= 10/12 (not triggered).

---

## Phase Structure

### Phase 1: Foundation & Scaffold (2 days)
**Stories:** 2 (partial)
**Focus:** Create the nested module, wire the root `replace` directive, split `emit.go`/`discover.go` into types-only (library) vs I/O (ATCR), stub the boundary adapter package.

| Task | AC Coverage | Type |
|------|------------|------|
| Create `./reconcile/go.mod` (module `github.com/samestrin/atcr/reconcile`, go 1.25, no requires) | 02-01 | Integration |
| Add `replace github.com/samestrin/atcr/reconcile => ./reconcile` to root `go.mod` | 01-01, 02-01 | Integration |
| Split `emit.go`: move `Verification`, `VerdictConfirmed/Refuted/Unverifiable`, library `Finding` to `reconcile/`; keep I/O layer ATCR-internal | 01-03, 02-04 | Integration |
| Split `discover.go`: move `Source` type to `reconcile/`; keep `Discover()` file-reading ATCR-internal | 01-03, 02-04 | Integration |
| Create stub `internal/reconcile/adapter/adapter.go` (package boundary for `stream.Finding` ↔ `reconcile.Finding`) | 02-04 | Integration |
| Verify `go build ./reconcile/...` succeeds (no non-stdlib imports) | 02-03 | Unit/Integration |

### Phase 2: Core Extraction (3 days)
**Stories:** 1, 2 (completion)
**Focus:** Mechanically move all pure reconcile logic into the library; migrate severity canonical ownership; preserve `sortMerged` total order and integer-cross-multiply dedupe thresholds exactly.

| Task | AC Coverage | Type |
|------|------------|------|
| Move `reconcile.go` → `reconcile/reconcile.go` (`Reconcile` entry + `Options`/`Result`/`Summary`; preserve `sortMerged`) | 02-02 | Unit |
| Move `merge.go` → `reconcile/merge.go` (`Merged`, `mergeVerification`, `verdictRank`, finding-merge rules) | 02-02 | Unit |
| Move `dedupe.go` → `reconcile/dedupe.go` (Jaccard integer-cross-multiply thresholds preserved) | 02-02 | Unit |
| Move `disagree.go` → `reconcile/disagree.go` (`BuildDisagreements`) | 02-02 | Unit |
| Move `confidence.go` → `reconcile/confidence.go` (`ConfidenceForVerdict`/`ConfidenceAtOrAbove`) | 02-02 | Unit |
| Move `cluster.go` → `reconcile/cluster.go` (FILE, LINE±3 single-linkage) | 02-02 | Unit |
| Move `ambiguous.go` → `reconcile/ambiguous.go` (`AmbiguousID`/`AmbiguousHash`/`AmbiguousCluster`) | 02-02 | Unit |
| Move `attribution.go` → `reconcile/attribution.go` (`EvidenceSep`/`FixAttribution`) | 02-02 | Unit |
| Move `internal/stream/severity.go` (NormalizeSeverity/SeverityRank) → `reconcile/severity.go`; eliminate `merge.go:30` init-copy | 02-05 | Unit |
| Enforce stdlib-only: `golangci-lint run ./reconcile/...` clean; testify confined to `*_test.go` | 02-03 | Unit |
| Move pure-logic tests with library code (e.g., `TestReconcile_TwoReviewersAgreeHighConfidence` → `reconcile/reconcile_test.go`) | 02-02 | Unit |
| Complete `internal/reconcile/adapter/adapter.go`: `stream.Finding` ↔ `reconcile.Finding` conversion + path-validation stamping on `JSONFinding` | 01-02 | Unit |

**RED tests (new behavior only):** boundary adapter round-trip (`stream.Finding` → `reconcile.Finding` → `JSONFinding` with `PathValid`/`PathWarning`/`PathSuggestion` preserved); `TestBoundaryAdapter_FindingConversionRoundTrip`.

### Phase 3: Consumer Import-Flip (2 days)
**Stories:** 1 (completion)
**Focus:** Rewire all 9 consumer packages to import `github.com/samestrin/atcr/reconcile`; grep-verify no residual `internal/reconcile` type re-declarations; confirm byte-identical fixtures.

| Task | AC Coverage | Type |
|------|------------|------|
| Flip `cmd/atcr` (github.go, reconcile.go, report.go, resume.go, review.go, verify.go) → import library | 01-04 | Integration |
| Flip `internal/debate` (debate.go, emit.go, envelope.go, select.go) → import library | 01-04 | Integration |
| Flip `internal/verify` (votes.go, severity.go) → import library | 01-04 | Integration |
| Flip `internal/report` (disagree.go, render.go) → import library | 01-04 | Integration |
| Flip `internal/ghaction` (render.go) → import library | 01-04 | Integration |
| Flip `internal/mcp` (handlers.go) → import library | 01-04 | Integration |
| Flip `internal/fanout` (metrics.go, postprocess.go) → import library (`NormalizeSeverity`/`SeverityRank`) | 01-04 | Integration |
| Flip `internal/scorecard` (reconcile.go) → import library | 01-04 | Integration |
| Flip `internal/registry` (config.go) → import library severity helpers | 01-04 | Integration |
| Grep-verify: no `internal/reconcile` severity/verdict re-declarations remain in any ATCR package | 01-04 | Integration |
| Diff all fixture files (`findings.json`, `ambiguous.json`, `disagreements.json`) against pre-extraction baseline — must be zero diff | 01-05 | Integration |
| `go test ./...` green in root module (ATCR corpus full pass) | 01-05, 01-06 | Integration |

### Phase 4: Adapter, Docs & Licensing (2 days)
**Stories:** 3, 4, 5
**Focus:** JSON adapter with `reconcile-json/v1` schema, README + godoc example, Apache 2.0 and commercial license files.

| Task | AC Coverage | Type |
|------|------------|------|
| Create `reconcile/adapter/json/adapter.go`: decode (sniff `[` vs `{`, single or array) → `[]reconcile.Source` | 03-01 | Unit |
| Create `reconcile/adapter/json/adapter.go`: encode `reconcile.Result` → versioned `reconcile-json/v1` JSON envelope | 03-02 | Unit |
| Apply `omitempty` to `Disagreement`/`*Verification`; byte-stability test (encode same `Result` twice → identical bytes) | 03-03 | Unit |
| Round-trip integration test: no path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) in encoded output | 03-04 | Integration |
| Write `reconcile/README.md`: public API surface (lifted-as-is), behavior (clustering/Jaccard/confidence/disagreement), install + quickstart, licensing pointer | 04-01, 04-02 | Integration |
| Write `reconcile/example_test.go`: runnable `ExampleReconcile()` — two sources, overlapping findings, merged `Result` with confidence and disagreement; verify `go test ./reconcile/...` runs it green | 04-03 | Unit |
| Add `reconcile/LICENSE` (verbatim Apache 2.0, copyright Sam Estrin 2026) | 04-04 | Integration |
| Add `reconcile/LICENSE-COMMERCIAL.md` (dual-license pairing statement, contact path, no-enforcement declaration) | 05-01, 05-02 | Integration |
| Add licensing section to `reconcile/README.md` cross-referencing both license files | 05-03 | Integration |
| Code scan: no license-check or payment-gating code in `./reconcile/` | 05-03 | Integration |

**RED tests:** `TestDecode_SingleSourceObject`, `TestDecode_ArrayOfSources`, `TestEncode_VersionedEnvelope`, `TestByteStability_IdenticalOutput`, `TestNoPathValidationFieldsInOutput`.

### Phase 5: CI, Leaderboard & Validation (1 day)
**Stories:** 6 + final validation
**Focus:** Wire independent module CI (tag-push + PR-time); add `docs/scorecard.md` citation; prove dual CI jobs green.

| Task | AC Coverage | Type |
|------|------------|------|
| Create `.github/workflows/reconcile-module.yml`: triggers on tag push; `cd ./reconcile && gofmt -l . && golangci-lint run --timeout 5m && go test -race ./...`; reuse `[gauntlet]` runner + Go 1.25 | 06-01 | E2E |
| Add PR/push job in `.github/workflows/ci.yml`: `cd ./reconcile && go test ./...`; inline comment `# root go test ./... does NOT cross ./reconcile's go.mod boundary` | 06-02 | E2E |
| Resolve golangci-lint version: pin `v2.12.2` in `reconcile-module.yml`; reconcile against `ci.yml` current version (flagged: `ci.yml` uses `v2.6.2/@v8`, story targets `v2.12.2/@v3` — resolve before implementation) | 06-01 | E2E |
| Update `docs/scorecard.md`: additive note citing `github.com/samestrin/atcr/reconcile` as standalone reference implementation backing every scorecard record | 06-03 | Unit |
| Prove nested-module boundary gap is closed: deliberately break `./reconcile` and verify PR-time `ci.yml` job catches it (root `go test ./...` does NOT) | 06-02 | E2E |
| `go test ./reconcile/...` green (library module corpus pass) | 01-06 | E2E |
| Final: `go test ./...` green in root module (ATCR corpus full pass, zero behavioral change confirmed) | 01-06 | E2E |

---

## Work Decomposition

### Story 1: Preserve ATCR as the Reference Implementation (L, High) — Phase 1-3
*As ATCR maintainer, I want ATCR to consume the extracted reconcile library through a boundary adapter that preserves path-validation fields, with every consumer package re-importing the library's public types.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 01-01: Root replace directive + nested module | 1 | Root `go.mod` has `replace github.com/samestrin/atcr/reconcile => ./reconcile`; `go build` succeeds | Integration |
| 01-02: Boundary adapter finding conversion | 2 | `adapter.Convert(stream.Finding)` → `reconcile.Finding` with all 9 wire fields; path-validation fields on ATCR `JSONFinding`; `*Verification` pointer-identity preserved | Unit |
| 01-03: Public type and I/O split | 2 | Library non-test files: zero `os`/`io` imports; `Verification`, `Verdict` constants, `Source`, `Finding` exported by library | Integration |
| 01-04: Consumer import-flip (all 9 packages) | 3 | All 9 packages compile importing `github.com/samestrin/atcr/reconcile`; no local re-declarations of verdict/severity constants | Integration |
| 01-05: Byte-identical fixtures | 3 | `findings.json`, `ambiguous.json`, `disagreements.json` zero-diff against pre-extraction baseline | Integration |
| 01-06: Test corpus green + dual CI | 5 | Root `go test ./...` + `go test ./reconcile/...` both green | E2E |

### Story 2: Embeddable Public API Module Scaffold (L, High) — Phase 1-2
*As external tool author, I want to import ATCR's reconciler as a standalone Go module with a stable public API without importing the ATCR binary.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 02-01: Nested module scaffold | 1 | `./reconcile/go.mod` exists; `go build ./reconcile/...` passes; `go test ./reconcile/...` passes | Integration |
| 02-02: Lifted-as-is public API surface | 2 | `go doc github.com/samestrin/atcr/reconcile` shows `Reconcile`, `Source`, `Finding`, `Merged`, `Options{ReconciledAt,Partial,Merges,Root}`, `Result`, `Summary`, `Verification`, `Verdict*` constants | Unit |
| 02-03: Stdlib-only boundary enforcement | 2 | `golangci-lint run ./reconcile/...` clean; no third-party imports in non-test library files; `go mod tidy` in `./reconcile/` yields empty `require` block | Unit/Integration |
| 02-04: Type/IO split + boundary adapter | 2 | `internal/reconcile/adapter/adapter.go` exists; `gate.go`/`validate.go` stay ATCR-internal; adapter converts `stream.Finding` ↔ `reconcile.Finding` | Integration |
| 02-05: Severity canonical migration | 2 | `reconcile/severity.go` owns `NormalizeSeverity`/`SeverityRank`; `merge.go:30` init-copy eliminated; `internal/stream/severity.go` sources from or defers to library | Unit |

### Story 3: JSON Format Adapter (M, High) — Phase 4
*As external tool author who emits JSON, I want a `reconcile-json/v1` adapter that decodes my finding stream into `[]reconcile.Source` and encodes a `reconcile.Result` back.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 03-01: Decode single and array sources | 4 | `Decode(single)` and `Decode(array)` → `[]Source`; unknown fields ignored | Unit |
| 03-02: Encode result to versioned envelope | 4 | Output has `"version":"reconcile-json/v1"`, RFC3339 `reconciled_at`, `findings[]`, `summary`, `ambiguous[]` | Unit |
| 03-03: Byte stability + omitempty | 4 | Two encodes of same `Result` → identical bytes; absent `disagreement`/`verification` → no keys in output | Unit |
| 03-04: Path-validation isolation + schema independence | 4 | No `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` in encoded output; `"version"` is never `"atcr-findings/v1"` | Integration |

### Story 4: OSS Adoption Documentation + Apache 2.0 (M, High) — Phase 4
*As OSS adopter, I want the module to ship a README with godoc, a runnable example, and an Apache 2.0 LICENSE.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 04-01: README public API surface | 4 | README documents `Reconcile`, all public types, constants; cross-checked against `go doc` output | Integration |
| 04-02: README behavior/install/quickstart | 4 | README covers clustering/Jaccard/confidence/disagreement behavior, `go get` install, quickstart snippet | E2E |
| 04-03: Runnable godoc example | 4 | `go test ./reconcile/...` runs `ExampleReconcile()` green; `go doc` renders it | Unit |
| 04-04: Apache 2.0 LICENSE | 4 | `reconcile/LICENSE` present; verbatim Apache 2.0 full text with Sam Estrin copyright line | Integration |

### Story 5: Commercial License Placeholder (S, Medium) — Phase 4
*As proprietary vendor, I want a clear commercial licensing option with contact path and no enforcement code.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 05-01: Commercial license placeholder | 4 | `reconcile/LICENSE-COMMERCIAL.md` exists; states commercial license available; references `LICENSE` pairing | Integration |
| 05-02: Contact path + no-enforcement statement | 4 | File names a reachable contact path; explicitly states no enforcement/payment-gating code | Integration |
| 05-03: README licensing section + code scan | 4 | `reconcile/README.md` has licensing section; grep `./reconcile/` finds zero license-check or payment-gating code | Integration |

### Story 6: Independent Module CI + Leaderboard Citation (M, High) — Phase 5
*As leaderboard maintainer and release engineer, I want the extracted module to pass independent CI on tag push and PRs, and the scorecard methodology to cite it.*

| AC | Phase | Testable Element | Test Type |
|----|-------|-----------------|-----------|
| 06-01: Tag-push release gate | 5 | `.github/workflows/reconcile-module.yml` exists; triggers on tag; runs gofmt + golangci-lint + go test -race | E2E |
| 06-02: PR-time module test job | 5 | `ci.yml` has `./reconcile` job; deliberate break in library caught by PR job, NOT by root `go test ./...` | E2E |
| 06-03: Scorecard methodology citation | 5 | `docs/scorecard.md` contains additive note citing `github.com/samestrin/atcr/reconcile` as reference implementation | Unit |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Same-package directory as code under test (`*_test.go`), per Go conventions.

**Test File Placement Examples:**
- `reconcile/reconcile_test.go` — library core, lifted corpus tests
- `reconcile/adapter/json/adapter_test.go` — JSON adapter unit + integration
- `internal/reconcile/adapter/adapter_test.go` — boundary adapter conversion
- `reconcile/example_test.go` — runnable godoc example

**Unit Tests:** Boundary adapter conversion, lifted API surface pinning, severity canonical migration, JSON decode/encode/byte-stability, godoc example, scorecard citation check.

**Integration Tests:** Module scaffold + `replace` directive wiring, type/IO split verification, consumer import-flip (all 9 packages compile), byte-identical fixture diff, stdlib-only boundary lint, JSON schema isolation (no path-validation fields), README/license file discovery, dual-license pairing.

**E2E Tests:** Dual CI job gate (root + library both green), README runnable quickstart, tag-push release gate workflow (`reconcile-module.yml` fires and passes), PR-time boundary-gap proof (deliberate `./reconcile` break caught only by PR job).

**Test Environment Status:**
- Framework: Go standard `testing` library + `github.com/stretchr/testify/assert`/`require` (testify confined to `*_test.go`) ✓
- Execution: `go test ./...` (root module) + `go test ./reconcile/...` (library module, separate job) — root `go test ./...` does NOT cross nested `go.mod` boundary
- Coverage Tools: `go test -coverprofile=coverage.out ./...` in both modules; baseline ≥80%

**NEW RED Tests (genuinely new behavior only):**
- `TestBoundaryAdapter_FindingConversionRoundTrip` — `stream.Finding` → `reconcile.Finding` → `JSONFinding` with path-validation fields preserved
- `TestDecode_SingleSourceObject` / `TestDecode_ArrayOfSources` — adapter input sniffing
- `TestEncode_VersionedEnvelope` — `reconcile-json/v1` output schema
- `TestByteStability_IdenticalOutput` — two encodes of same `Result` → identical bytes
- `TestNoPathValidationFieldsInOutput` — isolation assertion

---

## Architecture

**Primitives:**
- `reconcile.Finding` — 9 wire fields (Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer/Reviewers, Confidence) + `Disagreement string` + `*Verification`
- `reconcile.Source` — `{Name string; Findings []Finding}`
- `reconcile.Merged` — embeds `Finding` + owns `Reviewers []string` + `Confidence string`
- `reconcile.Options` — `{ReconciledAt time.Time; Partial bool; Merges map[string]int; Root string}` (lifted as-is)
- `reconcile.Result` — `{Findings []Merged; Ambiguous []AmbiguousCluster; Summary Summary}`
- `reconcile.Verification` — `{Skeptic, Verdict, Notes string}` (public library type)

**Module Boundaries:**
- `github.com/samestrin/atcr/reconcile` (`./reconcile/`) — public: `Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict*` constants, `NormalizeSeverity`, `SeverityRank`, `AmbiguousCluster`, `AmbiguousID`/`AmbiguousHash`, `BuildDisagreements`, `EvidenceSep`/`FixAttribution`
- `./reconcile/adapter/json/` — `reconcile-json/v1` adapter (stdlib `encoding/json` only)
- `internal/reconcile/adapter/` — ATCR-internal boundary: `stream.Finding` ↔ `reconcile.Finding` conversion + path-validation stamping + file I/O
- `internal/reconcile/` — residual ATCR-internal: `gate.go` (`IsFailing`/`CountAtOrAbove`), `validate.go` (path validation), `doc.go`

**External Dependencies to Wrap:**
- None in library (stdlib-only; testify test-only)
- `encoding/json` used only in JSON adapter (acceptable)

**Replaceability:**
- Library is stdlib-only and stateless — any consumer can swap the implementation by re-implementing the `Reconcile(sources []Source, opts Options) Result` signature
- JSON adapter is thin `encoding/json` wrapper — replaceable via a SARIF adapter or other format in a follow-on
- ATCR boundary adapter is a single file (`adapter.go`) — replaceability proven by the `stream.Finding` ↔ `reconcile.Finding` conversion being isolated there

**Critical Invariants to Preserve:**
- `sortMerged` total order (severity desc → file → line) — byte-identical output guarantee
- Integer-cross-multiply Jaccard thresholds (`inter*10 vs union*N`, exact 0.7/0.4 boundaries) — no float conversions
- `*Verification` pointer-identity across boundary (`Merged.Verification` mutated by `internal/debate` and read by `gate.go`)

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| JSON adapter decode | External JSON input → library types | Malformed JSON, excessively deep nesting, unknown field names causing decode failure | Use `encoding/json` defaults (ignore unknown fields); validate required fields after unmarshal; no `DisallowUnknownFields` (would break tolerance) |
| License file content | `LICENSE`, `LICENSE-COMMERCIAL.md` | Inadvertent commercial commitments via placeholder language | Explicit "terms negotiated on contact" language; avoid granting language in placeholder |
| `replace` directive in root `go.mod` | Development-only wiring | External consumers confused by `replace` in published module | Document as development-only; note that separate-repo publication removes it |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `sortMerged` (reconcile.go) | Called once per `Reconcile` invocation on all merged findings | Byte-identical, deterministic output | Preserve sort order exactly; no map-iteration dependency |
| Jaccard classify (dedupe.go) | O(n²) per cluster for dedup | Exact 0.7/0.4 thresholds, no float drift | Keep integer cross-multiply (`inter*10 vs union*N`); no float comparisons |
| `go test -race ./reconcile/...` in tag-push CI | CI gating | Under 5 min (golangci-lint timeout) | Library is synchronous and stateless; no goroutines; race detector add no overhead |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Degenerate input | Empty `[]Source`; single `Source` with zero findings; all findings identical | `Result` with empty `Findings`; single merged finding; one cluster with ambiguity threshold |
| Verification mutation across boundary | `debate` package mutates `Merged.Verification` after `Reconcile` returns | `*Verification` pointer shared; mutation visible to `gate.go`; no copy semantics introduced |
| JSON adapter input shape | Single object vs array; extra unknown fields; missing optional fields | Sniff first byte; `[` → array path; else → single wrap; unknown fields ignored; optional fields zero-valued |
| Byte-stability | Same `Result` encoded twice; optional fields absent; `omitempty` on `Disagreement`/`*Verification` | Identical bytes; absent optional fields produce no keys; `encoding/json` struct field order fixed by declaration |
| Consumer import-flip completeness | A consumer missed in the 9-package list re-declares a severity/verdict constant locally | Grep-verify `internal/*` and `cmd/atcr` for re-declarations; CI fails if any `internal/reconcile` import remains in non-adapter code |
| golangci-lint version drift | `ci.yml` uses `v2.6.2/@v8`; story targets `v2.12.2/@v3` | Resolve to a single pinned version before Phase 5; use `.golangci.yml` at repo root for local/CI parity |
| Tag-push CI trigger scope | `on: push: tags: ['*']` may fire on ATCR app tags, not just module release tags | Scope tag filter to module release convention; verify against recent tag history |

### Defensive Measures Required

- **Input Validation:** JSON adapter validates `"version"` field equals `"reconcile-json/v1"` after unmarshal; required fields (`severity`, `file`, `line`) must be non-zero before constructing `reconcile.Finding`.
- **Error Handling:** Return `([]Source, error)` from decode; return `([]byte, error)` from encode; propagate `json.Unmarshal`/`json.Marshal` errors, never swallow.
- **Logging/Audit:** No logging in library (stdlib-only, stateless); ATCR boundary adapter may log at the caller's discretion; library is silent.
- **Rate Limiting:** N/A — library is synchronous, in-process; no network I/O.
- **Graceful Degradation:** Reconciler returns a valid `Result` even with a single source (no merge performed, confidence unchanged); adapter returns decode error immediately on malformed JSON rather than partially-constructed `[]Source`.

---

## Risks

**Technical:**
- `emit.go`/`discover.go` type/IO split is error-prone (public types entangled with file I/O) → split mechanically: types first, compile-check in both packages, then relocate I/O; never move I/O and types in the same commit.
- Root `go test ./...` does not cross nested `go.mod` boundary → dual CI jobs are mandatory; library regressions silently hide without the PR-time `ci.yml` job.
- Consumer scope broader than original epic list (9 packages including `internal/scorecard` and `internal/registry`) → use the 2026-06-23 audit as the canonical source of truth; grep-verify no residual re-declarations after flip.
- `SeverityRank` dual-ownership (`merge.go:30` init-copy + `internal/stream/severity.go`) → library becomes canonical owner; either eliminate the `merge.go` copy or have `internal/stream` source from the library (not both).
- golangci-lint version drift (`ci.yml` v2.6.2 vs story target v2.12.2) → resolve to a single pinned version before Phase 5 implementation; document in `.golangci.yml`.

**TDD-Specific:**
- Extraction is mechanical-move-dominant: most of Stories 1-2 validated by existing corpus, not new RED tests → resist the urge to write new RED tests for moved behavior; only write RED for genuinely new behavior (boundary adapter conversion, JSON adapter).
- Byte-identical fixture test requires a pre-extraction snapshot baseline → capture baseline (diff-able snapshot of `findings.json`/`ambiguous.json`/`disagreements.json`) before any move; commit baseline as a test fixture file.
- `example_test.go` output ordering must be deterministic → depend on `sortMerged` total order; assert on sorted output explicitly; document that determinism is a feature.
- `go test -race ./reconcile/...` in tag-push CI validates that the stateless library has no data races → library is synchronous with no goroutines, so this is expected to pass; flag if any concurrency is accidentally introduced during move.

---

**Next:** `/create-sprint @.planning/plans/active/8.0_reconciler_library/ --gated`
