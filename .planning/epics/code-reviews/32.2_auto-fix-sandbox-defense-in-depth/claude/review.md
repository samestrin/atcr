# Code Review Stream - 32.2_auto-fix-sandbox-defense-in-depth (Epic)

**Started:** July 20, 2026 06:39:18PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — nil backend without --no-sandbox opt-out causes runAutoFix to refuse (no host validation, no GitHub call), proven by a direct runAutoFix unit test
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/autofix.go:398-415` (default fail-closed switch case: reverts patch, returns error, no GitHub call); test `cmd/atcr/autofix_test.go:897-926` (TestRunAutoFix_NilSandboxWithoutOptOutRefuses)
- **Notes:** Dispatch is now a 3-way switch; the `default` arm (nil backend, noSandbox=false) reverts the applied patch and returns an error before any GitHub call. Test asserts error, zero GitHub calls, and reverted tree with validateArgv=`true`, proving the guard fired rather than a silent fallback.

### Criterion: AC2 — the --no-sandbox opt-out path still runs host validation exactly as before (host-path tests updated to set noSandbox: true, all green)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/autofix.go:262-266` (validateAutoFixBackend sets be.noSandbox=true, sole setter); host dispatch `case be.noSandbox` at `cmd/atcr/autofix.go:388-391`; host-path tests updated in `cmd/atcr/autofix_test.go` and `cmd/atcr/autofix_integration_test.go`; `cmd/atcr/autofix_test.go:1238` asserts be.noSandbox==true after --no-sandbox
- **Notes:** Host path preserved byte-identical when the opt-out is present. ~6 host-path constructions updated to set noSandbox:true as the epic predicted.

### Criterion: AC3 — a docker Backend.Run returning context.Canceled maps to a TimedOut ValidationResult, not a StartError, with a table test
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/sandbox/docker.go:231-236` (errors.Is(runCtx.Err(), DeadlineExceeded) || errors.Is(..., Canceled) → res.TimedOut=true, ExitCode=timeoutExitCode); table test `internal/sandbox/docker_test.go:250-302` (TestDockerBackend_Run_CancellationClassIsTimedOut, deadline + canceled rows)
- **Notes:** Both cancellation-class ends fold into TimedOut (exit 124, nil error). Uses errors.Is (wrap-safe) rather than the prior == equality. The adjacent TestDockerBackend_DockerCmd_ContextCancelNotTimeout (Preflight path) is untouched, as the refinement flagged.

### Criterion: AC4 — AC 01-03's zero-behavior-change note is reconciled with the new explicit-opt-out contract
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/autofix.go:375-386` (dispatch comment rewritten to supersede AC 01-03's nil→host baseline); `cmd/atcr/autofix_test.go:868-877` (TestRunAutoFix_NilSandboxUsesHostPath doc reconciled); commit d568b966
- **Notes:** The stale zero-behavior-change note is reconciled in-code (AC 01-03 lives in Sprint 32.0). Comment now states the opt-out flag is required and its absence fails closed.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Discovery (no sprint-design.md risk profile — epic)
**Files Reviewed:** 5
**Issues Found:** 2 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 2

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 2

### Notes
Two independent hostile-review agents each found exactly 1 LOW finding; neither blocks. Core security change (fail-closed dispatch guard) and the docker Canceled→TimedOut fold were both actively probed and confirmed sound (bm valid at default arm, single be.noSandbox setter, switch ordering safe, errors.Is compiles and is wrap-safe, Preflight/dockerCmd path untouched). The docker.go:236 finding is in mild tension with the epic's own design intent (Canceled→TimedOut is the goal) — captured as a nuance for future consideration, not a defect to fix now.
