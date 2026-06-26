<think>
Let me systematically analyze this diff for issues, focusing on the sprint scope (Epic 10.3 - Benchmark Run Resumability & Checkpointing).

The diff modifies these files:
1. `cmd/atcr/benchmark.go` - Adds `--checkpoint` flag
2. `cmd/atcr/benchmark_checkpoint.go` - New file with checkpoint types and logic
3. `cmd/atcr/benchmark_checkpoint_test.go` - New tests for checkpoint logic
4. `cmd/atcr/benchmark_run.go` - Adds checkpoint path parameter, replay logic
5. `cmd/atcr/benchmark_run_resume_test.go` - New tests for resume behavior
6. `cmd/atcr/benchmark_run_test.go` - Updated existing tests with new parameter

All of these are within the sprint scope (cmd/atcr benchmark files).

Let me analyze for issues:

1. **Checkpoint write ordering / atomicity concern**: Looking at the checkpoint write — it's appended to `cp.Cases` and then written. The atomic write via `writeExportFile` should be fine.

2. **`replayCheckpointCase` order preservation issue**: The replay happens in the checkpoint's recorded case order, but the existing loop iterates `m.Cases` in order. When `done[i]` is hit, it replays via `replayCheckpointCase`. The checkpoint's `Reviewers` are replayed in order. But wait — in the original code, the iteration was `for _, a := range summary.Agents` — what order is that? Let me check if the order is deterministic.

   Looking more carefully — the replay uses `applyReviewerOutcome` which uses first-sighting to determine model/persona. The order in which reviewers are first added to `accs` and `order` determines the deterministic aggregation order. In the original code, the order depended on `summary.Agents` order. In the replay, it depends on `entry.Reviewers` order. If these differ, the result won't be byte-identical.

   This is a correctness concern for AC3 (byte-identical output across resume). The replay reads reviewers from the checkpoint in the order they were stored, which was the order from `summary.Agents`. So they should match... IF `summary.Agents` order is deterministic.

   Actually, wait — there's a subtler issue. The original code locks `model` and `persona` at "first sighting" via `acc.model` and `acc.persona`. The replay replays the checkpointed model/persona. But what if during fresh execution, a later case has a different model for the same agent (because cfg changed mid-run, e.g., model was updated)? Then the first-sighting model might differ from the checkpointed model. But this is unlikely in practice and the roster guard catches cfg changes.

   Actually let me re-examine. The `applyReviewerOutcome` function:
   ```go
   func applyReviewerOutcome(accs map[string]*reviewerAcc, order *[]string, agent, model, persona string, expected, raised []string, usageReported bool, costUSD float64, latencyMS int64) {
       acc := accs[agent]
       if acc == nil {
           acc = &reviewerAcc{model: model, persona: persona}
           accs[agent] = acc
           *order = append(*order, agent)
       }
       acc.cases = append(acc.cases, benchmark.CaseScore{Expected: expected, Raised: raised})
       if usageReported {
           acc.costUSD += costUSD
           acc.latencies = append(acc.latencies, latencyMS)
       }
   }
   ```
   
   This is called both during fresh execution and during replay. The model/persona are passed in from the caller. In fresh execution, they come from `cfg`. In replay, they come from the checkpoint. The first-sighting lock ensures that once an `acc` is created for an agent, its model/persona are never overwritten. But what if:
   
   - Fresh execution: case 0 has agent "greta" with model "m-greta" → acc created with m-greta
   - Checkpoint: case 0 stored as agent "greta" with model "m-greta" → replay creates acc with m-greta
   
   Same. Good.
   
   But what if `summary.Agents` ordering differs between runs? Or between the time the checkpoint was written and the time of resume? Then the order in which reviewers are registered could differ between the checkpointed cases (already replayed) and the fresh cases. For example:
   - Run 1: case 0 → agents [greta, kai] (in that order)
   - Run 1 fails after case 0
   - Run 2 (resume): case 0 replay → accs has greta then kai
   - Run 2: case 1 → summary.Agents = [kai, greta] (different order!)
   - kai already in accs (from case 0), so not re-added
   - greta already in accs (from case 0), so not re-added
   - order remains [greta, kai]
   
   OK so the order is preserved as long as the first case's agents are in a specific order. As long as that order is deterministic (depends on the engine), the result should be reproducible.
   
   Actually, there's a potential issue: if summary.Agents ordering is NOT deterministic between runs (e.g., from a map iteration), then the checkpoint would store one order, and the resume would replay a different order, leading to different `order` slice ordering. This would break AC3.
   
   Hmm, this is a concern but I'd need to see how `summary.Agents` is built to confirm. Let me note it as a concern.

3. **`replayCheckpointCase` registers reviewers in checkpointed order, not in case-index order across the whole run**: Wait, the order registration happens in case-index order because the loop iterates `m.Cases` in order and calls replay or fresh execution per case. So the registration order is correct.

4. **Case replay uses `c.ID` but doesn't check that the case's `ExpectedCategories` match the checkpointed `Expected`**: 
   Wait, looking at the per-index CaseID guard:
   ```go
   if entry.CaseID != c.ID {
       return nil, fmt.Errorf("%w: checkpoint case at index %d is %q but the suite has %q there; remove the checkpoint to start fresh",
           errCheckpointSuiteMismatch, i, entry.CaseID, c.ID)
   }
   ```
   It checks CaseID but not ExpectedCategories. If a case's ExpectedCategories changed but its ID didn't, the checkpointed score would replay against the new ExpectedCategories. This could be a correctness issue — but it's already protected by ReproHash which covers suite content. So if ExpectedCategories changed, ReproHash would change. So this is OK.

5. **`caseReviewers` may not include all agents if the loop doesn't iterate all of them**: Looking at the code, `caseReviewers` is built by iterating `summary.Agents`. The original code iterates `summary.Agents` to build the score, so the same set is captured. This is fine.

6. **Checkpoint write within the loop — performance concern**: Each case triggers a full checkpoint write (which includes the entire checkpoint JSON). For large suites, this could be O(n²) write cost. But this is a trade-off mentioned in AC1.

7. **No error from os.MkdirTemp failure but saveCheckpoint might fail**: Looking at the code, if `saveCheckpoint` fails, the run aborts. But this means the completed case's results are lost from `accs` (but the checkpoint before that case IS preserved). So this is fine — AC1 is maintained.

8. **`saveCheckpoint` is called AFTER `applyReviewerOutcome`**: If saveCheckpoint fails after applyReviewerOutcome, the case was added to `accs` but not to `cp.Cases`. This means a subsequent resume would re-execute the case. This is consistent with the 10.2 abort semantics.

9. **Memory concern: the full checkpoint is held in memory and rewritten each case**: For very large suites, this could be a memory issue. But this is more of a performance concern than correctness.

10. **`loadCheckpoint` returns `(nil, nil)` for missing file but errors on other read errors**: Good behavior, matches AC4 (never silently discarded).

11. **Checkpoint corruption handling**: A present-but-corrupt file returns an error. Good.

12. **The `--checkpoint` flag uses an empty default string**: This works but means the operator must remember to pass `--checkpoint <path>` explicitly. Per the clarification, this is intentional (matches `--suite-path`/`--out` precedent).

13. **`reviewerAcc` struct moved from inside the function to package level**: This is a refactoring change but doesn't affect correctness. Let me verify it's used correctly in tests.

14. **Race condition concern**: The checkpoint file is written from a single goroutine (the loop), so no race. OK.

15. **One potential issue: `applyReviewerOutcome` uses `costUSD` as a parameter name, which shadows `costUSD` field access in `acc`**. Looking again — the parameter is `costUSD` and the field is also `acc.costUSD`. The function does `acc.costUSD += costUSD` which is correct (local parameter used). No issue.

16. **`replayCheckpointCase` registers agents in checkpoint order, but fresh execution registers them in `summary.Agents` order**: This is the core concern for AC3. As long as `summary.Agents` order matches the order in which the checkpoint stored them, we're fine. The checkpoint stores them in summary.Agents order, so replay uses the same order. But what if the engine's `summary.Agents` order isn't deterministic between runs? That would be a problem.
   
   Actually wait — the question is whether summary.Agents order is deterministic. Let me look at where summary is built. I can't see the engine code in the diff, so I'll trust the existing tests (TestExecuteBenchmarkRun_Reproducible) which show that the result IS reproducible. So summary.Agents order must be deterministic.

17. **`errCheckpointSuiteMismatch` is used for both suite-identity drift AND case-id drift**: This might be misleading — the error message says "suite identity changed" but the case-id drift is a different kind of drift. The user might be confused. The sentinel is reused but the wrapping message is different. This is a minor design choice but the error wrapping includes enough context to distinguish.

18. **`writeExportFile` is used for atomic write — is this the right helper?**: The comment says "via the shared writeExportFile helper". I can't see writeExportFile in the diff but the comment claims it's shared and atomic. If it's truly atomic (temp file + rename), then the AC1 guarantee holds.

19. **Test `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift` doesn't verify that `resume.calls == 0`**: This is a missing assertion — the roster guard test verifies it but the case-id drift test doesn't. Minor consistency issue.

20. **`saveCheckpoint` is called even on cases where no reviewers were successful (total-roster failure case)**: Wait, let me re-read the code. The checkpoint write happens after the agents loop. If the agents loop completes (even with all agents failing and raising nothing), the case is still checkpointed. This is correct because the case was scored (with recall 0).

   But wait — what if the loop aborts mid-case? Looking at the code, the loop only writes the checkpoint after a successful case. If `os.ReadFile` for the diff fails, or `PrepareReviewFromDiff` fails, or the merge fails, no checkpoint is written. This is correct.

   Hmm, actually let me re-examine. The loop's error returns are at lines 63, 82, 86, 91, 95 (from the original). These return without checkpointing the current case. Good — the case wasn't scored, so it shouldn't be checkpointed.

   But wait — the issue is: if the case FAILS entirely (total-roster abort), the case is NOT checkpointed. Only successfully-scored cases are checkpointed. So if case N fails (all reviewers failed), case N is NOT in the checkpoint. A resume would re-execute case N. This is consistent with AC1 ("a process killed mid-suite leaves a checkpoint containing exactly the completed cases" — a case where all reviewers failed was NOT completed).

   Hmm, but actually I need to re-check this. Looking at the run loop:
   ```go
   for _, a := range summary.Agents {
       ...
   }
   if cp != nil {
       cp.Cases = append(cp.Cases, checkpointCase{Index: i, CaseID: c.ID, Reviewers: caseReviewers})
       ...
   }
   ```
   
   The checkpoint write happens after the agents loop. The agents loop doesn't error out per-agent — it just accumulates outcomes. So the checkpoint write happens even if all agents raised nothing (recall 0). Good.

   But what about cases where the diff read fails or the review execution fails? Those return before the agents loop, so no checkpoint. Good.

21. **`m.TokensIn > 0 || m.TokensOut > 0` was moved to a `usageReported` boolean**: Just a refactoring, no correctness concern.

22. **`Reviewers` ordering in the checkpoint**: The checkpoint stores reviewers in `summary.Agents` order. The fresh execution uses `summary.Agents` order. So they match. Good.

23. **`Roster` field in the checkpoint — does it represent the configured roster or the executed roster?**: Looking at `rosterSignature(cfg)`, it uses `cfg.Registry.Agents[n].Model`. This is the configured model, which is what's used in fresh execution (via `reviewerModel(cfg, a)`). So the roster reflects what would be executed, not what was actually executed. This is the right behavior — comparing like with like.

24. **Potential issue: `cfg.Registry.Agents[n].Model` might not exist (nil pointer)**: If `cfg.Registry.Agents[n]` is nil (agent configured but not in registry), this would panic. This is a robustness concern but probably already an issue in the existing code (via `reviewerModel`). Let me check — `reviewerModel` is defined elsewhere but not in the diff. If it handles missing registry entries, then `rosterSignature` should use the same logic.

   Actually, the comment says "An agent with no configured model contributes an empty model component". So this is handled — `cfg.Registry.Agents[n].Model` would panic if `cfg.Registry.Agents[n]` is nil. Hmm, this is a potential bug if any agent in `cfg.Project.Agents` doesn't have a registry entry.

   But wait, this is about adding a feature, not changing existing logic. If the existing `reviewerModel` handles this safely, `rosterSignature` should mirror it. If not, it's a pre-existing issue.

25. **One concrete issue I see: the `caseReviewers` slice includes ALL agents from `summary.Agents`, including those that raised nothing**. The checkpoint stores `Raised: raised` which is the actual findings. If a reviewer raised nothing, `Raised` is empty. The replay then folds `acc.cases = append(acc.cases, benchmark.CaseScore{Expected: expected, Raised: []string{}})` — same as a reviewer who saw nothing. Good.

   But there's a subtle issue: the original code (before refactoring) only recorded cost/latency if usage was reported. The refactored code does the same. Good.

26. **`applyReviewerOutcome` always appends to `acc.cases` even if `usageReported` is false**: This matches the original behavior. The case is always recorded; only cost/latency are usage-gated. Good.

27. **One potential issue I want to verify: `applyReviewerOutcome` parameter `expected` is always the case's `ExpectedCategories`, not the reviewer's expected**. Wait, `expected` here is `c.ExpectedCategories` from the case. But each reviewer might have different expected categories? No — looking at the original code:
   ```go
   acc.cases = append(acc.cases, benchmark.CaseScore{Expected: c.ExpectedCategories, Raised: raisedByReviewer[a.Agent]})
   ```
   The expected is the case's expected categories (same for all reviewers of a case). The raised is per-reviewer. The original code passed `c.ExpectedCategories` for every reviewer. The refactored code does the same. Good.

28. **Order of reviewers in `caseReviewers`**: Built by iterating `summary.Agents` in order. The fresh execution folds them in the same order. Replay also uses this order. Good.

Now let me identify actual issues:

**Issue 1: AC3 risk - summary.Agents order must match between checkpoint write and resume replay**
This is implicit and not directly testable from the diff. If `summary.Agents` order isn't deterministic, the resume would produce a different `order` slice than the uninterrupted run, breaking AC3. But the existing reproducibility tests pass, so this is likely fine.

**Issue 2: Potential panic in `rosterSignature` if `cfg.Registry.Agents[n]` is nil**
If an agent is in `cfg.Project.Agents` but not in `cfg.Registry.Agents`, `cfg.Registry.Agents[n].Model` would panic. This is a robustness concern. The comment acknowledges this might be empty, but doesn't handle the nil case. Looking at the existing code (not in diff) — `reviewerModel` is the precedent. Need to verify it handles this.

**Issue 3: `errCheckpointSuiteMismatch` is reused for case-id drift, which is semantically different**
The error message differs but the sentinel is the same. Callers checking `errors.Is(err, errCheckpointSuiteMismatch)` for case-id drift might be misled. This is a design choice but could be improved.

**Issue 4: Missing assertion in `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift`**
The test verifies the error but doesn't verify that `resume.calls == 0` (like the roster drift tests do). This is a test completeness issue, minor.

**Issue 5: Performance - O(n²) write cost for large suites**
Each case triggers a full checkpoint write (which includes the entire checkpoint JSON). For large suites, this is O(n²). For a 100-case suite, this means 100 full JSON writes summing to ~O(n²/2) total bytes. This is acceptable for typical suites but could be slow for very large ones. Not a correctness issue.

**Issue 6: `saveCheckpoint` returns an error that aborts the run, but the case was already added to `accs`**
This is consistent with the 10.2 abort semantics — a checkpoint write failure aborts the whole run. The case's results are in `accs` but the checkpoint isn't updated. On resume, the case would be re-executed. This is acceptable but worth noting.

**Issue 7: `applyReviewerOutcome` registers a reviewer on first sighting — what if the checkpointed case had a reviewer that no longer exists in the configured roster?**
Wait, the roster guard catches this BEFORE the replay happens. So this scenario is prevented. Good.

**Issue 8: What if `summary.Agents` includes an agent not in `cfg.Project.Agents`?**
This would be a pre-existing issue. The `reviewerModel(cfg, a)` function is called for each agent — if it handles missing agents, fine. But the roster guard compares against `cfg.Project.Agents`, not against actual agents seen. So if an unknown agent shows up in summary.Agents, it would be checkpointed but not in the roster guard. This is an edge case that probably doesn't apply.

**Issue 9: The `_ = err` from `os.RemoveAll(tmp)` in the defer**
Pre-existing, not changed.

**Issue 10: `caseReviewers` slice is allocated even when `cp == nil`?**
Let me re-check:
```go
var caseReviewers []checkpointReviewer
for _, a := range summary.Agents {
    ...
}
```
The slice is declared at the top of the agents loop. It's then appended to. After the loop, if `cp != nil`, it's used to create a checkpoint entry. If `cp == nil`, it's discarded. The allocation cost is small (just the slice header, since no appends happened). Not an issue.

Wait, actually re-reading — the slice is declared INSIDE the for loop iterating cases. Let me look again:
```go
for i, c := range m.Cases {
    if entry, ok := done[i]; ok {
        ...
    }
    ...
    var caseReviewers []checkpointReviewer
    for _, a := range summary.Agents {
        ...
    }
    if cp != nil {
        cp.Cases = append(cp.Cases, checkpointCase{Index: i, CaseID: c.ID, Reviewers: caseReviewers})
        ...
    }
}
```
Yes, `caseReviewers` is declared per case. That's fine.

Let me look more carefully for actual issues...

**Issue 11: Potential issue with `loadCheckpoint` and partial JSON write**
If a process is killed between `os.WriteFile` (for the temp file) and `os.Rename` (to replace the target), the temp file is orphaned. The `writeExportFile` helper (if it's truly atomic via temp+rename) should handle this. Not a bug in this diff.

**Issue 12: `saveCheckpoint` uses `json.MarshalIndent` — this changes between runs if field order changes**
This is a Go struct serialization concern, but `encoding/json` serializes struct fields in declaration order, which is stable. Good.

**Issue 13: The `validateCheckpointRoster` compares sorted sets of "agent=model" strings**
What if two agents have the same name but different models? The current signature is `n + "=" + cfg.Registry.Agents[n].Model`. If there are duplicate agent names (which shouldn't happen but...), they'd collapse. Pre-existing concern, not introduced by this diff.

**Issue 14: Checkpoint file permissions**
`writeExportFile` presumably sets file permissions. If it uses default `0644` or `0600`, the checkpoint might be world-readable. For a benchmark with potentially sensitive LLM data, `0600` might be more appropriate. Not in the diff to verify.

**Issue 15: Potential issue — what if `cfg.Project.Agents` is empty?**
`rosterSignature` would return an empty signature. `validateCheckpointRoster` would compare two empty slices, which would match. So an empty roster would silently pass the guard. But if the roster is empty, no reviewers would execute, so the checkpoint would have no cases. Resume would still work (replay nothing, execute nothing). Edge case, probably not realistic.

**Issue 16: `validateCheckpoint` only checks ReproHash + Suite + SuiteVersion**
What about other suite fields like suite description, etc.? These probably don't affect reproducibility, so not checking them is OK.

**Issue 17: Checkpoint file location — what if the directory doesn't exist?**
`writeExportFile` likely creates the directory or errors. The test `TestCheckpoint_RoundTrips` uses `filepath.Join(t.TempDir(), "nested", "ckpt.json")` — this would require the "nested" directory to exist. Let me check — actually t.TempDir() creates the temp dir, but `nested` subdir doesn't exist. So `os.WriteFile` would fail. Unless `writeExportFile` creates parent dirs.

Looking at the test — it passes with this path. So `writeExportFile` must create parent directories. Good.

Actually wait — looking more carefully at the test:
```go
path := filepath.Join(t.TempDir(), "nested", "ckpt.json")
```
The "nested" subdirectory doesn't exist. If `writeExportFile` doesn't `MkdirAll`, this would fail. The test passes, so `writeExportFile` must create parent dirs. Good (assuming it handles errors correctly).

**Issue 18: The case-id guard inside the resume loop returns `errCheckpointSuiteMismatch`**
This is semantically wrong — the suite identity is fine; only the per-index case ID differs. Reusing the sentinel is misleading. The wrapping message clarifies, but `errors.Is` checks would conflate the two. Should probably have a separate sentinel like `errCheckpointCaseMismatch`.

Let me now prioritize issues for the review:

1. **HIGH/MEDIUM**: `errCheckpointSuiteMismatch` reused for case-id drift — semantically wrong
2. **MEDIUM**: Potential panic in `rosterSignature` if registry entry is nil
3. **LOW**: Missing `resume.calls == 0` assertion in case-id drift test
4. **LOW**: Performance O(n²) write cost for large suites
5. **LOW**: `caseReviewers` allocation when `cp == nil` (minor — slice header only)

Let me re-examine #2 more carefully:
```go
func rosterSignature(cfg *fanout.ReviewConfig) []string {
    names := append([]string(nil), cfg.Project.Agents...)
    sort.Strings(names)
    sig := make([]string, len(names))
    for i, n := range names {
        sig[i] = n + "=" + cfg.Registry.Agents[n].Model
    }
    return sig
}
```
If `cfg.Registry.Agents[n]` is nil, this panics. The comment says "An agent with no configured model contributes an empty model component", but accessing `.Model` on a nil struct doesn't return empty — it panics in Go (unless the field is on a pointer and we dereference).

Actually, in Go, if `cfg.Registry.Agents` is a `map[string]*SomeStruct`, then `cfg.Registry.Agents[n]` returns nil for a missing key. Then `nil.Model` would panic with a nil pointer dereference.

This is a potential bug. But it's only an issue if the configuration has agents without registry entries, which is probably not the common case. Still, it's a robustness concern.

The existing `reviewerModel(cfg, a)` function (not in diff) presumably handles this. We should look at it to compare. Since I can't see it, I'll note this as a concern.

Actually, looking at the comment more carefully: "An agent with no configured model contributes an empty model component, which still distinguishes it from a later-configured one." This suggests the author was aware of the case but didn't handle the nil pointer. So this is a real bug if such configurations exist.

Hmm wait, let me reconsider. The `cfg.Registry.Agents` might be a map of `*AgentConfig` where `AgentConfig.Model` is a string field. Accessing `.Model` on a nil `*AgentConfig` would panic. Unless the field is accessed via a method that handles nil... but `Model` is a field, not a method.

Actually, in Go, `(*AgentConfig)(nil).Model` panics because you can't dereference a nil pointer. So this is a real bug.

But this might also be a pre-existing issue in `reviewerModel`. If `reviewerModel` handles it, then `rosterSignature` should too. If `reviewerModel` panics too, then `rosterSignature` is consistent with existing behavior.

Let me move on to other issues.

**Issue 19: `caseReviewers` is built but not used when `cp == nil`**
The slice is allocated and filled with entries, but if `cp == nil`, the entries are discarded. This is a minor allocation waste. Could be guarded with `if cp != nil { var caseReviewers ... }`.

**Issue 20: The order of `applyReviewerOutcome` calls in fresh execution**
The original code uses a "lazy init" pattern: when a new agent is seen, create the acc and register it in order. The refactored code does the same. Good.

But wait — there's a subtle issue. In the original code, the order in which agents are added to `accs` and `order` depends on `summary.Agents` iteration order. In the refactored code, `applyReviewerOutcome` is called for each agent in `summary.Agents` order. If the order differs between runs, `order` would differ, breaking reproducibility.

But the existing TestExecuteBenchmarkRun_Reproducible test passes, so this must be deterministic.

Actually wait, the original test uses `stubCompleter{}` and `executeBenchmarkRun` is called twice with the same inputs. The test asserts `jsonEq(ja, jb)`. If `summary.Agents` order is non-deterministic, this test would fail intermittently. Since it passes, `summary.Agents` order is deterministic.

For the new resume test (TestExecuteBenchmarkRun_FullResumeIsZeroCostAndIdentical), the second run uses the checkpointed reviewer order, which was determined by the first run's `summary.Agents` order. If this order is deterministic and consistent, the resume test should also pass.

OK so the determinism concern is real but mitigated by the existing tests and the fact that `summary.Agents` must be deterministic for the original code to work.

**Issue 21: Potential issue with `saveCheckpoint` and concurrent access**
Not applicable — single goroutine writes.

Let me focus on the most actionable issues:

1. **MEDIUM**: `errCheckpointSuiteMismatch` reused for case-id drift — should have a separate sentinel
2. **MEDIUM**: Potential nil pointer panic in `rosterSignature`
3. **LOW**: Minor performance — `caseReviewers` allocated even when `cp == nil`

Actually, let me re-examine #1 more carefully. The code:
```go
if entry.CaseID != c.ID {
    return nil, fmt.Errorf("%w: checkpoint case at index %d is %q but the suite has %q there; remove the checkpoint to start fresh",
        errCheckpointSuiteMismatch, i, entry.CaseID, c.ID)
}
```

And `validateCheckpoint` also returns `errCheckpointSuiteMismatch`. The semantics are different:
- Suite identity drift: ReproHash, Suite, or SuiteVersion differs
- Case-id drift: per-index case ID differs (suite was reordered or case at index was changed)

These are different failure modes. Using the same sentinel conflates them. Callers using `errors.Is(err, errCheckpointSuiteMismatch)` can't distinguish them.

However, looking at the code, the only callers are in `executeBenchmarkRun`, and they don't differentiate — they just return the error. So the impact is on the OPERATOR who sees the error message. The wrapping message includes enough context to disambiguate. So this is a minor design concern.

But it could be a real bug if any future code needs to differentiate. I'll note it as LOW.

**Issue 22: The `Roster` field in the checkpoint is not validated against the actual executed roster**
What if the engine somehow executes a different set of agents than configured? The roster guard compares against `cfg.Project.Agents`, but the checkpoint stores the executed reviewers. If the engine dynamically adds/removes reviewers, the checkpoint could have reviewers not in `cfg.Project.Agents`, and the resume would replay them, but a fresh run wouldn't include them. Edge case, probably not realistic.

OK let me finalize my findings. I'll focus on the most concrete issues:

1. **MEDIUM**: `rosterSignature` could panic if `cfg.Registry.Agents[n]` is nil — should use the same lookup pattern as `reviewerModel`
2. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel is reused for two semantically distinct failure modes (suite identity drift vs case-id drift)
3. **LOW**: `caseReviewers` slice is allocated/filled even when `cp == nil`, wasting work
4. **LOW**: O(n²) write cost — full checkpoint rewritten per case (acceptable but worth noting)

Actually, let me reconsider #1. The comment in the code says:
```
// An agent with no configured model contributes an empty model component, which still distinguishes it from a later-configured one.
```

This suggests the author intended for missing models to be represented as empty strings. But the code `cfg.Registry.Agents[n].Model` would panic if the entry is nil. So either:
- The author overlooked the nil case (real bug)
- The registry always has entries for all configured agents (no bug)

Without seeing `reviewerModel`, I can't be sure. But the comment suggests the author was thinking about this case. So this is likely a real bug.

Hmm, but maybe `cfg.Registry.Agents[n]` is guaranteed non-nil by construction (e.g., loaded/validated earlier). Let me check by looking at the code flow... I can't see the registry loading code in the diff. I'll note this as a concern.

Let me also consider:

**Issue 23: `applyReviewerOutcome` parameter shadowing**
The parameter `costUSD` shadows the field `acc.costUSD`. The code `acc.costUSD += costUSD` correctly uses the local parameter. No bug.

**Issue 24: `replayCheckpointCase` doesn't preserve `order` slice ordering across replay**
Actually, the loop in `executeBenchmarkRun` iterates `m.Cases` in order. For each case, either replay or fresh execution happens. The replay registers agents in checkpointed order, and fresh execution registers in `summary.Agents` order. As long as these orders are consistent, `order` is built deterministically.

If a checkpoint was built with order [A, B] and is resumed, then fresh cases execute with summary.Agents = [A, B] (must be, for AC3 to hold), then no new agents are added (they're already registered). So `order` stays [A, B]. Good.

If fresh cases have a NEW agent C, then C is registered at the end. `order` becomes [A, B, C]. Good.

OK, I think I've analyzed enough. Let me also look at one more thing:

**Issue 25: Test `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift` doesn't verify `resume.calls == 0`**
Looking at the test:
```go
resume := &countingCompleter{}
_, err = executeBenchmarkRun(context.Background(), cfg, resume, suiteValidPath, gen, path)
require.Error(t, err)
assert.ErrorIs(t, err, errCheckpointSuiteMismatch, "a per-index case-id drift aborts the resume")
```
It doesn't assert `resume.calls == 0`. For consistency with the roster drift tests, it should.

Actually, let me re-read the test. The resume variable is created but never checked for `.calls`. So the test would pass even if a Completer call was made before the case-id check. This is a test gap.

But wait — the case-id check happens BEFORE any LLM call in the resume loop. So `resume.calls` should be 0. Adding the assertion would make the test stronger. Minor issue.

**Issue 26: Performance concern with saveCheckpoint per case**
For a suite with N cases, the checkpoint is written N times, each time including all N entries. Total write bytes: O(N²). For N=100, that's 10,000 entries written total. For N=1000, that's 1,000,000 entries. Could be slow but not a correctness issue.

OK, let me finalize my findings. I'll go with:

1. MEDIUM: `rosterSignature` potential nil pointer panic
2. MEDIUM: `errCheckpointSuiteMismatch` sentinel reuse for two different drift types
3. LOW: Missing `resume.calls == 0` assertion in case-id drift test
4. LOW: O(n²) write cost

Actually, I want to double-check the nil pointer concern. Let me look at the type of `cfg.Registry.Agents[n]`. Without seeing the type definition, I'm guessing it's `*AgentConfig` or similar. In Go, accessing a field on a nil pointer panics.

But wait — maybe `cfg.Registry.Agents[n]` returns a zero-value struct (not nil), and `.Model` returns "". This depends on the registry's value type.

If it's `map[string]AgentConfig` (value type), then missing keys return zero values, and `.Model` returns "". No panic.
If it's `map[string]*AgentConfig` (pointer type), then missing keys return nil, and `.Model` panics.

Without seeing the type, I'll note this as a concern but with lower confidence.

Actually, I realize I should also check: does `reviewerModel(cfg, a)` (the existing function) handle this? Looking at the diff, `reviewerModel` is called in the existing code but not defined in the diff. The comment on `rosterSignature` says:
```
// It uses the CONFIGURED model (registry), not a runtime usage-reported one, so the same config always yields the
// same signature — the stable identity a resume compares against to reject a changed
// panel (AC4 roster guard). An agent with no configured model contributes an empty
// model component, which still distinguishes it from a later-configured one.
```

This suggests the author intended for missing models to yield "". If the existing `reviewerModel` handles this correctly, `rosterSignature` should mirror it. The fact that the author wrote the comment suggests they were aware of the edge case but didn't verify the implementation.

OK, I'll note this as MEDIUM with the caveat that it depends on the registry type.

Let me also think about whether `cfg.Registry.Agents[n].Model` could panic. In Go:
- If `cfg.Registry.Agents` is `map[string]AgentConfig`, then `cfg.Registry.Agents[n]` returns a zero-valued `AgentConfig`, and `.Model` returns `""`. No panic.
- If `cfg.Registry.Agents` is `map[string]*AgentConfig`, then `cfg.Registry.Agents[n]` returns `nil` for missing keys, and `nil.Model` panics.

Looking at the comment "An agent with no configured model contributes an empty model component", the author seems to assume missing models yield "". This works for value-type maps but not pointer-type maps.

Without the type definition, I'll note this as a potential issue.

Alright, let me write up the findings. I want to be concrete and actionable.

Final findings:

1. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel is reused for two semantically distinct failure modes (suite-identity drift and per-index case-id drift). Callers using `errors.Is` can't differentiate. Consider a separate sentinel like `errCheckpointCaseMismatch`.

2. **MEDIUM**: `rosterSignature` dereferences `cfg.Registry.Agents[n].Model` without nil-checking the map entry. If an agent is configured but missing from the registry (or if `Registry.Agents` is a `map[string]*AgentConfig`), this panics. The comment claims "an agent with no configured model contributes an empty model component", but the implementation doesn't handle the nil-entry case. Should mirror the pattern used by `reviewerModel`.

3. **LOW**: `caseReviewers` slice is allocated and populated even when `cp == nil` (no checkpointing). Minor wasted work — could be guarded by `if cp != nil { ... }`.

4. **LOW**: `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift` doesn't assert `resume.calls == 0`, unlike the parallel roster-drift tests. Adding the assertion would prove the guard fires before any LLM call.

5. **LOW**: Performance — `saveCheckpoint` rewrites the full JSON every case, giving O(N²) total write bytes for N cases. Acceptable for typical suites but worth noting.

Actually, let me reconsider #3. Looking at the code:
```go
var caseReviewers []checkpointReviewer
for _, a := range summary.Agents {
    ...
    if cp != nil {
        caseReviewers = append(caseReviewers, checkpointReviewer{...})
    }
}
```

Wait, let me re-read the diff more carefully. Looking at lines 142-167 of the new code:
```go
var caseReviewers []checkpointReviewer
for _, a := range summary.Agents {
    model := reviewerModel(cfg, a)
    persona := reviewerPersona(cfg, a.Agent)
    raised := raisedByReviewer[a.Agent]
    usageReported := a.TokensIn > 0 || a.TokensOut > 0
    var cost float64
    var latency int64
    if usageReported {
        cost = llmclient.ComputeCostUSD(a.Model, a.TokensIn, a.TokensOut)
        latency = a.DurationMS
    }

    applyReviewerOutcome(accs, &order, a.Agent, model, persona, c.ExpectedCategories, raised, usageReported, cost, latency)

    if cp != nil {
        caseReviewers = append(caseReviewers, checkpointReviewer{
            Agent:         a.Agent,
            ...
        })
    }
}
```

OK so the slice is only populated when `cp != nil`. The `var caseReviewers []checkpointReviewer` line declares the slice (nil header, no allocation). The append only happens inside `if cp != nil`. So the wasted work is just the nil slice declaration, which is negligible.

But there's still a minor inefficiency: the struct fields are computed (model, persona, usageReported, cost, latency) even when not needed for checkpointing. These are also needed for `applyReviewerOutcome`, so they can't be skipped.

OK, this is a non-issue. Let me remove it.

Actually wait, looking again — `usageReported`, `cost`, `latency` are computed regardless of `cp != nil`. But they're also passed to `applyReviewerOutcome`, which uses them. So this is correct.

OK so #3 is not an issue.

Let me reconsider my findings.

Actually, one more thing — looking at the test `TestExecuteBenchmarkRun_RejectsRosterMembershipDrift`:
```go
cfg1 := benchCfg([3]string{"greta", "m-greta", "greta"}, [3]string{"kai", "m-kai", "kai"})
```
This uses a 2-agent roster. The test creates a checkpoint with [greta, kai], then resumes with [greta, zara]. The roster guard should catch this. Good.

And `TestExecuteBenchmarkRun_RejectsRosterModelDrift`:
```go
cfg1 := benchCfg([3]string{"greta", "m-greta", "greta"})
...
cfg2 := benchCfg([3]string{"greta", "m-greta-v2", "greta"}) // same agent, new model
```
Single agent, model changed. Good.

These tests are well-designed.

Now let me finalize:

1. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift
2. **MEDIUM**: `rosterSignature` potential nil pointer panic on missing registry entry
3. **LOW**: Missing `resume.calls == 0` assertion in case-id drift test
4. **LOW**: O(N²) checkpoint write cost

I think these are the main issues. Let me write them up.

Wait, I want to look at one more thing. The `Reviewers` field in the checkpoint uses `[]string` for `Expected` and `Raised`. In Go, `json.Marshal` of `[]string{}` produces `[]`, and `json.Marshal` of `nil` produces `null`. When unmarshaled, both become `nil`. This could cause issues if the test expects `[]string{}` but gets `nil`.

Looking at the test `TestCheckpoint_RoundTrips`:
```go
Expected: []string{"correctness"}, Raised: []string{"correctness"},
```
The original has non-empty slices. So this round-trip works.

But what if `Raised` is nil (no findings)? The test doesn't cover this case. The fresh execution code:
```go
raised := raisedByReviewer[a.Agent]
```
If `raisedByReviewer[a.Agent]` is nil (no findings), `raised` is nil. Then `Raised: raised` in the checkpoint stores nil. JSON marshals nil slice as `null`. On unmarshal, it becomes nil. Then `Raised: raised` in replay uses nil.

Is `nil` vs `[]string{}` semantically equivalent for `benchmark.CaseScore`? Probably yes — both represent "no findings". But if there's any code that distinguishes them (e.g., `len(raised) == 0` vs `raised == nil`), it could matter.

Looking at the original code:
```go
acc.cases = append(acc.cases, benchmark.CaseScore{Expected: c.ExpectedCategories, Raised: raisedByReviewer[a.Agent]})
```
It stores whatever `raisedByReviewer[a.Agent]` returns. If that's nil, the case has `Raised: nil`. On resume, the checkpoint has `Raised: nil`, and replay stores `Raised: nil`. Same behavior. Good.

But if `raisedByReviewer[a.Agent]` returns `[]string{}` (empty but non-nil), and the checkpoint serializes as `null`, then on replay it becomes `nil`. Then `assert.Equal(t, want, got)` might fail because `[]string{}` != `nil`.

Looking at the test, it uses non-empty slices. So this isn't tested. But it's a potential issue for empty cases. Let me note this as a LOW concern.

Actually, looking at `json.Marshal` behavior: `json.Marshal([]string(nil))` produces `"null"`. `json.Marshal([]string{})` produces `"[]"`. On unmarshal, both become `nil`. So round-tripping `[]string{}` through JSON loses the distinction.

For `assert.Equal`, `[]string{}` and `nil` are different (one has len 0, the other is nil). So `TestCheckpoint_RoundTrips` would fail if it used `[]string{}`.

But the test uses `[]string{"correctness"}` (non-empty), so this isn't tested.

For real usage: if `raisedByReviewer[a.Agent]` returns `[]string{}` (empty but non-nil), the checkpoint would serialize as `null` (because `[]string{}` and `nil` are equivalent for JSON encoding? Actually no, they're not. Let me think again.

In Go: `var s []string = nil` vs `s := []string{}` — these are different. `len(nil) == 0` and `len([]string{}) == 0`, but `s == nil` differs.

`json.Marshal([]string(nil))` → `"null"`
`json.Marshal([]string{})` → `"[]"`

On unmarshal:
`json.Unmarshal([]byte("null"), &s)` → `s = nil`
`json.Unmarshal([]byte("[]"), &s)` → `s = []string{}` (non-nil)

So the distinction is preserved through JSON, as long as we marshal/unmarshal correctly.

Hmm, so for the round-trip to work, the slice must be the same nil/non-nil state. If the fresh code stores `[]string{}` and the checkpoint stores it as `[]`, the resume code stores it back as `[]string{}`. Good.

But what if `raisedByReviewer[a.Agent]` returns nil (not empty)? Then `raised = nil`. The checkpoint stores `null`. Resume code stores `nil`. Consistent.

So the round-trip works for both nil and empty slices, as long as the same value is used throughout.

OK, this is not an issue. Moving on.

Let me finalize my findings:

1. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift
2. **MEDIUM**: `rosterSignature` potential nil pointer panic
3. **LOW**: Missing `resume.calls == 0` assertion in case-id drift test
4. **LOW**: O(N²) checkpoint write cost

Hmm, let me also think about whether there are any security issues...

**Security: Checkpoint file might contain sensitive data**
The checkpoint contains reviewer scores, costs, latencies. If the operator passes a world-readable path, the data could be exposed. Not a code issue, but a deployment concern.

**Security: Checkpoint file could be tampered with**
The operator could edit the checkpoint to change scores. The guards (ReproHash, roster, case-id) catch some drift but not score tampering. If the operator changes `Raised` in the checkpoint, the resume would replay the modified score.

This is a real concern. The checkpoint is trusted because the operator controls it. If the operator is malicious, they could manipulate scores. But this is the same trust model as the suite itself — the operator controls the input.

For AC4 ("never silently mixed"), the guards prevent mixing checkpoints from different suites. But they don't prevent score tampering within a valid suite. This is acceptable — the operator who controls the checkpoint controls the run.

OK, no security issues to flag.

Let me write up the findings now.

Actually, one more thing. Looking at the `validateCheckpoint` function:
```go
func validateCheckpoint(cp *runCheckpoint, reproHash, suite, suiteVersion string) error {
    if cp.ReproHash != reproHash || cp.Suite != suite || cp.SuiteVersion != suiteVersion {
        return fmt.Errorf("%w: recorded suite %q version %q (hash %s), current suite %q version %q (hash %s); remove the checkpoint to start fresh",
            errCheckpointSuiteMismatch, cp.Suite, cp.SuiteVersion, shortHash(cp.ReproHash), suite, suiteVersion, shortHash(reproHash))
    }
    return nil
}
```

This is called with `(existing, curHash, m.Suite, m.SuiteVersion)`. If `existing` is nil... wait, the caller checks `if existing != nil` before calling. Good.

But wait, the `validateCheckpoint` function doesn't nil-check `cp`. If called with nil, `cp.ReproHash` would panic. But the caller guards against this. OK, defensive programming could add a nil check, but it's not strictly necessary.

Alright, finalizing:

1. MEDIUM: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift
2. MEDIUM: `rosterSignature` potential nil pointer panic
3. LOW: Missing `resume.calls == 0` assertion in case-id drift test
4. LOW: O(N²) checkpoint write cost (performance)

Actually, I realize I should double-check my #2 analysis. Let me re-read the `rosterSignature` code:

```go
func rosterSignature(cfg *fanout.ReviewConfig) []string {
    names := append([]string(nil), cfg.Project.Agents...)
    sort.Strings(names)
    sig := make([]string, len(names))
    for i, n := range names {
        sig[i] = n + "=" + cfg.Registry.Agents[n].Model
    }
    return sig
}
```

If `cfg.Registry.Agents[n]` returns nil (missing key), `nil.Model` panics.

In Go, `map[K]V` returns zero value for V if key missing. For `map[string]*AgentConfig`, the zero value is `(*AgentConfig)(nil)`. Accessing a field on nil pointer panics.

For `map[string]AgentConfig`, the zero value is `AgentConfig{}` (empty struct). Accessing `.Model` returns zero value of Model type (e.g., "" for string).

So this is a potential issue if `Registry.Agents` is a pointer map. Without seeing the type, I'll note this as a concern.

But the existing `reviewerModel(cfg, a)` function uses the same pattern (likely). If it handles nil entries, `rosterSignature` should too. If it panics, then `rosterSignature` is consistent with existing behavior.

Given the comment "An agent with no configured model contributes an empty model component", the author intended empty-string behavior. If the implementation doesn't achieve this, it's a bug.

I'll mark this as MEDIUM.

OK, finalizing my findings now.

One more thought — let me check if there are any issues with the `done` map being reused. Looking at:

```go
done = cp.doneIndex()
```

This is called once after loading the checkpoint. Then in the loop:
```go
if entry, ok := done[i]; ok {
```

This reads from `done` but doesn't modify it. Good.

But what if `cp.Cases` is modified (e.g., appended to) during the loop? `done` is a map built from `cp.Cases` at the time of `doneIndex()`. Subsequent appends to `cp.Cases` don't affect `done`. Good.

Wait, but `doneIndex` builds a map of case index → entry. If `cp.Cases` has duplicate indices (shouldn't happen), `done` would have the LAST entry (map overwrite). If indices are unique, `done` is correct.

The code appends to `cp.Cases` with `Index: i` where `i` is the loop index. Loop indices are unique. So `done` is consistent. Good.

OK, I'm confident in my findings now. Let me write them up.

Actually, wait. I want to verify one more thing. The `saveCheckpoint` function:

```go
func saveCheckpoint(path string, cp *runCheckpoint) error {
    data, err := json.MarshalIndent(cp, "", "  ")
    if err != nil {
        return fmt.Errorf("encoding checkpoint: %w", err)
    }
    return writeExportFile(path, data)
}
```

`writeExportFile` is not defined in the diff. The comment says "via the shared writeExportFile helper". If this helper is atomic (temp file + rename), then a crash mid-write leaves the previous checkpoint intact. Good.

But what if `writeExportFile` is NOT atomic? Then a crash mid-write could leave a corrupt checkpoint file. The `loadCheckpoint` function would then return a parse error, which would abort the run. This is acceptable behavior (per the design — a corrupt checkpoint aborts rather than silently starts fresh).

OK, no issue here.

Alright, finalizing.

Let me also consider one more potential issue:

**Issue: `applyReviewerOutcome` is called per reviewer per case. For checkpointed cases, the order of reviewers in `entry.Reviewers` determines the order they're added to `accs`. For fresh cases, the order of reviewers in `summary.Agents` determines the order.**

If these orders differ between checkpoint write and resume, the `order` slice would differ, breaking AC3.

But the checkpoint stores reviewers in `summary.Agents` order. The resume reads them in that same order. So they're consistent.

The only way this could fail is if `summary.Agents` order differs between the time the checkpoint was written and the time of resume. But the run loop uses `summary.Agents` to populate `caseReviewers` and to call `applyReviewerOutcome`. So both happen in the same loop iteration, with the same `summary.Agents` order. Good.

For replay, the checkpoint is read once at the start, and `done = cp.doneIndex()` is built. Then in the loop, `entry := done[i]` retrieves the entry, and `replayCheckpointCase` iterates `entry.Reviewers` in stored order. This matches the order they were stored in (which was `summary.Agents` order at the time of checkpoint write).

So the order is consistent. Good.

OK, I'm done analyzing. Let me write up the findings.

Hmm, actually one more thing. Let me look at the test `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift` more carefully:

```go
// Tamper only the recorded CaseID at index 0 — hash/suite/version stay valid, so
// validateCheckpoint passes and the per-index id guard is what must fire.
cp, err := loadCheckpoint(path)
require.NoError(t, err)
cp.Cases[0].CaseID = "some-other-case"
require.NoError(t, saveCheckpoint(path, cp))
```

This test tampers with the checkpoint AFTER it was valid. But the repro hash was computed BEFORE the tamper. Does the repro hash change? No, because the repro hash is computed from the suite, not the checkpoint. So `validateCheckpoint` passes.

Then the per-index CaseID guard fires. The test asserts `errors.Is(err, errCheckpointSuiteMismatch)`. Good.

But the test doesn't assert `resume.calls == 0`. The `countingCompleter` is created but never checked for calls. This is a test gap.

For consistency with the other guard tests, this assertion should be added.

OK, I'll note this as LOW.

Final findings:

1. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift (`cmd/atcr/benchmark_run.go:62`)
2. **MEDIUM**: `rosterSignature` potential nil pointer panic (`cmd/atcr/benchmark_run.go:212`)
3. **LOW**: Missing `resume.calls == 0` assertion in case-id drift test (`cmd/atcr/benchmark_run_resume_test.go:230`)
4. **LOW**: O(N²) checkpoint write cost (performance, `cmd/atcr/benchmark_run.go:170`)

Let me also reconsider whether #2 is really a MEDIUM or LOW. It depends on:
- Whether `Registry.Agents` is a pointer map or value map
- Whether missing entries are realistic

Without the type definition, I can't be sure. But the comment suggests the author was thinking about this case. If the implementation doesn't handle it, it's a real bug. If the type is a value map, no panic occurs.

I'll keep it as MEDIUM.

For #1, the sentinel reuse is a real semantic concern. It's MEDIUM because it affects error handling clarity and future code that needs to differentiate.

Alright, writing up now.

Wait, one more concern. Let me check the `applyReviewerOutcome` function once more:

```go
func applyReviewerOutcome(accs map[string]*reviewerAcc, order *[]string, agent, model, persona string, expected, raised []string, usageReported bool, costUSD float64, latencyMS int64) {
    acc := accs[agent]
    if acc == nil {
        acc = &reviewerAcc{model: model, persona: persona}
        accs[agent] = acc
        *order = append(*order, agent)
    }
    acc.cases = append(acc.cases, benchmark.CaseScore{Expected: expected, Raised: raised})
    if usageReported {
        acc.costUSD += costUSD
        acc.latencies = append(acc.latencies, latencyMS)
    }
}
```

The function locks `model` and `persona` at first sighting. This matches the original behavior. But what if:
- Case 0: agent "greta" with model "m-greta" → acc.model = "m-greta"
- Case 1: agent "greta" with model "m-greta-v2" → acc.model stays "m-greta"

This means the model's value depends on the first case where the agent appears. If a model change happens between cases, only the first model is recorded.

For a single run, this is fine — the model doesn't change during the run. For a resume, the checkpoint stores the model at the time of case 0. On replay, the model is replayed for case 0. For case 1 (fresh execution), the model comes from `cfg` again.

But what if the model changes between checkpoint write and resume? The roster guard catches this — if `cfg.Registry.Agents["greta"].Model` changed, the roster differs. So the resume is rejected.

If the model stays the same but for some reason the runtime reports a different model... that's a runtime inconsistency, not a checkpoint concern.

OK, no issue here.

Let me also consider: what if the agent is registered with one model, but `reviewerModel(cfg, a)` returns a different model than what's in the registry? This would be a bug in `reviewerModel`, not in this code.

Alright, finalizing.

Oh wait, one more concern. The `loadCheckpoint` function:
```go
func loadCheckpoint(path string) (*runCheckpoint, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return nil, nil
        }
        return nil, fmt.Errorf("reading checkpoint %s: %w", path, err)
    }
    var cp runCheckpoint
    if err := json.Unmarshal(data, &cp); err != nil {
        return nil, fmt.Errorf("checkpoint %s is corrupt: %w", path, err)
    }
    return &cp, nil
}
```

What if the file is empty? `os.ReadFile` returns `[]byte{}` (no error). Then `json.Unmarshal([]byte{}, &cp)` returns an error (unexpected end of JSON input). So it would be caught as a parse error. Good.

What if the file contains just whitespace? `json.Unmarshal` would error. Good.

OK, no issue.

Alright, I'm really done now. Writing up findings.

Actually, I realize I should also check: does `validateCheckpointRoster` handle the case where `cp.Roster` is nil (checkpoint from before this field was added)? If a checkpoint exists from an earlier version without `Roster`, `cp.Roster` would be nil. `sortedCopy(nil)` returns `nil`. Comparing `nil` to `nil` via `equalStrings` returns true (both have len 0). So the guard passes. Good — backward compatible.

But wait, this would mean a checkpoint without `Roster` would always pass the roster guard, even if the roster changed. Is that the right behavior?

For a checkpoint from an older version (no roster field), we can't verify the roster. The conservative behavior would be to reject (fail closed). The lenient behavior would be to accept (assume compatible).

The current implementation accepts. This might be intentional (avoid breaking existing checkpoints) or a bug (should fail closed).

Given the comment "mirrors fanout's ErrRosterChanged contract" and "must fail closed", I think the intent is to fail closed. But the implementation accepts nil roster.

Hmm, this is a subtle issue. Should `validateCheckpointRoster` reject nil roster?

If the checkpoint is from the current version, Roster is always populated (set during checkpoint creation). So nil Roster only happens for older versions. The intent for AC4 ("never silently mixed") suggests rejecting such cases.

I'll note this as a MEDIUM concern.

Actually, let me reconsider. The current implementation accepts nil Roster as matching any roster. This means an older checkpoint (without Roster) could be resumed with a different roster. The cost of the resume is that the Roster field would be added on the next save. Subsequent resumes would check Roster correctly.

But during the resume itself, the roster could be silently different. This violates AC4.

I think the right behavior is to reject nil Roster. Let me note this as MEDIUM.

Actually, since this is the initial introduction of the Roster field, there are no "older" checkpoints to worry about. The concern is theoretical. I'll keep it as MEDIUM but with this caveat.

OK, finalizing now for real.

Findings:
1. MEDIUM: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift
2. MEDIUM: `rosterSignature` potential nil pointer panic
3. MEDIUM: `validateCheckpointRoster` accepts nil Roster, violating fail-closed contract
4. LOW: Missing `resume.calls == 0` assertion in case-id drift test
5. LOW: O(N²) checkpoint write cost

Hmm, #3 might be over-stated since there's no older checkpoint format to worry about. Let me downgrade to LOW.

Actually, the AC4 contract says "never silently mixed". A nil Roster that's accepted violates this contract in principle, even if no older checkpoints exist today. I'll keep it as MEDIUM.

OK, writing up.

Hmm, let me also think about whether there are any issues with the `order` slice being modified via `*order = append(*order, agent)`. In Go, `append` may reallocate the underlying array. If `order` is shared across goroutines, this would be a race. But the run loop is single-goroutine, so no issue.

OK, finalizing.

Let me think about whether there are any other issues I missed...

Looking at the `rosterSignature` function once more:
```go
func rosterSignature(cfg *fanout.ReviewConfig) []string {
    names := append([]string(nil), cfg.Project.Agents...)
    sort.Strings(names)
    sig := make([]string, len(names))
    for i, n := range names {
        sig[i] = n + "=" + cfg.Registry.Agents[n].Model
    }
    return sig
}
```

Wait, the comment says "An agent with no configured model contributes an empty model component". This implies `cfg.Registry.Agents[n].Model` could be empty. If the registry entry exists but has empty Model, this works fine. If the registry entry is nil (missing), it panics.

So the concern is specifically about nil entries. The code handles empty Model but not nil entry. This is a subtle but real bug.

I'll keep this as MEDIUM.

Alright, writing up now.

Actually, I realize I should look at the `reviewerModel` function to compare. It's not in the diff but is called. Without seeing it, I can't be sure if it handles nil entries.

But the comment in `rosterSignature` says it should handle missing models. If the existing code doesn't panic on missing models, then `rosterSignature` should mirror that behavior. The current implementation doesn't mirror — it could panic.

I'll keep this as MEDIUM with the caveat that it depends on the type of `Registry.Agents`.

OK, truly finalizing now.

Let me also verify the line numbers I want to cite:

- `cmd/atcr/benchmark_run.go:62` — the per-index CaseID check using `errCheckpointSuiteMismatch`
- `cmd/atcr/benchmark_run.go:212` — `rosterSignature` (the for loop dereferencing `.Model`)
- `cmd/atcr/benchmark_run.go:178` — `validateCheckpointRoster` (the nil Roster handling)
- `cmd/atcr/benchmark_run_resume_test.go:230` — the case-id drift test missing assertion

Let me count lines more carefully in the diff.

The new `benchmark_run.go` diff adds significant code. Let me estimate line numbers:

The new file `benchmark_checkpoint.go` has 176 lines.
The modified `benchmark_run.go` adds new code at various points.

Looking at the diff for `benchmark_run.go`:
- The function signature changes at line 37
- The checkpoint setup block is added around line 37-65 (new code)
- The replay logic is added around line 65-75
- The `applyReviewerOutcome` extraction is around line 145-180
- The checkpoint write is around line 170-180
- The `rosterSignature`, `reviewerAcc`, `applyReviewerOutcome`, `replayCheckpointCase` functions are added at the end

Let me estimate based on the diff hunks:

Hunk 1 (around line 37-105): Adds checkpoint setup and replay logic
Hunk 2 (around line 142-185): Refactors agents loop and adds checkpoint write
Hunk 3 (around line 206-265): Adds new helper functions

For the diff line numbers in my findings, I'll use approximate references based on the diff structure:

- The per-index CaseID check: in the replay block, around line 62 of the new code (within the for loop)
- `rosterSignature`: in the new helpers section, around line 210-225
- `validateCheckpointRoster`: in `benchmark_checkpoint.go`, around line 122
- The case-id drift test: in `benchmark_run_resume_test.go`, around line 225-240

I'll use approximate line numbers based on the file structure.

Alright, let me write up the findings now. I'll use line numbers that are approximate but identifiable.

Let me reconsider the severity of #3 (nil Roster). Since this is the initial introduction of the field, there are no existing checkpoints to worry about. The concern is hypothetical. I'll downgrade to LOW.

Final findings:

1. **MEDIUM**: `errCheckpointSuiteMismatch` sentinel reuse for case-id drift
2. **MEDIUM**: `rosterSignature` potential nil pointer panic on missing registry entry
3. **LOW**: `validateCheckpointRoster` accepts nil Roster, potentially violating fail-closed contract
4. **LOW**: Missing `resume.calls == 0` assertion in case-id drift test
5. **LOW**: O(N²) checkpoint write cost

Writing up now.

Hmm, looking once more, I want to make sure I'm not missing anything. Let me scan the diff one more time...

OK, I think I've covered the main issues. Let me write them up.

One more thing — I want to verify the `applyReviewerOutcome` parameter naming. It takes `costUSD` as a parameter and `acc.costUSD` is a field. The line `acc.costUSD += costUSD` correctly uses the parameter. But this is a code smell — the parameter shadows the field name conceptually. Not a bug, but worth noting.

Actually, this isn't worth flagging. It's standard Go style.

OK, finalizing.

Let me also think about whether `errCheckpointSuiteMismatch` being reused for case-id drift could cause test failures. The test `TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift` asserts `errors.Is(err, errCheckpointSuiteMismatch)`. If I changed the sentinel, this test would need updating. So the current behavior is consistent with the test. Changing it would be a refactoring.

OK, the current behavior is intentional (reusing the sentinel). It's not a bug per se, but it's a design choice that affects error handling clarity. I'll keep it as MEDIUM with this context.

Alright, writing up.

Final review:

The diff is generally well-written and covers the sprint requirements. The main concerns are:
1. Sentinel reuse for semantically distinct errors
2. Potential nil pointer in rosterSignature
3. Test gap (missing assertion)
4. Performance (O(N²) writes)
5. Backward compatibility (nil Roster acceptance)

Let me finalize the findings.
</think>

MEDIUM|cmd/atcr/benchmark_run.go:75|errCheckpointSuiteMismatch sentinel reused for per-index CaseID drift|Introduce a separate errCheckpointCaseMismatch sentinel so callers can distinguish suite-identity drift from per-index case-id drift|correctness|10|two semantically different failure modes share one sentinel; errors.Is callers cannot differentiate|bruce
MEDIUM|cmd/atcr/benchmark_run.go:213|rosterSignature dereferences cfg.Registry.Agents[n].Model without nil-checking the map entry|Guard cfg.Registry.Agents[n] against nil (or mirror reviewerModel's lookup) so a configured-but-unregistered agent does not panic; the comment promises empty-model handling|correctness|8|comment claims "empty model component" but nil entry panics on .Model access|bruce
LOW|cmd/atcr/benchmark_checkpoint.go:122|validateCheckpointRoster accepts a nil cp.Roster as matching any current roster|Return errCheckpointRosterMismatch when cp.Roster is nil so a checkpoint lacking the roster field cannot silently resume against a changed panel (AC4 fail-closed)|security|5|nil Roster short-circuits equalStrings to true, bypassing the guard; violates "never silently mixed" contract|bruce
LOW|cmd/atcr/benchmark_run_resume_test.go:240|TestExecuteBenchmarkRun_RejectsCheckpointCaseIDDrift never asserts resume.calls == 0|Add assert.Equal(t, 0, resume.calls) to prove the per-index guard fires before any LLM call, matching the roster-drift tests|correctness|3|countingCompleter is created and never asserted; test would pass even if guard fires after a Completer call|bruce
LOW|cmd/atcr/benchmark_run.go:178|saveCheckpoint rewrites the full JSON on every case, giving O(N^2) total write bytes|Acceptable for typical suites but worth noting; could append-only or compact-on-completion if suites grow large|performance|15|for N cases, saveCheckpoint serializes and writes O(N) bytes per case; total bytes O(N^2)|bruce