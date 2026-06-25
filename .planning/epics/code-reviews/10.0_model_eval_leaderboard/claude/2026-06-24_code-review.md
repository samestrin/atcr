# Code Review Report: 10.0_model_eval_leaderboard

## 1. Executive Summary
- **Overall Result:** Pass (in-repo scope) — full-epic ACs are Partial by design
- **Items Checked:** 3 fully verified / 7 epic ACs (2 partial + 2 deferred are intentional, recorded scope deferrals)
- **Approval Status:** Approved
- **Review Date:** June 24, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)

> **Scope:** Per the epic's recorded Clarifications (2026-06-24 rubber duck), Epic 10.0
> was narrowed to the **in-repo CLI half**: T1 (public submission export format), T2
> (`atcr benchmark` verify/export contract), T6 (docs), T5 left as-is (survived-skeptic
> already wired). T3 (curated `standard-v1` suite content → external `atcr/benchmark-suite`),
> T4 (public static site + methodology page → Epic 15.3), `benchmark run` (→ Epic 10.1),
> and `demonstrated_rate` (→ dropped; Epic 11.0 has no code) are out of scope. The
> in-scope work is fully delivered with all quality gates green. The "incomplete" ACs
> below are deliberate deferrals to named future epics, not defects.

## 2. Acceptance Criteria Results

| AC | Verdict | Notes |
|----|---------|-------|
| AC1 — `leaderboard --export` schema-v1 anonymized JSON | VERIFIED ✅ | Allowlist `PublicRecord` + `scrubField` backstop; deterministic |
| AC2 — `benchmark run --suite` + versioned reproducible diffs | PARTIAL ⚠️ | Suite contract (Load/Validate/ReproHash) shipped; `run` deferred to Epic 10.1 |
| AC3 — `benchmark export` distinct suite-tagged record | VERIFIED ✅ | `source`/`suite`/`suite_version` envelope; reads RunResult not local scorecard |
| AC4 — survived-skeptic + demonstrated rate in export | PARTIAL ⚠️ | survived_skeptic_rate fully wired; demonstrated_rate dropped by decision |
| AC5 — public site renders table, 90-day expiry | NOT_FOUND ❌ | Deferred to Epic 15.3 (mockup exists) |
| AC6 — methodology page references OSS reconciler + suite | NOT_FOUND ❌ | Deferred to Epic 15.3 |
| AC7 — docs: submission guide, suite README, privacy model | VERIFIED ✅ | `docs/benchmark.md` + `docs/scorecard.md` |

## 3. Evidence Map
- **AC1:** `internal/scorecard/export.go:23` (SubmissionSchema=1), `:32-52` (allowlist + envelope), `:285-309` (scrubField), `cmd/atcr/leaderboard.go:156` (runLeaderboardExport), `docs/scorecard.md:277` (Privacy Model)
- **AC2 (contract):** `internal/benchmark/benchmark.go:56` (Load), `:83` (Validate), `:136` (ReproHash — content-addressed, order-independent SHA-256)
- **AC3:** `internal/benchmark/benchmark.go:200-227` (Submission + BuildSubmission), `:212` (SourceBenchmarkSuite), `cmd/atcr/benchmark.go:89` (runBenchmarkExport)
- **AC4 (survived-skeptic):** `internal/scorecard/export.go:38`, `:97-127` (finalize, count-from-totals with stored-rate fallback)
- **AC7:** `docs/benchmark.md` (suite README + verify/export + privacy), `docs/scorecard.md:277-316`

## 4. Remaining Unchecked Items (all deliberate, recorded deferrals)
- **AC2 `benchmark run`** — live execution + scoring → Epic 10.1. Contract half is shipped.
- **AC4 `demonstrated_rate`** — dropped; Epic 11.0 (executing reviewers) is a draft with no code.
- **AC5 public leaderboard site / 90-day expiry** → Epic 15.3.
- **AC6 methodology page** → Epic 15.3.

These are NOT routed to technical debt — they are tracked as named future epics, not repo debt.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** In-scope deliverables (T1/T2/T6) are complete, well-documented, and fully covered. All quality gates pass. Adversarial findings are non-blocking hardening items.

## 6. Coverage Analysis
- **Coverage:** 89.2% (total); epic packages: benchmark 92.3%, scorecard 92.9%, cmd/atcr 84.3%
- **Baseline:** 80%
- **Delta:** ↑9.2%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... (gofmt -l clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 4 production source files + tests (two independent hostile-reviewer passes)
- **Issues Found:** 9 (Critical: 0, High: 0, Medium: 4, Low: 5)

### Issues by Severity
**Medium (4):**
1. `internal/scorecard/export.go:160` — Non-finite floats (Inf/NaN) in clampNonNegF/clampRate crash the whole export via json.Marshal; violates the "drop, don't crash" contract.
2. `internal/scorecard/export.go:285` — scrubField backstop denylist gaps (FHS roots beyond the 5 hardcoded, UNC paths, no-TLD emails, sk_/AIza keys). Allowlist is the primary guarantee.
3. `internal/benchmark/benchmark.go:119` — isSafeRelPath ignores symlinks; Load/ReproHash follow a symlinked diff outside suitePath (untrusted external suites). Limited current impact.
4. `internal/benchmark/benchmark.go:158` — Unbounded os.ReadFile in ReproHash → memory-exhaustion DoS on a hostile/huge diff.

**Low (5):**
5. `internal/scorecard/export.go:258` — medianInt64 even-count int64 sum overflow (latent, unrealistic latency magnitudes).
6. `internal/scorecard/export.go:119` — survived-skeptic stored-rate fallback uses an unweighted mean (only on corrupt nil-count records).
7. `internal/benchmark/benchmark.go:108` — Validate accepts empty/duplicate expected_categories entries.
8. `cmd/atcr/benchmark.go:101` — export accepts whitespace-only suite fields and empty Reviewers slice (degenerate submission).
9. `cmd/atcr/benchmark.go:105` — runBenchmarkExport hardwires time.Now(), defeating BuildSubmission's injectable timestamp (consistent with existing leaderboard.go pattern).

Pre-existing tracked debt confirmed in scope: TD README:41 (benchmark export re-anonymization) is self-documented in code and docs — not re-reported.

## 9. Follow-ups
- Findings 1-9 above are captured in the code-review TD stream. Run `/reconcile-code-review @.planning/epics/completed/10.0_model_eval_leaderboard.md` to merge them into the technical-debt README, then `/resolve-td` to address. Recommend prioritizing the Medium robustness/privacy items (1-4).
- No follow-ups required for the deferred ACs — they are owned by Epics 10.1 and 15.3.

---
*Generated by /execute-code-review on June 24, 2026 09:03:02PM*
