# atcr Reconciled Review

## Summary

- Total findings: 4
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 1
- Authority promoted: 2
- Consensus filtered: 6 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 1 | 0 | 0 |
| MEDIUM | 2 | 0 | 0 |
| LOW | 1 | 0 | 0 |

## Disagreements

Top 9 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/verify/localvalidate_pgroup_unix.go:26` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: (configureProcessGroup) Cancel returns non-ESRCH kill errors directly to exec.CommandContext, which ignores cancel errors and proceeds to Wait; the kill error is discarded and never reaches the caller, so a failed group kill (e.g. EPERM from a privilege-dropped child) is silent and indistinguishable from a clean kill

### 2. gray_zone — `internal/verify/localvalidate.go:108` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: Timeout path returns res, nil unconditionally when context is done, silently discarding runErr — if the process-group Cancel fails (e.g. EPERM from setuid grandchildren or LSM restrictions), the kill is incomplete and orphans survive without any signal
- Detail: similarity 0.00
- Positions:
  - brad — MEDIUM: Timeout path returns res, nil unconditionally when context is done, silently discarding runErr — if the process-group Cancel fails (e.g. EPERM from setuid grandchildren or LSM restrictions), the kill is incomplete and orphans survive without any signal

### 3. solo_finding — `internal/verify/localvalidate_pgroup_unix.go:26` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: (configureProcessGroup) The group SIGKILL fires only when runCtx.Done() triggers Cancel; if the command exits 0 or non-zero before the timeout, Cancel never fires and any orphaned grandchildren survive indefinitely

### 4. gray_zone — `internal/verify/localvalidate_pgroup_unix.go:27` (MEDIUM) · score 2
- Reviewers: greta (independence 1)
- Problem: ESRCH is matched with == rather than errors.Is - correct today because syscall.Kill returns a bare Errno, but a future wrapped return would misreport the already-exited race as a hard kill error instead of ProcessDone
- Detail: similarity 0.00
- Positions:
  - greta — MEDIUM: ESRCH is matched with == rather than errors.Is - correct today because syscall.Kill returns a bare Errno, but a future wrapped return would misreport the already-exited race as a hard kill error instead of ProcessDone

### 5. severity_split — `internal/verify/localvalidate_pgroup_unix_test.go:47` (MEDIUM) · score 2
- Severity disagreement: LOW vs MEDIUM
- Reviewers: dax, mira (independence 2)
- Problem: (TestRunConfiguredValidation_TimeoutReapsGrandchild) processAlive uses bare PID without any uniqueness guarantee across the 5s Eventually window — OS PID recycling can flip the assertion to a false-negative failure or, if a new process happens to appear at that PID, a false-positive pass

### 6. gray_zone — `internal/verify/localvalidate_pgroup_unix_test.go:68` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: The happy-path test only covers exit-0; AC2 requires pass/fail runs to be unaffected, but no test verifies that a clean non-zero exit preserves ExitCode and sets Passed()=false under the new cancel override
- Detail: similarity 0.00
- Positions:
  - dax — MEDIUM: The happy-path test only covers exit-0; AC2 requires pass/fail runs to be unaffected, but no test verifies that a clean non-zero exit preserves ExitCode and sets Passed()=false under the new cancel override

### 7. gray_zone — `internal/verify/localvalidate_pgroup_unix.go:22` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: If the context is cancelled before cmd.Start() finishes, cmd.Process is nil. The override returns nil, but this race condition is untested and could mask start errors or interact unpredictably with future Go versions that call Cancel differently
- Detail: similarity 0.00
- Positions:
  - dax — LOW: If the context is cancelled before cmd.Start() finishes, cmd.Process is nil. The override returns nil, but this race condition is untested and could mask start errors or interact unpredictably with future Go versions that call Cancel differently

### 8. gray_zone — `internal/verify/localvalidate_pgroup_unix.go:29` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Non-ESRCH errors from syscall.Kill (e.g., EPERM if a group member drops privileges) propagate up through cmd.Run() and bypass the TimedOut branch in RunConfiguredValidation, masking a failed reap as a clean timeout
- Detail: similarity 0.00
- Positions:
  - dax — LOW: Non-ESRCH errors from syscall.Kill (e.g., EPERM if a group member drops privileges) propagate up through cmd.Run() and bypass the TimedOut branch in RunConfiguredValidation, masking a failed reap as a clean timeout

### 9. gray_zone — `internal/verify/localvalidate_pgroup_unix_test.go:53` (LOW) · score 1
- Reviewers: brad (independence 1)
- Problem: 250ms timeout races with shell scheduling under CI load: sh may be preempted before echo $! executes, leaving res.Stdout empty and causing require.NotEmpty to fail before the Eventually reaping assertion can run
- Detail: similarity 0.00
- Positions:
  - brad — LOW: 250ms timeout races with shell scheduling under CI load: sh may be preempted before echo $! executes, leaving res.Stdout empty and causing require.NotEmpty to fail before the Eventually reaping assertion can run

## Findings

### HIGH

- `internal/verify/localvalidate_pgroup_unix.go:26` — confidence HIGH, reviewers: mira
  - Problem: (configureProcessGroup) Cancel returns non-ESRCH kill errors directly to exec.CommandContext, which ignores cancel errors and proceeds to Wait; the kill error is discarded and never reaches the caller, so a failed group kill (e.g. EPERM from a privilege-dropped child) is silent and indistinguishable from a clean kill
  - Fix: Return nil on non-ESRCH errors so the cancel is acknowledged but don&#39;t surface the kill failure; surface the failure separately via a result field (e.g. ReapError) or log it at minimum
  - Evidence: Cancel returns err directly; exec.CommandContext.cancel does not propagate cancel errors

### MEDIUM

- `internal/verify/localvalidate_pgroup_unix.go:26` — confidence HIGH, reviewers: mira
  - Problem: (configureProcessGroup) The group SIGKILL fires only when runCtx.Done() triggers Cancel; if the command exits 0 or non-zero before the timeout, Cancel never fires and any orphaned grandchildren survive indefinitely
  - Fix: Acceptable within the timeout-only epic scope but leaves a leak path if a validation command backgrounds children and exits; document the limitation explicitly
  - Evidence: No mechanism for the clean-exit case
- `internal/verify/localvalidate_pgroup_unix_test.go:47` — confidence HIGH, reviewers: dax, mira
  - Severity disagreement: LOW vs MEDIUM
  - Problem: (TestRunConfiguredValidation_TimeoutReapsGrandchild) processAlive uses bare PID without any uniqueness guarantee across the 5s Eventually window — OS PID recycling can flip the assertion to a false-negative failure or, if a new process happens to appear at that PID, a false-positive pass
  - Fix: Use kill(-pid, 0) to probe the whole group rather than a single recycled PID, or capture the child start-time via os.FindProcess and compare to confirm it&#39;s the original process
  - Evidence: [dax] return syscall.Kill(pid, syscall.Signal(0)) != syscall.ESRCH / [mira] processAlive probes a single PID that may be recycled

### LOW

- `internal/verify/localvalidate_pgroup_unix.go:33` — confidence HIGH, reviewers: mira, otto
  - Problem: (configureProcessGroup) errors.Is not used — bare &#96;err == syscall.ESRCH&#96; works today but breaks if Kill wraps the error in a wrapped sentinel (e.g. os.(*PathError) wrapping syscall.Errno)
  - Fix: Use errors.Is(err, syscall.ESRCH) and add a comment that this must track any future wrapping
  - Evidence: [mira] bare == comparison on line 33 / [otto] if err == syscall.ESRCH
