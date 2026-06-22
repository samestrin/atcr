<think>
Let me carefully analyze the diff to find issues related to the sprint plan (Epic 7.0: Executor Model for Fix Generation).

The diff touches:
1. `examples/registry-with-executor.yaml` - new example file
2. `examples/registry-without-executor.yaml` - new example file
3. `internal/reconcile/confidence.go` - adds `ConfidenceAtOrAbove` function
4. `internal/reconcile/confidence_test.go` - new test file
5. `internal/reconcile/emit.go` - adds `FixWarning` field
6. `internal/registry/config.go` - adds `ExecutorConfig` struct and validation
7. `internal/registry/examples_test.go` - new test file
8. `internal/registry/executor_config_test.go` - new test file
9. `internal/verify/executor.go` - new file with fix generation logic
10. `internal/verify/executor_test.go` - new test file
11. `internal/verify/pipeline.go` - integrates fix generation into pipeline

Let me look for issues:

### 1. `internal/verify/pipeline.go` - Fix phase snapshot building logic

Looking at lines 162-186 (the new block):

```go
if reg.Executor != nil && !needTool {
    fixMinSev := reg.Executor.MinSeverity
    if fixMinSev == "" {
        fixMinSev = registry.DefaultFixMinSeverity
    }
    for _, f := range findings {
        if reconcile.ConfidenceAtOrAbove(f.Confidence, reconcile.ConfHigh) && meetsSeverityFloor(f.Severity, fixMinSev) {
            needTool = true
            break
        }
    }
}
```

Wait, there's a comment: "This branch runs only when no skeptic needs the tool harness, so no finding here will be promoted to VERIFIED by verification — current confidence is the final confidence for the gate."

But the verification happens after the tool harness is built. This is the pre-verification phase where we determine whether to build the snapshot. The findings' confidence at this point is pre-verification. So the comment says current confidence is final confidence.

But wait, this block is in the section where the tool harness is being decided. The order of operations in `runVerify` would be:
1. Determine need for tool harness
2. Build the harness if needed
3. Run verification (which promotes some findings to VERIFIED)
4. Recompute confidence
5. Run generateFixes (NEW - Epic 7.0)

The snapshot building decision in the new code only considers the current (pre-verification) confidence, not the post-verification confidence. So a finding that will be promoted to VERIFIED by verification might not get a fix because it wasn't HIGH at this point.

Actually, looking more carefully, the fix generation phase runs AFTER verdict application and confidence recompute (per the comment in executor.go: "after verdict application / confidence recompute"). So the actual fix generation will use the final (post-verification) confidence.

The issue is that the snapshot building decision is based on pre-verification confidence, but the actual fix generation is based on post-verification confidence. A finding that is MEDIUM pre-verification might be promoted to VERIFIED post-verification, but the snapshot might not be built because at the time of the decision, the finding was MEDIUM.

Wait, let me re-read the code more carefully...

```go
if reg.Executor != nil && !needTool {
```

So this only runs when `needTool` is false (no skeptic needs it). If no skeptic is configured, verification won't promote any findings to VERIFIED, so the comment is correct. But if the previous skeptic tool check set `needTool = true`, this whole block is skipped.

The comment says "This branch runs only when no skeptic needs the tool harness, so no finding here will be promoted to VERIFIED by verification." So in this branch, no finding will be promoted. OK, that makes sense.

But wait, there's still an issue. The fix generation also wants the snapshot for snippet reading. If `needTool` is already true (because of skeptics), the snapshot is already built. But the fix generation phase still needs to run.

Let me check the fix generation again:

```go
if reg.Executor != nil {
    generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)
}
```

And `disp` is the dispatcher. If `needTool` was false and the new executor-only check also doesn't set it to true, then... hmm, does `disp` still get built? Let me check more context.

Actually, I need to look at more of the pipeline.go to see how `disp` is constructed. The diff doesn't show the full pipeline. Let me check the existing code context.

From the diff:
```
@@ -224,6 +248,14 @@ func runVerify(ctx context.Context, reviewDir string, reg *registry.Registry, op
 		findings[i].Confidence = confidenceV2(findings[i].Confidence, v.Verdict)
 	}
 
+	// Fix-generation phase (Epic 7.0): once confidence is final, ask the single
+	// executor model for a minimal fix on every HIGH-or-better finding above the
+	// fix severity floor, populating the FIX column before the artifacts are
+	// serialized. A nil executor leaves findings untouched (no behavior change).
+	if reg.Executor != nil {
+		generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)
+	}
```

This is at line 248 (new). It uses `disp`. So `disp` must be initialized earlier.

OK so my concern is: the fix generation uses `disp` (the dispatcher) for snippet reading. The snapshot building decision is based on the new block. If `reg.Executor != nil` and `!needTool`, the new block checks if any finding has HIGH-or-better confidence AND meets severity floor. If not, `needTool` stays false.

But what if a finding will be promoted to VERIFIED by verification? The comment says "This branch runs only when no skeptic needs the tool harness, so no finding here will be promoted to VERIFIED by verification." So if no skeptic is configured, the tool harness isn't built, and no findings are promoted. So the comment is correct.

But what if `needTool` is already true (skeptic needs it)? Then the new block is skipped, and `needTool` stays true. The snapshot is built. Good.

But what if `needTool` is true but only because of skeptics, and no findings meet the executor's fix criteria? Then the snapshot is built but no fix generation happens. That's fine, but wasteful (snapshot built but not used for executor).

OK, that's an optimization concern but not a bug.

### 2. `internal/verify/executor.go` - Issue with `prov, ok := reg.Providers[ex.Provider]`

The `generateFixes` function checks if the provider exists:
```go
prov, ok := reg.Providers[ex.Provider]
if !ok {
    logPipelineWarning(log.FromContext(ctx), "executor_unknown_provider", ex.Provider)
    return
}
```

The comment says "Defense-in-depth: registry validation already guarantees the executor's provider is defined, so this guard never fires on a validated registry."

But what about when `reg` is nil? Let me check... The function signature is:
```go
func generateFixes(ctx context.Context, findings []reconcile.JSONFinding, ex *registry.ExecutorConfig, reg *registry.Registry, complete executorCompleter, disp Dispatcher)
```

If `reg` is nil, this would panic on `reg.Providers`. The function checks `ex == nil || complete == nil` but not `reg == nil`.

Looking at the test:
```go
generateFixes(context.Background(), findings, nil, execRegistry("MEDIUM"), &recordingExecutor{}, okDispatcher())
generateFixes(context.Background(), findings, execConfig("MEDIUM"), execRegistry("MEDIUM"), nil, okDispatcher())
```

The test passes a non-nil registry. But the function doesn't guard against nil registry.

In the pipeline:
```go
if reg.Executor != nil {
    generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)
}
```

`reg` is always non-nil here (it's a parameter to `runVerify`). So in production, `reg` is always non-nil. But for robustness, the function should guard against nil `reg`.

### 3. `internal/verify/executor.go` - `readFixSnippet` start line calculation

```go
start := line - fixSnippetRadius
if start < 1 {
    start = 1
}
end := line + fixSnippetRadius
```

If `line` is 0 or negative, `start` would be `-30` or similar, but the check `if start < 1` catches that. But what if `line` is exactly 0? `start` would be `-30`, then set to `1`. `end` would be `30`. OK.

What if `line` is negative? `start` would be even more negative, set to `1`. `end` would be `line + 30` which could be negative or small. The dispatcher would then try to read lines `1` to `end`. If `end` is negative, that's a problem. But the function returns the content regardless of what the dispatcher does with it.

Actually, the function doesn't validate `line > 0`. If `line` is 0 or negative, the calculation could produce invalid ranges. But this is a minor concern.

### 4. `internal/verify/executor.go` - `appendFixAttribution` idempotency

```go
func appendFixAttribution(evidence, name string) string {
    attr := fixAttributionPrefix + name
    if strings.TrimSpace(evidence) == "" {
        return attr
    }
    if strings.Contains(evidence, attr) {
        return evidence
    }
    return evidence + "; " + attr
}
```

This looks correct. If evidence already contains the attribution, return as-is. Otherwise, append.

### 5. `internal/verify/executor.go` - `meetsSeverityFloor` function

The `meetsSeverityFloor` function is called but not defined in the diff. It's presumably defined elsewhere in the `verify` package. Let me check if it's a new function or existing.

Looking at the diff, `meetsSeverityFloor` is called in:
- `executor.go`: `if !meetsSeverityFloor(f.Severity, minSev)`
- `pipeline.go`: `reconcile.ConfidenceAtOrAbove(f.Confidence, reconcile.ConfHigh) && meetsSeverityFloor(f.Severity, fixMinSev)`

But `meetsSeverityFloor` is not defined in the diff. It must be an existing function in the `verify` package. Let me check if there's a similar pattern.

Actually, the diff doesn't show the full pipeline.go or executor.go, so `meetsSeverityFloor` could be defined in the same package (`verify`) but not in the diff. This is fine.

### 6. `internal/verify/pipeline.go` - Fix phase snapshot building logic

Wait, I need to re-examine this. The new block in pipeline.go:

```go
if reg.Executor != nil && !needTool {
    fixMinSev := reg.Executor.MinSeverity
    if fixMinSev == "" {
        fixMinSev = registry.DefaultFixMinSeverity
    }
    for _, f := range findings {
        if reconcile.ConfidenceAtOrAbove(f.Confidence, reconcile.ConfHigh) && meetsSeverityFloor(f.Severity, fixMinSev) {
            needTool = true
            break
        }
    }
}
```

This runs BEFORE verification. The confidence at this point is the pre-verification confidence. The comment says "This branch runs only when no skeptic needs the tool harness, so no finding here will be promoted to VERIFIED by verification — current confidence is the final confidence for the gate."

But what if `needTool` is already true (skeptic needs it)? Then this block is skipped, and `needTool` stays true. The snapshot is built. Good.

What if `needTool` is false (no skeptic) and the executor is configured? Then this block checks if any finding meets both the confidence and severity criteria. If yes, `needTool` is set to true, and the snapshot is built. If no, `needTool` stays false, and no snapshot is built.

But what if a finding is MEDIUM confidence pre-verification and MEDIUM severity, and the fix floor is MEDIUM? Then it meets the severity floor but not the confidence floor (needs HIGH-or-better). So `needTool` stays false. But the finding won't get a fix anyway (confidence is below HIGH), so this is correct.

What if a finding is MEDIUM confidence pre-verification and HIGH severity, and the fix floor is MEDIUM? Then it meets the severity floor but not the confidence floor. So `needTool` stays false. The finding won't get a fix (confidence is below HIGH), so this is correct.

What if a finding is HIGH confidence pre-verification and LOW severity, and the fix floor is MEDIUM? Then it meets the confidence floor but not the severity floor. So `needTool` stays false. The finding won't get a fix (severity is below MEDIUM), so this is correct.

OK, the logic seems correct for the case where no skeptic is configured.

But wait, there's a subtle issue. The comment says "no finding here will be promoted to VERIFIED by verification." But what about findings that are already VERIFIED pre-verification? If a finding is already VERIFIED, it would meet the confidence floor (VERIFIED >= HIGH). But how would a finding be VERIFIED before verification? Maybe through a previous run? Or maybe VERIFIED is a confidence tier that can be set by the reconcile stage.

Actually, looking at the confidence.go file, `ConfidenceVerified` is a confidence tier. It could be set by the reconcile stage (e.g., if a finding was verified in a previous run and is being re-processed). So a finding could be VERIFIED pre-verification.

In that case, the new block would correctly identify it as meeting the confidence floor and set `needTool` to true. Good.

### 7. `internal/verify/executor.go` - `meetsSeverityFloor` with empty severity

What if `f.Severity` is empty? The `meetsSeverityFloor` function is called with `f.Severity`. If `f.Severity` is empty, the function might return false (empty is below any floor). This is probably correct behavior.

### 8. `internal/registry/config.go` - `validateExecutor` - `reviewSeverities` reference

```go
if normalized := stream.NormalizeSeverity(e.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
    errs = append(errs, fmt.Errorf("executor: min_severity_for_fix must be one of CRITICAL, HIGH, MEDIUM, LOW, got %q", e.MinSeverity))
}
```

`reviewSeverities` is referenced but not defined in the diff. It's presumably a map defined elsewhere in the `registry` package. Let me check if this is correct.

Looking at the existing code, there might be a `reviewSeverities` map that maps severity strings to bool. The error message says "CRITICAL, HIGH, MEDIUM, LOW" but what about "BLOCKER" or other severities? The test `TestExecutor_InvalidMinSeverityForFix` uses "BLOCKER" and expects an error, so "BLOCKER" is not a valid severity. This seems correct.

### 9. `internal/registry/config.go` - `applyDefaults` - `Name` default

```go
if r.Executor.Name == "" {
    r.Executor.Name = RoleExecutor
}
```

The default name is "executor". But in the example file, the name is "opus". If a user doesn't specify a name, it defaults to "executor". This is fine, but the test `TestExecutor_DefaultsApplied` doesn't check the name. Let me check if there's a test for the name default.

Looking at the tests:
- `TestExecutor_AbsentByDefault` - no executor, so name is irrelevant
- `TestExecutor_ParsedWhenPresent` - explicit name "opus", checks name
- `TestExecutor_DefaultsApplied` - no name, doesn't check name
- `TestExecutor_MinSeverityForFixExplicitAndNormalized` - no name, doesn't check name

So the name default is not tested. But this is a minor concern.

### 10. `internal/verify/executor.go` - `meetsSeverityFloor` function

The function `meetsSeverityFloor` is called but not defined in the diff. It's presumably defined in the `verify` package. Let me check if it exists.

Actually, I realize I can't check the full codebase. The diff doesn't show the definition of `meetsSeverityFloor`. If it's a new function added in this PR, it should be in the diff. If it's an existing function, it should exist in the codebase.

Given the context (the function is called in both `executor.go` and `pipeline.go`), it's likely a new function added in this PR but not shown in the diff (perhaps in a separate file or in the same file but not in the diff context).

Wait, the diff for `executor.go` shows the full new file. So `meetsSeverityFloor` is not in `executor.go`. It must be in another file in the `verify` package, either existing or new.

Let me check `pipeline.go` - the diff shows changes to `pipeline.go` but doesn't show the definition of `meetsSeverityFloor`. So it must be in another file.

This is a concern - if `meetsSeverityFloor` is not defined anywhere, the code won't compile. But since the tests are written and presumably pass, it must be defined somewhere.

### 11. `internal/verify/pipeline.go` - Fix phase runs after verdict application

Looking at the code flow:
1. Tool harness built (if needed)
2. Verification runs (skeptics check findings)
3. Verdict application: `findings[i].Confidence = confidenceV2(findings[i].Confidence, v.Verdict)`
4. Fix generation: `generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)`

Wait, the fix generation runs AFTER verdict application. So the confidence used in the fix generation gate is the post-verification confidence. This is correct.

But the snapshot building decision (in the new block) uses pre-verification confidence. The comment explains this: "This branch runs only when no skeptic needs the tool harness, so no finding here will be promoted to VERIFIED by verification — current confidence is the final confidence for the gate."

OK, this is consistent.

### 12. `internal/verify/executor.go` - `meetsSeverityFloor` with normalized severity

The `meetsSeverityFloor` function is called with `f.Severity` and `minSev`. The `minSev` is normalized in `applyDefaults`:
```go
r.Executor.MinSeverity = stream.NormalizeSeverity(r.Executor.MinSeverity)
```

But `f.Severity` is not normalized. If `f.Severity` is "high" (lowercase), the comparison might fail. This depends on how `meetsSeverityFloor` is implemented.

This is a potential issue - the function should normalize both sides for a correct comparison.

### 13. `internal/verify/executor.go` - `callExecutor` timeout

```go
func callExecutor(ctx context.Context, complete executorCompleter, prov registry.Provider, ex *registry.ExecutorConfig, prompt string) (string, error) {
    callCtx := ctx
    if ex.TimeoutSecs != nil && *ex.TimeoutSecs > 0 {
        var cancel context.CancelFunc
        callCtx, cancel = context.WithTimeout(ctx, time.Duration(*ex.TimeoutSecs)*time.Second)
        defer cancel()
    }
    return complete.Complete(callCtx, llmclient.Invocation{...})
}
```

The timeout is applied per-call. If the timeout is 120 seconds and the call takes 130 seconds, the context is cancelled and the call returns an error. The error is then caught in `generateFixes` and a `FixWarning` is set. This is correct.

But wait, the `defer cancel()` is called after `complete.Complete` returns. This is correct - the cancel function is called when `callExecutor` returns, which is after `complete.Complete` returns. So the context is not cancelled while `complete.Complete` is running. Good.

Actually, wait. The `defer cancel()` is inside the `if` block. So if the timeout is not set, `cancel` is not called (because it's not created). If the timeout is set, `cancel` is created and deferred. This is correct.

### 14. `internal/verify/executor.go` - `buildFixPrompt` line number prefix

The prompt includes the snippet with a note: "each line is prefixed with its line number as `N: `; do NOT include those line-number prefixes in your fix".

But the snippet returned by `readFixSnippet` is the raw content from the dispatcher. The dispatcher returns `res.Content`, which is the content of the file (lines). The prompt builder says each line is prefixed with its line number, but the snippet itself might not have those prefixes.

This is a discrepancy - the prompt tells the executor that lines are prefixed with line numbers, but the actual snippet might not have those prefixes. This could confuse the executor.

Wait, let me re-read the code:

```go
fmt.Fprintf(&b, "\nSource context around %s:%d (each line is prefixed with its line number as `N: `; do NOT include those line-number prefixes in your fix):\n```\n%s\n```\n", f.File, f.Line, snippet)
```

The prompt says lines are prefixed with `N: `. But the snippet is the raw content from `res.Content`. If the dispatcher doesn't add line number prefixes, the prompt is misleading.

This is a potential issue - the prompt builder assumes the snippet has line number prefixes, but the snippet might not have them. The executor would then either:
1. Trust the prompt and not include the prefixes (correct)
2. Not trust the prompt and include the raw content (incorrect)

The correct behavior depends on whether the dispatcher adds line number prefixes. This is an assumption that might not hold.

### 15. `internal/verify/executor.go` - `readFixSnippet` doesn't handle snapshot unavailability gracefully

The function returns "" if the dispatcher is nil or the read fails. But the comment in `generateFixes` says: "On snapshot unavailability, fix generation degrades gracefully (warning, finding still emitted) rather than failing the run."

The function doesn't log a warning when the snapshot is unavailable. It silently returns "". This is a minor concern - the warning is not logged, so the user might not know that the snippet was not included.

But looking at the code, the `generateFixes` function doesn't check if the snippet is empty and log a warning. It just proceeds with an empty snippet. This means the executor works from the finding text alone, which is the documented behavior. So this is correct.

### 16. `internal/registry/config.go` - `RoleExecutor` constant

The constant `RoleExecutor = "executor"` is added. But the comment says: "It is NOT a reviewer-panel role: an executor lives in the top-level executor: block, not in agents:, so roleValid (which gates agent roles) intentionally excludes it."

This is correct - the executor is not an agent role. The validation in `validateExecutor` checks the role separately.

### 17. `internal/verify/executor.go` - `meetsSeverityFloor` function not shown

As I mentioned, `meetsSeverityFloor` is called but not defined in the diff. This is a concern - if it's a new function, it should be in the diff. If it's an existing function, it should exist in the codebase.

Let me assume it's defined somewhere and move on.

### 18. `internal/verify/executor.go` - `generateFixes` mutates findings in place

The function takes `findings []reconcile.JSONFinding` and mutates them in place. This is correct for the use case, but it could be a concern if the caller doesn't expect mutation.

In `pipeline.go`:
```go
generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)
```

The `findings` slice is the local slice in `runVerify`. Mutating it is fine.

### 19. `internal/verify/executor.go` - `executorCompleter` interface

```go
type executorCompleter interface {
    Complete(ctx context.Context, inv llmclient.Invocation) (string, error)
}
```

This is a minimal interface that `*llmclient.Client` satisfies. This is good for testability.

### 20. `internal/verify/pipeline.go` - Fix phase snapshot building logic - edge case

What if `reg.Executor` is configured but `reg.Executor.MinSeverity` is set to a value that no finding meets? Then the new block doesn't set `needTool` to true. The snapshot is not built. The fix generation phase runs with `disp = nil` (or whatever the default is). The snippet is empty. The executor works from the finding text alone. This is correct.

But wait, what if `reg.Executor` is configured and `needTool` is already true (skeptic needs it)? Then the new block is skipped (because of `!needTool`). The snapshot is built for the skeptic. The fix generation phase uses the same snapshot. Good.

But what if the snapshot is built for the skeptic but no finding meets the executor's criteria? Then the fix generation phase is a no-op (no findings to fix). The snapshot was built but not used for the executor. This is wasteful but not incorrect.

### 21. `internal/verify/executor.go` - `appendFixAttribution` separator

```go
return evidence + "; " + attr
```

The separator is "; ". This is consistent with the existing evidence format. Good.

### 22. `internal/registry/config.go` - `applyDefaults` - `MinSeverity` normalization

```go
if r.Executor.MinSeverity == "" {
    r.Executor.MinSeverity = DefaultFixMinSeverity
} else {
    r.Executor.MinSeverity = stream.NormalizeSeverity(r.Executor.MinSeverity)
}
```

If `MinSeverity` is set, it's normalized. The validation in `validateExecutor` checks:
```go
if normalized := stream.NormalizeSeverity(e.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
```

So if `MinSeverity` is "high", it normalizes to "HIGH" and passes validation. Then in `applyDefaults`, it's normalized again to "HIGH". This is correct.

But wait, what if `MinSeverity` is set to an invalid value like "BLOCKER"? The validation catches it and returns an error. The `applyDefaults` is not called. So this is correct.

### 23. `internal/verify/executor.go` - `meetsSeverityFloor` with normalized minSev

The `minSev` is normalized in `applyDefaults`. So when `generateFixes` is called, `minSev` is already normalized (e.g., "MEDIUM" not "medium"). The `f.Severity` is not normalized. If `f.Severity` is "medium" (lowercase), the comparison might fail.

This depends on how `meetsSeverityFloor` is implemented. If it normalizes both sides, this is fine. If it doesn't, this is a bug.

Given that the test `TestExecutor_MinSeverityForFixExplicitAndNormalized` checks that "high" is normalized to "HIGH", and the test `TestGenerateFixes_SkipsBelowSeverity` uses "LOW" severity, the function likely normalizes both sides. But this is an assumption.

### 24. `internal/verify/executor.go` - `meetsSeverityFloor` function definition

I need to check if `meetsSeverityFloor` is defined in the codebase. Since the diff doesn't show it, and the tests pass (presumably), it must be defined somewhere.

Actually, looking at the pipeline.go diff, the function is called in the new block:
```go
reconcile.ConfidenceAtOrAbove(f.Confidence, reconcile.ConfHigh) && meetsSeverityFloor(f.Severity, fixMinSev)
```

This is in the `runVerify` function, which is in the `verify` package. The `meetsSeverityFloor` function is also in the `verify` package (called from `executor.go` which is in the same package).

Since the diff doesn't show the definition, it must be either:
1. An existing function in the `verify` package
2. A new function in the `verify` package, defined in a file not shown in the diff

If it's option 2, the diff is incomplete. If it's option 1, the function exists and is being reused.

Given the context (Epic 7.0 introduces the fix generation phase, and `meetsSeverityFloor` is used for the fix severity gate), it's likely a new function. But it's not in the diff.

Wait, maybe `meetsSeverityFloor` is defined in the same `executor.go` file but not shown in the diff? Let me re-read the diff for `executor.go`:

The diff shows the full new file `executor.go` with 180 lines. The function `meetsSeverityFloor` is not in this file. So it must be in another file in the `verify` package.

This is a concern - the function definition is missing from the diff. If it's a new function, it should be in the diff. If it's an existing function, it should exist in the codebase.

Let me assume it's an existing function and move on.

### 25. `internal/verify/executor.go` - `meetsSeverityFloor` import

If `meetsSeverityFloor` is in the same package, no import is needed. If it's in a different package, an import is needed. The diff doesn't show an import for `meetsSeverityFloor`, so it's likely in the same package.

### 26. `internal/verify/pipeline.go` - Fix phase snapshot building - `meetsSeverityFloor` call

Same concern as above.

### 27. `internal/verify/executor.go` - `reg` parameter could be nil

As I mentioned, the function doesn't guard against `reg == nil`. In production, `reg` is always non-nil (it's a parameter to `runVerify`). But for robustness, the function should guard against nil `reg`.

This is a minor concern.

### 28. `internal/verify/executor.go` - `reg.Providers` could be nil

If `reg.Providers` is nil, `reg.Providers[ex.Provider]` returns the zero value (`registry.Provider{}`) and `ok` is false. The function then logs a warning and returns. This is correct.

But wait, if `reg.Providers` is nil, the map access returns the zero value. The `ok` is false. The function handles this correctly.

### 29. `internal/verify/pipeline.go` - Fix phase runs even if no findings need fixing

```go
if reg.Executor != nil {
    generateFixes(ctx, findings, reg.Executor, reg, newExecutorClient(), disp)
}
```

This runs even if no findings meet the criteria. The `generateFixes` function iterates over all findings and skips those that don't meet the criteria. This is correct.

But wait, `newExecutorClient()` is called even if no findings need fixing. This creates a new client unnecessarily. This is a minor performance concern.

### 30. `internal/verify/executor.go` - `meetsSeverityFloor` with empty severity

What if `f.Severity` is empty? The function might return false. This is probably correct - an empty severity is below any floor.

But this could be a concern if a finding has an empty severity. The finding would never get a fix. This is probably the intended behavior.

### 31. `internal/verify/executor.go` - `meetsSeverityFloor` with empty minSev

The `minSev` is set in `generateFixes`:
```go
minSev := ex.MinSeverity
if minSev == "" {
    minSev = registry.DefaultFixMinSeverity
}
```

So `minSev` is never empty. Good.

### 32. `internal/registry/config.go` - `validateExecutor` - `MaxTimeoutSecs` reference

```go
if e.TimeoutSecs != nil && (*e.TimeoutSecs <= 0 || *e.TimeoutSecs > MaxTimeoutSecs) {
    errs = append(errs, fmt.Errorf("executor: fix_timeout must be within 1..%d", MaxTimeoutSecs))
}
```

`MaxTimeoutSecs` is referenced but not defined in the diff. It's presumably an existing constant. This is fine.

### 33. `internal/verify/executor.go` - `meetsSeverityFloor` normalization

I keep coming back to this. The `meetsSeverityFloor` function is called with `f.Severity` and `minSev`. The `minSev` is normalized. The `f.Severity` is not normalized. If `meetsSeverityFloor` doesn't normalize both sides, the comparison might fail.

This is a potential bug. The function should normalize both sides for a correct comparison.

But since I can't see the function definition, I can't confirm this. Let me flag it as a potential concern.

### 34. `internal/verify/pipeline.go` - Fix phase snapshot building - `meetsSeverityFloor` call

Same concern as above.

### 35. `internal/verify/executor.go` - `meetsSeverityFloor` function name

The function is called `meetsSeverityFloor`. This is a clear, descriptive name. Good.

### 36. `internal/verify/executor.go` - `generateFixes` doesn't handle the case where `reg` is nil

As I mentioned, the function doesn't guard against `reg == nil`. This is a minor concern for robustness.

### 37. `internal/verify/executor.go` - `generateFixes` doesn't handle the case where `findings` is nil

The function iterates over `findings`. If `findings` is nil, the loop doesn't execute. This is correct.

### 38. `internal/verify/executor.go` - `generateFixes` doesn't handle the case where `disp` is nil

The function passes `disp` to `readFixSnippet`. The `readFixSnippet` function checks for nil `disp`:
```go
if disp == nil || strings.TrimSpace(file) == "" {
    return ""
}
```

So this is handled correctly.

### 39. `internal/verify/executor.go` - `readFixSnippet` doesn't validate `line > 0`

The function calculates `start` and `end` based on `line`. If `line` is 0 or negative, the calculation could produce invalid ranges. But the function returns the content regardless of what the dispatcher does with it. The dispatcher would handle the invalid range (e.g., return an error, which is caught and returns "").

This is a minor concern.

### 40. `internal/verify/executor.go` - `readFixSnippet` doesn't handle the case where `end < start`

If `line` is 0, `start` is set to 1, `end` is 30. If `line` is negative, `start` is set to 1, `end` could be negative or small. If `end < start`, the dispatcher might return an error or an empty result. The function returns the result regardless.

This is a minor concern.

### 41. `internal/verify/executor.go` - `buildFixPrompt` doesn't handle the case where `persona` is empty

```go
if strings.TrimSpace(persona) == "" {
    persona = registry.DefaultExecutorPersona
}
```

This is handled correctly.

### 42. `internal/verify/executor.go` - `buildFixPrompt` doesn't handle the case where `f.Problem` is empty

The function includes `f.Problem` in the prompt. If `f.Problem` is empty, the prompt has an empty problem field. This is probably fine - the executor would work with the other fields.

### 43. `internal/verify/executor.go` - `buildFixPrompt` doesn't handle the case where `f.File` is empty

The function includes `f.File` in the prompt. If `f.File` is empty, the prompt has an empty file field. This is probably fine.

### 44. `internal/verify/executor.go` - `buildFixPrompt` doesn't handle the case where `f.Line` is 0

The function includes `f.Line` in the prompt. If `f.Line` is 0, the prompt has "0" as the line number. This is probably fine.

### 45. `internal/verify/executor.go` - `callExecutor` doesn't handle the case where `ex.TimeoutSecs` is 0

```go
if ex.TimeoutSecs != nil && *ex.TimeoutSecs > 0 {
```

If `ex.TimeoutSecs` is 0, the timeout is not applied. This is correct.

### 46. `internal/verify/executor.go` - `callExecutor` doesn't handle the case where `ex.TimeoutSecs` is negative

The validation in `validateExecutor` catches negative timeouts:
```go
if e.TimeoutSecs != nil && (*e.TimeoutSecs <= 0 || *e.TimeoutSecs > MaxTimeoutSecs) {
```

So `ex.TimeoutSecs` is always positive if set. The check `*ex.TimeoutSecs > 0` is redundant but harmless.

### 47. `internal/registry/config.go` - `applyDefaults` - `BatchFixes` default

```go
if r.Executor != nil {
    ...
}
```

The `BatchFixes` field is not given a default in `applyDefaults
</think>
