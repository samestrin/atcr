# Code Review Report: 7.0_executor_model_fix_generation

## 1. Executive Summary
- **Overall Result:** Partial
- **Items Checked:** 8 / 10 (acceptance criteria verified)
- **Approval Status:** Pending
- **Review Date:** June 22, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)

The implemented scope (optional `executor:` registry block, fix-generation phase in the verify pipeline, FIX/EVIDENCE population, config + validation, docs + example registries) is complete, well-tested, and passes all quality gates. The Partial result reflects two unsatisfied acceptance-criteria checkboxes (AC9 performance test, AC10 cost analysis) that were never delivered as artifacts; both are verification/documentation gaps, not functional defects, and neither appears in the epic's recorded IN-scope Boundaries list.

## 2. Acceptance Criteria Verified
- **AC1** Registry schema supports optional `executor` section — `internal/registry/config.go:141,289,407`
- **AC2** Generates fixes for verified findings (HIGH+ confidence, severity floor) — `internal/verify/executor.go:70-101`
- **AC3** No executor configured = unchanged behavior — `internal/verify/executor.go:54`, `pipeline.go:255`
- **AC4** Fix generation is a separate phase (post-verification, pre-emission) — `internal/verify/pipeline.go:251-257`
- **AC5** Executor called once per verified finding — `internal/verify/executor.go:70,87`
- **AC6** Output includes fix + executor name — `internal/verify/executor.go:99-100`, `internal/stream/writer.go:47,50`
- **AC7** Unit tests cover configured/not-configured/success/failure — `internal/verify/executor_test.go`, `internal/registry/executor_config_test.go`
- **AC8** Findings artifacts carry populated FIX column — `internal/stream/writer.go:42-59`

## 3. Evidence Map
- **Fix-generation gate:** `executor.go:72` `ConfidenceAtOrAbove(f.Confidence, ConfHigh)` + `executor.go:75` `meetsSeverityFloor` — HIGH-or-better AND severity floor, matching the recorded clarifications.
- **9-column schema preserved:** `writer.go:42-59` renders Fix as column 4 and Evidence (with "fix by <name>") as column 7; no 10th column.
- **Failure isolation:** `executor.go:88-98` — snippet/executor/empty-completion failures stamp `FixWarning` and continue, never failing the run.
- **Snapshot reuse:** `pipeline.go:162-184` extends the snapshot-need gate to fire when an executor is configured and at least one finding qualifies.

## 4. Remaining Unchecked Items
- **AC9 — Performance test (<10% overhead):** NOT_FOUND. No benchmark/perf test in `internal/verify/`; CI uses an injected fake completer only. Overhead is plausibly low by design but unproven.
- **AC10 — Cost analysis:** PARTIAL. Cost benefit is structural (1 executor vs N reviewers) and documented qualitatively in the epic + `CHANGELOG.md:3`, but no quantitative cost-analysis artifact exists.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked (implementation), Pending (epic AC completeness)
- **Rationale:** Core implementation is correct, defensible, and fully tested. Two AC checkboxes (verification/doc artifacts) remain open; surviving adversarial findings are LOW/MEDIUM polish, none blocking.

## 6. Coverage Analysis
- **Coverage:** 89.5% (total)
- **Baseline:** 80%
- **Delta:** ↑9.5%
- **Status:** PASSING
- **Epic packages:** verify 93.9%, registry 88.7%, reconcile 90.7%, stream 92.9%

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run (0 issues) |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l (clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 5 (executor.go, pipeline.go, config.go, confidence.go, emit.go)
- **Issues Found:** 12 (Critical: 0, High: 0, Medium: 2, Low: 10)

### Issues by Severity
**Medium**
- `executor.go:99` — Stale `FixWarning` not cleared on a successful re-run; a finding that failed then later succeeds carries both a valid Fix and a contradictory `fix_warning`. (correctness)
- `executor.go:157` — `ex.Persona` + finding/snippet content interpolated unsanitized into the executor prompt; no CR/LF strip or length cap. Registry-controlled, so bounded blast radius. (security)

**Low**
- `executor.go:70` — Fix generation is serial (O(n) sequential LLM calls), asymmetric with the bounded skeptic pool. (performance)
- `executor.go:108` — `fix_timeout` nil applies no per-call deadline despite the "inherit shared timeout" comment; a hung provider can block the run. (correctness)
- `executor.go:70` — No `ctx.Err()` check between findings; cancellation does not short-circuit the loop. (error-handling)
- `executor.go:142` — `readFixSnippet` swallows read failures with no log; silent fix-quality degradation. (error-handling)
- `emit.go:137` — `JSONFindings()` hand-copy omits `FixWarning`; field-addition footgun across two serializers. (maintainability)
- `config.go:148` — `BatchFixes` parsed/validated but never read; dead config. (maintainability)
- `config.go:413` — `validateExecutor` uses `== ""` not `TrimSpace`; whitespace provider/model yields a confusing error; model has no whitespace guard. (error-handling)
- `executor.go:82` — Substring "fix by <name>" idempotency guard in free-text Evidence is fragile; executor name unvalidated against agent names. (correctness)
- `executor.go:66` — Fix min-severity floor resolved in three places; extract `EffectiveFixMinSeverity()`. (maintainability)
- `pipeline.go:256` — `newExecutorClient()` built unconditionally even with zero eligible findings. (maintainability)

Several adversarial hypotheses were verified CLOSED: path traversal (jail-protected), unbounded snippet (64KB read cap + 60-line window), concurrent mutation (post-`wg.Wait` sequencing), nil deref (guarded), validation ordering (validate-before-defaults), fail-closed confidence gating.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/7.0_executor_model_fix_generation.md` to merge these 14 TD items (12 adversarial + 2 incomplete ACs) into the TD README.
- Decide AC9/AC10: either deliver the performance test + cost-analysis note, or formally de-scope them in the epic with rationale.
- Address the two MEDIUM findings (stale FixWarning; persona/prompt sanitization) before Epic 7.3 consumes the FIX column.

---
*Generated by /execute-code-review on June 22, 2026 12:15:40AM*
