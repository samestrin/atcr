The user wants me to review a diff based on a sprint plan. The sprint plan defines the scope: adding a `ClusterID` field to `JSONFinding`, stamping it in `applyOneClusterMerge`, filtering by it in `filterMergedClusters`, and updating tests.

Let me carefully examine the diff for issues within scope.

Files changed:
1. `internal/debate/cluster.go`
2. `internal/debate/cluster_test.go`
3. `internal/debate/debate.go`
4. `internal/reconcile/emit.go`
5. `internal/reconcile/emit_test.go`

Let me look at each change for correctness, security, performance, maintainability, error handling.

**internal/reconcile/emit.go:**
- Adds `ClusterID string` with `json:"cluster_id,omitempty"`. This looks correct. The comment explains it well.

**internal/debate/cluster.go:**
- `filterMergedClusters` signature changed to accept `clusterIdx`.
- It builds `mergedIDs` map from findings where `ClusterMerged && ClusterID != ""`.
- For each gray-zone item, it looks up `clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]` and checks if `mergedIDs[c.ID]`.
- If the item is gray-zone but NOT in `clusterIdx` (e.g., problem drifted), it passes through (ok is false, so it doesn't continue). This matches the comment: "An item whose cluster is not in clusterIdx ... likewise passes through and is re-debated rather than silently dropped."

Wait, what if the item IS in `clusterIdx`, but the cluster ID is empty? The code checks `mergedIDs[c.ID]`. If `c.ID` is empty, `mergedIDs[""]` is false (since we only populate mergedIDs with non-empty ClusterIDs). So it passes through. That seems fine.

What if there are multiple items for the same cluster? The code suppresses all items whose cluster ID is in `mergedIDs`. Since all items from the same cluster share the same cluster ID, they all get suppressed. This seems correct for idempotency.

But wait: what if a gray-zone item is NOT the representative problem? The `FindingKey` used in `clusterIdx` is indexed by `locationKey(cluster.File, cluster.Line)`... Let me check `indexClusters`.

Looking at `indexClusters` (not shown in diff but implied by context):
```go
func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconcile.AmbiguousCluster {
    ...
}
```
Wait, the diff doesn't show `indexClusters`. But from the test code, we can see:
```go
clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})
```
And items are matched via `clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]`.

In the old code, `indexClusters` used `locationKey` (File, Line). But now `FindingKey` includes `Problem`. Did `indexClusters` change?

Let me check if `indexClusters` is shown in the diff. The diff for `internal/debate/cluster.go` starts at line 43 and shows `indexClusters` is not modified (the hunk starts at 43 which is exactly where `indexClusters` might be, but the diff only shows `filterMergedClusters` and `applyOneClusterMerge`). 

Actually, looking at the diff header:
```
diff --git a/internal/debate/cluster.go b/internal/debate/cluster.go
index c2408a0..b5de99a 100644
--- a/internal/debate/cluster.go
+++ b/internal/debate/cluster.go
@@ -43,34 +43,37 @@ func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconci
```
Wait, the diff shows `indexClusters` signature but not its body? Let me re-read carefully.

Actually, the hunk starting at line 43 shows `func indexClusters` is there, but the diff only shows changes to `filterMergedClusters` which starts after it. The `indexClusters` function itself might not be in the diff because it wasn't changed, OR it's part of the context lines. Since the diff hunk starts at `@@ -43,34 +43,37 @@ func indexClusters...`, that means `indexClusters` is just context. The actual change starts later.

But wait: for `filterMergedClusters` to lookup by `FindingKey{File, it.File, Line: it.Line, Problem: it.Problem}`, `indexClusters` must have been updated to key by `{File, Line, Problem}` instead of just `{File, Line}`. 

Is `indexClusters` shown in the diff? Let me look again. The diff only shows `filterMergedClusters` and `applyOneClusterMerge`. It doesn't show `indexClusters` body. However, the sprint plan mentions this change touches `internal/debate/cluster.go` — `filterMergedClusters`, `applyOneClusterMerge`. It doesn't mention changing `indexClusters`. But logically, `indexClusters` MUST have been changed to key by `Problem` as well, or `filterMergedClusters` won't find the cluster.

Wait, looking at the old `filterMergedClusters`, it used `locationKey(it.File, it.Line)` which maps to the old `indexClusters` that probably returned `map[string]` or `map[FindingKey]` but only keyed by location. The new `filterMergedClusters` uses `FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}`. 

But the diff doesn't show `indexClusters` changing. Is it possible `indexClusters` already returned `map[FindingKey]reconcile.AmbiguousCluster` where `FindingKey` already had `Problem`? Let me think. The old code had:
```go
func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconciler...
```
Wait, the diff shows the signature in the context. The old signature in the diff context is:
`func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconci` (truncated).
The new signature would be the same. So `indexClusters` already returned `map[FindingKey]`.

The old `filterMergedClusters` didn't use `clusterIdx`, so `indexClusters` may have always used `FindingKey{File, Line, Problem}` but the old `filterMergedClusters` just didn't use it. Actually, looking at the old code, `runDebate` probably used `clusterIdx` for something else. But the point is, `indexClusters` is NOT in the diff, so we assume it's already correct or out of scope? Actually, if `indexClusters` wasn't changed but `filterMergedClusters` now relies on it in a new way, we need to verify it's correct.

Wait, let me re-read the scope constraint: "Only flag issues in files/areas directly related to the work items below. Do NOT flag issues in unrelated code that happens to appear in the diff (e.g., unrelated refactoring, formatting changes, or dependencies pulled in by the changes)."

But `indexClusters` is directly related if it's being used. However, since `indexClusters` itself isn't in the diff (its body isn't shown), I can't review its implementation. The signature is shown as context. I should focus only on the changed lines.

Now, looking at the changed `filterMergedClusters`:
```go
func filterMergedClusters(items []reconcile.DisagreementItem, findings []reconcile.JSONFinding, clusterIdx map[FindingKey]reconcile.AmbiguousCluster) []reconcile.DisagreementItem {
	mergedIDs := map[string]bool{}
	for _, f := range findings {
		if f.ClusterMerged && f.ClusterID != "" {
			mergedIDs[f.ClusterID] = true
		}
	}
	if len(mergedIDs) == 0 {
		return items
	}
	out := make([]reconcile.DisagreementItem, 0, len(items))
	for _, it := range items {
		if it.Kind == reconcile.KindGrayZone {
			if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok && mergedIDs[c.ID] {
				continue
			}
		}
		out = append(out, it)
	}
```

Wait, what if `it.Problem` is empty? The `FindingKey` would have empty problem. If `indexClusters` keys by the longest member problem (clusterDisplayProblem), then the item's problem needs to match that. In the test, they set `Problem` to the longest problem. So this relies on `it.Problem` being the cluster's representative problem. Is that always true? The comment says: "Each item resolves to its cluster (and thus its ID) via clusterIdx, the same {File,Line,Problem} index runDebate uses to apply rulings."

So `runDebate` must also use this key. This seems consistent.

Is there any issue with the logic?

Let's look at `applyOneClusterMerge`:
```go
merged := reconcile.MergeJSONFindings(group)
merged.File, merged.Line = c.File, c.Line
merged.ClusterMerged = true
merged.ClusterID = c.ID
```

This stamps `c.ID`. That's correct per AC2.

What about `MergeJSONFindings`? Does it preserve `ClusterID` if called elsewhere? The diff in `internal/reconcile/merge.go` is NOT shown. Wait, the sprint plan mentions `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer. But the diff doesn't include `merge.go`!

The sprint plan says:
- `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer.

But the diff provided does NOT show `internal/reconcile/merge.go`. This means either the diff is incomplete, or `MergeJSONFindings` already handles it? Actually, in `applyOneClusterMerge`, `merged` is the result of `reconcile.MergeJSONFindings(group)`. Then `merged.ClusterID = c.ID` is set AFTER. So `MergeJSONFindings` itself doesn't need to set `ClusterID`. It just needs to not drop it if it's already set? Wait, in this path, `merged` is a NEW struct returned by `MergeJSONFindings`. The `ClusterID` is set on the survivor AFTER the merge. That's fine.

But what if `MergeJSONFindings` is used elsewhere and needs to carry the `ClusterID`? The sprint plan mentions "carry/set the id through `MergeJSONFindings` if the merge path is the producer." But since `merge.go` is not in the diff, I cannot review it. And the scope constraint says only flag issues in files/areas directly related to the work items in the diff. Since `merge.go` is not in the diff, I shouldn't flag issues there unless it's clearly broken.

Wait, is there any issue in the test?

In `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`:
```go
c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
c2 := grayCluster("amb-2", "a.go", 10, "c2a", "MEDIUM", "carol", "cluster two longer problem", "HIGH", "dave")
clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})

items := []reconcile.DisagreementItem{
    {Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster one longer problem"},
    {Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster two longer problem"},
}

firstRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "c1a"},
}, clusterIdx)
```

Wait, the first run findings have `Problem: "c1a"` but the items have `Problem: "cluster one longer problem"`. This means the `FindingKey` for the finding is `("a.go", 10, "c1a")` but the item is `("a.go", 10, "cluster one longer problem")`. So `clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]` should match the cluster because `it.Problem` is the representative problem. But the finding `Problem: "c1a"` is different. That's fine because `filterMergedClusters` only looks at the findings to build `mergedIDs`, and looks at items to query `clusterIdx`. The findings' problem text doesn't matter for the lookup, only `ClusterID`.

Wait, but what if the item's problem text drifts and no longer matches the cluster's representative problem? Then `clusterIdx` lookup fails, the item passes through, and it's re-debated. This is explicitly documented as desired behavior: "An item whose cluster is not in clusterIdx (e.g. its representative problem drifted) likewise passes through and is re-debated rather than silently dropped." So that's fine.

Now, looking for real issues:

1. **Potential correctness issue in `filterMergedClusters`**: What if `clusterIdx` is nil or doesn't contain the item? The code handles it: `if c, ok := clusterIdx[...]; ok && mergedIDs[c.ID]`. If not ok, it doesn't suppress. Good.

2. **Potential issue: `clusterIdx` uses `Problem` as key, but the radar item's `Problem` might not match the cluster's representative problem.** This is acknowledged and handled. Not an issue.

3. **Potential issue in `TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress`**: The test creates a legacy record with `ClusterMerged: true` but no `ClusterID`. Then it asserts that `out` has 1 item. This is correct per the new logic: `f.ClusterMerged && f.ClusterID != ""` is false, so `mergedIDs` remains empty, so nothing is suppressed.

4. **Potential issue: `filterMergedClusters` returns early if `len(mergedIDs) == 0`.** This is an optimization but also means it doesn't allocate a new slice. The old code did the same. Fine.

5. **Potential issue: Performance.** No issues. Map lookups are O(1).

6. **Potential issue: Security.** No obvious security issues. No injection, no secrets.

7. **Potential issue: Error handling.** `filterMergedClusters` doesn't return errors. Fine.

8. **Potential issue: Maintainability.** The comment in `filterMergedClusters` is very long but accurate.

Wait, let me look closely at `applyOneClusterMerge` in `cluster.go`:
```go
merged := reconcile.MergeJSONFindings(group)
merged.File, merged.Line = c.File, c.Line
merged.ClusterMerged = true
merged.ClusterID = c.ID
```

What if `c.ID` is empty? Then `merged.ClusterID` is empty. Since `filterMergedClusters` only matches non-empty `ClusterID`, this merged record won't suppress anything. But `c.ID` is supposed to be stable and non-empty. Is there validation? The tests use "amb-1", "amb-2". In production, is `AmbiguousCluster.ID` always non-empty? The sprint plan says "Source the id from `AmbiguousCluster.ID`; verify it is stable for the same cluster content." It doesn't say it can be empty. But if it is empty, the behavior is: the cluster gets merged, but on re-run it won't be filtered out. However, `applyOneClusterMerge` might have an "already merged" guard elsewhere. The comment says "idempotent no-op via applyOneClusterMerge's already-merged guard". So even if `ClusterID` is empty and it re-debates, it would be a no-op. Still, it's a bit odd to stamp an empty ID, but not necessarily a bug.

Actually, looking at the test `TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress`, it expects that a record with empty ClusterID suppresses nothing. If a new merged survivor also has empty ClusterID, it would behave like a legacy record. But `AmbiguousCluster.ID` is likely always non-empty (content-addressed). So this is probably fine.

Is there a logic bug in `filterMergedClusters` when `clusterIdx` contains a cluster with an ID that matches a `mergedIDs` key, but the item is not gray-zone? The code only enters the suppression check for gray-zone items. Non-gray-zone items always pass through. Correct.

What about the `FindingKey` type? Is it defined with `Problem` field? The diff doesn't show it, but `indexClusters` signature suggests it uses `FindingKey`. If `FindingKey` didn't have a `Problem` field before, then this code wouldn't compile. But since this is a diff, we assume the codebase is consistent. The fact that `indexClusters` signature is `map[FindingKey]reconcile.AmbiguousCluster` suggests `FindingKey` exists. The new code constructs `FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}`. This implies `FindingKey` has a `Problem` field. This could be a pre-existing field or newly added. Since the diff doesn't show `FindingKey` definition, it's probably pre-existing or changed in a file not shown. But I can't verify. However, the scope constraint says I should only flag issues directly related to the work. Compilation issues due to missing fields in un-shown files are hard to judge. But wait, the diff DOESN'T show changes to `FindingKey`. If `FindingKey` previously didn't have `Problem`, then `cluster.go` would be broken. But the old `indexClusters` probably already used `FindingKey`. Let me think.

Actually, in the old code, `filterMergedClusters` didn't take `clusterIdx` and didn't use `FindingKey`. The old `indexClusters` is shown in the diff context:
`func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconciler...`
It was already returning `map[FindingKey]`. So `FindingKey` already existed. And `runDebate` already used it. It's likely `FindingKey` already had `Problem`. So no issue.

Let me think about potential subtle issues.

In `filterMergedClusters`, after the early return `if len(mergedIDs) == 0 { return items }`, if there are merged IDs, it creates a new slice `out`. It iterates items. For non-gray-zone items, it appends. For gray-zone items, if cluster not found or ID not merged, it appends. This is correct.

But wait: what if there are duplicate `ClusterID`s in `mergedIDs`? `mergedIDs` is a map, so duplicates don't matter.

What about findings that have `ClusterMerged: true` and `ClusterID: "amb-1"`, but there are multiple findings with the same `ClusterID`? That's fine, the map just stores true.

Now, is there a maintainability issue? The function now takes an additional parameter `clusterIdx`. This parameter must be built by the caller. In `debate.go`, it's called with `clusterIdx` which is built earlier. Good.

Wait, looking at `debate.go`:
```go
clusterIdx := indexClusters(grayClusters)
df.Items = filterMergedClusters(df.Items, findings, clusterIdx)
```
This is correct.

Is there any race condition? No.

Is there any issue with the JSON serialization in `emit.go`?
```go
ClusterID string `json:"cluster_id,omitempty"`
```
This is correct. Empty string is omitted.

In `emit_test.go`:
- `TestJSONFinding_ClusterIDOmittedWhenEmpty`: checks that a non-merged finding doesn't contain `cluster_id`. Correct.
- `TestJSONFinding_ClusterIDRoundTrips`: checks unmarshal and marshal. Correct.

Wait, in `TestJSONFinding_ClusterIDRoundTrips`, the raw JSON has `cluster_merged:true` and `cluster_id:"amb-1"`. After unmarshaling, it checks `f.ClusterID == "amb-1"` and `f.ClusterMerged == true`. Then it marshals and checks the output contains `"cluster_id":"amb-1"`. It doesn't check that `cluster_merged` is still present, but that's probably fine.

Now, is there any issue in the test for `TestRunDebate_GrayZoneMergeUnionsFindings`?
```go
assert.Equal(t, "amb-1", f[0].ClusterID, "the survivor must carry the source cluster's stable ID (Epic 6.2 AC2)")
```
This asserts that the survivor's `ClusterID` is "amb-1". This relies on the cluster generated by the test helper having ID "amb-1". This seems fine.

Let me reconsider if there are any issues I might have missed.

**Potential issue: The old behavior suppressed ALL co-located clusters if ANY was merged. The new behavior suppresses only the specific cluster. However, what about the "already merged" guard in `applyOneClusterMerge`?** The comment says: "A legacy ClusterMerged survivor written by a pre-6.2 debate carries no ClusterID, so it suppresses nothing here — the cluster is re-debated once (an idempotent no-op via applyOneClusterMerge's already-merged guard) and self-heals as soon as it is re-stamped."

This implies that `applyOneClusterMerge` has a guard that prevents double-merging. Since the diff doesn't show changes to that guard (the diff only shows the `merged.ClusterID = c.ID` line added), I assume the guard exists elsewhere in the function (not shown in the diff snippet). Actually, looking at the diff for `applyOneClusterMerge`, the hunk starts at `@@ -206,6 +209,9 @@`. The context lines before the change are not shown. The added lines are just:
```go
	// Stamp the source cluster's stable, content-addressed ID (Epic 6.2 AC2) so
	// filterMergedClusters can key idempotency on cluster identity, not File+Line.
	merged.ClusterID = c.ID
```
The guard is probably above this. So not an issue.

**Potential issue: `filterMergedClusters` might not handle nil `clusterIdx`**. If `clusterIdx` is nil, `clusterIdx[key]` returns zero value, `ok` is false. So it passes through. That's safe.

**Potential issue: `Problem` field uniqueness.** If two clusters have the same `File`, `Line`, and `Problem`, they would collide in `clusterIdx`. But the sprint plan says distinct clusters at the same File+Line. It doesn't say they have the same Problem. The test uses different problems. If they had the same problem, they wouldn't be distinct clusters? Actually, they could be distinct clusters with the same representative problem (unlikely but possible). If they collide in `clusterIdx`, the map would only contain one. But this is a pre-existing issue with `indexClusters`, not introduced by this diff. And the scope is to fix the over-suppression by adding ClusterID. I shouldn't flag pre-existing issues unrelated to the change.

Wait, but the change DOES rely on `clusterIdx` being keyed by `Problem`. If two clusters had the same `File`, `Line`, and `Problem`, `indexClusters` would lose one. Then `filterMergedClusters` would not suppress the lost one's items. But this seems like an edge case of the data model, not a bug introduced here.

Let me look for any issues in the diff that are clearly bugs.

One thing: In `filterMergedClusters`, if an item is `KindGrayZone`, and it's found in `clusterIdx`, and `c.ID` is in `mergedIDs`, it gets suppressed. But what if `c.ID` is empty? Then `mergedIDs[""]` is false (since we only add non-empty IDs to the map). So it passes through. This matches the legacy behavior for empty IDs.

Another thing: What if a gray-zone item's `Problem` doesn't match the cluster's representative problem? The lookup fails (`ok == false`), and it passes through. The comment explicitly says this is desired: "An item whose cluster is not in clusterIdx (e.g. its representative problem drifted) likewise passes through and is re-debated rather than silently dropped." This is correct and thoughtful.

Is there a performance issue with building `mergedIDs`? No.

Is there a correctness issue with `FindingKey`? The struct literal uses field names `File`, `Line`, `Problem`. If `FindingKey` doesn't have these exact field names, it won't compile. But since `indexClusters` already used `FindingKey`, it's presumably correct.

Wait, I should check if the import of `reconcile` is correct. No changes to imports shown.

What about `internal/debate/cluster_test.go`? The test `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID` uses `grayCluster` helper. Is `grayCluster` shown? No, but it's presumably existing.

I need to be careful: the user wants me to output ONLY pipe-delimited findings, one per line. If I find no issues, output nothing. If I find issues, output them.

After careful review, I don't see any clear issues. The code looks correct and well-tested. The comments are thorough. The logic handles edge cases (legacy empty ClusterID, problem drift, missing cluster index).

Wait, let me look one more time at `applyOneClusterMerge`. The diff shows:
```go
merged := reconcile.MergeJSONFindings(group)
merged.File, merged.Line = c.File, c.Line
merged.ClusterMerged = true
// Stamp ...
merged.ClusterID = c.ID
```
What if `merged` (the return value of `MergeJSONFindings`) is passed by value and setting fields on it is fine? `reconcile.MergeJSONFindings` likely returns a `JSONFinding` value (not a pointer). So `merged` is a copy. Setting fields on it modifies the local copy. That's fine. Then it's appended to `out`. Correct.

Actually, looking at the code below (not fully shown but implied):
```go
out := make([]reconcile.JSONFinding, 0, len(findings)-len(group)+1)
for i := range findings {
    // ...
}
```
Wait, the hunk for `applyOneClusterMerge` shows:
```go
	out := make([]reconcile.JSONFinding, 0, len(findings)-len(group)+1)
	for i := range findings {
```
This is the old code. The `merged` record needs to be appended to `out` somewhere. It's probably appended after the loop. The diff doesn't show that part, but it's presumably unchanged. So `merged` having `ClusterID` set is fine.

Let me reconsider if there could be a subtle bug with the `clusterIdx` lookup.

In `runDebate`, `clusterIdx` is built from `grayClusters`. Then `filterMergedClusters` is called. Later, `applyOneClusterMerge` uses `clusterIdx` or something else? The diff doesn't show that. But `clusterIdx` in `filterMergedClusters` is used to map items to clusters. 

What if a gray-zone item is in `df.Items` but its cluster is NOT in `grayClusters`? That seems impossible because gray-zone items come from gray clusters. But if it did happen, it passes through.

Actually, there is one potential issue: **The signature of `filterMergedClusters` changed to accept a third parameter, but is there any other caller besides `debate.go`?** If there's another caller in a file not shown in the diff, the codebase would fail to compile. But the diff only shows `debate.go` being updated. Are there other callers? The sprint plan doesn't mention any. The old signature was:
`func filterMergedClusters(items []reconcile.DisagreementItem, findings []reconcile.JSONFinding) []reconcile.DisagreementItem`
New signature adds `clusterIdx`. The diff shows `debate.go` updated. If there are other callers in other files (e.g., tests directly calling it), they would be broken. But the diff shows `cluster_test.go` updated. Are there other test files or callers? Not shown. But since I only see the diff, and the diff doesn't show other broken callers, I cannot know. However, if there were other callers within the diff, they would be shown. Since they're not shown, either they don't exist, or they are outside the diff (which means the diff is incomplete, but that's not something I can flag from the diff itself). I should not flag compilation errors that I can't verify.

Wait, but `filterMergedClusters` is unexported (lowercase `f`). So it's only callable within `internal/debate`. The diff shows all changes in `internal/debate` are in `cluster.go`, `cluster_test.go`, and `debate.go`. If there were another caller in `debate_test.go` or another file in the same package, it would need to be updated. But the diff doesn't show it. This could mean there are no other callers, or the diff missed it. I can't flag this as a confirmed issue.

Let me think if there's any correctness issue with the test.

In `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`:
```go
reRun := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-1"},
}, clusterIdx)
require.Len(t, reRun, 1, "only the merged cluster's item is suppressed; the co-located distinct cluster survives")
assert.Equal(t, "cluster two longer problem", reRun[0].Problem, "the surviving item must be cluster #2 (amb-2)")
```
This looks correct.

In `TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress`:
```go
out := filterMergedClusters(items, []reconcile.JSONFinding{
    {File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true},
}, clusterIdx)
```
Note: no `ClusterID` field. This means `ClusterID` defaults to empty string. In the function: `if f.ClusterMerged && f.ClusterID != ""`. Since `ClusterID` is empty, this record is not added to `mergedIDs`. So `mergedIDs` is empty. Then `len(mergedIDs) == 0`, so it returns `items`. So `out` is `items`, length 1. Correct.

Is there a potential issue with `ClusterID` being set on `JSONFinding` but not being copied by `MergeJSONFindings` when merging multiple findings? The sprint plan says:
- `internal/reconcile/merge.go` — carry/set the id through `MergeJSONFindings` if the merge path is the producer.

But `merge.go` is not in the diff. In `applyOneClusterMerge`, the code explicitly sets `merged.ClusterID = c.ID` AFTER `MergeJSONFindings`. So `MergeJSONFindings` doesn't need to set it. However, what if `MergeJSONFindings` is a generic merge function that zeros out unknown fields? No, it probably just merges specific fields. Since `ClusterID` is a new field, if `MergeJSONFindings` doesn't explicitly handle it, it might be dropped if it were present in inputs. But in this path, the inputs are the group members, which don't have `ClusterID` set. The survivor gets it set afterward. So it's fine.

But what if `MergeJSONFindings` is called in `applyOneClusterMerge` and it copies fields from the first finding or aggregates them? Since `ClusterID` is new, if `MergeJSONFindings` doesn't know about it, it will just be empty in the returned struct, which is then overwritten. Fine.

Wait, what if there's another path where `MergeJSONFindings` is called on findings that already have `ClusterID` (e.g., re-merging already merged findings)? Then `ClusterID` might be lost if `merge.go` doesn't handle it. But `merge.go` is not in the diff, so I can't verify. The sprint plan says it should be updated. If it's not updated, that's an issue. But since it's not in the diff, I don't know if it was updated or if it's even relevant. The diff is what I must review. The user says "Review the following diff". If `merge.go` was supposed to be changed but wasn't, that's not visible in the diff. I shouldn't speculate on files not in the diff.

Actually, looking carefully at the diff, there is NO `merge.go` file. The sprint plan explicitly says `internal/reconcile/merge.go` should be touched: "carry/set the id through `MergeJSONFindings` if the merge path is the producer." But the diff doesn't show it. This might mean:
1. The change to `merge.go` was done in a separate commit not shown here.
2. The change wasn't needed because `MergeJSONFindings` doesn't interfere.
3. The diff is incomplete.

But my job is to review the diff provided. I should not flag things that are not in the diff unless they are directly caused by the diff and would break compilation. Since I can't see `merge.go`, I can't say the diff breaks it.

Let me look for any issues that are definitely present.

**One potential issue: In `TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID`, the first run passes findings with `Problem: "c1a"` but no `ClusterMerged` flag. The function builds `mergedIDs` only from findings with `ClusterMerged: true`. So `mergedIDs` is empty. It returns all items. This is correct.**

**Another potential issue: The test doesn't test when `clusterIdx` is nil or item not found.** But that's not required.

**Another potential issue: In `filterMergedClusters`, the return value when `len(mergedIDs) == 0` is `items` (the original slice). When `len(mergedIDs) > 0`, it returns `out`, a new slice. This is fine.**

Wait, I just noticed something. In `filterMergedClusters`:
```go
for _, it := range items {
    if it.Kind == reconcile.KindGrayZone {
        if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok && mergedIDs[c.ID] {
            continue
        }
    }
    out = append(out, it)
}
```

What if `it.Kind == reconcile.KindGrayZone` and `ok == true` and `mergedIDs[c.ID] == true`, so it continues (is suppressed). But what if there are multiple items for the same merged cluster? They all get suppressed. That's correct.

What if `it.Problem` is different from the cluster representative problem? `ok` is false, item passes through. But then when it's re-debated, `runDebate` needs to map it to a cluster to apply the ruling. If `runDebate` uses the same `clusterIdx`, it might not find the cluster either! But the comment says "the same {File,Line,Problem} index runDebate uses to apply rulings." So if it doesn't find it in `filterMergedClusters`, `runDebate` also won't find it. Then it won't be merged? Actually, the comment says it will be "re-debated rather than silently dropped." This implies `runDebate` can still process it even if not in `clusterIdx`? Or maybe it means it will enter the debate and fail to apply, but at least it's not silently dropped from the radar. The comment seems to indicate a conscious design choice. So not a bug.

Alright, I've looked thoroughly. I genuinely cannot find any issues in this diff. It's a well-structured, clean change with good test coverage and backward compatibility handling.

Wait... one more thing. Is there an issue with the `FindingKey` struct literal in `cluster.go`? It uses named fields. If `FindingKey` is defined in the same package or imported, and if it has unexported fields, this wouldn't compile. But `File`, `Line`, `Problem` are capitalized in the literal, so they are exported fields. This is fine.

Let me check if there are any issues with JSON tag in `emit.go`:
```go
ClusterID string `json:"cluster_id,omitempty"`
```
The field is named `ClusterID`, tag is `cluster_id`. Correct.

Are there any issues with test assertions?
`assert.Equal(t, "amb-1", f[0].ClusterID, ...)` - if `f[0].ClusterID` is empty, this fails. Good.

`assert.NotContains(t, string(data), "cluster_id", ...)` - checks omission. Good.

`assert.Contains(t, string(out), `"cluster_id":"amb-1"`)` - checks presence. Good.

Everything looks solid.

Given the strict instruction: "If you find no issues, output nothing at all."

I should output nothing.

But wait, I need to be absolutely sure. Let me look at the diff ONE more time.

In `cluster.go`:
```go
-	mergedLocs := map[string]bool{}
+	mergedIDs := map[string]bool{}
 	for _, f := range findings {
-		if f.ClusterMerged {
-			mergedLocs[locationKey(f.File, f.Line)] = true
+		if f.ClusterMerged && f.ClusterID != "" {
+			mergedIDs[f.ClusterID] = true
 		}
 	}
-	if len(mergedLocs) == 0 {
+	if len(mergedIDs) == 0 {
 		return items
 	}
 	out := make([]reconcile.DisagreementItem, 0, len(items))
 	for _, it := range items {
-		if it.Kind == reconcile.KindGrayZone && mergedLocs[locationKey(it.File, it.Line)] {
-			continue
+		if it.Kind == reconcile.KindGrayZone {
+			if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok && mergedIDs[c.ID] {
+				continue
+			}
