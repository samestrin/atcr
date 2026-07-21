# atcr Reconciled Review

## Summary

- Total findings: 3
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 2
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 1 | 0 | 0 |
| MEDIUM | 1 | 1 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 3 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. severity_split — `cmd/atcr/autofix.go:380` (HIGH) · score 4
- Severity disagreement: LOW vs HIGH
- Reviewers: brad, dax (independence 2)
- Problem: (runAutoFix) Fail-closed default path wraps revert failure with %w, masking the original refusal reason and leaving the tree in an undefined partial-revert state without a clear operator recovery path

### 2. severity_split — `internal/sandbox/docker.go:232` (MEDIUM) · score 2
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax (independence 2)
- Problem: (Run) errors.Is check for context.Canceled folded into TimedOut but the kill-on-timeout path still runs &#96;docker kill&#96; for a cancellation — killing a container that may have already exited on cancel is harmless but wastes a subprocess spawn

### 3. solo_finding — `internal/sandbox/docker_test.go:243` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: TestDockerBackend_Run_CancellationClassIsTimedOut uses a fake docker script that sleeps 5s but the test only waits 50ms before cancel — the fake &#96;run&#96; subprocess may not have started yet, making the cancellation racy

## Findings

### HIGH

- `cmd/atcr/autofix.go:380` — confidence HIGH, reviewers: brad, dax
  - Severity disagreement: LOW vs HIGH
  - Problem: (runAutoFix) Fail-closed default path wraps revert failure with %w, masking the original refusal reason and leaving the tree in an undefined partial-revert state without a clear operator recovery path
  - Fix: Return a structured error or separate the refusal and revert failures, and log the original refusal explicitly
  - Evidence: [dax] default branch at :380-387 is unreachable in existing tests because all host-path tests now set noSandbox=true / [brad] return fmt.Errorf(&#34;auto-fix: refusing unsandboxed host validation without --no-sandbox AND revert failed: %w&#34;, rerr)

### MEDIUM

- `internal/sandbox/docker.go:232` — confidence HIGH, reviewers: brad, dax
  - Severity disagreement: LOW vs MEDIUM
  - Problem: (Run) errors.Is check for context.Canceled folded into TimedOut but the kill-on-timeout path still runs &#96;docker kill&#96; for a cancellation — killing a container that may have already exited on cancel is harmless but wastes a subprocess spawn
  - Fix: Consider checking if the container still exists before issuing kill on a pure cancellation (not deadline)
  - Evidence: [brad] if errors.Is(runCtx.Err(), context.DeadlineExceeded) // errors.Is(runCtx.Err(), context.Canceled) { res.TimedOut = true ... / [dax] killCtx block at :235-239 runs unconditionally for both DeadlineExceeded and Canceled
- `internal/sandbox/docker_test.go:243` — confidence MEDIUM, reviewers: dax
  - Problem: TestDockerBackend_Run_CancellationClassIsTimedOut uses a fake docker script that sleeps 5s but the test only waits 50ms before cancel — the fake &#96;run&#96; subprocess may not have started yet, making the cancellation racy
  - Fix: Add a ready-signal file or pipe to the fake script so the test cancels only after &#96;run&#96; has started
  - Evidence: fake script: &#96;if [ &#34;$1&#34; = &#34;run&#34; ]; then sleep 5; fi&#96; — cancel fires at 50ms, but exec.CommandContext may still be in fork/exec; no synchronization proves the cancellation landed during Run
