<think>
Let me analyze the diff systematically. The changes implement the resume functionality for code review panels as described in the sprint plan. Let me look at each file:

1. **README.md** - Documentation updates for `--resume` flag
2. **cmd/atcr/resume.go** - New file with `runResume` function
3. **cmd/atcr/resume_test.go** - New test file
4. **cmd/atcr/review.go** - Updates to handle `--resume` flag and updated `interruptMessage`
5. **cmd/atcr/review_test.go** - Updated tests
6. **internal/fanout/resume.go** - New file with resume logic
7. **internal/fanout/resume_test.go** - New test file
8. **internal/fanout/review.go** - Refactored to extract `runEngine` function

Let me focus on identifying issues within the scope of the sprint plan. The plan covers:
- Resume CLI flag
- Review state detection
- Roster filtering
- Incremental writes
- Git range validation
- Auto-reconciliation
- Tests

Let me look for issues:

1. **In `internal/fanout/resume.go`:**

The `buildPayloads` function is called in `PrepareResume`, but I should check the scope. Let me trace through:

- `PrepareResume` calls `buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)` 
- It uses `req.Range.Base` and `req.Range.Head` which were already validated against the manifest.

The `reconcile.Options` in `resumeReconcile` includes `Partial: fanout.ReadManifestPartial(dir)` - but this is called after `WriteManifest` so the manifest is already updated. The `Partial` is the value at the time of reading - this seems fine.

In `ExecuteResume`, I notice:
- `p.manifest = &m` is set after writing manifest. But this only affects the local `p.manifest` field. The caller in `runResume` (in `cmd/atcr/resume.go`) doesn't use `p.manifest` after this.

In `runResume` (cmd/atcr/resume.go):
- `result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)` is called
- Then it calls `resumeReconcile(ctx, cmd, result.Dir)` after success
- But `result.Dir` - need to check if `ReviewResult.Dir` is set

Looking at `ExecuteResume`:
```go
res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
```
Yes, `Dir` is set to `p.Dir`.

Now, in `resumeReconcile`:
```go
rec, err := reconcile.RunReconcile(ctx, dir, nil, reconcile.Options{
    ReconciledAt: time.Now(),
    Partial:      fanout.ReadManifestPartial(dir),
})
```

This calls `reconcile.RunReconcile` - need to check if this is an existing function in the codebase. Based on the diff, it appears to be a pre-existing function.

2. **In `cmd/atcr/resume.go` `resolveResumeDir` function:**

```go
func resolveResumeDir(anchor string) (string, error) {
    a := strings.TrimSpace(anchor)
    if a == "" || a == "latest" {
        return resolveReviewDir("") // empty arg → .atcr/latest
    }
    return resolveReviewDir(a)
}
```

The comment mentions that "latest" cannot be a real id, but it doesn't validate the path. If someone passes `--resume latest` but `.atcr/latest` doesn't exist, `resolveReviewDir` will return an error. That's correct.

3. **Potential issue in `runResume` (cmd/atcr/resume.go):**

```go
if cmd.Flags().Changed("fail-on") || ... {
```

Wait, looking at the test:
```go
func TestResume_FailOnFlagIsExit2(t *testing.T) {
    isolate(t)
    code, out := execResume(t, "review", "--resume", "latest", "--fail-on", "high")
    require.Equal(t, 2, code, "the one-shot gate is not supported with --resume")
    require.Contains(t, out, "does not support --fail-on")
}
```

The test expects an exit code 2. Looking at the implementation, `usageError` is called, which presumably returns a `codedError` with exit code 2. This is consistent.

4. **Critical observation in `runEngine` in `internal/fanout/review.go`:**

When `runEngine` was extracted from `ExecuteReview`, I need to verify the logic is identical. Let me compare:

Original `ExecuteReview`:
```go
poolDir := filepath.Join(p.Dir, "sources", "pool")
// ... tool harness setup ...
results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
// ... detect interrupt ...
// Write artifacts
// Build summary
// Finalize manifest
```

New version with `runEngine`:
```go
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
    // ... tool harness setup ...
    results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
    stage := reviewStageFor(results)
    if stage != nil {
        stage.SnapshotMode = snapMode
        stage.HeadSHA = snapHeadSHA
        stage.SnapshotWorktreePath = snapWorktreePath
    }
    return results, stage
}
```

The interrupt detection is now in `ExecuteResume` (only for `ExecuteReview`, not `runEngine`). Let me check `ExecuteResume`:

```go
func ExecuteResume(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
    poolDir := filepath.Join(p.Dir, "sources", "pool")
    results, _ := runEngine(ctx, completer, p, poolDir)
    interrupted := errors.Is(ctx.Err(), context.Canceled)
    // ...
}
```

But `runEngine` creates a child context with timeout:
```go
runCtx := ctx
if p.TimeoutSec > 0 {
    var cancel context.CancelFunc
    runCtx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
    defer cancel()
}
```

This is fine - the child context inherits from the parent, so cancellation propagates.

5. **Looking at `ExecuteResume` more carefully:**

The manifest partial flag is being set:
```go
m := *p.manifest
m.Partial = sum.Partial
m.CompletedAt = time.Now().UTC()
m.Interrupted = interrupted
```

But wait - the `m.TimeoutSecs` is not being set, but it's already in the original manifest. That's fine.

The `StartedAt` is preserved from the original manifest. Good.

6. **Issue in `runResume` (cmd/atcr/resume.go):**

```go
if err := preflightAPIKeys(prep.Slots); err != nil {
    return err // no pending slot can authenticate → exit 2 before any provider call
}
```

This calls `preflightAPIKeys` with the pending slots. If there's a partial agent that doesn't have its API key set, this will fail. That's reasonable behavior.

7. **Looking at the test `TestResume_AllCompleteReconcilesAndExitsZero`:**

The test calls:
```go
require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))
// ...
code, out := execResume(t, "review", "--resume", "latest", "--base", "HEAD^")
```

But `runResume` resolves the git range with the flags. Then it does `gitrange.Resolve(ctx, ".", gitrange.Options{Base: "HEAD^", ...})` and compares with the manifest. This should work.

Wait - the test uses `execCmd(t, "review", "--base", "HEAD^")` for the first review. So the manifest will store `HEAD^` resolved to a SHA. Then for the resume, the same `--base HEAD^` is passed, and it should resolve to the same SHA. Good.

8. **Issue in `runResume`:**

```go
res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
```

This is called with the flags. If the user doesn't pass any range flags (e.g., `--resume latest` without `--base/--head`), then the default detection mode is used. The manifest would have been created with the same default detection. So the resolved SHAs should match.

9. **Looking at `interruptedBeforeFanout`:**

The function is called in `runResume`:
```go
res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
if err != nil {
    if errors.Is(ctx.Err(), context.Canceled) {
        return interruptedBeforeFanout(cmd)
    }
    return usageError(fmt.Errorf("resume failed: %w", err))
}
```

This makes sense - if cancellation happens during range resolution, it's a pre-fan-out interrupt.

10. **Issue in `runResume` with the err handling for `fanout.ExecuteResume`:**

```go
result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)

if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}

if result != nil {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
        result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
}
if err != nil {
    return err // every agent (union) failed → exit 1, artifacts preserved
}
```

The `result` is checked before `err` is non-nil, but if `ExecuteResume` returns `(nil, err)`, then `result` is nil and the print is skipped. This is a potential issue if `ExecuteResume` returns `(nil, err)` - but looking at `ExecuteResume`, it always returns a non-nil `res`:
```go
res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
if sum.Total == 0 {
    return res, ErrEmptyRoster
}
if sum.Succeeded == 0 {
    return res, fmt.Errorf("%w: %s", ErrAllAgentsFailed, formatStatusFailures(statuses))
}
return res, nil
```

OK, `res` is always non-nil. But it could be returned with `Summary.Total == 0` and `ErrEmptyRoster`. In that case the print would show "0/0 agents succeeded" which is correct.

11. **In `runResume`, the `errors.Is(ctx.Err(), context.Canceled)` check after `ExecuteResume`:**

This is correct - it's the same pattern as the fresh review.

12. **In `PrepareResume`:**

```go
done, err := CompletedAgents(reviewDir)
if err != nil {
    return nil, nil, err
}
```

This calls `CompletedAgents(reviewDir)`. If the pool dir doesn't exist, it returns an empty set. If it does exist, it returns the completed agents. Good.

13. **Potential issue: `summary` vs `Summary` field naming:**

Looking at `RebuildPool`:
```go
sum := summarizeStatuses(statuses)
ps := PoolSummary{
    Agents:        statuses,
    Total:         sum.Total,
    Succeeded:     sum.Succeeded,
    Failed:        sum.Failed,
    Partial:       sum.Partial,
    TotalFindings: len(merged),
}
```

The `sum` returned is a `Summary` struct. This is correct.

14. **In `ExecuteResume`:**

```go
if err := writeResumedAgents(poolDir, results); err != nil {
    return nil, err
}

sum, statuses, err := RebuildPool(poolDir)
if err != nil {
    return nil, err
}
```

The order of operations is: write resumed agents' artifacts, then rebuild the pool (which reads from disk). This is correct.

But wait - if a re-run agent's previous artifacts exist (from a previous failed attempt), `writeResumedAgents` would overwrite them. That's the expected behavior.

15. **The `findingsFor(r)` function:**

This is called in `writeResumedAgents`. It's not in the diff but it's referenced. Let me check if it exists. The function is called as `fr := findingsFor(r)` and then passed to `writeAgentArtifacts`. The `findingsFor` function probably parses the agent's content into findings. This is an existing function.

16. **Potential issue in `ExecuteResume` with manifest write order:**

```go
m := *p.manifest
m.Partial = sum.Partial
m.CompletedAt = time.Now().UTC()
m.Interrupted = interrupted
if err := WriteManifest(p.Dir, &m); err != nil {
    return nil, err
}
p.manifest = &m
```

The manifest is updated even when the resume itself is interrupted. This is intentional (AC7).

17. **Issue in `runResume` (cmd/atcr/resume.go) with `interruptMessage`:**

```go
if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}
```

The `interruptMessage` function takes a `result` and `prep`. If `result` is nil (e.g., interrupt before `ExecuteResume` produces one), the function falls back to `prep.Dir` and `prep.ID`. This is the updated behavior.

18. **Issue in `runResume` with `AllComplete`:**

```go
if info.AllComplete() {
    _, _ = fmt.Fprintln(cmd.OutOrStdout(), "All configured agents already completed. Re-running reconciliation...")
    if err := fanout.ClearInterrupted(dir); err != nil {
        return usageError(fmt.Errorf("resume failed: %w", err))
    }
    return resumeReconcile(ctx, cmd, dir)
}
```

When all agents are complete, it clears the interrupted marker and reconciles. Good.

19. **Potential issue: When `AllComplete` is true, the test expects `RunCompleted` status.**

The test `TestResume_AllCompleteClearsStaleInterrupted` shows this behavior. The manifest's `Interrupted` is set to false, and after reconciliation, the derived status should be `RunCompleted`.

20. **Issue: The `RunInterrupted` status and `Partial` flag interaction.**

Looking at `ReadReviewStatus` - not in the diff but referenced. The status is derived from the manifest. The test expects `RunCompleted` after the resume clears the interrupted marker.

21. **In `PrepareResume`:**

```go
info := &ResumeInfo{}
for _, name := range configured {
    if done[name] {
        info.Completed = append(info.Completed, name)
    } else {
        info.Pending = append(info.Pending, name)
    }
}
```

The `configured` is `rosterNames(cfg.Project)` which is the configured roster. The `done` set is the agents with `StatusOK`. The pending list contains agents that are in the configured roster but not in the completed set.

But wait - what if an agent is in the `done` set but not in the configured roster? That could happen if a prior resume ran with a different roster. But the validation `ValidateResumeRoster` catches that case. So this should be safe.

22. **Issue: The `buildPayloads` function is called with the (validated) current range, but the function is the same as for a fresh review.**

Looking at the comment:
```go
// Payloads are rebuilt from the (validated-identical) recorded range so pending
// agents see exactly what the completed agents reviewed.
```

This is correct - the range has been validated to match the manifest, so rebuilding payloads from the current tree should produce the same payloads.

23. **Potential issue: `reconcile.RunReconcile` signature.**

The `resumeReconcile` function calls:
```go
rec, err := reconcile.RunReconcile(ctx, dir, nil, reconcile.Options{
    ReconciledAt: time.Now(),
    Partial:      fanout.ReadManifestPartial(dir),
})
```

The `nil` second argument and the `Options` struct are not in the diff. This relies on pre-existing code.

24. **Issue: `fanout.ReadManifestPartial(dir)` is called after `WriteManifest` has been called.**

In `ExecuteResume`:
```go
if err := WriteManifest(p.Dir, &m); err != nil {
    return nil, err
}
```

And in `resumeReconcile`:
```go
Partial: fanout.ReadManifestPartial(dir),
```

So `ReadManifestPartial` is called after the manifest has been written. The `Partial` value is `sum.Partial` from `RebuildPool`, which reflects the union of completed + newly run agents. Good.

25. **Issue: In `ExecuteResume`, the manifest is updated even on partial failure.**

If `runEngine` returns successfully but some agents failed, the manifest is still written. The `Partial` flag is set based on the union summary. Good.

26. **Looking at `cmd/atcr/resume_test.go`:**

The test `TestResume_IncompatibleWithIDIsExit2`:
```go
code, out := execResume(t, "review", "--resume", "latest", "--id", "x")
require.Equal(t, 2, code)
require.Contains(t, out, "--resume cannot be combined")
```

This should work because `runResume` checks for `--id` and `--output-dir` flags at the start.

27. **Potential issue: The `cliOverrides(cmd)` call in `runResume`.**

```go
cfg, err := fanout.LoadReviewConfig(".", cliOverrides(cmd))
```

This loads the config with CLI overrides. The `cliOverrides` is a pre-existing function. This should work.

28. **Issue: `runResume` doesn't handle the case where `cfg.Registry.ProjectProviderBanner()` returns a banner that includes the `latest` text.**

Wait, this is just printing a banner, not parsing. Not an issue.

29. **Issue: `prep.ID` is used in the output.**

In `runResume`:
```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(), "resuming review %s: %d completed, %d pending (%s)\n",
    prep.ID, len(info.Completed), len(info.Pending), strings.Join(info.Pending, ", "))
```

The `prep.ID` is `filepath.Base(reviewDir)`, which is the directory name. This should be the review ID.

30. **Potential issue: The `result.ID` and `prep.ID` in the success message.**

```go
if result != nil {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
        result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
}
```

This prints the review ID and the success count. Looks correct.

31. **Issue: The `preflightAPIKeys` function is called with `prep.Slots` which contains only pending agents.**

This is correct - we only need API keys for agents that will actually run.

32. **Issue: In `PrepareResume`, the `buildSlots` function is called.**

This builds the full roster of slots (all agents), then `filterPendingSlots` removes the completed ones. This is correct.

But wait - `buildSlots` might depend on the payload mode and other config. Let me check... The function signature is `buildSlots(cfg, payloads, req.Range)`. It should produce slots for all configured agents. Good.

33. **Potential issue: `buildPayloads` is called even when all agents are already complete.**

Looking at `PrepareResume`:
```go
payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
if err != nil {
    return nil, nil, err
}
slots, _, err := buildSlots(cfg, payloads, req.Range)
```

This builds payloads and slots even when all agents are already complete. This is wasted work in the `AllComplete` case. But it's a minor performance issue, not a correctness issue. Could be optimized by checking `AllComplete` first.

Actually, looking at the `AllComplete` check in `runResume`:
```go
if info.AllComplete() {
    // ...
    return resumeReconcile(ctx, cmd, dir)
}
```

This is after `PrepareResume` returns. So the payloads and slots are built even when all complete. This is inefficient but not incorrect.

34. **Issue: `runResume` calls `prep.ID` and `result.Dir` but `result` might be nil.**

Looking at:
```go
if result != nil {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
        result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
}
```

The nil check is there. Good.

35. **Potential issue: The `runEngine` function is now exported via the file structure.**

Actually, `runEngine` is in `internal/fanout/review.go` (lowercase, unexported). It's not in the diff as exported. The function signature is:
```go
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage)
```

This is fine for internal use.

36. **Issue: `runEngine` doesn't handle the `interrupted` flag.**

The original `ExecuteReview` had:
```go
interrupted := errors.Is(ctx.Err(), context.Canceled)
```

This was moved to `ExecuteResume`. In `ExecuteReview`:
```go
results, stage := runEngine(ctx, completer, p, poolDir)
interrupted := errors.Is(ctx.Err(), context.Canceled)
```

This is still there. Good.

37. **Potential issue: The `cmd/atcr/resume.go` file imports `time` but doesn't use it directly.**

Looking at the imports:
```go
import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"
    // ...
)
```

And the uses of `time`:
```go
StartedAt: time.Now(),
```
and
```go
ReconciledAt: time.Now(),
```

OK, `time` is used. Good.

38. **Issue: The `interruptMessage` function is called with `result` and `prep`.**

The `interruptMessage` function signature is:
```go
func interruptMessage(result *fanout.ReviewResult, prep *fanout.PreparedReview) string
```

The `prep` is `*fanout.PreparedReview`. The `prep` is the result of `PrepareResume`, which returns `*PreparedReview`. The `prep` is not nil here.

39. **Issue: The `prep.ID` and `result.Dir` are used in `interruptMessage`.**

These are set correctly in `PrepareResume` and `ExecuteResume`.

40. **Final check: The `cmd/atcr/resume.go` file has a comment about the resume flag.**

The `resolveResumeDir` function has a good comment explaining the behavior. The `runResume` function has a good comment explaining the flow.

41. **Issue: The test `TestResume_RosterMismatchIsExit2` expects the error message "roster changed".**

```go
require.Contains(t, out, "roster changed")
```

The error is generated by `rosterMismatch`:
```go
return fmt.Errorf("%w: recorded [%s], configured [%s]; start a fresh `atcr review`",
    ErrRosterChanged, strings.Join(r, " "), strings.Join(c, " "))
```

The error message contains "roster changed" (from `ErrRosterChanged = errors.New("the configured roster changed since the interrupted review")`). Good.

42. **Issue: The test `TestResume_RangeMismatchIsExit2` expects "working tree changed".**

```go
require.Contains(t, out, "working tree changed")
```

The error is generated by `ValidateResumeRange`:
```go
return fmt.Errorf("%w: recorded %s..%s, current %s..%s; start a fresh `atcr review`",
    ErrRangeChanged, shortRef(m.Base), shortRef(m.Head), shortRef(cur.Base), shortRef(cur.Head))
```

The error message contains "working tree changed" (from `ErrRangeChanged = errors.New("the working tree changed since the interrupted review (git base/head differ)")`). Good.

43. **Issue: The `TestResume_AllCompleteClearsStaleInterrupted` test writes a manifest with `Interrupted = true` after a real review.**

```go
m.Interrupted = true
require.NoError(t, fanout.WriteManifest(dir, m))
```

This simulates the narrow window where the signal landed after the last agent wrote ok. Then the resume should clear the marker.

44. **Issue: The `reconcile.RunReconcile` function call in `resumeReconcile`.**

This function is not in the diff but is referenced. It should be a pre-existing function.

45. **Issue: The `interruptedBeforeFanout(cmd)` function is called in `runResume`.**

This is a pre-existing function. The comment in `runResume` says:
```go
if errors.Is(ctx.Err(), context.Canceled) {
    return interruptedBeforeFanout(cmd)
}
```

This is a pre-existing function that handles the pre-fan-out interrupt case. Good.

46. **Issue: The `cliOverrides(cmd)` function.**

This is a pre-existing function. It returns CLI overrides for the config.

47. **Issue: The `correlateReviewID(ctx, prep.ID)` function.**

This is a pre-existing function. It correlates the context with the review ID.

48. **Issue: The `log.NewContext(ctx, ...)` call.**

This is a pre-existing function. It sets up logging with redaction.

49. **Issue: The `resolveRedactRoot(ctx, prep.Repo)` function.**

This is a pre-existing function. It returns the redaction root.

50. **Issue: The `preflightAPIKeys(prep.Slots)` function.**

This is a pre-existing function. It checks if all pending agents have their API keys set.

51. **Issue: The `runEngine` function extracts tool harness setup.**

This is a refactoring change. Let me check if it's correct.

Original:
```go
poolDir := filepath.Join(p.Dir, "sources", "pool")
// ... tool harness setup ...
results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
// ... interrupt detection ...
// ... write artifacts ...
// ... rebuild pool ...
// ... finalize manifest ...
```

New (after refactoring):
```go
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
    runCtx := ctx
    if p.TimeoutSec > 0 {
        var cancel context.CancelFunc
        runCtx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutSec)*time.Second)
        defer cancel()
    }
    // ... tool harness setup ...
    results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
    stage := reviewStageFor(results)
    if stage != nil {
        stage.SnapshotMode = snapMode
        stage.HeadSHA = snapHeadSHA
        stage.SnapshotWorktreePath = snapWorktreePath
    }
    return results, stage
}
```

The `poolDir` is now passed as a parameter. The `runEngine` returns results and stage. The `ExecuteReview` then uses these to write artifacts, rebuild pool, and finalize manifest.

This is a clean refactoring. The `ExecuteResume` uses `runEngine` with its own poolDir and doesn't write artifacts (it uses `writeResumedAgents` and `RebuildPool` instead).

52. **Potential issue: In `ExecuteResume`, the manifest is not updated with the `Review` field.**

Looking at:
```go
m := *p.manifest
m.Partial = sum.Partial
m.CompletedAt = time.Now().UTC()
m.Interrupted = interrupted
```

The `m.Review` is not set. But `p.manifest` already has the `Review` field set (from the original run). The `m := *p.manifest` copies it, so `m.Review` is preserved. Good.

53. **Issue: The `runEngine` returns `*payload.ReviewStage` but `ExecuteResume` discards it.**

```go
results, _ := runEngine(ctx, completer, p, poolDir)
```

The `_` discards the stage. This is because `ExecuteResume` uses `m.Review` from the original manifest. Good.

54. **Potential issue: The `interrupted` flag in `ExecuteResume`.**

```go
results, _ := runEngine(ctx, completer, p, poolDir)
interrupted := errors.Is(ctx.Err(), context.Canceled)
```

This checks for cancellation after `runEngine` returns. The `runEngine` creates a child context with timeout. If the parent context is cancelled, the child is also cancelled. The `runEngine` returns, and then the parent context's cancellation is checked.

But wait - the `runEngine` uses `defer cancel()` for the child context. The child context is cancelled when `runEngine` returns. But the parent context's cancellation is what we want to check.

The check `errors.Is(ctx.Err(), context.Canceled)` is on the parent context, which is correct.

55. **Issue: The `result.Summary.Partial` is set correctly.**

The `RebuildPool` returns `sum.Partial` based on the union of all agents. If some succeeded and some failed, `Partial` is true. If all succeeded, `Partial` is false. Good.

56. **Potential issue: The `interrupted` flag overrides the `Partial` flag in the manifest?**

No, they are separate fields. `Partial` is set from `sum.Partial` and `Interrupted` is set from the context cancellation. Both are written to the manifest.

57. **Issue: The `cmd/atcr/resume.go` doesn't handle the case where the `--resume` anchor is a path that doesn't exist.**

The `resolveResumeDir` function delegates to `resolveReviewDir` for non-"latest" anchors. If the path doesn't exist, `resolveReviewDir` should return an error. This is tested in `TestResolveResumeDir_UnknownIDErrors`.

58. **Issue: The `cmd/atcr/resume.go` doesn't handle the case where the anchor is a path to a file instead of a directory.**

This is not explicitly tested. If the user passes a file path, `resolveReviewDir` might return an error or might pass the validation. The comment says "verifies the directory holds a sources/ tree". So if it's not a directory or doesn't have sources/, it should error.

59. **Issue: The `runResume` function uses `cliOverrides(cmd)` to get CLI overrides.**

This is a pre-existing function. It should return the CLI flag values that override the config.

60. **Final review of the scope:**

The sprint plan scope is:
- Adding `--resume` flag
- Review state detection
- Roster filtering
- Incremental writing
- Git range validation
- Integration tests

All of these are implemented in the diff. The refactoring of `ExecuteReview` to extract `runEngine` is a supporting change. The `interruptMessage` update is a supporting change.

61. **Potential issue: In `PrepareResume`, the `buildPayloads` is called before the `AllComplete` check.**

If all agents are already complete, the payloads are still built. This is wasted work. But it's a minor performance issue.

62. **Potential issue: In `PrepareResume`, the `buildSlots` is called even when all agents are complete.**

Same as above. Minor performance issue.

63. **Potential issue: The `runEngine` function has a parameter `poolDir` that is computed in both `ExecuteReview` and `ExecuteResume`.**

This is fine - it allows the caller to specify the pool directory.

64. **Issue: The `ExecuteResume` function doesn't update the manifest with the `Review` field from the new run.**

The `Review` field is from the original run's `reviewStageFor(results)`. The resumed run's `reviewStageFor` is discarded. This is intentional because the `Review` field records the snapshot provenance from the original run, and the resumed run should preserve the same provenance.

But wait - the original run's `Review` field might not be set if no agent ran with tools. In that case, `m.Review` is nil. The resumed run might have tool-enabled agents, but the `Review` field is not updated. This is a potential issue.

Actually, looking at the original `ExecuteReview`:
```go
m.Review = reviewStageFor(results)
// Stamp the snapshot provenance
if m.Review != nil {
    m.Review.SnapshotMode = snapMode
    m.Review.HeadSHA = snapHeadSHA
    m.Review.SnapshotWorktreePath = snapWorktreePath
}
```

The `Review` field is set based on the results of the current run. In `ExecuteResume`:
```go
results, _ := runEngine(ctx, completer, p, poolDir)
// ...
m := *p.manifest
m.Partial = sum.Partial
m.CompletedAt = time.Now().UTC()
m.Interrupted = interrupted
```

The `m.Review` is preserved from `p.manifest`, which is the original manifest. The new run's `Review` field is discarded.

This could be an issue if the original run didn't have any tool-enabled agents but the resumed run does (or vice versa). The `Review` field would be stale.

But the plan says "the panel configuration is locked" and "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent". So the assumption is that the tool-enabled agents are the same. Good.

65. **Potential issue: The `m.Review` might have a different `SnapshotMode` or `HeadSHA` if the snapshot is regenerated.**

The `runEngine` in `ExecuteResume` would create a new snapshot if there are tool-enabled agents. But the `m.Review` is preserved from the original. The new snapshot's provenance is not recorded in the manifest.

This is a potential issue. The `ExecuteResume` should update `m.Review` with the new snapshot provenance, or at least preserve the original.

Actually, the comment in the code says:
```go
// m.Review is preserved from the original run: the roster is locked, so the
// original review stage already lists every tool agent (reviewStageFor records
// ToolsRequested even on a failed agent), and the resumed subset cannot add new
// members.
```

But the `SnapshotMode`, `HeadSHA`, and `SnapshotWorktreePath` might be different if the snapshot is regenerated. The original might have had a different snapshot mode or worktree path.

This is a minor issue. The resumed run regenerates the snapshot, but the manifest records the original snapshot provenance. This could lead to confusion if the snapshot path is different.

But this is a design decision, not a bug. The plan says to preserve the original manifest's state.

66. **Final check: The `cmd/atcr/resume.go` file.**

The file imports are correct. The functions are well-documented. The flow is clear.

67. **Final check: The `internal/fanout/resume.go` file.**

The file imports are correct. The functions are well-documented. The flow is clear.

68. **Final check: The `internal/fanout/review.go` file.**

The refactoring is clean. The `runEngine` function is well-documented.

69. **Potential issue: The `interrupted` flag in `ExecuteResume` is set even if the cancellation happened during `runEngine`'s tool harness setup.**

The `runEngine` creates a child context with timeout. The `defer cancel()` is called when `runEngine` returns. The parent context's cancellation is checked after `runEngine` returns.

If the parent context is cancelled during tool harness setup, the `runEngine` would still complete (or error), and then the check would catch it. Good.

70. **Potential issue: The `cmd/atcr/resume.go` doesn't check if the `result` from `ExecuteResume` is nil before calling `result.Summary.Succeeded`.**

Actually, it does:
```go
if result != nil {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
        result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
}
```

But the `result` is always non-nil (as analyzed earlier). So the check is defensive. Good.

71. **Issue: The `err` from `ExecuteResume` is not checked before the `result` print.**

```go
result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)

if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}

if result != nil {
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "review %s: %d/%d agents succeeded (%s)\n",
        result.ID, result.Summary.Succeeded, result.Summary.Total, result.Dir)
}
if err != nil {
    return err
}
```

If `ExecuteResume` returns `(result, err)` with err being `Err
</think>
