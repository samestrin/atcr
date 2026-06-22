<think>
Let me analyze this diff carefully against the sprint plan scope.

The sprint plan is about adding a `ClusterID` field to `JSONFinding` to fix the over-suppression of co-located distinct clusters in `filterMergedClusters`.

In-scope files:
- `internal/debate/cluster.go` - modify `filterMergedClusters` and `applyOneClusterMerge`
- `internal/reconcile/emit.go` - add `ClusterID` field
- `internal/reconcile/merge.go` - carry/set the id (mentioned but not in diff)
- `internal/debate/cluster_test.go` - update tests
- `internal/reconcile/emit_test.go` - test the new field

Let me trace through the changes:

1. **`internal/debate/cluster.go`** - `filterMergedClusters` now takes a `clusterIdx` parameter and uses `ClusterID` instead of location keying.
2. **`internal/debate/cluster.go`** - `applyOneClusterMerge` stamps `merged.ClusterID = c.ID`
3. **`internal/debate/debate.go`** - passes `clusterIdx` to `filterMergedClusters`
4. **`internal/reconcile/emit.go`** - adds `ClusterID string` field with `json:"cluster_id,omitempty"`
5. Tests updated in `cluster_test.go` and `emit_test.go`

Let me look for issues:

### Issue 1: Function signature change to `filterMergedClusters`

The function signature changed from `filterMergedClusters(items, findings)` to `filterMergedClusters(items, findings, clusterIdx)`. This is a breaking change to the function's API. The call site in `debate.go` is updated, but are there any other callers? Let me think... The test file also calls this function and has been updated. Looking at the test changes, they pass `clusterIdx` correctly. So this seems fine.

### Issue 2: Legacy empty ClusterID behavior

The comment says: "a legacy ClusterMerged survivor written by a pre-6.2 debate carries no ClusterID, so it suppresses nothing here — the cluster is re-debated once (an idempotent no-op via applyOneClusterMerge's already-merged guard) and self-heals as soon as it is re-stamped."

Wait, this could be a concern. If a cluster was merged pre-6.2, and then we re-run debate:
- The legacy merged record has `ClusterMerged: true, ClusterID: ""` 
- `filterMergedClusters` won't suppress it (no ClusterID match)
- The cluster gets re-debated
- `applyOneClusterMerge` gets called again
- What happens? The comment says "already-merged guard" makes it idempotent.

But wait - if the cluster is re-debated, the gray-zone cluster would have its members... Let me think about this. The gray-zone cluster was formed by reconciliation, and it's been merged already. The reconcile step likely re-runs each time (or at least it's the case that the cluster exists). If we re-debate, we re-apply the merge. The "already-merged guard" is supposed to prevent re-merging, but if it works, this should be safe.

However, I should check: the comment says "A legacy ClusterMerged survivor... carries no ClusterID, so it suppresses nothing here — the cluster is re-debated once (an idempotent no-op via applyOneClusterMerge's already-merged guard) and self-heals as soon as it is re-stamped." This is documented behavior, not a bug per se.

### Issue 3: Cluster not in clusterIdx

Comment says: "An item whose cluster is not in clusterIdx (e.g. its representative problem drifted) likewise passes through and is re-debated rather than silently dropped."

This is an important fallback. The previous implementation would have suppressed such an item, but now it doesn't. Is this correct? Actually, yes - if the cluster isn't found, we don't know if it was merged, so we should re-debate.

But wait - is this behavior change potentially dangerous? Previously, a drifted problem meant a merged cluster's item would be filtered out (over-suppressed), but now it would be re-debated. This is actually the intended improvement. The cluster might be re-merged, but the "already-merged guard" should prevent issues.

### Issue 4: The test `TestRunDebate_GrayZoneMergeUnionsFindings` adds an assertion

```go
assert.Equal(t, "amb-1", f[0].ClusterID, "the survivor must carry the source cluster's stable ID (Epic 6.2 AC2)")
```

This assumes the cluster has ID "amb-1". Looking at the test, the cluster is likely created with ID "amb-1" in the setup. Let me check if this is reasonable - the test was modified to include this new assertion, so presumably the cluster ID is set up correctly. Without seeing the full test setup, I'll trust this.

### Issue 5: Potential nil/empty clusterIdx

What if `clusterIdx` is nil? The function uses `clusterIdx[FindingKey{...}]` - this would panic if nil. But `indexClusters` always returns a non-nil map (Go map types are non-nilable, but you can have a nil map which you can read from but not write to). The call site in `debate.go` shows `clusterIdx := indexClusters(grayClusters)`. If `grayClusters` is empty, `indexClusters` would return... let me think. Without seeing the implementation, I assume it returns a non-nil empty map. But this is a potential issue if the map is nil.

Actually, looking more carefully, in Go, you can read from a nil map (returns zero value) without panicking, so this is probably fine. The `ok` check handles the case.

### Issue 6: What about the ClusterID source for non-cluster-merge path?

The plan mentions `internal/reconcile/merge.go` - "carry/set the id through `MergeJSONFindings` if the merge path is the producer." Looking at the diff, there's no change to `merge.go`. The diff only changes `applyOneClusterMerge` in `cluster.go` to set `merged.ClusterID = c.ID`. 

Is this a problem? The plan says the merge path is the producer only IF the merge path is the producer. Since `MergeJSONFindings` is used by `applyOneClusterMerge` (line 209 in the diff: `merged := reconcile.MergeJSONFindings(group)`), and the diff sets `ClusterID` after this call, the `MergeJSONFindings` function itself doesn't need to set it.

But wait - does `MergeJSONFindings` need to preserve a pre-existing `ClusterID`? If the input findings in the group have `ClusterID` set (e.g., for already-merged clusters), the merge might need to preserve that. But actually, for the gray-zone cluster being merged, the members wouldn't have `ClusterID` set - they were individual findings. So this is probably fine.

### Issue 7: `applyOneClusterMerge` - Group replacement

Looking at the function (from the diff):
```go
merged := reconcile.MergeJSONFindings(group)
merged.File, merged.Line = c.File, c.Line
merged.ClusterMerged = true
merged.ClusterID = c.ID
```

This looks correct. The merged survivor is created, then stamped with the cluster's canonical location and ID.

### Issue 8: What if `c.ID` is empty?

If `c.ID` is empty (e.g., for clusters created before stable IDs), the survivor would be stamped with `ClusterID: ""`. This would be the same as not stamping. With the new filter logic, this would not suppress the item on a re-run. This could be considered a silent issue, but it's an edge case.

### Issue 9: Test for the `filterMergedClusters` signature

The diff updates the test calls to pass `clusterIdx`. Let me verify these changes are consistent.

In `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`:
```go
firstRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "c1a"},
}, clusterIdx)
```

This passes `clusterIdx` - good.

### Issue 10: The `Problem` field in the test

The test uses `grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")` - the signature is `grayCluster(id, file, line, problem1, severity1, reviewer1, problem2, severity2, reviewer2)`. The items use `Problem: "cluster one longer problem"` (the longer problem) which matches the cluster's longest member. The test then uses this to match via `FindingKey{File, Line, Problem}`. This is consistent.

### Issue 11: Test `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID` reRun assertion

```go
reRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-1"},
}, clusterIdx)
require.Len(t, reRun, 1, "only the merged cluster's item is suppressed; the co-located distinct cluster survives")
assert.Equal(t, "cluster two longer problem", reRun[0].Problem, "the surviving item must be cluster #2 (amb-2)")
```

This looks correct - cluster #1 is suppressed, cluster #2 survives.

### Issue 12: Potential issue with non-`AmbiguousCluster` items

What if a `DisagreementItem` has `Kind == KindGrayZone` but its `File`, `Line`, `Problem` don't match any cluster? It passes through. The comment acknowledges this: "An item whose cluster is not in clusterIdx (e.g. its representative problem drifted) likewise passes through and is re-debated rather than silently dropped."

Is this correct? Well, if the item exists, it was generated by some earlier step. The cluster might have been... I'm not sure if this is exactly correct, but the behavior is documented and seems reasonable.

### Issue 13: `applyOneClusterMerge` signature/declaration not fully shown

The diff only shows the relevant changes. I don't have the full function context, but the change is clear.

### Issue 14: Migration concern

If a user has a pre-6.2 findings.json with a merged cluster at location X, and they run 6.2 against it, the cluster gets re-debated (because no ClusterID to match). The "already-merged guard" should prevent re-merging. But what if the "already-merged guard" doesn't exist or doesn't work as expected?

The comment claims: "the cluster is re-debated once (an idempotent no-op via applyOneClusterMerge's already-merged guard) and self-heals as soon as it is re-stamped."

This relies on a pre-existing guard. If the guard doesn't exist, this would be a HIGH severity issue - the cluster could be double-merged or otherwise corrupted.

However, this is a documentation/comment issue more than a code issue, and it's outside the scope of the diff review.

### Issue 15: The test `TestRunDebate_GrayZoneMergeUnionsFindings` was NOT updated to use the new filter signature

Wait, actually looking again - `TestRunDebate_GrayZoneMergeUnionsFindings` is at line 85 in the test file, and the change is just adding an assertion. This is a different test from `filterMergedClusters`. This test uses the full debate flow, so it doesn't directly call `filterMergedClusters`.

### Issue 16: The `Problem` field in `DisagreementItem`

Looking at the new filter:
```go
if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok && mergedIDs[c.ID] {
```

This uses the item's `Problem` field to look up the cluster. The `indexClusters` function is mentioned to use `{File, Line, Problem}` as the key. This is consistent with the comment in the diff: "the same {File,Line,Problem} index runDebate uses to apply rulings."

But what if the item's `Problem` doesn't match the cluster's representative problem? The cluster has a "longest member" representative problem, and the item should have the same. This depends on the `indexClusters` and item generation logic. If the problem text drifts, the lookup fails (as acknowledged in the comment).

### Summary of findings:

1. **Potential clusterID="" issue**: If `c.ID` is empty (edge case), the survivor is stamped with empty ClusterID, which won't suppress. This is silent.

2. **Legacy ClusterMerged records don't suppress**: Pre-6.2 records re-debate. This relies on the "already-merged guard" in `applyOneClusterMerge`. If this guard doesn't exist, there's a HIGH severity issue.

Actually, I should look at this more carefully. The "already-merged guard" claim is a critical dependency. If it doesn't exist, the re-debate could cause a duplicate merge, which is data corruption. This would be HIGH severity.

But I'm reviewing the diff, not the full codebase. The claim in the comment is something I should flag as needing verification.

3. **`filterMergedClusters` signature change**: Any other callers? Let me think - the function is in `cluster.go` and the test file is updated. The `debate.go` call site is updated. Are there any other callers? Without full context, I'll assume there are none, but this is worth checking.

Let me also check the `TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress` test - it confirms the behavior. The behavior is intentional and tested.

Let me check if there are any subtle issues in the diff:

Looking at the test:
```go
c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
```

What does `grayCluster` look like? I'm assuming it's a test helper. The signature seems to be:
`(id, file, line, problem1, severity1, reviewer1, problem2, severity2, reviewer2)`

So c1 has two members:
- ("a.go", 10, "c1a", "MEDIUM", "alice")
- ("a.go", 10, "cluster one longer problem", "HIGH", "bob")

The longest problem is "cluster one longer problem" (25 chars vs 3 chars for "c1a"). The `clusterDisplayProblem` would return this.

The item has `Problem: "cluster one longer problem"` - matches the cluster's representative.

### Potential issues to flag:

1. The `applyOneClusterMerge` change: `merged.ClusterID = c.ID`. If `c.ID` is empty, this silently sets to empty. Could be a concern.

2. The legacy behavior: pre-6.2 merged records have no ClusterID, so they don't suppress. This is documented and tested, but relies on the "already-merged guard" claim.

3. The `filterMergedClusters` signature is a breaking API change. If exported, external consumers would break. But this is internal, so it's probably fine.

4. **Missing the `merge.go` change**: The sprint plan mentions `internal/reconcile/merge.go` should carry/set the id. The diff doesn't change this file. Is this a missing piece?

Looking at the plan more carefully:
> `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer.

The condition is "if the merge path is the producer." The current code shows `applyOneClusterMerge` calls `MergeJSONFindings` and then sets `ClusterID` separately. The merge function itself doesn't need to set it. So this might be a conditional that doesn't apply.

But wait - if `MergeJSONFindings` is also used to merge findings that ALREADY have a `ClusterID` (e.g., during reconcile), the merge might need to preserve or clear that. The diff doesn't address this.

Actually, looking at the use case: `MergeJSONFindings` is used to merge a group of findings. The members being merged shouldn't have `ClusterID` set (they're individual findings, not merged survivors). So this is probably fine.

5. **The `clusterIdx` parameter is `map[FindingKey]reconcile.AmbiguousCluster`** - this matches the existing `indexClusters` return type. Good.

Let me check the `FindingKey` type and the lookups:

```go
if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok && mergedIDs[c.ID] {
```

This looks correct.

### Final list of findings:

Looking at this carefully, I think the most important findings are:

1. **Empty ClusterID could cause issues**: If `c.ID` is empty in `applyOneClusterMerge`, the survivor is stamped with empty ClusterID. This could happen for clusters without stable IDs (edge case).

2. **Breaking function signature**: The change to `filterMergedClusters` adds a new parameter. This is a breaking change to the function's API, but it's internal so probably fine.

3. **Behavior change for items not in clusterIdx**: Previously, any item at a merged location was suppressed. Now, items not in clusterIdx pass through. This is documented but is a behavior change.

Actually, let me reconsider. The scope is well-defined. Let me focus on issues that are:
- In scope (in the files mentioned)
- Real issues (not just documentation)
- Within the sprint plan goals

Let me look at the diff one more time...

Actually, one thing I notice: In the test `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`, the first run test:
```go
firstRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "c1a"},
}, clusterIdx)
assert.Len(t, firstRun, 2, "first run (no ClusterMerged record) must not filter either co-located cluster")
```

The early return `if len(mergedIDs) == 0 { return items }` handles this. The test confirms behavior is correct.

Hmm, let me think about whether the "first run" with a non-ClusterMerged finding is correct. The finding has `File: "a.go", Line: 10, Problem: "c1a"` - no `ClusterMerged` flag. So the loop `if f.ClusterMerged && f.ClusterID != ""` doesn't add it to `mergedIDs`. `len(mergedIDs) == 0`, so `return items`. Both items pass through. Correct.

For the re-run:
```go
reRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-1"},
}, clusterIdx)
```

The finding has `ClusterMerged: true, ClusterID: "amb-1"`. It gets added to `mergedIDs`. Then for each item, we check if its cluster's ID is in `mergedIDs`. The first item has `Problem: "cluster one longer problem"` which matches c1 (amb-1). c1's ID is in mergedIDs, so it's suppressed. The second item has `Problem: "cluster two longer problem"` which matches c2 (amb-2). c2's ID is NOT in mergedIDs, so it's not suppressed. Result: 1 item, which is the second one. Correct.

### What about the "first run" finding with `File: "a.go", Line: 10, Problem: "c1a"`?

This is a bit weird - it's a finding with problem "c1a" but no `ClusterMerged` flag. Why include it? It seems to simulate a non-merged finding at the same location. This shouldn't affect the filter (no ClusterMerged). OK, this is fine.

### Edge case: What if an item's Problem is the SHORTEST problem, not the longest?

The item's `Problem` is the representative (longest) problem. This is set by the radar generation. But what if it's not? Then the lookup would fail and the item would pass through. The comment acknowledges this drift.

### One more thing to check: What about the `applyOneClusterMerge` function?

The function takes a cluster `c` and stamps `merged.ClusterID = c.ID`. But what if `c.ID` is empty? The function is called from somewhere that has the cluster. If the cluster doesn't have an ID, the survivor is stamped with an empty ID.

This is a potential issue. Let me check if `AmbiguousCluster.ID` is guaranteed to be non-empty.

Looking at the plan: "Source the id from `AmbiguousCluster.ID`; verify it is stable for the same cluster content." So this is a risk in the plan, but the diff assumes the ID is always present.

If `c.ID` can be empty, then:
- `merged.ClusterID = ""` 
- The merged survivor is written with `cluster_id` omitted (omitempty)
- On re-run, the filter doesn't suppress the cluster
- The cluster gets re-debated and re-merged (double-merge)

This could be a HIGH severity issue. But it depends on the implementation of `AmbiguousCluster.ID` and the test setup.

The sprint plan explicitly says: "ClusterID instability across reconcile runs | Source the id from `AmbiguousCluster.ID`; verify it is stable for the same cluster content."

So this risk is acknowledged in the plan. The diff doesn't add a check for empty ID. This could be a finding.

### Final list of findings:

Let me organize my findings:

1. **No guard for empty `c.ID` in `applyOneClusterMerge`**: If the cluster has no ID, the survivor is silently stamped with empty ClusterID, which is then omitted from JSON, and the cluster won't be filtered on re-run. This could lead to duplicate merges. **MEDIUM** (depends on ID generation, but worth checking).

2. **Legacy ClusterMerged records don't suppress, relying on unverified "already-merged guard"**: The comment claims re-debating is an idempotent no-op via `applyOneClusterMerge`'s already-merged guard. This guard's existence and correctness is not verified in the diff. If it doesn't exist or doesn't work, re-debate could cause data corruption. **MEDIUM** (documented behavior, but the guard is critical).

Actually, looking at this more carefully - the second point is about a pre-existing guard. I can't verify it from the diff. It's a documentation/comment claim, not a code change. So it's more of a risk assessment than a code issue.

3. **Function signature change**: `filterMergedClusters` adds a new parameter `clusterIdx`. This is a breaking change to the function's API. While the only caller (`debate.go`) is updated, if this function is used elsewhere, those callers will break. **LOW** (probably internal-only, but worth noting).

4. **Missing the `merge.go` change mentioned in the plan**: The plan says `internal/reconcile/merge.go` should be updated if it's a producer. The diff doesn't change this file. The implementation has `applyOneClusterMerge` call `MergeJSONFindings` and then set `ClusterID` separately, so the merge function itself doesn't need to change. But if `MergeJSONFindings` is used in other contexts, it might need to preserve or clear `ClusterID`. **LOW** (conditional in the plan, probably not needed).

Actually, let me re-read the plan:
> `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer.

"if the merge path is the producer" - this is conditional. The current implementation has `applyOneClusterMerge` set the ID after the call, so the merge function isn't a producer. This is fine.

5. **The `clusterIdx` parameter type is `map[FindingKey]reconcile.AmbiguousCluster`**: The function uses `clusterIdx[FindingKey{...}]` which is fine for reading, but if `clusterIdx` is nil, the lookup returns the zero value with `ok=false`. The `ok` check handles this. **OK**

Let me look one more time at the diff for any subtle issues...

Looking at the test `TestRunDebate_GrayZoneMergeUnionsFindings`:
```go
assert.Equal(t, "amb-1", f[0].ClusterID, "the survivor must carry the source cluster's stable ID (Epic 6.2 AC2)")
```

This requires the cluster in the test to have ID "amb-1". Looking at the test (from context I infer), the cluster is likely set up with this ID. If not, this assertion would fail.

Actually, I don't have the full test setup. The assertion is added, so presumably the test setup is correct.

### Let me also consider: Is there any issue with the `Problem` field in `DisagreementItem`?

The `DisagreementItem` has a `Problem` field. In the new filter, we use `FindingKey{File, Line, Problem}`. The `indexClusters` function uses the same key structure (assumed). So the lookup is consistent.

But what if the item's `Problem` is different from the cluster's representative? The comment says this could happen due to drift. In that case, the lookup fails and the item passes through. This is documented behavior.

### Issue: What if `Problem` in the item is empty?

If `Problem` is empty, the `FindingKey` would have an empty Problem field, which might not match any cluster (since clusters have non-empty representative problems). The item would pass through.

This is an edge case, probably not a real issue.

### Let me think about thread safety / concurrency

The function is `filterMergedClusters`. It uses local maps, so no concurrency issues. The call site is in `runDebate`. Single-threaded context.

### Performance

The new code does a map lookup per item. This is O(1), so no performance concern.

### Memory

The `mergedIDs` map only contains non-empty ClusterIDs. The `clusterIdx` is the existing index. No memory issues.

### Error handling

The function doesn't return errors. It returns a filtered list. No error handling needed.

### Summary

After thorough analysis, I have a few potential issues:

1. **MEDIUM**: `applyOneClusterMerge` doesn't guard against empty `c.ID`. If the cluster has no stable ID, the survivor gets an empty ClusterID, which is omitted from JSON (omitempty), and the cluster won't be filtered on re-run, leading to potential double-merging.

2. **LOW**: The "already-merged guard" claim in the comment for legacy records is unverified. If it doesn't exist, re-debate could cause data corruption.

3. **LOW**: The function signature change to `filterMergedClusters` is a breaking API change for any external callers (though it's likely internal-only).

Let me also check for any obvious test issues...

Looking at the test more carefully:
```go
c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
c2 := grayCluster("amb-2", "a.go", 10, "c2a", "MEDIUM", "carol", "cluster two longer problem", "HIGH", "dave")
```

Both clusters have the same file and line, different IDs. The `grayCluster` helper creates clusters with two members. The longest problem in c1 is "cluster one longer problem" and in c2 is "cluster two longer problem".

The test items have these as their `Problem` field, which should match the cluster representatives.

OK, I think my analysis is complete. Let me format the findings.

Actually, one more thing - I should check if the test for the legacy case (`TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress`) handles the case correctly. The legacy record has `ClusterMerged: true` but no `ClusterID`. The filter:
- Loops through findings: `f.ClusterMerged && f.ClusterID != ""` - condition is false, so not added to `mergedIDs`.
- `len(mergedIDs) == 0`, so return items unchanged.
- Test asserts `Len(out, 1)` - correct.

OK, the test is correct.

Let me think about the `runDebate` function in `debate.go`:

```go
clusterIdx := indexClusters(grayClusters)
df.Items = filterMergedClusters(df.Items, findings, clusterIdx)
```

This is the only caller. The change is correct.

One more concern: what if `grayClusters` is empty? `indexClusters` would return... let me think. If `indexClusters` is implemented to return a non-nil empty map, then `clusterIdx` is a non-nil empty map. `len(mergedIDs) == 0` would short-circuit and return items unchanged. OK.

If `indexClusters` returns nil, then `clusterIdx[FindingKey{...}]` would return zero value with `ok=false`. The `ok` check handles this. So even with a nil map, it works.

Actually, I realize I should also check the "first run" path more carefully:

```go
firstRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "c1a"},
}, clusterIdx)
```

The finding has no `ClusterMerged` flag, so the `if f.ClusterMerged && f.ClusterID != ""` condition is false. `mergedIDs` is empty. Return items unchanged. Test asserts `Len(firstRun, 2)` - correct.

OK, I'm confident in my analysis. Let me write the findings.

Wait, let me also look at this line in the diff:
```go
// A legacy ClusterMerged survivor written by a pre-6.2 debate carries no
// ClusterID, so it suppresses nothing here — the cluster is re-debated once (an
// idempotent no-op via applyOneClusterMerge's already-merged guard) and
// self-heals as soon as it is re-stamped.
```

This is a critical claim. The "already-merged guard" must exist in `applyOneClusterMerge` for this to be safe. Looking at the diff for `applyOneClusterMerge`:

```go
func applyOneClusterMerge(findings []reconcile.JSONFinding, c reconcile.AmbiguousCluster) []reconcile.JSONFinding {
    group := clusterMemberFindings(findings, c)
    if len(group) == 0 {
        return findings
    }
    merged := reconcile.MergeJSONFindings(group)
    merged.File, merged.Line = c.File, c.Line
    merged.ClusterMerged = true
    merged.ClusterID = c.ID
    // ... rest of function
}
```

I don't see a "already-merged guard" in this snippet. The function is called per cluster, and it doesn't check if the cluster has already been merged. So when re-debated:
- The cluster is re-debated
- `applyOneClusterMerge` is called again
- The group is found (the members still exist in `findings`)
- A new merged record is created
- The old merged record is replaced with the new one

Wait, but the old merged record is in `findings` (it's a JSONFinding). When we call `clusterMemberFindings(findings, c)`, would it include the old merged record? Probably not, because the merged record has `ClusterMerged: true` and the cluster members are the original individual findings (which might or might not be in the findings list).

This is getting complex. Let me think...

If the cluster was previously merged:
- `findings` has a `ClusterMerged: true` record (the merged survivor) and possibly the original individual findings (which were "absorbed" into the merge).
- `clusterMemberFindings(findings, c)` finds the group of members. If the original findings are gone (replaced by the merged survivor), the group is empty. The function returns early.
- If the original findings are still present, the group includes them, and re-merging happens.

Hmm, this depends on how `findings` is structured after the first merge. If reconcile removes the absorbed findings, the second call to `applyOneClusterMerge` would have an empty group and return early - no double-merge.

If reconcile keeps the absorbed findings, the second call would re-merge them - potential double-merge.

This is a significant concern, but it's about the existing behavior of `applyOneClusterMerge`, not something the diff changes. The diff just adds `ClusterID` stamping.

So the "already-merged guard" claim is about the existing behavior of the function, not the new code. If the guard doesn't exist, it's a pre-existing issue, not a new one introduced by the diff.

OK, let me not flag this as a finding, since it's about pre-existing behavior.

### Final findings:

1. **MEDIUM**: `applyOneClusterMerge` doesn't guard against empty `c.ID`. If a cluster has no stable ID (e.g., a cluster created before stable IDs were introduced), the survivor gets an empty ClusterID, which is omitted from JSON, and the cluster won't be filtered on re-run.

Actually, this might be a non-issue if `AmbiguousCluster.ID` is always populated for gray-zone clusters. Let me not flag this as a strong finding.

2. **LOW**: The function signature change to `filterMergedClusters` adds a new parameter. This is a breaking change for any other callers, but seems to be only called in `debate.go`.

3. **LOW**: The comment in `filterMergedClusters` claims "an idempotent no-op via applyOneClusterMerge's already-merged guard" - this guard is not visible in the diff, so it relies on pre-existing behavior. If the guard doesn't exist, re-debate could cause issues. But this is a pre-existing concern, not introduced by the diff.

Hmm, I should also check: Does the `Problem` field in `DisagreementItem` correspond to the cluster's representative problem? Let me think...

The `DisagreementItem` is generated for each gray-zone cluster. The `Problem` field should be the cluster's representative (longest member's problem). This is how `indexClusters` would have indexed it.

In the new filter, we use `FindingKey{File, Line, Problem}` to look up the cluster. If the item's `Problem` matches the cluster's representative, the lookup succeeds.

If the item's `Problem` is different (e.g., due to drift), the lookup fails and the item passes through.

This is the expected behavior per the comment. OK.

### Let me also check the test setup for the grayCluster helper

I assume `grayCluster` is a test helper that creates an `AmbiguousCluster` with given parameters. The signature suggests:
```go
func grayCluster(id, file string, line int, problem1, severity1, reviewer1, problem2, severity2, reviewer2 string) reconcile.AmbiguousCluster
```

So:
```go
c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
```

Creates a cluster with ID "amb-1", file "a.go", line 10, and two members:
- Member 1: problem "c1a", severity "MEDIUM", reviewer "alice"
- Member 2: problem "cluster one longer problem", severity "HIGH", reviewer "bob"

The cluster's representative problem (longest) would be "cluster one longer problem".

The test item has `Problem: "cluster one longer problem"` - matches c1's representative.

OK, this is consistent.

### One more thought: What if the cluster has only one member?

If the cluster has only one member, the representative problem is that member's problem. The item would have the same problem. Lookup succeeds. OK.

### Let me finalize my findings

After this thorough analysis, I have limited findings. The code is well-tested, the changes are focused, and the behavior is documented.

The most relevant findings are:
1. The function signature change to `filterMergedClusters` is a breaking change.
2. The `applyOneClusterMerge` change to stamp `ClusterID` is straightforward and correct.

I don't see any HIGH or MEDIUM severity issues in the diff itself. The code is well-designed and tested.

Let me check if there's anything I missed...

Oh wait, I should look at the `merge.go` file mentioned in the plan. The diff doesn't include changes to `merge.go`. Is this a missing piece?

Looking at the plan:
> `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer.

The condition "if the merge path is the producer" is important. The current implementation has `applyOneClusterMerge` set the ID after calling `MergeJSONFindings`. So `MergeJSONFindings` is NOT the producer. Therefore, no change is needed in `merge.go`.

This is fine.

### Are there any issues with the JSON marshaling/unmarshaling?

Looking at:
```go
ClusterID string `json:"cluster_id,omitempty"`
```

Standard omitempty. Empty string is omitted. Non-empty string is included. Round-trips correctly.

The test confirms this.

### Final answer

I think the diff is well-implemented. There are no HIGH severity issues. Possible findings:

1. **LOW**: The function signature change to `filterMergedClusters` is a breaking API change. While the only caller is updated, any other (future) caller would need to pass the new parameter.

2. **LOW**: The comment in `filterMergedClusters` claims
</think>
