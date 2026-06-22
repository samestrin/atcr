We are given a diff that adds new files and modifies existing ones to implement Epic 6.1: inline application of judge gray-zone cluster rulings.

We must review the diff and output any issues in the specified TD_STREAM format.

Scope: Only issues in files/areas directly related to the sprint plan (internal/debate and internal/reconcile) are in scope.

Let's break down the diff:

1. New file: `internal/debate/cluster.go`
   - Contains helper functions for gray-zone cluster processing: locationKey, clusterDisplayProblem, indexClusters, filterMergedClusters, applyClusterMerges, applyOneClusterMerge.

2. New file: `internal/debate/cluster_test.go`
   - Tests for the cluster.go functions and the runDebate function with gray-zone merges.

3. Modified file: `internal/debate/debate.go`
   - Changes in runDebate:
     a. Added loading of grayClusters and building clusterIdx.
     b. Added filtering of already merged clusters (filterMergedClusters) for idempotency.
     c. Modified the itemOutcome struct to include clusterMerge and cluster.
     d. Changed the logic for applying verdicts: now handles gray-zone items separately (they don't go into the rulings map) and captures cluster merges.
     e. After the worker pool, applies the cluster merges via applyClusterMerges and logs if some merges couldn't be applied.

4. New file: `internal/reconcile/cluster_merge_test.go`
   - Tests for the MergeJSONFindings function and related helpers.

5. Modified file: `internal/reconcile/emit.go`
   - Added a new field `ClusterMerged` to the JSONFinding struct.

6. Modified file: `internal/reconcile/merge.go`
   - Added the MergeJSONFindings function and helper functions (unionReviewers, joinEvidence, verdictRank, mergeVerification) to support merging of JSONFinding records for gray-zone clusters.

We are to look for issues in the changed code that fall into the categories: security, error-handling, performance, correctness, maintainability.

Let's go through the changes and note potential issues:

### internal/debate/cluster.go

1. In `filterMergedClusters`:
   - We are building a set of merged locations from findings that have `ClusterMerged` set.
   - Then we filter out gray-zone items that are at a merged location.
   - However, note that the function returns the items unchanged if there are no merged locations (which is an optimization). This is acceptable.

2. In `applyOneClusterMerge`:
   - The function does two passes to match cluster members to findings.
   - It requires at least one exact hit (to anchor the cluster) and at least two matched records (to have something to merge).
   - If there are no exact hits or less than two matched records, it returns without merging.
   - It also checks if any matched record already has `ClusterMerged` set (to avoid re-merging) and returns without merging in that case.

   Potential issue: 
   - The function uses `firstIdx` to record the index of the first matched record, which will be the location where the merged record is placed.
   - However, note that the merged record's file and line are set to `c.File` and `c.Line` (the cluster's canonical location). This might be different from the location of the first matched record? 
     But the cluster's canonical location is taken from the radar item (which is the same for all members? Actually, the cluster is built from two members that are co-located? Not necessarily: the cluster might have members at different locations? 
     Looking at the test: the cluster is built with two members at the same file and line? Actually, in the test `grayCluster` function, both members have the same file and line. 
     However, the comment in `locationKey` says: "a member may have been further-merged with a third finding (≥0.7 similarity), replacing its problem text via longestField, so the full triple would not match between the cluster's raw member and the reconciled record."

   - The function `applyOneClusterMerge` uses the cluster's `File` and `Line` (from the cluster struct) for the merged record. This is set from the radar item (which is the same for the cluster). 
     But note: the cluster's `File` and `Line` are set when the cluster is built (from the ambiguous.json). In the test, the cluster is built with a specific file and line.

   - However, what if the cluster members are not at the same location? The sprint plan says: "Union ALL cluster members, not a hardcoded 2-finding pair." and the cluster might have members at different locations? 
     But the radar item (which is the gray-zone disagreement item) has a single file and line. The cluster is built from the ambiguous.json, which for a gray-zone cluster has a single file and line (the radar item's location). 
     The members of the cluster might have been further merged with third findings, so their locations might have changed? 

   - The function `locationKey` is used to group by file and line. The cluster's `File` and `Line` are the radar item's location. The members might be at different locations? 
     Actually, the cluster is built from the ambiguous.json, which is produced by the reconcile step. The ambiguous.json contains clusters that are groups of findings that are similar. 
     The cluster's `File` and `Line` in the ambiguous.json are the location of the radar item (which is the same for the cluster). 

   - However, the members of the cluster (in `c.Findings`) are stream.Finding objects, which have their own File and Line. 
     The function `applyOneClusterMerge` uses the cluster's `File` and `Line` for the merged record, but the members might be at different locations? 
     This would be incorrect because we are unioning findings that are at different locations and putting them at one location.

   - Looking at the sprint plan: "Union ALL cluster members, not a hardcoded 2-finding pair." and "The apply unions findings.json records at the cluster's location(s): merged PROBLEM = longest, severity = max, reviewer set = union, verification block preserved/merged."

   - The plan says "at the cluster's location(s)" (plural). But the current code puts the merged record at the cluster's canonical location (single file and line). 

   - This is a potential issue: if the cluster has members at multiple locations, we are putting the merged record at only one location (the radar item's location) and dropping the others? 
     But note: the function `applyOneClusterMerge` is designed to union the members into one record and place it at the cluster's canonical location. 
     The other locations are then dropped (because we are removing the matched records and putting one merged record in their place? Actually, we are removing all matched records and putting one merged record at the first matched record's index? 
     But we set the merged record's file and line to the cluster's canonical location, which might not match any of the members? 

   - Let me clarify: 
        merged.File, merged.Line = c.File, c.Line

   - And then we are placing this merged record at the position of the first matched record (which is at the location of that matched record). 
     But we are overwriting the file and line of that matched record with the cluster's canonical location.

   - This is incorrect if the cluster's canonical location (from the radar item) is different from the location of the first matched record.

   - However, note that the radar item's location is the same for the cluster, and the cluster members are supposed to be similar and co-located? 
     The sprint plan does not explicitly say they are co-located, but the radar item is a single disagreement item that represents the cluster. 
     In the current system, the radar item is built from a pair of findings that are similar and co-located? 

   - Looking at the test: the cluster is built with two members at the same file and line. 
     And the test for drift: one member's problem text drifted, but the location remains the same.

   - So it seems the assumption is that the cluster members are co-located (same file and line). 
     The sprint plan says: "Union ALL cluster members, not a hardcoded 2-finding pair." meaning the cluster might have more than two members, but they are still co-located? 

   - The function `locationKey` is used to group by file and line, and the cluster's `File` and `Line` are the same for all members? 
     Actually, the cluster's `File` and `Line` are set from the radar item, and the radar item is built from a pair of findings that are co-located (same file and line). 
     Then, when a member is further merged with a third finding, that third finding might be at a different location? 
     But the sprint plan says: "Third-finding wrinkle handled inline: when a gray member was already merged with a third finding (≥0.7), union all records the cluster spans at its location(s)."

   - This implies that the cluster might span multiple locations? 

   - However, the current code in `applyOneClusterMerge` does not handle multiple locations. It uses the cluster's canonical location (single file and line) for the merged record.

   - This is a potential correctness issue if the cluster spans multiple locations.

   - But note: the sprint plan says in the clarifications: "Union ALL cluster members, not a hardcoded 2-finding pair." and then "Third-finding wrinkle handled inline: when a gray member was already merged with a third finding (≥0.7), union all records the cluster spans at its location(s)."

   - And then: "Not punted to the authored-adjudication path."

   - The current code does not union at multiple locations; it puts the merged record at one location.

   - This is a deviation from the plan.

   - However, let's look at the test: 
        TestRunDebate_GrayZoneMergeDriftAndNoOverCapture
     Here, we have:
        findings: 
          a.go:10 - alpha problem text (alice)
          a.go:12 - longest merged problem text from a third finding (bob)   [this is the drifted member?]
          a.go:50 - totally unrelated finding (carol)
        cluster: 
          amb-1 at a.go:10, with two members:
            member A: a.go:10, "alpha problem text", alice
            member B: a.go:12, "beta problem text", bob   [note: the raw problem text for B is "beta problem text", but at a.go:12 we have a drifted record with problem text "longest merged problem text from a third finding"]

     The test expects that the two members (a.go:10 and a.go:12) are unioned into one record, and the unrelated finding at a.go:50 is left alone.

     The merged record is placed at a.go:10? 
        In the test: 
          merged = f[0] (which is at line 10) and unrelated = f[1] (which is at line 50) -> wait, no: 
          The test says: 
            var merged, unrelated *reconcile.JSONFinding
            for i := range f {
                if f[i].Line == 50 {
                    unrelated = &f[i]
                } else {
                    merged = &f[i]
                }
            }
          So the merged record is at line 10? and the unrelated at 50? 
          But note: the findings after merge should have two records: one merged (from the two members) and the unrelated.

          The merged record is at line 10? But the cluster's canonical location is a.go:10 (from the radar item). 
          And the merged record's file and line are set to c.File and c.Line (which is a.go:10). 
          So the merged record is placed at a.go:10, and the record that was at a.go:12 is removed? 

          How? 
          In the function applyOneClusterMerge:
            We are building a group of matched records (which are the two members: one at a.go:10 and one at a.go:12, but note the one at a.go:12 is matched by drift recovery).
            Then we remove all matched records and put one merged record at the position of the first matched record (which is the one at a.go:10, because we iterate in order).

          So the merged record is placed at a.go:10, and the record at a.go:12 is removed.

          This matches the test: the merged record is at line 10 and the unrelated at 50.

          But what if the cluster's canonical location was a.go:12? 
          Then we would set the merged record's file and line to a.go:12, and place it at the position of the first matched record (which is a.go:10) -> so we would put a record with file=a.go:12, line=12 at the position of the a.go:10 record? 
          That would be wrong because we are changing the location of the merged record to a.go:12 but putting it in the slot that was for a.go:10.

          However, note that the cluster's canonical location is taken from the radar item, which is built from the two members. 
          In the test, the radar item is built from the two members? 
          The cluster is built by the function `grayCluster` which takes an id, file, line, and then two findings. 
          The file and line passed to `grayCluster` are the radar item's location.

          In the test, we call:
            cluster := grayCluster("amb-1", "a.go", 10, ...)

          So the radar item is at a.go:10.

          Therefore, the cluster's canonical location is a.go:10.

          So the current code is consistent: the merged record is placed at the radar item's location.

          But what if the radar item's location is not the same as any of the members? 
          That should not happen because the radar item is built from the two members (which are at specific locations). 
          The radar item's location is the location of the disagreement, which is the same as the location of the two members? 
          In the current system, the radar item for a gray-zone cluster is built from two findings that are co-located? 

          The sprint plan does not explicitly say, but the test assumes co-location.

          Given the tests, it seems the design assumes that the cluster members are co-located (same file and line) or at least the radar item's location is the same as the location of the members (after accounting for drift?).

          However, the sprint plan says: "union all records the cluster spans at its location(s)" (plural). 
          This suggests that the cluster might span multiple locations and we should union them and put the merged record at each location? 
          But that doesn't make sense because we are collapsing multiple findings into one.

          Alternatively, it might mean that we union the records and put the merged record at each location where a member was? 
          But then we would have multiple merged records? 

          The sprint plan says: "the surviving merged JSONFinding" (singular).

          And the idempotency marker is a finding-level flag.

          So it seems we are to produce one merged record.

          The plan says: "union all records the cluster spans at its location(s)" meaning we take the records from all the locations and union them into one record, and then put that one record at one of the locations? 
          But which location? 

          The current code uses the radar item's location.

          The sprint plan says: "The apply unions findings.json records at the cluster's location(s): merged PROBLEM = longest, severity = max, reviewer set = union, verification block preserved/merged."

          This is ambiguous.

          However, note the test: 
            TestRunDebate_GrayZoneMergeSameLineDrift
          Here, two members are co-located at the same line (a.go:10). 
          One member's problem text drifted (so it no longer matches the exact problem text) but is the sole record at that line after the exact match.

          The test expects one merged record at a.go:10.

          So the current implementation matches the tests.

          Given that the tests pass and the sprint plan's clarifications (from the recorded 2026-06-21) say:
            "Option A locked. Debate applies a gray-zone `merge` decision **in-memory to `findings.json`** — union the cluster's member findings by location and re-emit inside debate's existing atomic group (debate.json + findings.json + manifest.json)."

          And: "Union ALL cluster members, not a hardcoded 2-finding pair."

          And: "Idempotency (AC4): a dedicated finding-level flag (e.g. `cluster_merged: true`) on the surviving merged `JSONFinding`"

          It seems the current implementation is as designed.

          Therefore, I do not see a correctness issue in the location handling.

   - However, there is a potential issue in the drift recovery: 
        We are grouping unmatched records by location. For a given location, if there is exactly one unmatched record, we take it. 
        If there are two or more, we take none.

        This is to avoid absorbing an unrelated co-located finding.

        But note: what if there are three unmatched records at a location? 
        We take none, which is safe.

        What if there are two unmatched records at a location and they are both members of the cluster? 
        Then we should take both? But we don't: we take none.

        This is a problem.

        Example: 
          Cluster has two members, both at location L.
          In findings, at location L we have two unmatched records (the two members) and no other records.
          Then in the drift recovery pass, we see two unmatched records at location L -> we take none.
          Then we have zero matched records from drift recovery, and we had zero exact hits (because the problem text drifted) -> so we fail the anchor check (exactHits==0) and return without merging.

        This is incorrect.

        The test `TestRunDebate_GrayZoneMergeSameLineDrift` avoids this because one member is matched exactly (so exactHits=1) and the other is recovered by drift (because at that location there is exactly one unmatched record).

        But if both members have drifted, then we have zero exact hits and two unmatched records at the location -> we take none in drift recovery -> then we have zero matched records -> fail.

        We require at least one exact hit to anchor the cluster.

        The sprint plan says: "At least one exact hit is required to anchor the cluster (so a cluster whose members were all refuted/removed cannot union purely fallback-matched, possibly unrelated, records)."

        So this is by design.

        Therefore, the drift recovery is only for members that have drifted but are the sole unmatched record at their location.

        This is acceptable.

   - Another issue: 
        In the drift recovery, we are only considering unmatched records that are at a location that is in the cluster's member locations (memberLocs). 
        This is correct.

   - The function `applyOneClusterMerge` returns early if any matched record already has `ClusterMerged` set. 
        This is for idempotency: we don't want to re-merge an already merged cluster.

        However, note that we have already filtered out gray-zone items that are at a merged location (via `filterMergedClusters`). 
        So we should not see a gray-zone item that is at a merged location. 
        But note: the filtering is done on the disagreement items (the radar items). 
        It is possible that a cluster has been merged, but then one of its members was further modified (e.g., by a single-finding verdict) so that it no longer matches the cluster? 
        Then the radar item might still be present? 

        However, the `filterMergedClusters` function only removes gray-zone items if the location (file, line) of the item is in the set of merged locations. 
        It does not check the problem text. 

        So if the cluster has been merged, but then the problem text of the merged record changed (so it no longer matches the radar item's problem text), then the radar item would not be filtered out? 
        But note: the radar item's problem text is the longest problem text of the cluster members at the time of the radar generation. 
        After merging, the merged record's problem text is the longest of the members (which is the same as the radar item's problem text? 
        Because the radar item's problem text is computed by `clusterDisplayProblem` which returns the longest problem text among the cluster's members. 
        And the merged record's problem text is set to the longest problem text of the group (which is the same set of members). 
        So the problem text should be the same.

        Therefore, the radar item's problem text should match the merged record's problem text? 
        But note: the merged record's problem text is set to the longest problem text of the group, which is the same as the radar item's problem text (because the radar item's problem text is computed the same way). 

        So the radar item should still match the merged record by exact File+Line+Problem? 
        Then the gray-zone item would be filtered out by `filterMergedClusters` because the location is merged.

        Therefore, we should not encounter a gray-zone item that is at a merged location.

        However, what if the cluster was merged, but then the merged record was subsequently modified by a single-finding verdict? 
        Then the problem text might have changed? 
        But note: the merged record is a findings.json record. 
        The single-finding verdicts are applied to findings.json records. 
        It is possible that after the cluster merge, a single-finding verdict is applied to the merged record? 
        But the merged record is now a single record representing the cluster. 
        The single-finding verdict would apply to that record.

        Then, in a subsequent debate run, the radar item for the cluster would be built from the current findings.json? 
        But note: the radar item is built from the ambiguous.json, which is produced by the reconcile step from the sources/ and the existing findings.json? 
        Actually, the ambiguous.json is produced by the reconcile step (which runs before debate) and it uses the current findings.json as input? 
        The sprint plan does not specify the exact flow, but from the context:

          The debate stage happens after verify, and it uses the findings.json that has been updated by verify.

          The reconcile step (which produces ambiguous.json) is run before debate? 
          Actually, the sprint plan says: "The guard at internal/debate/debate.go deliberately excludes gray-zone items from the per-finding rulings map"

          And the debate function loads disagreements from the reconcile-time disagreements.json snapshot? 
          But note: in the modified debate.go, we see:

            df := reconcile.LoadDisagreements(reviewDir, findings)

          This loads the disagreements from the reconcile step (which is based on the current findings.json?).

          Then we load the ambiguous clusters from the reconcile step (which is also based on the current findings.json?).

          So if the merged record has been modified by a single-finding verdict, then the ambiguous.json might not contain the cluster anymore? 
          Because the reconcile step would not see the two members as similar? 

          Therefore, it is complex.

        Given the complexity, and since we have the `filterMergedClusters` that removes gray-zone items at merged locations, 
        and the applyOneClusterMerge also checks for already merged records (as a belt-and-suspenders), 
        I think it is safe.

        However, note: the check in applyOneClusterMerge for `findings[i].ClusterMerged` is done on the matched record. 
        If the record is already merged, we return without merging. 
        This is safe.

   - Performance: 
        The function `applyOneClusterMerge` does two passes over the findings and builds several maps. 
        This is O(n) and acceptable.

### internal/debate/debate.go

1. In the modified runDebate function:
   - We load grayClusters: `grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)`
        We ignore the error. This is dangerous because if there is an error reading the ambiguous.json, we proceed as if there are no gray-zone clusters.
        The comment says: "A missing/corrupt ambiguous.json degrades to no gray-zone work." 
        So it is acceptable to ignore the error and treat it as an empty list.

   - We build clusterIdx: `clusterIdx := indexClusters(grayClusters)`
        This function panics if there is a duplicate key? 
        Because it does: `out[FindingKey{...}] = c` 
        If two clusters have the same FindingKey (file, line, problem), then the second will overwrite the first.

        Is it possible to have two clusters with the same file, line, and problem? 
        The problem is the longest problem text of the cluster. 
        Two different clusters at the same location would have to have the same longest problem text? 
        This is possible if they share the same longest problem text? 
        But note: the cluster's problem text is the longest problem text of its members. 
        Two different clusters at the same location would have different sets of members, but it is possible that the longest problem text in each set is the same.

        Example: 
          Cluster A: members with problems ["short", "medium long"]
          Cluster B: members with problems ["medium long", "another medium long"]
          Both have longest problem text "medium long".

        Then we would have two clusters with the same key (file, line, "medium long"). 
        The indexClusters function would overwrite the first with the second.

        This is a problem because we lose one cluster.

        We should change the indexClusters function to handle multiple clusters per key? 
        But note: the radar item is built from the cluster's key (file, line, problem). 
        How are radar items generated? 
        The radar item for a cluster is a DisagreementItem with:
          File: cluster.File
          Line: cluster.Line
          Problem: clusterDisplayProblem(cluster)   [which is the longest problem text]

        So two different clusters at the same location with the same longest problem text would produce two radar items with the same file, line, and problem? 
        Then the debate system would see two disagreement items with the same key? 
        How are disagreement items stored? 
        The disagreements.json is a list of DisagreementItem. 
        It is possible to have two items with the same file, line, and problem? 

        Then in the debate function, we have:
          sel := picker.Pick(df.Items, op.MaxParallel, op.Seed)

        The picker might pick both? 

        Then we would process both items. 
        For each item, we look up in clusterIdx by the key (file, line, problem) and we would get the same cluster (the last one stored) for both.

        This is incorrect.

        We need to change the data structure to allow multiple clusters per key? 
        But note: the radar item does not carry an cluster ID. 
        How do we know which cluster the radar item belongs to? 

        The sprint plan does not specify. 
        However, the test uses a cluster with an ID. 
        The ambiguous.json contains clusters that have an ID. 
        The radar item should be associated with the cluster by more than just file, line, and problem? 

        Looking at the test: 
          The cluster is built with an ID: "amb-1"

        And the radar item is built from the cluster? 
        Actually, the radar item is the ambiguous.json entry? 
        No, the ambiguous.json contains the clusters. 
        The radar item is a DisagreementItem that is generated from the cluster? 
        How? 
        The function `reviewDirWithGray` writes the clusters to ambiguous.json. 
        Then the debate function loads the disagreements (which are from disagreements.json) and the ambiguous clusters (from ambiguous.json). 
        The disagreements.json is loaded by `reconcile.LoadDisagreements` and it is supposed to contain the gray-zone items? 
        How are the gray-zone items generated? 
        They are generated by the reconcile step? 
        The reconcile step produces disagreements.json and ambiguous.json from the same input? 
        Then the gray-zone items in disagreements.json should correspond to the clusters in ambiguous.json? 
        But note: the disagreements.json contains DisagreementItem, which has:
          Outcome, Kind, File, Line, Problem, Reviewer, etc.

        And the ambiguous.json contains AmbiguousCluster, which has:
          ID, File, Line, Similarity, Findings (which are stream.Finding)

        How do we link a DisagreementItem to an AmbiguousCluster? 
        The current code in debate.go uses:
          if it.Kind == reconcile.KindGrayZone {
              // ... 
              if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok {
                  ...
              }
          }

        So it is using the triple (file, line, problem) to look up the cluster.

        Therefore, if two clusters share the same triple, we have a problem.

        We must change the lookup to use something more unique. 
        The cluster has an ID. 
        The DisagreementItem does not have an ID. 
        How is the DisagreementItem generated? 
        It is generated by the reconcile step. 
        We should change the reconcile step to put the cluster ID in the DisagreementItem? 
        But that is out of scope for this review? 
        However, we are only allowed to flag issues in the changed code.

        Since we are not changing the reconcile step in this diff, we must work with what we have.

        Alternatively, we can change the indexClusters to return a map from key to a list of clusters? 
        Then when we have a disagreement item, we would have to choose the correct cluster from the list? 
        But how? 
        We don't have any other information in the disagreement item to disambiguate.

        Therefore, this is a latent bug that might occur if two clusters at the same location have the same longest problem text.

        Given the low probability, but possible, we should flag it as a correctness issue.

        However, note: the problem text is the longest problem text of the cluster. 
        It is unlikely that two different clusters at the same location would have the exact same longest problem text? 
        But it is possible.

        We'll flag it as a MEDIUM correctness issue.

2. In the itemOutcome struct, we added:
        clusterMerge bool
        cluster      reconcile.AmbiguousCluster

   - We are storing the entire cluster. 
   - This is acceptable.

3. In the switch statement for the item result:
        case ir.Outcome == OutcomeUnresolved:
            // An unresolved item settles nothing — no application either way.
        case it.Kind == reconcile.KindGrayZone:
            // Epic 6.1: a gray-zone ruling is a cluster-level decision, not a
            // per-finding verdict, so it never enters the single-finding rulings
            // map. A "merge" unions the cluster's members in findings.json inline
            // (Option A); "separate" leaves them unmerged. The cluster is captured
            // here and applied after the pool drains.
            if ir.ClusterDecision == ClusterMerge {
                if c, ok := clusterIdx[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}]; ok {
                    oc.clusterMerge = true
                    oc.cluster = c
                }
            }
        default:
            ... // single-finding verdict

   - Note: we are only capturing merge decisions. Separate decisions are ignored (which is correct per the plan: separate leaves them unmerged).

   - However, what if the cluster decision is something else? 
        The ClusterDecision type is defined elsewhere? 
        We see in the test: 
          grayJudgeTurns(decision string) returns a turn with cluster_decision set to the decision string.

        And in the test we use "merge" and "separate".

        The code only handles ClusterMerge. 
        We assume there is also ClusterSeparate and maybe others? 
        But the code ignores non-merge decisions, which is correct.

   - We do not log if the cluster is not found in the index. 
        This is silent. 
        We should at least log a warning? 
        But note: the cluster might have been removed by a prior debate run? 
        However, we have the filterMergedClusters that removes gray-zone items at merged locations, but not for other reasons.

        It is possible that the cluster is not in the index because it was not loaded (due to error) or because it was filtered out by the indexClusters function (if there was a duplicate key and we lost it) or because the ambiguous.json did not contain it? 
        But we loaded the clusters from the ambiguous.json, so if it's not in the index, it's not in the ambiguous.json.

        This could happen if the reconcile step did not produce the cluster for some reason. 
        We should log a warning? 
        But the sprint plan does not specify.

        Given that the cluster decision was made by the judge, we expect the cluster to be present. 
        If it is not, then something is wrong.

        We can log a warning at debug level? 
        But we are not required to.

        However, note: we already have a log when applying the merges: 
            if applied < len(mergeClusters) {
                log.FromContext(ctx).Warn("debate: some gray-zone merge rulings could not be applied to findings.json",
                    "ruled", len(mergeClusters), "applied", applied)
            }

        This log will catch the case where we intended to apply a merge but failed (because the cluster was not found in the index, or because the applyOneClusterMerge failed). 
        So we are covered.

4. After the worker pool, we apply the cluster merges:
        if len(mergeClusters) > 0 {
            // Epic 6.1: union gray-zone clusters the judge ruled "merge" directly in the
            // post-verify findings.json (Option A) — never via RunReconcile, which would
            // rebuild from sources/ and erase the verify/debate verdicts above.
            var applied int
            findings, applied = applyClusterMerges(findings, mergeClusters)
            if applied < len(mergeClusters) {
                // A recorded merge ruling that could not be physically applied (its
                // members were not both present in findings.json) is otherwise silent —
                // debate.json still records the ruling, but findings.json is unchanged.
                log.FromContext(ctx).Warn("debate: some gray-zone merge rulings could not be applied to findings.json",
                    "ruled", len(mergeClusters), "applied", applied)
            }
        }

   - This is good.

### internal/reconcile/merge.go

1. The function `MergeJSONFindings`:
   - It returns an empty JSONFinding for an empty group. 
        This is acceptable because the caller (applyClusterMerges) only calls it with at least two records (because we check for len(matched)>=2 and exactHits>=1).

   - For a group of one, it returns the record unchanged. 
        The caller should never call it with one record? 
        Because we check for len(matched)>=2. 
        But the function is made total, which is good.

2. The function `unionReviewers`:
   - It returns a sorted, deduplicated list of reviewers. 
        This is correct.

3. The function `joinEvidence`:
   - It joins distinct non-empty evidence strings with " / ". 
        This is correct.

4. The function `verdictRank`:
   - It ranks verdicts: confirmed > unverifiable > refuted > (empty or unknown). 
        This matches the comment.

5. The function `mergeVerification`:
   - It combines verification blocks by taking the one with the highest verdict rank, and unioning the skeptics.