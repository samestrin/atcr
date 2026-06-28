<think>
Let me analyze the diff carefully to identify any issues. The diff is about implementing Bipartite Matching & DBSCAN for the reconcile package, related to Epic 13.2.

Let me go through the changes:

1. **reconcile/bipartite.go** - New file implementing Hungarian algorithm
2. **reconcile/dbscan.go** - New file implementing DBSCAN
3. **reconcile/distance.go** - New file with composite edge-weight distance
4. **reconcile/dedupe.go** - Modified to use bipartite matching
5. **reconcile/reconcile.go** - Modified to pass AST group keys
6. Test files - ac2_greedy_failures_test.go, ac3_dbscan_test.go, ac3_isolation_test.go, bipartite_test.go, dbscan_test.go, dedupe_test.go, distance_test.go
7. **reconcile/README.md** - Documentation update

Let me look for issues:

### Issue 1: `ac3_dbscan_test.go` test references undefined identifier
Line: `groups, amb := dedupeCluster([]Finding{...}, make([]string, 4), map[string]bool{id: true})`
This is the only test calling the lower-case `dedupeCluster` directly, which is fine since it's in the same package. But there's `fakeGrouper{keys: map[int]string{10: "K1", 20: ""}}` reference too. Let me check for the fakeGrouper definition.

Looking at the test, `fakeGrouper{keys: map[int]string{10: "K1", 20: ""}}` is referenced but I don't see its definition in the diff. This may be a test fixture defined elsewhere or it's a real issue.

Actually, looking at the `TestClusterKeys_UsesGrouper` test, it uses `fakeGrouper{keys: ...}` - this implies `fakeGrouper` type must exist somewhere. But I don't see it in the diff. This is a potential issue.

### Issue 2: Missing import in `ac2_greedy_failures_test.go`
The file imports only `"sort"` and `"testing"`. Looking at the code, I see it uses `fnd`, `length`, `eq`, `deepEq`, `mf` which must be defined elsewhere in the test helpers.

### Issue 3: `bipartite.go` - `hungarian` function
The function `hungarian` is defined but takes a square cost matrix and returns a permutation. The implementation looks like a standard Jonker-Volgenant variant. Let me check for issues:

- Line 36-37: `assign := make([]int, n)` and then `if p[j] >= 1 { assign[p[j]-1] = j - 1 }` - This will leave some entries as 0 if not assigned. This is potentially a bug, but in a valid Hungarian result, all rows are assigned.

Wait, looking more carefully at the `hungarian` function - there's a subtle issue. In a 1-indexed Jonker-Volgenant implementation, p[j] stores the row assigned to column j. So `assign[p[j]-1] = j - 1` is correct. The `if p[j] >= 1` check is a bit odd because p[j] is always >= 1 in a valid solution. But this is defensive.

### Issue 4: `ac3_isolation_test.go` - End-to-end test
Looking at this test, it calls `mf()` helper which is presumably from a test fixture file. The test reads both `findings.json` and `ambiguous.json`. This test does end-to-end testing.

### Issue 5: `bipartiteGroups` function - Performance
The function creates `len(groups) × len(cands)` cost matrix in `hungarianAssign`. For each row r, it iterates over all group members. This is O(groups × members × cands). For the typical case of V < 50, this should be fine.

### Issue 6: `dbscan.go` - Memory efficiency
`neighborsOf` function creates a new slice on every call. This is O(n) allocations per point per expansion. For DBSCAN this is acceptable but not ideal.

### Issue 7: `dedupe.go` - `soloRoot` and `noiseIdx` logic
Looking at the noise isolation logic, a finding is noise if:
- `labels[i] == dbscanNoise` (DBSCAN classified it as noise)
- `!grayInvolved[i]` (not in a surviving gray pair)
- `!soloRoot[i]` (it is the only member of its group)

Wait, the condition is `!soloRoot[i]` which means "it is NOT the only member of its root". So `soloRoot[i] = true` means it IS the only member. The noise filter is `!soloRoot[i]` which means "not the only member of its root"... but we want to isolate solo findings as noise.

Let me re-read this. `soloRoot` is `finding index → it is the only member of its root`. So `soloRoot[i] = true` when finding i is the only member of its group.

Then the noise filter says: `if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] { continue }`

So we skip if it's NOT noise, or if it's gray-paired, or if it's NOT the only member. So we ADD to noise only if it IS noise AND not gray-paired AND IS the only member.

Wait, that's weird. A noise point that is in a group of multiple members? That doesn't make sense because if a finding is in a multi-member group, it was matched via bipartite, which means it was corroborated.

Actually, looking at the groupOf function, a finding has groupOf[i] = gi where gi is the group. So if a finding is in a group of multiple members, it was matched with another source's finding. It can still be classified as noise by DBSCAN if it has no cross-source neighbors (e.g., both are from the same source).

Hmm, but bipartiteGroups already ensures that one source's findings don't merge with each other (since they're processed in source order). So a multi-member group has at least 2 sources, meaning at least one cross-source edge exists, meaning DBSCAN wouldn't label it as noise.

Wait, but if a group has 3 members from sources A, B, C, then:
- A-B: cross-source, mergeable → denseNeighbor true
- A-C: cross-source, mergeable → denseNeighbor true  
- B-C: cross-source, mergeable → denseNeighbor true

So all three would have at least 2 neighbors, and DBSCAN with minPts=2 would label them all as core. So they wouldn't be noise.

For a single member (solo root), say source A's finding, and another source B's finding was also at this location but had a different problem text (low Jaccard) - then:
- A-B: cross-source, not mergeable → denseNeighbor false
- A: no neighbors → noise

This makes sense. So the `!soloRoot[i]` check is a safety net.

But wait, what if A and B both have findings with identical text (mergeable, cross-source) - then they'd be in the same group, not solo. So solo root = single member = single source = the other source either isn't there or didn't merge.

Actually, there's a subtle case: what if there are 3 sources A, B, C, all in one group of 3, all with identical text? Then A-B, A-C, B-C are all dense neighbors. None are noise. None are solo roots. All good.

What if A has 1 finding, B has 1 finding, both in one group (merged via bipartite), but the mergeable predicate requires token-set Jaccard or AST key match, which they don't have? Then they wouldn't have been merged. So this case can't happen.

OK so the logic seems consistent.

### Issue 8: `bipartiteGroups` returns `[][]int` but `dedupeCluster` expects `[]Finding` indices
This is correct - the function returns indices, which are used to look up findings in `cluster`.

### Issue 9: Hungrian algorithm - tie-breaking determinism
The code claims: "Strict-less-than comparisons make the column tie-break favor the lowest index, so equal-cost inputs yield a deterministic, platform-stable assignment."

Let me check the tie-breaking in the algorithm:

```go
if cur < minv[j] {
    minv[j] = cur
    way[j] = j0
}
```

This uses strict less-than, so when cur equals minv[j], the old minv[j] is kept. This means: when exploring columns in order 1..n, the first (lowest-index) column with the minimum cost wins. OK.

```go
if minv[j] < delta {
    delta = minv[j]
    j1 = j
}
```

This also uses strict less-than. So when multiple columns have the same delta, the lowest-index one wins. OK.

This seems deterministic.

### Issue 10: `hungarianAssign` and the cost matrix
The function creates an n×n square matrix and pads with `noMatchSentinel = 2.0`. The `hungarian` function then runs. If a row is assigned to a padded column, `assign[r] = -1`.

But wait, when the assignment maps a row to a padded column, what is the column index? It's c = full[r]. The check is `if c < cols`. If c >= cols, the column is virtual, so assign[r] = -1. OK.

### Issue 11: The `p[j] == 0` check
In the Hungarian loop, `p[0]` is the scratch slot. The condition `if p[j0] == 0` is checking if we found an unassigned column. After breaking, we backtrack the augmenting path. OK.

### Issue 12: `ac3_isolation_test.go` - What about `emit` writes to a different filename?
The test reads `FindingsJSON` and `AmbiguousJSON`. These must be defined constants. Let me check if they exist in the codebase...

Actually, this isn't shown in the diff. I should trust that the test passes since this is in scope.

### Issue 13: `dedupe.go` line 50 - `bipartiteGroups` parameters
`bipartiteGroups(srcKeys, dist, mergeable)` - `srcKeys` is the source keys, `dist` is the distance matrix, `mergeable` is the predicate.

Wait, in `dedupe.go`, the mergeable predicate uses `classify(tokens[a], tokens[b])` which uses cross-multiplication and is integer-exact. Good.

### Issue 14: `pairDistance` - does not call `classify` when both keys match
```go
if keyA != "" && keyA == keyB {
    return distanceFloor
}
```
This is correct - when AST keys match, return 0 without computing Jaccard.

### Issue 15: `bipartiteGroups` - one source at a time
The algorithm processes sources one at a time. For each new source, it matches against existing groups. This is the "incremental pairwise" reduction mentioned in the sprint plan.

But this has a known limitation: the order of source processing matters. If source A is processed first, then source B's findings can match against A's groups. If source B is processed first, then source A's findings match against B's groups. The matchings could differ.

The sprint plan acknowledges: "Ordering sensitivity is bounded for typical N ≤ 5 reviewers."

Looking at the code, sources are processed in **sorted order** (line: `sort.Strings(order)`), so the order is deterministic. Good.

### Issue 16: `bipartiteGroups` - first source as seed
The first source seeds the groups: each of its findings becomes its own group. Then subsequent sources match against these groups. This means the first source's findings are never matched against each other (which is correct, since same-source findings aren't compared in this model).

But wait, the first source's findings could include duplicates that would benefit from being grouped together... but the same-source-no-merge rule means they're kept separate. OK.

### Issue 17: `ac3_dbscan_test.go` - `TestAC3_GrayPairedFindingNotDoubleCountedAsNoise`
This test has assertions about a gray pair, not a noise cluster. The DBSCAN eps is set to `1 - MergeThreshold = 0.3`. So:
- greta and kai have identical text → Jaccard 1 → distance 0 → mergeable
- mira's text "alpha beta gamma zeta eta" vs greta's "alpha beta gamma delta epsilon" → 3 shared, 7 total = 0.428 → gray

So the pair greta+kai is dense (cross-source, mergeable). mira is gray-paired with one of them, so `grayInvolved[mira] = true`. So mira is not isolated as noise.

But mira is also not merged (since 0.428 is gray). So mira is in its own group of 1. Then in the output:
- `groups` has 2 entries: [greta, kai] and [mira]
- `amb` has 1 entry: the gray pair

Looking at the test, it asserts `length(t, groups, 2, "consensus pair + mira's gray singleton")` and `length(t, amb, 0)` is NOT asserted - instead it checks that mira is in a 2-finding gray pair. OK.

Actually wait, looking more carefully:
```go
// The gray third stays as its own uncorroborated finding (not merged, not
// isolated as noise): the consensus pair plus mira's singleton.
length(t, groups, 2, "consensus pair + mira's gray singleton")
```

And then the loop checks that mira is in a gray pair (2 findings). So `amb` should have 1 entry (the gray pair with mira). And mira should be in it, not in a noise cluster.

### Issue 18: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`
This test uses `dedupeCluster` directly (lowercase). Looking at the call:
```go
groups, amb := dedupeCluster([]Finding{...}, make([]string, 4), map[string]bool{id: true})
```

This requires the new signature `dedupeCluster(cluster, keys, adjudicatedMerges)`. The signature in the diff matches: `func dedupeCluster(cluster []Finding, keys []string, adjudicatedMerges map[string]bool)`. Good.

But wait, the keys are all empty strings. That's fine - the mergeable predicate checks `keys[a] != "" && keys[a] == keys[b]`, which is false for empty keys, so it falls through to `classify`. OK.

### Issue 19: `reconcile.go` line 82 - clusterKeys with nil Grouper
```go
func clusterKeys(cluster []Finding, g Grouper) []string {
    keys := make([]string, len(cluster))
    if g == nil {
        return keys
    }
    ...
}
```

The function handles nil Grouper by returning empty keys. Good.

### Issue 20: `ac3_isolation_test.go` - missing `mf` definition
The test uses `mf()` helper. This must be defined in a test helper file. Let me check if it's in scope.

Looking at the imports:
```go
import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/samestrin/atcr/internal/stream"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

`mf` is presumably a test helper. If it's not defined, the test won't compile. This is a potential issue.

### Issue 21: `ac3_isolation_test.go` - `Emit` and `FindingsJSON`/`AmbiguousJSON` constants
The test calls `Emit(dir, res)` and reads `FindingsJSON`/`AmbiguousJSON`. These need to be defined in the package.

If the constants are `FindingsJSON = "findings.json"` and `AmbiguousJSON = "ambiguous.json"`, then the test reads those files. OK.

### Issue 22: Test helpers - `fnd`, `mf`, `length`, `eq`, `deepEq`, `isTrue`, `inDelta`, `notEq`
The tests use these helpers. They must be defined in a test helper file. Let me see if they're defined somewhere visible.

Looking at the test files in the diff, they all use these helpers. If they're not defined, the tests won't compile. This is a real concern.

Actually, looking at `dedupe_test.go` (which existed before the diff), it already uses `fnd`, `length`, `eq`. So these must be in a shared test helper file. OK.

`mf` is new - I don't see it in the pre-existing tests. So it must be defined for the new tests. Let me check if there's a helper file in the diff... no, I don't see one.

Actually, this might be defined in another test file in the package that wasn't changed. Let me not flag this as an issue since it's likely defined.

### Issue 23: `bipartite.go` - The square matrix creation
```go
square := make([][]float64, n)
for r := 0; r < n; r++ {
    square[r] = make([]float64, n)
    for c := 0; c < n; c++ {
        if r < rows && c < cols {
            square[r][c] = cost(r, c)
        } else {
            square[r][c] = noMatchSentinel
        }
    }
}
```

This creates an n×n matrix where n = max(rows, cols). For each cell, it either computes the real cost or assigns the sentinel. The `cost` function is called with r < rows and c < cols, so it should handle all real cost cells.

But wait, the cost function might be called with the same r multiple times (once per c). Looking at the function:
```go
assign := hungarianAssign(len(groups), len(cands), func(r, c int) float64 {
    m := distanceCeiling
    for _, gi := range groups[r] {
        if d := dist[gi][cands[c]]; d < m {
            m = d
        }
    }
    return m
})
```

For each (r, c), it iterates over groups[r] and finds the minimum distance. This is O(|groups[r]|). Total work: O(sum over r of |groups[r]| × cols). For typical case, this is O(groups × members × cands). Acceptable.

But this means the cost is called for every (r, c) pair in the square matrix, including padded cells. The function correctly returns `noMatchSentinel` for padded cells via the `if r < rows && c < cols` check in `hungarianAssign`. So padded cells get sentinel cost without calling the user function. Good.

### Issue 24: `dedupe.go` - `clusterKeys` integration
```go
groups, amb := dedupeCluster(cl, clusterKeys(cl, opts.Grouper), opts.Merges)
```

The Grouper is the new dependency. The sprint plan says "13.1 (AST Isomorphism) metric must be available to use as an edge weight." So this is the integration point. Good.

### Issue 25: `bipartite.go` - Hungarian implementation correctness
Let me trace through the algorithm for a small example.

For cost = [[1, 2], [2, 1]] (2x2), the algorithm should return assign = [0, 1] (or [1, 0], both have cost 2).

Step-by-step (1-indexed):
- n=2, u=[0,0,0], v=[0,0,0], p=[0,0,0], way=[0,0,0]
- i=1:
  - p[0]=1, j0=0, minv=[inf,inf,inf], used=[F,F,F]
  - Iteration 1:
    - used[0]=true, i0=p[0]=1, delta=inf, j1=-1
    - j=1: cur=cost[0][0]-u[1]-v[1]=1-0-0=1, minv[1]=1, way[1]=0
    - j=1: minv[1]=1 < inf=delta, delta=1, j1=1
    - j=2: cur=cost[0][1]-u[1]-v[2]=2-0-0=2, minv[2]=2, way[2]=0
    - j=2: minv[2]=2 > delta=1, no update
    - Update: for j in used: u[p[j]] += delta; for j not used: minv[j] -= delta
    - j=0 (used): u[p[0]] += 1 → u[1] = 0+1 = 1
    - j=1 (not used): minv[1] = 1-1 = 0
    - j=2 (not used): minv[2] = 2-1 = 1
    - j0=1, p[1]=0 → break
  - Backtrack: j0=1, j1=way[1]=0, p[1]=p[0]=1, j0=0. Done.
- i=2:
  - p[0]=2, j0=0, minv=[inf,inf,inf], used=[F,F,F]
  - Iteration 1:
    - used[0]=true, i0=p[0]=2, delta=inf, j1=-1
    - j=1: cur=cost[1][0]-u[2]-v[1]=2-0-0=2, minv[1]=2, way[1]=0
    - j=1: minv[1]=2 < inf, delta=2, j1=1
    - j=2: cur=cost[1][1]-u[2]-v[2]=1-0-0=1, minv[2]=1, way[2]=0
    - j=2: minv[2]=1 < delta=2, delta=1, j1=2
    - Update:
    - j=0 (used): u[p[0]] += 1 → u[2] = 0+1 = 1
    - j=1 (not used): minv[1] = 2-1 = 1
    - j=2 (not used): minv[2] = 1-1 = 0
    - j0=2, p[2]=0 → break
  - Backtrack: j0=2, j1=way[2]=0, p[2]=p[0]=2, j0=0. Done.
- Result: p[1]=1, p[2]=2
- assign[0]=0, assign[1]=1

So assign = [0, 1], cost = 1+1 = 2. Matches the test expectation. Good.

### Issue 26: The `cost` function in `hungarianAssign` is called even for padded cells in `hungarian`
Wait, let me re-check. In `hungarianAssign`:
```go
for r := 0; r < n; r++ {
    square[r] = make([]float64, n)
    for c := 0; c < n; c++ {
        if r < rows && c < cols {
            square[r][c] = cost(r, c)
        } else {
            square[r][c] = noMatchSentinel
        }
    }
}
```

The `cost` function is only called when `r < rows && c < cols`. So it's called exactly `rows × cols` times, not `n × n` times. Good.

### Issue 27: Potential issue with the `cost` function when used in `bipartiteGroups`
The cost function is:
```go
func(r, c int) float64 {
    m := distanceCeiling
    for _, gi := range groups[r] {
        if d := dist[gi][cands[c]]; d < m {
            m = d
        }
    }
    return m
}
```

If `groups[r]` is empty, `m` stays at `distanceCeiling = 1.0`. This is a reasonable cost (maximum meaningful distance). But the group can never be empty because we add at least one index to each group. Actually wait, the groups are seeded with at least one finding from the first source, so they're never empty. OK.

### Issue 28: `bipartiteGroups` - what if a candidate's group is empty after rejection?
The function:
```go
for ci, idx := range cands {
    if !used[ci] {
        groups = append(groups, []int{idx})
    }
}
```

This adds unaccepted candidates as new singleton groups. So the groups only grow. Good.

But wait, what if a candidate is accepted into an existing group? Then it's not added as a new group. The existing group is expanded. Good.

### Issue 29: Memory issue in `bipartiteGroups`
The function calls `hungarianAssign` once per source. Each call creates an O(n²) matrix. For V findings per cluster, this is O(V²) memory per call. For N sources, this is N × O(V²). For typical N=2-5 and V<50, this is fine.

### Issue 30: `dbscan.go` - The neighbor function
```go
neighborsOf := func(p int) []int {
    var out []int
    for q := 0; q < n; q++ {
        if q != p && neighbor(p, q) {
            out = append(out, q)
        }
    }
    return out
}
```

This is called for every point (once initially, once per core point expansion). For a point with K neighbors, this is O(n) work. For DBSCAN total, this is O(n²) in the worst case. Acceptable for n<50.

### Issue 31: `dbscan.go` - `cluster > 0` is the dense flag
```go
cluster := 0
for p := 0; p < n; p++ {
    ...
    cluster++
}
...
return labels, cluster > 0
```

The `cluster` counter increments every time a new cluster is formed. So `cluster > 0` means at least one cluster was created. But this includes the case where a cluster was started and the initial point ended up being noise (which would have `labels[p] = dbscanNoise` and `cluster` not incrementing).

Wait, `cluster++` is inside the `else` branch (when the point has enough neighbors). If the point is noise, we set `labels[p] = dbscanNoise` and `continue`, without incrementing cluster. So `cluster > 0` correctly means "at least one dense cluster was found". Good.

### Issue 32: `dbscan.go` - `dbscanNoise` constant
```go
dbscanNoise = -1
```

This conflicts with the convention that a valid cluster id should be 0-based. But since the function returns labels with `dbscanNoise = -1` for noise, the test should check `labels[i] == dbscanNoise`. The tests in `dbscan_test.go` do this correctly.

But wait, what if a valid cluster id could be -1? In the algorithm, cluster ids are 0, 1, 2, ... So -1 is reserved for noise. This is safe.

### Issue 33: `ac3_dbscan_test.go` - `TestAC3_SoloFindingIsNotIsolated`
```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 5, "unused import lingers", "greta"),
})
length(t, groups, 1, "solo finding stays in the output")
length(t, groups[0], 1, "the finding itself")
length(t, amb, 0, "nothing isolated without a dense cluster for context")
```

With one finding, the distance matrix is 1x1 (distance = 0 to itself, but we exclude i==j). Actually, the matrix is built for i<j, so dist[0][0] = 0 (never used).

DBSCAN: 1 point, no neighbors. labels[0] = dbscanNoise. But `dense = false` (cluster = 0). So no noise is isolated. 

But wait, `dense = false` means `noise = []` (the if dense block is skipped). So `amb` is empty. Good.

The output: groups = [[greta's finding]], amb = []. Correct.

### Issue 34: `ac3_dbscan_test.go` - `TestAC3_AllDistinctNoIsolation`
```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "alpha beta gamma", "greta"),
    fnd("a.go", 1, "delta epsilon zeta", "kai"),
    fnd("a.go", 1, "eta theta iota", "mira"),
})
length(t, groups, 3, "all three remain as independent findings")
length(t, amb, 0, "no corroboration context → no isolation")
```

3 findings, no mergeable pairs (Jaccard 0 between any pair, AST keys empty). So bipartite groups each finding into its own group.

DBSCAN: no neighbors (no mergeable pairs). All noise. But `dense = false`. So no isolation. Good.

Output: groups = [[greta], [kai], [mira]], amb = []. Correct.

### Issue 35: `ac3_dbscan_test.go` - `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`
```go
groups, _ := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked", "greta"),
    fnd("a.go", 1, "token never expires unchecked", "greta"),
})
length(t, groups, 2, "same-source duplicates stay as separate findings")
```

2 findings, same source. First source = greta. So both go into groups as singletons. The second source loop doesn't execute (only one source). So groups = [[greta1], [greta2]]. Good.

But wait, are these even at the same location? The findings have the same file/line but different content (different indices in the cluster). In `DedupeCluster`, they're passed in as `[]Finding`, so they're already a single location cluster. OK.

But within the location cluster, same-source duplicates would have been grouped at the same file/line. Are they treated as separate findings? Yes, because they're separate entries in the input. OK.

### Issue 36: `ac3_dbscan_test.go` - `TestDedupeCluster_UnattributedDuplicatesStillMerge`
```go
groups, _ := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked", ""),
    fnd("a.go", 1, "token never expires unchecked", ""),
})
length(t, groups, 1, "unattributed duplicates merge")
```

2 findings, both with empty Reviewer. srcKeys are "\x00anon\x000" and "\x00anon\x001" (different per finding). So they're treated as different sources. Bipartite groups them: first source seeds [idx0], second source matches idx1 against [idx0] (mergeable), so they merge. groups = [[idx0, idx1]]. Good.

### Issue 37: `bipartite.go` - The `p[j] >= 1` check
```go
for j := 1; j <= n; j++ {
    if p[j] >= 1 {
        assign[p[j]-1] = j - 1
    }
}
```

In a valid Hungarian solution, p[j] >= 1 for all j (every column is assigned). But the defensive check is OK. However, if p[j] is 0, then `assign[-1]` would panic. Wait, no - the check `if p[j] >= 1` prevents that. So this is safe.

### Issue 38: `bipartite.go` - The square matrix is not freed
The `square` matrix is created and then `hungarian` is called. The matrix goes out of scope after `hungarianAssign` returns. Go's GC will collect it. OK.

### Issue 39: `dedupe.go` - `pairDistance` and `classify`
The `classify` function is called from `pairDistance`. It returns `(rel, sim)`. The `sim` is computed via cross-multiplication (integer-exact). Good.

### Issue 40: `dedupe.go` - `groupOf` map initialization
```go
groupOf := make([]int, n)
for gi, g := range groups {
    for _, idx := range g {
        groupOf[idx] = gi
    }
}
```

`groupOf[i]` is the group index for finding i. Default is 0. Since groups is non-empty when n > 0, every finding has a valid group. OK.

Wait, but if `n == 0` (empty cluster), groups is empty (well, bipartiteGroups returns whatever, but with empty input it returns nil). The `if n == 0 { return nil, nil }` early return handles this. OK.

### Issue 41: `dedupe.go` - Adjudicated union-find on group ids
```go
uf := newUnionFind(len(groups))
```

If `len(groups) == 0` (which can only happen if n == 0), the function returns early. So `len(groups) >= 1` here. OK.

### Issue 42: `dedupe.go` - `ambiguousIdx` vs `ambiguousPairs`
```go
var ambiguousPairs [][2]int // group-id pair behind each entry
var ambiguousIdx [][2]int   // finding-index pair behind each entry
```

Two slices are kept in sync. This is a bit redundant - we could just use the finding indices and look up groups. But it's clear and correct.

### Issue 43: `dedupe.go` - The `out` slice construction
```go
out := make([][]Finding, 0, len(rootOrder))
for _, r := range rootOrder {
    idxs := rootIdx[r]
    if len(idxs) == 1 && noiseIdx[idxs[0]] {
        continue
    }
    sort.Ints(idxs)
    members := make([]Finding, len(idxs))
    for k, idx := range idxs {
        members[k] = cluster[idx]
    }
    out = append(out, members)
}
```

The members are sorted by index (which is the input order). This is deterministic. Good.

### Issue 44: `dedupe.go` - `ambiguous = append(ambiguous, noise...)`
Noise is appended after the surviving gray pairs. This is OK - the order of `ambiguous` doesn't matter for downstream consumers (probably).

But wait, `noise` might have entries that were filtered out by `grayInvolved` or `soloRoot`. Let me re-check:
```go
for i := 0; i < n; i++ {
    if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] {
        continue
    }
    noiseIdx[i] = true
    noise = append(noise, AmbiguousCluster{...})
}
```

So `noise` only contains findings that are:
- DBSCAN noise
- Not in a surviving gray pair
- Solo root (only member of group)

Then `noiseIdx[i] = true` is set for these. Later, the output loop filters out solo roots that are in `noiseIdx`. So they appear in `noise` (ambiguous) but not in `out` (groups). Good.

### Issue 45: `dedupe.go` - The `ambiguous` slice is built with `append`
Looking at:
```go
ambiguous = append(ambiguous, AmbiguousCluster{...})
ambiguousPairs = append(ambiguousPairs, [2]int{groupOf[i], groupOf[j]})
ambiguousIdx = append(ambiguousIdx, [2]int{i, j})
```

These slices grow as needed. The filtering at the end uses `keptAmb := ambiguous[:0]` which reuses the backing array. This is a common Go idiom to filter in-place. Good.

But wait, there's a potential issue: `keptAmb := ambiguous[:0]` then `keptAmb = append(keptAmb, ambiguous[k])`. This appends to the same backing array. Since we're filtering in-place, this works as long as the kept elements don't overlap with the original (which they don't, since we're copying from the original index to the same or earlier index in the kept slice). Good.

### Issue 46: `ac2_greedy_failures_test.go` - Token overlap computation
Let me verify the test case `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother`:

- A: "alpha beta gamma delta epsilon zeta eta" - 7 tokens
- P: "alpha beta gamma delta epsilon zeta p1" - 7 tokens
- Q: "alpha beta gamma delta epsilon zeta q1" - 7 tokens

Intersection(A, P) = {alpha, beta, gamma, delta, epsilon, zeta} = 6
Union(A, P) = {alpha, beta, gamma, delta, epsilon, zeta, eta, p1} = 8
Jaccard(A, P) = 6/8 = 0.75

So A and P are above the merge threshold of 0.7. They merge. Similarly A and Q.

In bipartite: greta's A is processed first (sorted order: greta, kai). greta's group = [A]. Then kai's P and Q are matched. The cost for both is 0.25 (1 - 0.75). Hungarian picks the assignment that minimizes total cost. With one group and two candidates, the assignment is P (cost 0.25) and Q is unmatched (matched to padding, so -1 in hungarianAssign). But wait, hungarianAssign expects a rectangular cost matrix and the result is a permutation. Let me re-trace.

Actually, in bipartiteGroups:
```go
assign := hungarianAssign(len(groups), len(cands), cost)
```

For the second source (kai), `len(groups) = 1` and `len(cands) = 2`. So `hungarianAssign(1, 2, cost)`.

In `hungarianAssign`:
- `n = max(1, 2) = 2`
- `square` is 2x2:
  - square[0][0] = cost(0, 0) = 0.25
  - square[0][1] = cost(0, 1) = 0.25
  - square[1][0] = noMatchSentinel (r=1 >= rows=1)
  - square[1][1] = noMatchSentinel
- `full = hungarian(square)` = [0, 1] (or [1, 0], both have cost 0.25+2=2.25)
- `assign[0] = full[0] = 0` or 1
- The function only initializes `assign[0]`, so `assign` is length 1.

So `assign[0]` is 0 or 1. Then:
```go
for r, c := range assign {
    if c < 0 {
        continue
    }
    cand := cands[c]
    accept := false
    for _, gi := range groups[r] {
        if mergeable(gi, cand) {
            accept = true
            break
        }
    }
    if accept {
        groups[r] = append(groups[r], cand)
        used[c] = true
    }
}
```

So `cand = cands[c] = cands[0 or 1]`. If c=0, cand = P. If c=1, cand = Q. Either way, the candidate is mergeable with A (Jaccard 0.75), so `accept = true` and the group becomes [A, cand].

Then:
```go
for ci, idx := range cands {
    if !used[ci] {
        groups = append(groups, []int{idx})
    }
}
```

The other candidate (not used) becomes a new singleton group.

So the result is `groups = [[A, P], [Q]]` or `groups = [[A, Q], [P]]`. After sorting by lowest member index, we get the same canonical form.

Group sizes: [1, 2]. The test expects `groupSizes(groups), []int{1, 2}`. Good.

Wait, the test calls `groupSizes(groups)` and asserts it equals `[1, 2]`. Let me check:
```go
func groupSizes(groups [][]Finding) []int {
    sizes := make([]int, len(groups))
    for i, g := range groups {
        sizes[i] = len(g)
    }
    sort.Ints(sizes)
    return sizes
}
```

So `groupSizes` returns the sorted sizes. For `groups = [[A, P], [Q]]`, sizes = [2, 1], sorted = [1, 2]. Good.

The test:
```go
deepEq(t, groupSizes(groups), []int{1, 2}, "consensus pair + one stranded finding (not a 3-way over-merge)")
length(t, amb, 0, "P/Q are same-source and never compared, so no gray pair is recorded")
```

But wait, are A and P or A and Q gray? Jaccard 0.75 is above MergeThreshold (which I assume is 0.7). So they're mergeable, not gray. So no gray pair is recorded. Good.

But wait, the test expects `amb` to be empty. Let me check the gray zone: [0.4, 0.7). Jaccard 0.75 is above 0.7, so it's not gray. Good.

But also, the gray pair loop:
```go
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        if groupOf[i] == groupOf[j] {
            continue
        }
        rel, sim := classify(tokens[i], tokens[j])
        if rel != relGray {
            continue
        }
        ...
    }
}
```

For (P, Q), they're same-source (kai). `groupOf[P] = 0` (group [A, P]), `groupOf[Q] = 1` (group [Q]). Different groups, so we don't skip. But `classify(P, Q)` returns rel = relMerge (Jaccard 0.75 is 6/8 = 0.75, but P and Q share {alpha, beta, gamma, delta, epsilon, zeta, p1 vs q1, ...} wait let me recompute).

P: {alpha, beta, gamma, delta, epsilon, zeta, p1} = 7 tokens
Q: {alpha, beta, gamma, delta, epsilon, zeta, q1} = 7 tokens
Intersection = {alpha, beta, gamma, delta, epsilon, zeta} = 6
Union = {alpha, beta, gamma, delta, epsilon, zeta, p1, q1} = 8
Jaccard = 6/8 = 0.75

So P and Q are mergeable (Jaccard >= 0.7). But they're same-source, so bipartite doesn't compare them. The gray pair loop does check them though. `rel = relMerge`, so we `continue`. No gray pair recorded. Good.

So `amb` is empty. The test passes.

### Issue 47: `ac2_greedy_failures_test.go` - `TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates`
- A: "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10" - 10 tokens
- B: "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1" - 10 tokens
- C: "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1" - 10 tokens

Jaccard(A, B) = |{w1-w8}| / |{w1-w10, c1, b1}| = 8/12 = 0.667
Jaccard(A, C) = |{w1-w9}| / |{w1-w10, c1}| = 9/11 = 0.818
Jaccard(B, C) = |{w1-w8, c1}| / |{w1-w10, c1, b1}| = 9/12 = 0.75

So A and B are gray (0.667 in [0.4, 0.7)). A and C are mergeable (0.818 >= 0.7). B and C are mergeable (0.75 >= 0.7).

In bipartite:
- greta (sorted first): group = [A]
- kai: B is matched. Cost(A, B) = 1 - 0.667 = 0.333. Assignment: B → A (mergeable). Group = [A, B].
- mira: C is matched. Cost([A,B], C) = min(1-0.818, 1-0.75) = min(0.182, 0.25) = 0.182. Assignment: C → [A,B] (mergeable). Group = [A, B, C].

But the test expects `groupSizes(groups), []int{1, 2}`. That means 2 groups: one of size 1, one of size 2. But with bipartite, we got one group of size 3.

Wait, this contradicts the test. Let me re-read the test:
```go
deepEq(t, groupSizes(groups), []int{1, 2}, "non-duplicate A-B not chained via C")
length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
```

So the expected behavior is that A and B stay separate (because they're gray, not mergeable), and C corroborates one of them. The other remains alone. Plus the gray A-B pair is recorded.

But in the bipartite algorithm, if A and B are gray, `mergeable(A, B) = false`. So when kai's B is matched against greta's A, the assignment is made (B → A) but `accept = false` (because mergeable is false). So B is NOT added to A's group. B becomes a new singleton group.

Let me re-trace:
- greta: groups = [[A]]
- kai: hungarianAssign(1, 1, cost). One group, one candidate. Cost = 1 - 0.667 = 0.333. Assignment: B → A. But `mergeable(A, B) = false`. So `accept = false`. B is added as a new singleton: groups = [[A], [B]].
- mira: hungarianAssign(2, 1, cost). Two groups, one candidate.
  - cost(0, 0) = 1 - Jaccard(A, C) = 1 - 0.818 = 0.182
  - cost(1, 0) = 1 - Jaccard(B, C) = 1 - 0.75 = 0.25
  - Hungarian picks: C → A (cost 0.182). But wait, the square matrix is 2x2:
    - square[0][0] = 0.182
    - square[0][1] = noMatchSentinel
    - square[1][0] = 0.25
    - square[1][1] = noMatchSentinel
  - hungarian(square) = [0, 1] (row 0 → col 0, row 1 → col 1). Cost = 0.182 + 2 = 2.182.
  - Or [1, 0] (row 0 → col 1, row 1 → col 0). Cost = 2 + 0.25 = 2.25.
  - The first is cheaper, so [0, 1].
  - assign[0] = 0 (C → A, within bounds)
  - assign[1] = 1 (beyond cols=1, so -1)
  - Wait, the code is: `if c := full[r]; c < cols { assign[r] = c }`. full[1] = 1, but cols = 1, so c < cols is false. assign[1] = -1.
  - So assign = [0, -1].
- For r=0, c=0: cand = cands[0] = C. `mergeable(A, C) = true` (Jaccard 0.818). `accept = true`. Group [A] becomes [A, C]. used[0] = true.
- For r=1, c=-1: skip.
- cands[0] = C is used, so no new group added.

Result: groups = [[A, C], [B]]. Sizes: [2, 1]. Sorted: [1, 2]. Matches the test expectation!

Then the gray pair (A, B) is recorded in `amb`. Good.

### Issue 48: `ac2_greedy_failures_test.go` - `TestAC2_OptimalAssignmentPrefersClosestPairing`
- A: "shared1 shared2 shared3 shared4 shared5 shared6 shared7 aone" (greta)
- B: "shared1 shared2 shared3 shared4 shared5 shared6 shared7 bone" (greta)
- P: same as B (kai)
- Q: same as A (kai)

So A == Q text, B == P text. Jaccard(A, Q) = 1, Jaccard(B, P) = 1, Jaccard(A, P) = ? 
- A: {shared1, shared2, shared3, shared4, shared5, shared6, shared7, aone}
- P: {shared1, shared2, shared3, shared4, shared5, shared6, shared7, bone}
- Intersection: 7 shared, Union: 9 total (aone vs bone). Jaccard = 7/9 = 0.778.

So A and P are mergeable (>= 0.7). Similarly A and B are mergeable (7/9 = 0.778). P and Q are mergeable (7/9 = 0.778). B and Q are mergeable (7/9 = 0.778).

In bipartite:
- greta: groups = [[A], [B]]
- kai: hungarianAssign(2, 2, cost). 
  - cost(0, 0) = 1 - Jaccard(A, P) = 1 - 0.778 = 0.222
  - cost(0, 1) = 1 - Jaccard(A, Q) = 1 - 1 = 0
  - cost(1, 0) = 1 - Jaccard(B, P) = 1 - 1 = 0
  - cost(1, 1) = 1 - Jaccard(B, Q) = 1 - 0.778 = 0.222
  - Hungarian optimal: A → Q (0), B → P (0). Total cost = 0.
  - Or A → P (0.222), B → Q (0.222). Total cost = 0.444.
  - Or A → Q (0), B → P (0). Total = 0. Or A → P, B → Q = 0.444. First is better.
  - So assignment is A → Q, B → P.
- For r=0, c=Q: mergeable(A, Q) = true. accept. Group [A] becomes [A, Q].
- For r=1, c=P: mergeable(B, P) = true. accept. Group [B] becomes [B, P].

Result: groups = [[A, Q], [B, P]]. After sorting by lowest member index:
- [A, Q] sorted: [0, 3] (indices of A and Q)
- [B, P] sorted: [1, 2]
- Group order: [A, Q] first (lowest index 0), then [B, P] (lowest index 1).

So `groups = [[A, Q], [B, P]]`. The test checks:
```go
for _, g := range groups {
    length(t, g, 2, "each group is a clean cross-source pair")
    eq(t, g[0].Problem == g[1].Problem, true, "paired findings share the same problem text")
}
```

Group [A, Q]: A.Problem == Q.Problem? A is "shared1 ... aone", Q is "shared1 ... aone". Yes.
Group [B, P]: B.Problem == P.Problem? B is "shared1 ... bone", P is "shared1 ... bone". Yes.

Test passes.

### Issue 49: `bipartite.go` - The `p` array reset
In `hungarian`, the `p` array is reset for each `i`:
```go
p[0] = i
j0 := 0
minv := make([]float64, n+1)
used := make([]bool, n+1)
for j := 0; j <= n; j++ {
    minv[j] = inf
}
```

So `p[0] = i` and `p[1..n]` retain their values from previous iterations. But that's fine - the algorithm is incremental and uses `p[j]` to track which row is currently assigned to column j. Initially all 0 (unassigned), and after the algorithm, each column has exactly one row assigned.

### Issue 50: `bipartite.go` - The `way` array
`way[j]` tracks the previous column in the augmenting path. Initialized to 0 and updated when minv[j] is updated. After the inner loop, we backtrack through way to update p.

### Issue 51: Performance of the Hungarian algorithm
The algorithm is O(n³) per the sprint plan. For n < 50 (V < 50 per location), this is at most 125,000 operations. Fast.

### Issue 52: `bipartiteGroups` - The cost function is called even for already-padded cells in the square matrix
Wait, in `hungarianAssign`:
```go
for r := 0; r < n; r++ {
    square[r] = make([]float64, n)
    for c := 0; c < n; c++ {
        if r < rows && c < cols {
            square[r][c] = cost(r, c)
        } else {
            square[r][c] = noMatchSentinel
        }
    }
}
```

The `cost` function is only called when `r < rows && c < cols`. So it's called exactly `rows × cols` times. For the rest, it's `noMatchSentinel`. Good.

### Issue 53: Memory - the cost function in bipartiteGroups iterates over group members
```go
func(r, c int) float64 {
    m := distanceCeiling
    for _, gi := range groups[r] {
        if d := dist[gi][cands[c]]; d < m {
            m = d
        }
    }
    return m
}
```

For each (r, c), it iterates over `groups[r]`. Total work: O(sum over r of |groups[r]|) per c = O(groups × members × cands). For typical V < 50, this is manageable.

### Issue 54: `bipartiteGroups` - The first source seeds as singletons
The first source's findings are not compared with each other (they're same-source). So each becomes its own group. This means the first source's findings can never be merged with each other. This is the documented behavior (same-source duplicates stay separate).

### Issue 55: `bipartiteGroups` - Subsequent sources
For each subsequent source, each finding is matched against existing groups. The cost is the minimum distance to any group member. Acceptance requires `mergeable` with at least one group member.

So a finding is added to a group only if:
- It's the optimal assignment for that group (Hungarian).
- It's mergeable with at least one member of that group.

This means: a finding can be in a group even if it's only mergeable with ONE member, not all. So if a group has [A, B, C] and a new finding D is mergeable only with C, D can be added to the group. But then D is in a group where it's not directly mergeable with A or B. Is that OK?

The sprint plan says: "Acceptance of a match is gated by the same integer cross-multiplication boundary (no float comparison): a pair at or above 0.7 similarity merges."

So D merges with C (similarity >= 0.7). D is now in a group with [A, B, C]. D is in the group because of its merge with C, not with A or B. The group is "corroborated by C".

OK this is fine.

### Issue 56: `dedupe.go` - `mergeable` and `classify`
```go
mergeable := func(a, b int) bool {
    if keys[a] != "" && keys[a] == keys[b] {
        return true
    }
    rel, _ := classify(tokens[a], tokens[b])
    return rel == relMerge
}
```

This checks AST key match first, then Jaccard merge. Both are deterministic. Good.

### Issue 57: `dedupe.go` - The gray pair loop's `groupOf` check
```go
if groupOf[i] == groupOf[j] {
    continue // matched into one group already; not adjudicable
}
```

This skips pairs already in the same group. Good - we don't need to record a gray pair for findings that are already merged.

### Issue 58: `dedupe.go` - The `keptIdx` slice
```go
keptIdx := ambiguousIdx[:0]
for k, gp := range ambiguousPairs {
    if uf.find(gp[0]) != uf.find(gp[1]) {
        keptAmb = append(keptAmb, ambiguous[k])
        keptIdx = append(keptIdx, ambiguousIdx[k])
    }
}
ambiguous = keptAmb
```

This keeps both the ambiguous cluster and the corresponding finding indices in sync. Good.

But wait, `keptIdx` is used later for `grayInvolved`:
```go
grayInvolved := make(map[int]bool, len(keptIdx)*2)
for _, p := range keptIdx {
    grayInvolved[p[0]] = true
    grayInvolved[p[1]] = true
}
```

So grayInvolved is computed from the surviving gray pairs (after adjudication filtering). This is correct.

### Issue 59: `dedupe.go` - `soloRoot` semantics
```go
soloRoot := make(map[int]bool, n) // finding index → it is the only member of its root
for _, idxs := range rootIdx {
    if len(idxs) == 1 {
        soloRoot[idxs[0]] = true
    }
}
```

So `soloRoot[i] = true` when finding i is the only member of its root. This is used to filter noise:
```go
if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] {
    continue
}
```

Wait, the condition is `!soloRoot[i]`. So we skip if it's NOT a solo root. So we add to noise only if it IS a solo root (and is DBSCAN noise and not gray-involved).

But why require solo root? Because if a finding is in a multi-member root (i.e., its group was merged with another group via adjudication), it's not really "alone" - it's part of a larger group. The intent is: a finding that is part of an adjudicated merge should not be isolated as noise.

Actually, let me think again. The `rootIdx` groups are formed by:
1. Bipartite matching creates initial groups.
2. Adjudication unions some groups.
3. `rootIdx[r]` collects all findings in root r.

A solo root is a root with exactly one member. This means: the finding was not merged with any other finding (neither by bipartite nor by adjudication). So if it's DBSCAN noise, it has no corroboration AND was not merged.

Why require solo root for noise? Because if a finding is in a multi-member root, it was merged with another finding (via adjudication or bipartite). If it was merged via bipartite, then there are at least 2 findings in the group, and the group has cross-source mergeable pairs (otherwise bipartite wouldn't have merged them). So DBSCAN wouldn't label any of them as noise (they all have at least 1 cross-source mergeable neighbor).

If it was merged via adjudication, the finding might still have no cross-source mergeable neighbor. But it's now part of a multi-member group, so it's not "alone". The decision was made to merge it, so it stays in the output.

Wait, but could a finding be in a multi-member root via adjudication but still be DBSCAN noise? Yes, if the other members of the root have no cross-source mergeable neighbors with it. For example:
- A (greta), B (kai) - gray, adjudicated to merge
- C (mira), D (host) - merge, no adjudication

After adjudication, A and B are in one root, C and D are in another. The roots are [A, B] and [C, D]. Now DBSCAN runs over all 4 findings.

If A and B have no cross-source mergeable neighbors with C and D (e.g., very different problems), then:
- A: no cross-source mergeable neighbor → noise
- B: no cross-source mergeable neighbor → noise
- C: cross-source mergeable with D → not noise
- D: cross-source mergeable with C → not noise

But A and B are in a multi-member root! So `soloRoot[A] = false`. So A and B are NOT isolated as noise. They stay in the output (as part of the adjudicated group).

Is this correct? The adjudicated pair A-B was explicitly merged by user decision. The user wants A-B to be together. So A-B should stay in the output, not be isolated as noise. The `!soloRoot` check correctly preserves adjudicated groups.

Good.

### Issue 60: `dedupe.go` - The noise filter and adjudicated groups
Actually, I realize there's a subtle issue. The test `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` checks:
```go
length(t, amb, 0, "no noise singletons: A and B were merged, not isolated")
```

This expects no noise isolation when A and B are adjudicated. Let me verify:
- A (mira), B (pool) - gray (0.5)
- C (greta), D (kai) - identical text (mergeable)
- A and B are adjudicated to merge
- C and D are merged by bipartite

Initial groups (after bipartite):
- greta (sorted first): group = [C]
- kai: D matches C (mergeable, Jaccard 1). Group = [C, D].
- mira: A has no mergeable pair. A is added as singleton. groups = [[C, D], [A]]
- pool: B is matched against [[C, D], [A]]. 
  - cost(0, 0) = 1 - Jaccard(C, B) = 1 - 0 (disjoint) = 1
  - cost(1, 0) = 1 - Jaccard(A, B) = 1 - 0.5 = 0.5
  - Hungarian picks the cheaper: B → A. But mergeable(A, B) = false (Jaccard 0.5 is gray). So B is not added to A's group. B becomes a new singleton: groups = [[C, D], [A], [B]].

Then adjudication: A-B is in adjudicatedMerges. So uf.union(groupOf[A], groupOf[B]) = uf.union(1, 2). Now A and B are in the same root.

rootIdx: {[C, D], [A, B]} (after union). rootOrder: [0, 1].

DBSCAN: 
- C-D: cross-source, mergeable → denseNeighbor true → not noise
- A-B: gray, not mergeable → denseNeighbor false → noise (potentially)
- A-C, A-D: disjoint text, no key match → not mergeable → not dense neighbor
- B-C, B-D: disjoint text, no key match → not mergeable → not dense neighbor
- So A and B are noise, C and D are in a cluster.

But `dense = true` (cluster 0 formed with C and D).

For A: labels[A] = dbscanNoise. grayInvolved[A]? Yes, A is in the gray pair (A, B) which was not adjudicated (well, the gray pair IS the adjudicated pair). Wait, let me re-check.

In the gray pair loop:
```go
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        if groupOf[i] == groupOf[j] {
            continue
        }
        rel, sim := classify(tokens[i], tokens[j])
        if rel != relGray {
            continue
        }
        id := AmbiguousID(...)
        if adjudicatedMerges[id] {
            uf.union(groupOf[i], groupOf[j])
            continue
        }
        ambiguous = append(ambiguous, ...)
        ambiguousPairs = append(ambiguousPairs, ...)
        ambiguousIdx = append(ambiguousIdx, ...)
    }
}
```

For (A, B): groupOf[A] = 1, groupOf[B] = 2 (different). classify(A, B) = relGray. id = AmbiguousID(...). adjudicatedMerges[id] = true. So uf.union(1, 2). Continue. So A-B is NOT added to `ambiguous`.

After the loop, `ambiguous` is empty. So `keptIdx` is empty. So `grayInvolved` is empty.

For A: labels[A] = dbscanNoise. grayInvolved[A] = false. soloRoot[A] = false (A is in root [A, B] after adjudication). So the condition `!soloRoot[A]` is true, so we skip A from noise. Correct.

For B: same as A. Skip.

So `noise` is empty. `noiseIdx` is empty.

In the output loop:
- rootOrder = [0, 1] (or sorted)
- For root 0: idxs = [C_idx, D_idx]. Not solo. Add to out: [C, D].
- For root 1: idxs = [A_idx, B_idx]. Not solo (len 2). Add to out: [A, B].

So out = [[C, D], [A, B]]. Sizes: [2, 2]. Test expects `length(t, groups, 2)` and `length(t, g, 2)` for each. Good.

`amb = []`. Test expects `length(t, amb, 0)`. Good.

### Issue 61: `bipartite.go` - The `cost` function and `dist` matrix
The `cost` function uses `dist[gi][cands[c]]`. The `dist` matrix is built in `dedupeCluster`:
```go
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        d := pairDistance(keys[i], keys[j], tokens[i], tokens[j])
        dist[i][j], dist[j][i] = d, d
    }
}
```

So `dist` is symmetric. The `cost` function uses `dist[gi][cands[c]]` for `gi` in groups[r] and `cands[c]`. Good.

### Issue 62: `ac3_isolation_test.go` - The `mf` helper
The test uses `mf(...)` which is presumably a helper to create a `stream.Finding`. This must be defined somewhere. Let me check if it's a new helper.

Looking at the test:
```go
mf("HIGH", "auth.go", 42, "token never expires unchecked", "guard it", "security", 15, "pool saw it", "greta"),
```

So `mf(severity, file, line, problem, suggestion, category, confidence, evidence, reviewer)`. This is a new helper. Where is it defined?

I don't see `mf` in the diff. It must be defined in another file. Let me not flag this as an issue since it's likely defined.

### Issue 63: `bipartite.go` - The algorithm's correctness for non-square matrices
The function `hungarianAssign` handles rectangular matrices by padding to a square. This is correct.

But wait, what if rows > cols? Then we have more rows than columns. The padding adds virtual columns. Rows matched to virtual columns get assign = -1. This means some rows are unmatched. Correct.

What if cols > rows? Then we have more columns than rows. The padding adds virtual rows. Virtual rows have no real cost (they're all noMatchSentinel). The algorithm will match real columns to virtual rows, but the output only considers real rows. So all real rows are matched to real columns. The extra real columns are unused. Correct.

But wait, in the bipartite context, if a source has more findings than the existing groups, some findings are unmatched (assigned to padding). If a source has fewer findings than groups, some groups don't get a new member. Both cases are handled.

### Issue 64: `bipartiteGroups` - When first source has multiple findings
The first source seeds each finding as a singleton group. Subsequent sources match against these. This means the first source's findings are never compared with each other.

For example, if source A has [X, Y] and source B has [Z]:
- A first: groups = [[X], [Y]]
- B: Z is matched against [[X], [Y]]. If Z is mergeable with X, group = [[X, Z], [Y]]. If Z is mergeable with Y, group = [[X], [Y, Z]].

This is correct: same-source findings are not compared.

### Issue 65: `bipartiteGroups` - Ordering of first source
The first source is the lexicographically smallest source key. This is deterministic. But the choice of "first source" affects the matching.

For example, if source A is first and source B is second, then A's findings are seeded as groups, and B's findings are matched against A's. If source B is first and source A is second, then B's findings are seeded, and A's findings are matched against B's.

The sprint plan says: "Ordering sensitivity is bounded for typical N ≤ 5 reviewers." So this is an accepted limitation.

But wait, the bipartiteGroups sorts sources and processes them in sorted order. So the smallest source (lexicographically) is always first. This is deterministic.

### Issue 66: `bipartiteGroups` - The cost function and group expansion
When a finding is added to a group, the group's member list grows. Future matchings against this group use the expanded member list. This means: a finding can be added to a group even if it's only mergeable with the new member (not the original).

For example:
- Group [A]: cost(X, [A]) = dist(A, X)
- X is added: Group [A, X]
- New finding Y: cost(Y, [A, X]) = min(dist(A, Y), dist(X, Y))
- If Y is mergeable with X (not A), Y can be added to the group.

This is "single-linkage" within the group: a new member only needs to be mergeable with one existing member. The 1:1 constraint prevents transitive over-merge (one source's finding cannot drag two non-duplicates into one group).

But within a group, a finding can be transitively connected: A-X-Y where A~X and X~Y but A~Y. This is the single-linkage behavior. Is this a problem?

The sprint plan says: "Bipartite 1:1 constraint bounds the over-merge risk of 'AST-match → distance 0': each finding matches at most one finding per other source, rather than single-linkage union-find collapsing transitively."

So the 1:1 constraint is across sources (one source per group member). Within a group, single-linkage is used. This is a reasonable trade-off.

### Issue 67: `bipartiteGroups` - The cost is min distance to any group member
The cost function:
```go
m := distanceCeiling
for _, gi := range groups[r] {
    if d := dist[gi][cands[c]]; d < m {
        m = d
    }
}
return m
```

This is the minimum distance from the candidate to any group member. So the cost is the "closest" match. This means: a candidate is assigned to the group it most resembles.

### Issue 68: `bipartiteGroups` - Acceptance requires mergeable with at least one member
```go
accept := false
for _, gi := range groups[r] {
    if mergeable(gi, cand) {
        accept = true
        break
    }
}
```

So the candidate is added only if it's mergeable with at least one existing group member. This is single-linkage acceptance.

### Issue 69: `dedupe.go` - The `keptAmb` and `keptIdx` filtering
```go
keptAmb := ambiguous[:0]
keptIdx := ambiguousIdx[:0]
for k, gp := range ambiguousPairs {
    if uf.find(gp[0]) != uf.find(gp[1]) {
        keptAmb = append(keptAmb, ambiguous[k])
        keptIdx = append(keptIdx, ambiguousIdx[k])
    }
}
ambiguous = keptAmb
```

Wait, `keptAmb = ambiguous[:0]` shares the backing array with `ambiguous`. Then `keptAmb = append(keptAmb, ambiguous[k])` writes to the same array. If `k` is greater than the current length of `keptAmb`, this would overwrite `ambiguous[k]`.

Let me trace:
- Suppose `ambiguous = [a, b, c]` and we keep `a` and `c` (drop `b`).
- `keptAmb = ambiguous[:0]` → `keptAmb` has length 0, cap 3, same backing array.
- k=0: uf.find(ambiguousPairs[0]) != uf.find(ambiguousPairs[1]) → keep. `keptAmb = append(keptAmb, ambiguous[0])` → keptAmb = [a], writes to index 0.
- k=1: drop. No append.
- k=2: uf.find != uf.find → keep. `keptAmb = append(keptAmb, ambiguous[2])` → keptAmb = [a, c], writes to index 1. But `ambiguous[1] = b` was already there. Overwrite? Wait, the append writes to index 1, which was `b`. So now `keptAmb[1] = c`. But `ambiguous[1]` is also `c` (since they share the backing array). So the original `b` is overwritten by `c`.

Hmm, this is the in-place filter idiom. The trick is: when you read `ambiguous[k]` and append to `keptAmb`, you're reading from a position that hasn't been overwritten yet (because `keptAmb` only writes to positions < its current length). The next read is at `k+1`, which is > the current length of `keptAmb`. So the original data is still there.

Wait, let me re-trace:
- keptAmb starts at length 0.
- k=0: read ambiguous[0] = a. Append to keptAmb. keptAmb = [a], length 1.
- k=1: read ambiguous[1] = b. If we drop, no append. If we keep, append. Suppose we drop.
- k=2: read ambiguous[2] = c. If we keep, append. keptAmb = [a, c], length 2. The append writes to index 1, which was originally b but is now being overwritten with c. The read was at index 2 (c), so we haven't read the overwritten data yet.

Yes, the in-place filter works because we only read at positions >= the current write position. The classic Go idiom.

But wait, what if we keep, drop, keep? Then:
- k=0: keep. keptAmb = [a], length 1. Write at index 0.
- k=1: drop.
- k=2: keep. keptAmb = [a, c], length 2. Write at index 1. But the read was at index 2 (c). The data at index 1 (b) is overwritten. But we don't need it anymore.

OK the idiom works.

But what about `keptIdx`? Same logic. Good.

### Issue 70: `dedupe.go` - The order of `ambiguous` and `noise`
```go
ambiguous = append(ambiguous, noise...)
```

So `noise` is appended after the surviving gray pairs. The order in `ambiguous` is: surviving gray pairs first, then noise clusters. This is deterministic.

### Issue 71: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` - `fakeGrouper`
Wait, the test doesn't use `fakeGrouper`. Let me re-check. The test directly calls `dedupeCluster(...)` with `make([]string, 4)` for keys and `map[string]bool{id: true}` for adjudicatedMerges. So no `fakeGrouper` is used.

But `TestClusterKeys_UsesGrouper` (in `dedupe_test.go`) uses `fakeGrouper{...}`. This is in the same package. Where is `fakeGrouper` defined?

I don't see `fakeGrouper` in the diff. It must be defined elsewhere (maybe in another test file). This is a potential issue if it's not defined.

Actually, looking at the diff, the existing `dedupe_test.go` was modified but doesn't have a `fakeGrouper` definition. The new test `TestClusterKeys_UsesGrouper` uses it but doesn't define it. So either:
1. `fakeGrouper` is defined in another file (test helper).
2. The test will fail to compile.

This is a HIGH severity issue if the test doesn't compile.

But wait, this is a code review, not a test execution. I should flag this as a potential issue.

### Issue 72: `bipartite.go` - The `assign` return value
```go
func hungarian(cost [][]float64) []int {
    n := len(cost)
    if n == 0 {
        return nil
    }
    ...
    assign := make([]int, n)
    for j := 1; j <= n; j++ {
        if p[j] >= 1 {
            assign[p[j]-1] = j - 1
        }
    }
    return assign
}
```

The function returns a permutation of 0..n-1. Each row is assigned to exactly one column. The `if p[j] >= 1` check is defensive: in a valid solution, p[j] >= 1 for all j. But if p[j] == 0 (unassigned), we skip. This means the corresponding row in `assign` stays at 0 (the zero value). This could be a silent bug if the algorithm has a bug that leaves some columns unassigned.

A safer approach would be to assert that all columns are assigned, or to use a sentinel value like -1 for unassigned.

But in practice, the Hungarian algorithm always assigns all columns (for a square matrix). So this is just defensive.

### Issue 73: `bipartite.go` - The `n := max(rows, cols)` for square matrix
```go
n := rows
if cols > n {
    n = cols
}
```

This is correct: n is the size of the square matrix.

### Issue 74: `bipartite.go` - The `cost` function is called only for real cells
```go
for r := 0; r < n; r++ {
    square[r] = make([]float64, n)
    for c := 0; c < n; c++ {
        if r < rows && c < cols {
            square[r][c] = cost(r, c)
        } else {
            square[r][c] = noMatchSentinel
        }
    }
}
```

The `cost` function is called exactly `rows × cols` times. Good.

### Issue 75: `bipartite.go` - Performance of cost computation
The `cost` function in `bipartiteGroups`:
```go
func(r, c int) float64 {
    m := distanceCeiling
    for _, gi := range groups[r] {
        if d := dist[gi][cands[c]]; d < m {
            m = d
        }
    }
    return m
}
```

For each (r, c) pair, it iterates over all members of groups[r]. This is O(|groups[r]|) per call. For `rows × cols` calls, the total is O(rows × cols × max_group_size).

For typical V < 50, max_group_size is small (usually 1-3 per source). So this is manageable.

But there's a potential optimization: precompute the min distance for each (r, c) pair before calling hungarianAssign. But the current implementation is correct.

### Issue 76: `bipartiteGroups` - The `cands` and `groups` are passed to cost
The `cands` slice contains indices into the original findings. The `groups[r]` also contains indices. So `dist[gi][cands[c]]` looks up the precomputed distance. Good.

### Issue 77: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` - Key 4 findings
The test has 4 findings. The keys are `make([]string, 4)` (all empty). The adjudicatedMerges is `map[string]bool{id: true}` where `id = AmbiguousID(a.File, a.Line, a.Problem, b.Problem)`.

So the keys are all empty. The mergeable predicate falls through to `classify`, which uses Jaccard. Good.

### Issue 78: `dedupe.go` - The `pairDistance` function
```go
func pairDistance(keyA, keyB string, tokA, tokB map[string]struct{}) float64 {
    if keyA != "" && keyA == keyB {
        return distanceFloor
    }
    _, sim := classify(tokA, tokB)
    return distanceCeiling - sim
}
```

When keys match, return 0. Otherwise, compute Jaccard and return 1 - Jaccard. This is the composite distance. Good.

### Issue 79: `dedupe.go` - The `classify` function
The `classify` function is called twice: once in `pairDistance` (for the distance matrix) and once in `mergeable` (for the merge predicate). This is redundant computation. But it's cheap.

Actually, this is a potential performance issue. The `classify` function is called O(V²) times in `pairDistance` and again in the `mergeable` predicate (called by `bipartiteGroups` for each assignment). The total is O(V²) + O(groups × cands × group_size). For V < 50, this is fine.

But we could precompute the mergeable matrix to avoid redundant computation. But the current code is correct.

### Issue 80: `bipartiteGroups` - Determinism of group ordering
```go
for _, g := range groups {
    sort.Ints(g)
}
sort.SliceStable(groups, func(i, j int) bool {
    return groups[i][0] < groups[j][0]
})
```

Groups are sorted by their lowest member index. This is deterministic. Good.

### Issue 81: `ac2_greedy_failures_test.go` - Test helper imports
The file imports only `sort` and `testing`. The helpers `fnd`, `length`, `eq`, `deepEq` are presumably defined in a shared test helper file. The `mf` helper is not used in this file. Good.

### Issue 82: `ac3_dbscan_test.go` - Test helper imports
The file imports only `testing`. The helpers `fnd`, `length`, `eq`, `isTrue` are used. They must be defined in a shared test helper file.

### Issue 83: `ac3_isolation_test.go` - `mf` helper
The test uses `mf(...)` extensively. This must be defined somewhere. If it's not defined, the test won't compile.

Looking at the signature: `mf(severity, file, line, problem, suggestion, category, confidence, evidence, reviewer)`. This creates a `stream.Finding` (from `github.com/samestrin/atcr/internal/stream`). The test uses `"github.com/samestrin/atcr/internal/stream"` for the `stream.Finding` type.

If `mf` is not defined, this is a HIGH severity issue.

### Issue 84: `reconcile.go` - The `Grouper` type
The code uses `opts.Grouper` of type `Grouper`. The `Grouper` type must be defined somewhere. Looking at the diff, I see `Grouper` referenced but not defined. It must be defined in the codebase.

### Issue 85: `dedupe_test.go` - `TestClusterKeys_UsesGrouper` - `fakeGrouper`
```go
keys := clusterKeys([]Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 20, "y", "r2")},
    fakeGrouper{keys: map[int]string{10: "K1", 20: ""}})
```

`fakeGrouper` is used as a `Grouper`. It must implement the `Grouper` interface (with a `GroupKey` method). If it's not defined, the test won't compile.

### Issue 86: Potential issue: `bipartiteGroups` does not handle the case where `len(groups) == 0` after first source
Wait, the first source always adds at least one group (for each finding). So `len(groups) >= 1` after the first source. Then for subsequent sources, we match against these groups. The function `hungarianAssign(0, cands, cost)` is never called (because `len(groups) >= 1` after the first source).

Actually, let me re-read:
```go
for _, s := range order {
    cands := bySource[s]
    if len(groups) == 0 {
        for _, c := range cands {
            groups = append(groups, []int{c})
        }
        continue
    }
    assign := hungarianAssign(len(groups), len(cands), ...)
    ...
}
```

So for the first source, `len(groups) == 0`, and we add each candidate as a singleton group. For subsequent sources, we match. Good.

But what if the first source has 0 findings? Then `cands` is empty, and the loop does nothing. The next source becomes the "first" source (groups is still empty). Good.

### Issue 87: `bipartiteGroups` - What if all findings are from the same source?
If all findings are from the same source, then:
- First (and only) source: groups = [[0], [1], ..., [n-1]]
- No more sources to process.
- groups is returned as n singleton groups.

This is the documented behavior (same-source duplicates stay separate). Good.

### Issue 88: `dedupe.go` - The `ambiguous` slice is built with `append`
The `ambiguous` slice grows as needed. The final `ambiguous = append(ambiguous, noise...)` adds noise at the end. Good.

### Issue 89: `dedupe.go` - The `out` slice
The `out` slice is built from `rootOrder`. Each root is expanded into its members. Solo roots that are noise are skipped. Good.

### Issue 90: `ac3_isolation_test.go` - `Emit` function
The test calls `Emit(dir, res)`. This must be a function in the `reconcile` package. It writes `findings.json` and `ambiguous.json` to the directory. The test reads these files. Good.

### Issue 91: `ac3_isolation_test.go` - `FindingsJSON` and `AmbiguousJSON` constants
The test uses `FindingsJSON` and `AmbiguousJSON` as filenames. These must be defined as constants in the package. The test asserts on the content of these files.

### Issue 92: Performance of the Hungarian algorithm
The algorithm is O(n³). For n = max(rows, cols) in the square matrix, and rows = len(groups), cols = len(cands). The groups can grow as more sources are processed. In the worst case, all findings are in one group, but each call to hungarianAssign is for a specific (groups, cands) pair.

For the typical case of V < 50 findings per location cluster and N <= 5 sources, the total work is bounded.

### Issue 93: `bipartiteGroups` - The cost function is called many times
For each subsequent source, hungarianAssign is called with (rows=len(groups), cols=len(cands)). The cost function is called `rows × cols` times. Each call iterates over `len(groups[r])`. So the total work per source is O(rows × cols × max_group_size).

For N sources with average findings F per source, the total work is O(N × F × F × F) = O(N × F³). For N=5 and F=10, this is 5000 operations. Fast.

### Issue 94: `dedupe.go` - The distance matrix is built once
```go
dist := make([][]float64, n)
for i := range dist {
    dist[i] = make([]float64, n)
}
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        d := pairDistance(keys[i], keys[j], tokens[i], tokens[j])
        dist[i][j], dist[j][i] = d, d
    }
}
```

The distance matrix is built once and reused. Good.

### Issue 95: `dedupe.go` - The tokens are computed once
```go
tokens := make([]map[string]struct{}, n)
for i, f := range cluster {
    tokens[i] = tokenize(f.Problem)
}
```

Tokens are computed once per finding. Good.

### Issue 96: `dedupe.go` - The `classify` function in `mergeable`
The `mergeable` predicate calls `classify` for every pair check. This is O(tokens_a + tokens_b) per call. For bipartiteGroups, this is called for each (group, candidate) pair where the candidate is accepted. So the total is bounded.

### Issue 97: `bipartite.go` - The `p` array is 1-indexed
The algorithm uses 1-indexed arrays (p[0] is scratch, p[1..n] are real). The output `assign` is 0-indexed. The conversion is done correctly:
```go
for j := 1; j <= n; j++ {
    if p[j] >= 1 {
        assign[p[j]-1] = j - 1
    }
}
```

### Issue 98: `bipartite.go` - The `way` array is 1-indexed
Same as above. `way[0]` is unused (or 0).

### Issue 99: `bipartite.go` - The `u` and `v` arrays
These are the dual variables (potentials). They are 0-indexed for rows and 1-indexed for columns? Let me check.

```go
u := make([]float64, n+1)  // 1-indexed
v := make([]float64, n+1)  // 1-indexed
```

Both are 1-indexed (size n+1). u[i] is for row i (1-indexed). v[j] is for column j (1-indexed).

In the algorithm:
```go
cur := cost[i0-1][j-1] - u[i0] - v[j]
```

So u[i0] and v[j] use 1-indexed values. The cost is 0-indexed. OK.

```go
u[p[j]] += delta
v[j] -= delta
```

p[j] is 1-indexed (the row assigned to column j). So u[p[j]] is valid. v[j] is 1-indexed. OK.

### Issue 100: `bipartite.go` - The `minv` and `used` arrays
```go
minv := make([]float64, n+1)  // 1-indexed
used := make([]bool, n+1)    // 1-indexed
```

Both 1-indexed. OK.

### Issue 101: `ac3_dbscan_test.go` - `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`
Wait, this test has 2 findings from the same source ("greta"). They have identical text. Bipartite: greta is the first (and only) source. groups = [[0], [1]]. No more sources. So groups = [[f1], [f2]] (singleton groups). The test expects `length(t, groups, 2)`. Good.

### Issue 102: `ac3_dbscan_test.go` - `TestDedupeCluster_UnattributedDuplicatesStillMerge`
2 findings, both with empty Reviewer. srcKeys are "\x00anon\x000" and "\x00anon\x001" (different). So they're treated as different sources.

Bipartite:
- First source "\x00anon\x000": groups = [[0]]
- Second source "\x00anon\x001": hungarianAssign(1, 1, cost). Cost = 1 - Jaccard = 1 - 1 = 0. Assignment: candidate 0 → group 0. mergeable(0, 0) = true (Jaccard 1). accept. Group = [0, 1]. used[0] = true. No more candidates.

Result: groups = [[0, 1]]. Test expects `length(t, groups, 1)` and `length(t, groups[0], 2)`. Good.

### Issue 103: `reconcile.go` - The `clusterKeys` function
```go
func clusterKeys(cluster []Finding, g Grouper) []string {
    keys := make([]string, len(cluster))
    if g == nil {
        return keys
    }
    for i, f := range cluster {
        keys[i] = g.GroupKey(f)
    }
    return keys
}
```

This is straightforward. The `Grouper` is an interface with a `GroupKey` method. The function returns the keys aligned by index with the cluster.

### Issue 104: `bipartiteGroups` - What if a source has findings that are all merged?
If all of a source's findings are accepted into existing groups, then no new groups are added. The loop:
```go
for ci, idx := range cands {
    if !used[ci] {
        groups = append(groups, []int{idx})
    }
}
```

Only unaccepted candidates become new groups. Good.

### Issue 105: `bipartiteGroups` - The first source's findings are all singletons
This is by design. Same-source findings are not compared. Each becomes its own group.

### Issue 106: `ac3_dbscan_test.go` - `TestAC3_GrayPairedFindingNotDoubleCountedAsNoise`
Let me re-verify. The test has:
- greta: "alpha beta gamma delta epsilon"
- kai: "alpha beta gamma delta epsilon" (identical to greta)
- mira: "alpha beta gamma zeta eta" (3 shared with greta: alpha, beta, gamma. 7 total tokens: alpha, beta, gamma, delta, epsilon for greta and alpha, beta, gamma, zeta, eta for mira. Union: 7. Intersection: 3. Jaccard: 3/7 = 0.429)

So:
- greta and kai: Jaccard 1, mergeable
- greta and mira: Jaccard 0.429, gray (in [0.4, 0.7))
- kai and mira: Jaccard 0.429, gray

Bipartite:
- greta (sorted first): groups = [[G]]
- kai: hungarianAssign(1, 1, cost). Cost = 1 - 1 = 0. K → G (mergeable). Group = [G, K].
- mira: hungarianAssign(1, 1, cost). Cost = 1 - 0.429 = 0.571. M → [G, K]. mergeable(G, M) = false (Jaccard 0.429 is gray). mergeable(K, M) = false. So not accepted. M is added as singleton: groups = [[G, K], [M]].

Now the gray pair loop:
- (G, M): groupOf[G] = 0, groupOf[M] = 1. Different. classify = relGray (0.429). id computed. adjudicatedMerges is nil. Add to ambiguous. ambiguousPairs = [[0, 1]]. ambiguousIdx = [[G, M]].
- (K, M): groupOf[K] = 0, groupOf[M] = 1. Different. classify = relGray. Add to ambiguous. ambiguousPairs = [[0, 1], [0, 1]]. ambiguousIdx = [[G, M], [K, M]].

So `ambiguous` has 2 entries. But these are duplicates (same group pair). Hmm, that's a problem.

Wait, let me re-read the gray pair loop:
```go
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        if groupOf[i] == groupOf[j] {
            continue
        }
        rel, sim := classify(tokens[i], tokens[j])
        if rel != relGray {
            continue
        }
        id := AmbiguousID(...)
        if adjudicatedMerges[id] {
            uf.union(groupOf[i], groupOf[j])
            continue
        }
        ambiguous = append(ambiguous, ...)
        ambiguousPairs = append(ambiguousPairs, ...)
        ambiguousIdx = append(ambiguousIdx, ...)
    }
}
```

So for (G, M) and (K, M), both are recorded. `ambiguous` has 2 entries. The IDs are different (because they have different problem texts).

But this is potentially a problem: M is involved in 2 gray pairs, but the test expects:
```go
length(t, groups, 2, "consensus pair + mira's gray singleton")
```

And then checks that mira is in a 2-finding gray pair (not a 1-finding noise cluster). The test doesn't check the number of gray pairs.

But wait, the test also says "Every sidecar entry mentioning mira must be a 2-finding gray pair, never a 1-finding noise cluster". So 2 gray pairs is OK, as long as none are 1-finding.

Actually, looking at the test:
```go
sawMira := false
for _, c := range amb {
    for _, f := range c.Findings {
        if f.Reviewer == "mira" {
            sawMira = true
            length(t, c.Findings, 2, "mira appears only in the gray pair, not as noise")
        }
    }
}
isTrue(t, sawMira, "mira is referenced in the gray sidecar")
```

So the test checks that each sidecar entry mentioning mira has 2 findings. Both (G, M) and (K, M) have 2 findings. So the test passes.

But the test also asserts:
```go
length(t, groups, 2, "consensus pair + mira's gray singleton")
```

So groups = [[G, K], [M]]. Sizes [2, 1]. OK.

Hmm, but this means the sidecar has duplicate entries (both mention mira, both are gray). Is this intentional?

Looking at the original `dedupeCluster` (before the rewrite), it had the same behavior: for each gray pair, add an entry. So this is the same behavior. OK.

But wait, in the test, the comment says "mira appears only in the gray pair, not as noise". The test checks that each sidecar entry mentioning mira has 2 findings. So mira is in 2 gray pairs (with G and with K), each of size 2. No noise entry. Good.

But DBSCAN: would M be noise? Let me check.
- G and K: cross-source, mergeable → denseNeighbor true
- M: no cross-source mergeable neighbor (M's only cross-source pairs are with G and K, both gray, not mergeable). So M is noise.
- But `dense = true` (G-K cluster formed).
- For M: labels[M] = dbscanNoise. grayInvolved[M]? Yes (M is in the gray pairs (G, M) and (K, M)). So skip M from noise.

Good, M is not isolated as noise.

### Issue 107: `ac3_dbscan_test.go` - `TestAC3_AllDistinctNoIsolation`
3 findings, all distinct, different sources. Jaccard 0 between any pair. So:
- bipartite: greta (sorted first): groups = [[G]]. kai: K matched against [G]. Cost = 1. mergeable = false. K added as singleton: groups = [[G], [K]]. mira: M matched against [[G], [K]]. Cost = 1 for both. Hungarian picks one (tie-break by lowest index). But mergeable is false. M added as singleton: groups = [[G], [K], [M]].
- Gray pair loop: no gray pairs (all are distinct, not relGray... wait, relDistinct).
- DBSCAN: no dense clusters. dense = false. No noise isolation.
- out = [[G], [K], [M]]. amb = [].

Test expects `length(t, groups, 3)` and `length(t, amb, 0)`. Good.

### Issue 108: `bipartite.go` - The `cost` function in `bipartiteGroups` uses `dist` from the outer scope
The `cost` closure captures `dist`, `groups`, `cands` from the outer scope. This is a closure. The closure is called by `hungarianAssign`, which is called by `bipartiteGroups`. The closure is called for each (r, c) pair.

This is correct. Closures in Go capture by reference.

### Issue 109: `bipartiteGroups` - The `assign` is the assignment
The `assign` returned by `hungarianAssign` is a slice of length `rows` (len(groups)). Each element is the column matched to that row, or -1 if unmatched.

The code iterates over `assign` and processes each assignment. Good.

### Issue 110: `bipartiteGroups` - What if a candidate is matched to a group but not accepted?
The code:
```go
for r, c := range assign {
    if c < 0 {
        continue
    }
    cand := cands[c]
    accept := false
    for _, gi := range groups[r] {
        if mergeable(gi, cand) {
            accept = true
            break
        }
    }
    if accept {
        groups[r] = append(groups[r], cand)
        used[c] = true
    }
}
for ci, idx := range cands {
    if !used[ci] {
        groups = append(groups, []int{idx})
    }
}
```

If a candidate is matched to a group but not accepted, it's not added to the group, and `used[c]` is false. So the candidate becomes a new singleton group. Good.

### Issue 111: `bipartiteGroups` - The cost for unaccepted candidates
The cost for a candidate matched to a group is the minimum distance to any group member. If the candidate is not accepted, it becomes a new singleton. The cost is not used again.

But wait, the cost is the OPTIMAL cost (Hungarian). If the candidate is not accepted, the optimal cost is "wasted" in the sense that we don't merge. But this is the design: optimal assignment + acceptance gate.

### Issue 112: `ac2_greedy_failures_test.go` - `TestAC2_OptimalAssignmentPrefersClosestPairing`
The test has A, B, P, Q. The expected pairing is A-Q, B-P. Let me verify with the algorithm.

Bipartite:
- greta: groups = [[A], [B]] (sorted order of sources: greta, kai)
- kai: hungarianAssign(2, 2, cost).
  - cost(0, 0) = 1 - Jaccard(A, P) = 1 - 7/9 = 2/9
  - cost(0, 1) = 1 - Jaccard(A, Q) = 1 - 1 = 0
  - cost(1, 0) = 1 - Jaccard(B, P) = 1 - 1 = 0
  - cost(1, 1) = 1 - Jaccard(B, Q) = 1 - 7/9 = 2/9
  - Optimal assignment: A → Q (cost 0), B → P (cost 0). Total = 0.
  - Or A → P (2/9), B → Q (2/9). Total = 4/9.
  - First is better.
- For r=0, c=Q: mergeable(A, Q) = true. accept. Group [A] becomes [A, Q].
- For r=1, c=P: mergeable(B, P) = true. accept. Group [B] becomes [B, P].

Result: groups = [[A, Q], [B, P]]. After sorting by lowest member index: [A, Q] first (index 0), [B, P] second (index 1).

The test checks:
```go
for _, g := range groups {
    length(t, g, 2, "each group is a clean cross-source pair")
    eq(t, g[0].Problem == g[1].Problem, true, "paired findings share the same problem text")
}
```

Group [A, Q]: A.Problem = "shared1 ... aone", Q.Problem = "shared1 ... aone". Same.
Group [B, P]: B.Problem = "shared1 ... bone", P.Problem = "shared1 ... bone". Same.

Test passes.

### Issue 113: `bipartite.go` - The tie-breaking in Hungarian
The algorithm uses strict less-than for `cur < minv[j]` and `minv[j] < delta`. This means: when there are ties, the lowest-index column wins. This is deterministic.

But the sprint plan says: "Strict-less-than comparisons make the column tie-break favor the lowest index, so equal-cost inputs yield a deterministic, platform-stable assignment."

So this is the intended behavior.

### Issue 114: `bipartite.go` - The `delta` initialization
```go
delta := inf
```

This is correct. `delta` is the minimum of `minv[j]` over unvisited columns.

### Issue 115: `bipartite.go` - The `j1` initialization
```go
j1 := -1
```

`j1` is the column with the minimum `minv`. Initialized to -1. If all `minv[j]` are inf (shouldn't happen), `j1` stays -1. But in practice, at least one column has finite `minv`. So `j1` is set to a valid column.

### Issue 116: `bipartite.go` - The `p[0]` check
```go
if p[j0] == 0 {
    break
}
```

`p[j0] == 0` means column j0 is unassigned. So we found a free column to assign to row i0. Break.

### Issue 117: `bipartite.go` - The backtrack
```go
for j0 != 0 {
    j1 := way[j0]
    p[j0] = p[j1]
    j0 = j1
}
```

This is the standard augmenting path backtrack. `way[j]` is the previous column in the path. We update `p[j0]` to the row that was previously assigned to `way[j0]`, and continue.

### Issue 118: `bipartite.go` - The `assign` construction
```go
for j := 1; j <= n; j++ {
    if p[j] >= 1 {
        assign[p[j]-1] = j - 1
    }
}
```

For each column j (1-indexed), p[j] is the assigned row (1-indexed). We set assign[p[j]-1] = j - 1. So assign[r] = c means row r is assigned to column c.

If p[j] == 0 (shouldn't happen), we skip. The corresponding row in `assign` stays at 0 (the zero value). This is a potential silent bug, but in practice it doesn't happen.

### Issue 119: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`
Wait, the test uses `dedupeCluster` (lowercase, internal function). This is fine since the test is in the same package.

But the test passes `make([]string, 4)` for keys. The keys are all empty. The mergeable predicate falls through to `classify`. Good.

### Issue 120: `ac3_dbscan_test.go` - The `mf` helper is not used
Looking at the test, it uses `fnd(...)`, not `mf(...)`. So `mf` is not needed for this test. Good.

### Issue 121: `ac3_isolation_test.go` - The `mf` helper
This test uses `mf(...)` to create `stream.Finding` instances. The `fnd(...)` helper (used in other tests) presumably creates `reconcile.Finding` instances. These are different types.

`stream.Finding` is the wire format, `reconcile.Finding` is the internal representation. The `Reconcile` function takes `[]Source` which has `[]stream.Finding`. The `DedupeCluster` function takes `[]Finding` (the internal type). So the test correctly uses `mf` for `stream.Finding` and `fnd` for `reconcile.Finding`.

### Issue 122: `ac3_isolation_test.go` - The `Source` and `Reconcile` types
The test uses `Source` and `Reconcile` (capitalized). These are exported types/functions in the `reconcile` package. The test is in the same package, so it can access them.

### Issue 123: `ac3_isolation_test.go` - The `Emit` function
The test calls `Emit(dir, res)`. This is presumably a function in the `reconcile` package that writes the result to the directory.

### Issue 124: `ac3_isolation_test.go` - The `Options` type
The test uses `Options{ReconciledAt: time.Unix(1700000000, 0).UTC()}`. This sets the reconciled timestamp.

### Issue 125: `ac3_isolation_test.go` - The assertion on `res.Ambiguous`
The test checks:
```go
require.Len(t, res.Ambiguous, 1, "exactly one isolated cluster")
assert.Len(t, res.Ambiguous[0].Findings, 1, "noise cluster carries one finding")
assert.Equal(t, "totally unrelated hallucinated nonsense", res.Ambiguous[0].Findings[0].Problem)
```

So the test expects exactly 1 ambiguous cluster with 1 finding. This is the noise cluster (DBSCAN-isolated hallucination).

But wait, are there any gray pairs in this test? The findings are:
- pool: [HIGH auth.go:42 "token never expires unchecked", LOW auth.go:42 "totally unrelated hallucinated nonsense"]
- host: [HIGH auth.go:43 "token never expires unchecked"]

Jaccard(pool[0], host[0]) = 1 (identical text). So they're mergeable.
Jaccard(pool[0], pool[1]) = Jaccard("token never expires unchecked", "totally unrelated hallucinated nonsense") = 0 (disjoint). Not gray, not mergeable. Same source, so not compared in bipartite.

Jaccard(host[0], pool[1]) = Jaccard("token never expires unchecked", "totally unrelated hallucinated nonsense") = 0. Not gray.

So no gray pairs. The only ambiguous entry is the noise cluster.

Bipartite:
- pool (sorted first): groups = [[pool[0]], [pool[1]]]
- host: hungarianAssign(2, 1, cost).
  - cost(0, 0) = 1 - Jaccard(pool[0], host[0]) = 1 - 1 = 0
  - cost(1, 0) = 1 - Jaccard(pool[1], host[0]) = 1 - 0 = 1
  - Hungarian picks: host[0] → pool[0] (cost 0). pool[1] matched to padding (-1).
  - assign = [0, -1].
- For r=0, c=0: cand = host[0]. mergeable(pool[0], host[0]) = true. accept. Group [pool[0]] becomes [pool[0], host[0]].
- For r=1, c=-1: skip.
- No more candidates. No new groups.

Result: groups = [[pool[0], host[0]], [pool[1]]].

DBSCAN:
- pool[0] and host[0]: cross-source, mergeable → denseNeighbor true
- pool[1]: no cross-source mergeable neighbor (only pair is with host[0], not mergeable). Noise.
- dense = true (pool[0]-host[0] cluster formed).

For pool[1]: labels[pool[1]] = dbscanNoise. grayInvolved[pool[1]]? No gray pairs. soloRoot[pool[1]]? Yes (singleton group). So pool[1] is added to noise.

noise = [AmbiguousCluster{Findings: [pool[1]], ...}].

In the output loop:
- root 0: idxs = [pool[0]_idx, host[0]_idx]. Not solo. Add to out: [pool[0], host[0]].
- root 1: idxs = [pool[1]_idx]. Solo AND noiseIdx[pool[1]] = true. Skip.

So out = [[pool[0], host[0]]]. amb = [noise cluster].

The test expects:
- `res.Ambiguous` has 1 entry with 1 finding. ✓
- `res.Ambiguous[0].Findings[0].Problem` = "totally unrelated hallucinated nonsense". ✓

But wait, the file content checks:
- `assert.NotContains(t, string(findings), "hallucinated nonsense", ...)` - the hallucination is NOT in findings.json. ✓
- `assert.Contains(t, string(amb), "hallucinated nonsense", ...)` - the hallucination IS in ambiguous.json. ✓
- `assert.Contains(t, string(findings), "token never expires", ...)` - the corroborated finding IS in findings.json. ✓

Good.

### Issue 126: `ac3_dbscan_test.go` - `TestAC3_SelfDuplicateIsNotCorroboration`
- greta: 2 identical findings (self-dup)
- host: 1 finding

Jaccard(greta[0], greta[1]) = 1. Jaccard(greta[0], host) = ? "token never expires unchecked here" vs "an entirely different real issue" = 0. Jaccard(greta[1], host) = 0.

Bipartite:
- greta: groups = [[greta[0]], [greta[1]]] (same source, separate groups)
- host: hungarianAssign(2, 1, cost).
  - cost(0, 0) = 1 - Jaccard(greta[0], host) = 1
  - cost(1, 0) = 1 - Jaccard(greta[1], host) = 1
  - Hungarian picks: host → greta[0] or greta[1] (tie, lowest index wins = greta[0]). assign = [0, -1].
- For r=0, c=0: cand = host. mergeable(greta[0], host) = false. Not accepted.
- For r=1, c=-1: skip.
- host not used, added as singleton: groups = [[greta[0]], [greta[1]], [host]].

DBSCAN:
- greta[0] and greta[1]: same source, not dense neighbor.
- greta[0] and host: not mergeable (Jaccard 0). Not dense neighbor.
- greta[1] and host: not mergeable. Not dense neighbor.
- No dense neighbors for any point. All noise. dense = false. No noise isolation.

out = [[greta[0]], [greta[1]], [host]]. amb = [].

Test expects:
- `length(t, amb, 0, ...)` ✓
- host is still in groups ✓

Good.

### Issue 127: `ac3_dbscan_test.go` - `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`
- greta: 2 identical findings.

Bipartite:
- greta: groups = [[0], [1]] (same source, separate groups)
- No more sources.

Result: groups = [[f1], [f2]].

DBSCAN: no cross-source mergeable pairs. dense = false. No isolation.

out = [[f1], [f2]]. amb = [].

Test expects `length(t, groups, 2)`. Good.

### Issue 128: `ac3_dbscan_test.go` - `TestDedupeCluster_UnattributedDuplicatesStillMerge`
- 2 findings, both unattributed (empty Reviewer).

srcKeys = ["\x00anon\x000", "\x00anon\x001"]. Different sources.

Bipartite:
- "\x00anon\x000" (sorted first): groups = [[0]]
- "\x00anon\x001": hungarianAssign(1, 1, cost). Cost = 0. Assignment: 1 → 0. mergeable(0, 1) = true. accept. Group = [0, 1].

Result: groups = [[0, 1]].

Test expects `length(t, groups, 1)` and `length(t, groups[0], 2)`. Good.

### Issue 129: `bipartite.go` - The `cost` function and `cands`
The `cands` slice contains indices into the original findings. The `cost` function uses `cands[c]` to look up the candidate's index, then uses `dist[gi][cands[c]]` to get the distance.

### Issue 130: `bipartiteGroups` - The `groups` slice is modified in place
The `groups` slice is modified as candidates are added. The cost function uses the current state of `groups`. This is correct: as groups grow, the cost for new candidates reflects the new group members.

### Issue 131: `bipartiteGroups` - The `assign` is the result of Hungarian
The `assign` returned by `hungarianAssign` is a permutation of 0..rows-1 (with -1 for unmatched rows). The code iterates over `assign` and processes each assignment.

### Issue 132: `bipartiteGroups` - The `cands` and `groups` are used in the closure
The closure captures `dist`, `groups`, `cands` from the outer scope. The `groups` slice is modified during the loop (as candidates are added). The closure sees the updated `groups`.

### Issue 133: `bipartiteGroups` - The closure is called by `hungarianAssign`
The closure is called for each (r, c) pair in the square matrix. The result is the cost for that pair.

### Issue 134: `ac2_greedy_failures_test.go` - The `length` helper
The `length` helper is used extensively. It must be defined in a shared test helper file.

### Issue 135: `ac2_greedy_failures_test.go` - The `eq` helper
The `eq` helper is used. It must be defined in a shared test helper file.

### Issue 136: `ac2_greedy_failures_test.go` - The `deepEq` helper
The `deepEq` helper is used. It must be defined in a shared test helper file.

### Issue 137: `ac2_greedy_failures_test.go` - The `fnd` helper
The `fnd` helper is used. It must be defined in a shared test helper file.

### Issue 138: `bipartite_test.go` - The `isTrue` helper
The `isTrue` helper is used. It must be defined in a shared test helper file.

### Issue 139: `dbscan_test.go` - The `notEq` helper
The `notEq` helper is used. It must be defined in a shared test helper file.

### Issue 140: `distance_test.go` - The `inDelta` helper
The `inDelta` helper is used. It must be defined in a shared test helper file.

### Issue 141: `dedupe_test.go` - The `fakeGrouper` is used but not defined
This is a potential issue. If `fakeGrouper` is not defined, the test won't compile.

### Issue 142: `ac3_isolation_test.go` - The `mf` helper is used but not defined
This is a potential issue. If `mf` is not defined, the test won't compile.

### Issue 143: `bipartite.go` - The `p[0]` is used as scratch
In the Hungarian algorithm, `p[0]` is the scratch slot. The initial assignment is `p[0] = i` (the current row). The algorithm uses `p[j] == 0` to check if column j is unassigned.

### Issue 144: `bipartite.go` - The `way` array
`way[j]` is the previous column in the augmenting path. Initialized to 0. Updated when `minv[j]` is updated.

### Issue 145: `bipartite.go` - The `minv` array
`minv[j]` is the minimum reduced cost for column j. Initialized to inf. Updated as the algorithm progresses.

### Issue 146: `bipartite.go` - The `used` array
`used[j]` marks column j as visited in the current iteration. Initialized to false.

### Issue 147: `bipartite.go` - The `u` and `v` arrays
`u[i]` is the potential for row i. `v[j]` is the potential for column j. Initialized to 0.

### Issue 148: `bipartite.go` - The algorithm is O(n³)
The outer loop is O(n). The inner loop is O(n²) per iteration (for each j, we scan all columns). So the total is O(n³).

### Issue 149: `bipartite.go` - The algorithm is correct for square matrices
The algorithm is the standard Jonker-Volgenant variant of the Hungarian algorithm. It's correct for square matrices.

### Issue 150: `bipartite.go` - The algorithm handles non-square matrices via padding
The `hungarianAssign` function pads the matrix to a square with `noMatchSentinel`. The algorithm then runs on the square matrix. Unmatched rows (matched to padding) get `assign[r] = -1`.

### Issue 151: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` - the `id` computation
```go
id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
```

The `AmbiguousID` function is presumably defined in the package. It generates a unique ID for the pair.

### Issue 152: `dedupe.go` - The `mergeable` predicate
The `mergeable` predicate checks AST key match first, then Jaccard merge. Both are deterministic.

### Issue 153: `dedupe.go` - The `classify` function
The `classify` function is called from `pairDistance` and `mergeable`. It returns the relation (relMerge, relGray, relDistinct) and the similarity.

### Issue 154: `dedupe.go` - The `relMerge`, `relGray`, `relDistinct` constants
These are presumably defined in the package. They are the possible relations returned by `classify`.

### Issue 155: `ac3_dbscan_test.go` - `TestAC3_GrayPairedFindingNotDoubleCountedAsNoise` - the `eq` helper
The `eq` helper is used for assertions. It must be defined.

### Issue 156: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` - the `dedupeCluster` call
The test calls `dedupeCluster` (lowercase) directly. This is an internal function. The test is in the same package, so it can access it.

### Issue 157: `ac2_greedy_failures_test.go` - The `DedupeCluster` call
The test calls `DedupeCluster` (capitalized), the exported function. This is correct.

### Issue 158: `dedupe_test.go` - The `dedupeCluster` call
The test calls `dedupeCluster` (lowercase) directly. This is an internal function. The test is in the same package.

### Issue 159: `ac3_dbscan_test.go` - The `DedupeCluster` calls
The test calls `DedupeCluster` (capitalized) in some tests and `dedupeCluster` (lowercase) in others. Both are in the same package.

### Issue 160: `bipartite.go` - The `hungarian` function is unexported
The function is unexported (lowercase first letter). This is correct for an internal function.

### Issue 161: `bipartite.go` - The `hungarianAssign` function is unexported
Same as above.

### Issue 162: `bipartite.go` - The `bipartiteGroups` function is unexported
Same as above.

### Issue 163: `dbscan.go` - The `dbscanLabels` function is unexported
Same as above.

### Issue 164: `dbscan.go` - The `dbscanMinPts`, `dbscanNoise`, `dbscanUnvisited` constants are unexported
Same as above.

### Issue 165: `distance.go` - The `pairDistance` function is unexported
Same as above.

### Issue 166: `distance.go` - The `distanceFloor`, `distanceCeiling` constants are unexported
Same as above.

### Issue 167: `reconcile.go` - The `clusterKeys` function is unexported
Same as above.

### Issue 168: `dedupe.go` - The `DedupeCluster` function is exported
This is the public API.

### Issue 169: `dedupe.go` - The `dedupeCluster` function is unexported
This is the internal implementation.

### Issue 170: `ac3_dbscan_test.go` - The test `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`
This test has 2 findings from the same source. They have identical text. Bipartite keeps them separate. The test expects `length(t, groups, 2)`. Good.

But wait, the test name says "SameSourceDuplicatesDoNotMerge". This is the documented behavior. But is this the desired behavior? The sprint plan says: "Bipartite 1:1 constraint bounds the over-merge risk". The 1:1 is across sources. So same-source duplicates are not merged. This is by design.

### Issue 171: `ac3_dbscan_test.go` - The test `TestDedupeCluster_UnattributedDuplicatesStillMerge`
Unattributed findings (empty Reviewer) are treated as their own source. So they can merge with each other. This prevents the "pseudo-source" problem where all unattributed findings collapse into one non-matchable source.

### Issue 172: `bipartite.go` - The `noMatchSentinel` is 2.0
The sentinel is 2.0, which is greater than any real distance (max 1.0). So the Hungarian algorithm will never prefer a padded cell over a real one. Good.

### Issue 173: `ac3_dbscan_test.go` - `TestAC3_GrayPairedFindingNotDoubleCountedAsNoise` - the expected behavior
The test expects:
- 2 groups: [G, K] and [M]
- mira is in a gray pair (2 findings), not a noise cluster (1 finding)

The test checks that mira is referenced in the sidecar, and each sidecar entry mentioning mira has 2 findings. This is correct.

### Issue 174: `ac3_dbscan_test.go` - `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` - the expected behavior
The test expects:
- 2 groups: [A, B] (adjudicated) and [C, D] (merged)
- 0 ambiguous entries (no noise isolation)

The adjudicated pair A-B is merged. The D-E consensus pair is merged. No noise isolation. Good.

### Issue 175: `bipartite.go` - The `cost` function in `bipartiteGroups` - potential O(V²) per call
Wait, the cost function iterates over `groups[r]` for each (r, c). So the cost is O(|groups[r]|) per call. For `rows × cols` calls, the total is O(rows × cols × max_group_size).

For typical V < 50, this is O(50 × 50 × 10) = 25000 operations. Fast.

### Issue 176: `dedupe.go` - The `mergeable` predicate - potential O(V) per call
The `mergeable` predicate calls `classify`, which is O(|tokens_a| + |tokens_b|). For typical problems, tokens are small (< 20). So this is fast.

### Issue 177: `bipartiteGroups` - The first source's findings are all singletons
This is by design. The first source seeds the groups.

### Issue 178: `bipartiteGroups` - Subsequent sources match against existing groups
Each finding of a subsequent source is matched against the existing groups. The cost is the minimum distance to any group member. Acceptance requires mergeable with at least one member.

### Issue 179: `bipartiteGroups` - The `cands` slice is iterated twice
First, to compute the assignment. Then, to add unaccepted candidates as new groups. This is correct.

### Issue 180: `bipartiteGroups` - The `used` array tracks accepted candidates
Candidates that are accepted are marked as used. Unaccepted candidates become new singleton groups. Good.

### Issue 181: `bipartiteGroups` - The `groups` slice grows monotonically
Groups are only added, never removed. This is correct.

### Issue 182: `ac2_greedy_failures_test.go` - The test name `TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates`
This test verifies that bipartite matching resolves the transitive bridge failure mode of greedy clustering. Good.

### Issue 183: `ac2_greedy_failures_test.go` - The test `TestAC2_OptimalAssignmentPrefersClosestPairing`
This test verifies that optimal assignment prefers the closest pairing. Good.

### Issue 184: `ac2_greedy_failures_test.go` - The test `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother`
This test verifies that a single finding from one source cannot absorb two findings from another source. Good.

### Issue 185: `ac3_dbscan_test.go` - The test `TestAC3_IsolatesHallucinationAmidConsensus`
This test verifies that a single uncorroborated finding amid a consensus is isolated as noise. Good.

### Issue 186: `ac3_dbscan_test.go` - The test `TestAC3_SoloFindingIsNotIsolated`
This test verifies that a solo finding (no corroboration context) is not isolated as noise. Good.

### Issue 187: `ac3_dbscan_test.go` - The test `TestAC3_AllDistinctNoIsolation`
This test verifies that mutually distinct findings are not isolated as noise. Good.

### Issue 188: `ac3_dbscan_test.go` - The test `TestAC3_GrayPairedFindingNotDoubleCountedAsNoise`
This test verifies that a gray-paired finding is not also isolated as noise. Good.

### Issue 189: `ac3_dbscan_test.go` - The test `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`
This test verifies that an adjudicated merge is not double-counted as noise. Good.

### Issue 190: `ac3_dbscan_test.go` - The test `TestAC3_SelfDuplicateIsNotCorroboration`
This test verifies that a self-duplicate (same source repeating) is not considered corroboration. Good.

### Issue 191: `ac3_dbscan_test.go` - The test `TestDedupeCluster_SameSourceDuplicatesDoNotMerge`
This test verifies the documented behavior that same-source duplicates stay separate. Good.

### Issue 192: `ac3_dbscan_test.go` - The test `TestDedupeCluster_UnattributedDuplicatesStillMerge`
This test verifies that unattributed duplicates still merge. Good.

### Issue 193: `bipartite_test.go` - The test `TestHungarian_2x2Diagonal`
This test verifies the Hungarian algorithm on a 2x2 diagonal matrix. Good.

### Issue 194: `bipartite_test.go` - The test `TestHungarian_2x2AntiDiagonal`
This test verifies the Hungarian algorithm on a 2x2 anti-diagonal matrix. Good.

### Issue 195: `bipartite_test.go` - The test `TestHungarian_3x3KnownOptimum`
This test verifies the Hungarian algorithm on a 3x3 matrix with a known optimum. Good.

### Issue 196: `bipartite_test.go` - The test `TestHungarian_Deterministic`
This test verifies that the Hungarian algorithm is deterministic. Good.

### Issue 197: `bipartite_test.go` - The test `TestHungarianAssign_Rectangular`
This test verifies that hungarianAssign handles rectangular matrices. Good.

### Issue 198: `bipartite_test.go` - The test `TestHungarianAssign_MoreRowsThanCols`
This test verifies that hungarianAssign handles more rows than columns. Good.

### Issue 199: `bipartite_test.go` - The test `TestHungarianAssign_RealColumnPreferredOverPadding`
This test verifies that a real column is preferred over padding. Good.

### Issue 200: `dbscan_test.go` - The test `TestDBSCAN_IsolatesNoisePoint`
This test verifies that DBSCAN isolates a noise point. Good.

### Issue 201: `dbscan_test.go` - The test `TestDBSCAN_AllIsolatedNoDenseCluster`
This test verifies that DBSCAN labels all points as noise when no cluster forms. Good.

### Issue 202: `dbscan_test.go` - The test `TestDBSCAN_DensityConnectedChain`
This test verifies that DBSCAN connects density-reachable points into one cluster. Good.

### Issue 203: `dbscan_test.go` - The test `TestDBSCAN_MinPtsThreeLeavesPairAsNoise`
This test verifies that minPts=3 leaves a pair as noise. Good.

### Issue 204: `dbscan_test.go` - The test `TestDBSCAN_Deterministic`
This test verifies that DBSCAN is deterministic. Good.

### Issue 205: `distance_test.go` - The test `TestPairDistance_SameASTKeyIsZero`
This test verifies that shared AST key gives distance 0. Good.

### Issue 206: `distance_test.go` - The test `TestPairDistance_DifferentASTKeysFallToJaccard`
This test verifies that different keys fall to Jaccard. Good.

### Issue 207: `distance_test.go` - The test `TestPairDistance_EmptyKeysUseJaccard`
This test verifies that empty keys use Jaccard. Good.

### Issue 208: `distance_test.go` - The test `TestPairDistance_IdenticalTextEmptyKeysIsZero`
This test verifies that identical text with empty keys gives distance 0. Good.

### Issue 209: `distance_test.go` - The test `TestPairDistance_OneEmptyKeyIsNotAMatch`
This test verifies that one empty key is not a match. Good.

### Issue 210: `distance_test.go` - The test `TestPairDistance_Symmetric`
This test verifies that the distance is symmetric. Good.

### Issue 211: `dedupe_test.go` - The test `TestDedupeCluster_BipartiteRefusesTransitiveOverMerge`
This test verifies that bipartite matching refuses the transitive over-merge. Good.

### Issue 212: `dedupe_test.go` - The test `TestDedupeCluster_SharedASTKeyMergesDisjointText`
This test verifies that shared AST key merges findings with disjoint text. Good.

### Issue 213: `dedupe_test.go` - The test `TestDedupeCluster_AdjudicatedGrayPairMerges`
This test verifies that an adjudicated gray pair is merged. Good.

### Issue 214: `dedupe_test.go` - The test `TestClusterKeys_UsesGrouper`
This test verifies that `clusterKeys` threads the grouper correctly. Uses `fakeGrouper`.

### Issue 215: `ac3_isolation_test.go` - The test `TestAC3_HallucinationIsolatedToAmbiguousSidecar`
This is the end-to-end test. It calls `Reconcile`, `Emit`, reads files, and checks assertions. Good.

### Summary of issues found

Looking at the diff, I don't see any clear bugs. The code is well-structured and the tests are comprehensive. The main concerns are:

1. The `fakeGrouper` and `mf` helpers are used in tests but not defined in the diff. If they're not defined elsewhere, the tests won't compile. This is a potential issue.

2. The Hungarian algorithm implementation is correct but uses a defensive `if p[j] >= 1` check that could mask bugs.

3. The DBSCAN implementation is correct and deterministic.

4. The bipartite matching implementation is correct and handles edge cases.

5. The dedupe.go rewrite is correct and preserves the documented behavior.

6. The composite edge weight (AST key + Jaccard) is correct.

7. The tests are comprehensive and cover the main scenarios.

Let me look more carefully for any issues I might have missed...

Actually, let me check the `ac2_greedy_failures_test.go` test `TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates` more carefully. The expected `groupSizes(groups), []int{1, 2}`. Let me re-verify.

After my trace:
- groups = [[A, C], [B]]. Sizes: [2, 1]. Sorted: [1, 2]. ✓

And `amb` has 1 entry (the gray A-B pair). ✓

Wait, but I said the gray pair loop would record (A, B) as gray. Let me re-check. The findings are A (greta), B (kai), C (mira). After bipartite:
- groupOf[A] = 0 (group [A, C])
- groupOf[B] = 1 (group [B])
- groupOf[C] = 0

Gray pair loop:
- (A, B): groupOf[A] = 0, groupOf[B] = 1. Different. Jaccard = 0.667 → relGray. Add to ambiguous.
- (A, C): groupOf[A] = 0, groupOf[C] = 0. Same. Skip.
- (B, C): groupOf[B] = 1, groupOf[C] = 0. Different. Jaccard = 0.75 → relMerge. Not gray. Skip.

So `ambiguous` has 1 entry (A, B). `ambiguousPairs` = [[0, 1]]. `ambiguousIdx` = [[A, B]].

No adjudication, so `keptAmb` = [A, B], `keptIdx` = [A, B].

`grayInvolved` = {A: true, B: true}.

DBSCAN:
- A and C: cross-source, mergeable (Jaccard 0.818). denseNeighbor true.
- A and B: not mergeable (Jaccard 0.667, gray). denseNeighbor false.
- B and C: cross-source, mergeable (Jaccard 0.75). denseNeighbor true.
- A: neighbors are C. So A has 1 cross-source mergeable neighbor. A is not noise (minPts=2 means 1 neighbor + self = 2 ≥ minPts, so A is a core point). Wait, the check is `len(nb) + 1 < minPts`, i.e., `len(nb) < minPts - 1 = 1`. So if len(nb) = 1, then 1 + 1 = 2 ≥ 2, so A is a core point. Good.
- B: neighbors are C. B is a core point.
- C: neighbors are A, B. C is a core point.

All three are in one cluster. No noise. dense = true.

For A: labels[A] = 0 (not noise). Skip.
For B: labels[B] = 0 (not noise). Skip.
For C: labels[C] = 0 (not noise). Skip.

noise = [].

Output:
- root 0: idxs = [A, C]. Not solo. Add to out: [A, C].
- root 1: idxs = [B]. Solo. noiseIdx[B]? No. Add to out: [B].

out = [[A, C], [B]]. amb = [(A, B)].

Test expects:
- `groupSizes(groups), []int{1, 2}` ✓
- `length(t, amb, 1)` ✓



### Issue 216: `ac2_greedy_failures_test.go` - The test name `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother`
This test verifies the 1:1 constraint. Good.

### Issue 217: `bipartite.go` - The `cost` function and `distanceCeiling`
The `cost` function initializes `m := distanceCeiling = 1.0`. If `groups[r]` is empty (shouldn't happen), `m` stays at 1.0. This is a reasonable default.

### Issue 218: `bipartiteGroups` - The `groups` slice is modified during iteration
The `groups` slice is modified as candidates are added. The closure used by `hungarianAssign` captures `groups` by reference, so it sees the updated `groups`. But `hungarianAssign` is called BEFORE any modifications (it computes the assignment based on the current `groups`). So the closure sees a consistent state during the call. Good.

### Issue 219: `bipartiteGroups` - The `assign` is computed once per source
For each source, the assignment is computed once. The assignment determines which group each candidate is matched to. Then the code iterates over the assignment and accepts/rejects. Good.

### Issue 220: `bipartiteGroups` - The `used` array
The `used` array tracks which candidates are accepted. Initialized to `false` for each source. After processing, unaccepted candidates become new groups. Good.

### Issue 221: `dedupe.go` - The `out` slice is built from `rootOrder`
The `rootOrder` is the order of roots in the order they were first seen. This is deterministic (since groups are processed in sorted order by their lowest member index). Good.

### Issue 222: `dedupe.go` - The `rootIdx` map
The `rootIdx` map maps each root to the list of finding indices in that root. The indices are in the order they were added to the group. Good.

### Issue 223: `dedupe.go` - The `sort.Ints(idxs)` in the output loop
The members are sorted by index before being materialized as `[]Finding`. This ensures stable order.
</think>
