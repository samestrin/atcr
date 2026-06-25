# Code Review Stream - 10.0_model_eval_leaderboard (Epic)

**Started:** June 24, 2026 09:03:02PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

> **Scope note:** Per the epic's recorded Clarifications (2026-06-24 rubber duck),
> this epic was narrowed to the **in-repo CLI half** (T1 export format, T2 `atcr
> benchmark`, T6 docs; T5 left as-is). T3 (curated `standard-v1` suite content) and
> T4 (public static site) are deferred to external repos / Epic 15.3, and
> `demonstrated_rate` was dropped (Epic 11.0 has no code). AC verdicts below note
> where a PARTIAL/NOT_FOUND is a deliberate, recorded deferral rather than a defect.

### Criterion: `atcr leaderboard --export` produces a schema-v1 anonymized submission JSON (no PII, no repo content, no provider keys)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/export.go:23` (SubmissionSchema=1), `export.go:32-52` (allowlist PublicRecord + ExportEnvelope), `export.go:285-309` (scrubField backstop: paths/email/keys), `cmd/atcr/leaderboard.go:156` (runLeaderboardExport), `docs/scorecard.md:277` (Privacy Model)
- **Notes:** Allowlist-based emission with defense-in-depth scrubbing. Deterministic (exportedAt threaded, no hidden time.Now in the library). Filters applied before anonymization.

### Criterion: `atcr benchmark run --suite standard-v1` runs the fixed-diff suite and produces a submission record; the suite diffs are versioned and reproducible
- **Verdict:** PARTIAL ⚠️ (deliberate deferral)
- **Evidence:** `internal/benchmark/benchmark.go:56` (Load/Validate), `benchmark.go:136` (ReproHash — content-addressed SHA-256, order-independent), `internal/benchmark/testdata/suite-valid/suite.json`
- **Notes:** The suite-loading + reproducibility CONTRACT is shipped and versioned. The `benchmark run` subcommand (live execution + scoring) is explicitly deferred to Epic 10.1 per the recorded scope; the curated `standard-v1` content lives in external `atcr/benchmark-suite` (T3). Not a defect.

### Criterion: `atcr benchmark export` produces a submission record referencing the suite version; record is distinct from the production `--export` format
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/benchmark/benchmark.go:200-227` (Submission envelope with source/suite/suite_version; BuildSubmission), `benchmark.go:212` (SourceBenchmarkSuite="benchmark-suite"), `cmd/atcr/benchmark.go:89` (runBenchmarkExport reads run-result, not local scorecard), `docs/benchmark.md:97`
- **Notes:** Distinct envelope via source/suite/suite_version. Reads a RunResult file (not the local scorecard) so a production run cannot be passed off as a suite submission.

### Criterion: Survived-skeptic rate and demonstrated rate appear in export when verification.json / execution evidence are present
- **Verdict:** PARTIAL ⚠️ (deliberate deferral)
- **Evidence:** `internal/scorecard/export.go:38` (SurvivedSkepticRate *float64 omitempty), `export.go:97-127` (finalize: count-based rate, fallback to stored rates), `export.go:83-94` (hasVerification gating)
- **Notes:** survived_skeptic_rate is fully wired with correct count-from-totals aggregation and the nil-pointer-omits-key semantics distinguishing "no verification ran" from "ran, 0% survived". `demonstrated_rate` was dropped by recorded decision (Epic 11.0 is a draft with no code). Not a defect.

### Criterion: Public leaderboard site renders the aggregate table; rows expire after 90 days without a fresh submission
- **Verdict:** NOT_FOUND ❌ (deliberate deferral)
- **Evidence:** None in this repo (by design)
- **Notes:** T4 (public static site) is explicitly out of scope and owned by Epic 15.3 (a mockup already exists at `atcr.dev/the-artifact/mockups/leaderboard.html`). Not a defect of this run.

### Criterion: Methodology page published alongside the leaderboard; references the OSS reconciler and standard suite
- **Verdict:** NOT_FOUND ❌ (deliberate deferral)
- **Evidence:** None in this repo (by design)
- **Notes:** Part of T4 (public site), deferred to Epic 15.3. The in-repo docs (`docs/benchmark.md`, `docs/scorecard.md`) already document the OSS-reconciler/standard-suite methodology that the public page will reference.

### Criterion: Docs — submission guide, benchmark suite README, privacy model
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/benchmark.md` (suite README + verify/export guide + privacy model), `docs/scorecard.md:277` (Privacy Model: allowlist, preserved vs. dropped fields), `docs/benchmark.md:161` (Privacy model section)
- **Notes:** Submission/export flow, suite-manifest contract, and privacy model all documented. The privacy doc honestly flags the benchmark-export re-scrub gap (matching TD README:41).

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (epic — no sprint-design.md risk profile)
**Files Reviewed:** 4 production source files (export.go, benchmark.go ×2, version.go) + tests
**Issues Found:** 9 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 4
- Low: 5

Two independent hostile-reviewer agents ran over the changed source. Findings were
manually vetted against the code (I read all three production files), deduped by
theme, and severity-adjusted down where real-world exploitability is low (controlled
inputs, allowlist-as-primary-guarantee, deferred execution surface). No blockers; all
9 are hardening items on an epic that already passed execute-epic's built-in
adversarial layers. Themes:
- **Robustness:** non-finite floats (Inf/NaN) in clampNonNegF/clampRate crash the
  whole export via json.Marshal — violates the stated "drop, don't crash" contract.
- **Privacy backstop:** scrubField denylist gaps (FHS roots beyond the 5 hardcoded,
  UNC paths, no-TLD emails, sk_/AIza keys). Allowlist remains the primary guarantee.
- **Untrusted-suite handling:** isSafeRelPath ignores symlinks; ReproHash ReadFile is
  unbounded (memory DoS). Limited current impact (external suite content + benchmark
  run are deferred), but the containment guarantee is real.

### Deferred acceptance criteria — NOT routed as technical debt
AC2 (`benchmark run` → Epic 10.1), AC4 (`demonstrated_rate` → dropped, Epic 11.0 has
no code), AC5 + AC6 (public site + methodology page → Epic 15.3) are deliberate,
recorded scope deferrals tracked in named future epics — not debt of this repo. They
are intentionally NOT written to the TD stream to avoid creating false "incomplete
sprint" debt; they are recorded as remaining items in this report instead.
INCOMPLETE_TD_COUNT = 0.

