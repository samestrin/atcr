# Sprint 22.2: Shared Guest ABI Extraction

---
executor: /execute-sprint
execution_mode: continuous
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 22.2 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Extraction of the duplicated Wasm guest ABI boilerplate — the `pins` map plus `alloc`/`free`/`emit` implementations — currently copy-pasted across `goparser`, `pyparser`, and `braceparser`, into one shared internal `guestabi` Go module. Each parser is rewired to import the shared module via a `go.mod` `require`+`replace` pair and thin `//go:wasmexport` wrappers, exactly mirroring the pattern already proven by Epic 8.0's `reconcile` extraction.

### Why This Matters

Three separate technical-debt rows (from sprints 13.1, 13.3, 13.4) flag the same duplication, each explicitly noting "revisit if parser count grows" — a threshold now crossed by the addition of `braceparser`. The duplicated code also carries an unstated non-moving-GC pointer-packing assumption, repeated per-parser instead of documented once in an authoritative location.

### Key Deliverables

- New isolated `internal/astgroup/parsers/src/guestabi` module exporting `Alloc`, `Free`, `Lookup`, `Emit`, with the non-moving-GC assumption pinned as a single doc comment (Task 01).
- `goparser` and `pyparser` rewired to import and delegate to `guestabi`, local ABI blocks removed (Task 02).
- `braceparser` rewired identically, all ten vendored `.wasm` binaries + `SHA256SUMS` regenerated, full `internal/astgroup` regression suite passing (Task 03).

### Success Criteria

- `goparser`, `pyparser`, and `braceparser` each import the shared `guestabi` package instead of defining their own alloc/free/emit/pins boilerplate (AC1).
- The non-moving-GC pointer-packing assumption is documented once, in `guestabi`, not repeated per-parser (AC2).
- `go build ./...` succeeds for all three parser Wasm modules; existing `internal/astgroup` tests pass unchanged (AC3).

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Plan Type:** 🔧 Tech-Debt (TASK-BASED) — no new `*_test.go` surface is created; correctness is verified via `GOOS=wasip1 GOARCH=wasm` compile checks plus the existing `internal/astgroup` regression suite run unchanged.

- **Verification model:** `PRIMARY_TEST_LOCATION` is `internal/astgroup` (existing host-side Go test suite). No unit-test harness exists for wasip1-only packages in the sibling parser modules, so compile-time verification substitutes for unit tests at each step (per the design's TDD-Specific risk mitigation).
- **Substitute verification per task:**
  - Task 01: `GOOS=wasip1 GOARCH=wasm go build`/`go vet` inside the new module + root `go build ./...` exclusion check + unchanged `go test ./internal/astgroup/...`.
  - Task 02: `GOOS=wasip1 GOARCH=wasm go build`/`go vet` run individually per parser (goparser fully built+tested before pyparser starts) + root regression checks.
  - Task 03: isolated single-tag smoke build before the full `build.sh`, then the full `internal/astgroup` suite (embed, host, crosscompile tests) as the closing regression gate, plus a `git status` check confirming only expected files changed.
- **Adversarial review:** ENABLED 🎯 — a fresh subagent reviews each task's changed files (inline-fix bar: **CRITICAL/HIGH**; defer **MEDIUM/LOW** to tech debt).
- **Gated execution:** Disabled — sprint runs continuously; no phase-boundary gates.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [tasks/](plan/tasks/) | The 3 tech-debt tasks this sprint executes |
| [documentation/](plan/documentation/) | Documentation source index (no specs matched this plan) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `GOOS=wasip1 GOARCH=wasm go build ./...` (inside the changed module) |
| T2: Module | After completing a task | `GOOS=wasip1 GOARCH=wasm go vet ./...` (inside the changed module) + `go test ./internal/astgroup/...` |
| T3: Full | DoD validation, pre-commit | `go build ./...` + `go test ./internal/astgroup/...` at repo root |

**Coverage:** `go test -coverprofile=coverage.out ./...` (existing `COVERAGE_COMMAND`, ≥80% baseline) — unaffected by this plan since `guestabi` and all three parsers are wasip1-only modules excluded from root-module coverage instrumentation.

### DoD Verification Checklist

1. Task Success Criteria: all checked
2. Tests (T3): `go test ./internal/astgroup/...` all passing
3. Build: `go build ./...` succeeds at repo root; `GOOS=wasip1 GOARCH=wasm go build ./...` succeeds per touched module
4. No scope creep: only the task's declared files touched
5. Adversarial review passed / findings resolved

### DoD Report Template

```
Phase-{N} DoD Complete
Auto: {X}/5 | Task-Specific: {Y}/{Z}
Manual Review: [ ] Artifact reviewed against original-requirements.md
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

**Coding Standards** (from `.planning/specifications/coding-standards.md`): Conventional Commits (`type(scope): description`), `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...`; error wrapping via `fmt.Errorf("...: %w", err)`; table-driven tests where applicable.

**Git Strategy** (from `.planning/specifications/git-strategy.md`): GitHub Flow — short-lived `feature/` branch, atomic Conventional Commits, PR against `main`, squash-and-merge.

**Implementation Standards** (from `.planning/specifications/implementation-standards.md`): Black-box interfaces — `guestabi` exposes `Alloc`/`Free`/`Lookup`/`Emit` only, hiding the `pins` map entirely; replaceable component — a future moving-GC-safe allocation strategy would replace only `guestabi.go`'s internals without touching any parser's `wasmexport` surface.

---

## External Resources

None — no specifications in `.planning/specifications/` matched this plan (semantic search returned 0 results at threshold 0.7; manual grep for wasm/astgroup/parser terms also found no matches). See [plan/documentation/source.md](plan/documentation/source.md).

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Create Shared guestabi Module (~0.3 day)

### 1.1 [x] **🔧 Create Shared guestabi Module**
   **Task:** [task-01-create-shared-guestabi-module.md](plan/tasks/task-01-create-shared-guestabi-module.md) (AC1 foundation, AC2)
   **Priority:** P2 | **Effort:** S | **Type:** Refactor
   1. Create the module directory `internal/astgroup/parsers/src/guestabi/` and write `go.mod` (module path `github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi`, `go 1.26`, isolated-module explanatory header comment mirroring `goparser/go.mod`).
   2. Write `guestabi.go`: `//go:build wasip1`, `package guestabi`, unexported `pins map[int32][]byte` carrying the non-moving-GC pointer-packing doc comment (adapted from `goparser/main.go:41-51` to describe this as the single extracted copy), exported `Alloc(n int32) int32`, `Free(p int32)`, `Lookup(p int32) ([]byte, bool)`, `Emit(v any) int64` (generalized `json.Marshal` passthrough with the existing `{"kind":"error","name":"marshal"}` fallback). No `//go:wasmexport` directive in this file.
   3. Run `GOOS=wasip1 GOARCH=wasm go build ./...` and `GOOS=wasip1 GOARCH=wasm go vet ./...` from inside the new module (T1/T2) — required env, since the default GOOS skips the `//go:build wasip1` file entirely.
   4. Run `go build ./...` from the repo root to confirm the new nested module is excluded from the parent build.
   5. Run `go test ./internal/astgroup/...` from the repo root to confirm no regression (no parser `main.go` touched yet).
   **Success Criteria:** `guestabi/go.mod` and `guestabi.go` exist as specified; `Lookup` is the only exported read-back path (`pins` stays unexported); non-moving-GC assumption documented once; `Emit` accepts `any`; wasip1 build/vet succeed inside the module; root `go build ./...`/`go test ./internal/astgroup/...` unaffected; no parser files touched.
   **Files:** `internal/astgroup/parsers/src/guestabi/go.mod` (create), `internal/astgroup/parsers/src/guestabi/guestabi.go` (create) | **Duration:** ~0.3 day
   6. COMMIT: `git add internal/astgroup/parsers/src/guestabi/go.mod internal/astgroup/parsers/src/guestabi/guestabi.go && git commit -m "refactor(astgroup): create shared guestabi module (task 01)"`

### 1.1.A [x] **Task 01 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/astgroup/parsers/src/guestabi/go.mod`, `internal/astgroup/parsers/src/guestabi/guestabi.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/guestabi/go.mod`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/guestabi/guestabi.go`
     - Ground-truth to verify against: `internal/astgroup/parsers/src/goparser/main.go` (lines 39-66, 189-197), `internal/astgroup/parsers/src/goparser/go.mod`, root `go.mod:37-41` (reconcile isolation precedent)
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - ISOLATION: Does the new module's `go.mod` correctly exclude it from the parent module's `go build ./...`/`go test ./...`, matching the sibling parsers' isolation pattern?
       - CONTRACT FIDELITY: Does `Emit(v any)` produce byte-identical JSON output to the original `emit(n node) int64` for a `node`-shaped argument?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | guestabi.go:47 | `Lookup` returns raw pinned buffer without the `int(n) > len(buf)` guard; future callers not re-validating n could trap. All 3 current parse() call sites retain the guard, so no live defect. | Doc note or `Lookup(p, n)` variant. Captured as TD-001. |
   | LOW | guestabi.go:65 | `(int64(p) << 32)` sign-extends for high-bit guest pointers (>= 2GB). Inherited byte-for-byte from original emit; not a regression. | Mask via `int64(uint32(p))`. Captured as TD-002. |
   | LOW | guestabi.go:63 | `Emit` pins a result buffer per call, relies on host to free resPtr; pins can grow unbounded. Inherited from original. | Document host free obligation. Captured as TD-003. |

   **Action taken:** No CRITICAL/HIGH. All findings inherited from the original per-parser ABI or by-design (Lookup mirrors the comma-ok index per task-01 spec). MEDIUM/LOW deferred to `tech-debt-captured.md` (TD-001/002/003) per the CRITICAL/HIGH inline-fix bar. Contract fidelity (Emit vs emit), isolation (nested go.mod excludes from parent), and security verified with no findings. Proceeding.

### 1.1.R [x] **Task 01 — REFACTOR / Address Findings** (no CRITICAL/HIGH; no code change)
   1. Fix CRITICAL/HIGH issues from 1.1.A (if any)
   2. Improve code quality (T1); re-run `GOOS=wasip1 GOARCH=wasm go build`/`go vet` inside the module
   3. Validate `go test ./internal/astgroup/...` still passes (T3)
   4. COMMIT (only if changes made): `git add internal/astgroup/parsers/src/guestabi/ && git commit -m "refactor(astgroup): address review findings (task 01)"`
   **Duration:** ~15-30 min

### 1.2 [x] **Phase 1 — DoD Verification**
   - [x] Task 01 Success Criteria all checked
   - [x] `GOOS=wasip1 GOARCH=wasm go build ./...` and `go vet ./...` succeed inside `internal/astgroup/parsers/src/guestabi/`
   - [x] `go build ./...` succeeds at repo root with the new module present but excluded
   - [x] `go test ./internal/astgroup/...` passes unchanged
   - [x] No parser `main.go` or `go.mod` files modified this phase
   - [x] Adversarial review passed / findings resolved (MEDIUM/LOW deferred to TD)
   **DoD Report:** emit Phase-1 DoD Report per template.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Integration — Wire goparser & pyparser to guestabi (~0.4 day)

### 2.1 [x] **🔧 Wire goparser and pyparser to guestabi**
   **Task:** [task-02-wire-goparser-pyparser-to-guestabi.md](plan/tasks/task-02-wire-goparser-pyparser-to-guestabi.md) (AC1)
   **Priority:** P2 | **Effort:** M | **Type:** Refactor
   1. Add `require`+`replace => ../guestabi` to `goparser/go.mod`, mirroring root `go.mod:37-41`. Confirm `guestabi`'s actual exported API/module path against Task 01's output before writing wrapper code.
   2. In `goparser/main.go`: delete the local `pins`/`alloc`/`free`/`emit` block; add the `guestabi` import (drop now-unused `unsafe`/`encoding/json`); add thin `//go:wasmexport alloc`/`free` wrappers delegating to `guestabi.Alloc`/`guestabi.Free`; switch `parse()`'s buffer lookup to `guestabi.Lookup(ptr)`; replace `emit` with a one-line delegate to `guestabi.Emit(n)`.
   3. Build and test `goparser` in isolation: `cd internal/astgroup/parsers/src/goparser && GOOS=wasip1 GOARCH=wasm go build ./... && GOOS=wasip1 GOARCH=wasm go vet ./...`. Fix any issue before touching `pyparser`.
   4. Repeat steps 1-2 for `pyparser/go.mod` and `pyparser/main.go` (same transformation; keep `"strings"` import; note `pyparser/main.go` has no `//go:build wasip1` tag, so the vet step below MUST run under `GOOS=wasip1` explicitly).
   5. Build and test `pyparser` in isolation: `cd internal/astgroup/parsers/src/pyparser && GOOS=wasip1 GOARCH=wasm go build ./... && GOOS=wasip1 GOARCH=wasm go vet ./...`.
   6. Confirm no regression from the repo root: `go build ./...` and `go test ./internal/astgroup/...`. `braceparser` stays untouched.
   **Success Criteria:** `goparser`/`pyparser` no longer define local ABI implementations; both `go.mod` carry the `require`+`replace` pair; `GOOS=wasip1 GOARCH=wasm go build ./...` succeeds independently in both; `braceparser` untouched; root `go build ./...`/`go test ./internal/astgroup/...` pass unchanged.
   **Files:** `internal/astgroup/parsers/src/goparser/go.mod`, `internal/astgroup/parsers/src/goparser/main.go`, `internal/astgroup/parsers/src/pyparser/go.mod`, `internal/astgroup/parsers/src/pyparser/main.go` | **Duration:** ~0.4 day
   7. COMMIT: `git add internal/astgroup/parsers/src/goparser/go.mod internal/astgroup/parsers/src/goparser/main.go internal/astgroup/parsers/src/pyparser/go.mod internal/astgroup/parsers/src/pyparser/main.go && git commit -m "refactor(astgroup): wire goparser and pyparser to guestabi (task 02)"`

### 2.1.A [x] **Task 02 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/astgroup/parsers/src/goparser/go.mod`, `internal/astgroup/parsers/src/goparser/main.go`, `internal/astgroup/parsers/src/pyparser/go.mod`, `internal/astgroup/parsers/src/pyparser/main.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/goparser/go.mod`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/goparser/main.go`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/pyparser/go.mod`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/pyparser/main.go`
     - Ground-truth to verify against: `internal/astgroup/parsers/src/guestabi/guestabi.go` (Task 01's actual exported API), root `go.mod:37-41`
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - WASMEXPORT PLACEMENT: Do `alloc`/`free` remain declared in `package main` (Go's wasip1 reactor ABI requirement), with only implementation bodies delegating to `guestabi`?
       - BUILD-TAG CORRECTNESS: Was `pyparser`'s vet step run under `GOOS=wasip1` given it lacks a `//go:build wasip1` tag?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | pyparser/main.go:1 | No `//go:build wasip1` tag; importing wasip1-tagged guestabi makes the module uncompilable under host GOOS (asymmetric with goparser/braceparser). build.sh/CI (always wasip1) + parent `./...` exclusion unaffected. | Add `//go:build wasip1` first line. Captured as TD-004. |
   | LOW | goparser/main.go:52, pyparser/main.go:44 | Negative n bypasses `int(n) > len(buf)` guard → `buf[:n]` panics. Pre-existing; braceparser already guards `n < 0`. | Add `int(n) < 0` to guard. Captured as TD-005. |

   **Action taken:** No CRITICAL/HIGH. WASMEXPORT placement, import hygiene (unsafe/encoding/json removed, strings retained), require+replace precedent, and emit/lookup behavior parity all verified correct. MEDIUM (newly-introduced but no supported build path or success criterion affected) + LOW (pre-existing) deferred to `tech-debt-captured.md` (TD-004/005) per the CRITICAL/HIGH inline-fix bar. Proceeding.

### 2.1.R [x] **Task 02 — REFACTOR / Address Findings** (no CRITICAL/HIGH; no code change)
   1. Fix CRITICAL/HIGH issues from 2.1.A (if any)
   2. Improve code quality (T1); re-run `GOOS=wasip1 GOARCH=wasm go build`/`go vet` for both parsers
   3. Validate `go test ./internal/astgroup/...` still passes (T3)
   4. COMMIT (only if changes made): `git add internal/astgroup/parsers/src/goparser/ internal/astgroup/parsers/src/pyparser/ && git commit -m "refactor(astgroup): address review findings (task 02)"`
   **Duration:** ~15-30 min

### 2.2 [x] **Phase 2 — DoD Verification**
   - [x] Task 02 Success Criteria all checked
   - [x] `GOOS=wasip1 GOARCH=wasm go build ./...` succeeds independently in both `goparser` and `pyparser`
   - [x] `braceparser` untouched
   - [x] Root `go build ./...` and `go test ./internal/astgroup/...` pass unchanged
   - [x] Adversarial review passed / findings resolved (MEDIUM/LOW deferred to TD)
   **DoD Report:** emit Phase-2 DoD Report per template.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/tasks/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Completion & Validation — Wire braceparser, Rebuild, Full Verify (~0.3 day)

### 3.1 [x] **🔧 Wire braceparser to guestabi and verify full build**
   **Task:** [task-03-wire-braceparser-and-verify-build.md](plan/tasks/task-03-wire-braceparser-and-verify-build.md) (AC1, AC2, AC3)
   **Priority:** P2 | **Effort:** S | **Type:** Refactor
   1. Add `require`+`replace => ../guestabi` to `braceparser/go.mod`, mirroring Task 02's pattern.
   2. In `braceparser/main.go`: remove the local `pins`/`alloc`/`free`/`emit` block; add thin `//go:wasmexport alloc`/`free` wrappers delegating to `guestabi`; replace `emit(...)` call sites with `guestabi.Emit(...)`; switch `parse()`'s buffer lookup to `guestabi.Lookup(ptr)`; update imports (drop `unsafe`/`encoding/json`, add `guestabi`); trim the now-resolved technical-debt paragraph from the doc comment.
   3. Build and test `braceparser` in isolation: single-tag smoke build (`GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -tags ts -o /tmp/braceparser-ts.wasm .`) before running the full `build.sh`.
   4. Regenerate all ten vendored `.wasm` binaries and refresh `SHA256SUMS` via `internal/astgroup/parsers/build.sh` from the repo root; confirm the build log lists all ten targets (go, python, ts, php, rust, bash, java, kotlin, cpp, csharp).
   5. Run the full `internal/astgroup` suite unchanged: `go test ./internal/astgroup/...` (must include `embed_test.go`'s `TestEmbeddedParsersMatchManifest`, `host_test.go`, `crosscompile_test.go`).
   6. Run `go build ./...` and `go vet ./...` from the repo root.
   7. Verify no drift: `git status` should show only the three parsers' `main.go`/`go.mod` plus regenerated `.wasm`/`SHA256SUMS` as diffs — no test files touched.
   **Success Criteria:** `braceparser` no longer defines local ABI; `go build ./...` succeeds for all three parser Wasm modules; `build.sh` produces all ten binaries + refreshed `SHA256SUMS`; `TestEmbeddedParsersMatchManifest` and the full `internal/astgroup` suite pass unchanged; all three plan acceptance criteria satisfied.
   **Files:** `internal/astgroup/parsers/src/braceparser/main.go`, `internal/astgroup/parsers/src/braceparser/go.mod`, `internal/astgroup/parsers/build.sh` (optional doc comment), `internal/astgroup/parsers/*.wasm`, `internal/astgroup/parsers/SHA256SUMS` | **Duration:** ~0.3 day
   8. COMMIT: `git add internal/astgroup/parsers/src/braceparser/ internal/astgroup/parsers/*.wasm internal/astgroup/parsers/SHA256SUMS internal/astgroup/parsers/build.sh && git commit -m "refactor(astgroup): wire braceparser to guestabi and rebuild wasm binaries (task 03)"`

### 3.1.A [x] **Task 03 — ADVERSARIAL REVIEW (subagent)**
   **Changed Files:** `internal/astgroup/parsers/src/braceparser/main.go`, `internal/astgroup/parsers/src/braceparser/go.mod`, regenerated `.wasm` binaries, `SHA256SUMS`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.1`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/braceparser/main.go`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/src/braceparser/go.mod`, `/Users/samestrin/Documents/GitHub/atcr/internal/astgroup/parsers/SHA256SUMS`
     - Ground-truth to verify against: `internal/astgroup/parsers/src/goparser/main.go` (Task 02's already-wired pattern), `internal/astgroup/embed_test.go`
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - DRIFT: Are all ten `.wasm` binaries and `SHA256SUMS` regenerated together with no stale entries?
       - CONSISTENCY: Does `braceparser`'s wiring match `goparser`/`pyparser`'s Task 02 pattern exactly (no divergent naming or structure)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings:**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | braceparser/main.go:38,46,48 | braceparser inlines `guestabi.Emit(...)` at call sites; goparser/pyparser keep a local `emit` delegate. Functionally identical; each parser followed its own task spec. | Converge on one style. Captured as TD-006. |

   **Action taken:** No CRITICAL/HIGH. DRIFT verified — all ten `.wasm` present in `SHA256SUMS`, checksums match committed binaries (reviewer recomputed), no stale entries. CONSISTENCY of import/wrapper/Lookup/Emit-delegation verified; the one LOW divergence is the plan-prescribed emit style difference, deferred to `tech-debt-captured.md` (TD-006) per the CRITICAL/HIGH inline-fix bar. Import hygiene clean; sibling files compile. Proceeding.

### 3.1.R [x] **Task 03 — REFACTOR / Address Findings** (no CRITICAL/HIGH; no code change)
   1. Fix CRITICAL/HIGH issues from 3.1.A (if any)
   2. Improve code quality (T1); if any fix touches source, re-run `build.sh` and the full `internal/astgroup` suite (T3)
   3. COMMIT (only if changes made): `git add internal/astgroup/parsers/src/braceparser/ internal/astgroup/parsers/*.wasm internal/astgroup/parsers/SHA256SUMS && git commit -m "refactor(astgroup): address review findings (task 03)"`
   **Duration:** ~15-30 min

### 3.2 [x] **Phase 3 — DoD Verification**
   - [x] Task 03 Success Criteria all checked
   - [x] `go build ./...` succeeds for all three parser Wasm modules
   - [x] `build.sh` produced all ten `.wasm` binaries + refreshed `SHA256SUMS`
   - [x] `TestEmbeddedParsersMatchManifest` and the full `internal/astgroup` suite pass unchanged
   - [x] `git status` shows only expected files changed (no test files touched)
   - [x] Adversarial review passed / findings resolved (LOW deferred to TD)
   **DoD Report:** emit Phase-3 DoD Report per template.

---

## Final Phase: Validation

### Validation Checklist
- [x] All tests passing (T3): `go test ./internal/astgroup/...` (full `go test ./...` also green)
- [x] Coverage meets threshold (88.9% total ≥ 80%; wasip1-only modules excluded from root coverage instrumentation as expected)
- [x] Lint/format clean: `golangci-lint run` (0 issues), `gofmt -l` (clean)
- [x] Build succeeds: `go build ./...`, `go vet ./...` (both clean)

### Optional: Targeted Mutation Testing
Mutation testing tooling is UNAVAILABLE in this environment (no `stryker-mutator` in package.json, no `mutmut`/`cargo-mutants` on PATH — this is a Go project). Skip.

### Drift Analysis
Compare final state against `plan/original-requirements.md`:
- All three parsers import the shared `guestabi` package (AC1) — confirm via Task 02/03 completion.
- Non-moving-GC assumption documented once in `guestabi` (AC2) — confirm via Task 01 completion.
- `go build ./...` succeeds; `internal/astgroup` tests pass unchanged (AC3) — confirm via Task 03's closing regression run.
- No scope creep: moving GC / arena allocation strategy changes and parser-specific structural-hash logic (pyparser quote-awareness, epic 22.3) remain untouched, per Out of Scope.
