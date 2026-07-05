# Code Review Stream - 19.0_finding_history (Epic)

**Started:** July 04, 2026 07:49:47PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: Two consecutive `review` runs append two record batches to `findings-history.jsonl`
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:339-344`, `internal/history/capture.go:28-66`, `internal/history/writer.go:19-45`
- **Notes:** `RecordReview` is hooked on every successful review after `ExecuteReview` returns; `Append` opens the ledger with `O_CREATE|O_WRONLY|O_APPEND`, so each run appends a batch rather than truncating. Within a run findings are deduped by id so the batch holds one record per distinct finding.

### Criterion: `atcr history --since 30d --package internal/registry` prints a markdown table filtered to that package and window
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/history.go:20-68`, `internal/history/filter.go:14-77`, `internal/history/render.go:24-104`, `cmd/atcr/main.go:198`
- **Notes:** `newHistoryCmd` registers `--since` (custom d/w parser) and `--package`; `runHistory` loads the ledger, filters via `Filter(recs, since, pkg, now)` with separator-aware prefix match, and prints `RenderTable`. Command wired into root via `AddCommand`.

### Criterion: Empty/absent history exits 0 with a "no history" message, not an error
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/history.go:47-64`, `internal/history/reader.go:19-25`
- **Notes:** `Load` returns `(nil, nil)` on an absent ledger. `runHistory` prints "no history recorded yet…" and returns nil when `len(recs)==0`, and "no history for <scope>" + nil when the filter empties the set. Both paths exit 0.

### Criterion: `go test ./...` passes; new code covered by `cmd/atcr/history_test.go` + `internal/history/*_test.go`
- **Verdict:** VERIFIED ✅ (test presence; suite execution in Phase 4)
- **Evidence:** `cmd/atcr/history_test.go`, `internal/history/capture_test.go`, `internal/history/edge_test.go`, `internal/history/filter_test.go`, `internal/history/render_test.go`
- **Notes:** Test files present for both the command and the package. `go test ./...` run in Phase 4 confirms passing.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md risk profile — epic)
**Files Reviewed:** 8 (internal/history/*.go + cmd/atcr/history.go + review.go hook)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 3

**Two findings empirically confirmed** (not just asserted): `ParseSince` overflow saturation bypassing the usage-error path, and `Load` bricking on a >1MiB line. **One finding excluded** from TD as out-of-scope: unbounded ledger growth / whole-file-in-RAM — retention/rotation is explicitly out of scope for Epic 19.0 and covered by follow-up epic 19.4_history_time_sharding.
