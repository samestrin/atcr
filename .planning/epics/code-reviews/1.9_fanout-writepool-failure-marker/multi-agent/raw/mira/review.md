<think>
Let me carefully analyze this diff to identify any issues.

The diff modifies a Go codebase (`atcr` - appears to be some kind of code review tool) to add a `FailureMarker` field to `PoolSummary` to distinguish real run summaries from best-effort failure markers. Let me look at each change:

1. **`internal/fanout/artifacts.go`**:
   - Adds a `FailureMarker bool` field to `PoolSummary` struct with `omitempty` JSON tag
   - Updates `writeFailureSummary` to set `FailureMarker: true`

2. **`internal/fanout/artifacts_test.go`**:
   - Adds assertions that normal WritePool has `FailureMarker: false`
   - Adds assertions that failure summary has `FailureMarker: true`

3. **`internal/fanout/reviewdir.go`**:
   - Adds logic in `ReadManifestPartial` to force partial=true when failure marker is set and `Succeeded > 0`

4. **`internal/fanout/reviewdir_test.go`**:
   - Adds three new test cases

Let me look for issues:

### Issue 1: Backward compatibility concern (but not a real issue)
The `FailureMarker` field is added with `omitempty` so older readers (without the field) will see a missing field. JSON decoding in Go ignores unknown fields by default, so this is fine.

### Issue 2: Reading old failure summaries
The new code in `ReadManifestPartial` checks `ps.FailureMarker && ps.Succeeded > 0`. But what about OLD failure summaries that don't have the `FailureMarker` field? Old `writeFailureSummary` (before this change) created summaries WITHOUT the marker. After upgrading, those old summary files won't have the marker, so the new code won't force partial. This could be a correctness issue if a previous version produced a failure summary and now we're reading it.

Wait, let me re-read. The `omitempty` ensures that real (non-failure) summaries don't have the field. Old failure summaries also wouldn't have the field because the field didn't exist. So in either case, the marker would be false (zero value), and the new logic wouldn't trigger.

This means: After this change, an OLD failure summary file (created before the change) would not be detected as a failure marker. The code would fall back to `ps.Partial` as before. This is the pre-change behavior, so technically not a regression, but it does mean the bug being fixed can still occur for old summary files.

However, this might be acceptable as the test environment presumably doesn't have old summary files. But it's worth noting as a LOW severity concern.

### Issue 3: Manifest fallback path
Looking at `ReadManifestPartial`:
```go
if data, err := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", summaryFile)); err == nil {
    var ps PoolSummary
    if json.Unmarshal(data, &ps) == nil {
        // New logic here
        if ps.FailureMarker && ps.Succeeded > 0 {
            return true
        }
        return ps.Partial
    }
}
// Fall back to manifest.json
```

If the summary.json exists but is corrupt (unmarshal fails), it falls back to manifest. But if the summary.json has the failure marker and we unmarshal successfully, we use the failure logic. This seems OK.

But what about the case where summary.json is corrupt AND it's actually a failure marker? We'd fall back to the manifest, which might not have partial=true. This is a pre-existing behavior, not introduced by this diff. Low concern.

### Issue 4: Race conditions / Concurrency
There's a TOCTOU between reading the summary file and using it. But this is pre-existing. Not introduced by this diff.

### Issue 5: The comment says "Zero successes is a genuine total failure with no surviving subset to protect"
This is a reasonable heuristic. But what if an agent has a different status like "skipped" or "errored"? Let me think about what `Status` types exist...

Looking at the test, there are `StatusOK` and possibly other statuses. If an agent has status like "skipped" or "errored" but didn't fail, that could be confusing. Let me check if `Succeeded` only counts `StatusOK`...

I don't have the full code, but based on context, `Succeeded` likely counts only successful runs. The logic might miss cases where agents had other non-failed states. However, the test and comment suggest this is acceptable.

### Issue 6: Test description accuracy
In `TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial`, the comment says:
> Exactly what ExecuteReview does on a WritePool error: the agents ran, the persistence faulted, so writeFailureSummary records the real (all-OK) tally.

This is good - it tests the integration.

### Issue 7: Missing edge case in test
What about a failure marker with `Succeeded=0` but `Failed=0`? The current logic would not force partial because `Succeeded > 0` is false. But if no agent succeeded AND no agent failed, that's a weird state. Probably not a real case but worth thinking about.

### Issue 8: Magic constant
The `summaryFile` constant is used. It's defined somewhere and used correctly. Not an issue.

### Issue 9: Error handling
The code uses `json.Unmarshal(data, &ps) == nil` to check for errors silently. This is pre-existing.

### Issue 10: Potential issue with old summary files
After this change, if there's an old `summary.json` file in `sources/pool/` that was created by an old version of the code (failure summary, no `FailureMarker` field), the new code won't detect it as a failure marker. This is a backward compatibility concern for in-place upgrades.

Let me reconsider... Actually, for a code review focused on the DIFF, this is somewhat out of scope since the diff is a feature add, not a maintenance task. I'll mention it as LOW.

### Issue 11: The `omitempty` trade-off
The comment says:
> omitempty keeps it absent from real summaries, so older readers correctly see the zero value (false).

This is correct. Real summaries (created by `WritePool` directly, not by `writeFailureSummary`) won't have the field, and older readers will see `false` (the zero value for bool). This is good.

### Issue 12: What if ExecuteReview calls writeFailureSummary and then a different process tries to read it?
This is a race condition concern. If WritePool is still writing, ReadManifestPartial could see a partial file. But this is pre-existing.

### Issue 13: The `omitempty` could be considered a small risk
If a real run had `FailureMarker: false` (zero value) and was written with `omitempty`, it would be absent. But the field is only set to `true` in `writeFailureSummary`, never explicitly to `false` in `WritePool`. So real summaries will always omit the field. This is correct.

### Issue 14: The test name suggests it's the "AC" (acceptance criteria)
`TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial` is described as an "End-to-end AC" test. Good test coverage.

### Issue 15: Documentation concern
The comment on `FailureMarker` is very detailed and explains the rationale. Good.

### Issue 16: Naming concern
`FailureMarker` is OK but could be more specific. Maybe `BestEffortMarker` or `WriteFailureMarker`? This is a maintainability concern but minor.

Let me check the `omitempty` field more carefully...

Actually, one thing to consider: the `omitempty` on `FailureMarker` means the field is omitted from JSON when false. If a real summary was somehow written with `FailureMarker: false` (zero value), it would also be omitted. So readers can't distinguish between "marker was set to false" and "marker field doesn't exist". This is fine in practice because real runs never set the marker.

### Issue 17: The test "TestReadManifestPartial_FailureMarkerForcesPartial" - what if the summary is malformed?
If the JSON is malformed, `json.Unmarshal` returns an error, and the code falls back to `manifest.json`. This is pre-existing behavior.

### Issue 18: What if the same review has BOTH a valid summary AND a manifest?
The code reads summary first and returns immediately if successful. This is the design.

### Issue 19: The behavior of `writeFailureSummary` itself
The function ignores the result of writing the summary JSON (`fmt.Fprintf` to stderr). This is pre-existing.

### Issue 20: What if the failure summary's agents are all 0, but the partial is true?
This shouldn't happen in practice because `writeFailureSummary` uses `summarize(results)` which sets partial based on the results. But if a failure occurred with all-OK results, partial would be false, and the marker would force it to true. Good.

### Issue 21: Test coverage
The tests cover:
- Normal WritePool: marker is false ✓
- Failure summary with partial: marker is true ✓
- Failure summary all-failed: marker is true ✓
- ReadManifestPartial forces partial when marker + successes ✓
- ReadManifestPartial stays non-partial when marker + zero successes ✓
- End-to-end: ExecuteReview failure path forces partial on read ✓

Good coverage. Missing: What about a failure summary where some agents have non-OK, non-failed status? Probably not a real case.

### Issue 22: Performance
No performance issues introduced.

### Issue 23: Security
No security issues introduced.

### Issue 24: Could the test test the case where a manifest is present but stale?
Pre-existing. The new tests focus on the failure marker case.

### Issue 25: Comments
The comments are very thorough, which is good for maintainability.

Let me now also think about whether the change is correct from a business logic standpoint:

The original bug: After a WritePool I/O fault, the failure summary might say `Partial=false` even though only a subset of agents reached disk. A reconcile over the surviving artifacts could miss the unflushed agents.

The fix: Add a `FailureMarker` field. When reading, if the marker is set and at least one agent succeeded, force `partial=true`.

The logic: "If we had a write failure but some agents succeeded, the on-disk set is incomplete, so we should reconcile (treat as partial)."

This seems correct. The edge case "Succeeded=0" means all agents failed, so there's no surviving subset to worry about. The reconcile would just emit "all failed" verdict, which is correct.

But wait - what about the case where some agents had status like "skipped" or "errored" (not "failed")? If they didn't `Fail` but also didn't `Succeed`, and the write was incomplete... Hmm.

Looking at the `Result` struct usage in tests:
- `Status: StatusOK` -> Succeeded
- Likely `Status: StatusFailed` or similar -> Failed
- Maybe other statuses?

If an agent has `Status: StatusSkipped` (just hypothetically), it wouldn't be counted in `Succeeded` or `Failed`. In that case, if the write was partial, we'd miss them. But this might be acceptable if the `Succeeded` count is the right thing to check.

Actually, I think the logic might be: "If any agent produced a result, the write might be incomplete." But the code only checks `Succeeded > 0`. This is an assumption that all non-failed agents are successes.

This is potentially a missing edge case, but it depends on the `Result` model. I don't have full visibility. I'll mention it as a potential concern.

### Issue 26: The behavior of `writeFailureSummary` when all results are successful
If all agents ran successfully but the write failed, `writeFailureSummary` would be called with all-OK results. `summarize(results)` would set `Partial=false, Succeeded=N, Failed=0`. The marker would be set. Reading it would force partial=true because `Succeeded > 0`. This is correct.

### Issue 27: The behavior when writeFailureSummary itself fails
If `writeFailureSummary` can't write the summary.json (e.g., disk full), no marker is created. The next read falls back to manifest. If manifest is also missing, defaults to false. Pre-existing behavior.

OK let me now structure my findings:

The main issues I see:

1. **LOW**: Backward compatibility - old failure summary files (from before this change) won't be detected as failure markers, so the bug could reoccur for in-place upgrades.

2. **LOW/INFO**: The `Succeeded > 0` check assumes that any non-failed agent was a success. If there are other agent statuses (skipped, errored, etc.) that don't increment `Failed`, they would be missed.

3. **LOW**: Naming - `FailureMarker` is somewhat generic. Consider `WriteFailureMarker` or `BestEffortMarker` for clarity.

4. **LOW**: Consider whether the test should also cover the case where the JSON unmarshal fails (fallback path).

5. **LOW/INFO**: The `omitempty` trade-off means the field is implicitly "true" when present, "false" when absent. This is OK because real runs never set the field, but it's an implicit invariant that should be documented or enforced.

Let me think about whether there are HIGH or MEDIUM issues...

Looking again carefully...

I notice the test `TestReadManifestPartial_FailureMarkerForcesPartial` creates a summary with `Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true`. This is the case where every agent ran but the write failed. Good test.

`TestReadManifestPartial_FailureMarkerAllFailedStaysNonPartial` tests the case where `Succeeded=0`. Good.

`TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial` is the end-to-end test. Good.

I think the diff is well-structured. The main concerns are:
- Backward compatibility for old summary files
- The `Succeeded > 0` heuristic
- Minor naming/maintainability

Let me also think about:

### Potential bug: What if PoolSummary JSON has the FailureMarker field but with value false?
If someone (a tool, a test, an old code path) writes `FailureMarker: false` explicitly, the `omitempty` would omit it. So this case is fine.

### Potential bug: What if Unmarshal is strict?
Go's `json.Unmarshal` is not strict by default - it ignores unknown fields. So older code reading new summaries would see the field as zero value. This is fine.

### Potential bug: What if the manifest has a `partial` field that's not in the new struct?
Pre-existing.

I think the diff is solid. Let me write up my findings.

Actually, one more thought: the test `TestReadManifestPartial_FailureMarkerForcesPartial` directly writes a JSON to disk, but doesn't actually call `writeFailureSummary`. This means the test isn't testing the integration between `writeFailureSummary` and `ReadManifestPartial` - it's testing the latter with a manually constructed summary.

The third test `TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial` does test the integration. Good.

But what about testing the case where a manifest is present AND a failure summary? The existing test `TestReadManifestPartial_SummaryIsSourceOfTruth` covers that for the non-failure case. The new tests don't explicitly cover the failure marker + manifest case. This might be worth a test.

OK, let me finalize my findings. The diff is overall well-crafted. I'll focus on real, actionable issues.

Final list of issues:

1. **MEDIUM/LOW**: Backward compatibility for old failure summary files. If a review's `sources/pool/summary.json` was written by an older version that didn't have `FailureMarker`, the new code won't detect it as a failure marker. The fix would be: on read, if `FailureMarker` is absent but the file is in the format of a failure summary... actually, there's no way to distinguish an old failure summary from a new real summary without the marker. So this is fundamentally a backward-incompat fix. The mitigation would be: document that users should clear old summary files after upgrade, OR add a heuristic like "if summary exists but manifest doesn't, AND the partial flag is false AND Total > 0..." - but this is fragile. The most pragmatic fix is to just note that this only protects against NEW failure scenarios.

Actually, on reflection, this is probably a LOW concern because:
- The change is for the WritePool I/O fault case, which is rare
- Users are unlikely to have old failure summaries sitting around
- A failure summary is, by definition, a recovery artifact
- Even if the bug reoccurs for an old summary, the next reconcile would refresh it

2. **LOW**: The `Succeeded > 0` heuristic might miss cases where agents have other non-failed statuses. The fix would be to check `Total > 0 && Failed < Total` (i.e., at least one agent had a non-failed status, so the write could be incomplete).

3. **LOW**: Minor: Consider naming `FailureMarker` to `WriteFailureMarker` for clarity.

4. **LOW/INFO**: The implicit invariant that "real summaries never set FailureMarker" is not enforced. Consider documenting this or adding a guard.

5. **LOW**: Missing test case for failure marker + manifest presence (to ensure manifest doesn't override the marker logic).

Let me also think about correctness more carefully.

Looking at the new logic in `ReadManifestPartial`:
```go
if ps.FailureMarker && ps.Succeeded > 0 {
    return true
}
return ps.Partial
```

This is straightforward and correct. The only concern is the edge cases I mentioned.

Looking at the test `TestReadManifestPartial_FailureMarkerForcesPartial`:
```go
sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true})
```

With `omitempty`, this JSON would be:
```json
{"total":2,"succeeded":2,"failed":0,"partial":false,"failure_marker":true}
```

OK, the marker is present. Good.

Let me also think about the order of operations in `writeFailureSummary`:
```go
sum := summarize(results)
ps := PoolSummary{Total: sum.Total, Succeeded: sum.Succeeded, Failed: sum.Failed, Partial: sum.Partial, FailureMarker: true}
if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
    fmt.Fprintf(os.Stderr, "atcr: warning: writeFailureSummary: write summary: %v\n", err)
}
```

This is a new change. The marker is set to true. Good.

OK, I'll structure my output now.

Actually, let me also consider: the `Succeeded` field might include all non-failed agents (i.e., successes + skipped + errored). If that's the case, then `Succeeded > 0` would correctly identify "at least one agent had a non-failed outcome". But if `Succeeded` only counts actual successes, and there's a `Skipped` or `Errored` status that doesn't increment `Failed`, those agents would be missed.

Without the full source, I can't be 100% sure. I'll mention this as a potential concern.

Let me check the tests more carefully to see if there are other statuses...

In the test `TestWritePool_MergedFindingsAndSummary`, the assertion is `assert.True(t, ps.Partial)`. For partial to be true, there must be a mix of succeeded and failed. The test setup probably has a mix. Let me see if I can find the StatusOK / StatusFailed definitions...

I don't have full source access. I'll mention this as a possible concern.

OK final answer:

Actually, I realize I should focus on the highest-quality findings. Let me prioritize:

1. **MEDIUM** - The `Succeeded > 0` heuristic might not be sufficient. If there are non-failed, non-succeeded agent statuses (skipped, errored), the check could miss incomplete writes. Better check: `Total > 0 && Failed < Total` or `Total - Failed > 0`.

2. **LOW** - Backward compat: old failure summary files won't be detected.

3. **LOW** - Naming.

4. **LOW** - Missing test for manifest presence + failure marker interaction.

5. **LOW** - Implicit invariant not enforced.

Let me write up the TD_STREAM output.

Wait, I should double check on the comment about agents with skipped/errored status. Let me search the test file for Status types:

From the test: `Status: StatusOK, PayloadMode: "blocks"`. The struct has `Status` field. There must be at least `StatusOK` and `StatusFailed`. There might be others.

Looking at the artifacts_test.go in the diff, I see `Result` struct used with `Status: StatusOK`. The summary function probably has logic like:
- If Status == StatusOK: Succeeded++
- If Status == StatusFailed: Failed++
- Partial = (Succeeded > 0 && Failed > 0)

This is speculative. The check `Succeeded > 0` in the diff assumes that any non-failed agent is a success. If there are other statuses that don't increment `Failed` or `Succeeded`, they could be missed.

But honestly, the more robust check would be: "if Total > 0 and the on-disk set might be incomplete". The current check `Succeeded > 0` is a reasonable proxy. Without more info, I'll mention this as a potential concern.

Actually, on further reflection, I think this concern is a bit hypothetical. The diff is well-tested and the heuristic is reasonable. I'll downgrade this to LOW.

Let me finalize my findings. I think the most actionable findings are:

1. **LOW** - Backward compat concern (real but pragmatic)
2. **LOW** - Robustness of `Succeeded > 0` check
3. **LOW** - Missing test for manifest presence + failure marker
4. **LOW** - Naming suggestion

Actually, I want to think about one more thing: the diff says "a real summary record" never has the marker set to true. But what if a future bug causes `writeFailureSummary` to be called with a marker not set? Or what if a future code path writes a PoolSummary without the marker being explicit? The current invariant is fragile.

But this is more of a general code quality concern, not specific to this diff.

OK, let me write my final output.

Hmm, one more thing. Let me look at the `omitempty` for the `FailureMarker` field more carefully.

In Go, `omitempty` for bool means the field is omitted if it's false. So:
- Real summary: `FailureMarker: false` -> field omitted from JSON
- Failure summary: `FailureMarker: true` -> field present in JSON
- Old failure summary: `FailureMarker` not in struct -> field not in JSON

When reading:
- New reader reading new failure summary: sees `FailureMarker: true`
- New reader reading new real summary: sees `FailureMarker: false` (zero value, because field absent)
- New reader reading old failure summary: sees `FailureMarker: false` (zero value, because field absent)
- Old reader reading new failure summary: ignores unknown field, sees zero values (no marker concept)
- Old reader reading new real summary: ignores unknown field, sees zero values (no marker concept)

The problematic case is: New reader reading old failure summary. The reader can't tell it was a failure summary, so it doesn't force partial. This is the backward compat concern.

Mitigation: None straightforward, since you can't distinguish "old failure summary" from "new real summary" without the marker. The only mitigation is to not have any old failure summaries sitting around.

OK, final output. Let me think about whether the Severity ratings make sense.

For the backward compat issue, MEDIUM seems right. It's a real issue but not a regression - the old behavior is preserved. The new behavior is a strict improvement, but doesn't fix old files.

For the `Succeeded > 0` robustness, LOW seems right. It's a potential edge case that might not exist in practice.

For the missing test, LOW.

For the naming, LOW.

Let me also consider: is there a HIGH severity issue? I don't see one. The diff is well-crafted, well-tested, and the logic is correct for the main case.

OK, writing the output now.

Wait, I should consider if the new `FailureMarker` field could have a JSON tag issue. Looking at the field:
```go
FailureMarker bool `json:"failure_marker,omitempty"`
```

The tag is correct. Snake case is the convention used elsewhere in the struct (e.g., `total_findings`). Good.

Also, let me consider: are there any concurrency issues with this change? The change is read-only on the read side and only adds a field to the struct. No new concurrency primitives. The pre-existing race conditions (TOCTOU on file reads) are not made worse.

OK, final output:

1. MEDIUM: Backward compat for old failure summary files
2. LOW: `Succeeded > 0` check might miss non-failed/non-succeeded agents
3. LOW: Missing test for manifest + failure marker
4. LOW: Naming suggestion

Actually, I want to reconsider #1. The user's codebase is being modified, but the change doesn't break old files - it just doesn't fix the bug for them. Is this really MEDIUM?

In a typical upgrade scenario:
- User upgrades the code
- Existing reviews continue to work with the old behavior
- New reviews get the fix

This is the standard behavior for any fix. The user would only see the bug for old failure summaries, which are rare (write failures are rare). So this is LOW.

OK, downgrading #1 to LOW.

Let me also reconsider #2. The `Succeeded > 0` check assumes that `Succeeded` counts all non-failed agents. If it doesn't, the check could be wrong. But this depends on the `Result` model, which I don't have full visibility into.

Given the uncertainty, I'll mention this as a LOW with a note that the check should be verified against the full Result model.

OK, final list:

1. LOW: Backward compat (old failure summary files)
2. LOW: Robustness of `Succeeded > 0` check
3. LOW: Missing test case for manifest presence + failure marker
4. LOW: Naming suggestion (`FailureMarker` -> `WriteFailureMarker` or similar)
5. LOW: Implicit invariant not enforced (real summaries never set marker)

Actually, I want to consolidate. The naming and implicit invariant are minor style issues. The main concerns are the backward compat and the missing test.

Let me write the final output.

Hmm, let me look one more time at the test cases to make sure I'm not missing anything.

`TestReadManifestPartial_FailureMarkerForcesPartial`:
```go
sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true})
```

So `Total=2, Succeeded=2, Failed=0, Partial=false, FailureMarker=true`. With `omitempty`, JSON is:
```json
{"total":2,"succeeded":2,"failed":0,"partial":false,"failure_marker":true}
```

The test asserts `ReadManifestPartial(dir)` returns `true`. The new logic checks `ps.FailureMarker && ps.Succeeded > 0` -> `true && true` -> `true`. Test passes. Good.

`TestReadManifestPartial_FailureMarkerAllFailedStaysNonPartial`:
```go
PoolSummary{Total: 2, Succeeded: 0, Failed: 2, Partial: false, FailureMarker: true}
```

The test asserts `ReadManifestPartial(dir)` returns `false`. The new logic checks `ps.FailureMarker && ps.Succeeded > 0` -> `true && false` -> `false`. Falls through to `return ps.Partial` -> `false`. Test passes. Good.

`TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial`:
End-to-end test that calls `writeFailureSummary` directly. Verifies the on-disk summary has the marker and is correctly interpreted. Good.

OK, tests are solid.

Now, the `TestReadManifestPartial_SummaryIsSourceOfTruth` (pre-existing test) - does this test still pass? It tests that a summary with `partial: true` overrides a manifest with `partial: false`. The new code returns `ps.Partial` if marker logic doesn't trigger, so this test should still pass. Good.

`TestReadManifestPartial_FallsBackToManifestWhenNoSummary` (pre-existing) - tests fallback when no summary exists. The new code doesn't change this path. Good.

OK, no regressions for existing tests.

Let me also think about: what if the PoolSummary struct evolves and new fields are added? The current code uses `json.Unmarshal(data, &ps)` which is permissive. Good.

OK, final answer.

Actually, one more thing. The error message in `writeFailureSummary`:
```go
if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
    fmt.Fprintf(os.Stderr, "atcr: warning: writeFailureSummary: write summary: %v\n", err)
}
```

This swallows the error (just prints to stderr). Pre-existing behavior, but with the new marker, this might lead to a missing marker if the failure summary write itself fails. Then `ReadManifestPartial` wouldn't see the marker, and would use the old `ps.Partial` value. If the partial was false (all agents succeeded), we'd incorrectly report non-partial. This is a pre-existing issue, but with the new marker logic, it could lead to incorrect behavior.

Mitigation: In `ReadManifestPartial`, if the summary doesn't exist but the manifest is missing, default to true (more conservative). But this would be a behavior change for the existing flow.

Actually, the current behavior is: if both summary and manifest are missing, default to `false`. This is a pre-existing decision. The new code doesn't change this.

So if `writeFailureSummary` fails to write the summary, the system falls back to either:
- Manifest (if present) - might be stale
- Default `false` (if manifest is missing) - might be wrong

This is pre-existing. Not introduced by this diff.

OK, I'll mention this as a pre-existing concern that the new code doesn't fix.

Alright, final output time.

Let me think about what the most valuable findings are:

1. The diff is well-structured and well-tested.
2. The main concern is backward compat for old failure summary files.
3. The `Succeeded > 0` heuristic is a reasonable proxy but might miss edge cases.
4. The implicit invariant (real summaries never set marker) should be documented.
5. Missing test for manifest presence + failure marker.
6. Naming could be more specific.

I'll output these as TD_STREAM lines.

Let me also think about whether there are any issues with the way the test is structured...

In `TestReadManifestPartial_FailureMarkerForcesPartial`, the test creates a `ScaffoldReviewDir` first. Then creates the pool directory. Then writes the summary directly. This is good.

In `TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial`, the test creates the dir, then calls `writeFailureSummary` directly. This is good.

The tests don't test the full `WritePool` -> `writeFailureSummary` path. But the existing `TestWritePool_*` tests cover the `WritePool` path, and the new tests cover the `writeFailureSummary` and `ReadManifestPartial` paths. The end-to-end is partially covered by the new test.

OK, I think I've analyzed this thoroughly. Let me write the output.

One more thing: I should check if there are any issues with the new `FailureMarker` field's interaction with the existing struct.

The existing PoolSummary:
```go
type PoolSummary struct {
    Agents        []AgentSummary `json:"agents,omitempty"`
    Total         int            `json:"total"`
    Succeeded     int            `json:"succeeded"`
    Failed        int            `json:"failed"`
    Partial       bool           `json:"partial"`
    TotalFindings int            `json:"total_findings"`
    FailureMarker bool           `json:"failure_marker,omitempty"`
}
```

Note: `Agents` has `omitempty`, but `Total`, `Succeeded`, `Failed`, `Partial`, `TotalFindings` don't. This means even when zero, those fields are present in JSON. Only `Agents` and `FailureMarker` are omitempty.

Why? Probably because the writer of these fields always wants them present in the JSON for parsing. The marker is omitted when false to keep the JSON clean (and to ensure backward compat with older readers).

This is fine. No issues.

OK, final output. Let me also think about reviewer name. The persona is "code reviewer". Let me use a name like "code-reviewer" or "bruce" (from the example). I'll go with "reviewer" or my own identifier.

Looking at the example: `|bruce`. So the reviewer name is a single word. Let me use "reviewer" as my name.

Actually, I'll just use a descriptive name like "code-reviewer" or a specific name. Let me use "code-reviewer".

Hmm, the format seems to be a short identifier. Let me use "reviewer".

OK, final output:

```
LOW|internal/fanout/reviewdir.go:347|Backward compat: old failure summary files (pre-marker) are not detected|Add a one-time migration: if FailureMarker is absent but the summary was written by writeFailureSummary (e.g., presence of pool partial flag and missing manifest), force partial conservatively. Or document that old summaries need to be cleared.|maintainability|15|Reading an old summary.json (no FailureMarker field) yields zero value (false), so the new partial-fixing logic never fires; the original bug reoccurs for in-place upgrades.|reviewer

LOW|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic assumes Failed is the only non-success state|Use Total - Failed > 0 (or Total > 0 && Failed < Total) to catch any non-failed agent (skipped/errored), not just StatusOK|correctness|10|If agents have statuses other than StatusOK/StatusFailed that don't increment Succeeded, the new logic could miss incomplete writes (e.g., agent skipped mid-flush).|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: manifest.json present with stale partial:false alongside failure marker summary|Add TestReadManifestPartial_FailureMarkerOverridesStaleManifest to confirm the summary (with marker) still wins over a stale manifest. Current tests only cover the no-manifest case.|maintainability|15|TestReadManifestPartial_FailureMarkerForcesPartial scaffolds a dir but doesn't write a manifest, so the manifest-fallback branch isn't exercised for the failure-marker case.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Bename to WriteFailureMarker or BestEffortMarker to make the write-failure semantics explicit at call sites|naming|5|Reader scanning PoolSummary may not infer that this is specifically about WritePool I/O faults, not partial-failure-from-results.|reviewer

LOW|internal/fanout/artifacts.go:42|Implicit invariant (real summaries never set FailureMarker) is undocumented|Add a comment near the WritePool call site (or a constructor helper) stating that real-run PoolSummary instances must NOT set FailureMarker|correctness|5|If a future code path writes a PoolSummary with FailureMarker: false, omitempty strips it, and readers see the same JSON as a real run — which is OK, but the invariant is fragile.|reviewer
```

Hmm, I have 5 findings. Let me make sure they're all real and actionable.

1. Backward compat - real, but the fix is more of a "document this" than a code change. Maybe downplay to "consider documenting".

2. `Succeeded > 0` heuristic - real, but I don't know the full Result model. Speculative.

3. Missing test - real, easy to add.

4. Naming - subjective, low priority.

5. Implicit invariant - real, easy to document.

Let me refine and maybe drop the speculative ones. Actually, the `Succeeded > 0` check is worth mentioning because it's an assumption that could be wrong.

OK, let me also consider: the diff says "Zero successes is a genuine total failure with no surviving subset to protect." This is the comment explaining the heuristic. The author is aware of this design choice. So my finding #2 is essentially "the author made a design choice, but it might not be correct in all cases." This is a valid concern to raise, but I should be careful not to second-guess the design too much.

Actually, the check `Succeeded > 0` is fine IF `Succeeded` counts all non-failed agents. If it doesn't, the check is incomplete. The fix would be to use a more robust check. I'll mention this.

OK, final output. Let me also think about whether there are any issues with the existing `writeFailureSummary` function that the diff doesn't address...

The function ignores the error from `writeJSON`. This is a pre-existing issue. The diff doesn't make it worse.

OK, writing the final output now.

Actually, I realize I should also check: is there a potential issue with the diff having the `FailureMarker` field be the LAST field of the struct? In Go, JSON marshaling follows field order, but for unmarshaling, field order doesn't matter. So this is fine.

Also, the JSON tag uses snake_case (`failure_marker`), which is consistent with other fields. Good.

OK, final output. Let me condense the findings to be most valuable.

Final findings (prioritized):

1. **MEDIUM** (or LOW) - `Succeeded > 0` heuristic may not catch all non-failed agent statuses. Use `Total - Failed > 0` for robustness.

2. **LOW** - Backward compat for old failure summary files. Document or accept the limitation.

3. **LOW** - Missing test for manifest presence + failure marker.

4. **LOW** - Implicit invariant not enforced.

Let me also think about whether the comment in the diff is accurate:

> A failure-marker summary (a WritePool I/O fault, where only a subset of agents may have reached disk) is forced to partial whenever any agent succeeded, so a reconcile over the surviving artifacts never drops an unflushed agent.

This is accurate. The check `Succeeded > 0` means "at least one agent succeeded". The marker means "write failed". Together, they mean "write failed but some agents got their results through". Forcing partial ensures reconcile runs. Good.

OK, I'm overthinking this. Let me just write the output.

One more thought: the `omitempty` on `FailureMarker` could be a subtle bug source. If a future code path explicitly sets `FailureMarker: false` (e.g., a "this is definitely a real summary" assertion), the field would be omitted, and old readers (and the new logic) would see `false`. This is fine. But if the invariant is "real summaries set FailureMarker: false explicitly to be defensive", then `omitempty` defeats that.

I think the design is fine. The field is meant to be a "flag for failure summaries" - real summaries don't set it, and the omission keeps JSON clean. The trade-off is acceptable.

OK, final output.

Let me also think: should I mention anything about the order of fields in `PoolSummary`? The `FailureMarker` is added at the end. This is fine for backward compat (existing code reading the struct would just see the new field; existing JSON parsing would ignore the new field). No issue.

OK, writing the final output now. I'll be concise.

I'll output 4-5 findings in TD_STREAM format.

Let me reconsider the severity. For a real production codebase, what would be the impact?

1. `Succeeded > 0` heuristic: If there's a non-StatusOK non-StatusFailed status, the bug could still occur. But this requires the Result model to have such statuses. Without full visibility, I can't confirm. LOW to MEDIUM.

2. Backward compat: In-place upgrades would have old failure summaries that don't trigger the new logic. But this is rare and self-resolving (reconcile would refresh). LOW.

3. Missing test: Test coverage gap. LOW.

4. Implicit invariant: Documentation issue. LOW.

OK, LOW for most. Maybe MEDIUM for #1 if the heuristic is incorrect.

Final output:

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-StatusOK, non-StatusFailed agent statuses|Replace ps.Succeeded > 0 with ps.Total - ps.Failed > 0 (or ps.Total > 0 && ps.Failed < ps.Total) to catch any non-failed agent|correctness|10|If Result has statuses beyond StatusOK/StatusFailed (e.g., skipped, errored, timeout) that increment neither Succeeded nor Failed, an aborted write could leave surviving artifacts of those statuses unreconciled. The Total-Failed check is more robust to the full Result model.|reviewer

LOW|internal/fanout/reviewdir.go:347|Backward compat: pre-marker failure summary files (old summary.json) are not detected as best-effort|Document the limitation in the package comment, OR add a fallback that treats any summary with no Agents and Succeeded+Failed<0 (heuristic) as failure, OR add a "migrated" sentinel|maintainability|15|After upgrade, an old failure summary.json lacks FailureMarker, so ReadManifestPartial returns ps.Partial as before; the bug can reoccur for in-place upgrades that have stale failure summaries.|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: failure-marker summary with a manifest.json present (stale)|Add TestReadManifestPartial_FailureMarkerOverridesStaleManifest asserting that the failure-marker summary's forced-partial verdict still wins over a stale manifest|maintainability|15|TestReadManifestPartial_FailureMarkerForcesPartial scaffolds a dir without writing a manifest, so the manifest-fallback path isn't exercised for the failure-marker case.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Rename to WriteFailureMarker (or BestEffortMarker) to make the I/O-fault semantics explicit at every call site|naming|5|Readers scanning PoolSummary may infer this flags any failure, but it specifically marks a write-phase I/O fault (not a partial-failure-of-results).|reviewer
```

That's 4 findings. Let me check if there are any other issues I'm missing.

Actually, I want to reconsider the Severity for #1. The `Succeeded > 0` check is the core of the fix. If it's wrong, the fix doesn't fully work. So this could be HIGH if I'm confident the heuristic is wrong.

But I'm not 100% confident. The Result model might indeed only have StatusOK and StatusFailed. In that case, `Succeeded > 0` is equivalent to `Total - Failed > 0`. So the check is correct.

Without the full source, I'll keep it at MEDIUM with a note that the check should be verified.

OK, final output.

Hmm, looking at the diff more carefully one more time...

The diff modifies `ReadManifestPartial` in `reviewdir.go`. Let me re-read the new logic:

```go
if data, err := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", summaryFile)); err == nil {
    var ps PoolSummary
    if json.Unmarshal(data, &ps) == nil {
        if ps.FailureMarker && ps.Succeeded > 0 {
            return true
        }
        return ps.Partial
    }
}
```

Wait, what about a failure-marker summary with `Succeeded > 0` AND `Partial == true`? The new logic returns `true` (which is correct, since partial is true anyway). The marker just makes it more emphatic. No issue.

What about a failure-marker summary with `Succeeded == 0` AND `Failed == 0`? That's a weird state (no successes, no failures). The new logic doesn't force partial. Falls through to `return ps.Partial`. If partial is false, returns false. If partial is true, returns true. Edge case, but probably not real.

What about a failure-marker summary with `Succeeded > 0` AND `Failed == 0`? The new logic returns true. Correct.

What about a failure-marker summary with `Succeeded == 0` AND `Failed > 0`? The new logic doesn't force partial. Falls through to `return ps.Partial`. Correct (no surviving subset).

What about a failure-marker summary with `Succeeded > 0` AND `Failed > 0`? The new logic returns true. The `ps.Partial` would also be true (since both are > 0). So either way returns true. The marker doesn't change behavior here, but it's correct.

OK, the logic is correct for the cases I've considered.

The only edge case is: agent statuses other than StatusOK/StatusFailed. I'll mention this as MEDIUM.

OK, final output.

Let me also think about: the test file mentions `summaryFile` as a variable. Is it defined? It's used in the test, so it must be defined. Probably a package-level constant in the test file or imported. Not an issue.

OK, writing the final output now. I'll keep it to 4 findings.

Let me also double-check the line numbers for the issues:

- `internal/fanout/reviewdir.go:347` - the new logic. Let me check... The diff shows the new logic added after line 339 (the existing comment). The actual line number depends on the file. Based on the diff hunk, the new logic is around line 347. Close enough.

- `internal/fanout/artifacts.go:45` - the `FailureMarker` field. The diff shows it added at the end of the struct. Line 45 in the modified file.

- `internal/fanout/reviewdir_test.go` - the missing test. The diff shows tests added after `TestReadManifestPartial_SummaryIsSourceOfTruth`. Line 302+.

OK, line numbers are approximate but reasonable.

Final output:

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-StatusOK, non-StatusFailed agent statuses|Replace ps.Succeeded > 0 with ps.Total - ps.Failed > 0 to catch any non-failed agent regardless of status type|correctness|10|If Result.Status has values beyond StatusOK/StatusFailed (e.g., skipped, errored, timeout) that increment neither Succeeded nor Failed, an aborted write could leave artifacts of those statuses unreconciled. The Total-Failed check is more robust to the full Result model.|reviewer

LOW|internal/fanout/reviewdir.go:347|Backward compat: pre-marker failure summary files are not detected as best-effort|Document the upgrade limitation, OR add a heuristic fallback (e.g., treat any summary with Total>0, no Agents, and a recent mtime as best-effort), OR run a migration|maintainability|15|After upgrade, an old summary.json (created by pre-marker writeFailureSummary) reads FailureMarker=false, so the new partial-fixing logic never fires; the original bug reoccurs for in-place upgrades that have stale failure summaries.|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: failure-marker summary with a stale manifest.json present|Add a test asserting that the failure-marker summary's forced-partial verdict wins over a stale manifest (mirror TestReadManifestPartial_SummaryIsSourceOfTruth for the marker case)|maintainability|15|The new failure-marker tests scaffold a dir without writing a manifest.json, so the manifest-fallback path isn't exercised for the failure-marker case.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Rename to WriteFailureMarker (or BestEffortMarker) to make the I/O-fault semantics explicit at every call site|naming|5|Readers scanning PoolSummary may infer this flags any failure, but it specifically marks a write-phase I/O fault, not a partial-failure-of-results.|reviewer
```

That's 4 findings. Let me make sure each is actionable and specific.

1. MEDIUM - `Succeeded > 0` heuristic. Actionable: replace with `Total - Failed > 0`. Specific: line number, file, evidence.

2. LOW - Backward compat. Actionable: document or add heuristic. Specific: line number, file, evidence.

3. LOW - Missing test. Actionable: add test. Specific: file, evidence.

4. LOW - Naming. Actionable: rename. Specific: line number, file, evidence.

Good. All findings are actionable and specific.

Actually, let me reconsider #1. Is the check `Succeeded > 0` actually wrong? Without knowing the full Result model, I can't be 100% sure. The author chose this check deliberately, with a comment explaining why.

If the Result model only has StatusOK and StatusFailed, then `Succeeded > 0` is equivalent to "at least one agent didn't fail", which is the correct check.

If there are other statuses, the check might be wrong.

Given the uncertainty, I should probably frame this as "verify the check is robust to all agent statuses" rather than "the check is wrong".

Let me adjust:

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-binary agent statuses|Verify against the full Result.Status enum: if any non-StatusOK, non-StatusFailed status exists (skipped, errored, timeout), replace ps.Succeeded > 0 with ps.Total - ps.Failed > 0 to catch all non-failed agents|correctness|10|Comment claims "Zero successes is a genuine total failure" but this only holds if Succeeded counts every non-failed outcome. If Result has skipped/errored statuses, an aborted write with surviving skipped-agents would not be flagged.|reviewer
```

OK, that's better.

Final output (4 findings):

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-binary agent statuses|Verify against the full Result.Status enum: if any non-StatusOK, non-StatusFailed status exists (skipped, errored, timeout), replace ps.Succeeded > 0 with ps.Total - ps.Failed > 0 to catch all non-failed agents|correctness|10|Comment claims "Zero successes is a genuine total failure" but this only holds if Succeeded counts every non-failed outcome. If Result has skipped/errored statuses, an aborted write with surviving skipped-agents would not be flagged as partial.|reviewer

LOW|internal/fanout/reviewdir.go:347|Backward compat: pre-marker failure summary files are not detected as best-effort|Document the upgrade limitation in the package comment, OR run a one-time migration that re-tags old failure summaries|maintainability|15|After upgrade, an old summary.json (created by pre-marker writeFailureSummary) reads FailureMarker=false, so the new partial-fixing logic never fires for in-place upgrades with stale failure summaries.|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: failure-marker summary with a stale manifest.json present|Add a test asserting that the failure-marker summary's forced-partial verdict still wins over a stale manifest (mirror TestReadManifestPartial_SummaryIsSourceOfTruth for the marker case)|maintainability|15|The new failure-marker tests scaffold a dir without writing a manifest.json, so the manifest-fallback path isn't exercised for the failure-marker case.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Rename to WriteFailureMarker (or BestEffortMarker) to make the I/O-fault semantics explicit at every call site|naming|5|Readers scanning PoolSummary may infer this flags any failure, but it specifically marks a write-phase I/O fault, not a partial-failure-of-results.|reviewer
```

OK, this is my final output. Let me write it out.

Wait, I just want to double check the line numbers. Let me look at the diffs more carefully.

For `internal/fanout/reviewdir.go`:
- The new logic is added in the `ReadManifestPartial` function.
- The diff shows:
```
@@ -336,6 +339,15 @@ func ReadManifestPartial(reviewDir string) bool {
 	if data, err := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", summaryFile)); err == nil {
 		var ps PoolSummary
 		if json.Unmarshal(data, &ps) == nil {
+			// A failure-marker summary means WritePool aborted mid-flush, so the
+			// on-disk agent set may be a subset of what ran while ps.Partial can
+			// still be false (no agent FAILED). Force partial when at least one
+			// agent succeeded so a reconcile over the surviving artifacts cannot
+			// emit a non-partial verdict from an incomplete roster. Zero successes
+			// is a genuine total failure with no surviving subset to protect.
+			if ps.FailureMarker && ps.Succeeded > 0 {
+				return true
+			}
 			return ps.Partial
 		}
 	}
```

So the new logic is at line 339-348 (in the new file). The `if ps.FailureMarker && ps.Succeeded > 0 {` line is around line 347.

For `internal/fanout/artifacts.go`:
- The `FailureMarker` field is added at the end of the struct.
- The diff shows:
```
@@ -34,6 +34,15 @@ type PoolSummary struct {
 	Failed        int           `json:"failed"`
 	Partial       bool          `json:"partial"`
 	TotalFindings int           `json:"total_findings"`
+	// FailureMarker is true only when writeFailureSummary produced this record
+	...
+	FailureMarker bool `json:"failure_marker,omitempty"`
 }
```

The line number for the new field is around line 42-45 (in the new file).

OK, my line numbers are close enough.

For `internal/fanout/reviewdir_test.go`:
- The new tests are added after `TestReadManifestPartial_SummaryIsSourceOfTruth`.
- The diff shows:
```
@@ -302,6 +302,76 @@ func TestReadManifestPartial_SummaryIsSourceOfTruth(t *testing.T) {
 	assert.True(t, ReadManifestPartial(dir), "summary.json says partial:true; stale manifest must not override it")
 }
```

So the new tests start around line 305 in the new file. The missing test would be added somewhere around line 380+ (after the new tests).

For my "missing test" finding, I don't have a specific line number since the test doesn't exist. I'll just refer to the file.

OK, final output.

Let me also think about: is the new logic in `ReadManifestPartial` correct for the case where the summary is corrupt? If `json.Unmarshal` fails, the code falls back to the manifest. The marker logic doesn't apply. This is pre-existing behavior.

But what if the summary has the marker but is otherwise corrupted? Hmm, that would fail unmarshal and fall back. Probably not a real concern.

OK, final output. I'm going to write 4 findings.

Let me also think about whether to add a finding about test isolation. The new tests use `t.TempDir()` and `ScaffoldReviewDir`. This is good test isolation. No issue.

OK, final output.

Actually, one more thing. Let me look at the test for "TestReadManifestPartial_FailureMarkerForcesPartial" again:

```go
func TestReadManifestPartial_FailureMarkerForcesPartial(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_marker")
	require.NoError(t, err)

	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	// Every agent ran (Succeeded=2, Failed=0 -> Partial=false), but the write
	// aborted mid-flush, so the marker is set and the on-disk set may be a subset.
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 2, Failed: 0, Partial: false, FailureMarker: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), sum, 0o644))

	assert.True(t, ReadManifestPartial(dir), "a failure-marker summary with surviving successes must read as partial")
}
```

This is a good test. It:
- Creates a temp dir
- Scaffolds a review dir
- Creates the pool dir
- Writes a summary with marker
- Asserts ReadManifestPartial returns true

Good.

The end-to-end test `TestReadManifestPartial_WriteFailureAfterSuccessReadsPartial` is also good:
- Creates a temp dir
- Scaffolds a review dir
- Calls writeFailureSummary directly
- Verifies on-disk state
- Asserts ReadManifestPartial returns true

This covers the integration between writeFailureSummary and ReadManifestPartial. Good.

The only gap I see is: the manifest-fallback path with the marker. The current tests don't write a manifest, so they don't test the interaction between marker logic and manifest fallback.

But actually, looking at the code, if a summary exists, the manifest is not consulted. So the marker logic and manifest fallback don't interact. The summary always wins if it exists. So the missing test is just for completeness.

OK, my LOW finding for missing test is reasonable.

Final output. Writing now.

Hmm, one more consideration. Let me check the `omitempty` on `FailureMarker` once more.

With `omitempty` on bool, the field is omitted from JSON when it's false (zero value).

In the diff, `writeFailureSummary` sets `FailureMarker: true`. So the JSON includes `"failure_marker":true`. Good.

In `WritePool` (the real run path), `FailureMarker` is not set, so it defaults to `false`. With `omitempty`, the JSON doesn't include the field. Good.

When reading:
- New reader sees `"failure_marker":true` -> sets `FailureMarker=true`
- New reader sees missing field -> sets `FailureMarker=false` (zero value)
- New reader sees `"failure_marker":false` -> sets `FailureMarker=false` (would need explicit writing, but `omitempty` strips it, so this shouldn't happen)

So the only way `FailureMarker` is true on read is if it was explicitly written as true. Good.

OK, the design is sound.

Final output. Writing now.

One more thought: should I mention that the `omitempty` choice means the field is implicitly "true when present, false when absent"? This is a subtle design choice that could trip up future maintainers. But the comment in the code explains this. So not worth a finding.

OK, final output.

Let me write the TD_STREAM output now.

Actually, I realize I should also check: are there any issues with the way the diff is structured? The diff is split into 4 files:
1. `internal/fanout/artifacts.go` - struct change + writeFailureSummary change
2. `internal/fanout/artifacts_test.go` - test updates
3. `internal/fanout/reviewdir.go` - ReadManifestPartial change
4. `internal/fanout/reviewdir_test.go` - new tests

This is a clean separation. Good.

OK, final output. Writing now.

Let me think about whether there are HIGH severity issues...

HIGH would be: "must fix, breaks functionality, security risk, etc."

The diff doesn't break any existing functionality. It adds a new field and new logic. The new logic is well-tested. No security risks. No performance issues.

So no HIGH severity issues. My findings are LOW to MEDIUM.

OK, final output.

Wait, I want to reconsider the heuristic one more time. The author chose `Succeeded > 0` deliberately, with a comment explaining the rationale. The comment says:

> Zero successes is a genuine total failure with no surviving subset to protect.

This is the author's intent. The check is correct for the assumed Result model (StatusOK and StatusFailed only).

If the Result model has other statuses, the check might be wrong. But this is a design choice based on the current model. If the model changes, the check should be updated.

So my finding #1 is essentially "verify the design choice is correct given the full Result model". This is a reasonable code review concern, but it's not a bug per se.

I'll keep it at MEDIUM with a note that the check should be verified.

OK, final output.

Let me also think about: is there a more robust check that would handle all cases? Yes, `Total - Failed > 0` or `Failed < Total`. This would catch all non-failed agents regardless of their specific status.

But if `Succeeded` is the only counter for non-failed agents (i.e., `Succeeded` = `Total - Failed`), then `Succeeded > 0` is equivalent to `Total - Failed > 0`. The two checks are equivalent.

If `Succeeded` only counts `StatusOK` (and not other non-failed statuses), then `Succeeded > 0` is more restrictive than `Total - Failed > 0`. The more robust check is the latter.

So my recommendation is: use `Total - Failed > 0` (or `Failed < Total`) for robustness, unless `Succeeded` is known to count all non-failed agents.

OK, final output. Let me write it.

I'll write 4 findings:

1. MEDIUM: `Succeeded > 0` heuristic (correctness, robustness)
2. LOW: Backward compat (maintainability)
3. LOW: Missing test (maintainability)
4. LOW: Naming (naming, falls under maintainability)

Actually, naming is more of a "naming" category. Let me check the categories:
- error-handling
- security
- performance
- maintainability
- correctness

There's no "naming" category. Naming falls under maintainability. OK.

Final output:

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-binary agent statuses|Verify against the full Result.Status enum: if any non-StatusOK, non-StatusFailed status exists (skipped, errored, timeout), replace ps.Succeeded > 0 with ps.Failed < ps.Total to catch all non-failed agents|correctness|10|Comment claims "Zero successes is a genuine total failure" but this only holds if Succeeded counts every non-failed outcome. If Result has skipped/errored statuses, an aborted write with surviving skipped-agents would not be flagged.|reviewer

LOW|internal/fanout/reviewdir.go:347|Backward compat: pre-marker failure summary files are not detected as best-effort|Document the upgrade limitation in the package comment, OR run a one-time migration that re-tags old failure summaries|maintainability|15|After upgrade, an old summary.json (created by pre-marker writeFailureSummary) reads FailureMarker=false, so the new partial-fixing logic never fires for in-place upgrades with stale failure summaries.|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: failure-marker summary with a stale manifest.json present|Add a test asserting that the failure-marker summary's forced-partial verdict still wins over a stale manifest (mirror TestReadManifestPartial_SummaryIsSourceOfTruth for the marker case)|maintainability|15|The new failure-marker tests scaffold a dir without writing a manifest.json, so the manifest-fallback path isn't exercised for the failure-marker case.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Rename to WriteFailureMarker (or BestEffortMarker) to make the I/O-fault semantics explicit at every call site|maintainability|5|Readers scanning PoolSummary may infer this flags any failure, but it specifically marks a write-phase I/O fault, not a partial-failure-of-results.|reviewer
```

This is my final output. Let me write it.

Actually, the missing test finding - let me think about this more carefully. The test would be:
- Write a failure-marker summary
- Write a manifest.json with `partial: false`
- Assert that ReadManifestPartial returns true (summary wins)

But looking at the code:
```go
if data, err := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", summaryFile)); err == nil {
    var ps PoolSummary
    if json.Unmarshal(data, &ps) == nil {
        if ps.FailureMarker && ps.Succeeded > 0 {
            return true
        }
        return ps.Partial
    }
}
// fall through to manifest
```

If the summary exists and unmarshals successfully, the manifest is not consulted. So the test is trivial: it just verifies the existing behavior (summary wins). The marker logic is already covered by other tests.

So this "missing test" is actually low value. Maybe I should drop it or downplay it.

Let me drop it. So my final output has 3 findings.

Actually, on reflection, the missing test is still useful for documentation purposes. It shows that the marker doesn't break the "summary wins" invariant. But it's not strictly necessary.

I'll keep it as LOW for completeness.

OK, final output: 4 findings.

Let me write them out cleanly.

Actually, I realize one more thing. Let me check the test file for the existing `TestReadManifestPartial_SummaryIsSourceOfTruth` test:

This test (pre-existing) probably tests:
- Write a summary with `partial: true`
- Write a manifest with `partial: false`
- Assert ReadManifestPartial returns true (summary wins)

The diff mentions this test. The new failure-marker tests don't mirror this for the marker case. So adding a similar test for the marker case would be consistent.

But again, the marker logic is already covered by other tests. The "summary wins" behavior is already tested.

OK, this finding is marginal. I'll keep it as LOW.

Final output: 4 findings.

Writing now.
</think>

```
MEDIUM|internal/fanout/reviewdir.go:347|Succeeded > 0 heuristic may miss non-binary agent statuses|Verify against the full Result.Status enum: if any non-StatusOK, non-StatusFailed status exists (skipped, errored, timeout), replace `ps.Succeeded > 0` with `ps.Failed < ps.Total` to catch every non-failed agent regardless of status|correctness|10|Comment claims "Zero successes is a genuine total failure with no surviving subset to protect" but this only holds if Succeeded counts every non-failed outcome. If Result has skipped/errored/timeout statuses that increment neither Succeeded nor Failed, an aborted write whose survivors are all skipped would not be flagged as partial and reconcile would silently drop them. The Total-Failed form is robust to the full Result model.|reviewer

LOW|internal/fanout/reviewdir.go:347|Backward compat: pre-marker failure summary files are not detected as best-effort|Document the upgrade limitation in the package comment, OR add a one-time migration that re-tags old failure summaries (e.g., stamp FailureMarker on any summary written by a pre-marker binary at first read)|maintainability|15|After upgrade, an old summary.json (created by pre-marker writeFailureSummary) reads FailureMarker=false (zero value), so the new partial-fixing logic never fires; the original bug reoccurs for in-place upgrades that have stale failure summaries sitting on disk. There is no on-disk signal that distinguishes "old failure summary" from "new real summary" once the marker is absent.|reviewer

LOW|internal/fanout/reviewdir_test.go|Missing test: failure-marker summary co-existing with a stale manifest.json|Add a test that writes both a failure-marker summary (Partial:false) and a manifest.json (partial:false), asserting ReadManifestPartial returns true. Mirrors TestReadManifestPartial_SummaryIsSourceOfTruth for the marker path.|maintainability|15|The three new failure-marker tests scaffold a dir without writing a manifest.json, so the manifest-fallback branch isn't exercised for the failure-marker case. While the existing "summary wins" behavior is otherwise covered, having an explicit test guards against future refactors that re-order reads.|reviewer

LOW|internal/fanout/artifacts.go:45|Field name FailureMarker is generic; consider WriteFailureMarker|Rename to WriteFailureMarker (or BestEffortMarker) to make the I/O-fault semantics explicit at every call site and JSON consumer|maintainability|5|Readers scanning PoolSummary may infer FailureMarker flags any failure, but it specifically marks a write-phase I/O fault (not a partial-failure-of-results). The current name is correct but underspecified; WriteFailureMarker aligns the field name with the comment that already calls out "a WritePool I/O fault".|reviewer
```