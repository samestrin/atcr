# Code Review Stream - 7.3_github_action_pr_integration (Epic)

**Started:** June 22, 2026 04:37:14PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — action.yml exists; workflow can call it on pull_request, running atcr review + atcr reconcile
- **Verdict:** VERIFIED ✅
- **Evidence:** `action.yml:37-81` (composite action; `atcr review --base` at :73, `atcr reconcile` at :81), `docs/github-action.md:27-43` (`on: pull_request` example, `uses: samestrin/atcr@v1`)
- **Notes:** Root-level composite action builds the binary via setup-go + `go build ./cmd/atcr` and runs the full pipeline. Matches epic decision (root action.yml, full pipeline).

### Criterion: AC2 — Action posts a PR check summarizing findings, honoring --fail-on for the merge gate
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/ghaction/render.go:57-142` (`Conclusion` + `BuildCheckOutput` honoring fail-on), `cmd/atcr/github.go:110-141` (posts check run, gate rides exit code via codedError)
- **Notes:** Empty fail-on → neutral/informational; threshold → failure when a non-refuted finding is at/above. Gate exposed both via check conclusion and process exit 1.

### Criterion: AC3 — Inline comments render PROBLEM at FILE:LINE; when FIX populated, fix + executor attribution shown
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/ghaction/comments.go:50-60` (`commentBody`: "ATCR found: <problem>. Fix: <fix>. Suggested by: <executor>"), `internal/ghaction/render.go:33-43` (`FixAttribution` parses "fix by <name>" from Evidence), `cmd/atcr/github_integration_test.go:91-94` (asserts all three clauses)
- **Notes:** Fix/attribution clauses omitted when source data absent — matches AC3 contract and epic decision (no 10th column; attribution parsed from EVIDENCE token).

### Criterion: AC4 — A toggle disables inline comments (artifacts/check only)
- **Verdict:** VERIFIED ✅
- **Evidence:** `action.yml:16-19` (`inline-comments` input default `false`), `cmd/atcr/github.go:31,129-135` (inline opt-in; check + artifacts always produced)
- **Notes:** Default OFF as decided in epic clarifications. `--inline-comments` requires `--pr` (validated github.go:93-95).

### Criterion: AC5 — Real-PR integration test demonstrates the end-to-end flow
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/github_integration_test.go:20-95` (`TestGithubCmd_EndToEndFlow`, httptest fake GitHub API: failing check + 2 inline comments in one invocation), `docs/github-action.md:27-43` (committed `on: pull_request` example workflow)
- **Notes:** No live network calls, consistent with repo's established integration-test pattern and epic decision.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic discovery mode)
**Files Reviewed:** 5 (cmd/atcr/github.go, internal/ghaction/{render,comments,client}.go, action.yml)
**Issues Found:** 15 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 15

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 7
- Low: 7

### Top finding
- **HIGH (correctness):** `ghaction.Conclusion` (render.go:57-74) reimplements the merge gate and omits the `category == "out-of-scope"` exclusion that the canonical `reconcile.IsFailing` enforces — `atcr github` and `atcr reconcile` can return opposite verdicts on the same review. Verified against gate.go:96-98 and reconcile.go:168. Fail-closed (false block), so HIGH not CRITICAL.
- Recurring theme: the `ghaction` package re-derives gate/severity/verdict semantics rather than reusing `internal/reconcile`, opening several divergence + display-mismatch gaps.
