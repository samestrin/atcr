# Sprint Design: Extract Shared Wasm Guest ABI

**Created:** July 13, 2026
**Plan:** [Extract Shared Wasm Guest ABI](.)
**Plan Type:** 🔧 Tech-Debt
**Status:** Design Complete

---

## Original User Request

> Extract the duplicated alloc/free/emit/pins Wasm guest ABI boilerplate — currently copy-pasted across `goparser`, `pyparser`, and `braceparser` — into one shared internal guest package, now that the project's own documented extraction threshold ("parser count > 2") has been crossed.
>
> 1. Extract the shared alloc/free/emit/pins guest ABI into one internal guest package, imported by `goparser`, `pyparser`, and `braceparser`'s `main.go` (via `go.mod` replace directives coordinated in `build.sh`, matching the existing Wasm build convention).
> 2. Add a build-time note (doc comment) pinning the non-moving-GC pointer-packing assumption in the shared package, so a future moving GC change is a documented, discoverable risk rather than a silent one.

**Referenced Resources:** None — no specifications in `.planning/specifications/` matched this plan (semantic search returned 0 results at threshold 0.7; manual grep for wasm/astgroup/parser terms also found no matches). See `documentation/source.md`.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Shared Guest ABI Extraction
**Complexity:** 3/12 (SIMPLE)
**Timeline:** 1 day
**Phases:** 3
**Pattern:** Foundation → Integration → Completion & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go wasip1 isolated module extraction
go.mod replace directive local module
wasmexport thin wrapper delegation pattern
shared ABI package duplication threshold
non-moving GC pointer packing Wasm
```

---

## Complexity Breakdown

- **Architecture:** 0/3 - Follows the existing isolated wasip1-only Go module convention (own `go.mod` per parser) and the exact `require`+`replace` in-tree extraction mechanics already proven by Epic 8.0's `reconcile` module extraction (`go.mod:37-41`). No new architectural pattern is introduced.
- **Integration:** 1/3 - The identical wiring pattern (`require`+`replace => ../guestabi`, thin `wasmexport` wrapper) is applied mechanically to 3 sibling parser modules — repetition of one integration, not increasing integration diversity; `build.sh` is touched for verification/comment only, not functional change.
- **Story/Task & Test:** 1/3 - 3 tasks, each Effort S/M/S, with no new unit-test harness added (`guestabi` has no existing test pattern to extend); correctness is verified via `GOOS=wasip1` compile checks plus the existing `internal/astgroup` regression suite run unchanged.
- **Risk/Unknowns:** 1/3 - Minor, already-identified unknowns: `go.mod` replace resolution under `build.sh`'s per-directory invocation, `wasmexport`'s package-main constraint, `pyparser`'s missing `//go:build wasip1` tag affecting `go vet` env requirements, and the rebuild→`SHA256SUMS`→embed-test chain — all carry documented mitigations in the task files already.

**Time Formula:** SIMPLE baseline is 1-3 days; floor selected because all three tasks are individually low-effort (S/M/S) mechanical extraction with no new test surface, matching the source epic's own "Estimated time: 1 day."
**Calculation:** 3 tasks, sequential dependency chain (Task 01 → Task 02 → Task 03), each completable within a few hours ≈ 1 day total across 3 phases.

---

## Recommended Flags

**Adversarial:** true
**Gated:** false
**Recommendation strength:** standard (not strong)
**Suggested command:** `/create-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/ --adversarial`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 (complexity is 3/12, but phase count of 3 trips it); gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days (none met); strong gated at complexity >= 10/12 (not met).

---

## Phase Structure

### Phase 1: Foundation — Create Shared guestabi Module (~0.3 day)
- **Item:** Task 01 — Create Shared guestabi Module
- **Focus:** Stand up the new isolated Go module `internal/astgroup/parsers/src/guestabi` (own `go.mod`, wasip1-only, mirroring the per-parser isolation convention). Implement unexported `pins`, exported `Alloc`/`Free`/`Lookup`/`Emit`, and pin the non-moving-GC pointer-packing doc comment carried from `goparser/main.go:41-51` as the single authoritative copy. Verify with `GOOS=wasip1 GOARCH=wasm go build`/`go vet` inside the module, plus a root `go build ./...` check confirming the new module stays excluded from the parent build. No parser `main.go` touched yet.

### Phase 2: Integration — Wire goparser & pyparser (~0.4 day)
- **Item:** Task 02 — Wire goparser and pyparser to guestabi
- **Focus:** Wire `goparser/go.mod` and `pyparser/go.mod` with `require`+`replace => ../guestabi`; delete each parser's local `pins`/`alloc`/`free`/`emit` block; add thin `//go:wasmexport alloc`/`free` wrappers delegating to `guestabi`; re-point `parse()`'s buffer lookup to `guestabi.Lookup`. Build and test `goparser` fully before starting `pyparser`, catching a misconfigured `replace` directive immediately. `braceparser` stays untouched.

### Phase 3: Completion & Validation — Wire braceparser, Rebuild, Full Verify (~0.3 day)
- **Item:** Task 03 — Wire braceparser to guestabi and verify full build
- **Focus:** Apply the same wiring to `braceparser`, then close out the plan: regenerate all ten vendored `.wasm` binaries and `SHA256SUMS` via `build.sh`, run the full `internal/astgroup` suite (`embed_test.go`, `host_test.go`, `crosscompile_test.go`) unchanged, and confirm `go build ./...` / `go vet ./...` succeed at the repo root. This phase is the plan's Definition of Done gate — all three source TD rows resolve here.

---

## Work Decomposition

Grounded in the existing `tasks/` directory (WORK_ITEM_SOURCE = tasks) — no re-scoping performed.

### Task 01: Create Shared guestabi Module (AC1 foundation, AC2)
- **Testable elements:** `internal/astgroup/parsers/src/guestabi/go.mod` exists with its own module path and isolated-module comment; `guestabi.go` exports `Alloc`, `Free`, `Lookup(p int32) ([]byte, bool)`, `Emit(v any) int64` with unexported `pins`; non-moving-GC assumption documented once as a doc comment on `pins`; no `//go:wasmexport` directive present in the file.
- **Test type:** Compile-time verification (`GOOS=wasip1 GOARCH=wasm go build`/`go vet` inside the module) + root `go build ./...` exclusion check + unchanged `go test ./internal/astgroup/...` regression run — N/A for new unit tests (no existing harness pattern in sibling wasip1-only modules).
- **Dependencies:** None (foundation task).

### Task 02: Wire goparser and pyparser to guestabi (AC1)
- **Testable elements:** `goparser/main.go` and `pyparser/main.go` no longer define local `pins`/`alloc`/`free`/`emit`; both `go.mod` files carry the `require`+`replace => ../guestabi` pair; `parse()` in each uses `guestabi.Lookup`; `braceparser` untouched.
- **Test type:** `GOOS=wasip1 GOARCH=wasm go build`/`go vet` run individually per parser immediately after wiring (not batched), plus root `go build ./...` and `go test ./internal/astgroup/...` regression checks.
- **Dependencies:** Task 01 (`guestabi` module must exist and export the confirmed API).

### Task 03: Wire braceparser to guestabi and verify full build (AC1, AC2, AC3)
- **Testable elements:** `braceparser/main.go`/`go.mod` wired identically to Task 02's pattern; all ten vendored `.wasm` binaries + `SHA256SUMS` regenerated via `build.sh`; `TestEmbeddedParsersMatchManifest` (`internal/astgroup/embed_test.go:15`) passes against the regenerated binaries; full `internal/astgroup` suite passes unchanged; root `go build ./...`/`go vet ./...` succeed.
- **Test type:** Isolated single-tag smoke build before full `build.sh`, then `go test ./internal/astgroup/...` (embed, host, crosscompile tests) as the closing regression gate; `git status` check confirming only expected files (3 parsers' `main.go`/`go.mod`, regenerated `.wasm` + `SHA256SUMS`) changed.
- **Dependencies:** Task 01 (must be complete); should run after Task 02.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `internal/astgroup` (existing host-side Go test suite; `*_test.go` naming convention)

**Test File Placement Examples:** `internal/astgroup/embed_test.go` (`TestEmbeddedParsersMatchManifest`), `internal/astgroup/host_test.go`, `internal/astgroup/crosscompile_test.go` — none modified by this plan; all run as unchanged regression gates.

**Unit/Integration/E2E:**
- **Unit:** None added — `guestabi` is a wasip1-only package with no existing test harness pattern in the sibling parser modules; correctness is verified by successful `GOOS=wasip1 GOARCH=wasm` compilation, not new `_test.go` files.
- **Integration:** `go test ./internal/astgroup/...` run from the repo root after each parser is wired (goparser, then pyparser, then braceparser), confirming the existing host-side suite passes unchanged at each step rather than only once at the end.
- **E2E:** `build.sh` regenerates all ten vendored `.wasm` binaries and refreshes `SHA256SUMS` (Task 03); `TestEmbeddedParsersMatchManifest` gates any drift between the committed binaries and the manifest, the closest thing this plan has to an end-to-end check.

**Test Environment Status:**
- **Framework:** `go test` (project `TEST_COMMAND` = `go test ./...`); coverage baseline 80% per `.planning/.config/config.yaml`, unaffected here since `guestabi` and all three parsers are wasip1-only modules excluded from the root module's build/coverage instrumentation.
- **Execution:** `GOOS=wasip1 GOARCH=wasm go build`/`go vet` per module (existing per-parser pattern); `go test ./internal/astgroup/...` at the repo root for host-side regression after each wiring step.
- **Coverage Tools:** `go test -coverprofile=coverage.out ./...` (existing `COVERAGE_COMMAND`) — unaffected by this plan since none of the changed packages participate in root-module coverage instrumentation.

---

## Architecture

**Primitives:** unexported `pins map[int32][]byte`; guest pointer `int32`; packed `int64` (`ptr<<32 | len`) return convention; `any` (generalized `Emit` marshal input, replacing the per-parser `node` type).

**Module Boundaries:** New isolated `internal/astgroup/parsers/src/guestabi` module (own `go.mod`, wasip1-only, excluded from the parent module's `go build ./...`/`go test ./...`) exports `Alloc`, `Free`, `Lookup`, `Emit`. `//go:wasmexport alloc`/`free` must remain declared in each parser's own `package main` per Go's wasip1 reactor ABI — only the implementation bodies delegate to `guestabi`.

**External Dependencies:** Stdlib only (`unsafe`, `encoding/json`) — no new dependency at the parent module level. Each of the three parsers adds a local `require`+`replace` pointing at `../guestabi`, mirroring Epic 8.0's `reconcile` in-tree extraction pattern (`go.mod:37-41`).

**Replaceability:** `guestabi` is a clean swap point — a future moving-GC-safe allocation strategy would replace only `guestabi.go`'s internals; the `wasmexport` surface (`alloc`/`free`/`parse`) each parser exposes to the wazero host, and every parser's own `node` wire-contract type, are unaffected.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Guest/host pointer boundary (`pins` map) | `guestabi.Alloc`/`Free`/`Lookup`, called by all 3 parsers' `wasmexport` wrappers | A pointer-packing bug (e.g. a future Go GC that moves heap objects) could hand the host a stale/dangling guest offset, corrupting AST data crossing the wazero sandbox boundary | Non-moving-GC assumption pinned as a doc comment on `pins` in the single shared location (Task 01's core deliverable) — makes a future Go GC change a documented, discoverable review point instead of a silent memory-safety issue |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Wasm build regeneration (`build.sh`) | 10 `.wasm` binaries rebuilt once per this change (not per-request) | Same build time as today — extraction changes source only, not the compiled artifact's runtime behavior | No new optimization needed; Task 03 confirms `build.sh`'s existing per-directory `go build` invocation resolves the added `replace` directives without added latency |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Nested-module leakage | `guestabi` accidentally becomes a subpackage of the parent module instead of an isolated wasip1-only module | Root `go build ./...` must still exclude it; Task 01 Step 5 explicitly verifies this |
| Wrong `wasmexport` placement | `alloc`/`free` logic moved into `guestabi` instead of staying as thin wrappers in each parser's `package main` | Go's wasip1 reactor ABI requires `wasmexport` funcs live in `package main`; violating this breaks compilation, caught immediately by the per-parser `GOOS=wasip1` build in Tasks 02/03 |
| `pyparser` missing build tag | `pyparser/main.go` has no `//go:build wasip1` tag (unlike `goparser`/`braceparser`); a default-GOOS `go vet` would fail resolving the wasip1-tagged `guestabi` import | Task 02 explicitly requires `GOOS=wasip1 GOARCH=wasm` for `pyparser`'s vet step, not just its build |
| Stale vendored binaries | `.wasm` files committed without a matching regenerated `SHA256SUMS` (or vice versa) | `TestEmbeddedParsersMatchManifest` fails `go test ./...` on any drift; Task 03 Step 8's `git status` check confirms both are staged together |

### Defensive Measures Required

- **Input Validation:** N/A — no new external input surface; `Emit(v any)` is called only by trusted in-repo parser code, never externally supplied data.
- **Error Handling:** `Emit` retains today's existing `json.Marshal` failure fallback (`{"kind":"error","name":"marshal"}` sentinel) verbatim — no new error paths introduced.
- **Logging/Audit:** N/A — no logging surface exists in the guest ABI today; extraction preserves that.
- **Rate Limiting:** N/A — this is a compile-time/build-time concern, not a runtime request path.
- **Graceful Degradation:** N/A — either the wasm module builds and passes host round-trip tests, or the build fails outright; there is no partial-success state to reconcile.

---

## Risks

**Technical:**
- `go.mod` replace misconfiguration silently breaks one parser's wasm build → Build and `go vet` each parser individually immediately after wiring it (Tasks 02/03), not once at the end after all are touched.
- `Emit(v any)` generalization silently changes JSON output shape → Direct `json.Marshal(v)` passthrough is byte-identical to today's `json.Marshal(n)` for a `node`-shaped argument; verified by unchanged `host_test.go` round-trips.
- Vendored `.wasm`/`SHA256SUMS` committed out of sync → Task 03 Step 8's `git status` check plus `TestEmbeddedParsersMatchManifest` gate catch this before merge.

**TDD-Specific:**
- No unit-test harness exists for wasip1-only packages → Compile-time verification (`GOOS=wasip1 go build`/`go vet`) substitutes for unit tests at each step; existing `host_test.go`/`embed_test.go`/`crosscompile_test.go` serve as the regression safety net across all three tasks.
- Sequencing risk: wiring all three parsers before testing any could mask which one broke → Tasks mandate one-parser-at-a-time build+test before proceeding, carried into the phase structure above (Phase 2 tests `goparser` before starting `pyparser`).

---

**Next:** `/create-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/ --adversarial`
