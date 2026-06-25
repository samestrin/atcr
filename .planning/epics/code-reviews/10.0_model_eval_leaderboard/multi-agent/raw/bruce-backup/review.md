<think>
Let me analyze this diff carefully to identify issues in the scope of Epic 10.0.

The sprint plan defines the scope as:
- T1: Export-format changes (in `internal/scorecard/export.go`)
- T2: `atcr benchmark` command (run/export/verify) in `cmd/atcr`
- T5: Survived-skeptic already wired - no-op
- T6: Docs (submission guide, benchmark README, privacy model)

Let me review each file:

1. **cmd/atcr/benchmark.go** - new file for `atcr benchmark` command
2. **cmd/atcr/benchmark_test.go** - tests for benchmark command
3. **cmd/atcr/leaderboard_test.go** - tests updated for new export format
4. **cmd/atcr/main.go** - added benchmark command
5. **cmd/atcr/main_test.go** - test updated for 16 subcommands
6. **docs/benchmark.md** - new documentation
7. **docs/scorecard.md** - updated documentation
8. **internal/benchmark/benchmark.go** - new internal package
9. **internal/benchmark/benchmark_test.go** - tests
10. **internal/benchmark/testdata/** - test fixtures
11. **internal/boundaries_test.go** - updated import allowlist
12. **internal/scorecard/export.go** - updated export format
13. **internal/scorecard/export_test.go** - updated tests
14. **internal/version/version.go** - new version package

Now let me look for issues:

### `internal/scorecard/export.go`

1. **Missing `nil` check for `FindingsRefuted`** - In the `add` method of `reviewerAcc`, there's:
```go
if r.FindingsVerified != nil {
    a.verified += clampNonNeg(*r.FindingsVerified)
    a.hasVerification = true
}
if r.FindingsRefuted != nil {
    a.refuted += clampNonNeg(*r.FindingsRefuted)
    a.hasVerification = true
}
```
But there's no check for `r.FindingsVerified != nil` before dereferencing it in the first block. If `FindingsVerified` is non-nil but `FindingsRefuted` is nil, this works. But if the record has `FindingsVerified = nil` and `FindingsRefuted = non-nil`, we still set `hasVerification = true` correctly. Actually, looking at the logic again - the `hasVerification` flag should be set whenever we have any verification data, which is handled correctly by checking each field independently.

However, I need to trace through `finalize()` more carefully. When we call `clampNonNeg(*r.FindingsVerified)`, if `r.FindingsVerified` is nil, this would panic. But the nil check prevents that. So the logic is sound - we only dereference after confirming it's not nil.

There's a potential issue though: if `FindingsVerified` is non-nil but `FindingsRefuted` is nil, we set `hasVerification = true` and accumulate the verified count. But then in `finalize()`, if we hit the case where `a.verified+a.refuted > 0` is true (because verified is positive), we correctly compute the ratio. The `nil` checks are in place to prevent panics when dereferencing optional fields.

The real concern is what happens if `FindingsVerified` is nil but `FindingsRefuted` is non-nil - we'd still set `hasVerification = true`, but then in `finalize()` we'd try to dereference `FindingsVerified` without checking if it's nil first. That's where the bug lies. nil pointer dereference in `finalize()` when `FindingsVerified` is nil but `FindingsRefuted` is non-nil, since the code unconditionally dereferences `*r.FindingsVerified` without a nil check. This is a correctness issue.

Looking at `AnonymizeRecord`, the same pattern appears where `r.FindingsVerified` is dereferenced directly in the conditional check without verifying it's non-nil first, though this particular function is only invoked when records pass through `ApplyFilters` which may have its own safeguards.

The `scrubField` function uses `strings.Builder` correctly without issues. In `cmd/atcr/benchmark.go`, the `runBenchmarkExport` function reads and validates the run-result file as expected.

There's a subtle issue with `medianInt64` for even-length slices—dividing the sum of two middle elements by 2 can cause integer overflow with very large latencies, though this is unlikely in practice and Go 1.17+ handles negative numbers differently in integer division. The diff path validation appears sound, and `benchmark.go` properly checks for missing suite metadata.

In `benchmark_test.go`, the test constructs a RunResult with a single reviewer and verifies the JSON output structure, including the submission schema version and source fields.  However, the test data includes `survived_skeptic_rate` but the assertion doesn't validate it appears in the output—though `RunResult` uses `scorecard.PublicRecord` which has `SurvivedSkepticRate` as `*float64` with `omitempty`, so it would only serialize if non-nil. Looking at the test data, neither the reviewer object nor the envelope includes this field, so the export correctly omits it.

There's an incomplete thought about a `TestBuildSubmission` in `benchmark_test.go` that's cut off.

I should also examine `isSafeRelPath` more carefully. The logic rejects absolute paths, then checks if the cleaned path equals ".." or starts with "..", but this might not catch all traversal cases like "../../file" that clean to "../..". Looking at the validation logic more carefully, it checks if the cleaned path equals ".." or starts with "..", but this misses cases like "foo/../../../etc/passwd" which would still be rejected by the filepath.Clean normalization. Testing the actual behavior: for "../../etc/passwd", after cleaning it becomes "../../etc/passwd" which fails the prefix check since it starts with "../.."; for "foo/../../../etc/passwd", the cleaned path is "../../../etc/passwd" which also fails the check. So the validation is actually working as intended.

One thing I'm noticing in `export.go` is that the reviewer accumulation logic checks if `r.FindingsVerified` is non-nil before dereferencing it to add to the verification count.

However, there's a potential issue with how the rate is calculated in the `finalize` method. When the code has verdict counts available, it computes the rate using `clampRate(ratio(a.verified, a.verified+a.refuted))`, but if both verified and refuted are zero, the denominator becomes zero, which could cause a division problem. The condition `a.verified+a.refuted > 0` should prevent this, but I need to verify the ratio function handles this edge case properly. The condition `a.verified+a.refuted > 0` prevents division by zero, so the logic handles the edge case. When verification data exists but verdict counts are zero, the fallback averages the stored rates. If there are no stored rates either, `len(a.storedRates)` is 0, which means the division `0.0 / 0.0` produces NaN. This NaN would then be passed to `clampRate`, which doesn't handle NaN values. The NaN propagates through and gets marshaled into the JSON as null. The fallback case can produce NaN when the stored rates are empty. This only occurs in a degenerate scenario where a record has `FindingsVerified` and `FindingsRefuted` as nil but `SurvivedSkepticRate` as a pointer to a value—the shape is malformed but permissible in the Record type. For this to actually occur, `r.SurvivedSkepticRate` must be non-nil while both `FindingsVerified` and `FindingsRefuted` are nil.

In that case, `a.verified` and `a.refuted` remain 0, triggering the fallback logic that accesses `a.storedRates`. If that slice is also empty, NaN gets returned, which seems like an actual bug in the code.

Looking at the initialization, `a.storedRates` starts as nil, and `append` should handle nil slices correctly. But I need to trace through what happens in the `add` method more carefully—specifically whether `SurvivedSkepticRate` is being set properly when only that field is non-nil.

The issue is that when `SurvivedSkepticRate` is present but the verification counts are missing, the rate gets added to `storedRates` and `hasVerification` is marked true. However, in the `finalize` method, the logic checks if verification counts exist first—if they don't, it falls back to using `storedRates`, but if that's also empty, we're returning NaN instead of a meaningful value.

The real question is whether this edge case can actually occur in practice. The `PublicRecord` struct defines `SurvivedSkepticRate` as a pointer, so nil means no verification ran while non-nil means it did. But when aggregating from multiple records, each with their own verification state, the combination creates scenarios where we might end up with verification data but no concrete counts to work from. If there's verification data, the rate calculation should always be computable—but if all records have nil `SurvivedSkepticRate`, then `storedRates` stays empty and `verified`/`refuted` remain 0, triggering the NaN bug. Looking at the `add` method, I notice the nil checks for `FindingsVerified` and `FindingsRefuted` are in place, but `SurvivedSkepticRate` only gets added to `storedRates` if it's non-nil, which is correct. The issue is that when these verification pointers are all nil, there's nothing to aggregate and the final rate calculation breaks. I need to understand what values are actually stored in the Record struct. The question is whether `FindingsVerified` and `FindingsRefuted` remain nil even after verification runs complete. Looking at Epic 3.0, these fields get populated when verification happens. So if `FindingsVerified` is nil, it means either no verification occurred, or the data got lost somewhere. The scenario where all three fields are nil but `SurvivedSkepticRate` is set suggests a potential data inconsistency that the code doesn't handle.

The NaN output is genuinely problematic—it would corrupt the JSON and break downstream consumers. This deserves a MEDIUM severity rating since it's an edge case that produces invalid data.

I should also examine whether the code handles negative latency values properly. The `add` method calls `clampNonNeg64` on the raw latency, which would silently convert negative values to zero without warning. While this prevents NaN, it could mask underlying issues with the data pipeline. Looking at the test expectations, negative values should be clamped but not cause panics. The real issue is that `medianInt64` receives a pre-clamped slice, so it never encounters negative values in practice—the defensive clamping happens upstream in `add`.

The NaN bug in `finalize()` is the more significant concern here. There are a few other points worth considering: the integer division for even-length medians is fine for typical values but could overflow with very large latencies, the filter-echoing removal is a deliberate change, and I need to verify whether the privacy documentation accurately reflects what `BuildSubmission` actually does with the run-result data.

Looking at `BuildSubmission` more closely, it wraps `rr.Reviewers` directly into the submission without re-scrubbing, which means it inherits the privacy properties of the input. The documentation correctly notes that scrubbing happens at the producer level rather than at export time, though it also flags this as technical debt worth addressing. The test data in `run-result.json` doesn't include `survived_skeptic_rate` fields, so those would naturally be omitted from the output as expected.

The test assertions cover the source, suite, and suite_version fields, but they don't validate whether `survived_skeptic_rate` should appear or be absent. Since the test fixtures don't provide verification data, the field should indeed be omitted when serializing to JSON.

Looking at the PublicRecord struct, `SurvivedSkepticRate` is a `*float64` with `omitempty`, so it only appears in the output if explicitly set. The reviewers array in the test data doesn't include this field, which means it correctly wouldn't be in the serialization. The real question is whether the test should be explicitly checking for the absence of `survived_skeptic_rate` rather than just verifying the fields that are present.

For the NaN bug in `finalize()`, it only triggers under very specific conditions—when verification data exists but no verdict counts are available and no stored rates exist. This edge case occurs if the scorecard stores `SurvivedSkepticRate` without populating the underlying `FindingsVerified` and `FindingsRefuted` fields, which would create an inconsistent state where the aggregate indicates verification occurred but there's no supporting data.

However, the code explicitly derives `SurvivedSkepticRate` from the ratio of verified to refuted findings, so the rate shouldn't exist independently of those counts. The only way this inconsistent state arises is through manual JSON construction or direct struct manipulation bypassing the normal derivation logic. The test suite validates the happy path but doesn't cover this malformed scenario.

The NaN issue could manifest in production if users manually edit their scorecard files or construct submissions externally, then publish them. While the happy path functions correctly, the defensive programming gap here warrants a MEDIUM severity rating.

In `cmd/atcr/benchmark.go`, the command definition appears sound with appropriate argument handling.

Looking at the `Load` function, it validates the manifest and then checks that each case's diff file exists via `os.Stat`. If the diff file isn't found, it returns an error. The `Validate` method only performs structural checks without filesystem access, which is the appropriate separation of concerns.

There's a potential edge case though—if `Diff` contains a path traversal attempt like `../../../etc/passwd`, `Validate` would catch it. But if the diff path is something like `./valid-case/../../../etc/passwd`, after `filepath.Clean` this becomes `../../../etc/passwd`, which would be rejected since it starts with `../..`. The `isSafeRelPath` check should handle these cases correctly.

Looking at the logic more carefully: `filepath.IsAbs` rejects absolute paths, and the second check catches anything that, when cleaned, starts with `..`—which means any attempt to escape the directory tree gets blocked. So the path validation appears sound.

I should also verify the `RunResult` struct definition to ensure the diff paths are being processed correctly throughout the flow. it wraps `rr.Reviewers` verbatim. The testdata confirms reviewers are pre-scrubbed, so the approach is sound. The only concrete issue I'm spotting is the NaN bug in `finalize()`.

The `scrubField` function in export.go has a loop that calls `replace` multiple times, which could be inefficient if there are many replacements needed.

Looking at the replacements themselves: it strips email patterns, Windows paths, Unix absolute paths, and credential-like patterns. These are all applied sequentially, which should handle the various sanitization cases adequately.

The only real bug I'm confident about is the NaN issue. Let me double-check the fallback logic in `finalize()` to make sure I understand it correctly.

The code has a switch statement that first checks if `verified + refuted > 0` to compute the rate from totals. If that's not available, it falls back to averaging the stored rates by summing them and dividing by the count. But if `storedRates` is empty when we reach this fallback, we'd be dividing by zero and getting NaN.

For this edge case to actually occur, we'd need `hasVerification` to be true (meaning at least one verification-related pointer was non-nil), while both `verified` and `refuted` stay zero (meaning no verdict count pointers were set). This could happen if `SurvivedSkepticRate` was set but the verdict counts weren't, creating that specific data inconsistency.

The NaN would then get marshaled to JSON as `null`, which means the key gets omitted entirely due to `omitempty`. So the practical impact is that users with this malformed data structure end up with the field missing from their submission rather than seeing an explicit NaN value.

I should also check whether `BenchmarkExport_ProducesSuiteTaggedJSON` actually validates the submission's correctness, particularly around the `submission_schema` field and other key attributes. `json:"submission_schema"` is correct. ✓

The `OutputFlag` test verifies that JSON gets written to the file rather than stdout, which is what we expect. ✓

The primary issue to address is in `internal/scorecard/export.go` around lines 103-110, where the NaN bug occurs. I should consider whether there are other MEDIUM severity concerns, but the NaN problem is the most concrete one I've identified. The submission schema looks correct across the envelope and benchmark packages, and the test data in `testdata/run-result.json` includes all the expected fields like suite, suite_version, generated_at, and reviewers.

I don't see HIGH severity issues in the diff. The key findings are: a NaN vulnerability in `finalize()` where missing verdict counts and stored rates cause division by zero, and a potential path traversal concern in `Load` that seems adequately mitigated by the `isSafeRelPath` validation. The main actionable issue is the NaN bug in `internal/scorecard/export.go`.
</think>

MEDIUM|internal/scorecard/export.go:103-110|NaN returned when verification data exists but verdict counts and stored rates are absent, producing invalid JSON|null check on storedRates before division|correctness|10|len(a.storedRates)==0 && verdict counts==0: 0.0/0.0 = NaN, propagates to JSON as null|bruce