# Code Review Stream - 13.1_ast_plugin_architecture (Epic)

**Started:** June 27, 2026 08:42:28PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — Wasm host runtime (wazero) integrated into the reconciler with zero CGO dependencies
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/host.go:30-58` (wazero.NewRuntime + WASI preview1, pure Go), `internal/astgroup/crosscompile_test.go:16-26` (CGO_ENABLED=0 GOOS=linux GOARCH=arm64 build passes), `internal/reconcile/gate.go:243-247` (grouper wired into RunReconcile pipeline)
- **Notes:** wazero v1.12.0 is the runtime; no `import "C"` anywhere in internal/astgroup. WASI-init failure is recorded (initErr) and degrades to proximity grouping instead of panicking. NOTE: go.mod lists wazero as `// indirect` despite the direct import in host.go — minor (flagged for adversarial review).

### Criterion: AC2 — Mechanism implemented to fetch and cache language-specific `.wasm` parser plugins dynamically
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/embed.go:5-6` (go:embed parsers/go.wasm parsers/python.wasm), `internal/astgroup/host.go:127-170` (Host.Parser compiles + instantiates once per language, caches in h.parsers), `internal/astgroup/host.go:35-39` (WithOverrideDir runtime drop-in path), `internal/astgroup/override_test.go`
- **Notes:** "Fetch" = load from embedded FS (or override dir first); "cache" = wazero compiled-module + instance cache keyed by lang under mutex. Both PoC parsers (go.wasm 3.8MB, python.wasm 3.0MB) vendored + embedded.

### Criterion: AC3 — AST mapping correctly groups findings that are offset by whitespace or minor line-number drift
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/cover.go:30-42` (CoveringBlock maps line→smallest covering block + structural address), `internal/astgroup/merkle.go:18-30` (MerkleHash folds kind/name/child structure but NOT line numbers), `internal/astgroup/grouper.go:67-91` (GroupKey = file+addr+merkle), `internal/astgroup/benchmark_test.go:38-118` (22 labeled pairs; asserts AST recall ≥0.95, precision FP=0, dominance over ±3 proximity), `internal/reconcile/gate.go:238-247` + `internal/reconcile/astgrouping.go:11-31` (AST adopted as PRIMARY signal, ±3 fallback, opt-out via ATCR_DISABLE_AST_GROUPING)
- **Notes:** AC3 default-flip adopted per epic adopt-or-revert gate. Benchmark pass confirmed in Phase 4 test run.


## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — discovery-only)
**Files Reviewed:** 13
**Issues Found:** 29 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 29

### Issues by Severity (verified)
- Critical: 0
- High: 5
- Medium: 12
- Low: 12

**Top HIGH findings:**
- `internal/astgroup/host.go:197` — unchecked `res[0]`/`pr[0]` from wasm result slices can panic the host on a malformed override plugin (contradicts the "degrade, not crash" contract).
- `internal/astgroup/host.go:206` — `parse.Call` uses `context.Background()`; no timeout/fuel — a pathological input or hostile plugin can hang forever holding `p.mu`.
- `internal/astgroup/cover.go:70` — `coveringChain` non-block descent discards `blockIdx`, producing sibling-address collisions that over-merge distinct anonymous blocks (defeats the address mechanism's purpose).
- `internal/astgroup/parsers/build.sh:16` — no toolchain pin / version check / CI rebuild-diff; committed `.wasm` can drift from `src/` silently.
- `internal/reconcile/astgrouping.go:27` — `ATCR_DISABLE_AST_GROUPING` is truthiness-by-presence: `=0`/`=false` disables, the opposite of user intent.
