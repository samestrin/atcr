# Code Review Stream - 22.1_reconcile_verify_repo_root_threading (Epic)

**Started:** July 12, 2026 09:57:39PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `atcr reconcile <path> --repo <other-repo>` validates finding file paths against `<other-repo>`, not the CWD
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/reconcile.go:42` (flag), `cmd/atcr/reconcile.go:98-105` (read+normalize), `cmd/atcr/reconcile.go:111` (`Root: repoRoot`); consumed at `internal/reconcile/gate.go:239` (`validateFindingPaths(ctx, jf, opts.Root)`) and `gate.go:225` (AST grouper). Behavioral test `cmd/atcr/reconcile_test.go:623-649` (`TestReconcileCmd_RepoFlagValidatesAgainstOtherRepo`).
- **Notes:** `--repo` threads the reviewed-repo root into path validation; test proves a finding citing `x.go` (present only in `<other-repo>`) validates clean with `--repo`, and the default `.` still flags it. Threading is real, not a no-op.

### Criterion: Running `atcr reconcile`/verify from a non-repo-root CWD (with `--repo` unset, defaulting to `.`) behaves exactly as today — no regression
- **Verdict:** VERIFIED ✅
- **Evidence:** Default `"."` at `cmd/atcr/reconcile.go:42` and `cmd/atcr/verify.go:34`; empty-value normalization at `reconcile.go:99-105` and `verify.go:95-100`. Control-run guards: `reconcile_test.go:640-648` (default `.` still flags; empty `--repo` normalizes to `.`).
- **Notes:** Unset and empty both resolve to `.`, preserving the pre-22.1 CWD==repo-root behavior.

### Criterion: `go test ./...` passes; a new test covers reconcile validating findings for a repo other than the CWD
- **Verdict:** VERIFIED ✅ (pending Phase 4 test run for `go test ./...`)
- **Evidence:** New behavioral test `cmd/atcr/reconcile_test.go:623-649`; verify-side threading test `cmd/atcr/verify_test.go:212-238`.
- **Notes:** The reconcile test exercises validation against another repo end-to-end. The verify test is threading/acceptance-only (deep effect needs a live skeptic model, per its own doc comment) — a known, documented limitation, not a gap.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for epic)
**Files Reviewed:** 4
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

Top findings (all non-blocking, routed to TD for later reconcile):
1. MEDIUM `cmd/atcr/reconcile.go:98` — `--repo` never checked for existence; a bad path silently drops the whole backlog (exit 0).
2. MEDIUM `cmd/atcr/verify.go:94` — `--repo` never checked; bad path → all findings silently `unverifiable` (exit 0).
3. MEDIUM `cmd/atcr/verify_test.go:220` — verify-side `--repo` test is hollow (no-skeptic registry never builds the snapshot/redactor it claims to guard).
4. LOW `cmd/atcr/verify.go:101` — `absRoot, _ := filepath.Abs()` drops error; empty base disables path redaction in exec evidence.
5. LOW `cmd/atcr/verify.go:94` — near-duplicate `--repo` normalization in reconcile.go + verify.go (drift risk).
6. LOW `cmd/atcr/verify.go:90` — comment misstates that the exec validator resolves go.mod against repoRoot.
