The user wants me to review a diff based on a sprint plan. I need to:

1. Only flag issues in files/areas directly related to the sprint plan's tasks/stories
2. Output findings in TD_STREAM format (pipe-delimited)
3. If no issues, output nothing

Let me analyze the diff against the sprint plan:

Sprint Plan Scope:
- New canonical rank map + NormalizeSeverity in internal/stream
- Migration of reconcile, fanout/postprocess, verify, report
- Fixing casing-asymmetry lookup at internal/reconcile/merge.go:104
- Tests proving single source of truth
- Out of scope: registry reviewSeverities, any change to severity values or ordering

Files changed in diff:
1. internal/boundaries_test.go - adds "stream" to verify imports. This is related to verify migration (in scope).
2. internal/fanout/postprocess.go - migration to stream.SeverityRank and stream.NormalizeSeverity. In scope.
3. internal/reconcile/disagree.go - fixes at boundary lookups. In scope.
4. internal/reconcile/merge.go - alias + merge.go:104 fix. In scope.
5. internal/reconcile/severity_consolidation_test.go - new tests. In scope.
6. internal/report/render.go - migration to stream. In scope.
7. internal/report/render_test.go - tests. In scope.
8. internal/stream/severity.go - new canonical owner. In scope.
9. internal/stream/severity_test.go - tests. In scope.
10. internal/verify/severity.go - migration to stream. In scope.

Now let me look for issues in these changes.

Issue 1: internal/reconcile/merge.go
The change modifies mergeSeverity to use `norm` for `seen`, `max`, and `minSev`.
Wait, the sprint plan says: "Fix the raw-vs-upper-cased lookup asymmetry... all consumers normalize identically."
The merge.go change normalizes the severity and stores `norm` in `max`. But what gets returned? The function returns `max` (the normalized string), not the original `f.Severity`.
Is this correct? The sprint plan says "the returned max/min are canonical." That's intentional. But wait - does this change behavior? If a reviewer outputs "critical" and it gets normalized to "CRITICAL", the merged finding will have Severity "CRITICAL". This is mentioned in the test: `if got.Severity != "CRITICAL" { t.Fatalf(...) }`. So this is intended.
But wait - looking at the code carefully:
```go
if rank > maxRank {
    maxRank, max = rank, norm
}
```
The return values are `(max, disagreement string)`. The `max` string is returned as the severity. But `norm` is the normalized form. This means mixed-case input gets canonicalized to uppercase in the output. The sprint plan clarifies says this is intended: "and the returned max/min are canonical."
So no issue there.

Issue 2: internal/reconcile/disagree.go
```go
if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
    if r > maxRank {
        maxRank, maxSev = r, f.Severity
    }
}
```
Wait, `maxSev` is set to `f.Severity` (the raw, unnormalized severity), but the rank lookup uses the normalized severity. Then later:
```go
score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
```
If `maxSev` is raw, it gets normalized again for the score. But what if `f.Severity` is " critical "? `NormalizeSeverity` handles it. But what about the `if r > maxRank` check? It uses the normalized rank. So "critical" gets rank 4. But maxSev stores " critical " (with spaces). Is that used anywhere else? `scoreFor` normalizes it again. But is `maxSev` used for anything else in the function? Let me check the surrounding code.
Looking at the diff:
```go
for _, f := range c.Findings {
    if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
        if r > maxRank {
            maxRank, maxSev = r, f.Severity
        }
    }
    revSet[f.Reviewer] = true
    positions = append(positions, Position{File: f.File, Line: f.Line, Severity: f.Severity})
}
```
`maxSev` is only used later for:
```go
score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
```
So it gets normalized again there. But wait - what if maxSev is empty or unknown? It might remain empty string, which NormalizeSeverity handles. This seems okay but slightly inconsistent. However, not necessarily a bug since it's normalized before use.

Wait, but what if there are mixed case severities in the same cluster? The first one encountered with the max rank wins and its raw casing is stored in maxSev. Since it gets normalized before being passed to scoreFor, the score will be correct. But is maxSev used anywhere else? The function returns a DisagreementItem, which includes a Score but not a Severity directly from maxSev. Let me check if DisagreementItem has a Severity field. The diff doesn't show it, but the sprint plan says `disagree.go:338 AND 360` are boundary sites. So the change is intended. No issue.

Issue 3: internal/verify/severity.go
The function `meetsSeverityFloor` uses `stream.SeverityRank[stream.NormalizeSeverity(...)]`.
What if minSeverity is empty? NormalizeSeverity("") returns "". stream.SeverityRank[""] is not in the map, so it returns 0. `mr` would be 0. `fr >= mr` would be `fr >= 0`. But wait, the old code:
```go
fr := severityRank[normalizeSeverity(findingSeverity)]
mr := severityRank[normalizeSeverity(minSeverity)]
if fr == 0 {
    return false
}
return fr >= mr
```
If `minSeverity` is unknown/empty, `mr` is 0. So `fr >= 0` is true for any valid finding. But the intention of `meetsSeverityFloor` is probably that if minSeverity is unknown, it should maybe not pass everything? Actually, if minSeverity is empty or unknown, the old behavior would also have mr=0, and since fr>=0 is true (for fr>0), findings would pass. Wait, fr is an int. For a valid severity, fr is 1-4. So fr >= 0 is always true. So an empty/unknown minSeverity means everything passes (except unknown findings which return false at fr==0). This seems like pre-existing behavior, not changed.

Issue 4: internal/reconcile/merge.go alias
`var SeverityRank = stream.SeverityRank`
This is a direct map assignment. Since maps are reference types in Go, `reconcile.SeverityRank` and `stream.SeverityRank` point to the same underlying map. The sprint plan explicitly says: "reconcile migrates via re-export alias — var SeverityRank = stream.SeverityRank (Option A)."
This means any mutation to `reconcile.SeverityRank` would also mutate `stream.SeverityRank`. The comment in stream says: "Read-only after init: the map is written once at package load and only read thereafter. Concurrent fan-out agents share this map, so a write would race — if mutation is ever needed, copy it locally first."
But the sprint plan acknowledges this: "Kept as an exported alias so reconcile's internal lookups read it unqualified and external callers keep a stable symbol."
Is there a risk that some code modifies reconcile.SeverityRank? The old code had a literal map. Now it's an alias to the canonical map. If any code does `reconcile.SeverityRank["NEW"] = 5`, it would corrupt the canonical map for all consumers. But the sprint plan says this is the chosen design. And the comment warns against mutation.
However, looking at the code, is there any mutation? I don't see any. But from a security/maintainability perspective, an exported alias to a mutable shared map is a bit risky. But the sprint plan explicitly chose this option. The question is whether I should flag it.
The sprint plan says: "reconcile migrates via re-export alias... Full removal (Option B) was rejected as unnecessary churn."
Since the sprint plan explicitly chose this approach and it's within scope, I should not flag it as an issue unless it's clearly wrong. But wait - the map is mutable and shared. If any test or code accidentally mutates it, it affects global state. In tests, this could cause flakiness. But again, this is the intended design per the sprint plan.

Issue 5: Test `TestMerge_NormalizesMixedCaseSeverity`
```go
func TestMerge_NormalizesMixedCaseSeverity(t *testing.T) {
    group := []stream.Finding{
        {Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
        {Severity: "LOW", File: "a.go", Line: 1, Reviewer: "r2"},
    }
    got := Merge(group)
    if rank := SeverityRank[stream.NormalizeSeverity(got.Severity)]; rank != 4 {
        t.Fatalf("Merge severity = %q (rank %d), want a CRITICAL-rank (4) winner", got.Severity, rank)
    }
}
```
This test calls `Merge(group)` which returns a `stream.Finding` (I assume). It checks that the rank of the returned severity is 4. But it doesn't check that `got.Severity == "CRITICAL"`. It only checks the rank. However, there is another test `TestMerge_MixedCaseDuplicateIsNotADisagreement` that checks `got.Severity == "CRITICAL"`. So combined they cover it. Not necessarily an issue.

Issue 6: Test `TestSeverityRankOf_MatchesCanonical`
```go
func TestSeverityRankOf_MatchesCanonical(t *testing.T) {
    for sev, rank := range stream.SeverityRank {
        assert.Equal(t, rank, severityRankOf(sev), "severity %s must rank identically in report and reconcile", sev)
    }
    assert.Equal(t, 0, severityRankOf("unknown"), "unknown severity must rank 0 in report view")
}
```
This test iterates over `stream.SeverityRank` and checks `severityRankOf(sev)`. But `severityRankOf` normalizes the input. Since the keys in `stream.SeverityRank` are already normalized ("CRITICAL", etc.), this tests the main case. The new test `TestSeverityRankOf_NormalizesCasing` tests the boundary. This seems fine.

Issue 7: internal/report/render.go
```go
func severityRankOf(s string) int {
    if r, ok := stream.SeverityRank[stream.NormalizeSeverity(s)]; ok {
        return r
    }
    return 0
}
```
This is fine.

Issue 8: internal/fanout/postprocess.go
The code removes the local `severityRank` map and uses `stream.SeverityRank` and `stream.NormalizeSeverity`. This looks correct. No issues.

Issue 9: internal/stream/severity.go
```go
var SeverityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
```
This is the canonical map. It's a package-level var, exported. It could be mutated by any package importing stream. But the comment warns about this. Is there a way to make it immutable? In Go, you could return a copy or use an unexported map with a getter. But the sprint plan specifically wants the exported map so consumers can do direct lookups: `stream.SeverityRank[key]`. So this is by design.

Issue 10: internal/reconcile/merge.go
Wait, looking at the function signature of `mergeSeverity`:
```go
func mergeSeverity(group []stream.Finding) (max, disagreement string) {
```
The return value `max` is now set to `norm` (e.g., "CRITICAL"). But what about the `disagreement` string? Let's look at the logic:
```go
seen := make(map[string]bool)
for _, f := range group {
    norm := stream.NormalizeSeverity(f.Severity)
    rank, ok := SeverityRank[norm]
    if !ok {
        continue // unknown severity ignored for max/min
    }
    seen[norm] = true
    ...
}
if max == "" {
    return SevLow, ""
}
if len(seen) > 1 {
    disagreement = fmt.Sprintf("%s vs %s", max, minSev)
}
return max, disagreement
```
Since `seen` is keyed by `norm`, and `max` and `minSev` are also `norm`, if there are multiple severities, the disagreement string will use the normalized forms. E.g., "HIGH vs LOW". This seems correct and consistent.

But wait - what if `minSev` is empty? If all severities are unknown except one valid severity? Actually, if `max` is set, then `minRank` is also set because the same `f` that sets max also sets min. But what if the group has one valid severity? `len(seen)` is 1, so no disagreement. Correct.

What about the order of the disagreement string? It says `fmt.Sprintf("%s vs %s", max, minSev)`. If max is the highest severity and minSev is the lowest, it prints "HIGHEST vs LOWEST". That's probably fine.

Let me look carefully for any real issues.

Potential issue: In `internal/reconcile/disagree.go`, the `maxSev` variable is assigned `f.Severity` (raw), not normalized. Then later it's normalized when computing score. But what if `maxSev` is used in the returned struct?
Let me check the surrounding code in disagree.go. The diff only shows:
```go
for _, f := range c.Findings {
    if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
        if r > maxRank {
            maxRank, maxSev = r, f.Severity
        }
    }
    revSet[f.Reviewer] = true
    positions = append(positions, Position{File: f.File, Line: f.Line, Severity: f.Severity})
}
```
And later:
```go
score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
```
And the function returns a `DisagreementItem`. Does DisagreementItem contain maxSev? Looking at the diff, the return statement isn't shown, but the sprint plan says this is a boundary site for normalization. The raw `maxSev` is only used for the scoreFor call where it gets normalized. So it's safe.

Wait, actually, looking at the diff for disagree.go:
```go
@@ -335,7 +335,7 @@ func grayZoneItem(c AmbiguousCluster) DisagreementItem {
 	revSet := map[string]bool{}
 	positions := make([]Position, 0, len(c.Findings))
 	for _, f := range c.Findings {
-		if r, ok := SeverityRank[f.Severity]; ok {
+		if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
 			if r > maxRank {
 				maxRank, maxSev = r, f.Severity
 			}
@@ -357,7 +357,7 @@ func grayZoneItem(c AmbiguousCluster) DisagreementItem {
 	}
 	reviewers := sortedKeys(revSet)
 	indep := atLeastOne(len(reviewers))
-	score := scoreFor(spread, indep, SeverityRank[maxSev])
+	score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
```
What if `maxRank` stays -1 because ALL severities are unknown/invalid? Then `maxSev` is empty string. Then `scoreFor` gets `SeverityRank[NormalizeSeverity("")]` = `SeverityRank[""]` = 0. That's the unknown severity floor. The comment in the code says: "When all members carry unknown or blank severities, SeverityRank[maxSev] is 0 and spread is 0". This is intended. But wait, before the change, if maxSev was empty, `SeverityRank[""]` would panic? No, map lookup in Go on a missing key returns the zero value (0). So `SeverityRank[""]` returns 0. No panic. After the change, `SeverityRank[NormalizeSeverity("")]` also returns 0. So behavior is identical.

Is there any bug introduced? Let's think about `merge.go`.
The function `mergeSeverity` returns `(max, disagreement string)`. The `max` is now normalized. The caller `Merge` probably assigns it to `finding.Severity`. This means the output finding has canonical severity. This is intended and tested.

What about `verify/severity.go`?
```go
func meetsSeverityFloor(findingSeverity, minSeverity string) bool {
    fr := stream.SeverityRank[stream.NormalizeSeverity(findingSeverity)]
    mr := stream.SeverityRank[stream.NormalizeSeverity(minSeverity)]
    if fr == 0 {
        return false
    }
    return fr >= mr
}
```
Wait, if `minSeverity` is unknown (e.g., "foo"), `mr` is 0. Then `fr >= 0` is true for any valid finding (fr=1,2,3,4). This means an unknown minSeverity floor acts like the lowest possible floor (everything valid passes). Is this intentional?
Looking at the old code:
```go
fr := severityRank[normalizeSeverity(findingSeverity)]
mr := severityRank[normalizeSeverity(minSeverity)]
if fr == 0 {
    return false
}
return fr >= mr
```
Yes, the old code had the exact same behavior. `mr` would be 0 for unknown minSeverity, and `fr >= 0` is true for valid findings. So no behavior change.

Is there any issue with the import in `boundaries_test.go`? It adds "stream" to verify's allowed imports. This is because verify now imports stream. That's correct and in scope.

Let me check `internal/report/render.go`.
It adds an import for `stream` and changes `severityRankOf` to use `stream.SeverityRank[stream.NormalizeSeverity(s)]`. This is correct.

Let me think about race conditions or concurrency issues. `stream.SeverityRank` is a shared map. In `postprocess.go`, the old comment said:
> Read-only after init: the map is written once at package load and only read thereafter. If mutation is ever needed, use a sync.Map or initialize a local copy — concurrent fan-out agents share this map, so a write would race.

The new code uses `stream.SeverityRank` directly. The comment in `stream/severity.go` says the same thing. Since nobody is writing to it, it's safe.

What about `internal/reconcile/merge.go`? It now aliases `stream.SeverityRank`. Since it's the same map, the reconcile package could theoretically mutate it, affecting everyone. But there are no writes.

Could there be a nil map issue? `stream.SeverityRank` is initialized with a composite literal, so it's never nil.

Let me look at the test files.
`internal/stream/severity_test.go`:
```go
func TestSeverityRank_CanonicalOrdering(t *testing.T) {
    want := map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
    for sev, rank := range want {
        if got := SeverityRank[sev]; got != rank {
            t.Errorf("SeverityRank[%q] = %d, want %d", sev, got, rank)
        }
    }
    if SeverityRank["CRITICAL"] <= SeverityRank["HIGH"] ||
        SeverityRank["HIGH"] <= SeverityRank["MEDIUM"] ||
        SeverityRank["MEDIUM"] <= SeverityRank["LOW"] ||
        SeverityRank["LOW"] <= 0 {
        t.Errorf("ordering not strictly descending CRITICAL>HIGH>MEDIUM>LOW>0")
    }
    if _, ok := SeverityRank["unknown"]; ok {
        t.Errorf("unknown severity must be absent from the rank map (rank 0)")
    }
}
```
This is fine.

`internal/reconcile/severity_consolidation_test.go`:
Tests `Merge`, `grayZoneItem`, and mixed-case duplicates. All look correct.

`internal/report/render_test.go`:
Tests `severityRankOf` with canonical and normalized casing. Fine.

Are there any logic errors?
In `internal/reconcile/merge.go`, the `mergeSeverity` function:
```go
for _, f := range group {
    norm := stream.NormalizeSeverity(f.Severity)
    rank, ok := SeverityRank[norm]
    if !ok {
        continue // unknown severity ignored for max/min
    }
    seen[norm] = true
    if rank > maxRank {
        maxRank, max = rank, norm
    }
    if rank < minRank {
        minRank, minSev = rank, norm
    }
}
if max == "" {
    return SevLow, ""
}
if len(seen) > 1 {
    disagreement = fmt.Sprintf("%s vs %s", max, minSev)
}
return max, disagreement
```
What if the group has findings with unknown severity and no known severity? Then `max` stays "". The function returns `SevLow, ""`. This is pre-existing behavior. Fine.

Wait, I need to look carefully at one thing: the `disagree.go` change.
```go
for _, f := range c.Findings {
    if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
        if r > maxRank {
            maxRank, maxSev = r, f.Severity
        }
    }
    ...
}
```
If `f.Severity` is " critical ", `NormalizeSeverity` returns "CRITICAL", rank is 4. `maxSev` is set to " critical " (with spaces). Then later:
```go
score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
```
`NormalizeSeverity(" critical ")` returns "CRITICAL", so rank is 4. This is fine.

But is `maxSev` used anywhere else in `grayZoneItem`? Let's look at the code structure. The diff doesn't show the full function, but the `grayZoneItem` returns a `DisagreementItem`. The score is computed and likely stored in the struct. The sprint plan specifically mentions `disagree.go:338` and `disagree.go:360` as boundary sites. So the use of `maxSev` is limited to the score calculation. This is fine.

Hmm, let me look at `internal/fanout/postprocess.go` one more time.
```go
func enforceConstraints(findings []stream.Finding, agent, minSeverity string, maxFindings *int) []stream.Finding {
    var dropped, truncated int

    // 1. Severity floor.
    if floor := stream.NormalizeSeverity(minSeverity); floor != "" {
        floorRank, known := stream.SeverityRank[floor]
        if !known {
            fmt.Fprintf(os.Stderr, "[WARN] agent %q: unknown min_severity %q, failing open (no findings dropped)\n", agent, minSeverity)
        } else {
            kept := make([]stream.Finding, 0, len(findings))
            for _, f := range findings {
                if stream.SeverityRank[stream.NormalizeSeverity(f.Severity)] >= floorRank {
                    kept = append(kept, f)
                } else {
                    dropped++
                }
            }
            findings = kept
        }
    }
```
This looks correct. No issues.

Actually, wait. In `internal/verify/severity.go`, the comment was updated:
```go
// comparison is case-insensitive via stream.NormalizeSeverity, the canonical normalizer shared
// by every severity consumer.
```
This is accurate.

Is there any potential nil pointer dereference or panic? No.

What about `internal/boundaries_test.go`?
The diff adds "stream" to verify's allowed imports. That's because verify now imports stream. Is "stream" already in the list for other packages? Yes, it's already there for reconcile, report, etc. Adding it for verify is correct.

Let me think if there are any issues I might be missing.
- Are there any import cycles introduced? No, because stream has zero internal dependencies and verify already imports other packages. Adding stream to verify is fine because stream doesn't import verify.
- Is the alias `var SeverityRank = stream.SeverityRank` safe? It's the same map. If someone does `delete(reconcile.SeverityRank, "LOW")`, it deletes from the canonical map. But there's no code that does that. It's a maintainability risk, but the sprint plan explicitly chose this option.
- The sprint plan says: "reconcile migrates via re-export alias". So this is explicitly in scope and intended.

Wait, I should check if there are any places where `reconcile.SeverityRank` is written to (mutated). If any test or code does a write, it would now corrupt the shared map. I don't see any writes in the diff. But is there any in the existing codebase outside the diff? The diff only shows the changes. The sprint plan says to migrate consumers. Since I'm only reviewing the diff, I should only flag issues visible in the diff.

Let me look at the diff one more time for subtle bugs.

In `internal/reconcile/merge.go`, the return when `max == ""`:
```go
if max == "" {
    return SevLow, ""
}
```
Wait, if all findings in the group have unknown severity, `max` stays "". The function returns `SevLow` (which is "LOW"). Is that correct? That was the pre-existing behavior. The function returns a default severity of LOW when all are unknown. This seems intentional.

What about when there are multiple findings and some have unknown severity? Unknown severities are skipped (`continue`). So only known severities participate in max/min and seen set. This is correct.

Let me consider if `internal/reconcile/disagree.go` has a bug.
Before the change:
```go
if r, ok := SeverityRank[f.Severity]; ok {
    if r > maxRank {
        maxRank, maxSev = r, f.Severity
    }
}
```
After:
```go
if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
    if r > maxRank {
        maxRank, maxSev = r, f.Severity
    }
}
```
This looks correct. It normalizes for the lookup but keeps the raw severity in maxSev. Wait, but it only keeps the raw severity of the *first* finding that achieves maxRank. If there are two findings with severity "CRITICAL" and "critical", and the loop encounters "critical" first, maxRank becomes 4 and maxSev becomes "critical". If it later encounters "CRITICAL", r == maxRank, so the condition `r > maxRank` is false, maxSev stays "critical". Then `scoreFor` normalizes maxSev to "CRITICAL", so score is still 4. This is fine.

But wait - what if the loop encounters "LOW" first, then "critical"? maxRank starts at -1. "LOW" has rank 1. maxRank becomes 1, maxSev becomes "LOW". Then "critical" has rank 4. 4 > 1, so maxRank becomes 4, maxSev becomes "critical". This is fine.

What if maxRank stays -1 because ALL findings have unknown severity? Then maxSev stays "". Then `scoreFor` uses `SeverityRank[NormalizeSeverity("")]` = 0. The comment says: "When all members carry unknown or blank severities, SeverityRank[maxSev] is 0 and spread is 0, so scoreFor returns 0." But wait, before the change, `SeverityRank[maxSev]` with maxSev="" would return 0. After the change, `SeverityRank[NormalizeSeverity("")]` also returns 0. So identical.

Is there any issue with `boundaries_test.go`? The verify package now imports stream. The test verifies allowed imports. This is correct.

I need to be careful about false positives. The instructions say "No false positives: Only report real issues."

Let me think about whether the alias introduces a real issue.
`var SeverityRank = stream.SeverityRank`
If someone writes to `reconcile.SeverityRank`, they write to `stream.SeverityRank`. This is a real issue in terms of encapsulation, but it's an explicit design choice in the sprint plan. The sprint plan says: "reconcile migrates via re-export alias — var SeverityRank = stream.SeverityRank (Option A)." It was a conscious decision. So I should not flag it.

What about the fact that `mergeSeverity` now returns normalized severity strings? Could that break downstream consumers that expect the original casing?
The sprint plan explicitly says: "the returned max/min are canonical." And the test verifies this. This is intended behavior change/fix.

Let me look at `internal/report/render.go`.
The function `severityRankOf` now uses `stream.SeverityRank[stream.NormalizeSeverity(s)]`. Before, it used `reconcile.SeverityRank[s]`. Since `reconcile.SeverityRank` is now an alias to `stream.SeverityRank`, and the old code did NOT normalize, there is a behavior change: mixed-case input now gets ranked correctly instead of returning 0. This is the intended fix.

Let me check if there are any issues in the new stream package.
`internal/stream/severity.go`:
```go
var SeverityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}

func NormalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
```
The function is simple and correct. The map is correct.

Wait, is there a risk that `strings.ToUpper` doesn't handle Unicode correctly? For ASCII severity strings, it's fine. The existing code used `strings.ToUpper` too. So no issue.

Are there any issues with the tests?
`internal/reconcile/severity_consolidation_test.go` imports `stream` without any alias, which is fine since it's in the same module.

`internal/stream/severity_test.go`:
```go
if SeverityRank["CRITICAL"] <= SeverityRank["HIGH"] ||
    SeverityRank["HIGH"] <= SeverityRank["MEDIUM"] ||
    SeverityRank["MEDIUM"] <= SeverityRank["LOW"] ||
    SeverityRank["LOW"] <= 0 {
    t.Errorf("ordering not strictly descending CRITICAL>HIGH>MEDIUM>LOW>0")
}
```
This is fine.

`internal/report/render_test.go`:
```go
func TestSeverityRankOf_MatchesCanonical(t *testing.T) {
    for sev, rank := range stream.SeverityRank {
        assert.Equal(t, rank, severityRankOf(sev), "severity %s must rank identically in report and reconcile", sev)
    }
    assert.Equal(t, 0, severityRankOf("unknown"), "unknown severity must rank 0 in report view")
}
```
This iterates over the map and checks each key. Fine.

`internal/report/render_test.go` adds import of `stream`. Fine.

After careful review, I don't see any real issues in the diff that are within scope. The changes are clean, well-tested, and follow the sprint plan exactly.

Wait, let me look one more time at `internal/reconcile/disagree.go`.
The `maxSev` variable stores the raw severity string. It is then normalized before being used in `scoreFor`. But what if `maxSev` is used in the returned `DisagreementItem`? The diff doesn't show the return statement. Let me infer from the code.

The diff shows:
```go
func grayZoneItem(c AmbiguousCluster) DisagreementItem {
    maxRank := -1
    var maxSev string
    revSet := map[string]bool{}
    positions := make([]Position, 0, len(c.Findings))
    for _, f := range c.Findings {
        if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
            if r > maxRank {
                maxRank, maxSev = r, f.Severity
            }
        }
        revSet[f.Reviewer] = true
        positions = append(positions, Position{File: f.File, Line: f.Line, Severity: f.Severity})
    }
    // ...
    score := scoreFor(spread, indep, SeverityRank[stream.NormalizeSeverity(maxSev)])
    // ...
}
```
I don't see maxSev being put into the returned struct directly. The score is computed. This seems fine.

Actually, I want to double-check one thing. In `merge.go`, the old code had:
```go
var SeverityRank = map[string]int{SevCritical: 4, SevHigh: 3, SevMedium: 2, SevLow: 1}
```
Where `SevCritical` etc are constants:
```go
const (
    SevCritical = "CRITICAL"
    SevHigh     = "HIGH"
    SevMedium   = "MEDIUM"
    SevLow      = "LOW"
)
```
The new code is:
```go
var SeverityRank = stream.SeverityRank
```
This means the map keys are still "CRITICAL", "HIGH", etc. The constants `SevCritical` etc are still defined but not used in the map initialization anymore. Are they used elsewhere? The diff doesn't show them being removed. The sprint plan says: "the reconcile.Sev* constants (stay in reconcile)". So they are kept. That's fine.

Is there any place in the diff where `reconcile.SevCritical` etc are still used? In `verify/severity.go`, the old code used:
```go
var severityRank = map[string]int{
    reconcile.SevCritical: 4,
    reconcile.SevHigh:     3,
    reconcile.SevMedium:   2,
    reconcile.SevLow:      1,
}
```
This is removed. So `verify` no longer depends on `reconcile.Sev*`. That's fine.

Wait, I just noticed something. In `verify/severity.go`, the old code imported `reconcile`. The new code imports `stream` instead. But the `boundaries_test.go` change adds "stream" to verify's allowed imports, but does NOT remove "reconcile" from verify's allowed imports. The diff for `boundaries_test.go` shows:
```go
-	"verify":    {"reconcile", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs"},
+	"verify":    {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs"},
```
It adds "stream" but keeps "reconcile". Does verify still import reconcile? The diff for `verify/severity.go` shows it removed the reconcile import. But does verify import reconcile anywhere else? The diff doesn't show other verify files. If verify no longer imports reconcile anywhere, then "reconcile" should be removed from the allowed imports list. But the diff keeps it.
However, looking at the diff for `boundaries_test.go`, it ONLY shows the verify line change. It doesn't show other files in verify. I don't know if verify still imports reconcile in other files. The sprint plan doesn't explicitly say to remove the reconcile import from verify unless it's no longer needed. Since I only see the diff, and the diff only changes `severity.go` in verify, I cannot conclude that verify as a whole no longer imports reconcile. There might be other files in verify that still import reconcile. So keeping "reconcile" in the allowed list is safe and not necessarily an issue.

Actually, wait. The diff for `verify/severity.go` removes the import of reconcile. If that's the ONLY file in verify that imported reconcile, then the test `boundaries_test.go` would still pass because "reconcile" is still in the allow list. But if no file in verify imports reconcile anymore, it's a "dead" allowance. Is that an issue? It's maintainability debt, but it's in a test file that explicitly defines allowed imports. It might be intentionally conservative. The sprint plan scope doesn't mention cleaning up the boundaries_test.go list. So I should probably not flag it.

Let me reconsider if there are ANY issues.
- The alias `var SeverityRank = stream.SeverityRank` means the map is shared. If a consumer does a ranged write or delete, it affects everyone. But this is the explicit design.
- The tests look good and cover the boundary cases.
- The casing asymmetry is fixed.
- The migration is clean.

I think there are no real issues to report. The code is clean.

But I need to be absolutely sure. Let me look at `internal/fanout/postprocess.go` again.
Old code:
```go
var severityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
```
New code uses `stream.SeverityRank` and `stream.NormalizeSeverity`. In the sort:
```go
sort.SliceStable(findings, func(i, j int) bool {
    return stream.SeverityRank[stream.NormalizeSeverity(findings[i].Severity)] > stream.SeverityRank[stream.NormalizeSeverity(findings[j].Severity)]
})
```
This is correct. `SliceStable` is used, which is good for preserving order of equal elements.

In `enforceConstraints`, after severity floor filtering, `findings` is reassigned:
```go
findings = kept
```
Then truncation happens on `findings`. This is correct.

What about the `maxFindings` truncation? If `maxFindings` is nil or 0 or negative, it skips. The condition is:
```go
if maxFindings != nil && *