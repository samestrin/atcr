Let me analyze the diff carefully, looking for line-level bugs in the changed regions.

Let me go through each changed file:

1. `.planning/epics/active/15.1_leaderboard-cost-na-rendering.md` - new file, planning doc, no code bugs.

2. `.planning/technical-debt/README.md` - adds a TD item. Let me check the stats:
   - Before: LOW | 0 | 28 | 0
   - After: LOW | 1 | 28 | 0
   - Before: Open Items: 0 | Deferred Items: 59 | Total Items: 59
   - After: Open Items: 1 | Deferred Items: 59 | Total Items: 60
   
   Wait, the new item is marked `[ ]` (open checkbox) with severity LOW. So Open LOW goes from 0 to 1. Total goes from 59 to 60. But Deferred stays at 59? Let me check: the new item is open (not deferred). So Open=1, Deferred=59, Total=60. That works: 1+59=60. ✓

3. `CHANGELOG.md` - documentation, no code bugs.

4. `cmd/atcr/benchmark_run_resume_test.go` - test changes. Let me look carefully.

   The field `CostPerCorroboratedFindingUSD` changed from `float64` to `*float64`. Tests now dereference.

   Line: `total += *rv.CostPerCorroboratedFindingUSD` - after `require.NotNil`. OK.

   `require.Greater(t, *baseline.Reviewers[0].CostPerCorroboratedFindingUSD, 0.0, ...)` - after NotNil. OK.

5. `cmd/atcr/benchmark_run_test.go` - test changes. Similar pattern.

   `require.NotNil(t, greta.CostPerCorroboratedFindingUSD, "corroborated findings exist -> key present even at 0 cost")`
   `assert.InDelta(t, 0.0, *greta.CostPerCorroboratedFindingUSD, 1e-9, "stub reports no usage")`
   
   OK.

6. `docs/benchmark.md` and `docs/scorecard.md` - documentation.

7. `internal/benchmark/score.go` - the core change.

   ```go
   if matchedFindings > 0 {
       v := r.CostUSD / float64(matchedFindings)
       pr.CostPerCorroboratedFindingUSD = &v
   }
   ```
   
   This is fine. `v` is a local variable, taking its address is safe in Go (escapes to heap).

8. `internal/benchmark/score_test.go` - test changes.

9. `internal/scorecard/export.go` - the core change.

   ```go
   func costPer(totalCost float64, corroborated int) *float64 {
       if corroborated <= 0 {
           return nil
       }
       v := totalCost / float64(corroborated)
       return &v
   }
   ```
   
   This is fine.

   And in `finalize()`:
   ```go
   pr.CostPerCorroboratedFindingUSD = costPer(a.costTotal, a.corroborated)
   ```
   
   OK.

10. `internal/scorecard/export_test.go` - test changes.

Let me look more carefully for bugs.

Looking at `TestExport_ClampsNegativeMetrics`:
```go
rec.FindingsRaised = -5
rec.FindingsCorroborated = -2
rec.CostUSD = -1.0
rec.LatencyMS = -100
...
r := parseEnvelope(t, data).Reviewers[0]
assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)
if r.CostPerCorroboratedFindingUSD != nil {
    assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0)
}
```

The TD item added in README.md actually calls this out:
> TestExport_ClampsNegativeMetrics sets FindingsCorroborated=-2 which clamps to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil in this scenario and the nil-guard makes the GreaterOrEqual(0.0) assertion on that field dead code, never exercising negative-CostUSD clamping through the non-nil pointer path.

So this is a known issue, now filed as TD. The test change wraps the assertion in `if r.CostPerCorroboratedFindingUSD != nil` which makes the assertion dead code (never runs because the field is always nil when FindingsCorroborated=-2 clamps to 0). This is a real bug - the test no longer tests what it claims to test.

But this is already filed as TD. However, the change itself introduces a dead path. Let me think about whether to flag this.

The change is in the diff. The `if r.CostPerCorroboratedFindingUSD != nil` guard makes the assertion dead code in this specific test scenario. This is a real issue at the line level - a condition that can never be true (in this test). The TD item acknowledges it but the code change still introduces the dead path.

Actually, let me reconsider. The TD item says "the nil-guard makes the GreaterOrEqual(0.0) assertion on that field dead code". So the author knows. But the change is still a bug - the test no longer verifies what its name claims (clamping negative metrics). 

Let me flag this as a finding. The category would be "testing" or "dead-path".

Actually, looking more carefully at the TD description: it says the fix is to "Add a second case (or separate test) with a positive FindingsCorroborated and negative CostUSD so the clamp is actually exercised through the non-nil pointer branch." So this is acknowledged but not fixed in this PR.

Let me look for other bugs.

Looking at `internal/scorecard/export.go` `finalize()`:
```go
pr := PublicRecord{
    Model:             a.model,
    Persona:           a.persona,
    Runs:              a.runs,
    FindingsRaisedAvg: avgPerRun(a.raisedTotal, a.runs),
    CorroborationRate: clampRate(ratio(a.corroborated, a.raisedTotal)),
    LatencyP50MS:      medianInt64(a.latencies),
}
pr.CostPerCorroboratedFindingUSD = costPer(a.costTotal, a.corroborated)
```

Wait - `costPer` is called unconditionally now, but it returns nil when `corroborated <= 0`. So the field is set to nil when no corroboration. That's correct.

But wait - is there a clamping issue? In the original code, `costPer` returned `0` when `corroborated <= 0`. Now it returns `nil`. But what about negative `totalCost`? The TD item mentions `TestExport_ClampsNegativeMetrics` sets `CostUSD = -1.0`. With `FindingsCorroborated = -2`, this clamps to 0 at ingestion, so `corroborated <= 0` → nil. The negative cost path through the non-nil branch is never tested.

But actually, is there a clamping issue in the production code? Let me check - does `costPer` clamp negative costs? Looking at the original:
```go
func costPer(totalCost float64, corroborated int) float64 {
    if corroborated <= 0 {
        return 0
    }
    return totalCost / float64(corroborated)
}
```

No clamping of negative `totalCost`. So if `totalCost` is negative and `corroborated > 0`, the result is negative. The test `TestExport_ClampsNegativeMetrics` was supposed to verify that negative values get clamped to ≥ 0, but the field assertion was `assert.GreaterOrEqual(t, r.CostPerCorroboratedFindingUSD, 0.0)` which would FAIL if the value is negative. 

Wait, but the test was passing before. So either:
1. The negative cost gets clamped somewhere upstream (ingestion), OR
2. `FindingsCorroborated = -2` clamps to 0, making `corroborated <= 0`, so `costPer` returns 0 (not negative).

The TD item says: "FindingsCorroborated=-2 which clamps to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil in this scenario". So option 2. The negative CostUSD never reaches the division because corroborated is 0.

So the test change wraps the assertion in a nil guard, making it dead code. This is a real issue but it's already filed as TD. 

Hmm, but should I flag it? The instructions say "Find problems the author would prefer you didn't." The author already filed this as TD. But the change itself introduces a dead path. Let me flag it as a LOW finding since it's a dead path in the changed code.

Actually, wait. Let me re-read the scope rules: "Review only the changed regions." The changed region includes the `if r.CostPerCorroboratedFindingUSD != nil { assert.GreaterOrEqual(...) }` block. This is a dead path - the condition can never be true in this test scenario. This is exactly the kind of thing archer should catch.

Let me also check: is there a clamp on negative CostUSD anywhere? If `FindingsCorroborated` is positive but `CostUSD` is negative, `costPer` returns a negative value with no clamping. The test name says "ClampsNegativeMetrics" but doesn't actually test the negative-cost-with-positive-corroborated case. This is a real gap.

Let me look at the `AnonymizeRecord` path too. It calls `finalize()` via `reviewerAcc`, so it shares the same `costPer` logic. Good.

Now let me look at the benchmark `score.go` more carefully:

```go
if matchedFindings > 0 {
    v := r.CostUSD / float64(matchedFindings)
    pr.CostPerCorroboratedFindingUSD = &v
}
```

`r.CostUSD` - is this clamped? In the benchmark path, `CostUSD` comes from `ReviewerScore.CostUSD`. If it's negative, no clamping. But that's pre-existing behavior.

Let me check the test `TestScore_CostPerCorroborated`:
```go
CostUSD: 0.12,
Cases: []CaseScore{{Expected: []string{"correctness", "security"}, Raised: []string{"correctness", "security", "perf"}}},
```
2 matched findings (correctness, security), 1 unmatched (perf). 0.12 / 2 = 0.06. ✓

`TestScore_CostPerCorroboratedNilWhenPaidButUnmatched`:
```go
CostUSD: 0.5,
Cases: []CaseScore{{Expected: []string{"correctness"}, Raised: []string{"perf"}}},
```
Expected "correctness", raised "perf" - no match. `matchedFindings = 0`, so field is nil. ✓

Now let me look at the `TestExport_CostPerCorroboratedPresentAndZeroWhenGenuinelyFree` test:
```go
rec := exportRec("bruce", "m", 1)
rec.CostUSD = 0
data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
...
assert.Contains(t, s, `"cost_per_corroborated_finding_usd"`, "genuinely free reviewer still carries the key")
r := parseEnvelope(t, data).Reviewers[0]
require.NotNil(t, r.CostPerCorroboratedFindingUSD)
assert.Equal(t, 0.0, *r.CostPerCorroboratedFindingUSD)
```

This depends on `exportRec` setting `FindingsCorroborated` to a positive value. Let me check - `exportRec("bruce", "m", 1)` - I don't have the full definition, but the test `TestExport_CostPerCorroboratedFinding` uses the same and expects 0.04/7.0, so `FindingsCorroborated` is 7 (positive). With `CostUSD = 0`, `costPer(0, 7) = 0/7 = 0.0`, returns `&0.0`. So the key is present with value 0.0. ✓

But wait - `omitempty` on a `*float64` - does Go's `encoding/json` omit a non-nil pointer to 0.0? Let me think. `omitempty` for a pointer omits when the pointer is nil, NOT when the pointed-to value is zero. So `*float64` pointing to 0.0 will still be emitted as `0.0`. ✓ Good, this is correct.

Actually, let me double-check this. In Go's `encoding/json`:
- For a pointer type with `omitempty`, the field is omitted if the pointer is nil.
- The zero value of a pointer is nil.
- A non-nil pointer to 0.0 is NOT the zero value of the pointer type, so it's NOT omitted.

Yes, this is correct. `*float64` pointing to 0.0 will be serialized as `0.0`, not omitted. ✓

Now let me look at the `SurvivedSkepticRate` pattern for comparison - it's been working, so the pattern is proven.

Let me look for any other issues...

Looking at `benchmark_run_resume_test.go`:
```go
for _, rv := range baseline.Reviewers {
    require.NotNil(t, rv.CostPerCorroboratedFindingUSD, "priced reviewer with corroborated findings must carry the key")
    total += *rv.CostPerCorroboratedFindingUSD
}
```

This asserts that all reviewers in the baseline have non-nil cost. But what if a reviewer has zero matched findings? Then the field would be nil, and `require.NotNil` would fail. Is this a problem?

The test uses `usageStubCompleter` which provides usage, and the test comment says "Two priced reviewers (ComputeCostUSD is non-zero for these model ids per rates.go) drive a non-trivial cost sum". The test expects both reviewers to have corroborated findings. If the stub review produces findings that match expected categories, then `matchedFindings > 0` and the field is non-nil. This is a test expectation, not a bug.

But wait - what if the stub completer returns findings that DON'T match any expected category? Then `matchedFindings == 0` and the field is nil, causing `require.NotNil` to fail. This would be a test setup issue, not a code bug. The test is asserting that the stub produces matching findings. If the stub doesn't, the test fails - which is the intended behavior (guard against vacuous state).

Let me look at the `TestExport_ClampsNegativeMetrics` change more carefully:

```go
rec.FindingsRaised = -5
rec.FindingsCorroborated = -2
rec.CostUSD = -1.0
rec.LatencyMS = -100
data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
require.NoError(t, err)
r := parseEnvelope(t, data).Reviewers[0]
assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)
if r.CostPerCorroboratedFindingUSD != nil {
    assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0)
}
assert.GreaterOrEqual(t, r.LatencyP50MS, int64(0))
assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)
assert.LessOrEqual(t, r.CorroborationRate, 1.0)
```

The `if r.CostPerCorroboratedFindingUSD != nil` guard means the assertion `assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0)` is dead code in this test - it will never execute because `FindingsCorroborated = -2` clamps to 0, making `corroborated <= 0`, so `costPer` returns nil.

This is the exact issue filed in the TD item. The test name claims to clamp negative metrics, but the cost-per assertion is now dead code. The negative `CostUSD = -1.0` is never actually tested through the non-nil path.

This is a legitimate finding. The changed code introduces a dead path. Category: dead-path or testing.

Let me also check if there's a clamping issue in the production code. If `FindingsCorroborated` is positive but `CostUSD` is negative, `costPer` returns a negative value. Is there any clamping? Looking at the original code and the new code - no clamping of the result. The test was supposed to catch this but doesn't (because it sets both to negative, and the negative corroborated clamps to 0 first).

Actually, is there clamping of `FindingsCorroborated` at ingestion? The TD says "clamps to 0 at ingestion". So the `Record` ingestion path clamps negative `FindingsCorroborated` to 0. But `CostUSD` - is it clamped? If `CostUSD` is negative and `FindingsCorroborated` is positive, `costPer` returns negative. The test doesn't cover this case.

But this is pre-existing - the original code also didn't clamp. The test change just makes the dead-code nature explicit. Let me flag the dead path.

Let me also look at whether there's a mismatch between the benchmark `score.go` and the production `export.go` in how they compute cost-per. The epic says they should be byte-compatible.

Benchmark `score.go`:
```go
if matchedFindings > 0 {
    v := r.CostUSD / float64(matchedFindings)
    pr.CostPerCorroboratedFindingUSD = &v
}
```

Production `export.go`:
```go
func costPer(totalCost float64, corroborated int) *float64 {
    if corroborated <= 0 {
        return nil
    }
    v := totalCost / float64(corroborated)
    return &v
}
```

The benchmark uses `matchedFindings > 0` (strictly greater), production uses `corroborated <= 0` → nil (i.e., `corroborated > 0` → compute). These are equivalent: `matchedFindings > 0` ↔ `!(matchedFindings <= 0)`. For positive integers, `> 0` and `<= 0` are complementary. ✓

But wait - `matchedFindings` in benchmark is an `int` computed by counting matches. It can't be negative (it's a count). `corroborated` in production is also an int from `a.corroborated`. Can it be negative? The TD says `FindingsCorroborated = -2` clamps to 0 at ingestion. So after ingestion, `a.corroborated` is ≥ 0. So `corroborated <= 0` means `corroborated == 0`. ✓

The logic is equivalent. Good.

Now, is there a parity issue? The benchmark divides by `matchedFindings` (findings whose category matched), while production divides by `corroborated` (corroborated findings). Are these the same concept? In production, "corroborated" means cross-reviewer corroboration. In benchmark, "matched" means matched expected categories. These are different concepts but both represent "the denominator for cost-per." The epic says the benchmark repurposes `corroboration_rate` as category recall, and `cost_per_corroborated_finding_usd` as cost ÷ matched findings. So the semantics differ between production and benchmark, but the field name is the same. This is documented. Not a bug.

Let me look at one more thing - the `AnonymizeRecord` function. It calls `finalize()` on a `reviewerAcc`. The `reviewerAcc.add(r Record)` method accumulates `a.corroborated` and `a.costTotal`. Let me check if `a.corroborated` can be negative after clamping. If `Record.FindingsCorroborated` is clamped at ingestion to ≥ 0, then `a.corroborated` ≥ 0. ✓

OK, let me also check the CHANGELOG. It says `## [15.1.0] - 2026-07-02` but the epic is numbered 15.1. The previous entry was `## [15.0.0] - 2026-07-01`. So 15.1.0 follows 15.0.0. That's fine semantically.

Wait, actually let me re-read the CHANGELOG diff:
```
+## [15.1.0] - 2026-07-02
+
+Fixed a public leaderboard schema ambiguity...
+
+### Changed
+
+- `cost_per_corroborated_finding_usd` in the public leaderboard/benchmark submission schema is now `omitempty`...
```

And then:
```
 ## [Technical Debt] - 2026-07-01
```

So the new entry is above the Technical Debt entry. The Technical Debt entry is dated 2026-07-01 and the new entry is 2026-07-02. So chronologically, 15.1.0 comes after Technical Debt. That's fine.

But wait - is `## [15.1.0]` a valid semver? 15.1.0 is a valid semver. But the previous release was `15.0.0`. A schema change (adding omitempty, changing float64 to *float64) is technically a breaking change for consumers who expect the key to always be present. Should this be 16.0.0? Well, the CHANGELOG says "Changed" not "Breaking". This might be a concern but it's a judgment call, not a clear bug.

Actually, the `SubmissionSchema` constant is still 1 (not bumped). The epic says "It is intentionally decoupled from the on-disk store's SchemaVersion." But if the public submission schema changed (the key is now omitempty when it was always present before), shouldn't `SubmissionSchema` be bumped to 2? 

Let me look at the code:
```go
const SubmissionSchema = 1
```

This wasn't changed. But the schema DID change - `cost_per_corroborated_finding_usd` went from always-present to omitempty. Downstream consumers who expect the key to always be present will break. The epic's Risks table says "Schema change breaks downstream consumers | Choose the least-breaking option; add migration note to CHANGELOG". The CHANGELOG was updated, but `SubmissionSchema` wasn't bumped.

Hmm, but `SubmissionSchema` is described as "the version of the PUBLIC leaderboard submission format (Epic 10.0). It is intentionally decoupled from the on-disk store's SchemaVersion." If the public format changed, the version should bump. But this is a judgment call - maybe the author considers omitempty addition to be backward-compatible (consumers who handle the key being present should also handle it being absent). 

Actually, this IS a real issue. The whole point of `SubmissionSchema` is to version the public format. Changing `cost_per_corroborated_finding_usd` from always-present to omitempty IS a schema change. The constant should bump to 2. But... the constant wasn't changed in this diff. Is this in the diff scope? The diff touches `export.go` but the `SubmissionSchema` line wasn't changed. Let me check - the diff shows changes starting at line 20-something, and `SubmissionSchema` is at line ~22. Let me look:

```go
const SubmissionSchema = 1
```

This line is NOT in the diff (not changed). So it's outside the changed region. I'd need to flag it as out-of-scope if I want to mention it.

Actually wait, let me re-read the diff for export.go:

```
@@ -20,28 +20,30 @@ var ErrNoExportRecords = errors.New("no records match the export filters")
 // SubmissionSchema is the version of the PUBLIC leaderboard submission format
 // (Epic 10.0). It is intentionally decoupled from the on-disk store's
 // SchemaVersion: the local record format and the public submission format evolve
 // independently, so bumping one must not silently change the other.
 const SubmissionSchema = 1
```

The `const SubmissionSchema = 1` line is in the context (not changed). So it's outside the diff. But the comment above it says "the public submission format evolve independently, so bumping one must not silently change the other." The schema DID change but the version wasn't bumped. This is a real issue but outside the changed lines.

Hmm, but the instructions say: "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it." But this isn't pre-existing - it's a new issue introduced by this PR (the schema changed but the version didn't). The line itself is unchanged, but the issue is newly introduced by the surrounding changes.

Actually, the instructions say findings whose FILE:LINE falls outside the changed lines will be discarded. So I should flag it with CATEGORY out-of-scope.

Wait, but is this really a bug? Let me think more carefully. `SubmissionSchema = 1` is the schema version. The change makes `cost_per_corroborated_finding_usd` omitempty. Is this a breaking change?

For JSON consumers:
- Before: key always present (float64)
- After: key present when corroborated > 0, absent when corroborated == 0

A consumer that does `record["cost_per_corroborated_finding_usd"]` (JavaScript) would get `undefined` instead of `0.0` for the absent case. This IS a behavioral change. A consumer that does `record.CostPerCorroboratedFindingUSD` (Go) would need to handle nil pointer. This IS breaking.

So `SubmissionSchema` should arguably bump. But the author might have intentionally kept it at 1 if they consider this backward-compatible (omitempty is additive, not breaking). This is a judgment call.

Let me move on and look for clearer bugs.

Let me re-examine the test in `export_test.go`:

```go
func TestExport_CostPerCorroboratedPresentAndZeroWhenGenuinelyFree(t *testing.T) {
    rec := exportRec("bruce", "m", 1)
    rec.CostUSD = 0
    data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
    require.NoError(t, err)
    s := string(data)
    assert.Contains(t, s, `"cost_per_corroborated_finding_usd"`, "genuinely free reviewer still carries the key")
    r := parseEnvelope(t, data).Reviewers[0]
    require.NotNil(t, r.CostPerCorroboratedFindingUSD)
    assert.Equal(t, 0.0, *r.CostPerCorroboratedFindingUSD)
}
```

This test sets `CostUSD = 0` but relies on `exportRec("bruce", "m", 1)` having positive `FindingsCorroborated`. If `exportRec` sets `FindingsCorroborated` to 0 or negative, this test would fail. But since `TestExport_CostPerCorroboratedFinding` uses the same `exportRec` and expects 0.04/7.0, `FindingsCorroborated` must be 7. So this test is correct. ✓

Now let me look at the `TestExport_CostPerCorroboratedAbsentWhenNoCorroboration` test:
```go
rec := exportRec("bruce", "m", 1)
rec.FindingsCorroborated = 0
rec.CostUSD = 0.5
data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
require.NoError(t, err)
s := string(data)
assert.NotContains(t, s, `"cost_per_corroborated_finding_usd"`, "omitempty must drop the key when undefined")
r := parseEnvelope(t, data).Reviewers[0]
assert.Nil(t, r.CostPerCorroboratedFindingUSD, "no corroborated findings => key absent, not 0.0")
```

This sets `FindingsCorroborated = 0` and `CostUSD = 0.5`. `costPer(0.5, 0)` returns nil. The key is omitted. ✓

But wait - does `Export` clamp `FindingsCorroborated`? If `FindingsCorroborated = 0` is set directly on the `Record` and `Export` reads it, does it go through ingestion clamping? Let me think - `Export` takes `[]Record` directly, not ingesting from files. So `rec.FindingsCorroborated = 0` is used as-is. `a.corroborated` accumulates to 0. `costPer(0.5, 0)` → nil. ✓

OK, let me look at one more thing. In the `reviewerAcc.add` method, does it clamp negative values? The TD says "clamps to 0 at ingestion" - but `Export` receives already-ingested records. If a test sets `FindingsCorroborated = -2` directly, does `Export` clamp it? 

Looking at the test `TestExport_ClampsNegativeMetrics`:
```go
rec.FindingsCorroborated = -2
```

If `Export` doesn't clamp, then `a.corroborated = -2`, and `costPer(-1.0, -2)` → `corroborated <= 0` → nil. So the field is nil regardless. The TD says "clamps to 0 at ingestion" but in this test, the record is constructed directly and passed to `Export`, not ingested. So the clamping might not happen here. But `costPer` treats `corroborated <= 0` as nil anyway, so the result is the same: nil.

But wait - what about `CorroborationRate`? `clampRate(ratio(a.corroborated, a.raisedTotal))` - if `a.corroborated = -2` and `a.raisedTotal = -5` (from `FindingsRaised = -5`), then `ratio(-2, -5)` = ... let me check the `ratio` function. I don't have it, but it probably divides. `-2 / -5 = 0.4`. Then `clampRate(0.4)` = 0.4. But the test asserts `assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)` and `assert.LessOrEqual(t, r.CorroborationRate, 1.0)`. 0.4 is in [0, 1]. ✓

But wait - if `FindingsRaised = -5` and it's NOT clamped, then `a.raisedTotal = -5`. `avgPerRun(-5, 1) = -5.0`. The test asserts `assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)`. -5.0 < 0.0, so this would FAIL. But the test was passing before (it's not a new test). So either:
1. `FindingsRaised` is clamped somewhere in `Export`, or
2. `avgPerRun` clamps, or
3. The test was failing before (unlikely since it's existing).

Let me look at `avgPerRun`:
```go
func avgPerRun(total, runs int) float64 {
    if runs <= 0 {
        return 0
    }
    return float64(total) / float64(runs)
}
```

No clamping. So if `total = -5` and `runs = 1`, returns -5.0. The test would fail on `assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)`.

Unless `a.raisedTotal` is clamped in `add()`. Let me check - I don't have the full `add` method, but the diff shows `func (a *reviewerAcc) add(r Record) {` at line 97. The method body isn't in the diff. But the test was passing before this PR, so `add` must clamp negative values, or `FindingsRaised` is clamped before reaching `add`.

Actually, the TD item says "FindingsCorroborated=-2 which clamps to 0 at ingestion". Maybe `Export` does clamp negative values. Or maybe `add` does. Either way, the test was passing before, so negative values are handled somewhere.

If `FindingsCorroborated = -2` clamps to 0, then `a.corroborated = 0`, and `costPer(anything, 0)` → nil. The `if r.CostPerCorroboratedFindingUSD != nil` guard means the assertion is dead code. This is the issue the TD filed.

OK, I think the main finding here is the dead path in `TestExport_ClampsNegativeMetrics`. Let me also check if there are any other issues.

Let me look at the `docs/scorecard.md` change:
```
| `cost_per_corroborated_finding_usd` | float | **omitempty** | Total cost ÷ corroborated findings. **Omitted entirely** when there are zero corroborated findings...
```

The type says `float` but the Go type is now `*float64`. In JSON, it's still a number (float) when present. The doc says `omitempty` which is the Go tag, not a JSON type. This is a doc convention, not a bug. The `survived_skeptic_rate` row also says `float` with `**omitempty**`, so this is consistent. ✓

Let me look at the `docs/benchmark.md` change:
```
| `cost_per_corroborated_finding_usd` | Recorded cost ÷ findings whose category matched an expected one. **Omitted from the JSON entirely** when zero findings matched an expected category — the metric is undefined, not `0.0` (a paid-but-uncorroborated reviewer must not read identically to a genuinely free one). Present as `0.0` only when matched findings exist and the provider reports no usage cost. |
```

This is accurate. ✓

Now let me look at the `.planning/technical-debt/README.md` change more carefully. The new TD item:

```
| U | [ ] | LOW | internal/scorecard/export_test.go:381 | TestExport_ClampsNegativeMetrics sets FindingsCorroborated=-2 which clamps to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil in this scenario and the nil-guard makes the GreaterOrEqual(0.0) assertion on that field dead code, never exercising negative-CostUSD clamping through the non-nil pointer path. | Add a second case (or separate test) with a positive FindingsCorroborated and negative CostUSD so the clamp is actually exercised through the non-nil pointer branch. | EDGE_CASES | 15 | execute-epic-independent |
```

The line citation is `internal/scorecard/export_test.go:381`. Let me check - in the diff, the `TestExport_ClampsNegativeMetrics` function is at `@@ -356,15 +377,17 @@`. So after the change, it starts around line 377. The `assert.GreaterOrEqual(t, r.CostPerCorroboratedFindingUSD, 0.0)` line that was changed would be around line 381-383 in the new file. Let me count:

The diff shows:
```
@@ -356,15 +377,17 @@ func TestExport_PreservesProviderPrefixedModel(t *testing.T) {
 func TestExport_ClampsNegativeMetrics(t *testing.T) {
 	rec := exportRec("bruce", "m", 1)
 	rec.FindingsRaised = -5
 	rec.FindingsCorroborated = -2
 	rec.CostUSD = -1.0
 	rec.LatencyMS = -100
 	data, err := Export([]Record{rec}, FilterOpts{Since: "30d"}, fixedExportNow)
 	require.NoError(t, err)
 	r := parseEnvelope(t, data).Reviewers[0]
 	assert.GreaterOrEqual(t, r.FindingsRaisedAvg, 0.0)
-	assert.GreaterOrEqual(t, r.CostPerCorroboratedFindingUSD, 0.0)
+	if r.CostPerCorroboratedFindingUSD != nil {
+		assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0)
+	}
 	assert.GreaterOrEqual(t, r.LatencyP50MS, int64(0))
 	assert.GreaterOrEqual(t, r.CorroborationRate, 0.0)
 	assert.LessOrEqual(t, r.CorroborationRate, 1.0)
 }
```

The `if r.CostPerCorroboratedFindingUSD != nil {` line is at approximately line 381 (377 + 4 lines into the function = 38