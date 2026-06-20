# Code Review Report: 5.0_file_path_validation

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 6 / 7 (AC1–AC6 verified; AC7 intentionally out-of-scope)
- **Approval Status:** Approved
- **Review Date:** June 20, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Branch:** main (epic merged in f8084a3, PR #58)

## 2. Acceptance Criteria Results

| AC | Verdict | Evidence |
|----|---------|----------|
| AC1 — validate file existence after parsing | VERIFIED ✅ | `internal/reconcile/gate.go:225`, `internal/reconcile/validate.go:11-17` |
| AC2 — flag PathValid=false / PathWarning="file not found" | VERIFIED ✅ | `internal/stream/validate.go:13,34-70` |
| AC3 — reports display warning | VERIFIED ✅ | `internal/reconcile/emit.go:319-324`, `internal/report/render.go:314-320` |
| AC4 — findings preserved, not discarded | VERIFIED ✅ | flag-only stamping; tests assert problem text retained |
| AC5 — unit tests valid/invalid/typo/wrong-dir | VERIFIED ✅ | `internal/stream/validate_test.go:13-117` |
| AC6 — integration test, hallucinated path → report | VERIFIED ✅ | `internal/reconcile/validate_test.go:99-154` |
| AC7 — (Optional) fuzzy matching >90% | DEFERRED ❌ | Out-of-scope per epic Clarifications (line 200); captured in TD |

## 3. Evidence Map
- **Core check:** `internal/stream/validate.go` — `ValidatePath(f, root)` stats `filepath.Join(root, f.File)`, clamped to within root (rejects `..` traversal / absolute escape), distinguishes exists / not-found / indeterminate.
- **Pipeline integration:** `internal/reconcile/validate.go` `validateFindingPaths([]Merged, root)` runs in `RunReconcile` after `Reconcile` / before `Emit` (`gate.go:225`); `Reconcile` stays I/O-free; `Options.Root` defaults empty (disables validation for deterministic tests).
- **Production wiring:** CLI `reconcile`/`resume`/`review` and the MCP reconcile handler all pass `Root: "."`.
- **Surfacing:** `path_valid`/`path_warning` added to `JSONFinding` (omitempty → byte-identical findings.json for unflagged findings); warning line rendered in report.md (`emit.go`) and `atcr report` markdown/checklist/refuted views (`render.go`).

## 4. Remaining Unchecked Items
- **AC7 (fuzzy matching / path correction):** Intentionally deferred. The epic's own Clarifications and Open Questions select the safe default (correction behind a flag). Not a defect; captured as technical debt. No in-scope deliverables are unfinished.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All in-scope acceptance criteria implemented with strong, adversarial-grade unit + end-to-end tests. Implementation is well-documented (security/limitation contracts inline). No critical or high findings.

## 6. Coverage Analysis
- **Coverage:** 89.7% (total); epic packages: stream 94.8%, reconcile 90.8%, report 97.7%
- **Baseline:** 80%
- **Delta:** ↑9.7%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run (0 issues) |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt (only throwaway .planning/.temp/spike-2.0 files flagged; no epic source) |

## 8. Adversarial Analysis
- **Files Reviewed:** 11 source files changed by the epic
- **Issues Found:** 10 (Critical: 0, High: 0, Medium: 2, Low: 8)
- **Risk classification:** 0 anticipated/addressed, 0 anticipated/missed, 10 unanticipated (no epic risk profile)

### Confirmed NON-issues (reviewers tried and failed to break)
- findings.json round-trip preserves PathValid/PathWarning on re-read.
- verify re-emit (`computeReEmitFindingsBytes`) preserves both fields.
- `Reconcile` remains pure / I/O-free; validation correctly bolted onto `RunReconcile`.
- Markdown injection defenses (esc/codeSpan/newline flatten) hold across all report views.

### Issues by Severity
**MEDIUM**
- `internal/mcp/handlers.go:320` — handleReconcile hardcodes `Root: "."` instead of `e.root`; correct only because `serve` passes `"."`. Diverges from every other operation in the handler. Latent.
- `internal/reconcile/emit.go:85` — `PathValid bool,omitempty` conflates "validated-missing" with "never-validated"; documented as unreadable-in-isolation. Drop it or make `*bool`.

**LOW**
- `internal/stream/validate.go:59` — `os.Stat` follows symlinks; repo symlink + crafted File is an existence oracle outside root. Use `os.Lstat`/EvalSymlinks.
- `internal/stream/validate.go:59` — directory / `.` File stats as valid; reject `IsDir` after stat.
- `internal/stream/validate.go:67` — indeterminate stat error swallowed (no log); finding left silently unflagged.
- `internal/stream/validate.go:32` — case-insensitive FS (macOS/Windows) misses case-typo paths; documented + deferred, no test pins the gap.
- `internal/reconcile/validate.go:15` — no stat caching; duplicate paths stat'd N times.
- `internal/reconcile/gate.go:225` — stat loop runs in un-cancellable window before ctx check.
- `internal/report/render.go:320` — "File not found" literal duplicated across two renderers, third spelling vs stored constant.
- `cmd/atcr/serve.go:42` — MCP server validation hostage to client CWD (no repo-root resolution); links to the handlers.go:320 item.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/5.0_file_path_validation.md` to merge these 10 findings into the TD README, then `/resolve-td` for the hardening items.
- Highest-value quick win: `handlers.go:320` → `Root: e.root` (15 min, removes a standing trap).

---
*Generated by /execute-code-review on June 20, 2026 01:17:47PM*
