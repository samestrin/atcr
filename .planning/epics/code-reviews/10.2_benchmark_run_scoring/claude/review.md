# Code Review Stream - 10.2_benchmark_run_scoring (Epic)

**Started:** June 25, 2026 03:44:26PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: `atcr benchmark run --suite-path <dir>` loads + validates the suite, executes each case's diff through the pipeline (via Epic 10.1 ingestion), and writes a `benchmark.RunResult` JSON.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark.go:84-95` (`newBenchmarkRunCmd` — `--suite-path` required, `--out`), `cmd/atcr/benchmark_run.go:37-138` (`executeBenchmarkRun`: `benchmark.Load` → `fanout.PrepareReviewFromDiff` → `fanout.ExecuteReview` → `Score` → `RunResult`), `cmd/atcr/benchmark.go:110-124` (marshals + writes JSON to stdout/`--out`)
- **Notes:** Per-case diff ingested via the exact 10.1 path (`PrepareReviewFromDiff`/`ExecuteReview`); roster discovered via `fanout.LoadReviewConfig` like `atcr review`.

### Criterion: Findings are scored against `Case.ExpectedCategories` into per-reviewer `scorecard.PublicRecord` fields; each record is re-scrubbed via `ScrubPublicRecord`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/benchmark/score.go:55-70` (`Score` emits `[]scorecard.PublicRecord`, `ScrubPublicRecord` applied line 58), `score.go:73-115` (`scoreOne` computes recall→`CorroborationRate`, `FindingsRaisedAvg`, `Runs`), `score.go:117-130` (case-insensitive/trim normalization)
- **Notes:** Recall = macro-avg of (expected categories surfaced)/(distinct expected). Precision intentionally omitted (documented). `internal/scorecard` untouched.

### Criterion: The run-result written by `run` is consumed unchanged by `benchmark export --in` (round-trip produces a valid suite-tagged `Submission`).
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run_test.go:124-162` (`TestBenchmarkRun_RoundTripsThroughExport` — writes run-result, feeds real `benchmark export` cmd, asserts `source=="benchmark-suite"`, suite, version, reviewers), `cmd/atcr/benchmark.go:145-182` (`runBenchmarkExport` reads run-result → `BuildSubmission`)
- **Notes:** Round-trip asserts `submitted_at` reuses run-result `generated_at`.

### Criterion: `GeneratedAt` is injectable so two runs over the same suite + transcript are byte-identical.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run.go:37` (`generatedAt time.Time` param), `:135` (`generatedAt.UTC().Format(RFC3339)`), `cmd/atcr/benchmark.go:110` (CLI injects `time.Now().UTC()`), `cmd/atcr/benchmark_run_test.go:87-101` (`TestExecuteBenchmarkRun_Reproducible` — `JSONEq` on two runs)
- **Notes:** Determinism reinforced by sorted reviewer order (`score.go:63`), index-keyed per-case temp dirs, fixed `Date`/`TimeSuffix`/`StartedAt` in the review request.

### Criterion: An end-to-end `run` test uses `internal/benchmark/testdata/suite-valid/` + a stub `Completer` (no network); benchmark README documents the `run → export` flow.
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run_test.go:21` (`suiteValidPath` fixture), `:51-55` (`stubCompleter`, no network), `:61-83` (`TestExecuteBenchmarkRun_ScoresSuite`), `docs/benchmark.md:101-156` (`run → export` flow + scoring table)
- **Notes:** Stub raises one `correctness` finding/case → case-01 recall 1.0, case-02 recall 0.5, macro-avg 0.75, proving per-case grouping.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md in epic mode)
**Files Reviewed:** 4 (cmd/atcr/benchmark.go, cmd/atcr/benchmark_run.go, internal/benchmark/score.go, internal/benchmark/benchmark.go)
**Issues Found:** 11 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 11

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 10

### Reviewer notes
- Both agents independently confirmed the core implementation is solid: `ScrubPublicRecord` is applied to every emitted record (`score.go:58` + `benchmark.go:277-280`), reviewer attribution is engine-stamped (never model-supplied), determinism/reproducibility holds (sorted order, hermetic fixed timestamps, order-independent float accumulation), path traversal is guarded (`isSafeRelPath` + `Load`'s `Lstat`/`IsRegular`), and the length-prefix ReproHash is collision-resistant.
- Agent 1's HIGH (total-case-failure aborts the run) was **downgraded to LOW**: it is documented intended behavior (`docs/benchmark.md:150-152`), an operational/cost tradeoff, not a defect.
- The 1 MEDIUM (unbounded `os.ReadFile` of case diff with no size cap, asymmetric with `verify`'s `MaxDiffBytes` guard) is the only finding above LOW. All findings are latent/defense-in-depth — none block the acceptance criteria.
