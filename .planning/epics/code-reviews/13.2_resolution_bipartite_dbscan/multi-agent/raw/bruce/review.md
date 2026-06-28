<think>
Let me carefully review this diff for the Bipartite Matching & DBSCAN sprint plan (Epic 13.2). I need to focus on the in-scope files and work items:

**In scope:**
- `reconcile/dedupe.go` (partitioning rewrite)
- `reconcile/bipartite.go` (new - Hungarian algorithm)
- `reconcile/dbscan.go` (new - DBSCAN)
- `reconcile/distance.go` (new - composite edge weight)
- Test files for AC2/AC3
- `reconcile/reconcile.go` (changes to call new dedupeCluster signature)
- `reconcile/README.md` (docs update)

Let me systematically go through the changes:

### 1. `reconcile/bipartite.go`

**`hungarian` function:**
Looking at the classic Jonker-Volgenant / Kuhn-Munkres implementation. Let me trace through:

```go
u := make([]float64, n+1)
v := make([]float64, n+1)
p := make([]int, n+1)
way := make([]int, n+1)
for i := 1; i <= n; i++ {
    p[0] = i
    j0 := 0
    minv := make([]float64, n+1)
    used := make([]bool, n+1)
    for j := 0; j <= n; j++ {
        minv[j] = inf
    }
    ...
}
```

Wait, there's a problem here. In the canonical Hungarian algorithm with potentials (u, v) and the matching p, when we add a new row i, we should:
1. Start with a new potential update
2. Try to find augmenting path

Looking at the algorithm structure - the inner `for { ... }` is the augmenting path search. The code has:

```go
for {
    used[j0] = true
    i0 := p[j0]
    delta := inf
    j1 := -1
    for j := 1; j <= n; j++ {
        if used[j] {
            continue
        }
        cur := cost[i0-1][j-1] - u[i0] - v[j]
        if cur < minv[j] {
            minv[j] = cur
            way[j] = j0
        }
        if minv[j] < delta {
            delta = minv[j]
            j1 = j
        }
    }
    ...
    j0 = j1
    if p[j0] == 0 {
        break
    }
}
```

There's a potential issue with `i0 := p[j0]` - when j0 == 0, p[0] = i (the current row), so i0 = i, which is the unassigned row. That's correct for the start.

Then `j1 = -1` initially, and we set `j1 = j` when minv[j] < delta. The issue: what if all columns are used? Then j1 stays -1. But then we have a bug because j0 = j1 = -1 would be used.

Actually wait - the loop sets `j0 = j1` at the end. Initially j0 = 0, and j0 != 0 loop will break if p[j0] == 0. After the first iteration j0 = j1. If j1 is -1, then j0 = -1, and accessing p[j0] is out of bounds. But this shouldn't happen because we should have j1 set (there's at least one unused column with finite minv).

Hmm, but what if all columns become used before we find p[j0] == 0? Let me re-check. In each iteration:
- Mark j0 as used
- Find the smallest minv[j] over unused j
- Update potentials
- Move to j1

The algorithm should always find a path because we're in a connected graph (with sentinel). Actually, this looks like a problem if `j1` is never updated. Let me think again.

In each iteration, j0 is marked used. We iterate over all j from 1 to n. If all j are used, then the inner loop doesn't update delta or j1. Then we update potentials for all used j (delta stays inf so nothing changes), and then minv[j] -= inf (also no change). Then j0 = j1 = -1. That's a problem.

But wait - in the standard algorithm, there should always be at least one unused column. The way to guarantee this is that we have n+1 columns (0 to n) and only n rows to assign. Actually... the algorithm uses n columns and n rows, so this should be fine in theory. But if all n columns get used, we'd have a problem.

Actually, in the standard Hungarian implementation, the algorithm guarantees that we always find an augmenting path. The j0 = 0 starting point is "free", and we work to extend the path. But if all columns become used, that means we've explored all possible extensions, which shouldn't happen in a correct implementation.

Let me look more carefully... Actually I think this is the standard implementation. Let me move on.

**`hungarianAssign` function:**

```go
n := rows
if cols > n {
    n = cols
}
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
full := hungarian(square)
for r := 0; r < rows; r++ {
    if c := full[r]; c < cols {
        assign[r] = c
    }
}
return assign
```

This looks correct. But wait - what if the cost function panics on out-of-range? Let me check the call:

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

The cost function is called with `r < rows` and `c < cols` (as guarded in `hungarianAssign`). Good.

**`bipartiteGroups` function:**

```go
bySource := map[string][]int{}
var order []string
for i, key := range srcKeys {
    if _, seen := bySource[key]; !seen {
        order = append(order, key)
    }
    bySource[key] = append(bySource[key], i)
}
sort.Strings(order)
```

Wait, there's an issue here. `order` collects unique keys as they appear in srcKeys, then is sorted. The order is correct. But the way the order of original keys is preserved initially doesn't matter since we sort anyway.

```go
var groups [][]int
for _, s := range order {
    cands := bySource[s]
    if len(groups) == 0 {
        for _, c := range cands {
            groups = append(groups, []int{c})
        }
        continue
    }
    assign := hungarianAssign(len(groups), len(cands), func(r, c int) float64 {
        m := distanceCeiling
        for _, gi := range groups[r] {
            if d := dist[gi][cands[c]]; d < m {
                m = d
            }
        }
        return m
    })
    used := make([]bool, len(cands))
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
}
```

This looks reasonable. The approach is incremental pairwise: each new source is matched against existing groups.

Wait - but the `cands[c]` index `c` corresponds to position in the cost matrix, which is column. The hungarian function returns a permutation where `assign[r]` is the column matched to row r. The check `if c < 0` correctly handles unmatched rows. But here we're iterating cands by index `ci` (not `c`) to check `used[ci]`. Let me re-check.

Actually wait, the cost function uses `cands[c]` where c is the column index. So column c corresponds to candidate cands[c]. The assignment from hungarian is assign[r] = c. So assign[r] = c means row r (group r) is matched to column c (candidate cands[c]). 

When we iterate `for r, c := range assign`, `c` is the column index, and we use `cand := cands[c]`. Then we set `used[c] = true`. The `used` slice is indexed by column. And later `for ci, idx := range cands { if !used[ci] ... }`. So `ci` is the column index, and we check `used[ci]`. That matches. OK, that's correct.

But wait - what about when a row is matched to a virtual column (padding)? In that case, `c < cols` fails and `assign[r] = -1`. Then in bipartiteGroups, we `continue` for that row. But what if a row should be matched to a real column but the row's column is actually a virtual one? The hungarianAssign function already handles this - it only returns a valid column if `c < cols`. So this should be fine.

Hmm, but wait. The `hungarian` function is called on a square matrix. The returned assignment is a permutation of the columns. Some of those columns may be virtual. `hungarianAssign` translates -1 to the case where the matched column is virtual (>= cols). This is correct.

But there's a subtle issue: if a row's optimal match is a virtual column, but a real column is also available, the algorithm should prefer the real column if it has lower cost. The way the algorithm works with the noMatchSentinel = 2.0, this should be the case.

OK, I think the bipartite matching is mostly correct.

**Now let me look at the test for `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "alpha beta gamma delta epsilon zeta eta", "greta"), // A
    fnd("a.go", 1, "alpha beta gamma delta epsilon zeta p1", "kai"),    // P
    fnd("a.go", 1, "alpha beta gamma delta epsilon zeta q1", "kai"),    // Q
})
deepEq(t, groupSizes(groups), []int{1, 2}, "consensus pair + one stranded finding")
```

The sources are: greta, kai, kai. So P and Q are from the same source. Bipartite matching only matches between different sources, so P and Q can each match A. The 1:1 constraint means at most one of {P, Q} can match A. The other is stranded.

But wait - the Jaccard thresholds. Let me check the token sets:
- A: {alpha, beta, gamma, delta, epsilon, zeta, eta} = 7 tokens
- P: {alpha, beta, gamma, delta, epsilon, zeta, p1} = 7 tokens
- Q: {alpha, beta, gamma, delta, epsilon, zeta, q1} = 7 tokens

Jaccard A-P: 6/8 = 0.75 → merge (above 0.7)
Jaccard A-Q: 6/8 = 0.75 → merge (above 0.7)
Jaccard P-Q: 6/8 = 0.75 → merge (but same source, no comparison)

So A matches either P or Q. The other is stranded. Result: groups = [1, 2] where one group is {A, P} or {A, Q}, and the other is {Q} or {P}. 

**Now `TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C
})
```

Tokens:
- A: {w1,w2,w3,w4,w5,w6,w7,w8,w9,w10} = 10
- B: {w1,w2,w3,w4,w5,w6,w7,w8,c1,b1} = 10
- C: {w1,w2,w3,w4,w5,w6,w7,w8,w9,c1} = 10

Jaccard A-B: 8/12 = 0.667 → gray
Jaccard A-C: 9/11 = 0.818 → merge
Jaccard B-C: 9/11 = 0.818 → merge

Sources: greta, kai, mira. All different. 

Bipartite matching: 
- First source greta: groups = [{A}]
- Second source kai: match A-B. mergeable(A,B) is false (gray). So B starts new group. groups = [{A}, {B}]
- Third source mira: match against groups. distance(A,C) = 0.182, distance(B,C) = 0.182. Both are same. The Hungarian algorithm has a strict-less-than tie-break. Looking at the implementation, when costs are equal, it picks the lowest-index one (j1 with lowest j in the iteration). Hmm, let me think about this more carefully.

In the algorithm:
```go
for j := 1; j <= n; j++ {
    if used[j] {
        continue
    }
    cur := cost[i0-1][j-1] - u[i0] - v[j]
    if cur < minv[j] {
        minv[j] = cur
        way[j] = j0
    }
    if minv[j] < delta {
        delta = minv[j]
        j1 = j
    }
}
```

The delta is the minimum minv[j] over unused j. With equal costs, the j1 will be the first unused j (lowest index). So in our case, the third source's C will be matched to group 0 (A), not group 1 (B), because delta for j=1 is reached first.

Then:
- A-C: mergeable? Yes (Jaccard 0.818, above 0.7). So C joins A. groups = [{A,C}, {B}]
- amb: A-B is gray, not adjudicated, so it's in ambiguous. But wait, the comment says "the unresolved A-B pair is recorded for adjudication". 

Looking at the test:
```go
length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
```

So `amb` should have 1 entry. The code adds it during the gray pair scan:
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
        ambiguous = append(ambiguous, ...)
        ambiguousPairs = append(ambiguousPairs, [2]int{groupOf[i], groupOf[j]})
        ambiguousIdx = append(ambiguousIdx, [2]int{i, j})
    }
}
```

A and B are in different groups, so they are scanned. Their relation is gray. So one ambiguous entry is added. 

But wait, what about C? C is in the same group as A. So C-A wouldn't be scanned. But C is in a different group from B... no, wait. C is matched with A in the bipartite step. So groupOf[A] = groupOf[C] = 0, groupOf[B] = 1. So scanning A-B is the only gray pair. amb has 1 entry. ✓

**`TestAC2_OptimalAssignmentPrefersClosestPairing`:**

```go
groups, _ := DedupeCluster([]Finding{
    fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 aone", "greta"), // A
    fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 bone", "greta"), // B
    fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 bone", "kai"),   // P == B text → B~P merge
    fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 aone", "kai"),   // Q == A text → A~Q merge
})
```

Sources: greta, greta, kai, kai.

First source greta: A and B start new groups. groups = [{A}, {B}]
Second source kai: P and Q candidates. 
- cost(group 0, P) = 1 - Jaccard(A, P) = 1 - 1/9 = 8/9 (since they share 7/9 = 7 out of 9 tokens... wait)
- Let me re-tokenize: A = {shared1, shared2, shared3, shared4, shared5, shared6, shared7, aone} = 8 tokens. P = {shared1, shared2, shared3, shared4, shared5, shared6, shared7, bone} = 8 tokens.
- Jaccard(A, P) = 7/9 = 0.778 → above 0.7 → merge → distance 0.222
- Jaccard(A, Q) = 1.0 → distance 0
- Jaccard(B, P) = 1.0 → distance 0
- Jaccard(B, Q) = 7/9 = 0.778 → merge → distance 0.222

So cost matrix (rows=groups, cols=candidates):
- group 0 (A) vs P: 0.222
- group 0 (A) vs Q: 0
- group 1 (B) vs P: 0
- group 1 (B) vs Q: 0.222

Hungarian finds minimum cost assignment:
- Either (group 0, Q) and (group 1, P) → 0 + 0 = 0
- Or (group 0, P) and (group 1, Q) → 0.222 + 0.222 = 0.444

Optimal is (group 0, Q) and (group 1, P) → 0. So Q joins A, P joins B. Both mergeable. Result: groups = [{A, Q}, {B, P}] = 2 groups of 2. ✓

**Now let me think about edge cases and bugs.**

1. **Determinism issue with the Hungarian tie-break:**

The Hungarian algorithm with strict-less-than comparisons favors the lowest index. But the test `TestHungarian_Deterministic` only verifies all-equal cost (where all assignments are optimal). What about partial ties?

Actually, for this application, strict determinism is important. Let me check the test more carefully:

```go
func TestHungarian_2x2AntiDiagonal(t *testing.T) {
    cost := [][]float64{{2, 1}, {1, 2}}
    a := hungarian(cost)
    eq(t, totalCost(cost, a), 2.0, "anti-diagonal is optimal (1+1)")
    eq(t, a[0], 1, "row0→col1")
    eq(t, a[1], 0, "row1→col0")
}
```

OK, this tests the anti-diagonal. Row 0 has cost 2 to col 0 and 1 to col 1. Row 1 has cost 1 to col 0 and 2 to col 1. Optimal is row0→col1 (1) and row1→col0 (1), total 2. The test verifies a[0]=1, a[1]=0. 

2. **Bug in `bipartiteGroups` related to candidate ordering within a source:**

When processing a source with multiple candidates, the cost function uses `cands[c]` (the c-th candidate in source's order). But the order of candidates is determined by their position in `srcKeys`, which is the order in the input cluster. This is deterministic.

But wait - there's an issue with the `used[c]` tracking. `c` is the column index in the cost matrix. `used[c] = true` marks the candidate at column c. Then we check `if !used[ci]` to seed new groups. Since `ci` iterates over `cands` in order, this is correct.

3. **Edge case: empty source key vs non-empty key:**

In the test `TestDedupeCluster_SharedASTKeyMergesDisjointText`:
```go
groups, amb := dedupeCluster([]Finding{
    fnd("a.go", 1, "alpha beta", "greta"),
    fnd("a.go", 1, "gamma delta", "kai"),
}, []string{"a.go\x00H", "a.go\x00H"}, nil)
```

Two findings with the same non-empty AST key. The `mergeable` function returns true for shared keys. So they should merge. But wait - they have different sources (greta, kai), so the bipartite match runs. cost = 0 (same key). Hungarian assigns them. mergeable = true. They merge. ✓

4. **Critical observation: The test for greedy failures is `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother` - I want to verify the assignment works correctly:**

Sources in order: greta, kai, kai. 
- First source greta: groups = [{A}]
- Second source kai: 1 candidate (P). cost = 0.222 (Jaccard A-P = 0.75, distance 0.25... wait, let me recheck)

Actually, Jaccard A-P:
- A = {alpha, beta, gamma, delta, epsilon, zeta, eta} = 7 tokens
- P = {alpha, beta, gamma, delta, epsilon, zeta, p1} = 7 tokens
- |A ∩ P| = 6 (alpha, beta, gamma, delta, epsilon, zeta)
- |A ∪ P| = 8 (7 + 7 - 6)
- Jaccard = 6/8 = 0.75

0.75 >= 0.7 → merge → relMerge.

cost(group 0, P) = 1 - 0.75 = 0.25. Hungarian assigns P to group 0. mergeable(A, P) = true (relMerge). P joins A. groups = [{A, P}]. 

- Third source kai: 1 candidate (Q). cost(group 0, Q) = 0.25. Hungarian assigns Q to group 0. mergeable: Q vs A is relMerge (0.75), so mergeable(A, Q) is true. But Q is also a candidate and mergeable(Q, A) should be checked... well, we iterate groups. Q is checked against each group member. A is a member of group 0. So mergeable(A, Q) = true. Q joins A. groups = [{A, P, Q}].

Wait, but the test expects `groupSizes(groups) == []int{1, 2}`. That's a 2-group and a 1-group. But my analysis says we get a single 3-group!

Hmm, let me re-read the test:
```go
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta eta", "greta"), // A
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta p1", "kai"),    // P: vs A 0.75 merge
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta q1", "kai"),    // Q: vs A 0.75 merge
```

OK so all three have different sources? No, P and Q are both "kai". 

Wait, but the bipartiteGroups processes sources in sorted order. Order: greta, kai.

- First source greta: 1 candidate A. groups = [{A}]
- Second source kai: 2 candidates P, Q. 
  - cost(group 0, P) = 0.25, cost(group 0, Q) = 0.25
  - Hungarian: 1 row, 2 cols. The row takes cheapest col. Both are equal at 0.25. With strict-less-than tie-break, the lowest col wins. col 0 (P) wins.
  - assign[0] = 0 → P joins A. mergeable(A, P) = true. P joins A. groups = [{A, P}]. used[0] = true.
  - For Q: c was 0 (P), not -1 for Q's index. Q's column is 1, and we check used[1] = false. So Q seeds new group. groups = [{A, P}, {Q}].
  
Wait, but the test expects 1 group of 2 and 1 group of 1. That matches! ✓

Actually, I was confused. Let me re-read the bipartiteGroups code:

```go
for _, s := range order {
    cands := bySource[s]
    if len(groups) == 0 {
        for _, c := range cands {
            groups = append(groups, []int{c})
        }
        continue
    }
    assign := hungarianAssign(len(groups), len(cands), func(r, c int) float64 {
        m := distanceCeiling
        for _, gi := range groups[r] {
            if d := dist[gi][cands[c]]; d < m {
                m = d
            }
        }
        return m
    })
    used := make([]bool, len(cands))
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
}
```

OK so for kai with 2 candidates (P, Q) and 1 group (A):
- 1 row, 2 cols. cost = 0.25 for both. Hungarian assigns row 0 to col 0 (P). 
- assign[0] = 0. cand = cands[0] = P. accept = true (mergeable). P joins A. used[0] = true.
- Q is not used (used[1] = false). Seeds new group. groups = [{A, P}, {Q}].



But wait - what if the algorithm matches row 0 to col 1 (Q) due to different tie-break? Then P would seed new group, and we'd have {A, Q} and {P}. Both are valid 1:1 matchings. The test only checks group sizes, not the specific pairing. So `groupSizes(groups) == []int{1, 2}`. ✓

OK so the test passes regardless of which one of P or Q joins A.

5. **DBSCAN test - `TestAC3_AllDistinctNoIsolation`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "alpha beta gamma", "greta"),
    fnd("a.go", 1, "delta epsilon zeta", "kai"),
    fnd("a.go", 1, "eta theta iota", "mira"),
})
length(t, groups, 3, "all three remain as independent findings")
length(t, amb, 0, "no corroboration context → no isolation")
```

Tokens are all disjoint (Jaccard 0 → distinct). So no merges. groups = [{greta}, {kai}, {mira}] = 3 groups. 
DBSCAN: no neighbors. So no cluster. dense = false. No noise isolated. amb = 0. ✓

6. **`TestAC3_SoloFindingIsNotIsolated`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 5, "unused import lingers", "greta"),
})
length(t, groups, 1, "solo finding stays in the output")
length(t, amb, 0, "nothing isolated without a dense cluster for context")
```

One finding. n=1. tokens[0] = {unused, import, lingers}. 
- bipartiteGroups: 1 source, 1 candidate. groups = [{0}]. 
- amb: no pairs.
- DBSCAN: n=1, no neighbors, no dense cluster. dense = false. No noise. ✓

7. **`TestAC3_GrayPairedFindingNotDoubleCountedAsNoise`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "alpha beta gamma delta epsilon", "greta"),
    fnd("a.go", 1, "alpha beta gamma delta epsilon", "kai"), // merge with greta
    fnd("a.go", 1, "alpha beta gamma zeta eta", "mira"),     // 3/7 ~0.43 gray to the pair
})
length(t, groups, 2, "consensus pair + mira's gray singleton")
```

Tokens:
- greta: {alpha, beta, gamma, delta, epsilon} = 5
- kai: {alpha, beta, gamma, delta, epsilon} = 5
- mira: {alpha, beta, gamma, zeta, eta} = 5

Jaccard greta-kai: 5/5 = 1.0 → merge
Jaccard greta-mira: 3/7 = 0.429 → gray
Jaccard kai-mira: 3/7 = 0.429 → gray

Sources: greta, kai, mira. Sorted: greta, kai, mira.
- greta: groups = [{greta}]
- kai: cost(0, kai) = 0. Hungarian assigns to col 0. mergeable = true. kai joins greta. groups = [{greta, kai}]
- mira: 1 candidate, 1 group. cost(0, mira) = 0.571. Hungarian assigns. mergeable(greta, mira) = false (gray), mergeable(kai, mira) = false (gray). Don't accept. So mira is unmatched, seeds new group. groups = [{greta, kai}, {mira}]

Now the gray pair scan:
- greta-kai: same group, skip
- greta-mira: different groups, gray. ambiguous = [{greta, mira}]. 
- kai-mira: different groups, gray. ambiguous = [{greta, mira}, {kai, mira}].

Hmm, that's 2 ambiguous entries, not 1! The test expects `length(t, amb, 0)` for the sidecar, but the comment says "the unresolved A-B pair is recorded for adjudication" which seems contradictory.

Wait, let me re-read:
```go
length(t, groups, 2, "consensus pair + mira's gray singleton")
// Every sidecar entry mentioning mira must be a 2-finding gray pair, never a
// 1-finding noise cluster (no double representation).
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

So the test doesn't require `len(amb) == 1`, it just checks that mira appears in a 2-finding cluster. 

Now DBSCAN: denseNeighbor requires mergeable AND cross-source. greta and kai are mergeable (Jaccard 1.0) and cross-source. So they form a dense cluster. mira's neighbor to greta/kai: gray, not mergeable. So mira has no dense neighbors. mira is noise.
But: grayInvolved[mira] = true (mira is in the gray pair). So mira is excluded from noise. 
greta and kai: not noise (they're in the cluster). 

Result: no noise entries. groups = [{greta, kai}, {mira}]. ✓

Wait, but the test says `length(t, groups, 2)` - 2 groups. And we have [{greta, kai}, {mira}] = 2 groups. ✓

8. **`TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`:**

```go
a := fnd("x.go", 1, "alpha beta gamma", "mira")
b := fnd("x.go", 1, "alpha beta delta", "pool") // gray to A (0.5)
id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
groups, amb := dedupeCluster([]Finding{
    a, b,
    fnd("x.go", 1, "zeta eta theta iota kappa", "greta"),
    fnd("x.go", 1, "zeta eta theta iota kappa", "kai"), // identical → dense pair
}, make([]string, 4), map[string]bool{id: true})
length(t, groups, 2, "adjudicated A-B pair and the D-E consensus pair")
```

So we have:
- A: mira
- B: pool
- C: greta
- D: kai

Tokens:
- A: {alpha, beta, gamma} = 3
- B: {alpha, beta, delta} = 3
- C: {zeta, eta, theta, iota, kappa} = 5
- D: {zeta, eta, theta, iota, kappa} = 5

Jaccard A-B: 2/4 = 0.5 → gray
Jaccard A-C: 0/8 → distinct
Jaccard A-D: 0/8 → distinct
Jaccard B-C: 0/8 → distinct
Jaccard B-D: 0/8 → distinct
Jaccard C-D: 5/5 = 1.0 → merge

Sources sorted: greta, kai, mira, pool.
- greta (C): groups = [{C}]
- kai (D): cost = 0. mergeable. D joins C. groups = [{C, D}]
- mira (A): 1 candidate, 1 group. cost(0, A) = 1.0. mergeable(C, A) = false, mergeable(D, A) = false. Not accepted. A seeds new group. groups = [{C, D}, {A}]
- pool (B): 2 candidates, 2 groups. 
  - cost(0, B) = 1.0, cost(1, B) = 0.5.
  - Hungarian: row 0 → col 0 (cost 1.0), row 1 → col 1 (cost 0.5). Total = 1.5.
  - Or row 0 → col 1, row 1 → col 0: 0.5 + 1.0 = 1.5. Same.
  - With strict-less-than, the first j (col 0 for row 0) wins. assign[0] = 0, assign[1] = 1.
  - B assigned to group 0 (C, D). mergeable(C, B) = false, mergeable(D, B) = false. Not accepted.
  - B assigned to group 1 (A). mergeable(A, B) = false (gray). Not accepted.
  - So B is not used. Seeds new group. groups = [{C, D}, {A}, {B}]

Hmm, but the test expects 2 groups! What's going on?

Oh wait, the test uses adjudicatedMerges. Let me re-trace with adjudication.

The adjudicated merge ID is for the A-B gray pair. So during the gray pair scan, A and B are detected as gray, and the ID matches the adjudicated set, so they are unioned.

But wait, the gray pair scan happens AFTER bipartite grouping. Let me re-check the dedupeCluster flow:

```go
groups := bipartiteGroups(srcKeys, dist, mergeable)
groupOf := make([]int, n)
for gi, g := range groups {
    for _, idx := range g {
        groupOf[idx] = gi
    }
}

// Adjudicated gray pairs force-merge groups; union-find runs over GROUP ids.
uf := newUnionFind(len(groups))
var ambiguous []AmbiguousCluster
var ambiguousPairs [][2]int
var ambiguousIdx [][2]int
for i := 0; i < n; i++ {
    for j := i + 1; j < n; j++ {
        if groupOf[i] == groupOf[j] {
            continue
        }
        rel, sim := classify(tokens[i], tokens[j])
        if rel != relGray {
            continue
        }
        id := AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[j].Problem)
        if adjudicatedMerges[id] {
            uf.union(groupOf[i], groupOf[j])
            continue
        }
        ...
    }
}
```

So A and B are in different groups (0 and 1, or 1 and 2). Their relation is gray. ID is in adjudicatedMerges. So uf.union(groupOf[A], groupOf[B]) = uf.union(1, 2) (or 0, 1). 

After all gray pair processing, the union-find is used to merge groups:
```go
rootIdx := map[int][]int{}
var rootOrder []int
for gi, g := range groups {
    r := uf.find(gi)
    if _, seen := rootIdx[r]; !seen {
        rootOrder = append(rootOrder, r)
    }
    rootIdx[r] = append(rootIdx[r], g...)
}
```

So groups 1 and 2 (A and B's groups) are merged into one root. Result: 2 root groups.

Now DBSCAN. denseNeighbor requires mergeable AND cross-source. The dense cluster is C-D (greta, kai). C and D are mergeable. So they're a dense cluster. mira (A) and pool (B) are gray-paired, so they're noise candidates. But grayInvolved[A] = true, grayInvolved[B] = true. So they're excluded from noise. 

Result: groups = [{C, D}, {A, B}], amb = 0 (the A-B gray pair was adjudicated, so it was removed during the kept filter).

Test expects:
- `length(t, groups, 2)` ✓
- `length(t, amb, 0)` ✓
- Each group has 2 members ✓

OK so the test should pass.

But wait - looking at the test:
```go
length(t, groups, 2, "adjudicated A-B pair and the D-E consensus pair")
for _, g := range groups {
    length(t, g, 2, "each group has two members; nothing was wrongly isolated")
}
```

The D-E mention suggests the test creator thought of the findings as A, B, D, E. But the test uses fnd("x.go", 1, "alpha beta gamma", "mira") for A, fnd("x.go", 1, "alpha beta delta", "pool") for B, and then two more. The D-E in the comment doesn't match the actual variable names. Minor doc inconsistency, not a bug.

9. **`TestAC3_IsolatesHallucinationAmidConsensus`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked here", "greta"),
    fnd("a.go", 1, "token never expires unchecked here", "kai"),
    fnd("a.go", 1, "completely different spurious claim", "mira"),
})
length(t, groups, 1, "only the corroborated consensus pair remains in the output")
length(t, groups[0], 2, "greta + kai")
length(t, amb, 1, "the hallucination is isolated into the sidecar")
```

Tokens:
- greta: {token, never, expires, unchecked, here} = 5
- kai: {token, never, expires, unchecked, here} = 5
- mira: {completely, different, spurious, claim} = 4

Jaccard greta-kai: 5/5 = 1.0 → merge
Jaccard greta-mira: 0/9 → distinct
Jaccard kai-mira: 0/9 → distinct

Sources: greta, kai, mira. Sorted: greta, kai, mira.
- greta: groups = [{greta}]
- kai: cost 0, mergeable. kai joins greta. groups = [{greta, kai}]
- mira: cost(0, mira) = 1.0. mergeable = false. mira seeds new group. groups = [{greta, kai}, {mira}]

Gray pair scan:
- greta-kai: same group, skip
- greta-mira, kai-mira: distinct, not gray, skip

So amb = 0 initially.

DBSCAN: 
- denseNeighbor(greta, kai) = true (mergeable, cross-source). 
- denseNeighbor(greta, mira) = false (not mergeable).
- denseNeighbor(kai, mira) = false.
- For greta: neighbors = [kai]. len + 1 = 2 >= 2. So greta is core, label = 0.
- For kai: labeled 0 already.
- For mira: neighbors = []. len + 1 = 1 < 2. mira is noise. label = -1.
- dense = true (cluster 0 formed).
- grayInvolved: empty (no gray pairs).
- soloRoot: groups[1] (mira) has 1 member. soloRoot[mira] = true.
- mira is noise, not gray, is soloRoot. So noise = [mira]. noiseIdx[mira] = true.

Output: 
- For group 0 (greta, kai): len 2, not in noiseIdx. Keep. → [{greta, kai}]
- For group 1 (mira): len 1, noiseIdx[mira] = true. Skip. → []
- ambiguous = noise = [{mira}]

Result: groups = [{greta, kai}], amb = [{mira}]. ✓

10. **`TestAC3_SelfDuplicateIsNotCorroboration`:**

```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked here", "greta"),
    fnd("a.go", 1, "token never expires unchecked here", "greta"),
    fnd("a.go", 1, "an entirely different real issue", "host"),
})
length(t, amb, 0, "a self-duplicate is not corroboration → no isolation")
```

Sources: greta, greta, host. Sorted: greta, host.
- greta: 2 candidates (greta1, greta2). 0 groups initially. So each seeds a new group. groups = [{greta1}, {greta2}]
- host: 1 candidate (host1). cost(0, host1) = 1.0, cost(1, host1) = 1.0. Hungarian: equal cost, row 0 → col 0 (lowest j). assign[0] = 0.
  - mergeable(greta1, host1) = false. Not accepted. 
  - host1 is unmatched (assign[0] = 0, but not accepted, so used[0] is still false). Seeds new group. groups = [{greta1}, {greta2}, {host1}]

Gray pair scan:
- greta1-greta2: same source. But wait, the relation is computed regardless of source. Jaccard 1.0 → relMerge. So this would be merged by bipartite, but same source means no comparison in bipartite. 

Actually wait - in the gray pair scan, we check `if groupOf[i] == groupOf[j] { continue }`. greta1 and greta2 are in different groups. So they're scanned. relMerge, not relGray, so we `continue`. ✓

- greta1-host1: distinct, skip
- greta2-host1: distinct, skip

DBSCAN: 
- denseNeighbor requires mergeable AND cross-source.
- greta1 vs greta2: mergeable but same source → not dense.
- greta1 vs host1: not mergeable.
- All points are noise. No dense cluster. dense = false. No noise isolated. amb = 0. ✓

11. **`TestDedupeCluster_SameSourceDuplicatesDoNotMerge`:**

```go
groups, _ := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked", "greta"),
    fnd("a.go", 1, "token never expires unchecked", "greta"),
})
length(t, groups, 2, "same-source duplicates stay as separate findings")
```

Sources: greta, greta. Same source. Sorted: greta.
- greta: 2 candidates. groups initially empty. Each seeds new group. groups = [{greta1}, {greta2}]

Gray pair scan: greta1-greta2, relMerge, skip. amb = 0.

DBSCAN: same source, no dense. No noise. 

Result: groups = 2, amb = 0. ✓

12. **`TestDedupeCluster_UnattributedDuplicatesStillMerge`:**

```go
groups, _ := DedupeCluster([]Finding{
    fnd("a.go", 1, "token never expires unchecked", ""),
    fnd("a.go", 1, "token never expires unchecked", ""),
})
length(t, groups, 1, "unattributed duplicates merge")
length(t, groups[0], 2, "both members")
```

Reviewer is empty. srcKeys = ["\x00anon\x000", "\x00anon\x001"]. Different keys, so they're treated as different sources.

- First source "\x00anon\x000": groups = [{0}]
- Second source "\x00anon\x001": cost = 0 (Jaccard 1.0). mergeable. Joins. groups = [{0, 1}]
- amb = 0. No noise (DBSCAN: dense cluster is just 1 group of 2; no extra noise points).

Result: groups = [{0, 1}], 1 group of 2. ✓

13. **The `ac3_isolation_test.go` end-to-end test:**

```go
sources := []Source{
    {Name: "pool", Findings: []stream.Finding{
        mf("HIGH", "auth.go", 42, "token never expires unchecked", "guard it", "security", 15, "pool saw it", "greta"),
        mf("LOW", "auth.go", 42, "totally unrelated hallucinated nonsense", "n/a", "style", 5, "pool only", "greta"),
    }},
    {Name: "host", Findings: []stream.Finding{
        mf("HIGH", "auth.go", 43, "token never expires unchecked", "guard it", "security", 15, "host also", "host"),
    }},
}
```

Wait, this test uses mf() helper which creates a stream.Finding. The sources have:
- pool: 2 findings, both from reviewer "greta"
- host: 1 finding, from reviewer "host"

Reviewers: greta, greta, host. Different from sources (pool, host).

Sources in this test are reviewed at the Finding level. AllFindings flattens them. So the order is: pool[0] (greta), pool[1] (greta), host[0] (host). 

Then clusters are formed by proximity (file + line window). The two greta findings are at auth.go:42. The host finding is at auth.go:43. Line 43 is within 42±3, so they cluster together.

In the cluster [greta1, greta2, host], sources are greta, greta, host. 

- greta: 2 candidates. groups = [{greta1}, {greta2}]
- host: 1 candidate. cost(0, host) = Jaccard(greta1, host) = ?

Tokens:
- greta1: {token, never, expires, unchecked} = 4
- greta2: {totally, unrelated, hallucinated, nonsense} = 4
- host: {token, never, expires, unchecked} = 4

Jaccard greta1-host: 4/4 = 1.0 → merge
Jaccard greta2-host: 0/8 → distinct

cost(0, host) = 0 (greta1), cost(1, host) = 1.0 (greta2). Hungarian assigns row 0 → col 0 (cheapest). mergeable = true. host joins greta1. groups = [{greta1, host}, {greta2}]

DBSCAN: denseNeighbor(greta1, host) = true (mergeable, cross-source). So cluster forms. greta2 has no neighbors. greta2 is noise. But:
- grayInvolved[greta2]: need to check. The gray pair scan: greta1-greta2 are distinct, not gray. greta2-host are distinct, not gray. greta1-host same group, skip. So grayInvolved is empty.
- soloRoot[greta2] = true (only member of its group).

So greta2 is isolated as noise. noise = [greta2]. Output:
- group 0 (greta1, host): 2 members, not in noise. Keep.
- group 1 (greta2): 1 member, in noise. Skip.

groups = [{greta1, host}], amb = [noise(greta2)].

Then in the test:
```go
assert.NotContains(t, string(findings), "hallucinated nonsense", ...)
assert.Contains(t, string(amb), "hallucinated nonsense", ...)
assert.Contains(t, string(findings), "token never expires", ...)
require.Len(t, res.Ambiguous, 1, "exactly one isolated cluster")
assert.Len(t, res.Ambiguous[0].Findings, 1, "noise cluster carries one finding")
assert.Equal(t, "totally unrelated hallucinated nonsense", res.Ambiguous[0].Findings[0].Problem)
```

The findings.json should have the corroborated finding (token never expires), and ambiguous.json should have the hallucination. ✓

But wait - there's a potential issue. The test sources are in the order [pool, host]. AllFindings gives [pool[0], pool[1], host[0]]. The cluster ordering is by first appearance. The cluster has findings at indices 0, 1, 2 (greta, greta, host).

Hmm, but I need to check how the location clustering works. Let me look at the file... I don't have access to it, but based on the README, clusters are keyed on FILE and LINE ± 3 window. So all three findings are in the same cluster.

The test should work as I described.

14. **Now let me look at potential issues:**

a. **`dbscanLabels` - the noise label is shared with cluster IDs (cluster 0, 1, 2, ..., or -1 for noise):**

```go
const (
    dbscanNoise = -1
    dbscanUnvisited = -2
)
```

Cluster IDs start at 0 and increment. So label 0 is cluster 0, label 1 is cluster 1, etc. The label -1 is noise, -2 is unvisited. This is unambiguous.

b. **Issue with `bipartiteGroups` and source processing order:**

```go
bySource := map[string][]int{}
var order []string
for i, key := range srcKeys {
    if _, seen := bySource[key]; !seen {
        order = append(order, key)
    }
    bySource[key] = append(bySource[key], i)
}
sort.Strings(order)
```

`order` initially collects unique keys in input order, then gets sorted. OK, this is fine. But the comment says "Determinism: sources are processed in sorted order" - which matches.

c. **Potential bug in `bipartiteGroups` when processing a source that has multiple candidates and they all need to seed new groups:**

```go
for ci, idx := range cands {
    if !used[ci] {
        groups = append(groups, []int{idx})
    }
}
```

If a candidate is accepted into an existing group, used[ci] = true. Otherwise, it seeds a new group. ✓

But wait - what if a candidate is assigned by hungarianAssign to a virtual column? Then assign[r] = -1. The for loop does `continue` for c < 0. But the candidate's `used[ci]` is still false. So it would seed a new group. That's correct behavior.

What if a candidate is assigned to a real column but mergeable returns false? Then `accept = false`, so used[ci] remains false, and the candidate seeds a new group. ✓

d. **The cost function in `bipartiteGroups`:**

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

This computes the single-linkage (minimum) distance from group r to candidate c. The minimum over all group members. So the cost is the best match within the group. ✓

e. **`mergeable` is called for each pair (gi, cand) in the group:**

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

So we check if the candidate is mergeable with ANY member of the group. If yes, accept. This is the structural fix for the over-merge - the candidate joins the group with the best match, but only if it can merge with at least one member. ✓

f. **Looking at `dedupe.go` and the new `srcKeys`:**

```go
srcKeys := make([]string, n)
for i, f := range cluster {
    if f.Reviewer != "" {
        srcKeys[i] = f.Reviewer
    } else {
        srcKeys[i] = "\x00anon\x00" + strconv.Itoa(i)
    }
}
```

For each finding, srcKeys[i] is the Reviewer (or unique for empty). This is the "source" for bipartite matching. ✓

But wait - what if two different Reviewers have the same name? Then they get the same srcKey, and they're treated as the same source. That's correct behavior.

What if Reviewer is a NUL-containing string? The unique key is `"\x00anon\x00" + i`. Could a real Reviewer value contain `"\x00anon\x00"`? Theoretically yes, but in practice no (Reviewer comes from a tool name). The comment says "NUL-prefixing cannot collide with a stream-column reviewer value" - this is a reasonable assumption.

g. **The `dbscanLabels` function - let me trace through the dense cluster scenario:**

```go
func dbscanLabels(n, minPts int, neighbor func(i, j int) bool) (labels []int, dense bool) {
    labels = make([]int, n)
    for i := range labels {
        labels[i] = dbscanUnvisited
    }
    neighborsOf := func(p int) []int {
        var out []int
        for q := 0; q < n; q++ {
            if q != p && neighbor(p, q) {
                out = append(out, q)
            }
        }
        return out
    }
    cluster := 0
    for p := 0; p < n; p++ {
        if labels[p] != dbscanUnvisited {
            continue
        }
        nb := neighborsOf(p)
        if len(nb)+1 < minPts { // +1 counts p itself toward the core threshold
            labels[p] = dbscanNoise
            continue
        }
        labels[p] = cluster
        seeds := append([]int(nil), nb...)
        for k := 0; k < len(seeds); k++ {
            q := seeds[k]
            if labels[q] == dbscanNoise {
                labels[q] = cluster
            }
            if labels[q] != dbscanUnvisited {
                continue
            }
            labels[q] = cluster
            qn := neighborsOf(q)
            if len(qn)+1 >= minPts {
                seeds = append(seeds, qn...)
            }
        }
        cluster++
    }
    return labels, cluster > 0
}
```

For TestAC3_IsolatesHallucinationAmidConsensus:
- n=3, minPts=2
- p=0 (greta): neighborsOf(0) = [kai] (denseNeighbor is true). len+1=2 >= 2. label[0] = 0. seeds = [kai].
  - k=0: q=kai. label[1] = dbscanUnvisited. Set to 0. qn = neighborsOf(1) = [greta]. len+1=2 >= 2. seeds = [kai, greta].
  - k=1: q=greta. label[0] = 0 (not dbscanUnvisited). Continue.
- p=1 (kai): already labeled. Continue.
- p=2 (mira): neighborsOf(2) = []. len+1=1 < 2. label[2] = -1 (noise).
- cluster = 1, dense = true. ✓

For TestAC3_SoloFindingIsNotIsolated:
- n=1, minPts=2
- p=0: neighborsOf(0) = []. len+1=1 < 2. label[0] = -1 (noise).
- cluster = 0, dense = false (0 clusters). ✓

For TestAC3_AllDistinctNoIsolation:
- n=3, minPts=2
- p=0: no neighbors. label[0] = -1.
- p=1: already labeled (-1). Continue.
- p=2: already labeled (-1). Continue.
- cluster = 0, dense = false. ✓

OK the algorithm is correct.

h. **Wait, let me double-check the `dense` return:**

```go
return labels, cluster > 0
```

`cluster` is incremented at the end of each cluster's processing. So `cluster` is the number of clusters. dense = (cluster > 0). ✓

i. **Potential issue: in dedupeCluster, `for _, g := range groups { sort.Ints(g) }` is in bipartiteGroups, but the merge of adjudicated groups happens later:**

```go
rootIdx := map[int][]int{}
var rootOrder []int
for gi, g := range groups {
    r := uf.find(gi)
    if _, seen := rootIdx[r]; !seen {
        rootOrder = append(rootOrder, r)
    }
    rootIdx[r] = append(rootIdx[r], g...)
}
```

`groups[r]` is already sorted. When we union groups via uf, the concatenated indices are not sorted. Then in the output building:

```go
out := make([][]Finding, 0, len(rootOrder))
for _, r := range rootOrder {
    idxs := rootIdx[r]
    if len(idxs) == 1 && noiseIdx[idxs[0]] {
        continue
    }
    sort.Ints(idxs) // stable member order after any adjudicated union
    members := make([]Finding, len(idxs))
    for k, idx := range idxs {
        members[k] = cluster[idx]
    }
    out = append(out, members)
}
```

OK, idxs is sorted here. ✓

j. **What about `rootOrder`?**

```go
var rootOrder []int
for gi, g := range groups {
    r := uf.find(gi)
    if _, seen := rootIdx[r]; !seen {
        rootOrder = append(rootOrder, r)
    }
    rootIdx[r] = append(rootIdx[r], g...)
}
```

`rootOrder` collects the first seen root for each group, in group order. Then:

```go
for _, r := range rootOrder {
    idxs := rootIdx[r]
    ...
}
```

`r` is the root ID. `rootIdx[r]` is the merged indices. So the output order is determined by the order in which the first group of each root was seen. This is a stable order. ✓

k. **Test `TestDedupeCluster_BipartiteRefusesTransitiveOverMerge`:**

This replaces `TestDedupeCluster_TransitivelyMergedGrayPairDropped`. The new test asserts the bipartite behavior, not the greedy one.

The old behavior was: groups = [{A, B, C}], amb = [].
The new behavior: groups = [{A or B, C}, {the other}], amb = [{gray pair}].

The test asserts:
```go
length(t, groups, 2, "bipartite keeps the non-duplicate A-B apart instead of chaining all three")
sizes := map[int]int{}
for _, g := range groups {
    sizes[len(g)]++
}
eq(t, sizes[2], 1, "one corroborated pair (C joins one of A/B)")
eq(t, sizes[1], 1, "one finding left as its own group")
length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
```

sizes[2] = 1 (one group of 2) ✓
sizes[1] = 1 (one group of 1) ✓
amb = 1 ✓

l. **What about the noise isolation for this case?**

For the bipartite test case: greta, kai, mira, all different sources. C joins one of A/B. The other is alone. amb = [{gray pair}].

DBSCAN: 
- denseNeighbor requires mergeable AND cross-source.
- A-C: mergeable (0.818). 
- B-C: mergeable (0.818).
- A-B: not mergeable (gray).
- Suppose C joined A. Then groupOf[A] = groupOf[C] = 0, groupOf[B] = 1.
- denseNeighbor(A, C) = true. A's neighbors: [C]. core.
- denseNeighbor(B, A) = false. denseNeighbor(B, C) = true. B's neighbors: [C]. core.

So both A and B are core points (each has a cross-source neighbor in C). A and B are in the same cluster (density-reachable via C). 

Wait, let me trace:
- p=0 (A): neighborsOf = [C]. len+1=2 >= 2. label[0] = 0. seeds = [C].
  - k=0: q=C. label[2] = unvisited. Set to 0. qn = neighborsOf(2) = [A or B, depending]. 
  - If C joined A: qn = [A]. seeds = [C, A]. k=1: q=A. already labeled. Continue.
  - If C joined B: qn = [B]. seeds = [C, B]. k=1: q=B. label[1] = unvisited. Set to 0. qn = neighborsOf(1) = [C]. len+1=2 >= 2. seeds = [C, B, C]. k=2: q=C. already labeled. Continue.
- p=1 (B): if labeled, continue. If not labeled:
  - In the case C joined A: p=1 (B). neighborsOf(1) = [C]. len+1=2 >= 2. label[1] = 0. seeds = [C]. But C is already labeled 0. k=0: q=C. not unvisited. Continue. cluster++. Hmm, this would create a separate cluster.
  
Wait, this depends on the case. Let me re-check.

In the bipartite matching with strict-less-than tie-break, when C is the third source and matches to either A or B with equal cost, the lowest-index group wins. Greedy, kai, mira. A is group 0 (greta), B is group 1 (kai). With strict-less-than, group 0 (lowest index) wins. So C joins A.

But wait, in the test, the source order is greta, kai, mira. The first source is greta, which seeds group 0. The second is kai, which seeds group 1. The third is mira with 1 candidate C. C matches against both groups. cost(group 0, C) = cost(group 1, C) = 0.182. Hungarian with strict-less-than: assigns to lowest-index group. So C joins group 0 (A's group).

So:
- groups from bipartite = [{A, C}, {B}]
- groupOf: A=0, C=0, B=1

Gray pair scan:
- A-B: different groups, gray (0.667). amb = [{A, B}]. ambiguousPairs = [[0, 1]]. ambiguousIdx = [[0, 1]].
- A-C: same group, skip.
- B-C: different groups, gray (0.667). amb = [{A, B}, {B, C}]. ambiguousPairs = [[0, 1], [1, 0]]. ambiguousIdx = [[0, 1], [1, 2]].

So 2 gray pairs. Neither adjudicated. kept = 2.

DBSCAN:
- denseNeighbor(A, C) = true.
- denseNeighbor(A, B) = false (gray, not mergeable).
- denseNeighbor(B, C) = true.
- p=0 (A): neighbors = [C]. core. label[0] = 0. seeds = [C].
  - k=0: q=C. label[2] = unvisited. Set to 0. qn = neighborsOf(2) = [A, B] (C is mergeable with both A and B). len+1=3 >= 2. seeds = [C, A, B].
  - k=1: q=A. labeled. Continue.
  - k=2: q=B. label[1] = unvisited. Set to 0. qn = neighborsOf(1) = [C]. len+1=2 >= 2. seeds = [C, A, B, C]. k=3: q=C. labeled. Continue.
- p=1 (B): already labeled. Continue.
- p=2 (C): already labeled. Continue.
- cluster = 1, dense = true.

grayInvolved: A, B, C are all in keptIdx. So grayInvolved = {0, 1, 2}.

Output:
- group 0 (A, C): 2 members, no noise. Keep.
- group 1 (B): 1 member. Check noiseIdx. Is B in noiseIdx? B is not noise (B is in the cluster, label 0). So noiseIdx[B] = false. Keep.

groups = [{A, C}, {B}]. ✓
amb = [{A, B}, {B, C}, noise?]. noise check: grayInvolved[A] = true, grayInvolved[B] = true, grayInvolved[C] = true. So no noise. amb = [{A, B}, {B, C}].

Test expects `length(t, amb, 1)`. But we have 2 ambiguous entries! That's a bug.

Wait wait. Let me re-check the test:

```go
func TestDedupeCluster_BipartiteRefusesTransitiveOverMerge(t *testing.T) {
    groups, amb := DedupeCluster([]Finding{
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A and B 0.818 merge
    })
    length(t, groups, 2, "bipartite keeps the non-duplicate A-B apart instead of chaining all three")
    sizes := map[int]int{}
    for _, g := range groups {
        sizes[len(g)]++
    }
    eq(t, sizes[2], 1, "one corroborated pair (C joins one of A/B)")
    eq(t, sizes[1], 1, "one finding left as its own group")
    length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
}
```

Test expects `amb == 1`. My trace says `amb == 2` (A-B and B-C). This is a potential issue!

Wait, but B-C is gray (0.667), not merge. So after bipartite, B and C are in different groups. A and C are in the same group. The gray pair scan would find both A-B and B-C.

Hmm, but the comment says "the unresolved gray A-B pair is recorded for adjudication". The test only expects 1 entry, not 2.

Let me re-read the dedupeCluster code:

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
        ambiguous = append(ambiguous, AmbiguousCluster{...})
        ambiguousPairs = append(ambiguousPairs, [2]int{groupOf[i], groupOf[j]})
        ambiguousIdx = append(ambiguousIdx, [2]int{i, j})
    }
}
```

So all gray pairs across different groups are added. A-B and B-C are both gray and in different groups. So 2 entries.

But the test expects 1! This is a real bug.

Wait, let me re-read the test again. Hmm, the test does have `length(t, amb, 1)`. Let me think about this.

Oh, I see. After the bipartite match, C joins A. So groupOf[A] = groupOf[C] = 0, groupOf[B] = 1. The gray pairs are:
- A-B: groupOf[0]=0, groupOf[1]=1, relGray. amb += 1. ambiguousPairs += [0, 1].
- A-C: same group, skip.
- B-C: groupOf[1]=1, groupOf[2]=0, relGray. amb += 1. ambiguousPairs += [1, 0].

So 2 entries in amb.

But the test expects 1. Either:
1. The test is wrong.
2. My understanding of the algorithm is wrong.

Let me re-read the dedupe code more carefully...

Actually, wait. Let me re-read the comment for TestDedupeCluster_TransitivelyMergedGrayPairDropped:
```go
// A~B is gray (0.667) but A~C and B~C both merge (0.818), so A and B end up
// under the same union-find root via C. The A-B pair must NOT be emitted.
```

This is the old test, which was for greedy matching. With greedy, A~B~C would all merge into one group. The A-B gray pair would be dropped because they're already in the same group.

The new test, TestDedupeCluster_BipartiteRefusesTransitiveOverMerge, replaces this. With bipartite, C can only match one of A/B. So C joins A. B is left alone. The A-B gray pair is now adjudicable (they're in different groups).

But what about B-C? They are also gray (0.667). They're in different groups (B=1, C=0). So they're also a gray pair that should be adjudicable.

So the test expectation of `amb == 1` seems incorrect! It should be `amb == 2` (A-B and B-C).

Hmm, but wait. Let me think about this. The bipartite test does say "the unresolved gray A-B pair is recorded for adjudication". Singular. So the test author thought only A-B would be a gray pair. But B-C is also gray...

Actually, let me re-verify the Jaccard:
- A: {w1, w2, w3, w4, w5, w6, w7, w8, w9, w10} = 10
- B: {w1, w2, w3, w4, w5, w6, w7, w8, c1, b1} = 10
- C: {w1, w2, w3, w4, w5, w6, w7, w8, w9, c1} = 10

A-B: intersection = {w1..w8} = 8, union = {w1..w10, c1, b1} = 12. Jaccard = 8/12 = 0.667. Gray.
A-C: intersection = {w1..w9} = 9, union = {w1..w10, c1} = 11. Jaccard = 9/11 = 0.818. Merge.
B-C: intersection = {w1..w8, c1} = 9, union = {w1..w10, b1, c1} = 12. Wait...

Let me recompute B-C:
- B = {w1, w2, w3, w4, w5, w6, w7, w8, c1, b1}
- C = {w1, w2, w3, w4, w5, w6, w7, w8, w9, c1}
- B ∩ C = {w1, w2, w3, w4, w5, w6, w7, w8, c1} = 9
- B ∪ C = {w1, w2, w3, w4, w5, w6, w7, w8, w9, w10, c1, b1} = 12
- Jaccard = 9/12 = 0.75. Merge (above 0.7)!

Oh! B-C is a MERGE pair (0.75), not gray! I miscalculated.

Let me re-verify the comment in the test: "B: vs A 0.667 gray". The comment says A-B is gray, which is correct. But the comment doesn't say what B-C is.

Let me re-check the comment in the test:
```go
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A and B 0.818 merge
```

The comment says "C: vs A and B 0.818 merge". Let me recheck:
- A-C: 9/11 = 0.818 ✓ merge
- B-C: I just calculated 9/12 = 0.75. The comment is wrong?

Actually wait, the comment in the test says "0.818" but I calculated 9/12 = 0.75. Let me recount.

B = {w1, w2, w3, w4, w5, w6, w7, w8, c1, b1}
C = {w1, w2, w3, w4, w5, w6, w7, w8, w9, c1}

B ∩ C:
- w1: in B, in C → yes
- w2: in B, in C → yes
- w3: in B, in C → yes
- w4: in B, in C → yes
- w5: in B, in C → yes
- w6: in B, in C → yes
- w7: in B, in C → yes
- w8: in B, in C → yes
- c1: in B, in C → yes
- b1: in B, NOT in C → no
- w9: NOT in B, in C → no
- w10: in B, NOT in C → no

So B ∩ C = {w1, w2, w3, w4, w5, w6, w7, w8, c1} = 9.

B ∪ C:
All elements from B and C = {w1, w2, w3, w4, w5, w6, w7, w8, w9, w10, c1, b1} = 12.

Jaccard = 9/12 = 0.75.

But the comment says 0.818. That's incorrect. Let me check the AC2 test:

```go
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A,B 0.818 merge
```

Same test data, different comment. The comment says "vs A,B 0.818" - implying BOTH are 0.818. But B-C is actually 0.75.

So the comment is misleading but the actual Jaccard for B-C is 0.75 which is a merge. So:

A-B: 0.667 gray
A-C: 0.818 merge
B-C: 0.75 merge

Now back to bipartite:
- greta (A): groups = [{A}]
- kai (B): cost(0, B) = 1 - 0.667 = 0.333. mergeable = false. B seeds new group. groups = [{A}, {B}]
- mira (C): cost(0, C) = 1 - 0.818 = 0.182. cost(1, C) = 1 - 0.75 = 0.25. Hungarian: row 0 → col 0 (cheapest). C joins A. groups = [{A, C}, {B}]

Gray pair scan:
- A-B: relGray, amb += 1.
- A-C: same group, skip.
- B-C: relMerge, not gray. Skip.

So amb = 1. ✓ 

I was wrong earlier. B-C is 0.75 which is a merge, not gray. So no amb entry for B-C. The test expectation of `amb == 1` is correct.

OK let me also verify TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates:
```go
groups, amb := DedupeCluster([]Finding{
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
    fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A,B 0.818 merge
})
deepEq(t, groupSizes(groups), []int{1, 2}, "non-duplicate A-B not chained via C")
length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
```

Same data, same expected behavior. ✓

m. **Let me also re-verify TestAC2_OptimalAssignmentPrefersClosestPairing:**

I already verified this above. It should work. ✓

n. **Looking at potential issues with the code:**

1. **In `bipartiteGroups`, the candidate selection:**

When a source has multiple candidates, they are processed in cluster order. The order is deterministic. But the assignment is based on the cheapest column. If two columns have equal cost, the lowest-index wins (strict-less-than in Hungarian).

This is correct for determinism.

2. **In `dedupeCluster`, the merging logic:**

```go
for _, g := range groups {
    sort.Ints(g)
}
sort.SliceStable(groups, func(i, j int) bool {
    return groups[i][0] < groups[j][0]
})
```

This is in `bipartiteGroups`. It sorts each group and orders groups by their first member. This is deterministic. ✓

But wait, after adjudicated merges, the groups may need re-sorting:
```go
for _, r := range rootOrder {
    idxs := rootIdx[r]
    if len(idxs) == 1 && noiseIdx[idxs[0]] {
        continue
    }
    sort.Ints(idxs) // stable member order after any adjudicated union
    ...
}
```

`sort.Ints(idxs)` is called for the merged indices. ✓

3. **What about ordering of adjudicated-merged groups?**

`rootOrder` is built in the order groups are processed. So if group 0 and group 1 are merged (via adjudication), `rootOrder[0] = 0` (the first seen root for that union). Then `rootIdx[0] = [group0_indices, group1_indices]`. The output is `{group0, group1}`. ✓

4. **Wait, there's a potential ordering issue:**

In `bipartiteGroups`, groups are sorted by first member. So group 0 has the smallest first member, group 1 has the next smallest, etc.

When we union groups via adjudicatedMerges, the union-find operations preserve the original ID. uf.union(0, 1) makes 0 the parent (or 1, depending on implementation). Let me check the union-find:

```go
type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
    uf := &unionFind{parent: make([]int, n)}
    for i := range uf.parent {
        uf.parent[i] = i
    }
    return uf
}

func (uf *unionFind) find(x int) int {
    for uf.parent[x] != x {
        uf.parent[x] = uf.parent[uf.parent[x]]
        x = uf.parent[x]
    }
    return x
}

func (uf *unionFind) union(x, y int) {
    rx, ry := uf.find(x), uf.find(y)
    if rx != ry {
        uf.parent[rx] = ry
    }
}
```

Wait, I need to look at the union-find code. Let me check:

Actually, I don't have the union-find code in the diff. But based on the test `TestDedupeCluster_TransitiveMerge`, it should work like a standard union-find.

OK, I'll assume the union-find is correct.

5. **Potential issue: in `dedupeCluster`, `keep` filter for adjudicated pairs:**

```go
for k, gp := range ambiguousPairs {
    if uf.find(gp[0]) != uf.find(gp[1]) {
        keptAmb = append(keptAmb, ambiguous[k])
        keptIdx = append(keptIdx, ambiguousIdx[k])
    }
}
```

So gray pairs whose groups are now in the same root are dropped. This is correct.

But wait - what if `gp[0] == gp[1]`? In bipartite, two findings in the same group have `groupOf[i] == groupOf[j]`, so they're already skipped before reaching the gray pair scan. So `gp[0] != gp[1]` is guaranteed.

But in the kept filter, we check `uf.find(gp[0]) != uf.find(gp[1])`. If they're in the same group originally, they're not in amb. If they're in different groups but were adjudicated to merge, then `uf.find(gp[0]) == uf.find(gp[1])` after the union. So the entry is dropped. ✓

6. **What about the `soloRoot` check?**

```go
soloRoot := make(map[int]bool, n)
for _, idxs := range rootIdx {
    if len(idxs) == 1 {
        soloRoot[idxs[0]] = true
    }
}
```

Wait, this is computed after adjudicated unions. If a single group is unioned with another (via adjudication), `len(idxs) > 1`. So soloRoot only contains single groups that were NOT adjudicated. ✓

But there's a subtle issue: what if a single group is unioned with another (resulting in len > 1), and the OTHER group had len > 1 originally? The new root has indices from both. So soloRoot only marks singletons. ✓

7. **Looking at the dedupeCluster code more carefully:**

```go
denseNeighbor := func(a, b int) bool { return mergeable(a, b) && srcKeys[a] != srcKeys[b] }
```

`denseNeighbor` is the DBSCAN neighborhood. It requires `mergeable` (which uses Jaccard) AND cross-source. So two findings from the same source are never neighbors in DBSCAN. ✓

8. **Looking at how `dedupeCluster` is called from `reconcile.go`:**

```go
groups, amb := dedupeCluster(cl, clusterKeys(cl, opts.Grouper), opts.Merges)
```

`clusterKeys` returns AST group keys. The signature is correct. ✓

9. **What about `opts.Grouper`?**

If `opts.Grouper` is nil, `clusterKeys` returns empty strings. The bipartite match then relies entirely on Jaccard. ✓

10. **Looking at the `emit` test:**

The test reads `findings.json` and `ambiguous.json`. Let me check if these are the correct file names. The constants are `FindingsJSON` and `AmbiguousJSON`. I assume these are defined in the reconcile package and match the actual emit output. Let me search the diff... The constants are referenced but not defined in the diff. They're assumed to exist.

11. **Let me also look at potential issues in the new code:**

a. **`distance.go` uses `classify`:**

```go
_, sim := classify(tokA, tokB)
return distanceCeiling - sim
```

`classify` returns (relation, similarity). We ignore the relation and just use the similarity. The similarity is the Jaccard similarity. So `distance = 1 - Jaccard`. ✓

b. **`bipartite.go` uses `distanceCeiling`:**

```go
const noMatchSentinel = 2.0
```

And in bipartiteGroups:
```go
m := distanceCeiling
```

These are 2.0 and 1.0 respectively. The sentinel exceeds the real distance ceiling, so a virtual cell is always more expensive than a real cell. ✓

c. **`hungarian.go` uses `inf`:**

```go
const inf = math.MaxFloat64
```

This is the standard inf for Hungarian. Operations on inf should be careful, but in the standard implementation it works.

12. **Let me look at potential issues with the Hungarian implementation more carefully:**

The standard Jonker-Volgenant / Hungarian with potentials:

```go
for i := 1; i <= n; i++ {
    p[0] = i
    j0 := 0
    minv := make([]float64, n+1)
    used := make([]bool, n+1)
    for j := 0; j <= n; j++ {
        minv[j] = inf
    }
    for {
        used[j0] = true
        i0 := p[j0]
        delta := inf
        j1 := -1
        for j := 1; j <= n; j++ {
            if used[j] {
                continue
            }
            cur := cost[i0-1][j-1] - u[i0] - v[j]
            if cur < minv[j] {
                minv[j] = cur
                way[j] = j0
            }
            if minv[j] < delta {
                delta = minv[j]
                j1 = j
            }
        }
        for j := 0; j <= n; j++ {
            if used[j] {
                u[p[j]] += delta
                v[j] -= delta
            } else {
                minv[j] -= delta
            }
        }
        j0 = j1
        if p[j0] == 0 {
            break
        }
    }
    ...
}
```

Wait, there's a potential bug. In the inner loop:
```go
for j := 0; j <= n; j++ {
    if used[j] {
        u[p[j]] += delta
        v[j] -= delta
    } else {
        minv[j] -= delta
    }
}
```

When j=0, used[0] is initially false. minv[0] = inf. After the first iteration, used[0] is true (set at the start of the for loop iteration). Then for j=0, we do `u[p[0]] += delta` and `v[0] -= delta`. 

But p[0] is set to i at the start. u[i] += delta, v[0] -= delta.

Hmm, but we also do `minv[0] -= delta` if used[0] is false. After used[0] = true, this doesn't apply. So the code is correct.

13. **Let me look at the very first iteration:**

i=1 (row 1). p[0] = 1. j0 = 0. minv = [inf, inf, ..., inf]. used = [false, false, ..., false].
- Loop iteration 1:
  - used[0] = true. i0 = p[0] = 1.
  - For j = 1 to n: cur = cost[0][j-1] - u[1] - v[j] = cost[0][j-1] - 0 - 0 = cost[0][j-1].
  - For each unused j, set minv[j] = cost[0][j-1], way[j] = 0. Then find min minv, j1.
  - delta = min minv. j1 = argmin.
  - For j=0, used[0] is true. u[p[0]] += delta → u[1] += delta. v[0] -= delta.
  - For j=1 to n, used[0] is true for j=0 only. minv[j] -= delta. (All minv's decreased by delta. But minv[j] was set to cost - delta, so after this it's cost - 2*delta... wait that's wrong.)

Hmm, let me think again. In the standard implementation, we have:

```
for j = 0 to n:
  if used[j]:
    u[p[j]] += delta
    v[j] -= delta
  else:
    minv[j] -= delta
```

For j=0: used[0] is true. u[p[0]] += delta. v[0] -= delta.
For j=1..n: if used[j] is false, minv[j] -= delta.

But wait, we also need to update u[p[j]] and v[j] for j where used[j] is true. The u and v potentials are adjusted.

Hmm, but I notice that the code in the diff has:
```go
for j := 0; j <= n; j++ {
    if used[j] {
        u[p[j]] += delta
        v[j] -= delta
    } else {
        minv[j] -= delta
    }
}
```

This iterates j from 0 to n. For each j, if used, update u and v. Else, update minv. This is the standard formulation.

The first iteration (j0=0, used[0]=true): u[p[0]] += delta, v[0] -= delta.
For other used j: same.
For unused j: minv[j] -= delta.

OK this is correct.

14. **Let me check the termination condition:**

```go
j0 = j1
if p[j0] == 0 {
    break
}
```

If j1 == -1, then j0 = -1, and p[j0] is out of bounds. This would be a bug. But j1 should always be set to a valid unused column.

Wait, but what if j1 is never updated? Let me think... if all columns are used, then in the inner loop, we skip all j (because used[j] is true for all j from 1 to n). Then j1 stays at its initial value of -1. Then j0 = -1, and p[j0] is out of bounds.

But this shouldn't happen in a correct Hungarian implementation. Why? Because we always start with j0 = 0, and we mark j0 as used. Then we expand to one of the unused columns (j1). If there are n rows and n columns, and we've assigned some, the algorithm should find an augmenting path.

Actually, let me think about this more carefully. In the first iteration of the outer i loop, no rows are assigned, so p[j] = 0 for all j. As we expand from j0=0, we go to some j1. If p[j1] = 0 (no row assigned to j1), we break and assign p[j1] = p[0] = i.

In subsequent iterations, we have p[j] != 0 for some j. We expand until we find a j with p[j] = 0.

If the algorithm gets stuck (all columns used but no j with p[j] = 0), that means we've explored all columns without finding an augmenting path. But the algorithm should always find one.

Hmm, but in the code, j1 is only set in the inner loop. If no j satisfies `minv[j] < delta`, j1 stays at -1. But `minv[j] < delta` should be true for at least one j, because minv[j] = cost[i0][j] - u[i0] - v[j] (or 0 if just set), and we initialize minv to inf. So at least one j will have a finite minv, and the comparison `minv[j] < delta` (with delta = inf initially) will be true.

Wait, but `delta = inf` initially. After the first inner iteration, we set `delta = minv[j1]` where j1 is the column with the smallest minv. So delta becomes finite.

In the next outer iteration, delta = inf again. We iterate over unused columns, update minv, find the smallest, set delta and j1.

So as long as there's at least one unused column with a finite minv (which should always be the case), j1 will be set.

What if all columns are used? Then in the next outer iteration, the inner loop skips all columns (used[j] = true for all j from 1 to n). delta stays inf, j1 stays -1. Then j0 = -1, which is out of bounds for p.

But when does "all columns are used" happen? After we've gone through n+1 iterations (j0, j1, j2, ..., j_n) of the inner for loop. At that point, we've used up all n columns. But the algorithm should terminate before this.

Actually, the algorithm guarantees termination because we always find a j with p[j] = 0 (an unmatched column) within at most n+1 iterations. The reasoning is that the alternating path is bounded by the number of columns.

So the "all columns used" case shouldn't happen in practice. But it's a defensive concern.

Actually, I realize: the algorithm processes up to n+1 columns in the inner loop (j0, j1, j2, ..., j_n) before finding an unmatched one. If all n columns are matched (p[j] != 0 for all j from 1 to n), the algorithm would loop through all of them, but on the n+1th iteration (going through all n columns), we'd be in a situation where no new j can be added (because all are used and matched). 

Wait, this is the issue. Let me think again.

Initial: p[j] = 0 for all j. j0 = 0. After iteration 1, j0 = j1 (some j with p[j] = 0). We assign p[j1] = i. Break.

For the next i, we start fresh. p[0] = i. j0 = 0. We expand to j1. If p[j1] = 0, we assign. Otherwise, p[j1] = some previous row k. Then we continue expanding to j2 (next unused). This continues until we find j_n with p[j_n] = 0.

In the worst case, we go through all n columns. After n iterations of the inner for loop, all columns are used. But the algorithm guarantees that we find an unmatched column before exhausting all n columns. Why?

Because the alternating path in the matched graph is bounded. We start at j0 (the free node, always available), and we walk along alternating edges. In a bipartite graph, this walk is guaranteed to terminate at a free column.

OK so the algorithm is correct. The "all columns used" case won't happen in practice.

But wait, what if the algorithm has a bug and we do end up with j1 = -1? Then p[j0] = p[-1] is out of bounds, causing a panic. This is a robustness concern.

Let me see if there's any edge case. What if n = 1?

n = 1. cost = [[X]]. The single row matches the single column. 
- i=1, p[0]=1, j0=0. minv = [inf, inf]. used = [false, false].
- Inner loop iter 1: used[0]=true, i0=p[0]=1. For j=1: cur = cost[0][0] - 0 - 0 = X. minv[1] = X, way[1] = 0. delta = X, j1 = 1.
- For j=0: used[0]=true, u[p[0]]+=delta → u[1]+=X, v[0]-=X. For j=1: used[1] is false, minv[1] -= X. So minv[1] = 0.
- j0 = 1. p[j0] = p[1] = 0. Break.
- After the inner loop, the augmenting path is built: j0 = 1, j1 = way[1] = 0, ... p[1] = p[0] = 1.
- assign[0] = 1.

Looks correct.

What if n = 0?

n = 0. return nil. ✓

15. **Looking at the Hungarian implementation for off-by-one errors:**

The algorithm uses 1-indexed arrays (u, v, p, way of size n+1, indices 0 to n). cost is 0-indexed. The conversion is:
- u[i] for i in 0..n (where u[0] is unused, u[i] corresponds to row i-1)
- v[j] for j in 0..n (where v[0] is unused, v[j] corresponds to column j-1)
- p[j] for j in 0..n (where p[0] is scratch, p[j] is the row assigned to column j-1)
- cost[r][c] is 0-indexed (r and c are 0 to n-1)

In the code:
- i0 = p[j0]. i0 is 1-indexed. cost[i0-1][j-1] is 0-indexed. ✓
- j from 1 to n. j is 1-indexed. cost[i0-1][j-1] is 0-indexed. ✓
- p[0] = i. i is 1-indexed. p[0] stores the current row. ✓
- way[j] = j0. j is 1-indexed, j0 is 0-indexed (or 1-indexed?). j0 starts at 0, then is set to j1 which is in 1..n. So j0 is in {0, 1, ..., n}. ✓

OK, the indices look correct.

16. **Looking for potential issues in the test file `ac2_greedy_failures_test.go`:**

The tests are well-documented. They test the bipartite matcher's behavior on the greedy failure cases. The expected results match the analysis.

But wait, there's a potential issue with the test `TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother`:

```go
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta eta", "greta"), // A
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta p1", "kai"),    // P: vs A 0.75 merge
fnd("a.go", 1, "alpha beta gamma delta epsilon zeta q1", "kai"),    // Q: vs A 0.75 merge
```

Sources: greta, kai, kai.

When processing in bipartiteGroups, the order is sorted: greta, kai.
- greta: 1 candidate A. groups = [{A}]
- kai: 2 candidates P, Q. cost(0, P) = cost(0, Q) = 0.25. 

In hungarianAssign(1, 2, cost), the algorithm pads to a 2x2 matrix:
```
[0.25, 0.25]
[2.0,  2.0 ]  (padding row)
```
Hungarian finds min assignment. Row 0 (only real row) takes col 0 (cheapest, both equal). 

But wait, the algorithm should give row 0 to col 0 with strict-less-than tie-break. Then row 1 (padding) takes col 1 (only remaining). But the padding row has cost 2.0 to both, so it doesn't matter.

assign[0] = 0 (col 0 = P). mergeable(A, P) = true. P joins A. used[0] = true.
Q: used[1] = false. Q seeds new group. groups = [{A, P}, {Q}]. ✓

Result: groups = 2, groupSizes = [1, 2]. Test asserts deepEq(t, groupSizes(groups), []int{1, 2]). ✓

17. **Looking at potential issues in the test file `ac3_dbscan_test.go`:**

`TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`:
```go
a := fnd("x.go", 1, "alpha beta gamma", "mira")
b := fnd("x.go", 1, "alpha beta delta", "pool") // gray to A (0.5)
id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
groups, amb := dedupeCluster([]Finding{
    a, b,
    fnd("x.go", 1, "zeta eta theta iota kappa", "greta"),
    fnd("x.go", 1, "zeta eta theta iota kappa", "kai"), // identical → dense pair
}, make([]string, 4), map[string]bool{id: true})
```

This calls `dedupeCluster` directly with 4 elements. The 4th element is a duplicate. Sources: mira, pool, greta, kai. Sorted: greta, kai, mira, pool.

Let me trace:
- greta: 1 candidate C. groups = [{C}]
- kai: 1 candidate D. cost(0, D) = 0. mergeable. D joins C. groups = [{C, D}]
- mira: 1 candidate A. cost(0, A) = 1.0 (Jaccard A-C = 0, A-D = 0). mergeable(C, A) = false, mergeable(D, A) = false. Not accepted. A seeds new group. groups = [{C, D}, {A}]
- pool: 1 candidate B. cost(0, B) = 1.0, cost(1, B) = 0.5. Hungarian: row 0 → col 0 (cheapest). mergeable(C, B) = false, mergeable(D, B) = false. Not accepted. But cost is 1.0 for row 0... hmm.

Wait, let me re-think. In hungarianAssign(2, 1, cost), we have 2 rows (groups) and 1 column (B). The matrix is padded to 2x2:
```
[1.0, 2.0]
[0.5, 2.0]
```

Hungarian: row 0 → col 0 (cost 1.0), row 1 → col 1 (cost 2.0). Total = 3.0.
Or row 0 → col 1 (cost 2.0), row 1 → col 0 (cost 0.5). Total = 2.5.
Or row 0 → col 1 (padding), row 1 → col 0. Total = 0.5 + 2.0 = 2.5.

Wait, no. The padding is for cells that don't have real values. Real cells are (r, c) where r < 2 and c < 1, so only (0, 0) and (1, 0). The other cells are padding.

Matrix:
```
[1.0, 2.0]  (row 0: col 0 = real = 1.0, col 1 = padding = 2.0)
[0.5, 2.0]  (row 1: col 0 = real = 0.5, col 1 = padding = 2.0)
```

Hungarian:
- Row 0 → col 0 (1.0), row 1 → col 1 (padding, 2.0). Total = 3.0.
- Row 0 → col 1 (2.0), row 1 → col 0 (0.5). Total = 2.5.

Min is 2.5: row 0 → col 1 (padding), row 1 → col 0. 

Hmm, but with strict-less-than tie-break, which assignment wins?

Let me trace the algorithm:
- i=1 (row 0 of cost). p[0] = 1. j0 = 0. minv = [inf, inf, inf]. used = [F, F, F].
- Inner iter 1: used[0] = T, i0 = 1. j=1: cur = cost[0][0] - 0 - 0 = 1.0. minv[1] = 1.0, way[1] = 0. delta = 1.0, j1 = 1. j=2: cur = cost[0][1] - 0 - 0 = 2.0. minv[2] = 2.0, way[2] = 0. delta = 1.0, j1 = 1.
- For j=0: u[1] += 1, v[0] -= 1. For j=1: minv[1] -= 1 = 0. For j=2: minv[2] -= 1 = 1.0.
- j0 = 1. p[1] = 0. Break.
- Augment: j0 = 1, j1 = way[1] = 0. p[1] = p[0] = 1. p[0] = 1. 

Wait, p[0] is the scratch, and we keep its value across the outer loop. After the outer loop body for i=1, we have p[1] = 1 (row 0 matched to col 0).

- i=2 (row 1 of cost). p[0] = 2. j0 = 0. minv = [inf, inf, inf]. used = [F, F, F].
- Inner iter 1: used[0] = T, i0 = 2. j=1: cur = cost[1][0] - u[2] - v[1] = 0.5 - 0 - 0 = 0.5. minv[1] = 0.5, way[1] = 0. delta = 0.5, j1 = 1. j=2: cur = cost[1][1] - 0 - 0 = 2.0. minv[2] = 2.0, way[2] = 0. delta = 0.5, j1 = 1.
- For j=0: u[2] += 0.5, v[0] -= 0.5. For j=1: minv[1] -= 0.5 = 0. For j=2: minv[2] -= 0.5 = 1.5.
- j0 = 1. p[1] = 1 (from before). Continue.
- Inner iter 2: used[1] = T, i0 = p[1] = 1. j=2: cur = cost[0][1] - u[1] - v[2] = 2.0 - 1.0 - 0 = 1.0. minv[2] = 1.0, way[2] = 1. delta = 1.0, j1 = 2.
- For j=0: used[0]=T, u[p[0]] += 1.0. p[0] = 2, so u[2] += 1.0. v[0] -= 1.0. For j=1: used[1]=T, u[p[1]] += 1.0. p[1] = 1, so u[1] += 1.0. v[1] -= 1.0. For j=2: used[2]=F, minv[2] -= 1.0 = 0.
- j0 = 2. p[2] = 0. Break.
- Augment: j0 = 2, j1 = way[2] = 1. p[2] = p[1] = 1. j0 = 1, j1 = way[1] = 0. p[1] = p[0] = 2. j0 = 0. Done.

Final: p[1] = 2, p[2] = 1.
assign[1] = 1, assign[2-1] = 1. Wait that's confusing.

Let me re-read the output construction:
```go
assign := make([]int, n)
for j := 1; j <= n; j++ {
    if p[j] >= 1 {
        assign[p[j]-1] = j - 1
    }
}
```

For p[1] = 2: assign[2-1] = assign[1] = 1-1 = 0. So assign[1] = 0.
For p[2] = 1: assign[1-1] = assign[0] = 2-1 = 1. So assign[0] = 1.

So row 0 → col 1, row 1 → col 0.

Going back to hungarianAssign:
```go
for r := 0; r < rows; r++ {
    if c := full[r]; c < cols {
        assign[r] = c
    }
}
```

rows = 2, cols = 1. full = [1, 0]. For r=0, c=1. c < cols = 1 is false. So assign[0] = -1. For r=1, c=0. c < cols = 1 is true. So assign[1] = 0.

So assign = [-1, 0]. Row 0 to padding, row 1 to col 0 (B).

In bipartiteGroups, for r=0, c=-1. Continue (not assigned). For r=1, c=0. cand = cands[0] = B. mergeable(A, B) = false (gray). Not accepted. used[0] stays false.

B is unmatched. Seeds new group. groups = [{C, D}, {A}, {B}].

But the test expects 2 groups after adjudication merges A and B's groups. Let me re-check the gray pair scan and adjudication:

Gray pair scan:
- A-B: gray. ID in adjudicated. uf.union(groupOf[A], groupOf[B]) = uf.union(1, 2).
- A-C: distinct, skip.
- A-D: distinct, skip.
- B-C: distinct, skip.
- B-D: distinct, skip.
- C-D: merge, not gray, skip.

So uf.union(1, 2). After: uf.find(1) = uf.find(2) (depends on impl, but they're in the same set).

rootIdx: 
- gi=0: r = uf.find(0) = 0. rootIdx[0] = [indices of group 0] = [C, D]
- gi=1: r = uf.find(1). After uf.union(1, 2), uf.find(1) could be 1 or 2 (parent). Let's say uf.find(1) = 1 (parent is ry = 2, so parent[1] = 2, then find(1) = find(2) = 2). Hmm.

Wait, let me re-read unionFind:
```go
func (uf *unionFind) union(x, y int) {
    rx, ry := uf.find(x), uf.find(y)
    if rx != ry {
        uf.parent[rx] = ry
    }
}
```

So parent[rx] = ry. find compresses. If rx = 1, ry = 2, then parent[1] = 2. find(1) = find(parent[1]) = find(2) = 2. So both 1 and 2 map to root 2.

rootIdx:
- gi=0: r = 0. rootIdx[0] = [C, D] (or whatever indices)
- gi=1: r = 2. rootIdx[2] = [A]
- gi=2: r = 2. rootIdx[2] = [A, B]

So rootIdx = {0: [C, D], 2: [A, B]}. rootOrder = [0, 2].

soloRoot: rootIdx[0] has 2 members, not solo. rootIdx[2] has 2 members, not solo. So soloRoot is empty.

Output:
- r=0: idxs = [C, D]. 2 members, not in noise. Keep. out = [[C, D]]
- r=2: idxs = [A, B]. 2 members, not in noise. Keep. out = [[C, D], [A, B]]

amb: 
- A-B was adjudicated, so it's dropped from amb. (The `kept` filter checks if uf.find(gp[0]) != uf.find(gp[1]). After union, they're equal, so dropped.)

So amb = [].

groups = 2, amb = 0. ✓

DBSCAN: denseNeighbor requires mergeable AND cross-source.
- C-D: mergeable, cross-source. dense cluster.
- A-B: not mergeable (gray). 
- A-C, A-D, B-C, B-D: not mergeable.

For p=0 (C): neighbors = [D]. core. label[0] = 0. seeds = [D].
  - k=0: q=D. label[1] = unvisited. Set to 0. qn = neighborsOf(1) = [C]. seeds = [D, C].
  - k=1: q=C. already labeled. Continue.
For p=1 (D): already labeled.
For p=2 (A): no neighbors (A is not mergeable with anyone). label[2] = -1. noise.
For p=3 (B): no neighbors. label[3] = -1. noise.

grayInvolved: A is in adjudicated pair, but the entry was dropped from amb. Let me re-check the code:

```go
grayInvolved := make(map[int]bool, len(keptIdx)*2)
for _, p := range keptIdx {
    grayInvolved[p[0]] = true
    grayInvolved[p[1]] = true
}
```

keptIdx is the surviving ambiguous pairs. A-B was adjudicated, so it's not in kept. So grayInvolved is empty.

soloRoot: A and B are in the same root with len 2. Not solo. So soloRoot[A] = false, soloRoot[B] = false.

noise check: A and B are noise, but not soloRoot. So they're NOT isolated. 

But wait, the test expects `length(t, amb, 0)`. And my analysis says amb is []. So no noise. groups = [{C, D}, {A, B}]. ✓

But hmm, the test says "no noise singletons: A and B were merged, not isolated". So A and B should NOT be isolated as noise. My analysis confirms this because they're in a 2-member root.

Actually wait, looking at the code again:
```go
if len(idxs) == 1 && noiseIdx[idxs[0]] {
    continue // isolated as DBSCAN noise → sidecar only
}
```

A and B are in idxs = [A, B] (len 2). So `len == 1` is false. They stay in the output. ✓

But hmm, there's a potential issue. The code checks `soloRoot` BEFORE this:
```go
soloRoot := make(map[int]bool, n)
for _, idxs := range rootIdx {
    if len(idxs) == 1 {
        soloRoot[idxs[0]] = true
    }
}
...
if dense {
    for i := 0; i < n; i++ {
        if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] {
            continue
        }
        noiseIdx[i] = true
        ...
    }
}
```

So `noiseIdx[i]` is only set to true if `soloRoot[i]` is true. This means only solo (1-member) roots that are also DBSCAN noise are isolated.

In the test case, A and B are in a 2-member root. So `soloRoot[A] = false`, and the noise isolation is skipped. They stay in the output. ✓

OK so the test should pass.

18. **Let me also look for any potential issues in the noise logic:**

```go
if dense {
    for i := 0; i < n; i++ {
        if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] {
            continue
        }
        noiseIdx[i] = true
        noise = append(noise, AmbiguousCluster{
            ID:       AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[i].Problem),
            File:     cluster[i].File,
            Line:     cluster[i].Line,
            Findings: []Finding{cluster[i]},
        })
    }
}
```

`dense = true` means at least one cluster formed. If no cluster formed, no noise is isolated. ✓

For each point i: if it's not noise, or it's gray-involved, or it's not a solo root, skip. Otherwise, isolate as noise.

But wait, what about the case where a solo root is NOT noise (it's not labeled -1)? In that case, it wouldn't be added to noise. But it would still be in the output as a solo group. ✓

19. **Looking at the `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise` test:**

Wait, I traced through this. The result is `groups = 2, amb = 0`. But the test also says:
```go
for _, g := range groups {
    length(t, g, 2, "each group has two members; nothing was wrongly isolated")
}
```

So each group has 2 members. [{C, D}, {A, B}] - yes, each has 2 members. ✓

20. **Let me look at the README update:**

```diff
-- **Dedupe — token-set Jaccard, 0.7 / 0.4 thresholds.** Within a cluster,
-  findings are compared by token-set Jaccard similarity using integer
-  cross-multiplication (no float comparison). A pair at or **above 0.7 merges**;
-  a pair in the **0.4–0.7 gray zone is ambiguous** — left unmerged and recorded
-  in the ambiguity sidecar; below 0.4 the findings are distinct.
+- **Dedupe — optimal bipartite matching.** Within a cluster, findings from
+  different sources are matched 1:1 by the Kuhn-Munkres (Hungarian) algorithm
+  over a composite edge-weight distance: **0** for findings sharing an
+  AST-isomorphism group key, otherwise **`1 − token-set Jaccard`**. Acceptance of
+  a match is gated by the same integer cross-multiplication boundary (no float
+  comparison): a pair at or **above 0.7 similarity merges**. The 1:1 constraint
+  structurally avoids the transitive over-merge a greedy single-linkage pass
+  suffers (one finding cannot drag two non-duplicates into one group). A pair in
+  the **0.4–0.7 gray zone is ambiguous** — left unmerged and recorded in
+  the ambiguity sidecar; below 0.4 the findings are distinct.
```

Looks good. ✓

```diff
-- **Ambiguity sidecar.** Gray-zone clusters (0.4–0.7) are not silently merged or
-  dropped — they are surfaced in `Result.Ambiguous` as `AmbiguousCluster` values
-  so a host can adjudicate them (and force a merge via `Options.Merges`).
+- **Ambiguity sidecar.** Two kinds of cluster surface in `Result.Ambiguous` as
+  `AmbiguousCluster` values, neither silently merged nor dropped: **gray-zone
+  pairs** (0.4–0.7) a host can adjudicate (and force a merge via
+  `Options.Merges`); and **DBSCAN-isolated noise** — a single uncorroborated
+  finding standing alone amid a corroborated cluster (a likely single-model
+  hallucination), carried as a one-finding cluster and removed from the merged
+  output so the consensus findings stay trustworthy.
```

Looks good. ✓

21. **Let me look at potential issues with the `Hungarian` function's determinism:**

The test `TestHungarian_Deterministic` runs hungarian twice on the same input and verifies the output is the same. The implementation should be deterministic for the same input. The strict-less-than tie-break favors the lowest index. ✓

22. **Let me look for issues with the `mergeable` function and `denseNeighbor`:**

```go
mergeable := func(a, b int) bool {
    if keys[a] != "" && keys[a] == keys[b] {
        return true
    }
    rel, _ := classify(tokens[a], tokens[b])
    return rel == relMerge
}
```

So mergeable returns true if:
- Both have the same non-empty AST key, OR
- The Jaccard similarity is in the merge range (>= 0.7)

```go
denseNeighbor := func(a, b int) bool { return mergeable(a, b) && srcKeys[a] != srcKeys[b] }
```

Dense neighbors are mergeable AND cross-source. This means same-source findings are never dense neighbors. So a single source repeating itself doesn't form a dense cluster. ✓

23. **Looking for a potential bug in dedupeCluster:**

Wait, the test `TestDedupeCluster_BipartiteRefusesTransitiveOverMerge` should pass, and I confirmed this. But let me also check the AC2 test:

```go
func TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates(t *testing.T) {
    groups, amb := DedupeCluster([]Finding{
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
        fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A,B 0.818 merge
    })
    deepEq(t, groupSizes(groups), []int{1, 2}, "non-duplicate A-B not chained via C")
    length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
}
```

Wait, the comment says "C: vs A,B 0.818 merge" but B-C is 0.75 which is a merge. So the comment is misleading but the test data is consistent.

24. **Looking at potential issue: in `bipartiteGroups`, what if a group has 1 member (solo) and a candidate matches but is gray?**

```go
accept := false
for _, gi := range groups[r] {
    if mergeable(gi, cand) {
        accept = true
        break
    }
}
```

`accept` is true if mergeable with at least one member. For a solo group, there's 1 member. So accept = mergeable(gi, cand). If gray, accept = false. ✓

But what if the solo group's member is the candidate itself? That can't happen because the candidate is from a different source (we sort by source, and a source's candidates are distinct from the group's source). So `gi != cand` always.

25. **Let me look for issues in `dbscanLabels`:**

The DBSCAN algorithm processes points in order. For each unvisited point, it computes neighbors. If the point has enough neighbors, it's a core. Otherwise, noise. Then it expands the cluster via seeds.

I traced through several cases and they all work correctly. The algorithm is standard.

26. **Potential issue: floating-point determinism**

The Hungarian algorithm uses float64. The `cost` matrix entries are computed from Jaccard similarity, which is a rational number (intersection/union). The result is exact in float64 if the numbers are small enough.

But wait, `classify` returns a float similarity. The internal computation uses integer cross-multiplication (as the README says). So the similarity is exact up to the float conversion.

For the bipartite matching, we use `1 - sim` which is a float. This could have small floating-point errors. But the acceptance is gated by the integer-exact `mergeable` predicate, not the float cost. So the float cost is only used for the assignment (which matching), not for the final merge decision.

This is consistent with the sprint plan's requirement for determinism. ✓

27. **Looking at the `ac3_isolation_test.go` test:**

The test uses `stream.Finding` (the actual stream type) and `mf` helper. I assume this is a test helper that creates a `stream.Finding` with the given parameters. Let me check if the test is correct...

Actually, the test source uses `mf` to create `stream.Finding` values. The `Source` type has a `Findings` field of `[]stream.Finding`. This is the actual data flow.

The sources:
- pool: 2 findings (both from greta)
- host: 1 finding (from host)

When `Reconcile` is called, the sources are processed. AllFindings flattens them. The findings are clustered by location. The two greta findings are at the same file+line (auth.go:42). The host finding is at auth.go:43. The line window is ±3, so they're in the same cluster.

In the cluster, the findings are processed by `dedupeCluster`. The two greta findings (same source) are both from greta. The host finding is from host. The bipartite matching would:
- greta: 2 candidates (greta1, greta2). groups = [{greta1}, {greta2}].
- host: 1 candidate (host1). cost(0, host1) = 1 - Jaccard(greta1, host1) = 1 - 1.0 = 0. cost(1, host1) = 1 - Jaccard(greta2, host1) = 1 - 0 = 1. Hungarian: row 0 → col 0. mergeable(greta1, host1) = true. host1 joins greta1. groups = [{greta1, host1}, {greta2}].

DBSCAN: denseNeighbor(greta1, host1) = true. core. dense cluster. greta2 has no neighbors (greta2 is from same source as greta1, not cross-source). greta2 is noise. 

grayInvolved: empty (no gray pairs).
soloRoot: greta2 is solo.

greta2 is noise, not gray, is solo. Isolated as noise.

Output: groups = [{greta1, host1}], amb = [noise(greta2)].

Test asserts:
- findings.json doesn't contain "hallucinated nonsense" (greta2's problem) ✓ (greta2 is in ambiguous, not findings)
- ambiguous.json contains "hallucinated nonsense" ✓
- findings.json contains "token never expires" ✓ (greta1's problem)
- res.Ambiguous has 1 cluster ✓
- res.Ambiguous[0].Findings has 1 finding ✓
- res.Ambiguous[0].Findings[0].Problem == "totally unrelated hallucinated nonsense" ✓

28. **Looking at potential issues with the file naming convention in the test:**

The test reads `FindingsJSON` and `AmbiguousJSON`. These are constants. They should match the actual file names emitted by `Emit`. I assume they are defined as `findings.json` and `ambiguous.json` respectively.

But I notice the test does `string(findings)` and `string(amb)`. This converts the entire file content to a string. The assertion checks if "hallucinated nonsense" is in the string. As long as the JSON encoder doesn't escape the spaces or the words, this should work.

JSON encoding escapes `\`, `"`, and control characters. Spaces and alphanumeric characters are not escaped. So "hallucinated nonsense" would appear literally in the JSON. ✓

29. **Looking at potential issues with the source name:**

The `Source.Name` is "pool" and "host". The `Source.Name` is used for source identification. But in dedupeCluster, the source key is based on `Finding.Reviewer`, not `Source.Name`. The reviewers are "greta" and "host". So the bipartite matching uses the reviewer, not the source name.

This is a potential issue if the test assumes source-based matching. But the README and code make it clear that the matching is reviewer-based. ✓

30. **Let me look at potential issues with the comment in the test:**

```go
mf("HIGH", "auth.go", 42, "token never expires unchecked", "guard it", "security", 15, "pool saw it", "greta"),
```

The 9th argument is "greta", which is the Reviewer. The 8th argument "pool saw it" is probably the source name (within the finding's metadata). The `mf` helper extracts the Reviewer from a specific position. I trust the test is correct.

31. **Looking at potential issues with the `mf` helper:**

I don't see the `mf` helper in the diff. It must be defined elsewhere in the test files. The test uses it to create `stream.Finding` values. As long as the helper is correct, the test should work.

32. **Potential bug: the `mergeable` function uses `keys[a]` but keys is the AST group keys, not the source keys.**

```go
mergeable := func(a, b int) bool {
    if keys[a] != "" && keys[a] == keys[b] {
        return true
    }
    rel, _ := classify(tokens[a], tokens[b])
    return rel == relMerge
}
```

`keys[a]` and `keys[b]` are the AST group keys. If they match (non-empty), mergeable. This is independent of source. So two findings from the same source with the same AST key would be mergeable. But the bipartite matching only matches across sources, so same-source findings are never compared. ✓

33. **Potential issue: what if two findings from the same source have the same AST key?**

Same source means they're in `bySource[source]`. The bipartite matching only processes different sources. So they're never compared in bipartite. They stay as separate findings (in the same source's group). ✓

34. **Looking at potential issues with the `srcKeys`:**

```go
srcKeys := make([]string, n)
for i, f := range cluster {
    if f.Reviewer != "" {
        srcKeys[i] = f.Reviewer
    } else {
        srcKeys[i] = "\x00anon\x00" + strconv.Itoa(i)
    }
}
```

For an empty Reviewer, the srcKey is `"\x00anon\x00" + i`. This is unique per finding. So unattributed findings are treated as their own source. ✓

But what if multiple findings have empty Reviewer? Each gets a unique srcKey. So they're treated as different sources, and can match each other. ✓

35. **Looking at potential issues with the constant `noMatchSentinel`:**

```go
const noMatchSentinel = 2.0
```

This is the cost for virtual cells. It's set to 2.0, which exceeds the real distance ceiling (1.0). So virtual cells are always more expensive. ✓

36. **Potential issue: what if the real cost is exactly 2.0?**

In `pairDistance`, the max real cost is 1.0 (when Jaccard is 0). So real cost is in [0, 1]. Virtual cost is 2.0. No conflict. ✓

37. **Looking at potential issues with the `distanceCeiling` constant:**

```go
const (
    distanceFloor   = 0.0
    distanceCeiling = 1.0
)
```

`distanceCeiling = 1.0`. The maximum distance is 1.0 (when Jaccard is 0). ✓

But the sentinel is 2.0. So the sentinel exceeds the ceiling by 1.0. This is a safety margin. ✓

38. **Looking at potential issues with how `classify` is used:**

`classify` returns (relation, similarity). The relation is one of relDistinct, relGray, relMerge. The similarity is a float. We only use the similarity in `pairDistance`. We use the relation in `mergeable` and the gray pair scan.

In `pairDistance`:
```go
_, sim := classify(tokA, tokB)
return distanceCeiling - sim
```

We ignore the relation. This is fine because we only need the distance. ✓

In `mergeable`:
```go
rel, _ := classify(tokens[a], tokens[b])
return rel == relMerge
```

We use the relation. The similarity is ignored. ✓

In the gray pair scan:
```go
rel, sim := classify(tokens[i], tokens[j])
if rel != relGray {
    continue
}
```

We use the relation. The similarity is used for `AmbiguousCluster.Similarity`. ✓

39. **Looking for potential bugs in the gray pair scan:**

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

We only add gray pairs that are in different groups. This avoids double-counting merged findings. ✓

But what about adjudicated gray pairs? The check is `if adjudicatedMerges[id] { uf.union(groupOf[i], groupOf[j]); continue }`. So adjudicated pairs are unioned but not added to amb. ✓

40. **Looking for potential issues with the kept filter:**

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

We keep pairs whose groups are not transitively merged. The groups in `ambiguousPairs` are group IDs, not finding indices. So we compare group roots. ✓

`keptIdx` tracks the finding indices for the gray-involved check. ✓

41. **Looking for potential issues with `soloRoot`:**

```go
soloRoot := make(map[int]bool, n)
for _, idxs := range rootIdx {
    if len(idxs) == 1 {
        soloRoot[idxs[0]] = true
    }
}
```

This marks findings that are the only member of their root. After adjudicated unions, only true solo findings are marked. ✓

But what about a finding that was a solo group but got unioned with another via adjudication? `len(idxs) > 1`, so not solo. ✓

42. **Potential issue: noise isolation for adjudicated pairs**

In `TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise`, A and B are adjudicated. They're unioned. So their root has 2 members. soloRoot is false for A and B. Even if they were noise in DBSCAN, they wouldn't be isolated. ✓

But wait, what if a solo group is adjudicated? Then it's unioned with another group. `len(idxs) > 1`. Not solo. ✓

What if two solo groups are adjudicated? They form a 2-member root. Not solo. ✓

43. **Let me look at potential issues with the test for adjudicated merge:**

The test `TestDedupeCluster_AdjudicatedGrayPairMerges` (in dedupe_test.go) is similar:
```go
a := fnd("a.go", 1, "alpha beta gamma", "greta")
b := fnd("a.go", 1, "alpha beta delta", "kai") // 0.5 → gray
id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
groups, amb := dedupeCluster([]Finding{a, b}, []string{"", ""}, map[string]bool{id: true})
length(t, groups, 1, "adjudicated duplicate merges into one group")
length(t, groups[0], 2, "both members")
length(t, amb, 0, "an adjudicated pair is not re-recorded as ambiguous")
```

Sources: greta, kai.
- greta: 1 candidate a. groups = [{a}]
- kai: 1 candidate b. cost(0, b) = 0.5. mergeable(a, b) = false (gray, Jaccard 0.5). Not accepted. b seeds new group. groups = [{a}, {b}]

Gray pair scan:
- a-b: gray. ID in adjudicated. uf.union(0, 1). Continue (not added to amb).

kept filter: uf.find(0) == uf.find(1). So no entries kept.

amb = [].

rootIdx:
- gi=0: r = 0. rootIdx[0] = [a]
- gi=1: r = uf.find(1) = 0 (after union). rootIdx[0] = [a, b]

soloRoot: rootIdx[0] has 2 members. Not solo.

Output: groups = [{a, b}], amb = [].

DBSCAN: denseNeighbor(a, b) = false (not mergeable). No dense cluster. dense = false. No noise.

groups = 1, amb = 0. ✓

44. **Let me look at potential issues with `TestClusterKeys_UsesGrouper`:**

```go
func TestClusterKeys_UsesGrouper(t *testing.T) {
    keys := clusterKeys([]Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 20, "y", "r2")},
        fakeGrouper{keys: map[int]string{10: "K1", 20: ""}})
    eq(t, keys[0], "K1", "grouper key threaded through")
    eq(t, keys[1], "", "empty key preserved")
}
```

This tests that `clusterKeys` correctly threads the Grouper. `fakeGrouper` is a mock Grouper. The test expects the grouper's keys to be used.

But wait, `fakeGrouper` is a struct with a `keys` field. The method is `GroupKey(f Finding) string`. Let me check if it's defined elsewhere. The diff doesn't define it, so it must be in a separate test file. I trust it's correct.

45. **Potential issue: `clusterKeys` doesn't handle nil Grouper correctly?**

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

If g is nil, return empty strings. Otherwise, call g.GroupKey(f). ✓

But wait, what if g is a typed nil interface? Then `g == nil` is false, but calling `g.GroupKey(f)` would panic. Hmm. But in practice, the caller (`reconcile.go`) passes `opts.Grouper`, which is a user-supplied Grouper interface. If the user doesn't set it, it's nil. So this should be fine.

46. **Looking at potential issues with the `Grouper` interface:**

The `Grouper` interface must have a `GroupKey
</think>
