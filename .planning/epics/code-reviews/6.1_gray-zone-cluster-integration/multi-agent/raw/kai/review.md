The user wants me to review a code diff and output findings in a specific pipe-delimited format. I need to consider the sprint plan scope carefully.

Let me analyze the diff files:
1. `internal/debate/cluster.go` - new file, in scope (debate package, gray-zone cluster merge logic)
2. `internal/debate/cluster_test.go` - new file, tests for the above, in scope
3. `internal/debate/debate.go` - modified file, debate logic, in scope
4. `internal/reconcile/cluster_merge_test.go` - new file, tests for MergeJSONFindings, in scope (reconcile package mentioned in sprint plan)
5. `internal/reconcile/emit.go` - modified file, adds ClusterMerged field, in scope
6. `internal/reconcile/merge.go` - modified file, adds MergeJSONFindings and helpers, in scope

Now let me look for issues:

**Security:**
- No obvious SQL injection, XSS, etc. This is backend Go code.

**Error handling:**
- In `cluster.go`, `applyOneClusterMerge` doesn't seem to have major error handling issues, but let me check.
- In `debate.go`, line with `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` - the error is ignored! This is explicitly mentioned in the comment "A missing/corrupt ambiguous.json degrades to no gray-zone work." But is this safe? The sprint plan says it's intentional degradation. However, if the file exists but is corrupt, silently ignoring might hide issues. But the comment explicitly says this is intentional. Still, logging the error might be better, but the sprint plan says this is acceptable degradation.

**Performance:**
- In `applyOneClusterMerge`, we iterate over findings multiple times. Not a major issue for typical sizes.
- In `filterMergedClusters`, we build a map and filter. Fine.

**Correctness:**
- `debate.go`: The outcomes loop collects `mergeClusters` but doesn't check for duplicate clusters. If two gray-zone items from the same cluster are selected and debated separately (though that seems unlikely if the radar presents one item per cluster), but wait - the radar items are per cluster? Looking at the code, `df.Items` contains DisagreementItems. Each gray-zone cluster becomes one DisagreementItem? If the same cluster appears twice, it could be merged twice. But the code later does `applyClusterMerges` which checks `ClusterMerged` flag to prevent re-merge. However, what if two different debate outcomes reference the same cluster and both say merge? The second one would be a no-op because `ClusterMerged` is already set on the findings. Wait, but the first merge would have set the flag and rewritten findings. Then in the outcomes loop, we collect all mergeClusters. If there are duplicates in mergeClusters, `applyClusterMerges` handles them one by one; the first applies, the second hits the `ClusterMerged` check and returns false. So that's safe.

- `cluster.go`: In `applyOneClusterMerge`, the logic for drift recovery:
  ```go
  for _, idxs := range unmatchedAtLoc {
      if len(idxs) == 1 {
          matched[idxs[0]] = true
      }
  }
  ```
  This doesn't check if the unmatched record actually corresponds to a member whose exact match wasn't found. It just checks location. The comment says "a member location with exactly one unmatched record contributes it (the drifted member)". But what if there are multiple members at the same location, and one matched exactly, leaving one unmatched at that location? Then it would match the remaining one. That's the "same-line drift" case. But what if there's a member location with 0 exact matches, and 1 unrelated unmatched finding at that location? Then `exactHits == 0` would prevent the merge. Good.
  
  What if there are two members at the same location, NEITHER matches exactly, and there's one unrelated finding at that location? `exactHits == 0`, so no merge. Good.
  
  What if there are two members at the same location, one matches exactly, and there are TWO unmatched findings at that location (one is the drifted member, one is unrelated)? Then `len(idxs) == 2`, so no drift recovery for that location. The exact matched one is in, the other two are out. `len(matched) == 1`, so `< 2`, no merge. Good.

  But wait: what if there are two members at the same location, neither matches exactly, but there is exactly one unmatched finding at that location (which is unrelated)? `exactHits == 0`, so no merge. Good.

  What if there are two members at different locations. Member A matches exactly. Member B doesn't match exactly. At member B's location, there's exactly one unmatched finding (unrelated). `exactHits == 1`, `len(matched)` would be 2 (member A exact + member B drift). But member B's drifted match is actually unrelated! This is a bug. If member B's location has exactly one unmatched record, but that record is NOT actually the member (because the member was removed/refuted and an unrelated finding exists there), it would be incorrectly merged.
  
  Wait, but the comment says: "Per-location drift recovery handles the same-line case (two co-located members where one drifted): the drifted member is the lone unmatched record left at that location after the exact pass. More than one unmatched record at a member location is ambiguous, so none is taken — an unrelated co-located finding is never absorbed."
  
  However, this assumes that if there's exactly one unmatched record at a member location, it MUST be the drifted member. But what if the member was removed and an unrelated finding is at that location? The code would absorb it incorrectly. But the `exactHits == 0` guard only helps if NO members match exactly. If at least one member matches exactly, and another member's location has exactly one unmatched record (unrelated), it gets absorbed.
  
  Is this a real issue? The comment acknowledges "unrelated co-located finding is never absorbed" only when there are MORE than one unmatched records. It seems the code assumes that if there's exactly one unmatched record at a member's location, it's the drifted member. But this is not guaranteed. However, looking at the sprint plan and the specific test cases, this might be considered acceptable or within the design. But as a reviewer, I should flag this as a correctness issue if the logic can over-capture.
  
  Actually, looking more carefully: the `memberLocs` map contains all member locations. In pass 2, we look at unmatched findings. If an unmatched finding is at a member location, and it's the only one, we match it. If the member was removed and an unrelated finding exists there, we would indeed match it. But the `exactHits == 0 || len(matched) < 2` guard: if exactHits > 0 and len(matched) >= 2, we proceed. So if we have one exact hit and one false drift recovery, len(matched) == 2, exactHits == 1, we proceed to merge. That would incorrectly merge the unrelated finding.
  
  But is this a realistic scenario? The test `TestRunDebate_GrayZoneMergeDriftAndNoOverCapture` tests that an unrelated finding at a DIFFERENT line is not absorbed. But what about an unrelated finding at the SAME line as a member whose problem text drifted (or was removed)?
  
  Wait, if a member was removed/refuted, there might be no finding at that location, or an unrelated one might be there. The test `TestRunDebate_GrayZoneMergeRequiresExactAnchor` shows that when NO exact anchor exists, nothing is merged. But that's when ALL members don't match. If ONE member matches exactly and another member's location has an unrelated finding (because the member was removed), it seems like the unrelated finding would be absorbed.
  
  However, looking at the code again: the `cluster` passed to `applyOneClusterMerge` comes from `mergeClusters` which was built from the debate outcomes. The debate outcome references a cluster by `FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}`. The `clusterIdx` maps from that key to `AmbiguousCluster`. So the cluster is the original cluster from `ambiguous.json`. The findings are from `findings.json`.
  
  If a member was refuted/removed during verify stage, it might not be in findings.json. In that case, the member's location might have no finding, or an unrelated finding might have appeared there. If there's exactly one unmatched finding at that location, it gets absorbed. This seems like a real correctness issue.

  But wait - is there any check that the unmatched record's problem text is related? No. The code explicitly says it takes "the sole still-unmatched record at that member's location". The design intentionally ignores problem text for drift recovery. But this means any unrelated finding at that location (sole unmatched) gets absorbed if there's at least one exact anchor elsewhere.

  Let me check if there's any mitigation. The test `TestRunDebate_GrayZoneMergeRequiresExactAnchor` has two unrelated findings at the member locations, but both members don't match exactly. So no merge. But what if one member matches and the other location has an unrelated finding?
  
  Actually, there's another issue. In `applyOneClusterMerge`:
  ```go
  if exactHits == 0 || len(matched) < 2 {
      return findings, false
  }
  ```
  `len(matched) < 2`: if there are two members but only one exact hit and no drift recovery, len(matched) == 1, so it returns false. Good. If there are two members, one exact hit, and one drift recovery (true or false), len(matched) >= 2, proceeds.
  
  If the drift recovery is false (because there are 0 or 2+ unmatched findings at that location), len(matched) == 1, returns false. Good.
  
  So the only way to absorb an unrelated finding is if there's exactly 1 unmatched finding at a member location. The code assumes that's the drifted member. Is there any way to verify? The sprint plan says "Per-location drift recovery handles the same-line case...". It seems this is a known design tradeoff.
  
  Actually, I think this is worth flagging as MEDIUM correctness. The drift recovery assumes sole unmatched record at member location is the drifted member, but it could be an unrelated finding that replaced the removed member. A safer approach might be to require that drifted members only be recovered if the location matches AND there are no other unmatched findings. But that's what it does. The issue is that it doesn't verify the record is actually a member.
  
  Wait, could we compare the record's problem length or something? No, that's the drift. Could we check if the record was originally a member? Not without more metadata.
  
  Given the explicit design in the sprint plan ("apply by location on the live record"), this might be intentional. But I should still flag it as a potential correctness issue because unrelated findings at member locations can be incorrectly merged.

  Let me look for other issues.

**debate.go:**
- The `clusterIdx` is built from all `grayClusters`, but then in the worker goroutine:
  ```go
  if ir.ClusterDecision == ClusterMerge {
      if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok {
          oc.clusterMerge = true
          oc.cluster = c
      }
  }
  ```
  There's no `else` or warning if `!ok`. The cluster decision is "merge" but if the cluster isn't found in the index, it's silently ignored. This seems like it could happen if `ambiguous.json` and the disagreement item are out of sync. The outcome just doesn't get applied. Should there be a warning? The code later logs if `applied < len(mergeClusters)`, but here the cluster might not even enter `mergeClusters`. Actually, if `!ok`, `oc.clusterMerge` stays false, so it won't be in `mergeClusters`. The judge ruled merge but it won't be applied and won't be counted as "ruled but not applied". Wait, it would be recorded in `debate.json` via `items = append(items, oc.ir)` and `oc.ir` contains `ClusterDecision`. But there's no indication to the user that the cluster wasn't found. This could be a correctness issue. However, the sprint plan scope is about applying cluster decisions. If the cluster isn't found, it can't be applied. Should we log? The code doesn't. But is this a real bug or just an edge case?
  
  Actually, looking at the code flow: `df.Items` comes from `reconcile.LoadDisagreements`, which uses `ambiguous.json` to build gray-zone items. The `it.File, it.Line, it.Problem` for a gray-zone item should correspond to a cluster. And `clusterIdx` is built from `reconcile.ReadAmbiguousClusters(reviewDir)` which reads the same `ambiguous.json`. So they should be in sync unless there's a race or corruption. But the error from `ReadAmbiguousClusters` is ignored! So if `ambiguous.json` is corrupt, `clusterIdx` might be empty, but `df.Items` might still have gray-zone items (from `LoadDisagreements` which might handle errors differently). Wait, does `LoadDisagreements` read ambiguous.json too? Probably. If it succeeds but `ReadAmbiguousClusters` fails (e.g., partial read?), there could be a mismatch.
  
  Actually, if `ReadAmbiguousClusters` returns an error and nil/empty clusters, `clusterIdx` is empty. But `df.Items` was loaded by `LoadDisagreements` which may have its own error handling. If `df.Items` contains gray-zone items but `clusterIdx` doesn't have them, the merge decisions will be silently dropped. This is a real issue because the error from `ReadAmbiguousClusters` is discarded.
  
  The comment says "A missing/corrupt ambiguous.json degrades to no gray-zone work." But `df.Items` might still contain gray-zone items if `LoadDisagreements` succeeded. In that case, we'd debate them but not merge them. Actually, `LoadDisagreements` likely also reads ambiguous.json, so if it's corrupt, both might fail. But if `LoadDisagreements` is more lenient... Hard to say without seeing its code.
  
  Regardless, silently ignoring the error from `ReadAmbiguousClusters` means we might miss clusters that exist. But the comment explicitly says this is intentional degradation. However, if the file is corrupt (not missing), should we degrade? A corrupt file might indicate a serious issue. But the design says it's OK.
  
  I think this is worth flagging as MEDIUM - at least log the error.

- `debate.go`: In the outcomes loop:
  ```go
  for _, oc := range outcomes {
      items = append(items, oc.ir)
      tally(&res, oc.ir)
      if oc.apply {
          rulings[oc.key] = oc.rule
      }
      if oc.clusterMerge {
          mergeClusters = append(mergeClusters, oc.cluster)
      }
  }
  ```
  If the same cluster appears twice in `mergeClusters` (e.g., two gray-zone items pointing to the same cluster? unlikely but possible), `applyClusterMerges` will process it twice. The second time, the finding will have `ClusterMerged` set, so `applyOneClusterMerge` returns false. Then `applied < len(mergeClusters)` will log a warning that some merges could not be applied, even though the first one succeeded. This is a false warning. But is it possible for the same cluster to appear twice? The radar might deduplicate by `FindingKey`. If not, this could happen. But looking at the code, `df.Items` is a list of disagreement items. If there are duplicate gray-zone items, they might both be selected. However, the `filterMergedClusters` drops already merged clusters, but not duplicate pending clusters. This seems like a minor issue.

- `cluster.go`: `applyClusterMerges` modifies the `findings` slice header (reassigning) but the caller in `debate.go` uses the returned slice:
  ```go
  findings, applied = applyClusterMerges(findings, mergeClusters)
  ```
  This is correct.

- `cluster.go`: In `filterMergedClusters`, if `len(mergedLocs) == 0`, it returns `items` (the original slice). If not, it creates a new slice. This is fine.

- `merge.go`: `joinEvidence` builds a string with `out += " / "` and `out += p` in a loop. This is O(n²) in the total evidence length because string concatenation in Go creates a new string each time. With few items it's fine, but it's still inefficient. Should use `strings.Join`. The code for `joinEvidence` could be:
  ```go
  return strings.Join(parts, " / ")
  ```
  But the current code does manual concatenation. For typically small number of parts, it's low severity. But since `parts` is already built, using `strings.Join` is cleaner and more performant.
  
  Actually, looking at the code:
  ```go
  out := ""
  for i, p := range parts {
      if i > 0 {
          out += " / "
      }
      out += p
  }
  return out
  ```
  This is indeed inefficient for large numbers of parts (but there won't be large numbers). Still, maintainability/performance: use `strings.Join`. LOW severity.

- `merge.go`: `mergeVerification` copies the chosen verification:
  ```go
  out := *chosen // copy so the source finding's block is not mutated
  out.Skeptic = strings.Join(skeptics, ",")
  return &out
  ```
  This is good, prevents mutation.

- `merge.go`: `MergeJSONFindings` builds `sf` (stream.Finding slice) from the group to reuse existing merge helpers like `mergeSeverity`, `longestField`, `modalCategory`, `maxEstMinutes`. But `joinEvidence` operates on `JSONFinding` directly. That's fine.

- `merge.go`: `MergeJSONFindings` uses `group[0].File` and `group[0].Line`. But the caller in `cluster.go` sets:
  ```go
  merged.File, merged.Line = c.File, c.Line
  ```
  So the canonical location comes from the cluster, not the first member. Good.

- `merge.go`: In `unionReviewers`, empty reviewers are filtered: `if r != ""`. Good.

- `emit.go`: `ClusterMerged bool` with `omitempty`. The test confirms it works. But `bool` with `omitempty` in JSON means false values are omitted. That's correct.

- `cluster_test.go`: The test `TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched` checks that `ambiguous.json` is not rewritten. It compares `before` and `after` bytes. This is fine.

- `cluster_test.go`: The tests look comprehensive for the happy path and several edge cases.

**Potential HIGH issue:**
In `debate.go`, when processing outcomes:
```go
if oc.clusterMerge {
    mergeClusters = append(mergeClusters, oc.cluster)
}
```
There is no deduplication of clusters. If the same cluster is debated multiple times (e.g., re-run but not filtered? No, filterMergedClusters handles that. But what if the same cluster appears as two different items in `df.Items`?), `mergeClusters` could have duplicates. Then `applyClusterMerges` applies the first, the second hits `ClusterMerged` and returns false. Then `applied < len(mergeClusters)` triggers a warning log. Also, the second application attempt is a waste. Not a high severity bug though.

Wait, is there a data race?
In `debate.go`:
```go
for i, it := range sel.Selected {
    sem <- struct{}{}
    go func(idx int, item reconcile.DisagreementItem) {
        defer func() { <-sem }()
        // ...
        oc := itemOutcome{ir: ir}
        // ...
        outcomes[idx] = oc
    }(i, it)
}
wg.Wait()
```
This looks correct. Each goroutine writes to a distinct index in `outcomes`.

In `cluster.go`: `filterMergedClusters` takes `findings []reconcile.JSONFinding` but doesn't modify it. Good.

Actually, I see an issue in `applyOneClusterMerge`. Look at this logic:
```go
for i := range findings {
    switch {
    case i == firstIdx:
        out = append(out, merged)
    case matched[i]:
        // dropped — folded into merged
    default:
        out = append(out, findings[i])
    }
}
```
What if `firstIdx` is not the first matched index? That's fine, we just insert merged there and drop other matched indices. But wait: what if `firstIdx` comes AFTER some other matched index? The `out` slice is built in order of `i`. When `i` reaches `firstIdx`, it appends `merged`. Then for later `i` where `matched[i]` is true, it drops them. For `i` before `firstIdx` that are not matched, they get appended. This preserves the relative order of unmatched items and inserts the merged record at `firstIdx`. But `firstIdx` is the first matched index encountered in the loop. So the merged record replaces the first matched member. That's fine.

But what if `firstIdx` is -1? The loop `for i := range findings { if matched[i] { if firstIdx < 0 { firstIdx = i } ... } }` ensures `firstIdx` is set if `len(matched) >= 2` (which is checked before). Actually, `len(matched)` is the number of true entries in the map. So if `len(matched) >= 2`, `firstIdx` must have been set. Good.

Wait, there's a subtle issue with `applyOneClusterMerge`. The `exactHits` counts exact matches. `len(matched)` counts total matched indices (exact + drift). But what if a member matches both exactly and via drift? No, once `matched[i]` is true in pass 1, pass 2 skips it (`if matched[i] { continue }`). So no double counting.

Another issue: In `filterMergedClusters`, the `mergedLocs` map is keyed by `locationKey(f.File, f.Line)`. Then when filtering items:
```go
if it.Kind == reconcile.KindGrayZone && mergedLocs[locationKey(it.File, it.Line)] {
    continue
}
```
This drops ALL gray-zone items at that location, not just the specific cluster. If two different gray-zone clusters happen to be at the same file/line (unlikely but possible), both would be filtered. But gray-zone clusters are built from ambiguous clusters, and if two clusters share the same canonical location, they might be the same cluster or overlapping. Given the design, this seems acceptable. But is it a bug? The comment says "A cluster is 'already applied' when a findings.json record flagged ClusterMerged sits at the gray-zone item's location". It assumes one cluster per location. Given that clusters are co-located at the same line by definition (members are at the same location), this seems fine.

Let me look again at `debate.go` for the ignored error:
```go
grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
```
This is definitely error swallowing. Even if the intent is degradation, logging the error would help debugging. The category would be error-handling. But is it in scope? Yes, it's in `internal/debate/debate.go`. Severity: MEDIUM. The fix: log the error instead of ignoring it.

But wait, if I look at the sprint plan scope: "internal/debate/debate.go — emit cluster decisions in a form the reconcile adjudication path can consume (or call into it directly after the debate loop)." So `debate.go` is definitely in scope.

Is there any issue with the `merge.go` changes?
The `joinEvidence` function uses string concatenation in a loop. It's inefficient and unidiomatic. LOW severity, maintainability/performance.

Let me check `MergeJSONFindings`:
```go
sf := make([]stream.Finding, len(group))
for i, f := range group {
    sf[i] = stream.Finding{
        Severity: f.Severity, File: f.File, Line: f.Line,
        Problem: f.Problem, Fix: f.Fix, Category: f.Category,
        EstMinutes: f.EstMinutes, Evidence: f.Evidence,
    }
}
```
Wait, `stream.Finding` might not have all these fields? The code compiles (presumably), so it's fine. But I don't know the shape of `stream.Finding`. The test passes, so it must match.

Let me check the test `TestMergeJSONFindings_VerificationPrecedence`:
```go
assert.Equal(t, "sk1,sk2", m.Verification.Skeptic, "skeptic provenance from all members is unioned")
```
The `mergeVerification` function joins skeptics with comma: `out.Skeptic = strings.Join(skeptics, ",")`. So the assertion expects comma-joined. Good.

Is there any issue with `verdictRank`?
```go
func verdictRank(verdict string) int {
    switch strings.ToLower(strings.TrimSpace(verdict)) {
    case VerdictConfirmed:
        return 3
    case VerdictUnverifiable:
        return 2
    case VerdictRefuted:
        return 1
    default:
        return 0
    }
}
```
This is case-insensitive. Good.

Now, looking at `cluster.go`: `locationKey` uses `\x00` as separator. If file path contains `\x00` (impossible on most filesystems), it could break. Not an issue.

Let me reconsider the drift recovery issue. Is it serious enough to flag?
The sprint plan explicitly says: "Per-location drift recovery handles the same-line case...". It seems the design accepts this heuristic. As a reviewer, I should flag if I think it's a correctness issue, but if it's a documented design decision, maybe not. However, the prompt says "No false positives: Only report real issues". Is the drift recovery a real issue? If an unrelated finding is at a member location and is the sole unmatched record, it gets merged. This violates AC1/AC2? AC1 says union the cluster's members. If it unions an unrelated finding, that's incorrect. But the probability is low because:
1. The member location must have exactly one unmatched record.
2. That record must be unrelated (not the drifted member).
3. There must be at least one exact anchor elsewhere.

This is a real edge case. I think it's worth flagging as MEDIUM correctness. But the evidence needs to be concise.

Wait, I should also check if there's a `ClusterDecision` value validation issue.
In `debate.go`:
```go
if ir.ClusterDecision == ClusterMerge {
```
What if `ClusterDecision` is some unexpected string? It's compared to `ClusterMerge`. What are the possible values? `merge`, `separate`, `skipped` per the sprint plan. Only `merge` triggers the inline apply. `separate` and `skipped` do nothing. That's correct.

What about `applyClusterMerges`:
```go
func applyClusterMerges(findings []reconcile.JSONFinding, clusters []reconcile.AmbiguousCluster) ([]reconcile.JSONFinding, int) {
    applied := 0
    for _, c := range clusters {
        var ok bool
        findings, ok = applyOneClusterMerge(findings, c)
        if ok {
            applied++
        }
    }
    return findings, applied
}
```
If `clusters` contains duplicates, the second one will fail (because `ClusterMerged` is set). Then `applied < len(mergeClusters)` will log a warning. Is this a bug? Not really, it's just a false warning. But the root cause would be duplicate clusters in `mergeClusters`. Should we deduplicate? LOW maintainability.

Another issue: In `debate.go`, the `mergeClusters` slice is built from outcomes. If `oc.clusterMerge` is true but `oc.cluster` is empty (zero value), `applyOneClusterMerge` will likely fail because `memberExact` will be empty or something. But `clusterMerge` is only set if `c, ok := clusterIdx[...]; ok`, so `oc.cluster` is always a valid cluster. Good.

Is there any issue with concurrency? `findings` slice is read by multiple goroutines (in `filterMergedClusters` it's passed by value, but the underlying array is shared). However, `filterMergedClusters` is called before the goroutines spawn, and it doesn't modify the slice. Inside the goroutines, `findings` is not accessed. The `clusterIdx` is read-only. `outcomes` is written to distinct indices. So no race.

Wait, in `debate.go`, `findings` is a slice. The goroutines might capture `findings`? Let's look:
```go
for i, it := range sel.Selected {
    sem <- struct{}{}
    go func(idx int, item reconcile.DisagreementItem) {
        // ...
    }(i, it)
}
```
The goroutine doesn't capture `findings`. Good.

Let me look at `TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched`. It tests that debate doesn't rewrite ambiguous.json. But the code in `debate.go` calls `reconcile.ReadAmbiguousClusters(reviewDir)`. Does this function open the file? Yes. But it doesn't write it. The test checks before and after. That's fine.

What about `TestRunDebate_GrayZoneMergeIsIdempotent`. It checks that a re-run doesn't re-merge. The test passes `res2.Selected == 0`. This relies on `filterMergedClusters`. Good.

Are there any issues in `cluster_merge_test.go`?
The test names are long but that's Go convention.

One more thing: in `debate.go`, the `itemOutcome` struct now has new fields. In the loop:
```go
oc := itemOutcome{ir: ir}
switch {
case ir.Outcome == OutcomeUnresolved:
case it.Kind == reconcile.KindGrayZone:
    // ...
default:
    oc.apply = true
    // ...
}
outcomes[idx] = oc
```
If `ir.Outcome == OutcomeUnresolved` and `it.Kind == reconcile.KindGrayZone`, the first case matches and nothing happens. That's correct. If `ir.Outcome != OutcomeUnresolved` and `it.Kind == reconcile.KindGrayZone`, the second case matches. Good.

Now, is there a bug where a gray-zone item with `OutcomeUnresolved` could have `ClusterDecision` set? Probably not, but even if it did, the switch falls through to the unresolved case which does nothing.

Let me look at `applyOneClusterMerge` once more.
```go
memberExact := map[FindingKey]bool{}
memberLocs := map[string]bool{}
for _, mf := range c.Findings {
    memberExact[FindingKey{File: mf.File, Line: mf.Line, Problem: mf.Problem}] = true
    memberLocs[locationKey(mf.File, mf.Line)] = true
}
```
`c.Findings` is `[]stream.Finding`. `FindingKey` is defined elsewhere. `mf.Line` is `int`. The `FindingKey.Line` is presumably `int`. Good.

What if `c.Findings` has duplicate members? Then `memberExact` deduplicates. `memberLocs` deduplicates. If a cluster has the same member twice, `exactHits` might be 1 but `len(matched)` might be 1 (if only one finding matches). Then `len(matched) < 2` prevents merge. Good.

I think the main issues are:
1. **Ignored error** in `debate.go:111` when calling `ReadAmbiguousClusters` - MEDIUM, error-handling.
2. **String concatenation in loop** in `merge.go:347` (`joinEvidence`) - LOW, performance/maintainability. Actually, looking at the line numbers in the diff... wait, I don't have exact line numbers for the new files since they're added. I need to count lines in the diff.

For new files, the line numbers are relative to the new file (starting from 1). Let me calculate.

`internal/debate/debate.go` modifications:
- Line 110 (in new file): `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` - actually in the diff, it's around line 110 in the modified file. Let me count from the context.

In the diff for `debate.go`:
```
@@ -107,6 +107,15 @@ func runDebate(ctx context.Context, reviewDir string, reg *registry.Registry, op
 	// post-verify verification_disagreement items (absent from the reconcile-time
 	// disagreements.json snapshot) alongside severity splits and gray-zone clusters.
 	df := reconcile.LoadDisagreements(reviewDir, findings)
+	// Gray-zone cluster decisions (Epic 6.1) apply inline: load the clusters so a
+	// judge "merge" ruling can union the member findings in findings.json directly
+	// (Option A), and drop clusters a prior debate already merged so a re-run is a
+	// no-op (AC4). A missing/corrupt ambiguous.json degrades to no gray-zone work.
+	grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
```
So `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` is at approximately line 113 in the new file.

In `merge.go`, the `joinEvidence` function:
```
+func joinEvidence(group []JSONFinding) string {
+	seen := map[string]bool{}
+	parts := make([]string, 0, len(group))
+	for _, f := range group {
+		if f.Evidence == "" || seen[f.Evidence] {
+			continue
+		}
+		seen[f.Evidence] = true
+		parts = append(parts, f.Evidence)
+	}
+	out := ""
+	for i, p := range parts {
+		if i > 0 {
+			out += " / "
+		}
+		out += p
+	}
+	return out
+}
```
Line number in the new file: `merge.go` had 248 lines originally, and this function is added after. Let's count the added lines. The diff shows `@@ -248,3 +248,146 @@` so the new content starts at line 251. `joinEvidence` appears after `unionReviewers`. `unionReviewers` is maybe 20 lines, `joinEvidence` maybe 30 lines down? Let's estimate line 305? Actually, I should be precise. The diff adds `MergeJSONFindings` (about 45 lines), then `unionReviewers` (about 20 lines), then `joinEvidence` (about 20 lines). Starting at line 251:
- `MergeJSONFindings`: lines ~251-310
- `unionReviewers`: lines ~312-330
- `joinEvidence`: lines ~332-350
So roughly line 345 for the string concat loop.

Actually, looking at the exact diff lines for `joinEvidence`:
```
+func joinEvidence(group []JSONFinding) string {
+	seen := map[string]bool{}
+	parts := make([]string, 0, len(group))
+	for _, f := range group {
+		if f.Evidence == "" || seen[f.Evidence] {
+			continue
+		}
+		seen[f.Evidence] = true
+		parts = append(parts, f.Evidence)
+	}
+	out := ""
+	for i, p := range parts {
+		if i > 0 {
+			out += " / "
+		}
+		out += p
+	}
+	return out
+}
```
The `for i, p := range parts` loop is about 9 lines into the function. If `joinEvidence` starts around line 332 in the new file, the loop starts at ~341.

Wait, I should provide the exact line numbers as they appear in the final file. Since these are new files or patches, I need to count carefully.

For `internal/debate/cluster.go`, new file:
- `locationKey`: lines 10-12
- `clusterDisplayProblem`: lines 15-22
- `indexClusters`: lines 25-32
- `filterMergedClusters`: lines 35-55
- `applyClusterMerges`: lines 58-68
- `applyOneClusterMerge`: lines 71-171

For `internal/debate/cluster_test.go`, new file:
- Tests start at line 1, various functions. Not looking for issues here.

For `internal/debate/debate.go`:
Original lines 1-107 remain. Then additions at line 108-116 (the grayClusters loading). So `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)` is at line 113.

Then later in `debate.go`, the switch statement:
Original had lines 178-189. Now it becomes lines 192-215 or so. Let's see:
```
@@ -178,10 +189,22 @@ func runDebate(ctx context.Context, reviewDir string, reg *registry.Registry, op
 				ir = debateOne(ctx, debateDir, it, cfg, reg, cc, disp)
 			}
 			oc := itemOutcome{ir: ir}
-			// Apply verdicts only to single-finding items. A gray-zone ruling is a
-			// cluster-level merge/separate decision recorded for the existing
-			// adjudication path, not a per-finding verdict (see Clarifications).
-			if ir.Outcome != OutcomeUnresolved && it.Kind != reconcile.KindGrayZone {
+			switch {
+			case ir.Outcome == OutcomeUnresolved:
+				// An unresolved item settles nothing — no application