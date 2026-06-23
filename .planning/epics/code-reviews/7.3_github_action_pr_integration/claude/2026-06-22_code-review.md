# Code Review Report: 7.3_github_action_pr_integration

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 22, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **action.yml / docs/github-action.md** – AC1: composite action runs `atcr review` + `atcr reconcile`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `action.yml:37-81`, `docs/github-action.md:27-43`
- **internal/ghaction/render.go / cmd/atcr/github.go** – AC2: PR check honoring `--fail-on`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/ghaction/render.go:57-142`, `cmd/atcr/github.go:110-141`
- **internal/ghaction/comments.go / render.go** – AC3: inline comments render PROBLEM + FIX + executor attribution
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/ghaction/comments.go:50-60`, `internal/ghaction/render.go:33-43`
- **action.yml / cmd/atcr/github.go** – AC4: inline-comments toggle (default off)
  - Before: `[ ]` → After: `[x]`
  - Evidence: `action.yml:16-19`, `cmd/atcr/github.go:31,129-135`
- **cmd/atcr/github_integration_test.go** – AC5: end-to-end integration test
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/github_integration_test.go:20-95`

## 3. Evidence Map
- **AC1 — action.yml runs review + reconcile**
  - Evidence: `action.yml:62-81`, `docs/github-action.md:27-43`
  - Summary: Composite action builds the binary from `github.action_path`, runs `atcr review --base` then `atcr reconcile` against the consumer checkout; committed example workflow triggers `on: pull_request`.
- **AC2 — PR check honoring --fail-on**
  - Evidence: `internal/ghaction/render.go:57-74` (Conclusion), `:96-142` (BuildCheckOutput), `cmd/atcr/github.go:111-141`
  - Summary: Empty fail-on → neutral/informational; threshold → failure when a non-refuted finding is at/above. Gate rides both the check conclusion and process exit 1.
- **AC3 — inline comment contract**
  - Evidence: `internal/ghaction/comments.go:50-60`, `internal/ghaction/render.go:33-43`
  - Summary: Body is "ATCR found: <problem>. Fix: <fix>. Suggested by: <executor>", with Fix/attribution clauses omitted when absent; executor parsed from the "fix by <name>" EVIDENCE token.
- **AC4 — inline toggle**
  - Evidence: `action.yml:16-19`, `cmd/atcr/github.go:31,91-95,129-135`
  - Summary: `inline-comments` defaults false; check + artifacts always produced; `--inline-comments` requires `--pr`.
- **AC5 — integration test**
  - Evidence: `cmd/atcr/github_integration_test.go:20-95`
  - Summary: httptest fake GitHub API exercises a failing check + two inline comments in one `atcr github` invocation; no live network.

## 4. Remaining Unchecked Items
No remaining unchecked items - all 5 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria are backed by code + tests. The implementation is well-structured and the script-injection defense in `action.yml` holds across all `run:` blocks. Adversarial review surfaced quality/hardening debt (none blocking), captured to the TD stream for reconciliation.

## 6. Coverage Analysis
- **Coverage:** epic packages — `internal/ghaction` 91.0%, `cmd/atcr` 83.9%
- **Baseline:** 80%
- **Delta:** ↑ above baseline
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l (epic files) |

## 8. Adversarial Analysis
- **Files Reviewed:** 5
- **Issues Found:** 15 (Critical: 0, High: 1, Medium: 7, Low: 7)
- **Mode:** Discovery (no sprint-design.md risk profile — epic)

### Issues by Severity
- **HIGH — correctness** (`internal/ghaction/render.go:57-74`): `Conclusion` reimplements the merge gate and omits the `category == "out-of-scope"` exclusion enforced by the canonical `reconcile.IsFailing` (gate.go:96-98) used by `atcr reconcile` (reconcile.go:168) — `atcr github` and `atcr reconcile` can return opposite verdicts on the same review. Fail-closed.
- **MEDIUM (7):** isRefuted lacks whitespace/canonical normalization (render.go:48); `cell()` does not neutralize markdown structure → check-run table spoofing (render.go:79); `BuildCheckOutput` discards `ParseSeverity` error → malformed-title silent pass (render.go:114); `BuildInlineComments` posts empty "ATCR found: ." comments (comments.go:30); single off-diff 422 fails the run (github.go:166); no 429/5xx/Retry-After handling, amplified by per-finding loop (client.go:98); required `permissions:` only in input description, late 403 (action.yml:28).
- **LOW (7):** un-normalized severity/confidence display (render.go:131); FixAttribution duplicates executor token grammar (render.go:33); stringly-typed conclusion constants (github.go:139); no aggregate context deadline across N posts (client.go:32); baseURL skips https validation before attaching token (client.go:35); error drops rate-limit/request-id headers (client.go:98); generic PR-number error on non-pull_request trigger (action.yml:99).

**Recurring theme:** the `ghaction` package re-derives gate/severity/verdict semantics instead of reusing `internal/reconcile`, the source of the HIGH divergence and several display/consistency gaps.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/7.3_github_action_pr_integration.md` to merge these 15 findings into the TD README with reviewer attribution.
- Priority fix: reuse `reconcile.IsFailing` inside `ghaction.Conclusion` to close the gate divergence (HIGH).

---
*Generated by /execute-code-review on June 22, 2026 04:37:14PM*
