# Code Review Stream - 3.4_scorecard-diagnostics-writer (Epic)

**Started:** June 16, 2026 07:47:13AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — A test injects a buffer and asserts on a scorecard diagnostic's text
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/scorecard_test.go:295` (TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter), `internal/scorecard/store_test.go:371` (malformed-record), plus over-long-line and write-failure variants
- **Notes:** Buffer injected via `EmitOpts{Diag: &buf}` / `ReadOpts{Writer: &buf}`; assertions check `buf.String()` contains the exact diagnostic text (e.g. "has no matching raised finding").

### Criterion: AC2 — No fmt.Fprintf(os.Stderr,…) operational diagnostics remain in internal/scorecard/
- **Verdict:** VERIFIED ✅
- **Evidence:** grep for `Fprintf/Fprintln/Fprint(os.Stderr` in `internal/scorecard/*.go` (non-test) returns NONE
- **Notes:** All 9 diagnostic sites route through the threaded writer (`w`/`diagWriter(...)`); returns are checked via `_, _ =`.

### Criterion: AC3 — CLI reconcile diagnostics route through cmd.ErrOrStderr()
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/reconcile.go:91` (`EmitOpts{... Diag: cmd.ErrOrStderr()}`), `cmd/atcr/scorecard.go:55` and `cmd/atcr/leaderboard.go:65` (`ReadOpts{Writer: cmd.ErrOrStderr()}`)
- **Notes:** All three CLI scorecard entry points source their writer from cobra.

### Criterion: AC4 — MCP handleReconcile supplies a defined writer with no emission regression
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/handlers.go:220` (`EmitOpts{Diag: os.Stderr}`)
- **Notes:** Documented default per Key Design Gap; emission path unchanged (EmitForReconcile still called best-effort).

### Criterion: AC5 — Default behavior (no writer) still writes to os.Stderr; existing tests pass
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/store.go:31-34` (`diagWriter` resolves nil → os.Stderr); nil-default tests at `internal/scorecard/store_test.go:389` and `internal/scorecard/scorecard_test.go:338`
- **Notes:** `ReadOpts{}`/`EmitOpts{}` zero-values preserve prior behavior; full suite passes.

---

## Test & Quality Gates

- **Tests:** PASSING (full `go test ./...`, 0 failures)
- **Coverage:** 88.7% total (baseline 80%, ↑8.7%); scorecard package 92.7% — PASSING
- **Lint (`golangci-lint run`):** PASSING (0 issues)
- **Types (`go vet ./...`):** PASSING
- **Format (`gofmt -l`):** PASSING for epic files (2 unrelated pre-existing `.planning/.temp/spike-2.0/` files excluded)

---

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md risk profile for epics)
**Files Reviewed:** 5 production files (store.go, scorecard.go, cmd/atcr/{reconcile,scorecard,leaderboard}.go, internal/mcp/handlers.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 5

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 4

### Notes
- Two hostile reviewers (fresh context) confirmed the writer is threaded through **every** diagnostic site (incl. `FindByRunID` → inner `ReadRecords`); no diagnostic remains hardcoded to `os.Stderr` except the intended `diagWriter` nil-default branch. Discarded write errors (`_, _ =`) are the correct best-effort posture.
- The MEDIUM finding (MCP `os.Stderr` vs `e.log` structured logger) is **enhancement debt against a deliberate, documented epic decision** (Clarifications Q2 placed `e.log` adaptation out of scope); recorded with that context for `/resolve-td`.
- 2 additional raised findings (Writer/Diag naming `store.go:21`; `diagWriter` re-resolution `store.go:194`) were **already captured** to `.planning/technical-debt/README.md` during `/execute-epic` and are intentionally not re-recorded (avoids duplicate rows post-reconcile).

### New Issues (this review → td-stream.txt, REVIEWER=claude)
- LOW `internal/scorecard/reconcile.go:20` — stale `EmitForReconcile` doc comment ("MCP passes a zero EmitOpts") contradicts the new `EmitOpts{Diag: os.Stderr}` wiring.
- LOW `internal/scorecard/store.go:25` — `ReadOpts.Writer`/`EmitOpts.Diag` document no concurrency contract for caller-supplied writers (latent).
- LOW `internal/scorecard/store.go:114` — diagnostics embed absolute store paths/usernames; latent leak only if the writer is ever routed to a non-local sink.
- MEDIUM `internal/mcp/handlers.go:220` — MCP scorecard diagnostics bypass `e.log` structured logging (deliberate/de-scoped; enhancement debt).
- LOW `internal/scorecard/scorecard_test.go:291` — no wiring-level test that MCP/CLI callers pass the intended writer (unit routing is tested; call-site wiring is not).
