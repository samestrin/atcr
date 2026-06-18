Let me carefully analyze this diff against the sprint plan scope. The sprint plan is for Epic 4.1.1: Resume Support, which adds a `--resume` flag to the `atcr review` command.

In-scope files:
- `cmd/atcr/review.go` — CLI subcommand definition
- `internal/fanout/engine.go` — fanning out queries to agents (note: the actual changes are in `internal/fanout/review.go` and `internal/fanout/resume.go`)
- `cmd/atcr/resume.go` (new)
- `cmd/atcr/resume_test.go` (new)
- `internal/fanout/resume.go` (new)
- `internal/fanout/resume_test.go` (new)
- `README.md` (documentation)
- `cmd/atcr/review_test.go` (test updates)

Let me review each file carefully.

### cmd/atcr/resume.go

Looking at the `runResume` function:

1. Line ~30-35: `resolveResumeDir` - delegates to `resolveReviewDir`. Seems fine.

2. The `--resume` flag handling:
   - Rejects `--id` and `--output-dir` combinations (exit 2) ✓
   - Rejects `--fail-on`, `--verify`, `--require-verified` (exit 2) ✓

3. Git range resolution:
```go
res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
if err != nil {
    if errors.Is(ctx.Err(), context.Canceled) {
        return interruptedBeforeFanout(cmd)
    }
    return usageError(fmt.Errorf("resume failed: %w", err))
}
```
This maps ALL gitrange errors to usage errors (exit 2). But a gitrange error could be a transient/network issue? Actually gitrange.Resolve is about resolving git refs, so it's local git operations. Mapping to exit 2 seems reasonable.

4. `PrepareResume` call - errors are mapped to usage errors.

5. The `AllComplete()` path:
```go
if info.AllComplete() {
    _, _ = fmt.Fprintln(cmd.OutOrStdout(), "All configured agents already completed. Re-running reconciliation...")
    if err := fanout.ClearInterrupted(dir); err != nil {
        return usageError(fmt.Errorf("resume failed: %w", err))
    }
    return resumeReconcile(ctx, cmd, dir)
}
```

6. `preflightAPIKeys` check - returns error directly (not wrapped in usageError). Let me check if this is consistent... Looking at the code, `preflightAPIKeys` returns an error. In the fresh review path, how is this handled? I can't see the fresh review path fully, but the comment says "no pending slot can authenticate → exit 2 before any provider call". If `preflightAPIKeys` returns a plain error without `usageError` wrapping, it might not map to exit 2. Let me check... Actually, I'd need to see how errors are mapped to exit codes. The `usageError` function likely wraps errors to produce exit 2. If `preflightAPIKeys` returns a plain error, it might produce exit 1 instead of exit 2. But I can't confirm this without seeing the error mapping logic. This could be a correctness issue but I'm not 100% sure.

Actually, looking more carefully:
```go
if err := preflightAPIKeys(prep.Slots); err != nil {
    return err // no pending slot can authenticate → exit 2 before any provider call
}
```
The comment says exit 2, but it returns `err` directly without `usageError`. If the error mapping treats non-`usageError` errors as exit 1, this would be exit 1 instead of exit 2. This seems like a potential bug, but I'd need to verify the error mapping. Let me flag it as a potential issue.

7. The interrupt handling:
```go
result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)

if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}
```
This checks `ctx.Err()` for cancellation. But `ExecuteResume` internally uses `runEngine` which creates a child context with timeout. If the parent context is cancelled, `ctx.Err()` would be `context.Canceled`. This looks correct.

But wait - there's a subtle issue. `ExecuteResume` calls `runEngine` which creates its own `runCtx` with a timeout cancel. The `runEngine` function checks `ctx.Err()` (the parent) for interrupt detection... actually no, `runEngine` just runs the engine. The interrupt detection is in `ExecuteResume`:

```go
func ExecuteResume(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
    poolDir := filepath.Join(p.Dir, "sources", "pool")
    results, _ := runEngine(ctx, completer, p, poolDir)
    interrupted := errors.Is(ctx.Err(), context.Canceled)
    ...
}
```

Wait, `runEngine` creates a child context:
```go
runCtx := ctx
if p.TimeoutSec > 0 {
    var cancel context.CancelFunc
    runCtx, cancel = context.WithTimeout(ctx, ...)
    defer cancel()
}
```

Then `runEngine` returns. Back in `ExecuteResume`, it checks `ctx.Err()` (the parent). If the parent was cancelled (SIGINT), `ctx.Err()` returns `context.Canceled`. If the timeout fired, `runCtx.Err()` returns `DeadlineExceeded` but `ctx.Err()` returns `nil`. This is correct - matching the pattern in `ExecuteReview`.

8. In `ExecuteResume`, the error from `runEngine` is discarded:
```go
results, _ := runEngine(ctx, completer, p, poolDir)
```
The error is discarded. Looking at `runEngine`, it doesn't actually return an error - it returns `([]Result, *payload.ReviewStage)`. So there's no error to discard. OK, that's fine.

Wait, let me re-read `runEngine`:
```go
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
```
Yes, no error return. The `_` is for the stage. Actually no, it returns `([]Result, *payload.ReviewStage)`, so `results, _ := runEngine(...)` discards the stage. But in `ExecuteResume`, the stage is not used (the comment says the review stage is preserved from the original run). This seems intentional.

### internal/fanout/resume.go

1. `ValidateResumeRange` - compares Base and Head SHAs. Straightforward.

2. `ValidateResumeRoster` - set equality check. Looks correct.

3. `CompletedAgents` - scans `sources/pool/raw/agent/` directories. Reads `status.json` for each. Returns map of OK agents. Looks correct.

4. `agentStatusName` - reads status.json, returns agent name if status is OK. Correct.

5. `ReadManifest` - reads manifest.json. Correct.

6. `ClearInterrupted` - reads manifest, clears Interrupted flag, writes back. This is NOT atomic - there's a TOCTOU race between ReadManifest and WriteManifest. But this is a CLI tool, so concurrent access is unlikely. Low severity.

7. `PrepareResume`:
   - Reads manifest
   - Validates range
   - Validates roster
   - Builds payloads
   - Builds slots
   - Gets completed agents
   - Partitions into completed/pending
   - Filters slots
   
   One issue: `buildPayloads` and `buildSlots` are called even when all agents are complete. This is wasted work when `AllComplete()` is true. But it's a minor performance issue since the caller checks `AllComplete()` before deciding whether to fan out. Actually, the payloads and slots are needed for the `PreparedReview` struct even in the all-complete case (though they won't be used). This is a minor inefficiency but not a bug.

   Actually wait - looking more carefully at `PrepareResume`, it builds payloads and slots BEFORE checking which agents are completed. If all agents are already complete, we've wasted time building payloads and slots. But the caller (`runResume`) checks `info.AllComplete()` and skips the fan-out. The `prep.Slots` would be empty in that case (since `filterPendingSlots` returns empty when all are done). So the wasted work is just `buildPayloads` and `buildSlots`. This is a minor performance concern.

8. `ExecuteResume`:
   - Runs the engine
   - Writes resumed agent artifacts
   - Rebuilds the pool
   - Finalizes manifest
   
   Looking at the error handling:
   ```go
   results, _ := runEngine(ctx, completer, p, poolDir)
   ```
   The stage is discarded. The comment in the code explains why: "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent."
   
   But wait - what if a pending agent uses tools and the original run didn't have tool agents? Actually, the roster is locked, so the same agents are configured. If an agent uses tools, it would have been recorded in the original review stage. But what if the agent failed in the original run before the review stage was recorded? The comment says "reviewStageFor records ToolsRequested even on a failed agent" - so the original manifest's Review stage should already list all tool agents. This seems correct.

   Actually, there's a subtle issue. If the original review was interrupted before ANY agent ran (all agents pending), the manifest might not have a Review stage at all. In that case, `m.Review` would be nil. When `ExecuteResume` runs all agents (some possibly using tools), it preserves `m.Review = nil` (since it copies from `*p.manifest`). The newly run tool agents' snapshot provenance would be lost. But this is an edge case and the comment suggests this was considered. Let me check...

   Actually, looking at the `ExecuteResume` code:
   ```go
   m := *p.manifest
   m.Partial = sum.Partial
   m.CompletedAt = time.Now().UTC()
   m.Interrupted = interrupted
   ```
   
   The `m.Review` is preserved from the original manifest. If the original run was interrupted before any agent ran, `m.Review` would be nil (or whatever was in the manifest). The resumed run doesn't update it. This means tool agent provenance from the resumed run is lost. But this is a known design decision per the comment. I'll flag it as LOW.

   Wait, actually there's a more significant issue. The `runEngine` function sets up the tool harness (snapshot, jail, dispatcher) and returns a `stage` with snapshot provenance. In `ExecuteResume`, this stage is discarded:
   ```go
   results, _ := runEngine(ctx, completer, p, poolDir)
   ```
   
   So if pending agents use tools, their snapshot provenance is computed but never recorded in the manifest. The comment says the original review stage already has this info, but that's only true if the original run actually ran those tool agents. If the original run was interrupted before a tool agent ran, the manifest's Review stage wouldn't have that agent's entry. This is a correctness issue - the manifest would be missing tool provenance for resumed tool agents.

   Actually, let me re-read the comment more carefully:
   > "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent (reviewStageFor records ToolsRequested even on a failed agent), and the resumed subset cannot add new members."

   So `reviewStageFor` records tool agents even if they failed. But what if the agent never ran at all (interrupted before it started)? In that case, there would be no Result for that agent, and `reviewStageFor` wouldn't include it. So if a tool agent was pending (never ran) in the original interrupted run, and then runs in the resume, its tool provenance would be lost.

   Hmm, but actually `reviewStageFor` works on `[]Result`. If the agent never ran, there's no Result, so it wouldn't be in the stage. The resumed run generates Results for pending agents, but those Results' stage info is discarded. This is a real issue.

   Let me check the severity. The sprint plan says "Modifying the agent panel configuration mid-resume" is out of scope, but tool provenance tracking is about data integrity. The manifest's Review stage would be incomplete. I'd say MEDIUM severity - the review still works, but tool provenance metadata is lost for resumed tool agents.

   Actually, let me reconsider. The original manifest was written by `ExecuteReview` which calls `reviewStageFor(results)` on ALL results (including failed/timed-out ones). But if the review was interrupted before a tool agent even started, that agent has no Result, so it's not in the review stage. When resumed, that agent runs with tools, but the stage is discarded. So the manifest's Review stage is missing that agent's tool info.

   But wait - is this actually in scope? The sprint plan doesn't mention tool provenance. It's about resume support. The tool harness is existing functionality. The resume code discards the stage from `runEngine`, which means tool provenance from resumed agents is lost. This seems like a real correctness issue in the new code.

   Let me flag this as MEDIUM.

9. `writeResumedAgents` - writes per-agent artifacts. Looks correct.

10. `RebuildPool` - reads all agent dirs, merges findings, writes summary. Looks correct.

11. `summarizeStatuses` - counts OK vs non-OK. Correct.

12. `formatStatusFailures` - formats failure reasons. Correct.

### internal/fanout/review.go (modified)

The `ExecuteReview` function is refactored to extract `runEngine`. The extraction looks correct - the same logic is used by both `ExecuteReview` and `ExecuteResume`.

One thing I notice: in the original `ExecuteReview`, the snapshot provenance variables (`snapMode`, `snapHeadSHA`, `snapWorktreePath`) were local to the function and stamped onto the review stage. Now they're in `runEngine` and stamped there. The `ExecuteReview` function receives the stage from `runEngine` and assigns it to `m.Review`. This looks correct.

But in `ExecuteResume`, the stage is discarded (`results, _ := runEngine(...)`). As I noted above, this loses tool provenance for resumed agents.

### cmd/atcr/review.go (modified)

The `--resume` flag is added and `runReview` branches to `runResume` when `--resume` is changed. The `interruptMessage` is updated to reference `--resume`. Looks correct.

### cmd/atcr/resume_test.go (new)

Tests look comprehensive. Let me check for any issues...

1. `liveReviewConfig` writes to the user's home directory (`~/.config/atcr/registry.yaml`). This modifies global state outside the test's temp directory. In a parallel test run, this could interfere with other tests. But looking at the `isolate(t)` call, it likely handles this. Also, the test uses `t.Setenv` which suggests environment isolation. This might be OK depending on what `isolate` does.

2. `TestResume_RunsPendingAgentThenReconciles` - uses `writeResumeReviewFixture` to pre-populate bruce as completed. Then runs resume. Checks that robin gets results. Looks correct.

3. `TestResume_AllCompleteClearsStaleInterrupted` - tests the AC6 scenario where a stale interrupt marker is cleared. Looks correct.

### cmd/atcr/review_test.go (modified)

The test assertion is updated from `assert.NotContains(t, msg, "--resume")` to `assert.Contains(t, msg, "atcr review --resume 2026-06-17_feat")`. This is correct per the sprint plan.

### README.md (modified)

Documentation for the `--resume` flag. Looks correct and matches the implementation.

### Summary of Issues Found

1. **MEDIUM - ExecuteResume discards tool provenance stage** (`internal/fanout/resume.go:~225`): `runEngine` returns a review stage with snapshot provenance, but `ExecuteResume` discards it (`results, _ := runEngine(...)`). If a pending tool agent runs during resume, its snapshot provenance is lost from the manifest. The comment claims the original review stage already lists every tool agent, but that's only true if the agent actually ran (even if failed) in the original run - a pending agent that never started has no entry.

2. **LOW/MEDIUM - preflightAPIKeys error not wrapped in usageError** (`cmd/atcr/resume.go:~115`): The comment says "exit 2" but returns `err` directly without `usageError` wrapping. If the error mapping treats unwrapped errors as exit 1, this would be exit 1 instead of the intended exit 2.

Actually, let me look at this more carefully. In the fresh review path, how is `preflightAPIKeys` handled? I don't have the full `review.go` to compare. But looking at the pattern in `runResume`, other validation errors use `usageError(err)` to map to exit 2. The `preflightAPIKeys` check returns `err` directly. If `preflightAPIKeys` returns a `usageError`-compatible error, this might be fine. But if it returns a plain error, it would map to exit 1. Without seeing the implementation of `preflightAPIKeys` and the error-to-exit-code mapping, I can't be 100% sure. But the inconsistency with the surrounding code (which wraps in `usageError`) is suspicious.

Let me also check if there are any other issues...

3. Looking at `ExecuteResume` more carefully:
```go
results, _ := runEngine(ctx, completer, p, poolDir)
interrupted := errors.Is(ctx.Err(), context.Canceled)
```

The `interrupted` check is on `ctx.Err()`. But `runEngine` creates a child context with timeout:
```go
runCtx := ctx
if p.TimeoutSec > 0 {
    var cancel context.CancelFunc
    runCtx, cancel = context.WithTimeout(ctx, ...)
    defer cancel()
}
```

If the timeout fires, `runCtx` is cancelled but `ctx` is not. So `ctx.Err()` returns nil, and `interrupted` is false. This is correct - a timeout is not an interrupt.

But what if the parent context is cancelled (SIGINT)? Then `ctx.Err()` returns `context.Canceled`, and `interrupted` is true. This is also correct.

4. Looking at the `RebuildPool` function:
```go
if fdata, ferr := os.ReadFile(filepath.Join(agentDir, findingsFile)); ferr == nil {
    if pr, perr := stream.ParseSource(fdata); perr == nil {
        merged = append(merged, pr.Findings...)
    }
}
```
If `ParseSource` fails, the error is silently ignored. This means corrupt findings are silently dropped. This could lead to a review that appears to have fewer findings than it should. But the comment says "a missing or unparseable findings.txt contributes no findings but does not fail the rebuild." This is a design decision, not a bug. Though it could mask data corruption.

5. In `CompletedAgents`:
```go
entries, err := os.ReadDir(rawDir)
if err != nil {
    if errors.Is(err, fs.ErrNotExist) {
        return map[string]bool{}, nil
    }
    return nil, err
}
```
This handles the case where the pool dir doesn't exist. But what about permission errors? A permission error would be returned as an error, which would cause the resume to fail. This seems correct - we don't want to silently skip agents if we can't read their status.

6. Looking at `PrepareResume`:
```go
payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
if err != nil {
    return nil, nil, err
}
slots, _, err := buildSlots(cfg, payloads, req.Range)
if err != nil {
    return nil, nil, err
}
```

These errors are returned directly, not wrapped in `usageError`. But `PrepareResume` is in the `fanout` package, which shouldn't know about CLI exit codes. The caller (`runResume`) wraps the error in `usageError`:
```go
prep, info, err := fanout.PrepareResume(ctx, cfg, dir, req)
if err != nil {
    return usageError(err)
}
```
So this is correct.

7. One more thing - in `ExecuteResume`, the `runEngine` results' error is not checked. But `runEngine` doesn't return an error, so there's nothing to check. The engine's `Run` method returns `[]Result`, and individual agent failures are captured in each `Result`'s `Status` field. This is consistent with `ExecuteReview`.

8. Looking at the `ClearInterrupted` function:
```go
func ClearInterrupted(reviewDir string) error {
    m, err := ReadManifest(reviewDir)
    if err != nil {
        return err
    }
    if !m.Interrupted {
        return nil
    }
    m.Interrupted = false
    return WriteManifest(reviewDir, m)
}
```
This is a read-modify-write without any locking. If another process is writing the manifest concurrently, this could lose data. But for a CLI tool, concurrent access is unlikely. Low severity.

9. In `resumeReconcile`:
```go
func resumeReconcile(ctx context.Context, cmd *cobra.Command, dir string) error {
    rec, err := reconcile.RunReconcile(ctx, dir, nil, reconcile.Options{
        ReconciledAt: time.Now(),
        Partial:      fanout.ReadManifestPartial(dir),
    })
    if err != nil {
        return usageError(fmt.Errorf("resume failed: %w", err))
    }
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)
    return nil
}
```

`ReadManifestPartial` returns a bool. If the manifest is corrupt or missing, what does it return? Looking at the function name, it probably returns false on error. But if the manifest was just written by `ExecuteResume`, it should be valid. This seems fine.

10. Let me reconsider the tool provenance issue. The sprint plan says:
> "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent (reviewStageFor records ToolsRequested even on a failed agent), and the resumed subset cannot add new members."

The key claim is that `reviewStageFor` records tool agents even on failed agents. Let me think about this...

If an agent was pending (never ran) in the original interrupted run, it has no Result. `reviewStageFor` works on `[]Result`, so a never-ran agent has no entry. When the resume runs this agent, it produces a Result, but the stage from `runEngine` is discarded. So the manifest's Review stage is missing this agent's entry.

But wait - the original `ExecuteReview` calls `reviewStageFor(results)` where `results` includes ALL agents that ran (even failed ones). If the review was interrupted, some agents may not have run at all. Those agents have no Result and no entry in the review stage. When resumed, those agents run and produce Results, but the stage is discarded. So the manifest's Review stage is incomplete.

However, the practical impact depends on whether any pending agents use tools. If all tool agents completed in the original run, the stage is complete. If a tool agent was pending, its provenance is lost.

I think this is a real MEDIUM severity issue. The fix would be to merge the stage from `runEngine` into the existing manifest's Review stage.

Actually, let me re-examine. The `reviewStageFor` function creates a new stage from results. In `ExecuteResume`, if we were to use the stage from `runEngine`, it would only contain entries for the pending agents that just ran, not the completed ones. So we'd need to merge them. The current code preserves the original stage (which has completed agents' entries) but loses the pending agents' entries. Using the new stage would lose the completed agents' entries. The correct fix would be to merge both stages.

This is a real issue but the fix is non-trivial. Let me flag it as MEDIUM.

Actually, I realize I should also check: does the sprint plan mention tool agents or tool provenance? Looking at the sprint plan... it doesn't mention tools at all. The tool harness is existing functionality from another epic. The resume code needs to handle it correctly, but the sprint plan doesn't call it out. I think this is still in scope because it's a correctness issue in the new resume code.

Let me also check if there's a simpler issue I'm missing...

11. In `runResume`, the `base`, `head`, and `mergeCommit` flags are read:
```go
base, _ := cmd.Flags().GetString("base")
head, _ := cmd.Flags().GetString("head")
mergeCommit, _ := cmd.Flags().GetString("merge-commit")
```
The errors from `GetString` are ignored. This is a common pattern in cobra (flags are registered and known to exist), so this is typically safe. But it's still not best practice. However, this matches the pattern used in the existing `runReview` function, so it's consistent. I won't flag this.

12. Looking at the test `TestResume_InterruptThenResumeCompletesAllAgents`:
```go
cfg := fourAgentConfig("http://unused")
prep, err := PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
```
This uses `http://unused` as the provider URL, but the fake completer drives this phase. Then for resume:
```go
cfg2 := fourAgentConfig(srv.URL)
rprep, info, err := PrepareResume(context.Background(), cfg2, prep.Dir, reviewReq(repo, repo, base, head))
```
This creates a NEW config with the real mock server URL. But `PrepareResume` validates the roster against the manifest. Since both configs have the same roster (`greta, kai, mira, otto`), this should pass. The range is also the same. This looks correct.

13. One more thing - in `ExecuteResume`, after `runEngine` returns:
```go
results, _ := runEngine(ctx, completer, p, poolDir)
interrupted := errors.Is(ctx.Err(), context.Canceled)

if err := writeResumedAgents(poolDir, results); err != nil {
    return nil, err
}
```

If `runEngine` returns partial results (some agents completed, some didn't due to interrupt), `writeResumedAgents` writes those partial results. Then `RebuildPool` rebuilds the union. This is correct - partial results are preserved.

But what if `writeResumedAgents` fails? The results from `runEngine` are lost. The manifest is not updated. The caller gets an error. This seems like correct error handling - if we can't write the results, we fail.

14. Let me check the `RebuildPool` function more carefully:
```go
entries, err := os.ReadDir(rawDir)
if err != nil {
    return Summary{}, nil, fmt.Errorf("reading agent artifacts: %w", err)
}
```
If the raw dir doesn't exist (no agents ran at all), this returns an error. But `CompletedAgents` handles the missing dir case gracefully. There's an inconsistency: `CompletedAgents` returns an empty set for a missing dir, but `RebuildPool` returns an error. This could be a problem if `ExecuteResume` is called with no pending agents... but actually, `runResume` checks `AllComplete()` before calling `ExecuteResume`, so this path shouldn't be reached. Still, it's an inconsistency.

Actually wait, `RebuildPool` is called from `ExecuteResume`, which is only called when there are pending agents. The pending agents run via `runEngine`, which writes results to the pool dir. So the pool dir should exist. But if `runEngine` fails to create the dir... let me check.

`runEngine` calls `NewEngine(completer, opts...).Run(runCtx, p.Slots)`. The engine's `Run` method writes per-agent artifacts. If no slots are pending (which shouldn't happen since we checked `AllComplete()`), the engine might not create the dir. But since `ExecuteResume` is only called when there are pending slots, this should be fine.

15. One more issue: in `ExecuteResume`, the `runEngine` function is called with `poolDir` which is `filepath.Join(p.Dir, "sources", "pool")`. But `runEngine` uses `poolDir` only for the transcript writer. The actual per-agent artifacts are written by `writeResumedAgents(poolDir, results)`. Let me check if `runEngine` writes anything to `poolDir`...

Looking at `runEngine`:
```go
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
    ...
    if anyToolAgent(p.Slots) && p.Head != "" {
        ...
        opts = append(opts, WithTranscriptWriter(transcript.NewWriter(filepath.Join(poolDir, poolRawAgentDir))))
        ...
    }
    ...
    results := NewEngine(completer, opts...).Run(runCtx, p.Slots)
    ...
}
```

So `runEngine` sets up a transcript writer under `poolDir`. The engine's `Run` method writes transcripts. But the per-agent artifacts (review.md, findings.txt, status.json) are written by `writeResumedAgents` in `ExecuteResume` (or by `writeAgentArtifacts` in `ExecuteReview`). This seems correct.

But wait - in `ExecuteReview`, the per-agent artifacts are written by `writeAgentArtifacts`:
```go
for _, r := range results {
    ...
    if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
        ...
    }
}
```

And in `ExecuteResume`, the same is done by `writeResumedAgents`:
```go
func writeResumedAgents(poolDir string, results []Result) error {
    for _, r := range results {
        dir, err := agentDirName(r.Agent)
        if err != nil {
            return err
        }
        fr := findingsFor(r)
        if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
            return err
        }
    }
    return nil
}
```

This looks correct - the same `writeAgentArtifacts` function is used.

OK, let me also look for any security issues...

16. In `resolveResumeDir`:
```go
func resolveResumeDir(anchor string) (string, error) {
    a := strings.TrimSpace(anchor)
    if a == "" || a == "latest" {
        return resolveReviewDir("")
    }
    return resolveReviewDir(a)
}
```
The anchor is user-provided. `resolveReviewDir` presumably validates it. If `resolveReviewDir` accepts arbitrary paths, a user could point to any directory. But this is a CLI tool where the user already has local access, so this isn't a security issue.

17. In `PrepareResume`, the `reviewDir` is used to read the manifest and scan for completed agents. If `reviewDir` is a symlink to a sensitive directory, it could be used to read arbitrary files. But again, this is a local CLI tool, so the user already has access. Not a security issue.

Let me finalize my findings:

1. **MEDIUM** - `internal/fanout/resume.go` line ~225 (ExecuteResume): Tool provenance from `runEngine` is discarded. If a pending tool agent runs during resume, its snapshot provenance is lost from the manifest.

2. **LOW/MEDIUM** - `cmd/atcr/resume.go` line ~115: `preflightAPIKeys` error returned without `usageError` wrapping, potentially mapping to exit 1 instead of intended exit 2.

Actually, let me reconsider #2. I need to check if `preflightAPIKeys` already returns a `usageError`. Looking at the code:
```go
if err := preflightAPIKeys(prep.Slots); err != nil {
    return err // no pending slot can authenticate → exit 2 before any provider call
}
```
The comment says "exit 2". If `preflightAPIKeys` returns a `usageError`-typed error, then returning it directly is fine. But if it returns a plain error, it would map to exit 1. Without seeing the implementation of `preflightAPIKeys`, I can't be sure. But the inconsistency with the surrounding code (which wraps in `usageError`) is worth flagging.

Actually, looking at the fresh review path in `review.go`, I can see that `preflightAPIKeys` is likely called there too. If the fresh review path also returns the error directly, then this is consistent and probably fine. Let me check... I don't have the full `review.go` but the diff shows `runReview` branching to `runResume` early. The fresh review path is below that. Without seeing it, I can't confirm. But if the pattern is the same in both paths, it's consistent.

Let me also check if there's a `preflightAPIKeys` function visible in the diff... No, it's not in the diff. It's an existing function. The comment says "exit 2", so presumably it returns a `usageError`-typed error. I'll lower the severity to LOW or skip it.

Actually, let me look at this differently. The comment explicitly says "exit 2". If the function returns a plain error, the comment is wrong. If it returns a `usageError`, the comment is correct. Since I can't verify, I'll flag it as LOW with a note to verify.

Hmm, actually I realize I should focus on clear issues. Let me re-evaluate...

The tool provenance issue is the clearest issue I've found. Let me also look for any other issues...

18. In `ExecuteResume`:
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

The manifest is copied, modified, and written. Then `p.manifest` is updated. This is correct - the local copy is only adopted on successful write.

But note that `m.Review` is preserved from `*p.manifest`. As I discussed, this loses tool provenance from resumed agents. The fix would be to merge the stage from `runEngine` into `m.Review`.

19. Let me also check the `RebuildPool` function for the `TotalFindings` field:
```go
ps := PoolSummary{
    Agents:        statuses,
    Total:         sum.Total,
    Succeeded:     sum.Succeeded,
    Failed:        sum.Failed,
    Partial:       sum.Partial,
    TotalFindings: len(merged),
}
```
This looks correct - `TotalFindings` is the count of merged findings from all agents.

20. One more thing - in `summarizeStatuses`:
```go
func summarizeStatuses(sts []AgentStatus) Summary {
    s := Summary{Total: len(sts)}
    for _, st := range sts {
        if st.Status == StatusOK {
            s.Succeeded++
        } else {
            s.Failed++
        }
    }
    s.Partial = s.Failed > 0 && s.Succeeded > 0
    return s
}
```
The `Partial` flag is true when there's at least one failure AND at least one success. This means an all-failed review has `Partial = false`. Is this correct? Looking at the `ExecuteResume` code:
```go
if sum.Succeeded == 0 {
    return res, fmt.Errorf("%w: %s", ErrAllAgentsFailed, formatStatusFailures(statuses))
}
```
So an all-failed review returns `ErrAllAgentsFailed`. The `Partial` flag being false in that case seems intentional - it's not "partial", it's "all failed".

But wait - in the context of a resume, "all failed" means all pending agents failed (the completed agents are still OK on disk). The `statuses` slice from `RebuildPool` includes ALL agents (completed + pending). So if all pending agents fail but some completed agents succeeded, `sum.Succeeded > 0` and `sum.Failed > 0`, making `Partial = true`. This is correct.

If ALL agents (completed + pending) failed... that would mean the completed agents' status.json records non-OK. But `CompletedAgents` only returns agents with `StatusOK`, so completed agents have `StatusOK`. In `RebuildPool`, all agents with a readable status.json are included. So completed agents would have `StatusOK` in the union. This means `sum.Succeeded > 0` whenever there's at least one completed agent. So the all-failed case only happens when there are no completed agents and all pending agents fail. This is correct.

OK, I think I've been thorough enough. Let me also check for any edge cases in the tests...

21. In `TestResume_RunsPendingAgentThenReconciles`:
```go
writeResumeReviewFixture(t, "2026-06-18