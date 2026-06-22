<think>
Let me carefully analyze this diff. The sprint plan is about Epic 6.1: Inline application of judge gray-zone cluster rulings. The key files in scope are:
- `internal/debate/debate.go` - emit cluster decisions
- `internal/reconcile/ambiguous.go` - Adjudication / Decision model and LoadAdjudication validation
- `internal/reconcile/gate.go` - adjudication re-invocation / apply path
- `internal/reconcile/dedupe.go` - dedupeCluster merge application

The diff modifies:
- `internal/debate/cluster.go` (NEW)
- `internal/debate/cluster_test.go` (NEW)
- `internal/debate/debate.go` (MODIFIED)
- `internal/reconcile/cluster_merge_test.go` (NEW)
- `internal/reconcile/emit.go` (MODIFIED)
- `internal/reconcile/merge.go` (MODIFIED)

Let me analyze each change for correctness issues.

**internal/debate/cluster.go (NEW)**

1. `locationKey` function - uses `\x00` separator. Fine.

2. `clusterDisplayProblem` - returns longest problem. Fine.

3. `indexClusters` - maps clusters by `FindingKey{File, Line, Problem}`. Uses `clusterDisplayProblem` which returns longest problem. This is used later in `debate.go` to look up by `FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}`. Need to check what `it.Problem` is for a gray-zone item. The comment says "a debated DisagreementItem (it.File, it.Line, it.Problem) resolves to the AmbiguousCluster whose members must be unioned." Need to check if `it.Problem` is the longest member problem.

4. `filterMergedClusters` - filters out gray-zone items whose location matches a `ClusterMerged` finding. Looks fine.

5. `applyClusterMerges` - calls `applyOneClusterMerge` for each cluster.

6. `applyOneClusterMerge`:
   - Pass 1: exact match on File+Line+Problem
   - Pass 2: drift recovery at member locations with exactly 1 unmatched record
   - Requirement: `exactHits == 0 || len(matched) < 2` returns false
   - Check `ClusterMerged` flag on any matched record → strict no-op
   - Merges via `MergeJSONFindings`
   - Sets `merged.File, merged.Line = c.File, c.Line`
   - Sets `merged.ClusterMerged = true`
   - Removes matched records, inserts merged at firstIdx

Wait, there's a potential issue: The check for `ClusterMerged` happens AFTER the `exactHits == 0 || len(matched) < 2` check. So if we have exactHits >= 1 AND len(matched) >= 2 AND one of them already has ClusterMerged=true, we return false. That's the strict no-op for re-runs. Good.

But wait - if `filterMergedClusters` already filters out the gray-zone item, then how would we get here with a record already flagged ClusterMerged? The filter operates on DisagreementItems. But what if the cluster's members span multiple records at multiple locations, and only some of those locations have been previously merged? Actually, if the cluster was already merged, all its members would be collapsed into one record with ClusterMerged=true at the cluster's canonical location. So this shouldn't happen in practice, but it's a safety check.

Actually wait - let me think more carefully. The `filterMergedClusters` filter drops gray-zone items where the cluster's file+line matches a ClusterMerged record. So if a gray-zone item is NOT filtered (because its location doesn't match any ClusterMerged record), and yet one of its member locations DOES have a ClusterMerged record... that's possible. For example, if member A was at a.go:10 and member B was at a.go:12, and somehow a.go:10 had a ClusterMerged record but a.go:12 didn't... that would be weird state. The safety check handles it.

**internal/debate/debate.go (MODIFIED)**

Key changes:
- Load `grayClusters` from `reconcile.ReadAmbiguousClusters(reviewDir)`
- Build `clusterIdx` via `indexClusters`
- Filter `df.Items` via `filterMergedClusters`
- In the outcome switch:
  - `OutcomeUnresolved` → do nothing
  - `KindGrayZone` → if `ClusterDecision == ClusterMerge`, look up cluster in `clusterIdx`
  - Default → existing ruling application

Let me check the cluster lookup. The gray-zone item is looked up by `FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}`. But the cluster is indexed by `FindingKey{File: c.File, Line: c.Line, Problem: clusterDisplayProblem(c)}`. 

This is where I need to verify: does `it.Problem` for a gray-zone DisagreementItem equal the cluster's display problem (longest member problem)?

Looking at the sprint plan, it says: "a debated DisagreementItem (it.File, it.Line, it.Problem) resolves to the AmbiguousCluster whose members must be unioned." And the code comments say this. But I need to check what actually populates `it.Problem` for gray-zone items. This is in code I can't see (in `reconcile.LoadDisagreements` / `BuildDisagreements`).

Let me check the test code. In `TestRunDebate_GrayZoneMergeUnionsFindings`:
- Findings have problems "off by one in loop" and "loop boundary error causes overflow"
- The cluster is created with the same problems
- The test expects a successful merge

So `it.Problem` must equal the longest member problem for the lookup to work. This is consistent with the comment "the same representative BuildDisagreements writes as the gray-zone radar item's Problem."

OK, let me look for other issues.

**Potential issue 1: `df.Items` modification order**
The `filterMergedClusters` modifies `df.Items` in place. But `df` is `reconcile.LoadDisagreements(reviewDir, findings)`. So we're modifying the items before the loop. This looks fine.

**Potential issue 2: Missing return value handling**
```go
grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
```
The error is silently dropped. This is documented: "A missing/corrupt ambiguous.json degrades to no gray-zone work." OK, but worth noting. Actually, the sprint plan says Option A is in scope, so a missing ambiguous.json should just mean no gray-zone work. That's fine.

**Potential issue 3: Look at the `debateOne` function signature**
`debateOne(ctx, debateDir, it, cfg, reg, cc, disp)` - I don't see the full definition but the changes don't modify it. The result `ir` must have a `ClusterDecision` field. Let me check what `ClusterMerge` is. It's referenced as `ir.ClusterDecision == ClusterMerge`. Need to check this enum.

Wait, looking at the diff again - in the modified debate.go, the comment says:
```go
if ir.ClusterDecision == ClusterMerge {
```

But `ClusterMerge` and `ClusterDecision` must be defined somewhere. They might be new types/consts added elsewhere. Let me check if they're in this diff... No, they're not shown in the diff. They must have been added in a prior diff or are imported from elsewhere. Since the diff shows `internal/debate/debate.go` being modified but not the full file, I assume these constants exist in the package already or are added in a part of the diff not shown.

Actually wait - the diff shown includes specific line numbers. Let me check what the diff shows for debate.go around the switch statement. It shows lines around 178-197. The `ClusterDecision` and `ClusterMerge` types might be in the existing ItemResult struct or added separately. Since I can't see the full file, I'll trust the diff.

**Potential issue 4: `applyRulings` vs `applyClusterMerges` order**
After the wait group, the code does:
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

Then:
```go
if len(rulings) > 0 {
    applyRulings(findings, rulings)
}
if len(mergeClusters) > 0 {
    findings, applied = applyClusterMerges(findings, mergeClusters)
    ...
}
```

So rulings are applied first, then cluster merges. This is correct because a cluster merge might union records that were also modified by single-finding rulings (though in practice, gray-zone items wouldn't be in the rulings map). The order doesn't really matter since they don't overlap.

Wait actually, there's a subtle issue. The `rulings` map excludes gray-zone items (`it.Kind != reconcile.KindGrayZone`). So there's no overlap. The order doesn't matter.

**internal/reconcile/emit.go (MODIFIED)**

Added `ClusterMerged bool` field with `json:"cluster_merged,omitempty"`. Tests verify omitempty. Good.

**internal/reconcile/merge.go (MODIFIED)**

Added `MergeJSONFindings` and helpers:

1. `MergeJSONFindings(group []JSONFinding) JSONFinding`:
   - Empty group → empty JSONFinding
   - Single → returns as-is
   - Multiple → computes merge fields

2. `unionReviewers`:
   - Iterates through `f.Reviewers` (a slice)
   - Uses map for dedup
   - Sorts alphabetically
   - Good.

3. `joinEvidence`:
   - Joins distinct non-empty evidence with " / "
   - Good.

4. `verdictRank`:
   - confirmed = 3, unverifiable = 2, refuted = 1, else = 0
   - Uses `strings.ToLower(strings.TrimSpace(verdict))`
   - Good.

5. `mergeVerification`:
   - Combines Verification blocks
   - Uses verdictRank for precedence
   - First member wins on ties
   - Unions Skeptic provenance (deduped, comma-joined)
   - Copies the chosen block (`out := *chosen`)
   - Returns nil if no member had a block

Wait, there's a subtle issue with `mergeVerification`. The `chosen` is `*Verification`, and the code does:
```go
if chosen == nil || verdictRank(v.Verdict) > verdictRank(chosen.Verdict) {
    chosen = v
}
```

So `chosen` is a pointer to one of the member's Verification blocks. Then:
```go
out := *chosen // copy
out.Skeptic = strings.Join(skeptics, ",")
return &out
```

This copies the chosen block and overrides the Skeptic. Good. But wait - `chosen` points to a member's Verification, so the copy is correct. The returned `&out` doesn't share memory with the member. Good.

But wait - is `VerdictRefuted`, `VerdictUnverifiable`, `VerdictConfirmed` from a prior diff? These should be defined in the reconcile package. They likely exist already based on the test file using them.

**Potential issue 5: `ModalCategory` and `mergeSeverity`**
`MergeJSONFindings` uses `mergeSeverity(sf)` and `modalCategory(sf)`. These must already exist in the merge.go file. They're used elsewhere so likely fine.

**Potential issue 6: longestField**
```go
Problem: longestField(sf, func(f stream.Finding) string { return f.Problem }),
Fix: longestField(sf, func(f stream.Finding) string { return f.Fix }),
```
This uses the existing `longestField` helper. OK.

**Potential issue 7: `mergeSeverity` returns what?**
`maxSev, disagreement := mergeSeverity(sf)` - returns two values. Looking at the test:
```go
assert.Equal(t, "HIGH", m.Severity)
assert.Equal(t, "MEDIUM vs HIGH", m.Disagreement)
```
So mergeSeverity returns the max severity and a disagreement annotation. This is the same as `Merge` for stream.Findings. Fine.

**Now let me check the testing more carefully.**

Test: `TestRunDebate_GrayZoneMergeDriftAndNoOverCapture`
- Findings at a.go:10 ("alpha problem text", alice, MEDIUM), a.go:12 ("longest merged problem text from a third finding", bob, HIGH), a.go:50 ("totally unrelated finding", carol, LOW)
- Cluster has member A at a.go:10 (problem "alpha problem text") and member B at a.go:12 (problem "beta problem text")
- Member A: exact match at a.go:10
- Member B: no exact match (problem drifted). a.go:12 has exactly 1 unmatched record after member A matches → drift recovery picks it up
- a.go:50 is NOT at any member location → not matched
- Result: 2 findings (merged at line 10, and unrelated at line 50)

Wait, the test sets `cluster.Findings[1].Line = 12` after creating the cluster. So member B is at a.go:12. And the gray-zone DisagreementItem is at... the cluster's location which is a.go:10 (from `c.File, c.Line` in `indexClusters`). So the radar item is at a.go:10 with `it.Problem = clusterDisplayProblem(cluster) = "longest merged problem text from a third finding"` (longest of "alpha problem text" and "beta problem text").

Wait, that's interesting. `clusterDisplayProblem` returns the longest problem among `c.Findings`. So it returns "longest merged problem text from a third finding"? No wait - the cluster members' raw problems are "alpha problem text" and "beta problem text". The longest of those is... "alpha problem text" (16 chars) vs "beta problem text" (16 chars). They're the same length. Hmm, "alpha problem text" has 17 chars and "beta problem text" has 16 chars. So the longest is "alpha problem text".

But the findings.json record has problems "alpha problem text" at a.go:10 and "longest merged problem text from a third finding" at a.go:12. The drift is on the SECOND member (bob's record at a.go:12 has a drifted problem).

So `clusterDisplayProblem(cluster)` returns "alpha problem text". And the radar item's `it.Problem` should also be "alpha problem text" (from BuildDisagreements). The cluster lookup uses `FindingKey{File: "a.go", Line: 10, Problem: "alpha problem text"}`. This matches the index key. Good.

Now in `applyOneClusterMerge`:
- Member A: a.go:10, "alpha problem text" (raw cluster problem, not drifted)
- Member B: a.go:12, "beta problem text" (raw cluster problem, drifted in findings.json)
- memberExact = {a.go:10/alpha, a.go:12/beta}
- Pass 1: matches finding at a.go:10/alpha (exactHits=1, matched[0]=true)
- Pass 2: a.go:12 is in memberLocs, finding at a.go:12/"longest merged..." is unmatched → unmatchedAtLoc["a.go\x0012"] = [1]. len == 1, so matched[1] = true.
- exactHits=1, len(matched)=2. OK.
- merged = MergeJSONFindings([f[0], f[1]])
- merged.File, merged.Line = c.File, c.Line = "a.go", 10
- merged.ClusterMerged = true
- Output: [merged, f[2]] where f[2] is the unrelated finding at a.go:50.

Expected: 2 findings, one merged with reviewers {alice, bob}, one unrelated at a.go:50. Test passes.

**Test: `TestRunDebate_GrayZoneMergeSameLineDrift`**
- Both members at a.go:10.
- Findings: alice at a.go:10/"alpha problem text", bob at a.go:10/"longest merged problem text from a third finding"
- Cluster: member A at a.go:10/"alpha problem text", member B at a.go:10/"beta problem text"
- memberExact = {a.go:10/alpha, a.go:10/beta}
- Pass 1: matches f[0] (a.go:10/alpha). exactHits=1, matched[0]=true.
- Pass 2: a.go:10 is in memberLocs. f[1] (a.go:10/"longest merged...") is unmatched. unmatchedAtLoc["a.go\x0010"] = [1]. len == 1, matched[1] = true.
- exactHits=1, len(matched)=2. OK.
- merged = MergeJSONFindings([f[0], f[1]])
- Output: [merged] (one record).

Expected: 1 finding, flagged ClusterMerged, reviewers {alice, bob}. Test passes.

**Test: `TestRunDebate_GrayZoneMergeRequiresExactAnchor`**
- Findings: alice at a.go:10/"unrelated record now at line 10", bob at a.go:12/"unrelated record now at line 12"
- Cluster: member A at a.go:10/"original member A text", member B at a.go:12/"original member B text"
- memberExact = {a.go:10/"original member A text", a.go:12/"original member B text"}
- Pass 1: no matches (problems differ). exactHits=0.
- Check: exactHits == 0 → return false. No merge.
- Output: 2 findings unchanged.

Expected: 2 findings, no merge. Test passes.

**Test: `TestRunDebate_GrayZoneMergeIsIdempotent`**
- Run 1: merge applied. After: 1 finding with ClusterMerged=true.
- Run 2: 
  - `filterMergedClusters` finds the ClusterMerged record at a.go:10.
  - The gray-zone item at a.go:10 is filtered out.
  - `df.Items` no longer contains the gray-zone item.
  - `sel.Selected` is empty (or doesn't include the gray-zone item).
  - Wait - what if the gray-zone item is the only item? Then `sel.Selected` has 0 items.
  - Test expects `res.Selected == 0`. Good.

Wait, but the test sets up `res2` from `runDebate`. The `Selected` field must be in the Result struct. Let me think... The test expects `assert.Equal(t, 0, res2.Selected)`. So the count of selected items is 0 after filtering. Good.

**Test: `TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched`**
- Verifies that ambiguous.json is not modified and adjudication.json / ambiguous.original.json are not created.
- Looks correct.

**Now let me check for issues I might have missed.**

**Issue: What if a gray-zone item's Problem doesn't match the cluster's display problem?**

If `it.Problem` for a gray-zone DisagreementItem is NOT the cluster's display problem (longest member problem), the cluster lookup would fail silently. The `oc.clusterMerge` would not be set, and the cluster wouldn't be merged. This could happen if:
1. The cluster's members' problems change between radar build and cluster merge (unlikely)
2. The radar item uses a different representative than clusterDisplayProblem

Looking at the test for `TestRunDebate_GrayZoneMergeDriftAndNoOverCapture`, the cluster has member problems "alpha problem text" (17 chars) and "beta problem text" (16 chars). The display problem would be "alpha problem text". The radar item is at a.go:10 with it.Problem = ?

Actually, looking at the test setup, I see that the cluster has `c.File = "a.go"` and `c.Line = 10` (grayCluster's first 3 args after id). The radar item would be at c.File, c.Line with the display problem.

But wait - in the drift test, member B is at a.go:12 (after `cluster.Findings[1].Line = 12`). The cluster itself is still at a.go:10. So the radar item is at a.go:10 with display problem "alpha problem text".

When the drift recovery runs, it looks for member locations (a.go:10 and a.go:12). It correctly identifies alice's record at a.go:10/alpha (exact match) and bob's record at a.go:12/longest (drift recovery). Good.

**Issue: Empty member findings?**

What if `c.Findings` is empty? Then `memberExact` is empty, `memberLocs` is empty. Pass 1: no matches. exactHits=0 → return false. Good.

What if `c.Findings` has only 1 member? Same as above - no exact matches (because the member problem might match or not), but even if it does, len(matched)=1 < 2 → return false. Wait actually, drift recovery could add more matches... let me think.

If a cluster has 1 member at a.go:10, and there are 2 records at a.go:10 with different problems... memberExact has 1 entry. Pass 1: matches the one with the right problem (exactHits=1). Pass 2: at a.go:10, there's 1 unmatched record → matched. Now len(matched)=2. But this would absorb an unrelated record at the same location.

Hmm, but the sprint plan says clusters are pairs (today). And the code is "Forward-compatible with Epic 13.2 (DBSCAN produces variable-size clusters)." So we should handle N-member clusters.

But what about a 1-member cluster? That's not a gray-zone cluster really. A gray-zone cluster by definition has ≥2 ambiguous findings. So 1-member clusters shouldn't exist. But the code's safety check `< 2` handles it.

Actually wait, what about a 3-member cluster? Let me check:
- memberExact = 3 entries
- Pass 1: matches as many as possible by exact
- Pass 2: for each member location, if there's exactly 1 unmatched record, match it
- If at least 1 exact hit AND total matched >= 2, proceed

This should work for 3+ members.

**Issue: Same-location multiple records with multiple member locations**

What if member A is at a.go:10 and member B is at a.go:10 (same location), and there are 3 records at a.go:10?
- Pass 1: matches records with exact member problems.
- Pass 2: a.go:10 has N unmatched records. If N == 1, match it. If N >= 2, skip.

So if Pass 1 matches 0 and Pass 2 would need to match 3, exactHits=0 → return false. But this might miss a case where all members drifted.

Actually, this is fine. The exactHits >= 1 requirement ensures at least one member's problem is preserved. If all members' problems drifted, the cluster can't be safely anchored.

**Issue: What about `pathWarning` and `pathSuggestion`?**

The comment says: "the Epic 5.0 path-validation fields carry the first member's — the members are co-located, so their path status is identical."

This is an assumption. If members are at different locations (e.g., a.go:10 and a.go:12), their path status could differ. Wait - they're different files/lines. The "first member" is the one at group[0].File/Line, which is the cluster's location (a.go:10 after `merged.File, merged.Line = c.File, c.Line`).

Actually, `mergeVerification` and other functions use `group[0].PathValid` etc. But before the `merged.File, merged.Line = c.File, c.Line` assignment, the first member is at group[0]'s location. The group is built by iterating `findings` in order, so group[0] is the first matched finding in findings order.

Hmm, this is a bit muddled. Let me re-read:

```go
group := make([]reconcile.JSONFinding, 0, len(matched))
firstIdx := -1
for i := range findings {
    if matched[i] {
        if findings[i].ClusterMerged {
            return findings, false
        }
        if firstIdx < 0 {
            firstIdx = i
        }
        group = append(group, findings[i])
    }
}

merged := reconcile.MergeJSONFindings(group)
merged.File, merged.Line = c.File, c.Line
merged.ClusterMerged = true
```

So `group[0]` is the first matched finding (by findings index). The merged record's location is then overwritten with `c.File, c.Line`. So `group[0].PathValid` etc. could be from any of the matched findings, not necessarily the one at the cluster's canonical location.

This is a minor issue. The comment says "the members are co-located, so their path status is identical." But they're not necessarily co-located - the drift test has members at different lines (a.go:10 and a.go:12). However, after the merge, the merged record is at c.File/c.Line (a.go:10). The PathValid should reflect the cluster's location, not group[0]'s location.

But wait, in the drift test, both members' findings.json records are at a.go:10 and a.go:12. Their PathValid might differ. group[0] is alice's record at a.go:10 (because it's first in findings). So merged.PathValid = alice's PathValid (from a.go:10). And merged.File/Line = c.File/Line = a.go:10. So they match.

But what if group[0] is bob's drifted record at a.go:12 (because alice was filtered out for some reason)? Then merged.PathValid = bob's PathValid (from a.go:12), but merged.File/Line = a.go:10. Mismatch!

This is a subtle correctness issue. But it's edge-case-y. The drift recovery only happens when one member drifts, and the other matches exactly. So the exactly-matched one should be at the canonical location (c.File/c.Line), since the cluster's File/Line is set from one of its members.

Hmm, actually I think c.File/c.Line IS one of the member locations. Let me check the cluster setup in the test:
```go
cluster := grayCluster("amb-1", "a.go", 10, ...)
```
And `grayCluster` sets `File: file, Line: line` for the cluster. So c.File/c.Line matches member A's location.

But what about a cluster where the canonical location doesn't match any member? E.g., if the cluster was created with a different File/Line than its members. That would be a bug in cluster creation, not in this code. So I'll assume c.File/c.Line is always one of the member locations.

So group[0] might not be at c.File/c.Line (if the exactly-matching member is not group[0]). But since c.File/c.Line is always one of the member locations, and the cluster's File/Line is one of the members', and the drift recovery picks the lone unmatched at that location, the PathValid should be consistent... 

Actually, this is getting complicated. Let me just flag it as a low-priority issue if I think it's worth it.

Wait, actually - if a member's PathValid is determined by the file/line it was originally at, and that member has been merged into a record at a different file/line, then the PathValid might not match the new file/line. But `merged.File, merged.Line = c.File, c.Line` ensures the merged record is at the cluster's canonical location. So if group[0] is at a different location than c.File/c.Line, merged.PathValid would be wrong.

But this is highly edge-case. Let me look at what `applyOneClusterMerge` does with group[0]:

Actually, group[0] is the FIRST matched finding. The matched findings could be at various locations. The drift recovery picks drifted records at member locations. The exact match picks records with matching problems.

If member A is at a.go:10 (matches exactly) and member B is at a.go:12 (drifted), then:
- group = [record at a.go:10, record at a.go:12]
- group[0] = record at a.go:10
- merged.File, merged.Line = c.File, c.Line = a.go:10
- merged.PathValid = record at a.go:10's PathValid
- Match.

If only the drifted member is matched (no exact), then... but exactHits >= 1 is required. So this case doesn't happen.

If the exact match is at a different location than c.File/c.Line... but c.File/c.Line is one of the member locations, and the exact match is at a member's location with the member's problem. So they should align.

Hmm, actually, c.File/c.Line is the cluster's location, which is set when the cluster is created. The cluster's File/Line might be different from all members' File/Line in theory, but in practice they should match one member. I'll leave this as a low-priority concern.

**Issue: `mergeVerification` mutates the chosen Verification block?**

```go
out := *chosen // copy so the source finding's block is not mutated
out.Skeptic = strings.Join(skeptics, ",")
return &out
```

This copies `*chosen` into `out`, then modifies `out.Skeptic`. Since `out` is a copy, the original is not mutated. Good.

But wait - `chosen` is a pointer to a member's Verification block. If the member's Verification block is shared (e.g., shallow-copied), modifying `out.Skeptic` wouldn't affect the member. But if `out := *chosen` copies the struct, then `out.Skeptic` is a separate string from `chosen.Skeptic`. Modifying `out.Skeptic` doesn't affect `chosen.Skeptic`. Good.

**Issue: `mergeVerification` loses Notes from non-winning members?**

The code does:
```go
out := *chosen // keeps chosen's Verdict, Notes, ChallengeSurvived, etc.
out.Skeptic = strings.Join(skeptics, ",")
```

So Notes from non-winning members is lost. This is intentional - the chosen member's notes represent the verdict. But what if the confirmed verdict's notes are less informative than the refuted verdict's notes? The comment says "ties resolve to the first member" but doesn't address notes preservation.

This is a minor design choice. Not necessarily a bug, but worth noting.

**Issue: What about `VerdictRefuted` text comparison?**

```go
case VerdictRefuted:
    return 1
```

The `verdictRank` function uses `strings.ToLower(strings.TrimSpace(verdict))`. But the case values are direct string constants (e.g., `VerdictConfirmed`). These should be lowercase already (e.g., "confirmed"). But the function lowercases them anyway, so it's safe.

Wait, what if `VerdictRefuted` is defined as "Refuted" (capitalized)? Then `strings.ToLower("Refuted")` = "refuted", which matches. Good.

**Issue: `applyRulings` is called BEFORE `applyClusterMerges`**

What if a single-finding ruling and a cluster merge affect the same record? The single-finding ruling would be applied first, then the cluster merge would absorb that record into the merged one. But wait - gray-zone items are excluded from `rulings`, so no overlap.

But what about a non-gray-zone item that happens to be at the same location as a cluster member? The cluster merge would absorb that record too (if it's matched). The single-finding ruling would have been applied first, modifying the record. Then the cluster merge absorbs it.

Hmm, but the cluster's File/Line is at the gray-zone item's location. The single-finding rulings are for non-gray-zone items. So they shouldn't overlap in practice.

But what if a non-gray-zone finding happens to be at the same File/Line/Problem as a cluster member? That would be a separate finding, not a cluster member. The cluster merge would only affect records at cluster member locations. The single-finding ruling would affect a record at (File, Line, Problem) matching the non-gray-zone item. If these are different (different Problem), no overlap.

OK, I think this is fine.

**Issue: `filterMergedClusters` modifies `df.Items` in-place**

This modifies the disagreements struct's items slice. Is this a problem? `df` is a local variable, so it's fine. But the original `LoadDisagreements` might return a slice that should not be modified. Let me check the call site:

```go
df := reconcile.LoadDisagreements(reviewDir, findings)
df.Items = filterMergedClusters(df.Items, findings)
```

`filterMergedClusters` returns a new slice. So `df.Items` is reassigned. The original `df.Items` is not modified in place (well, the underlying array might be shared, but we don't write to it). Actually, `filterMergedClusters` builds a new slice with `make`, so the underlying array is new. Good.

**Issue: What if `df.Items` is nil or empty?**

`filterMergedClusters` handles empty items gracefully (returns empty result). Good.

**Issue: `clusterIdx` is built from `grayClusters` even if there are no gray-zone items**

If `grayClusters` is empty, `clusterIdx` is an empty map. The lookup in the switch would fail (no match). Good.

**Issue: Concurrent access to `clusterIdx`**

The switch is inside the goroutine. Multiple goroutines read `clusterIdx`. Maps in Go are safe for concurrent reads (no writes after construction). So this is fine.

**Issue: `applyClusterMerges` is called sequentially**

The cluster merges are applied sequentially, not concurrently. But this is fine - it's after the parallel debate phase.

**Issue: `applyOneClusterMerge` modifies `findings` slice**

It builds a new `out` slice and returns it. The caller's `findings` variable is reassigned. Good - no aliasing issues.

**Issue: What about `evidence` field preservation in `MergeJSONFindings`?**

`joinEvidence` joins distinct non-empty evidence strings with " / ". But the original `Merge` function uses different logic. Let me check... actually I don't have visibility into the original Merge function's evidence handling. The new `joinEvidence` is specific to MergeJSONFindings.

**Issue: `category` field**

`modalCategory(sf)` returns the modal category. If members have different categories, the modal one wins. This is the same as `Merge`. Good.

**Issue: `estMinutes` field**

`maxEstMinutes(sf)` returns the max. Same as `Merge`. Good.

**Issue: Confidence**

`confidenceFor(len(reviewers))` returns the confidence based on reviewer count. With 2 reviewers, returns HIGH (ConfHigh). The test confirms this. Good.

**Issue: `PathValid`, `PathWarning`, `PathSuggestion`**

These are set to `group[0]`'s values. As discussed, this could be wrong if group[0] is not at the cluster's canonical location. But in practice, group[0] should be at or near the cluster's location.

Wait, let me think again. The cluster's File/Line is one of the member locations. The drift recovery picks drifted records at member locations. If the cluster's canonical member (the one at c.File/c.Line) drifts, then no exact match exists at that location, and exactHits would be 0 → no merge. So the cluster's canonical member must have an exact match (or the merge fails).

Therefore, group[0] might or might not be at c.File/c.Line. If the first matched finding is at a different member's location (e.g., the drifted one), then merged.PathValid would be from that location.

Hmm, but the order of iteration in `applyOneClusterMerge` is by findings index. The exact matches come first in the matched set (since they're identified first in pass 1). Then drift recovery adds more. group is built by iterating findings in order, so the first matched finding is the first exact match (or drift recovery match) by findings index.

In the drift test:
- findings: [alice at a.go:10 (exact match), bob at a.go:12 (drift match)]
- group[0] = alice's record at a.go:10
- c.File/c.Line = a.go:10
- Match.

What if bob's record is at index 0 (before alice's)? Then group[0] = bob's record at a.go:12. But the cluster's canonical is a.go:10. Mismatch.

But in practice, the findings order should reflect the source order, and alice's record (member A) is at the cluster's location. So this is unlikely. But it's a possibility.

This is a subtle issue. Let me see if it's worth flagging.

Actually, I think the comment in the code addresses this: "the Epic 5.0 path-validation fields carry the first member's — the members are co-located, so their path status is identical." But "co-located" is the assumption - members at different lines are NOT co-located.

I'll flag this as a low-priority correctness issue.

Actually wait, let me re-read the comment more carefully:

"Verification is combined by mergeVerification (verdict precedence confirmed > unverifiable > refuted, skeptic provenance unioned) and the Epic 5.0 path-validation fields carry the first member's — the members are co-located, so their path status is identical."

The comment says "the members are co-located"
</think>
