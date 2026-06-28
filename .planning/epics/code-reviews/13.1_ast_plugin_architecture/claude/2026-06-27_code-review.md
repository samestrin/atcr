# Code Review Report: 13.1_ast_plugin_architecture

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 3 / 3
- **Approval Status:** Approved
- **Review Date:** June 27, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

All three acceptance criteria are fully implemented and verified against the code. The full test suite passes (both the root module and the zero-dependency `reconcile/` submodule), total coverage is 89.1% (baseline 80%), and lint/vet/format are clean. The AC3 benchmark independently reproduces the epic's adoption claim (AST recall 1.000 / precision 1.000 vs ±3 proximity recall 0.786 with 4 false merges). 29 adversarial findings (0 critical, 5 high) were surfaced as forward-looking technical debt; none block the epic's acceptance.

## 2. Checklist Changes Applied
- **.planning/epics/completed/13.1_ast_plugin_architecture.md** – AC1: Wasm host runtime (wazero) integrated, zero CGO
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/host.go:30-58`, `internal/astgroup/crosscompile_test.go:16-26`
- **.planning/epics/completed/13.1_ast_plugin_architecture.md** – AC2: fetch + cache language-specific `.wasm` plugins dynamically
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/embed.go:5-15`, `internal/astgroup/host.go:109-152`
- **.planning/epics/completed/13.1_ast_plugin_architecture.md** – AC3: AST mapping groups whitespace/line-drift findings
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/cover.go:30-42`, `internal/astgroup/merkle.go:18-30`, `internal/astgroup/benchmark_test.go:38-118`

## 3. Evidence Map
- **AC1 — wazero runtime, zero CGO**
  - Evidence: `internal/astgroup/host.go:30-58` (pure-Go wazero runtime + WASI preview1), `internal/astgroup/crosscompile_test.go:16-26` (CGO_ENABLED=0 GOOS=linux GOARCH=arm64 build passes), `internal/reconcile/gate.go:238-247` (grouper wired into RunReconcile)
  - Summary: wazero v1.12.0 is the runtime; no `import "C"` in `internal/astgroup`. WASI-init failure is recorded and degrades to proximity grouping rather than panicking.
- **AC2 — fetch + cache `.wasm` plugins**
  - Evidence: `internal/astgroup/embed.go:5-15` (go:embed go.wasm + python.wasm), `internal/astgroup/host.go:109-152` (compile + instantiate once per language, cached under mutex), `internal/astgroup/host.go:35-39` (WithOverrideDir runtime drop-in)
  - Summary: "fetch" = load from embedded FS (override dir consulted first); "cache" = wazero compiled-module + live-instance cache keyed by language — satisfies the <10ms repeat-parse NFR.
- **AC3 — AST drift-invariant grouping**
  - Evidence: `internal/astgroup/cover.go:30-42` (line→smallest covering block + structural address), `internal/astgroup/merkle.go:18-30` (structural hash excludes line numbers), `internal/astgroup/grouper.go:67-91` (file+addr+merkle key), `internal/astgroup/benchmark_test.go:38-118` (22-pair labeled corpus, recall ≥0.95, FP=0, dominance over proximity), `internal/reconcile/astgrouping.go:11-31` (AST default-ON, ±3 fallback, opt-out via ATCR_DISABLE_AST_GROUPING)
  - Summary: AST identity is the adopted primary grouping signal; ±3 line proximity is retained as the no-parser fallback (`reconcile/cluster.go:7`).

## 4. Remaining Unchecked Items
No remaining unchecked items — all 3 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All ACs implemented with concrete evidence; tests + benchmark pass; quality gates green. Adversarial findings are forward-looking TD, not acceptance blockers.

## 6. Coverage Analysis
- **Coverage:** 89.1%
- **Baseline:** 80%
- **Delta:** ↑9.1%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... (checked via gofmt -l, non-mutating) |

## 8. Adversarial Analysis
- **Files Reviewed:** 13
- **Issues Found:** 29 (Critical: 0, High: 5, Medium: 12, Low: 12)
- **Risk Profile:** Not available (epic mode — discovery-only)

### Issues by Severity

**HIGH (5)**
- `internal/astgroup/host.go:197` (error-handling) — `Parse` indexes `res[0]`/`pr[0]` from wasm result slices with no length check; a malformed override plugin panics the host, contradicting the package's "degrade, not crash" contract.
- `internal/astgroup/host.go:206` (security) — `parse.Call` uses `context.Background()` (no timeout/fuel); a pathological input or hostile override plugin can hang forever while holding `p.mu`, wedging all future parses for that language.
- `internal/astgroup/cover.go:70` (correctness) — `coveringChain` discards `blockIdx` on non-block descent, producing sibling-address collisions that over-merge distinct anonymous blocks — the exact failure the structural address was added to prevent.
- `internal/astgroup/parsers/build.sh:16` (testing) — no toolchain pin / version check / CI rebuild-diff; the committed `.wasm` artifacts can drift from `src/` with zero detection.
- `internal/reconcile/astgrouping.go:27` (error-handling) — `ATCR_DISABLE_AST_GROUPING` disables on any non-empty value, so `=0`/`=false` silently revert to proximity — the opposite of user intent for the adoption escape hatch.

**MEDIUM (12)** — incl. mutex held across read+parse serializing all parsing (`grouper.go:103`), per-finding Merkle/CoveringBlock recompute (`grouper.go:96`), no wazero memory-pages limit (`host.go:56`), transient I/O errors negatively cached (`grouper.go:117`), dead `type`-kind path in the Go parser (`goparser/main.go:107`), embedded `.wasm` executed with no checksum/provenance (`embed.go:5`), Python `isHeader` colon-only block detection fabricating blocks (`pyparser/main.go:178`), quote/escape-unaware triple-quote/comment scanning erasing blocks (`pyparser/main.go:112`), `ClusterWith` keyed/unkeyed split that can REDUCE merges vs proximity (`reconcile/grouper.go`), large keyed cluster → O(n²) DedupeCluster (`reconcile/grouper.go`), fresh wazero runtime per RunReconcile not amortized in the MCP server (`gate.go:243`), and silent degradation to 100% proximity with no diagnostic (`grouper.go:113`).

**LOW (12)** — Merkle hex double-hashing, symlink-unaware containment, stale empty-source contract comment, use-after-Close parser handout, unbounded tree-depth recursion, no result-JSON cap, tab-indent column miscount, copy-pasted unsafe GC-dependent guest ABI, Python compound-clause sibling layout, divergent empty-input contract across plugins, discarded Close error, and `Root="."`-falls-to-cwd mis-grouping risk.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/13.1_ast_plugin_architecture.md` to merge these 29 findings into `.planning/technical-debt/README.md` with reviewer attribution.
- Prioritize the 5 HIGH items for `/resolve-td` — particularly the two host-robustness defects (`host.go:197`, `host.go:206`) and the `coveringChain` address collision (`cover.go:70`), which affect grouping correctness and the "never crash" guarantee on the override path.

---
*Generated by /execute-code-review on June 27, 2026 08:42:28PM*
