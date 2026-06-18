The user wants me to review a diff and output findings in a specific pipe-delimited format. I need to:

1. Focus only on files/areas directly related to the sprint plan (Epic 4.1.1: Resume Support)
2. Output ONLY findings in TD_STREAM format, no prose
3. If no issues, output nothing
4. SEVERITY: HIGH, MEDIUM, or LOW
5. FILE:LINE must be precise
6. Be actionable and prioritized

Let me analyze the diff carefully. The files changed are:
- README.md (documentation, out of scope for functional issues unless there's a critical doc bug, but the sprint plan focuses on code implementation)
- cmd/atcr/resume.go
- cmd/atcr/resume_test.go
- cmd/atcr/review.go
- cmd/atcr/review_test.go
- internal/fanout/resume.go
- internal/fanout/resume_test.go
- internal/fanout/review.go

The sprint plan scope includes:
- Adding --resume flag to atcr review
- Review state detection
- Roster filtering
- Incremental writing
- Validation checks (git range, roster)
- Integration tests
- Implementation in cmd/atcr/review.go, internal/fanout/engine.go (actually they created internal/fanout/resume.go and modified internal/fanout/review.go)

Let me look for issues:

1. In `cmd/atcr/resume.go`:
   - `runResume` gets flags with `cmd.Flags().GetString("base")` etc., but ignores errors from GetString. This is common in Go but could be an issue if the flag doesn't exist... though it should exist since addRangeFlags was called.
   - `preFlightAPIKeys(prep.Slots)` - what if prep.Slots is empty? Actually it handles AllComplete before that.
   - The `ctx` passed to `fanout.PrepareResume` - if the root context is canceled, `PrepareResume` doesn't do long-running work so it returns before checking ctx usually, but it does call `buildPayloads` and `buildSlots` which might be long-running? Actually the comment says "The interrupt path is not reachable here: PrepareResume performs no long-running work." But `buildPayloads` could potentially take time? However, the sprint plan says PrepareResume performs no long-running work.
   - `llmclient.New()` is passed to `fanout.ExecuteResume` - but `ExecuteResume` takes a `Completer`, and `llmclient.New()` returns something that implements `Completer`. This seems fine.
   - `resumeReconcile` calls `reconcile.RunReconcile(ctx, dir, nil, ...)` - what is the third argument? It passes `nil`. Is that safe? Looking at the signature from reconcile package... not visible in diff. Assume it's okay.
   - In `runResume`, after calling `fanout.ExecuteResume`, it checks `errors.Is(ctx.Err(), context.Canceled)` BEFORE checking `err` from ExecuteResume. But `ExecuteResume` might return an error AND have a canceled context. The comment says "Checked before err so an interrupted resume is never reported as a clean completion." This seems correct behavior.

2. In `internal/fanout/resume.go`:
   - `ValidateResumeRange` compares `m.Base != cur.Base || m.Head != cur.Head`. What if the manifest stores short SHAs and current is long, or vice versa? The sprint plan says "manifest.json stores them verbatim" and gitrange.Resolve produces "already-resolved SHAs". But if one is a full SHA and the other is a short SHA, this would fail. However, the sprint plan says these are compared as "already-resolved SHAs", implying they should both be full SHAs.
   - `CompletedAgents` reads `rawDir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir)`. It uses `poolRawAgentDir` which is presumably a constant. What is its value? Not shown in diff, but based on usage in `writeResumedAgents` and tests, it seems to be `"raw/agent"` or similar.
   - `agentStatusName` reads status.json and checks `st.Status != StatusOK || st.Agent == ""`. This means if Agent field is empty, it's treated as pending. That's safe (fail-closed).
   - `ReadManifest` - no issues.
   - `ClearInterrupted` - reads manifest, modifies copy, writes back. Not atomic. If process crashes between read and write, could corrupt. But that's probably acceptable.
   - `filterPendingSlots` - iterates over `slots` and filters. What if `slots` is nil? Returns empty slice. Fine.
   - `PrepareResume`:
     - It builds payloads using `buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)`. This rebuilds payloads from the current repo. The sprint plan says "Payloads are rebuilt from the (validated-identical) recorded range so pending agents see exactly what the completed agents reviewed." But wait - if the current working tree files haven't changed but the git range is validated, then building payloads from the current repo is fine because the range is the same. However, the sprint plan says "Validate the range by re-running gitrange.Resolve on the current tree and comparing resolved Base/Head SHAs to the manifest; rebuild payloads from the stored (validated-identical) base/head so pending agents see the original context." The diff shows it rebuilding from req.Range which comes from current tree. Since the SHAs match, the tree content should be the same. But there's a subtle issue: what if there are uncommitted changes? The sprint plan mentions comparing base/head SHAs, but doesn't mention working tree cleanliness. However, since the SHAs are the same, git diff between base and head should be identical. So building from current repo is probably okay, though the sprint plan technically says "rebuild payloads from the stored (validated-identical) base/head". But in practice, since base/head are the same SHAs, git diff is deterministic. No real issue here.
     - Wait, the sprint plan says "rebuild payloads from the stored (validated-identical) base/head so pending agents see the original context." The code uses `req.Range.Base` and `req.Range.Head` which are from `gitrange.Resolve` on current tree. Since they match manifest, it's the same. So not an issue.
   - `ExecuteResume`:
     - `results, _ := runEngine(ctx, completer, p, poolDir)` - ignores the second return value (the review stage). Is that intentional? The fresh `ExecuteReview` uses the stage. But `ExecuteResume` doesn't seem to update `m.Review` with the stage from resumed agents. Wait, looking at `ExecuteResume`:
       ```go
       m := *p.manifest
       m.Partial = sum.Partial
       m.CompletedAt = time.Now().UTC()
       m.Interrupted = interrupted
       // Finalize the manifest... m.Review is preserved from the original run...
       ```
       It preserves `m.Review` from the original run. But if the resumed run has tool agents that ran, should it update the review stage? The comment says "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent (reviewStageFor records ToolsRequested even on a failed agent), and the resumed subset cannot add new members."
       But `runEngine` returns a stage that might contain snapshot provenance for this run. If the original run was interrupted before any tool agents ran, and the resumed run runs tool agents, the stage from `runEngine` would contain snapshot info. However, `ExecuteResume` ignores it (`results, _ := runEngine(...)`). This means snapshot provenance from the resumed run is lost! The comment in `ExecuteReview` says snapshot provenance is stamped onto the review stage. But in `ExecuteResume`, it's ignored.
       Is this an issue? The sprint plan doesn't explicitly mention updating the review stage on resume, but it does say "The manifest is finalized with the union's partial flag and the interrupt marker". It also says "m.Review is preserved from the original run". If the original run was interrupted before tool agents could create a snapshot, and the resumed run DOES create a snapshot, then the manifest would not record the snapshot provenance. This seems like a correctness/maintainability issue. However, is it in scope? The sprint plan says "Ensure the status.json and manifest.json are cleanly merged and updated." It doesn't explicitly mention the review stage. But it could be considered part of manifest updates.
       Actually, looking more carefully: in `ExecuteReview`, `runEngine` returns `results, stage`. Then it sets `m.Review = stage` and stamps snapshot info. In `ExecuteResume`, it ignores the stage. This means if the original run was interrupted very early (before tool snapshot), the resumed run would not record the snapshot info. But maybe that's okay because the original manifest might not have had a Review stage, and the resumed run creates one... but it's ignored.
       This might be a MEDIUM correctness issue: "ExecuteResume discards review stage snapshot provenance from resumed run". But looking at the comment in ExecuteResume: "m.Review is preserved from the original run: the roster is locked, so the original review stage already lists every tool agent (reviewStageFor records ToolsRequested even on a failed agent), and the resumed subset cannot add new members." This implies they intentionally keep the old review stage because it already contains the full roster. But the old review stage might NOT contain snapshot provenance if it was interrupted before the snapshot completed. And the new stage DOES contain it. So ignoring the new stage loses data.
       However, `reviewStageFor` might record tools requested based on the slots, not the results. Looking at `ExecuteReview`, the stage is created from `reviewStageFor(results)` which likely looks at what agents actually ran and whether they used tools. In a resume, the pending slots are only a subset. But the comment says the original review stage already lists every tool agent. If `reviewStageFor` only records agents that ran in THIS call to `runEngine`, then the new stage would only have the resumed subset, not the full roster. Preserving the old stage makes sense for the roster list, but you might want to merge snapshot provenance.
       Actually, the sprint plan doesn't mention this, and the comment explains the rationale. I'll skip this as it's arguably by design.

   - `writeResumedAgents` - iterates over results. Calls `agentDirName(r.Agent)` which presumably sanitizes. Then `writeAgentArtifacts`. Fine.
   - `RebuildPool` - reads from disk. If `os.ReadDir` fails (e.g., directory not exist), returns error. But what if `rawDir` doesn't exist? It would error. However, `ExecuteResume` calls `writeResumedAgents` first, which creates directories. But if there are no pending agents (AllComplete), `ExecuteResume` is not called. In `runResume`, if `info.AllComplete()`, it goes straight to `resumeReconcile`. So `RebuildPool` is only called when there are pending agents.
   - `RebuildPool` iterates entries. For each dir, reads status.json. If missing or unparseable, skips. Then reads findings.txt. If missing or unparseable, ignores (contributes no findings). This means a completed agent with missing findings.txt is counted in statuses but contributes no findings. That's fine.
   - However, `RebuildPool` calls `writeFindings` and `writeJSON`. These are presumably existing functions.
   - `summarizeStatuses` - sets `Partial = s.Failed > 0 && s.Succeeded > 0`. This is correct.
   - `formatStatusFailures` - handles empty name with `<unnamed>`. Good.

3. In `cmd/atcr/resume_test.go`:
   - `liveReviewConfig` writes to user's home dir (`~/.config/atcr`). This is NOT isolated! The test writes to the real user's home directory. This is a HIGH issue. Tests should not write to the real user's home directory. They should use `t.TempDir()` and set `HOME` environment variable or use a mock.
   - `scaffoldResumeReview` - fine.
   - `writeResumeReviewFixture` - writes to `.atcr/reviews/...`. Fine since `isolate(t)` presumably handles cwd.
   - `execResume` - creates `newRootCmd()`. Fine.
   - `TestResolveResumeDir_LatestAndEmpty` - uses `scaffoldResumeReview` and `fanout.WriteLatest`. Fine.
   - `TestResume_AllCompleteReconcilesAndExitsZero` - calls `liveReviewConfig` which writes to real home dir. This is bad.
   - `TestResume_RunsPendingAgentThenReconciles` - same.
   - `TestResume_AllCompleteClearsStaleInterrupted` - same.
   - Actually, looking at other tests in the project, maybe `isolate(t)` sets up a temp home? The function `isolate` is not shown in the diff, but other tests in `review_test.go` likely use it. However, `liveReviewConfig` explicitly calls `os.UserHomeDir()` which respects the actual home unless `HOME` is set. If `isolate(t)` doesn't set `HOME`, this is a problem. Even if it does, writing to a shared location between tests could cause flakiness. But since I can't see `isolate`, I need to be careful.
   - Wait, `liveReviewConfig` is defined in this file. It uses `os.UserHomeDir()`. If `isolate(t)` doesn't override the home directory, this test pollutes the developer's machine. However, looking at the existing codebase pattern, there might be an `isolate` function that sets `HOME` to a temp dir. But I can't verify that from the diff. The fact that `liveReviewConfig` hardcodes `filepath.Join(home, ".config", "atcr")` is suspicious. But without seeing `isolate`, I can't be 100% sure. However, looking at `cmd/atcr/resume_test.go`, it imports `os` and uses `os.UserHomeDir()`. If the test suite is well-written, `isolate` probably sets HOME. But maybe not. This could be a real issue.
   - Actually, looking at the diff, there's no `isolate` function shown. But it's called everywhere. If I don't know what it does, should I flag it? The instructions say "No false positives: Only report real issues". If `isolate` sets HOME, then it's not an issue. But the fact that `liveReviewConfig` doesn't take a home dir parameter and instead discovers it dynamically is a test design issue. But maybe acceptable.
   - I'll skip this since I can't confirm it's a real issue without seeing `isolate`.

4. In `internal/fanout/resume_test.go`:
   - `fourAgentConfig` uses `ptrF(0.7)`. Not shown but presumably exists.
   - `writeAgentStatusFixture` - fine.
   - `TestResume_InterruptThenResumeCompletesAllAgents`:
     - `fake := &cancelAfterCompleter{cancelAt: 2, cancel: cancel}` - where is `cancelAfterCompleter` defined? Not in the diff. It might be defined in `review_test.go` or elsewhere. If it's not defined in this file or a shared test helper, this test won't compile. But it's probably defined elsewhere in the package.
     - `llmclient.New()` in `ExecuteResume` - requires `ATCR_TEST_KEY` env var to be set? Actually `llmclient.New()` might not need it if the provider is mocked. But `fourAgentConfig` sets `APIKeyEnv: "ATCR_TEST_KEY"` and `t.Setenv("ATCR_TEST_KEY", "secret")` is set. So `llmclient.New()` when calling the mock provider... wait, in Phase 2 it uses `llmclient.New()` with the real mock provider server. `llmclient.New()` probably creates an HTTP client. If it tries to read the API key from env, it will find it. Good.
     - `reviewReq(repo, repo, base, head)` - not defined in diff. Probably defined elsewhere.
   
5. In `cmd/atcr/review.go`:
   - `interruptMessage` now references `--resume`. The old comment said "deliberately references only `atcr status <id>` — not a `--resume` flag". The new implementation adds `--resume`. Fine.

6. In `internal/fanout/review.go`:
   - The refactoring extracts `runEngine`. This looks correct. It preserves behavior.
   - `runEngine` returns `results, stage`. `stage` is built from `reviewStageFor(results)` with snapshot info stamped.
   - In `ExecuteReview`, `results, stage := runEngine(...)`. Then later `m.Review = stage`. This matches the old behavior.
   - One potential issue: `runEngine` sets up `tools.NewSnapshotManager` and cleanup. But in the old code, `cleanup` was deferred inside `ExecuteReview`. Now it's inside `runEngine`. Since `runEngine` is called from both `ExecuteReview` and potentially... wait, `runEngine` is only called from `ExecuteReview` in this diff. `ExecuteResume` does NOT call `runEngine`? Wait, looking at `ExecuteResume`:
     ```go
     results, _ := runEngine(ctx, completer, p, poolDir)
     ```
     Actually yes, `ExecuteResume` DOES call `runEngine`. So the snapshot cleanup is deferred inside `runEngine`, which means it will run when `runEngine` returns. That should be fine.
   - Wait, but `runEngine` has `defer cancel()` for the timeout context, and `defer cleanup()` for the snapshot. These defers will run when `runEngine` returns. That's correct.

Let me look for more subtle issues.

In `cmd/atcr/resume.go`:
- `resolveResumeDir` calls `resolveReviewDir("")` for empty anchor. Then `resolveReviewDir` likely checks for `.atcr/latest`. The comment says empty arg → .atcr/latest.
- `runResume` calls `gitrange.Resolve(ctx, ".", ...)` with the current flags. If the user passes `--resume latest` without `--base`/`--head`, what happens? The sprint plan says "Pass the same range flags (`--base`/`--head`/`--merge-commit`) the original review used so the range matches." But it doesn't say they're required. If omitted, `gitrange.Resolve` probably uses defaults. If the defaults resolve to the same range as before, great. If not, `ValidateResumeRange` will catch it. So that's fine.

In `internal/fanout/resume.go`:
- `PrepareResume` calls `buildSlots(cfg, payloads, req.Range)`. `buildSlots` presumably builds slots for the entire configured roster. Then `filterPendingSlots(slots, done)` filters them. This is correct.
- But what if the manifest's `Roster` contains agents not in the configured roster? `ValidateResumeRoster` checks set equality, so this would fail. Good.
- What if the manifest's `Roster` has duplicates? `nameSet` would deduplicate them. The configured roster from `rosterNames(cfg.Project)` might also deduplicate? Unknown. If manifest has duplicates but configured doesn't, lengths would differ (since `len(recorded) != len(current)` is checked before set comparison). Actually `nameSet` deduplicates, so `len(recorded)` in the set might be less than `len(m.Roster)`. But the check `len(recorded) != len(current)` compares the SET sizes. If manifest has duplicates, `len(recorded)` (set size) < `len(m.Roster)` (slice length). If configured doesn't have duplicates, it could pass even though manifest had duplicates. This is a minor issue but probably doesn't matter.

In `RebuildPool`:
- It reads `rawDir` entries. For each directory, reads `statusFile` and `findingsFile`.
- If status.json exists but findings.txt doesn't, it appends status but no findings. That's fine.
- If status.json is corrupt, `continue` skips it entirely. So it won't be counted in Total. But `summarizeStatuses` is called on the statuses slice, so Total is based on how many statuses were successfully read. This means an agent with corrupt status.json is completely omitted from the summary. In `ExecuteResume`, after rebuilding, it checks `sum.Total == 0` (ErrEmptyRoster) and `sum.Succeeded == 0` (ErrAllAgentsFailed). If there are 4 agents and 1 has corrupt status.json, `sum.Total` would be 3. That's probably okay because corrupt status means the agent didn't complete properly.
- But wait: in `CompletedAgents`, a corrupt status.json means the agent is treated as pending (not skipped). So `ExecuteResume` would run that agent. After running, `writeResumedAgents` would write a proper status.json. Then `RebuildPool` would read it. So `RebuildPool` should see valid statuses for all resumed agents. For agents that were already completed, their status.json was valid (since `CompletedAgents` only counts OK if valid). So `RebuildPool` should never encounter corrupt status.json for completed agents. But what if the completed agent's status.json becomes corrupt between `CompletedAgents` and `RebuildPool`? Race condition with external process? Unlikely. So not a real issue.

In `cmd/atcr/resume.go`:
- The `llmclient.New()` call: looking at `ExecuteResume(ctx, llmclient.New(), prep)`. What if `llmclient.New()` returns nil or errors? The signature in Go probably returns a concrete type or pointer. If it returns an interface and could be nil, calling `ExecuteResume` with a nil completer could panic inside `runEngine`. But I don't know `llmclient.New()` signature. In `cmd/atcr/review.go`, `ExecuteReview` is called with `llmclient.New()` too, so it's consistent.

Wait, I found a potential issue in `cmd/atcr/resume.go`:
```go
base, _ := cmd.Flags().GetString("base")
head, _ := cmd.Flags().GetString("head")
mergeCommit, _ := cmd.Flags().GetString("merge-commit")
```
It ignores the error return from `GetString`. While these flags are defined by `addRangeFlags(cmd)`, if for some reason they aren't registered, the error would be silently ignored and empty strings used. This is a common pattern in the codebase (also in `runReview`?), so probably not an issue. Actually in `runReview` in the diff:
```go
base, _ := cmd.Flags().GetString("base")
head, _ := cmd.Flags().GetString("head")
mergeCommit, _ := cmd.Flags().GetString("merge-commit")
```
Same pattern. So it's consistent.

Let me look at `cmd/atcr/resume_test.go` again:
- `liveReviewConfig` does `reg += "  " + a + ":\n    provider: p\n    model: m-" + a + "\n    persona: " + a + "\n"`. This builds a YAML string. If agent names contain special characters, this could be malformed. But in tests, names are simple.
- `writeResumeReviewFixture` sets `m := &payload.Manifest{...}` and then `PerAgentPayload: map[string]string{}`. This is fine.

Potential correctness issue in `cmd/atcr/resume.go`:
- `resumeReconcile` reads `fanout.ReadManifestPartial(dir)`. If the manifest partial flag is true (from a previous interrupted run), it passes `Partial: true` to reconcile. But after a successful resume where all agents complete, shouldn't the partial flag be false? Actually `ExecuteResume` finalizes the manifest with `m.Partial = sum.Partial` where `sum.Partial` is true if there are both succeeded and failed agents. If all agents succeed, `sum.Partial` is false. But `resumeReconcile` is called in two places:
  1. From `runResume` after `ExecuteResume` succeeds: `result.Dir` is passed. At this point, `ExecuteResume` has already written the manifest with the correct partial flag. But `resumeReconcile` reads `ReadManifestPartial(dir)` which reads the manifest fresh. So it gets the updated value. Good.
  2. From `runResume` when `info.AllComplete()`: before calling `resumeReconcile`, it calls `fanout.ClearInterrupted(dir)`. But does it update the partial flag? If all agents were already complete, the partial flag should reflect whether any originally failed. But if all agents are complete and ok, partial should be false. `ClearInterrupted` only clears the interrupted flag, not partial. However, if the original run had some failed agents but then was interrupted, and on resume all agents are complete (some ok, some failed), then `sum.Partial` from a rebuild would be true. But since we skip `ExecuteResume` entirely when `AllComplete()`, we never rebuild the pool. The manifest still has its old partial flag. `resumeReconcile` passes that old flag. That seems correct.

Wait, there's an edge case: `info.AllComplete()` returns true when there are NO pending agents. But what if some agents failed in the original run and some succeeded? `CompletedAgents` only marks `StatusOK` as complete. Failed agents are NOT in `done`, so they are in `Pending`. Thus `info.AllComplete()` would be false if any agent failed. So `AllComplete()` means ALL agents are StatusOK. Therefore partial should be false. The manifest might have `Partial=true` from before if there was some weird state, but if all agents are OK, the review is not partial. However, the code doesn't clear the partial flag in this path. It only clears interrupted. So if somehow the manifest had `Partial=true` but all agents are now OK (maybe due to manual fixing?), it would remain true. But that's an edge case.

Actually, `AllComplete()` is defined as `len(r.Pending) == 0`. If `configured` has all agents in `done`, then `Pending` is empty. Since `done` only contains OK agents, all configured agents are OK. So partial should indeed be false. The code doesn't explicitly set it, but `resumeReconcile` uses `ReadManifestPartial(dir)` which might still read true if it was somehow left true. But `ExecuteResume` is not called in this path. So the manifest isn't updated. This is a minor issue but probably not realistic because if all agents are OK, the original manifest finalization (if it completed) would have set partial=false. If it was interrupted, partial might be true, but if all agents are actually OK, that's a stale marker. The code clears interrupted but not partial. I think this is a valid but minor issue. However, looking at `summarizeStatuses`, `Partial = s.Failed > 0 && s.Succeeded > 0`. If all succeeded, partial=false. So after a normal completion, partial is false. If the run was interrupted before finalization, partial might be whatever was last written. But if all agents are OK, the run was either completed (partial=false) or interrupted before finalization (partial might be true from an earlier state). In the latter case, `ClearInterrupted` is called but partial remains true. This seems like a bug: if all agents are complete and OK, the resume should clear the partial flag too, or at least rebuild the summary.

Wait, looking at `runResume` AC2 path:
```go
if info.AllComplete() {
    _, _ = fmt.Fprintln(cmd.OutOrStdout(), "All configured agents already completed. Re-running reconciliation...")
    if err := fanout.ClearInterrupted(dir); err != nil {
        return usageError(...)
    }
    return resumeReconcile(ctx, cmd, dir)
}
```
If every agent is complete, but the original run was interrupted before finalizing the manifest, the manifest might have `Partial=true` (from some earlier partial state? Actually `Partial` is computed from statuses at finalization time; if interrupted before finalization, the manifest might not have been updated at all, or it might have been updated by a previous partial write?). The sprint plan AC6 says "the review status is updated to 'completed'". It doesn't mention partial flag. But the success criteria says "Review state is preserved atomically". I think leaving partial=true when all agents are complete is inconsistent. But is it serious enough to flag?

Let me think about `ReadManifestPartial(dir)` - this function presumably reads the `Partial` field from manifest.json. If the manifest was from an interrupted run, `Partial` reflects the state at the time of interrupt. But if all agents are actually complete (all status.json are OK), then the run is NOT partial. So reconcile should not see `Partial=true`. The code should probably rebuild the pool summary or at least clear the partial flag.

However, looking at the test `TestResume_AllCompleteClearsStaleInterrupted`, it checks that status becomes `RunCompleted` but doesn't check the partial flag. The `ReadReviewStatus` probably derives status from `Interrupted` and other fields. If `Interrupted` is cleared, it might derive to Completed regardless of `Partial`.

I think there might be a correctness issue here, but it's subtle. Let me see if there's anything more obvious.

Looking at `cmd/atcr/resume.go`:
```go
func runResume(cmd *cobra.Command, anchor string) error {
    ctx := cmd.Context()
    // ...
    dir, err := resolveResumeDir(anchor)
    // ...
    res, err := gitrange.Resolve(ctx, ".", gitrange.Options{Base: base, Head: head, MergeCommit: mergeCommit})
    // ...
    cfg, err := fanout.LoadReviewConfig(".", cliOverrides(cmd))
    // ...
    req := fanout.ReviewRequest{...}
    prep, info, err := fanout.PrepareResume(ctx, cfg, dir, req)
    // ...
    ctx = correlateReviewID(ctx, prep.ID)
    ctx = log.NewContext(ctx, log.WithRedactor(...))
    // ...
}
```
Notice that `ctx` is overwritten with `correlateReviewID` and `log.NewContext` AFTER `PrepareResume`. This means if `PrepareResume` takes a long time (e.g., building payloads), any logs or context values from that phase won't have the review ID correlation or redaction. But `PrepareResume` is supposed to be fast. The comment says "The interrupt path is not reachable here: PrepareResume performs no long-running work." But `buildPayloads` could be non-trivial. However, the sprint plan says it's not long-running. This is probably fine.

Wait, I see something in `internal/fanout/resume.go` `ExecuteResume`:
```go
results, _ := runEngine(ctx, completer, p, poolDir)
interrupted := errors.Is(ctx.Err(), context.Canceled)
```
Then later:
```go
if err := writeResumedAgents(poolDir, results); err != nil {
    return nil, err
}
sum, statuses, err := RebuildPool(poolDir)
```
If `runEngine` returns results and the context was canceled, `results` contains whatever agents completed before cancellation. `writeResumedAgents` writes them. `RebuildPool` rebuilds from disk. Then:
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
This looks correct for AC7: if interrupted, `Interrupted=true` is written.

But what if `runEngine` returns an error along with canceled context? In Go, `runEngine` might return results and an error? Looking at `ExecuteReview` which calls `runEngine`:
```go
results, stage := runEngine(...)
// ...
```
`runEngine` signature is `([]Result, *payload.ReviewStage)`. It doesn't return an error! So `runEngine` itself doesn't fail? Actually looking at `runEngine` code, it calls `NewEngine(...).Run(...)`. If the engine returns errors per agent, they are stored in `Result.Err`. The `runEngine` function doesn't return an overall error. So `ExecuteResume` doesn't check for a global engine error. Good.

Now, looking at `cmd/atcr/resume.go` again:
```go
result, err := fanout.ExecuteResume(ctx, llmclient.New(), prep)

if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}

if result != nil {
    _, _ = fmt.Fprintf(...)
}
if err != nil {
    return err
}
```
Wait, `interruptMessage(result, prep)` uses `prep` (the PreparedReview from resume) if `result` is nil? Actually `interruptMessage` signature is `func interruptMessage(result *fanout.ReviewResult, prep *fanout.PreparedReview) string`. It handles nil result by falling back to `prep`. But `prep` here is the `PreparedReview` from `PrepareResume`. However, if the interrupt happens during `ExecuteResume`, `result` might be non-nil (because `ExecuteResume` populates `res := &ReviewResult{...}` before returning). Looking at `ExecuteResume`:
```go
res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
if sum.Total == 0 {
    return res, ErrEmptyRoster
}
if sum.Succeeded == 0 {
    return res, fmt.Errorf(...)
}
return res, nil
```
So `result` is always non-nil when `ExecuteResume` returns (unless there's a write error before that). Actually if `WriteManifest` fails, it returns `nil, err`. But for the interrupt path, `WriteManifest` happens after `runEngine`, so `res` has been computed. So `result` is likely non-nil. Good.

But wait, in `cmd/atcr/resume.go`, the interrupt check uses `prep` from `PrepareResume`. The `interruptMessage` function expects `prep *fanout.PreparedReview`. In `runResume`, `prep` is indeed available. But `interruptMessage` uses `prep.ID` and `prep.Dir`. However, the message also uses `done, total` from `result` if available. That's fine.

Actually, there's a subtle bug in `cmd/atcr/resume.go`:
```go
if errors.Is(ctx.Err(), context.Canceled) {
    _, _ = fmt.Fprint(cmd.ErrOrStderr(), interruptMessage(result, prep))
    return &codedError{code: exitFailure, err: errors.New("review interrupted")}
}
```
If `ctx` is canceled, `result` might be nil if `ExecuteResume` returned an error before assigning `res` (e.g., `writeResumedAgents` failed). Then `interruptMessage(nil, prep)` uses `prep`. But `prep` here is the `PreparedReview` from resume. The message will say `Run 'atcr status <id>' to inspect...`. This is fine.

But what if `err` from `ExecuteResume` is non-nil AND `ctx.Err()` is canceled? The code checks `ctx.Err()` first, so it returns interrupted instead of the underlying error. This is intentional per the comment.

Let me look for issues in the diff more carefully. One thing I notice:

In `internal/fanout/resume.go`:
```go
func ValidateResumeRange(m *payload.Manifest, cur ReviewRange) error {
	if m.Base != cur.Base || m.Head != cur.Head {
		return fmt.Errorf("%w: recorded %s..%s, current %s..%s; start a fresh `atcr review`",
			ErrRangeChanged, shortRef(m.Base), shortRef(m.Head), shortRef(cur.Base), shortRef(cur.Head))
	}
	return nil
}
```
This looks fine.

But wait, in `internal/fanout/review.go` (the refactoring):
The original `ExecuteReview` had this logic:
```go
// Wire the read-only tool harness...
var snapMode, snapHeadSHA, snapWorktreePath string
...
if anyToolAgent(p.Slots) && p.Head != "" {
    if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {
        log.Warnf(...)
    } else {
        defer cleanup()
        snapMode = "git_worktree"
        snapHeadSHA = p.Head
        snapWorktreePath = root.Path()
        opts = append(opts, WithJail(...), WithDispatcher(...))
    }
}
...
results := NewEngine(completer, opts...).Run(runCtx, p.Slots)

m := *p.manifest
...
m.Review = reviewStageFor(results)
if m.Review != nil {
    m.Review.SnapshotMode = snapMode
    m.Review.HeadSHA = snapHeadSHA
    m.Review.SnapshotWorktreePath = snapWorktreePath
}
```

After refactoring into `runEngine`:
```go
func runEngine(...) ([]Result, *payload.ReviewStage) {
    ...
    var snapMode, snapHeadSHA, snapWorktreePath string
    ...
    if anyToolAgent(p.Slots) && p.Head != "" {
        if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {
            log.Warnf(...)
        } else {
            defer cleanup()
            snapMode = "git_worktree"
            snapHeadSHA = p.Head
            snapWorktreePath = root.Path()
            opts = append(opts, WithJail(...), WithDispatcher(...))
        }
    }
    ...
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
And in `ExecuteReview`:
```go
func ExecuteReview(...) (*ReviewResult, error) {
    poolDir := filepath.Join(p.Dir, "sources", "pool")
    results, stage := runEngine(ctx, completer, p, poolDir)
    ...
    m.Review = stage
    ...
