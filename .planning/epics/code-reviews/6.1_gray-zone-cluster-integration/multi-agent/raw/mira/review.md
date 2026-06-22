<think>
Let me analyze this diff carefully. The sprint plan is about Epic 6.1 - inline application of judge gray-zone cluster rulings. The in-scope files are:
- `internal/debate/debate.go` 
- `internal/debate/cluster.go` (new)
- `internal/debate/cluster_test.go` (new)
- `internal/reconcile/emit.go` (JSONFinding struct)
- `internal/reconcile/merge.go` (MergeJSONFindings)
- `internal/reconcile/cluster_merge_test.go` (new)

Let me review the code carefully for issues:

1. **`internal/debate/cluster.go`** - New file
   - `locationKey` function uses `"\x00"` as separator - could be a security/maintainability concern if filenames contain null bytes (unlikely on most systems but technically possible)
   - `indexClusters` - looks fine
   - `filterMergedClusters` - if `findings` is nil, the loop just doesn't execute, which is fine. But there's no nil check.
   - `applyOneClusterMerge` - The exact match uses `Problem` field for matching. If multiple findings.json records have the same File+Line+Problem (which shouldn't happen normally), multiple exact hits would be recorded as matched. But the apply path only collapses them once, so could lose data.
   - In `applyOneClusterMerge`, when `firstIdx < 0` is checked, it could be that `len(matched) >= 2` but the `i == firstIdx` check uses first match... wait, this is correct.
   - The condition `if exactHits == 0 || len(matched) < 2` is checked AFTER the pass 2. The exactHits counts exact matches. But what if pass 2 alone contributes all matches? The `exactHits == 0` check prevents that.
   - However, the `len(matched) < 2` check - what about a cluster with 3+ members? The plan says "Union ALL cluster members". A 3-member cluster could have 1 exact match + 2 drift matches. After pass 2, matched has 3 entries, which is correct. But what if a member location has only one match in pass 2, but it's actually NOT the drifted member (it's an unrelated co-located finding)? The "exactly one unmatched record" rule helps but isn't foolproof. The comment says "an unrelated co-located finding is never absorbed", but actually if a member's record was deleted and an unrelated finding happens to be at the same line, the unrelated finding would be absorbed.

2. **`internal/debate/debate.go`** - Modified
   - `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` - Error is silently swallowed! This is an error-handling issue. The comment says "A missing/corrupt ambiguous.json degrades to no gray-zone work" but a CORRUPT file should be reported, not silently ignored.
   - The cluster index is built unconditionally even if no gray-zone items exist. This is a minor performance issue.
   - `filterMergedClusters` is called even when there are no gray-zone items. Performance issue.
   - In the switch statement, the `default` case handles single-finding items. The condition `it.Kind == reconcile.KindGrayZone` is correct, but the switch could be simplified.

3. **`internal/reconcile/emit.go`** - Added `ClusterMerged` field
   - The comment says "It is never set by any reconcile-time path" - this is a code comment, not an actual constraint. There's no enforcement. This is a maintainability concern but not a real issue.
   - `omitempty` is correct for byte-stability.

4. **`internal/reconcile/merge.go`** - New `MergeJSONFindings` and helpers
   - The `sort` import is not shown in the diff, but `sort.Strings` is used. Need to verify it's imported.
   - `joinEvidence` is O(n²) due to string concatenation in a loop. For small groups this is fine.
   - `mergeVerification` - The `out.Skeptic = strings.Join(skeptics, ",")` uses comma-joined, but the test expects "sk1,sk2". Consistent.
   - In `mergeVerification`, when `chosen` is set but `v.Verdict` has lower rank, the previous chosen is kept (correct precedence).
   - The VerdictUnverifiable constant is referenced - is it defined? In a test it uses `VerdictRefuted` and `VerdictConfirmed`.

5. **Error handling issue**: `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` swallows ALL errors. A corrupt ambiguous.json should at least be logged, not silently ignored.

6. **Correctness issue**: The drift recovery pass can absorb an unrelated finding if a member's record was deleted/replaced. The comment claims "an unrelated co-located finding is never absorbed" but this isn't strictly true - if a member's location now has a different unrelated record, and that location has exactly one record after exact pass, it will be matched. The exact-anchor check at least requires one member to match, but the drifted members could still be wrong.

7. **Performance issue**: `filterMergedClusters` is called before knowing if there are any gray-zone items. The function builds a set of merged locations even when no findings have ClusterMerged. The early return handles that case.

8. **Maintainability issue**: The `locationKey` uses `"\x00"` as separator without documenting the choice. Could collide with files containing null bytes (impossible on most systems but odd choice).

9. **Test issue**: The test `TestRunDebate_GrayZoneMergeIsIdempotent` asserts `res2.Selected == 0` but the comment says "the cluster is already applied — it must be filtered out of selection." This is the AC4 idempotency. Looking at the code, the filter is applied to `df.Items` which presumably feeds into selection. If the filter is working, this should pass.

10. **Test missing**: The drift recovery case where an unrelated co-located finding IS absorbed (i.e., negative test) is not tested explicitly. The `TestRunDebate_GrayZoneMergeRequiresExactAnchor` covers the "no exact anchor" case, but what about "1 exact + 1 unrelated drift"?

11. **Memory leak/performance**: The `applyOneClusterMerge` function uses an O(n*m) lookup. For typical n, m this is fine.

12. **Correctness**: In `applyOneClusterMerge`, after drift recovery, `len(matched) < 2` check uses `len(matched)` but exactHits check is `exactHits == 0`. So 1 exact + 1 drift = 2 matched, allowed. 0 exact + 2 drift = rejected. Good.

13. **Correctness issue**: The `out := make([]reconcile.JSONFinding, 0, len(findings)-len(group)+1)` reserves the exact final size, but if `group` had items added via drift recovery, this is correct because `group` is built from `matched` which has the same size as the final collapsed count.

14. **Edge case**: If `c.Findings` is empty (malformed cluster), `memberExact` and `memberLocs` are both empty. Pass 1 finds nothing, pass 2 finds nothing. `exactHits == 0`, returns false. OK, that's safe.

15. **Edge case**: If a cluster has only 1 finding, `len(matched) < 2` will reject. OK.

16. **Real issue - error suppression**: `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` silently swallows errors. A malformed ambiguous.json should be reported, even if the work is degraded. This is a clear error-handling issue.

17. **Real issue - verification Skeptic separator inconsistency**: In `mergeVerification`, skeptics are joined by `","`. In the existing code elsewhere, what separator is used? Looking at the test, "sk1,sk2" is the expected. This should be consistent with how Skeptic is serialized elsewhere in the codebase, but since this is new code it's fine.

18. **Potential issue**: The new `MergeJSONFindings` function is added to `merge.go` but the doc comment says "A zero- or one-member group is returned as-is (the apply path never unions fewer than two records, but the helper stays total)." The `if len(group) == 0` returns `JSONFinding{}` (zero-value), which might not be what the apply path expects. But the apply path checks `len(matched) < 2` so it never calls Merge with 0 or 1 members.

19. **Possible issue**: In `applyOneClusterMerge`, when `findings[i].ClusterMerged` is true for a matched record, the function returns `(findings, false)`. But the `filterMergedClusters` should have already removed these items from selection. So this is a defensive check. Good.

20. **Atomicity**: The diff adds inline application of merges to findings.json. The comment says it's done as part of the atomic group (debate.json + findings.json + manifest.json). This is consistent with the existing pattern.

21. **Concern**: The `applyClusterMerges` is called AFTER `applyRulings` (single-finding). What if a single-finding ruling affects a record that is also part of a cluster merge? The single-finding ruling at it.File, it.Line, it.Problem - if that finding is also a member of a gray-zone cluster, the cluster merge would apply first, then the single-finding ruling would be on a record that no longer exists. The item selection in `runDebate` should have already filtered out items whose findings are already merged (via `filterMergedClusters`). So this shouldn't happen in practice.

Let me prioritize the actual issues I can flag:

**HIGH/MEDIUM severity issues:**

1. **Error swallowing** at `debate.go:111` (the `grayClusters, _ := ...` line) - silently ignores ALL errors including corruption. Should at least log.

2. **Correctness issue** with `locationKey` using `"\x00"` separator - collisions theoretically possible if filename contains null bytes. But this is not realistic on POSIX/Windows filesystems. LOW.

3. **Unrelated co-located finding absorbed in drift recovery** - if a member's record is replaced with an unrelated finding at the same line, the drift recovery would absorb the unrelated finding. The "exactly one unmatched record" heuristic helps, but with the "exactly two" case (drifted member + unrelated at same line, but different problem), it would be ambiguous. The "more than one is ambiguous" rule helps. But what about "1 drifted member + 1 unrelated at the same line" - this is the "same-line drift" case tested, and there it's intended to absorb the drifted member. Hmm, actually the test correctly handles this: a.go:10 has TWO records (alpha and "longest merged problem text"). The cluster member B is supposed to be at a.go:10 but with "beta problem text" which doesn't match. The lone unmatched record at a.go:10 is "longest merged problem text" - this IS the drifted member B. So the test is correct. But in a scenario where member B's record was deleted and an unrelated finding now sits at a.go:10, it would be absorbed. This is a real concern but the design is a tradeoff.

4. **Drift recovery can absorb unrelated records** - this is a correctness issue. The "exactly one unmatched record" is a heuristic, not a guarantee. MEDIUM.

5. **The `filterMergedClusters` doesn't handle nil findings** - if `findings` is nil, `len(findings) == 0`, loop doesn't run, returns `items` unchanged. OK.

6. **Type assertion / missing import** - `sort` package is used in `unionReviewers` (`sort.Strings(out)`) but the import isn't shown in the diff. Need to verify. The diff shows `merge.go` changes but the import block isn't shown. If it was already imported elsewhere in the file, this is fine. Need to look at the existing imports of merge.go.

7. **`strings` package usage** - `strings.ToLower`, `strings.TrimSpace`, `strings.Join` are used. Need to verify imports.

8. **Performance issue - calling `filterMergedClusters` always** - if there are no gray-zone items and no merged clusters, the function still scans the findings list once. The early return at `if len(mergedLocs) == 0` helps. This is minor.

9. **Test for "1 exact + 1 drift + 1 unrelated same-line" scenario** - the "same-line drift" test has 2 records at a.go:10 and absorbs the drifted one. But what if there are 3 records at a.go:10 (member A + drifted member B + 1 unrelated)? Per the design, "more than one unmatched is ambiguous, contributes nothing." So the drift wouldn't apply. The cluster would still have 1 match from exact, fail `len(matched) < 2`, return false. This is a graceful no-op but might surprise users.

10. **The `firstIdx` logic** - In `applyOneClusterMerge`, the loop over findings to build `group` iterates over ALL findings, not just matched ones. Then for output, iterates again. This is O(n) each. Fine.

11. **`group` ordering** - The `group` is built in findings index order, not cluster member order. This affects tie-breaking in `mergeSeverity` and `longestField` - the earliest findings.json record wins on ties. The test expects "loop boundary error causes overflow" (bob's) to be the longer problem, so this is fine. But if there's a tie, the order matters.

12. **`mergeVerification` tie-breaking** - The doc says "ties resolve to the first member, for determinism." But the condition `verdictRank(v.Verdict) > verdictRank(chosen.Verdict)` only replaces on STRICTLY higher rank. On equal rank, `chosen` is kept. The "first member" is the one with the lowest index in `group`, which corresponds to the first matched record. OK.

13. **`mergeVerification` returns a copy** - `out := *chosen` makes a copy. But the source `*chosen` is a pointer to a record in `group`. After `MergeJSONFindings` returns, the records in `group` are dropped (replaced by `merged`), so the original is GC'd. The copy is correct.

14. **The `applyClusterMerges` processes clusters in slice order** - this is deterministic given a stable input.

15. **Log from context** - `log.FromContext(ctx).Warn(...)` - need to verify this is the right logger. Probably is.

16. **The `if len(mergeClusters) > 0` guard** - guards `applyClusterMerges` call. Good.

17. **In `debate.go` the `if applied < len(mergeClusters)` warning** - this catches when some clusters couldn't be applied. Good.

Let me focus on the most significant issues:

**Issue 1: Silently swallowed error in debate.go**
```go
grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
```
The comment says "A missing/corrupt ambiguous.json degrades to no gray-zone work" but a corrupt file is silently ignored. This is an error-handling issue. MEDIUM.

**Issue 2: Drift recovery can absorb unrelated records**
The "exactly one unmatched record" heuristic in `applyOneClusterMerge` can match an unrelated co-located finding if a member's record has been replaced. The exact-anchor check helps (requires at least 1 exact hit) but doesn't fully prevent this. MEDIUM. The test `TestRunDebate_GrayZoneMergeDriftAndNoOverCapture` covers the "unrelated at different line" case but not the "unrelated at SAME line as a drifted member" case.

**Issue 3: Imports not shown in diff**
`sort` and `strings` are used in `merge.go` - need to verify imports are present. The diff only shows new code, not the import block. If they're new, this is a compile error. If they were already imported, fine. Let me assume they're already there since the test `TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched` works, implying the code compiles.

**Issue 4: Same-line drift recovery can absorb an unrelated finding**
If a member's record was removed and an unrelated finding happens to be at the same line, and the OTHER member has an exact match, the unrelated finding would be absorbed. This is a real concern, though the design is a tradeoff. MEDIUM.

**Issue 5: `len(matched) < 2` after drift might miss the 3+ member case**
The plan says "Union ALL cluster members" forward-compat with Epic 13.2 (N-size clusters). The check `len(matched) < 2` allows 3+ matches, which is correct. But if a 3-member cluster has 1 exact + 1 drift + 1 not-found, only 2 match, allowed. If 0 exact + 3 drift, rejected. If 1 exact + 2 drift, allowed. Seems correct.

**Issue 6: `c.Findings` is `[]stream.Finding` in `applyOneClusterMerge`**
This is using the cluster's member list, which is from ambiguous.json. The plan says to apply by location. The member list is used to identify WHICH findings to merge. OK.

**Issue 7: `findings[i].ClusterMerged` check in apply**
```go
if findings[i].ClusterMerged {
    return findings, false // already applied: strict no-op
}
```
This is defensive - should be caught by `filterMergedClusters` first. But it's also a strong guarantee: a re-run past the radar filter is a no-op. Good.

Let me also check the `applyOneClusterMerge` function more carefully for the drift recovery edge case:

The drift recovery pass groups unmatched records by member location. If a member location has exactly 1 unmatched record, it's matched. This means:
- A drifted member (its record no longer has the original Problem) → matched
- An unrelated co-located finding replacing a deleted member → matched (BAD)

The exact-anchor requirement (at least 1 exact hit) helps but doesn't fully prevent this.

Example: Cluster has 2 members at a.go:10 with problems "alpha" and "beta". findings.json has:
- a.go:10 "alpha" (exact match for member A)
- a.go:10 "completely unrelated" (no match for member B's "beta")

Pass 1: "alpha" matches exactly. matched = {idx_alpha}.
Pass 2: At a.go:10, there's 1 unmatched record: "completely unrelated". matched = {idx_alpha, idx_unrelated}.
exactHits = 1, len(matched) = 2. 
Group = [alpha, unrelated]. Merge → some merged record.

The unrelated finding IS absorbed! The plan's "an unrelated co-located finding is never absorbed" claim is FALSE for this scenario.

This is a real correctness issue. MEDIUM or HIGH depending on severity.

Actually wait, let me re-read the plan:
> "Two or more is ambiguous and contributes nothing"

So if a.go:10 has 2 unmatched records (the drifted member + unrelated), it's ambiguous, neither is matched. But if a.go:10 has 1 unmatched record (only the drifted member, no unrelated), it's matched. The "exactly one" rule is the key safeguard.

So the scenario I described:
- a.go:10 "alpha" (exact match)
- a.go:10 "completely unrelated" (no exact match)

At a.go:10, there's 1 unmatched record ("completely unrelated"). It's matched. The "completely unrelated" is absorbed.

Yes, this is a real bug. The "exactly one" rule only protects against MULTIPLE unmatched records at the same location. A single unmatched record is assumed to be the drifted member, but it could be an unrelated finding.

Wait, but if "completely unrelated" is at a.go:10 and is the ONLY record there besides the exact-matched "alpha", the assumption is that "completely unrelated" is the drifted member. But what if "completely unrelated" is genuinely an unrelated finding? Then the design incorrectly absorbs it.

However, this is a tradeoff. The alternative is to skip the cluster entirely if any member can't be exactly matched, which is too strict (doesn't handle drift). The "exactly one" rule is a best-effort heuristic.

**HIGH severity**: Drift recovery can absorb unrelated records when a member's record has been replaced. The "exactly one unmatched record" assumption is flawed.

Wait, I should consider what "unrelated co-located" means in the test. The test `TestRunDebate_GrayZoneMergeDriftAndNoOverCapture` has the unrelated finding at a.go:50, which is NOT a member location. So the unrelated finding is never considered for drift recovery. Good. But it doesn't test the case where the unrelated finding is at a member's location.

So my concern is valid - there's an untested edge case where the drift recovery would incorrectly absorb an unrelated finding at a member's location.

**Issue 8: The function `applyOneClusterMerge` doesn't validate that the merged record's location is correct**
It sets `merged.File, merged.Line = c.File, c.Line` - this uses the cluster's canonical location. If the cluster's location differs from the actual findings.json locations, the merged record is at a different location. This is intentional per the plan.

**Issue 9: `merged.ClusterMerged = true` is set AFTER `MergeJSONFindings`**
`MergeJSONFindings` doesn't set ClusterMerged (it returns a copy of the fields from `group[0]`). So the caller setting `merged.ClusterMerged = true` is correct.

But wait, `MergeJSONFindings` returns a NEW `JSONFinding` struct, not a pointer. The fields are copied from `group[0]`. The `ClusterMerged` field is not set by MergeJSONFindings. So the caller's `merged.ClusterMerged = true` is correct.

**Issue 10: `findings[i].ClusterMerged` check could miss**
When the loop iterates over `findings`, it checks each matched record's `ClusterMerged`. If any is true, returns false. But if 2 members and 1 is already merged, the other is still fresh. The function would reject. But this is an edge case where the filter should have already excluded the item.

OK let me also check:

**Issue 11: `evidence` is "ev-alice" / "ev-bob" in test, "ev1" / "ev2" in another test**
The `joinEvidence` function concatenates them with " / ". Not tested.

**Issue 12: Performance - O(n*m) for cluster merge**
`applyClusterMerges` is O(n*m) where n is findings count and m is cluster count. For typical sizes this is fine. But if there are 1000 findings and 100 clusters, this is 100K operations. OK for typical use.

**Issue 13: `filterMergedClusters` always runs even if no gray-zone items**
The function checks `it.Kind == reconcile.KindGrayZone` for each item. If there are no gray-zone items, it just returns `items` unchanged. The function is called regardless. The early return for `len(mergedLocs) == 0` is the only optimization. Minor performance issue.

**Issue 14: The `clusterIdx` is built unconditionally**
Even if no gray-zone items are in `df.Items`, the cluster index is built. Minor.

Let me also check:

**Issue 15: `ApplyRulings` is called BEFORE `applyClusterMerges`**
```go
if len(rulings) > 0 {
    applyRulings(findings, rulings)
}
if len(mergeClusters) > 0 {
    findings, applied = applyClusterMerges(findings, mergeClusters)
}
```

So single-finding rulings are applied first, then cluster merges. This could cause issues if a single-finding ruling targets a record that's also a cluster member. The merged cluster would be applied on top, potentially with stale or missing data. But `filterMergedClusters` should have excluded the item, so this shouldn't happen.

**Issue 16: `readFindings` not shown in diff**
The test uses `readFindings(t, dir)` which is presumably a test helper from elsewhere. Not a diff issue.

OK, focusing on the real issues to report. The sprint plan is in scope, so I should report on these:

1. **HIGH: Silently swallowed error in `reconcile.ReadAmbiguousClusters` call** - error handling
2. **MEDIUM: Drift recovery can absorb unrelated records at a member's location** - correctness, untested edge case
3. **LOW: `applyClusterMerges` always processes all clusters** - performance (negligible)
4. **LOW: `filterMergedClusters` always runs even when no merged clusters exist** - performance (negligible)

Let me also think about the imports question. Looking at the diff, `merge.go` shows new functions using `sort.Strings` and `strings.ToLower`/`strings.TrimSpace`/`strings.Join`. The import block isn't shown in the diff. If these imports are already present in the file, this is fine. If not, the code won't compile.

Let me check the original `merge.go` (from the diff context, the file is `internal/reconcile/merge.go` and the function `confidenceFor` is at the end). Without seeing the original import block, I can't verify. But the diff doesn't add new imports, so the new code uses existing imports. This is a reasonable assumption.

Actually wait, let me re-read the diff more carefully. The diff shows:
```
@@ -248,3 +248,146 @@ func confidenceFor(reviewerCount int) string {
 	}
 	return ConfMedium
 }
```

This is a single hunk. The import block at the top of the file is not changed. So if `sort` and `strings` are already imported, this is fine. If not, it's a compile error.

Without seeing the original file, I'll note this as a potential issue but not a confirmed one.

Let me look at the test `TestMergeJSONFindings_VerificationPrecedence`:
```go
assert.Equal(t, "sk1,sk2", m.Verification.Skeptic, "skeptic provenance from all members is unioned")
```

So the skeptic separator is `,` (comma). And in `mergeVerification`:
```go
out.Skeptic = strings.Join(skeptics, ",")
```

OK, consistent.

Wait, I see in the diff for `applyOneClusterMerge`:
```go
if findings[i].ClusterMerged {
    return findings, false // already applied: strict no-op
}
```

This returns `findings` unchanged. But the comment says "strict no-op" - OK.

But what about the case where MULTIPLE members are matched, and ONE of them is already merged? The function returns false. So the cluster is not applied. But it might have been a partially-applied cluster. The plan says "Re-run idempotency holds — a second debate run does not re-merge or corrupt an already-applied cluster." So no-op is the right behavior.

Actually wait, the filter `filterMergedClusters` should have excluded this cluster from selection. So this is a defensive check. If the filter is working correctly, this branch is never hit.

But the filter checks if a finding at the gray-zone item's location has ClusterMerged. If yes, the item is filtered out. The gray-zone item's location is `it.File, it.Line` which is the cluster's canonical location. The merged survivor is at the cluster's canonical location (`merged.File, merged.Line = c.File, c.Line`). So the filter would catch this.

OK, that's consistent.

Let me also think about: what if a cluster's member record is later merged with a third finding (≥0.7 similarity)? The plan mentions this as the "third-finding wrinkle". The drift recovery is supposed to handle this. But how does the system know which record to use?

Scenario: Cluster has 2 members at a.go:10: "alpha" and "beta". In findings.json, the "beta" record has been merged with a third finding (≥0.7), resulting in a record with problem "gamma" (longestField). Now findings.json has:
- a.go:10 "alpha"
- a.go:10 "gamma"

Pass 1: "alpha" matches exactly. matched = {idx_alpha}.
Pass 2: At a.go:10, there's 1 unmatched record: "gamma". matched = {idx_alpha, idx_gamma}.
Group = [alpha, gamma]. Merge.

But is "gamma" really the drifted member B? Or is "gamma" the result of merging B with a third finding at a DIFFERENT location? The "gamma" record is at a.go:10 (the same as alpha), so the drift recovery correctly assumes it's the drifted member B.

But what if "gamma" is actually a MERGER of member B (originally at a.go:10) with a third finding at a.go:11? The merged record would be at a.go:10 (the cluster's canonical location, but actually, the longestField merge might not preserve location). This is a real concern.

Hmm, but the plan says:
> "Third-finding wrinkle handled inline: when a gray member was already merged with a third finding (≥0.7), union all records the cluster spans at its location(s). Not punted to the authored-adjudication path."

So the design is: assume the third-finding merge happened at the cluster's location(s). If the third-finding merge changed the location, the drift recovery might miss it. But this is a limitation of the design, not a bug per se.

OK, I think I've analyzed enough. Let me also note:

**Issue 17: `c.Findings` could be nil or empty for malformed clusters**
The function doesn't check for this. If `c.Findings` is empty, the loops don't execute, no matches, returns false. Safe.

**Issue 18: Concurrent access to `findings`**
The `runDebate` function uses a semaphore to limit concurrency, but the `findings` slice is shared. The merge happens after all goroutines complete, so no concurrent modification. OK.

**Issue 19: The `applyClusterMerges` is called even if `mergeClusters` is empty**
Wait, no, it's guarded by `if len(mergeClusters) > 0`. OK.

**Issue 20: `applyRulings` modifies findings in place, `applyClusterMerges` returns a new slice**
Inconsistent - applyRulings is `applyRulings(findings, rulings)` (no return), applyClusterMerges is `findings, applied = applyClusterMerges(findings, mergeClusters)`. The first one modifies in place, the second returns a new slice. The reason: applyRulings removes records, so the new slice has fewer records. applyClusterMerges also reduces the record count. Both could return new slices, but applyRulings doesn't. Inconsistency in API. LOW.

Actually, looking at the function signature, `applyRulings` might also return a new slice but it's assigned back to `findings`. Let me check the diff... I don't see the signature, but the call is `applyRulings(findings, rulings)`. If it modifies in place, the variable `findings` is still the same slice. The call `findings, applied = applyClusterMerges(findings, mergeClusters)` reassigns `findings` to a new slice.

Wait, `applyRulings(findings, rulings)` - if this doesn't reassign, the slice is still `findings`. Then `findings, applied = applyClusterMerges(findings, mergeClusters)` reassigns to a new slice. So the new slice is the cluster-merged findings. Good.

But what if `applyRulings` returns a new slice and the caller ignores it? Then the original `findings` slice is unchanged, and `applyClusterMerges` operates on the old slice. This could be a bug. But without seeing `applyRulings` signature, I'll trust the call.

OK let me finalize my list of issues to report:

1. **HIGH/MEDIUM: Silently swallowed error** in `internal/debate/debate.go` for `reconcile.ReadAmbiguousClusters(reviewDir)` - error handling
2. **MEDIUM: Drift recovery can absorb unrelated records** at a member's location - correctness, untested edge case
3. **LOW: Performance** - `filterMergedClusters` always runs and `indexClusters` always builds the map
4. **LOW: `applyRulings` vs `applyClusterMerges` API inconsistency** - maintainability

Let me also check the dedupe path: the plan says "The authored adjudication.json path (ambiguous.go / gate.go / dedupeCluster) is untouched — AC3 holds for free." Looking at the diff, only `emit.go` and `merge.go` in reconcile are touched, and `cluster_merge_test.go` is added. So `ambiguous.go`, `gate.go`, `dedupe.go` are untouched. OK.

But the `merge.go` file is shared between the two paths. The new `MergeJSONFindings` and helpers are in `merge.go`. The existing `Merge` function is for the authored path. The new function is for the inline path. They're separate functions, so no conflict.

But wait, the new `MergeJSONFindings` uses `mergeSeverity`, `longestField`, `modalCategory`, `maxEstMinutes` - these are existing helpers in `merge.go`. The `longestField` and `modalCategory` and `maxEstMinutes` are package-level. So the new function reuses them. Good.

Now let me finalize and write the findings.

Actually, let me look more carefully at the error handling:

```go
grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
clusterIdx := indexClusters(grayClusters)
df.Items = filterMergedClusters(df.Items, findings)
```

The error from `ReadAmbiguousClusters` is discarded. If the file is missing, `ReadAmbiguousClusters` might return an empty slice with no error (if it handles missing files gracefully). If the file is corrupt, it should return an error. The current code ignores this error.

The plan says: "A missing/corrupt ambiguous.json degrades to no gray-zone work." This is the intended behavior. But silently ignoring errors is generally bad practice. A WARN log would be appropriate.

This is a MEDIUM severity issue.

For the drift recovery issue:
- The "exactly one unmatched record" heuristic can absorb an unrelated finding.
- This is a real concern but a tradeoff.
- The plan says: "Per-location drift recovery handles the same-line case (two co-located members where one drifted): the drifted member is the lone unmatched record left at that location after the exact pass."
- The plan assumes the lone unmatched record is the drifted member. But it could be an unrelated co-located finding.

This is a MEDIUM severity correctness issue.

Let me also think about the test for "no exact anchor":
- `TestRunDebate_GrayZoneMergeRequiresExactAnchor` covers the case where 0 members match exactly. Returns no-op. Good.
- But it doesn't cover the case where 1 member matches exactly and 1 member's location has an unrelated record. The drift recovery would absorb the unrelated record.

This is a missing test, but the design is what it is. I'll flag this as a concern.

Now let me also check for any race conditions:
- `df.Items = filterMergedClusters(df.Items, findings)` - df is read here. findings is read here. Both are not modified.
- `clusterIdx := indexClusters(grayClusters)` - grayClusters is read.
- The goroutines: `debateOne` is called per item. `ir := debateOne(...)` returns `ir` which is assigned to `oc.ir`. `oc.clusterMerge` and `oc.cluster` are set based on `ir.ClusterDecision` and `clusterIdx`. The cluster is read from `clusterIdx` which is a map.

Wait, `clusterIdx` is a `map[FindingKey]reconcile.AmbiguousCluster`. Maps are not safe for concurrent access, but the goroutines only READ from it. So this is safe. But `cluster` is a value type, so each goroutine gets its own copy. Good.

But wait, the `AmbiguousCluster` struct contains a slice (`Findings`). The slice header is copied, but the underlying array is shared. If `applyClusterMerges` doesn't modify the slice, this is safe. Looking at the code, it doesn't modify the cluster's Findings. OK.

OK, final list of issues:

1. **MEDIUM: Silently swallowed error in `reconcile.ReadAmbiguousClusters`** at `internal/debate/debate.go:111`
2. **MEDIUM: Drift recovery can absorb unrelated records at a member's location** at `internal/debate/cluster.go` (the `unmatchedAtLoc` logic)
3. **LOW: Performance - always building cluster index** at `internal/debate/debate.go:112` even when no gray-zone items

Hmm, let me also check if there's a potential issue with `c.Findings` being nil:

```go
memberExact := map[FindingKey]bool{}
memberLocs := map[string]bool{}
for _, mf := range c.Findings {
    ...
}
```

If `c.Findings` is nil, the loop doesn't execute. `memberExact` and `memberLocs` are empty maps. `matched` is empty. `exactHits = 0`. `len(matched) < 2` is true. Returns `(findings, false)`. Safe.

If `c.Findings` has 1 entry, `len(matched) < 2` is true after at most 1 match. Returns false. Safe.

If `c.Findings` has 2+ entries and all match, `len(matched) >= 2`.
</think>
