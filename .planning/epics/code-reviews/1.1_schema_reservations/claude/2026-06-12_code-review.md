# Code Review Report: 1.1_schema_reservations

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 12, 2026
- **Review Mode:** Epic (Success Criteria + Adversarial) + Tests
- **Scope:** epic merge commit `8377a2b` (source/doc surface; planning-doc churn excluded)
- **Note:** Epic uses `## Success Criteria` as its acceptance-criteria source (user-confirmed substitution for the script's `## Acceptance Criteria` gate).

## 2. Checklist Changes Applied
- **.planning/epics/completed/1.1_schema_reservations.md** – Registry loader parses/validates reserved fields, inert in 1.x
  - Before: `[ ]` → After: `[x]` — Evidence: `internal/registry/config.go:62-65,187-196`
- **.planning/epics/completed/1.1_schema_reservations.md** – findings.json documents `verification`; report renders identically
  - Before: `[ ]` → After: `[x]` — Evidence: `internal/reconcile/emit.go:25-52`, `internal/report/render_test.go`
- **.planning/epics/completed/1.1_schema_reservations.md** – manifest.json carries `stages: ["review"]`
  - Before: `[ ]` → After: `[x]` — Evidence: `internal/payload/manifest.go:29-32`, `internal/fanout/review.go:195`
- **.planning/epics/completed/1.1_schema_reservations.md** – Format docs list all reserved fields with owning epic
  - Before: `[ ]` → After: `[x]` — Evidence: `docs/registry.md`, `docs/findings-format.md`
- **.planning/epics/completed/1.1_schema_reservations.md** – No new dependencies; all tests green
  - Before: `[ ]` → After: `[x]` — Evidence: `go.mod`/`go.sum` unchanged; suite green @ 87.2%

## 3. Evidence Map
- **Registry reserved fields (Criterion 1)**
  - Evidence: `internal/registry/config.go:62-65` (fields), `internal/registry/config.go:187-196` (load-time validation), `internal/registry/config_test.go`
  - Summary: `Tools`/`MaxTurns`/`ToolBudgetBytes`/`Role` parse under strict YAML and type/range-validate at load; no `applyDefaults` entry, no v1 reader — inert.
- **findings.json `verification` (Criterion 2)**
  - Evidence: `internal/reconcile/emit.go:25-32,52`, `internal/report/render_test.go` (`TestRender_IdenticalWithAndWithoutVerification`)
  - Summary: Pointer+omitempty ⇒ absent in 1.x; render is byte-identical with/without the block.
- **manifest `stages` (Criterion 3)**
  - Evidence: `internal/payload/manifest.go:29-32`, `internal/fanout/review.go:195`
  - Summary: `["review"]` set at the single construction site and emitted non-omitted.
- **Docs (Criterion 4)**
  - Evidence: `docs/registry.md` reserved-fields table; `docs/findings-format.md` verification + companion-artifacts table
  - Summary: Every reserved field documented as parsed/inert with its owning future epic.
- **Deps + tests (Criterion 5)**
  - Evidence: `go.mod`/`go.sum` unchanged; full suite green @ 87.2% coverage
  - Summary: stdlib + existing types only.

## 4. Remaining Unchecked Items
No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Purely additive schema reservation; all 5 success criteria verified against merged code; adversarial trace confirmed the reserved fields are inert (no v1 code path reads them) and validate at the right load-time boundaries. Six LOW future-stage hygiene nits captured, none blocking.

## 6. Coverage Analysis
- **Coverage:** 87.2%
- **Baseline:** 80%
- **Delta:** ↑7.2%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 5
- **Issues Found:** 6 (Critical: 0, High: 0, Medium: 0, Low: 6)

### Issues by Severity
**LOW (6) — all future-stage reserved-field hygiene, no 1.x defect:**
1. `internal/registry/config.go:197` — `MaxTurns` has a lower bound but no upper bound (unlike `TimeoutSecs`/`MaxTimeoutSecs`); unbounded turn cap is a runaway-cost vector when Epic 2.0 activates.
2. `internal/registry/config.go:200` — `ToolBudgetBytes` accepts `0` with undocumented semantics (block-all vs unlimited), unlike `PayloadByteBudget` which documents `0=unlimited`.
3. `internal/registry/config.go:64` — `Tools` is a value `bool` while the struct comment claims pointer fields exist to distinguish unset-vs-explicit; rationale applied inconsistently (harmless, planned default false).
4. `internal/reconcile/emit.go:33` — comment cites a nonexistent `reconcile/load.go`; the real reader is `emit.go:155`.
5. `internal/reconcile/emit.go:37` — `Verification.Verdict` has no read-side enum validation and no omitempty; an empty verdict round-trips silently.
6. `internal/payload/manifest.go:32` — `Stages` uses `omitempty`; the "1.x must carry stages" guarantee rests on the writer, not the schema, and absent-stages default behavior is undocumented.

## 9. Follow-ups
- 6 LOW findings written to `code-review/claude/td-stream.txt`. Run `/reconcile-code-review @.planning/epics/completed/1.1_schema_reservations.md` to merge into the TD README (this run did not write the TD README — no `--write-td`).
- No code changes required; epic 1.1 is contract-safe for 1.x.

---
*Generated by /execute-code-review on June 12, 2026 12:36:32PM*
