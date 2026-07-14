# Code Review Stream - 24.0_false_positive_wontfix_dismissal (Epic)

**Started:** July 14, 2026 09:23:17AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `atcr debt resolve --resolve <id> --status wontfix --reason "<text>"` marks a finding wontfix/false-positive and records the reason in `Justification`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt_resolve.go:57-58` (flags), `cmd/atcr/debt_resolve.go:72-77` (status validation → markDebtResolved), `cmd/atcr/debt_resolve.go:254-265` (stamps `rec.Status = status`, `rec.Justification = reason`)
- **Notes:** `resolveStatuses` enum accepts `resolved|wontfix` (:32). `--reason` populates `Justification` only when non-empty, preserving prior justification otherwise. Locked by debt_resolve_test.go.

### Criterion: A `wontfix` finding does not appear in `atcr debt resolve --list` (or `--json`) on subsequent runs.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt_resolve.go:103-110` (`isClosedStatus` recognizes `wontfix`), `cmd/atcr/debt_resolve.go:132-133` (selectOpenDebt folds terminal-status ids out of open backlog)
- **Notes:** Both `--list` (renderResolveList) and `--json` (renderResolveJSON) consume selectOpenDebt output, so both views fold out wontfix. Locked by "wontfix folds out of open list" test.

### Criterion: A `wontfix` finding is not re-appended to the local TD store when `atcr reconcile` re-detects the same finding (same `FindingID`).
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/reconcile.go:199-207` (persistLocalDebt seeds `seen` from full-history ReadAll incl. terminal records), `cmd/atcr/reconcile.go:253-256` (StampID then skip if `seen[rec.ID]`)
- **Notes:** Dedup is by `history.FindingID(file,line,problem)`; the wontfix finding's id is already present in the store, so reconcile skips re-append. Fails open to append only on a dedup-read error. Locked by reconcile_test.go (AC#3 test, +39 lines).

### Criterion: The dismissal state (status + reason) is preserved across runs — durable in the append-only `.atcr/debt/` store.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/debt_resolve.go:266` (markDebtResolved appends via `localdebt.Append`), `internal/localdebt/doc.go:56` (append-only store), `cmd/atcr/debt_resolve.go:254-259` (RunID/Timestamp/Status/ResolvedAt stamped)
- **Notes:** The dismissal is a durable append-only record in `.atcr/debt/` shards; status+justification survive process restarts and are re-read by ReadAll on every subsequent run.

## Adversarial Analysis (Discovery Mode)

**Mode:** Discovery-only (no sprint-design.md risk profile — epic mode)
**Files Reviewed:** 3 (cmd/atcr/debt_resolve.go, debt_resolve_test.go, reconcile_test.go)
**Issues Found:** 7 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 7

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 5

### Excluded (confirmed real, but already deferred by design)
- **Line-drift resets wontfix** (record.go:62, StampID line-sensitive id): re-detecting a wontfixed finding after its line number shifts hashes to a new id and re-appears. This is explicitly named in the epic's Out of Scope (line 36) — AST-structural-hash matching is deferred to a follow-up epic. Not persisted as new TD.

### Security / clean dimensions (no finding)
- Security clean: `--reason` is free text but json-marshaled into JSONL (no injection); paths scrubbed via basePathErr; no secrets handled.
- The `--status`/`--reason`-without-`--resolve` guard (debt_resolve.go:69) and invalid-`--status` rejection (:74) are correct and well-tested.
- selectOpenDebt and persistLocalDebt are O(n), not O(n²).
