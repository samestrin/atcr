# Code Review Report: 24.0_false_positive_wontfix_dismissal

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 4 / 4
- **Approval Status:** Approved
- **Review Date:** July 14, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial)

## 2. Checklist Changes Applied

- **.planning/epics/completed/24.0_false_positive_wontfix_dismissal.md** – AC#1: `--resolve <id> --status wontfix --reason` marks wontfix + records Justification
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/debt_resolve.go:57-58`, `cmd/atcr/debt_resolve.go:254-265`
- **.planning/epics/completed/24.0_false_positive_wontfix_dismissal.md** – AC#2: wontfix finding absent from `--list`/`--json`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/debt_resolve.go:103-110`, `cmd/atcr/debt_resolve.go:132-133`
- **.planning/epics/completed/24.0_false_positive_wontfix_dismissal.md** – AC#3: wontfix finding not re-appended by `atcr reconcile`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/reconcile.go:199-207`, `cmd/atcr/reconcile.go:253-256`
- **.planning/epics/completed/24.0_false_positive_wontfix_dismissal.md** – AC#4: dismissal state durable across runs
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/debt_resolve.go:266`, `internal/localdebt/doc.go:56`

## 3. Evidence Map

- **AC#1 — mark wontfix + record reason**
  - Evidence: `cmd/atcr/debt_resolve.go:32` (resolveStatuses={resolved,wontfix}), `cmd/atcr/debt_resolve.go:72-77` (validate → markDebtResolved), `cmd/atcr/debt_resolve.go:254-265` (stamps Status + Justification)
  - Summary: `--status wontfix` is an accepted terminal enum value; `--reason` populates the previously-unset `Justification` field when non-empty, preserving prior justification otherwise.
- **AC#2 — fold out of open views**
  - Evidence: `cmd/atcr/debt_resolve.go:103-110` (isClosedStatus recognizes wontfix), `cmd/atcr/debt_resolve.go:117-165` (selectOpenDebt fold)
  - Summary: Both `renderResolveList` and `renderResolveJSON` consume selectOpenDebt, so `--list` and `--json` both exclude wontfix items.
- **AC#3 — no re-append on reconcile**
  - Evidence: `cmd/atcr/reconcile.go:198-257` (persistLocalDebt dedup seeded from full-history ReadAll incl. terminal records, skip when seen)
  - Summary: Dedup is by `history.FindingID(file,line,problem)`; the wontfix finding's id is already present, so re-detection is skipped. Regression-locked by `TestPersistLocalDebt_WontfixSuppressesReappend`.
- **AC#4 — durable across runs**
  - Evidence: `cmd/atcr/debt_resolve.go:254-266` (append terminal record via localdebt.Append), `internal/localdebt/doc.go:56` (append-only store)
  - Summary: Status + justification are persisted durably in `.atcr/debt/` shards and re-read by ReadAll on every subsequent run.

## 4. Remaining Unchecked Items

No remaining unchecked items - all 4 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria are backed by real implementation and locked by tests (RED→GREEN commits present). The implementation is minimal and reuses the existing resolve/isClosedStatus/persistLocalDebt machinery exactly as the refined plan intended. Adversarial findings are all MEDIUM/LOW hardening items, none blocking.

## 6. Coverage Analysis
- **Coverage:** 85.1% (cmd/atcr, the changed component)
- **Baseline:** 80%
- **Delta:** ↑5.1%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 3
- **Issues Found:** 7 (Critical: 0, High: 0, Medium: 2, Low: 5)
- **Excluded (already deferred by design):** 1 — line-drift resets wontfix (epic Out of Scope, line 36; AST-hash matching deferred to a follow-up epic)

### Issues by Severity

**MEDIUM**
- `cmd/atcr/debt_resolve.go:240` (correctness) — Concurrent `--resolve` calls with different statuses (resolved vs wontfix) can each append a divergent terminal record for one id; the no-lock TD-004 comment still assumes identical duplicates, so the audit trail can disagree on fixed-vs-dismissed. Downstream status reader is not deterministic.
- `cmd/atcr/reconcile.go:205` (correctness) — persistLocalDebt dedup is status-blind: a previously `resolved` finding that genuinely regresses (same id) is silently dropped and never re-surfaced. Intended for wontfix, but wrong for resolved.

**LOW**
- `cmd/atcr/reconcile_test.go:569` (testing) — AC#3 test asserts `Status == wontfix` but suppression is by id-presence, independent of status; the test would still pass if wontfix were removed from isClosedStatus. Attribution overstates what is locked (the `--list` fold-out is locked by a separate test).
- `cmd/atcr/debt_resolve.go:247` (maintainability) — "already resolved" message is hardcoded; when the existing terminal is wontfix the message is factually wrong.
- `cmd/atcr/debt_resolve.go:263` (maintainability) — `--reason` overwrites `Justification` for both statuses, silently discarding prior reconcile-enrichment justification on a plain `--status resolved`.
- `cmd/atcr/debt_resolve.go:77` (error-handling) — `--status wontfix` accepted with no reason and no prior justification, producing an unauditable reasonless dismissal — counter to the epic's premise.
- `internal/localdebt/store.go:67` (performance) — Append-only store has no compaction; every command re-reads and folds full history (O(total-records)), with unbounded shard growth over many resolve cycles.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/24.0_false_positive_wontfix_dismissal.md` to merge these 7 findings into the TD README with reviewer attribution.
- The 2 MEDIUM correctness items (divergent concurrent status, status-blind resolved dedup) are the strongest candidates for a follow-up hardening pass; both are edge/pre-existing, not blocking.

---
*Generated by /execute-code-review on July 14, 2026 09:23:17AM*
