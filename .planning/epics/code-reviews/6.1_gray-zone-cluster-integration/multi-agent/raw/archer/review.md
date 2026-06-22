Let me analyze this diff carefully against the sprint plan scope.

The sprint plan is about Epic 6.1: Inline application of judge gray-zone cluster rulings. The scope includes:
- Translating judge `ClusterDecision` into applied cluster state in `findings.json`
- Preserving the existing `adjudication.json` path
- Files in scope: `internal/debate`, `internal/reconcile`

Let me review the diff for issues.

### Files changed:
1. `internal/debate/cluster.go` (new file) - cluster merge logic
2. `internal/debate/cluster_test.go` (new file) - tests
3. `internal/debate/debate.go` - wiring into the debate loop
4. `internal/reconcile/cluster_merge_test.go` (new file) - tests for MergeJSONFindings
5. `internal/reconcile/emit.go` - added ClusterMerged field
6. `internal/reconcile/merge.go` - MergeJSONFindings and helpers

Let me look for issues:

#### 1. `internal/debate/debate.go` - Error swallowed from ReadAmbiguousClusters

```go
grayClusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
```

The error is discarded. The comment says "A missing/corrupt ambiguous.json degrades to no gray-zone work." This is a deliberate design decision - a missing file is expected (first run), and a corrupt file degrades gracefully. But if the file is corrupt (not just missing), silently ignoring it could mask real issues. However, the comment explicitly documents this as intentional. This is a MEDIUM error-handling concern - a corrupt file is different from a missing file, and swallowing the error makes debugging harder. But the design decision is documented... Let me think about whether this is a real issue. The sprint plan says "A missing/corrupt ambiguous.json degrades to no gray-zone work" - so this is documented behavior. I'll flag it as MEDIUM since corrupt vs missing is a meaningful distinction that could hide bugs.

Actually, looking more carefully, the comment in the code says: "A missing/corrupt ambiguous.json degrades to no gray-zone work." This is explicitly documented as the intended behavior. The error is intentionally swallowed. I think this is still worth flagging as MEDIUM because a corrupt file (not just missing) being silently ignored could mask data integrity issues. But it's a judgment call.

#### 2. `internal/debate/cluster.go` - `applyOneClusterMerge` - potential issue with multiple clusters at same location

Looking at `applyClusterMerges`:
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

Each cluster is applied sequentially. If two clusters share a member (which shouldn't happen in normal operation but could in edge cases), the second cluster's merge might match the already-merged record. But the