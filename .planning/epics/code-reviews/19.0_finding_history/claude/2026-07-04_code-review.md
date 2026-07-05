# Code Review Report: 19.0_finding_history

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 4 / 4
- **Approval Status:** Approved
- **Review Date:** July 04, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **19.0_finding_history.md** – Two consecutive `review` runs append two record batches to `findings-history.jsonl`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/review.go:339-344`, `internal/history/writer.go:19-45`
- **19.0_finding_history.md** – `atcr history --since 30d --package internal/registry` prints a filtered markdown table
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/history.go:34-68`, `internal/history/filter.go:54-77`, `internal/history/render.go:24-104`
- **19.0_finding_history.md** – Empty/absent history exits 0 with a "no history" message
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/history.go:47-64`, `internal/history/reader.go:19-25`
- **19.0_finding_history.md** – `go test ./...` passes; new code covered by `history_test.go` + `internal/history/*_test.go`
  - Before: `[ ]` → After: `[x]`
  - Evidence: suite green (Phase 4), `internal/history` at 92.3% coverage

## 3. Evidence Map
- **AC1 — durable append per run**
  - Evidence: `cmd/atcr/review.go:339-344`, `internal/history/capture.go:28-66`, `internal/history/writer.go:19-45`
  - Summary: `RecordReview` is hooked on every successful review after `ExecuteReview` returns (before the conditional in-process reconcile), reads the always-present pool `findings.txt`, dedupes by id within the run, and `Append` opens the ledger `O_CREATE|O_WRONLY|O_APPEND` so each run adds a batch rather than truncating.
- **AC2 — filtered query + markdown table**
  - Evidence: `cmd/atcr/history.go:20-68`, `internal/history/filter.go:14-77`, `internal/history/render.go:24-104`, `cmd/atcr/main.go:198`
  - Summary: `--since` (custom d/w parser) and `--package` (separator-aware prefix match, registry vs registry2 correctly distinguished) drive `Filter`, and `RenderTable` prints counts by severity per package with per-package and grand totals. Command registered via `AddCommand`.
- **AC3 — empty/absent history exits 0**
  - Evidence: `cmd/atcr/history.go:47-64`, `internal/history/reader.go:19-25`
  - Summary: `Load` returns `(nil, nil)` on an absent ledger; `runHistory` prints a "no history" notice and returns nil both when the ledger is empty and when the filter empties the result set.
- **AC4 — tests pass, new code covered**
  - Evidence: `cmd/atcr/history_test.go`, `internal/history/{capture,edge,filter,render}_test.go`
  - Summary: Full suite green; `internal/history` 92.3%, `cmd/atcr` 83.5%, repo total 89.3%.

## 4. Remaining Unchecked Items
No remaining unchecked items - all 4 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria are backed by concrete `file:line` evidence, the implementation matches the recorded epic clarifications (pool-merged source, severity-excluded id, `.atcr/findings-history.jsonl` location, separator-aware `--package`, custom d/w `--since` parser), and the full test suite passes with coverage well above baseline. Adversarial review surfaced no critical/high defects; the six medium/low findings are hardening items, not correctness blockers for the shipped ACs.

## 6. Coverage Analysis
- **Coverage:** 89.3%
- **Baseline:** 80%
- **Delta:** ↑9.3%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 8 (internal/history/*.go + cmd/atcr/history.go + review.go hook)
- **Issues Found:** 6 (Critical: 0, High: 0, Medium: 3, Low: 3)
- **Risk Profile:** Not available (epic — no sprint-design.md); discovery mode.

### Issues by Severity

**MEDIUM**
- `internal/history/filter.go:32` — `ParseSince` silently accepts `Infd` / `1e30d` / `200000d`, saturating to a ~292-year window instead of returning a usage error (exit 2). Empirically confirmed.
- `internal/history/reader.go:47` — `Load` hard-fails (returns error, 0 records) on a single line >1MiB, bricking the whole ledger read and contradicting its own "never brick / skip malformed lines" contract. Empirically confirmed.
- `internal/history/capture.go:56` — first-wins dedupe makes the recorded severity order-dependent when two reviewers report the same finding at different severities; should take max severity for a deterministic trend table.

**LOW**
- `internal/history/writer.go:38` — doc comment overstates O_APPEND single-write atomicity; large batches can split across `write()` syscalls and interleave under concurrent reviews.
- `internal/history/capture.go:38` — `res.Skipped` from `ParseSource` is discarded, so malformed pool rows silently drop from history with no warning (undercount).
- `internal/history/render.go:85` — package/severity strings written into markdown cells unescaped; a pipe or newline breaks the table or injects a spoofed column.

### Excluded (out of scope)
- Unbounded ledger growth / whole-file-in-RAM on `Load` — retention/rotation is explicitly out of scope for Epic 19.0 and is covered by follow-up epic `19.4_history_time_sharding`.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/19.0_finding_history.md` to merge these findings into the technical-debt README with reviewer + confidence attribution.
- Consider addressing the two confirmed MEDIUM input-validation/robustness findings (`ParseSince` overflow, `Load` ErrTooLong) via `/resolve-td` — both are small, well-scoped, and empirically reproduced.

---
*Generated by /execute-code-review on 2026-07-04 19:49:47*
