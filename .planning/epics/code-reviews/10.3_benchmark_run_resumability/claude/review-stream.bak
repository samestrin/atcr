# Code Review Stream - 10.3_benchmark_run_resumability (Epic)

**Started:** June 25, 2026 07:11:38PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — checkpoint written per case immediately after scoring; killed process leaves exactly completed cases
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run.go:180-185` (save before loop advances), `cmd/atcr/benchmark_checkpoint.go:104-110` (atomic temp+rename via writeExportFile)
- **Notes:** Tests `TestExecuteBenchmarkRun_WritesCheckpointPerCase` and `TestExecuteBenchmarkRun_CheckpointHoldsOnlyCompletedCasesOnFailure` (benchmark_run_resume_test.go:46-95) assert one entry per scored case and that a mid-suite abort leaves exactly the completed case.

### Criterion: AC2 — re-run resumes from first unscored case; zero Completer calls for checkpointed cases
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run.go:91-103` (skip+replay), `:258-262` (replayCheckpointCase, no Completer)
- **Notes:** `TestExecuteBenchmarkRun_FullResumeIsZeroCostAndIdentical` asserts `second.calls == 0`; `TestExecuteBenchmarkRun_PartialResumeExecutesOnlyRemainder` asserts only the 1 unscored case executes.

### Criterion: AC3 — resumed run produces byte-identical RunResult
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_run.go:234-253` (single shared applyReviewerOutcome fold path), injected `generatedAt` + `sort.Strings(order)` (:188)
- **Notes:** Full + partial resume tests assert `assert.JSONEq` against an uninterrupted baseline.

### Criterion: AC4 — resume guarded by suite identity; mismatch rejected (fail-closed), never silently mixed
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark_checkpoint.go:126-147` (validateCheckpoint + validateCheckpointRoster, fail-closed sentinels), `cmd/atcr/benchmark_run.go:97-100` (per-index CaseID guard)
- **Notes:** Tests cover repro-hash drift, roster membership drift, roster model drift, and per-index case-id drift — all assert ErrorIs the fail-closed sentinel. Roster guard exceeds the AC (ReproHash covers only suite content).

### Criterion: AC5 — checkpointing opt-in via explicit flag; default unchanged; 10.2 abort semantics intact
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/benchmark.go:93` (`--checkpoint` flag, default `""`), `cmd/atcr/benchmark_run.go:49` (empty path = 10.2 path verbatim)
- **Notes:** `TestBenchmarkRunCmd_HasOptionalCheckpointFlag` + `TestExecuteBenchmarkRun_NoCheckpointPathWritesNothing`; abort-on-total-roster-failure preserved (CheckpointHoldsOnlyCompletedCasesOnFailure still requires error).

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (full hostile; epic had no embedded adversarial tasks)
**Files Reviewed:** 6
**Issues Found:** 14 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic — no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 11

### Notes
Production code is provably correct on the headline reproducibility risk (single shared
applyReviewerOutcome fold path, in-order accumulation, copy-sort median, airtight
suite-identity + per-index case-id guards). Two agent-reported CRITICAL/HIGH items were
verified and re-rated MEDIUM: both are test-quality gaps, not code defects —
(1) cost/latency replay only exercised with zero-usage stub; (2) the no-checkpoint-path
test is tautological (dir never passed to the function). Highest-value real finding:
persona omitted from the AC4 roster guard signature (silent identity drift on resume).
