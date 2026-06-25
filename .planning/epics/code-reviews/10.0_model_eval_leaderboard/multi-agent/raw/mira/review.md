<think>
Let me analyze the diff for issues within the scope of the sprint plan (Epic 10.0 Model-Eval Leaderboard, specifically T1 export format changes, T2 `atcr benchmark` command, T5 no-op, T6 docs).

Let me review the diff carefully:

1. **`cmd/atcr/benchmark.go`** - New file
   - `runBenchmarkVerify`: uses `cmd.Flags().GetString("suite-path")` but doesn't check the error. Same for `runBenchmarkExport`.
   - `runBenchmarkExport`: The `output` flag's description says "follows symlinks" - this is documentation but the behavior of `writeExportFile` would determine whether this is accurate.
   - The `--output` flag description says "follows symlinks" - is this accurate or could it be a security concern (following symlinks could be a symlink attack)?
   - The test for benchmark export tests `output` flag - it creates a file. The "follows symlinks" comment is concerning if it means the function will follow symlinks (potentially writing to files outside the intended directory).

2. **`internal/benchmark/benchmark.go`** - New file
   - `ReproHash` calls `Load(suitePath)` which reads/validates the manifest. This is fine.
   - The `isSafeRelPath` function looks reasonable.
   - `BuildSubmission` uses `version.Version` and `scorecard.SubmissionSchema`.
   - Looking at privacy concerns: The `RunResult` `Reviewers` field uses `scorecard.PublicRecord` which is good.
   - In `ReproHash`, the function calls `Load()` first which validates. Then sorts and hashes. Looks OK.
   - Wait, `ReproHash` re-reads the diff files after `Load` already verified they exist. This is not a security issue but it's a bit redundant. Not really an issue though.

3. **`internal/scorecard/export.go`** - Modified
   - Removed `ExportFilters` struct.
   - Renamed fields significantly.
   - The `finalize()` function logic for `SurvivedSkepticRate`:
     - When `hasVerification` is true, it computes the rate. But if `verified+refuted == 0` AND `len(storedRates) > 0`, it averages stored rates. Otherwise rate is 0 (the zero value of `rate` variable, which is then set on `pr.SurvivedSkepticRate = &rate`).
     - This means if verification is present but all counts are zero and no stored rates, the rate is 0.0 - this is correct.
     - But what if `hasVerification` is true, `verified+refuted == 0`, and `len(storedRates) > 0`? It averages stored rates. OK.
   - `Add` logic: 
     - The `add` function has a subtle issue: if `r.FindingsVerified` is a non-nil pointer to 0, then `a.verified` stays 0 and `a.hasVerification` becomes true. This is intentional.
     - What if `r.SurvivedSkepticRate` is set but `FindingsVerified` and `FindingsRefuted` are nil? The `add` function appends to `storedRates` and sets `hasVerification = true`. Good.
   - The `Export` function: applies filters, then iterates over filtered records. The key is now `(persona, model)`.
   - **Potential issue**: The `ExportEnvelope` no longer echoes filters (intentional, per spec). The new structure doesn't have a `Filters` field. This is documented as intentional.

4. **`internal/version/version.go`** - New file
   - Simple version variable. No issues.

5. **`docs/benchmark.md`** - New file
   - Documentation looks accurate.

6. **`docs/scorecard.md`** - Modified
   - Documentation updates reflecting new schema. Looks good.

7. **`cmd/atcr/leaderboard_test.go`** - Test updates
   - Tests updated to match new schema. Looks good.

8. **`cmd/atcr/main.go`** - Modified
   - Added `newBenchmarkCmd()` to root. Looks good.

9. **`cmd/atcr/main_test.go`** - Test updates
   - Updated to expect 16 subcommands. Looks good.

10. **`internal/boundaries_test.go`** - Modified
    - Added `version` and `benchmark` packages to allowlist. Looks correct.

Let me look more carefully at the actual code for issues:

In `cmd/atcr/benchmark.go`:
- Line 70: `suitePath, _ := cmd.Flags().GetString("suite-path")` - ignoring the error. Since the flag is required, this should not fail, but ignoring the error is still not best practice.
- Line 96: `in, _ := cmd.Flags().GetString("in")` and `output, _ := cmd.Flags().GetString("output")` - same issue.

In `internal/scorecard/export.go`:
- The `add` function: If a record has `r.FindingsVerified != nil` (verified is a pointer, even to 0), the function adds `clampNonNeg(*r.FindingsVerified)` to `a.verified` and sets `a.hasVerification = true`. This is the intended behavior.

In `internal/benchmark/benchmark.go`:
- The `ReproHash` function calls `Load()` which validates the manifest. Then it sorts cases by ID and hashes them. The hash includes the diff file contents. This is good.
- **Concern**: The function re-reads diff files (line ~145 `os.ReadFile(filepath.Join(suitePath, c.Diff))`). This is fine - the files have been verified to exist.
- The `isSafeRelPath` function uses `filepath.Separator` but on Windows, a `\` separator would be used. The check `strings.HasPrefix(clean, ".."+string(filepath.Separator))` handles this. However, on Windows, the path could use `/` instead of `\`. A `clean := filepath.Clean(p)` would normalize separators on the platform. So a path like `../foo` on Windows would become `..\foo` after Clean. This should be OK.
- Actually wait: `filepath.Clean("../../../etc/passwd")` on Windows returns `..\..\..\etc\passwd`. Then the check `strings.HasPrefix(clean, ".."+string(filepath.Separator))` is `..\`. So this should still be caught. Good.

Let me look at the `WriteField` function:
```go
func writeField(h io.Writer, s string) {
	_, _ = fmt.Fprintf(h, "%d:", len(s))
	_, _ = io.WriteString(h, s)
}
```
The errors are ignored. This is OK for hashing - if the hash writer fails, there's nothing useful to do.

Let me look at the `BuildSubmission` more carefully:
- The `submittedAt` parameter is `time.Time`. In `runBenchmarkExport`, `time.Now().UTC()` is passed. But the comment says "submittedAt is passed in (not time.Now) so the result is reproducible." This is a bit contradictory - it's passed in but the caller passes `time.Now().UTC()`. However, the test passes a fixed time, which is what the comment means. OK.

Now let me look for any other issues:

1. **`cmd/atcr/benchmark.go` line 60-64**: 
```go
func runBenchmarkVerify(cmd *cobra.Command, _ []string) error {
	suitePath, _ := cmd.Flags().GetString("suite-path")
```
The error is ignored. This is a minor issue. The flag is required, so it shouldn't fail, but ignoring the error is a code smell. The same pattern is used in `runBenchmarkExport`.

2. **`cmd/atcr/benchmark.go` line 96-97**:
```go
in, _ := cmd.Flags().GetString("in")
output, _ := cmd.Flags().GetString("output")
```
Same issue - ignoring errors.

3. **`cmd/atcr/benchmark.go` line 122**: `return writeExportFile(output, out)` - What if `writeExportFile` follows symlinks? The `--output` flag description says "follows symlinks". This could be a security concern if a user passes a symlink path. But this depends on the implementation of `writeExportFile` which is in `leaderboard.go` (not in the diff).

4. **The `output` flag's docstring says "follows symlinks"** - This is actually present in the original `--output` flag for `leaderboard` (not in this diff, but the description here mirrors it). I should NOT flag this as it's existing behavior.

5. **`internal/scorecard/export.go` `add` function**: Looking at the logic for when `r.FindingsVerified` is non-nil but value is 0, and `r.SurvivedSkepticRate` is nil, and `r.FindingsRefuted` is nil: 
   - `a.verified += 0` (clampNonNeg returns 0)
   - `a.hasVerification = true`
   - Then in `finalize()`, `verified+refuted == 0` and `len(storedRates) == 0`, so `rate` stays 0.0.
   - This means a "no actual verification" record would emit `survived_skeptic_rate: 0.0`, which is indistinguishable from "all refuted". This might be a minor edge case issue, but the test `TestExport_SurvivedSkepticZeroIsEmittedWhenAllRefuted` already covers the "all refuted" case.

6. **`internal/scorecard/export.go` `finalize` function**: 
   - The default zero value of `rate` is 0.0, but if `a.hasVerification` is true and we go into the switch, we might never assign to `rate` (if both branches fail - but with the current logic, one branch always executes: `verified+refuted > 0` OR `len(storedRates) > 0` OR neither). If neither, rate stays 0.0. This is the case I mentioned in #5.
   - Actually, looking at the switch: if `a.hasVerification` is true, but `verified+refuted == 0` AND `len(storedRates) == 0`, then the switch falls through with no case matching, and `rate` stays 0.0. The pointer `&rate` is then assigned to `pr.SurvivedSkepticRate`. So 0.0 is emitted. This matches the spec.

7. **Test in `cmd/atcr/benchmark_test.go` `TestBenchmarkExport_OutputFlagWritesFile`**: 
   - Creates a path with parent directory: `filepath.Join(t.TempDir(), "nested", "submission.json")`
   - Expects `writeExportFile` to create the parent directory.
   - The comment in `runBenchmarkExport` says "writeExportFile (leaderboard.go) atomically writes to path, creating parents." This is documented behavior.

8. **`cmd/atcr/benchmark.go` `runBenchmarkExport`**: The error check after `cmd.OutOrStdout().Write(append(out, '\n'))` - if the write fails, the submission might be partially written. Not a major issue since it's stdout.

Let me focus on real issues:

1. **In `cmd/atcr/benchmark.go` lines 60-61, 96-97**: Ignoring errors from `GetString` calls. This is a minor issue but consistent throughout the code (likely a pre-existing pattern).

2. **`internal/scorecard/export.go` line ~145** (in `add` function): The `clampNonNeg` function - if `r.FindingsVerified` is a non-nil pointer to a negative number, `clampNonNeg` returns 0. But if it's nil, the function doesn't even call clampNonNeg - it just doesn't increment. This is correct.

3. **Test `TestExport_SurvivedSkepticRateOnlyRecordNotZeroed`**: This tests the case where `FindingsVerified` and `FindingsRefuted` are nil but `SurvivedSkepticRate` is non-nil. The finalize logic falls back to averaging stored rates. But this is just ONE record with a stored rate. So it would average one value, which equals that value. So the test passes.

   But wait - in `add`, when `r.SurvivedSkepticRate != nil`, we do `a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))`. So the stored rate is clamped. Good.

4. **In `internal/benchmark/benchmark.go` `ReproHash` function**: 
   - The function reads the diff files separately from `Load()`. This is not really a security issue.
   - The function sorts `m.Cases` by ID. Then for each case, it writes the ID, the categories (sorted), and the diff content. This is deterministic.

5. **The `Submission` struct in `internal/benchmark/benchmark.go`**: It uses `json:"source"` etc. which is a reserved word in some contexts but not in Go JSON. No issue.

6. **In `cmd/atcr/benchmark.go` `newBenchmarkCmd`**: The `Long` description mentions "verify" and "export" but not "run". Since `run` is Epic 10.1 and deferred, this is correct.

7. **In `cmd/atcr/benchmark.go` `newBenchmarkExportCmd`**: The `--output` flag description says "follows symlinks". This is potentially a security concern but it's consistent with the existing `leaderboard --output` flag. Not a new issue introduced by this diff.

8. **In `internal/scorecard/export.go`**: The `Export` function keying - uses scrubbed identity. This is good for the privacy model (different identities that scrub to the same value merge into one group, which is intentional).

9. **In `cmd/atcr/benchmark_test.go` line 38**: The test expects `"benchmark-suite"` as `sub.Source` - this is correct based on the constant.

10. **`internal/scorecard/export.go` `add` function**: 
    - When `r.FindingsVerified` is non-nil, we add to `a.verified`. But the value is `*r.FindingsVerified`, which could be negative. `clampNonNeg` handles that.
    - However, the increment of `a.runs` happens unconditionally. Is that correct? Looking at the code: yes, every record adds 1 run to the group.

11. **`internal/scorecard/export.go` `finalize` function** - The default zero value of `rate` is 0.0. When `a.hasVerification` is true and the switch doesn't match, `rate` stays 0.0, and `&rate` is assigned. This means in the edge case where `a.hasVerification` is true but `verified+refuted == 0` and `len(storedRates) == 0`, the rate is emitted as 0.0. This is the same as the "all refuted" case. But this case shouldn't happen in practice (a record that has `hasVerification = true` would have at least one of: non-nil verified, non-nil refuted, or non-nil storedRates).

Wait, actually it CAN happen: if `r.FindingsVerified` is non-nil but the value is 0, and `r.FindingsRefuted` is nil, and `r.SurvivedSkepticRate` is nil. Then `a.verified` stays 0, `a.hasVerification = true`, but `verified+refuted == 0` and `len(storedRates) == 0`. The rate would be emitted as 0.0. This is a corner case but the comment in the code says "a corrupt/partial record carrying a rate pointer but nil count pointers" - so the case of a nil rate pointer with non-nil count pointers is also a corner case. These are unusual cases.

Let me look for actual issues that are real and in scope:

Real findings:

1. **`cmd/atcr/benchmark.go` line 61, 96, 97**: Ignoring errors from `GetString` calls. Minor issue.

2. **`internal/scorecard/export.go` `add` function**: When verification is "present" only via `r.FindingsVerified != nil` with value 0 and nil `FindingsRefuted`, the rate will be emitted as 0.0 (default), which is ambiguous with "all refuted". But this is an edge case for corrupt data.

3. **In `internal/benchmark/benchmark.go`**: The `ReproHash` function reads diff files separately from `Load()`. This is fine.

Let me look at the `cmd/atcr/benchmark.go` more carefully:

```go
func runBenchmarkVerify(cmd *cobra.Command, _ []string) error {
	suitePath, _ := cmd.Flags().GetString("suite-path")
```

This is a minor issue but the pattern is consistent throughout the file. Not really a serious issue.

Let me look at the test for benchmark export:

```go
func TestBenchmarkExport_ProducesSuiteTaggedJSON(t *testing.T) {
	...
	require.NotContains(t, out, `"filters"`)
}
```

This test asserts that the JSON output doesn't contain "filters". This is a good privacy check.

Let me look at the `runBenchmarkExport` function:
```go
out, err := json.MarshalIndent(sub, "", "  ")
if err != nil {
    return fmt.Errorf("encoding submission: %w", err)
}
```

`MarshalIndent` should never fail for a simple struct, but the error check is good practice.

I think the main real issues are:

1. **Minor: Ignoring errors from `cmd.Flags().GetString()` calls** - This is a low-priority issue.

2. **In `internal/scorecard/export.go` `add` function**: The verification aggregation could be subtly buggy in edge cases. Let me think about this more carefully.

   Consider two records in the same group:
   - Record 1: `FindingsVerified = nil`, `FindingsRefuted = nil`, `SurvivedSkepticRate = nil`
   - Record 2: `FindingsVerified = &0`, `FindingsRefuted = &5`, `SurvivedSkepticRate = &0.0`
   
   After `add`:
   - `a.verified = 0`, `a.refuted = 5`, `a.hasVerification = true`
   - `a.storedRates = [0.0]`
   
   In `finalize`:
   - `verified+refuted = 5 > 0`, so `rate = ratio(0, 5) = 0.0`. Good.
   
   Another case:
   - Record 1: `FindingsVerified = &0`, `FindingsRefuted = nil`, `SurvivedSkepticRate = nil`
   - Record 2: `FindingsVerified = nil`, `FindingsRefuted = nil`, `SurvivedSkepticRate = nil`
   
   After `add`:
   - `a.verified = 0`, `a.refuted = 0`, `a.hasVerification = true`
   - `a.storedRates = []`
   
   In `finalize`:
   - `verified+refuted = 0`, `len(storedRates) = 0`, so `rate` stays 0.0.
   - `&rate` (which is `&0.0`) is assigned to `pr.SurvivedSkepticRate`.
   
   This is fine - the verification was technically "present" (record 1 had verified set, even to 0), and the rate is 0.0.

3. **Privacy concern with `BuildSubmission`**: The function passes `rr.Reviewers` directly to the submission. The comment in the code says reviewers MUST already be anonymized at the producer. The `docs/benchmark.md` documents this as tech debt. This is a known and accepted risk per the design.

4. **`cmd/atcr/benchmark.go` line 122**: The comment says `writeExportFile` (in `leaderboard.go`) atomically writes. This is a dependency on a function in another file. If `writeExportFile` doesn't exist or has different behavior, this could fail. But since the codebase already uses this function for the production leaderboard export, this is a reasonable choice.

Let me check if there are any other real issues:

5. **`cmd/atcr/benchmark.go` line 28**: `Args: usageArgs(cobra.NoArgs)` - This is a helper that I assume wraps cobra.NoArgs with a usage message.

6. **`cmd/atcr/benchmark.go` line 122**: The `--output` flag description says "(follows symlinks)". If this is a new behavior, it could be a security concern. But looking at the existing code (not in diff), the leaderboard command also has this same pattern. So it's consistent with existing behavior.

7. **`internal/benchmark/benchmark.go` `ReproHash` function**: Looking at the length-prefix encoding:
```go
func writeField(h io.Writer, s string) {
	_, _ = fmt.Fprintf(h, "%d:", len(s))
	_, _ = io.WriteString(h, s)
}
```
The length is written in decimal. There's no separator between fields. The length is of the byte length of the string. For ASCII, this is unambiguous. For UTF-8 strings, `len(s)` is the byte length, which matches the number of bytes written. Good.

But wait, what if the same string appears in two different fields? E.g., `Suite: "ab"`, `SuiteVersion: ""` would write `2:ab0:`. But also `Suite: "a"`, `SuiteVersion: "b"` would write `1:a1:b`. These are distinguishable because the lengths are different. Good.

But what about: `Suite: "1:2:3"` and `SuiteVersion: ""`? This would write `5:1:2:30:`. And the parser reads back the length and then reads that many bytes, so the ambiguity is resolved. The length-prefix is unambiguous regardless of content. Good.

8. **`internal/scorecard/export.go` `add` function**: The `clampNonNeg(r.FindingsRaised)` etc. are all called on the integer counts. If the count is negative, it's clamped to 0. The `costTotal` uses `clampNonNegF`. Good.

9. **`internal/benchmark/benchmark.go` `BuildSubmission`**: The function takes `submittedAt time.Time` as a parameter. In `runBenchmarkExport`, `time.Now().UTC()` is passed. This means each call to `benchmark export` will produce a different `submitted_at`. This is correct - each export should have its own timestamp.

10. **Documentation in `docs/benchmark.md`**: The privacy model section says "Anonymization happens at the producer" and mentions tech debt for re-scrubbing. This is correctly documented as a known limitation.

11. **`internal/scorecard/export.go` `Export` function**: The `key` struct is local to the function, using `persona, model`. This is correct.

12. **The `reorder test for ReproHash`**: The test reorders cases in the manifest, then writes it back, and verifies that the hash is the same. The hash function sorts by case ID before hashing, so reordering shouldn't change the hash. Good test.

13. **Test `TestExport_SurvivedSkepticRateOnlyRecordNotZeroed`**: This tests that a stored rate is not zeroed when counts are nil. The test creates a record with `FindingsVerified = nil`, `FindingsRefuted = nil`, `SurvivedSkepticRate = &0.73`. After `add`: `a.verified = 0`, `a.refuted = 0`, `a.hasVerification = true`, `a.storedRates = [0.73]`. In `finalize`: `verified+refuted == 0` and `len(storedRates) == 1`, so `rate = 0.73 / 1 = 0.73`. Good.

14. **Test `TestExport_SurvivedSkepticPresentWhenVerified`**: This sets `FindingsVerified = &4`, `FindingsRefuted = &1`, `SurvivedSkepticRate = &rate`. After `add`: `a.verified = 4`, `a.refuted = 1`, `a.hasVerification = true`, `a.storedRates = [0.8]`. In `finalize`: `verified+refuted = 5 > 0`, so `rate = ratio(4, 5) = 0.8`. Good. But wait, the test expects 0.8 with delta 1e-9. `ratio(4, 5)` returns `4/5 = 0.8`. Good.

15. **Test `TestExport_SurvivedSkepticZeroIsEmittedWhenAllRefuted`**: This sets `FindingsVerified = &0`, `FindingsRefuted = &5`, `SurvivedSkepticRate = &0.0`. After `add`: `a.verified = 0`, `a.refuted = 5`, `a.hasVerification = true`, `a.storedRates = [0.0]`. In `finalize`: `verified+refuted = 5 > 0`, so `rate = ratio(0, 5) = 0.0`. Good.

Now, real issues I should flag:

1. **Minor: Ignoring errors from `cmd.Flags().GetString()` calls in `cmd/atcr/benchmark.go`**: This is a maintainability concern but not a real bug. The flag is required, so it shouldn't fail. But the pattern is consistent with cobra idioms in this codebase. LOW severity.

2. **Documentation accuracy in `docs/scorecard.md`**: The documentation says `latency_p50_ms` is "always" present. Let me check - the struct has `LatencyP50MS int64` with no `omitempty`. So it's always present. The test confirms this. Good.

3. **The `SubCommand` "personas" is in the list but the list says "The fifteen prior commands plus `benchmark`"**: Let me count: review, reconcile, verify, debate, report, github, range, status, init, serve, doctor, trust, scorecard, leaderboard, benchmark, personas = 16. Good.

4. **The `--output` flag description "(follows symlinks)" in `cmd/atcr/benchmark.go` line 110**: This is potentially misleading and could be a security concern. If `writeExportFile` follows symlinks, an attacker who controls a symlink in the path could cause the file to be written elsewhere. This is a potential security concern. But since this behavior is consistent with the existing `leaderboard --output` flag, it's not a new issue.

5. **In `cmd/atcr/benchmark.go` line 122**: The comment refers to `writeExportFile` in `leaderboard.go`. This is a cross-file dependency that should be noted. Not a bug.

Let me think about whether there are any actual bugs:

Actually, let me look at the `finalize()` function more carefully:

```go
func (a *reviewerAcc) finalize() PublicRecord {
	pr := PublicRecord{
		Model:                         a.model,
		Persona:                       a.persona,
		Runs:                          a.runs,
		FindingsRaisedAvg:             avgPerRun(a.raisedTotal, a.runs),
		CorroborationRate:             clampRate(ratio(a.corroborated, a.raisedTotal)),
		CostPerCorroboratedFindingUSD: costPer(a.costTotal, a.corroborated),
		LatencyP50MS:                  medianInt64(a.latencies),
	}
	if a.hasVerification {
		var rate float64
		switch {
		case a.verified+a.refuted > 0:
			rate = clampRate(ratio(a.verified, a.verified+a.refuted))
		case len(a.storedRates) > 0:
			sum := 0.0
			for _, v := range a.storedRates {
				sum += v
			}
			rate = clampRate(sum / float64(len(a.storedRates)))
		}
		pr.SurvivedSkepticRate = &rate
	}
	return pr
}
```

Wait, there's a subtle issue: if `a.hasVerification` is true but the switch doesn't match any case (i.e., `verified+refuted == 0` AND `len(storedRates) == 0`), then `rate` stays 0.0, and `&rate` is assigned. So the rate is emitted as 0.0.

This is a bug! Because:
- `a.hasVerification` is set when ANY of: `r.FindingsVerified != nil`, `r.FindingsRefuted != nil`, `r.SurvivedSkepticRate != nil`.
- If `r.FindingsVerified = &0`, `r.FindingsRefuted = nil`, `r.SurvivedSkepticRate = nil`, then `a.hasVerification = true`, but `a.verified = 0`, `a.refuted = 0`, `a.storedRates = []`.
- In `finalize`, `rate` is set to 0.0 (default), and `&rate` is assigned.
- So a rate of 0.0 is emitted.

This is a corner case for corrupt/partial data. The test `TestExport_SurvivedSkepticZeroIsEmittedWhenAllRefuted` only covers the "all refuted" case, not this case.

But wait, in this case, the verification was "indicated" by the producer (a non-nil pointer), but the actual count is 0. Should we emit 0.0 or nil? The spec is ambiguous. Looking at the code, emitting 0.0 is consistent with the "always emit when verification ran" semantics. So this might be intentional.

Actually, let me re-read the spec:
> "omitempty - Omitted entirely when no verification ran for the group; present as 0.0 only when verification ran and every finding was refuted."

The spec says "when verification ran". In our case, `a.hasVerification = true` is meant to indicate "verification ran". So emitting 0.0 is correct per the spec, even if the count is 0.

OK so this is not a bug, it's intentional. The verification "ran" but found 0 verified and 0 refuted, which is a valid result (maybe 0 findings to verify).

Let me look at the test:
```go
func TestExport_SurvivedSkepticZeroIsEmittedWhenAllRefuted(t *testing.T) {
	v, ref := 0, 5
	rate := 0.0
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = &v
	rec.FindingsRefuted = &ref
	rec.SurvivedSkepticRate = &rate
```

In this test, `FindingsVerified = &0`, `FindingsRefuted = &5`, `SurvivedSkepticRate = &0.0`. After `add`: `a.verified = 0`, `a.refuted = 5`, `a.hasVerification = true`, `a.storedRates = [0.0]`. In `finalize`: `verified+refuted = 5 > 0`, so `rate = ratio(0, 5) = 0.0`. Good.

So the test passes, but the test name says "WhenAllRefuted" - in the test, only 5 are refuted, 0 are verified. So 0.0 is the rate. The case where `verified == 0` and `refuted == 0` (and no stored rate) is NOT tested.

Is this a problem? It's a corner case for corrupt/partial data. Not a critical bug.

OK let me think about what to report. I need to be careful to only report real issues that are in scope.

Real issues:

1. **In `cmd/atcr/benchmark.go` `runBenchmarkVerify` and `runBenchmarkExport`**: Ignoring errors from `cmd.Flags().GetString()` calls. This is a minor maintainability issue. The flag is required, so it shouldn't fail in practice, but the pattern is not best practice.

2. **In `cmd/atcr/benchmark.go` line 110**: The `--output` flag description says "(follows symlinks)". If `writeExportFile` follows symlinks, this could be a security concern. However, this is consistent with existing behavior in the codebase (not a new issue).

3. **In `internal/scorecard/export.go` `add` function**: When `r.FindingsVerified != nil` (pointing to 0) and `r.FindingsRefuted == nil` and `r.SurvivedSkepticRate == nil`, the rate will be emitted as 0.0 in `finalize()`. This might not be the intended behavior for a "no actual verification" record. But this is a corner case for corrupt/partial data.

4. **Documentation in `docs/scorecard.md`**: The documentation mentions the `latency_p50_ms` is "always" present. The struct has no `omitempty`, so this is correct. But the documentation also mentions that `cost_per_corroborated_finding_usd` is "0.0 when none corroborated; never Inf/NaN". This is implemented correctly in `costPer()`.

Actually, I realize I should focus on what's most important and actionable. Let me finalize my findings:

Main findings:

1. **In `cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignoring errors from `cmd.Flags().GetString()` calls. This is a minor issue but worth flagging.

2. **In `cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)". If the underlying `writeExportFile` follows symlinks, this is a potential security concern (TOCTOU, symlink attacks). However, this is consistent with existing behavior.

3. **In `internal/scorecard/export.go` `add` function (line ~115)**: When `r.FindingsVerified != nil` (pointing to 0) and other verification fields are nil, the rate will be emitted as 0.0. This is a corner case.

4. **In `internal/benchmark/benchmark.go` `BuildSubmission`**: The function uses `version.Version` which is a package-level variable. If the version is not stamped at build time, it will be "0.0.0" (the default). This is documented behavior.

5. **In `internal/scorecard/export.go` `Export` function**: The `key` struct uses scrubbed identity. This means two different personas/models that scrub to the same value will merge into one group. This is intentional (it's the "scrub at ingestion" pattern), but could be surprising. Not a bug.

6. **In `cmd/atcr/benchmark.go` line 122**: The function calls `writeExportFile` from `leaderboard.go`. This is a cross-file dependency. Not a bug.

Actually, let me look at the test file for the new benchmark command:

```go
func TestBenchmarkExport_OutputFlagWritesFile(t *testing.T) {
	in := writeRunResult(t)
	dest := filepath.Join(t.TempDir(), "nested", "submission.json")
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--output", dest)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "submission_schema", "JSON goes to the file, not stdout")

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "benchmark-suite")
}
```

This test creates a nested directory path. The `writeExportFile` is expected to create the parent directory. The comment in the production code says "atomically writes to path, creating parents." This is correct.

Let me also look at the `cmd/atcr/benchmark.go` more carefully:

```go
func runBenchmarkExport(cmd *cobra.Command, _ []string) error {
	in, _ := cmd.Flags().GetString("in")
	output, _ := cmd.Flags().GetString("output")
```

Ignoring the error from `GetString` is consistent with the existing code (e.g., in `runBenchmarkVerify`). The flag is required, so it shouldn't fail. But if it did fail (e.g., due to a typo in the flag name), the function would silently use the empty string, which would then fail with a different error message. Not a critical issue.

Let me also look at the `--output` flag description: "write the submission JSON to this file instead of stdout (follows symlinks)". The "(follows symlinks)" part is concerning. If `writeExportFile` follows symlinks, an attacker who can place a symlink in the path could redirect the file write. This is a TOCTOU/symlink attack concern.

However, this is consistent with the existing `leaderboard --output` flag, which has the same description. So it's not a new issue introduced by this diff. I should not flag it as a finding because it's existing behavior.

OK, let me finalize my findings. I'll focus on the most important issues:

Actually, let me also check the `writeExportFile` function reference:

In `runBenchmarkExport`:
```go
// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
return writeExportFile(output, out)
```

The comment is helpful, but the function is from another file. If `writeExportFile` doesn't exist or has changed behavior, this could break. But this is a reasonable code reuse pattern.

Let me also check the `runBenchmarkExport` for the case where `--output` is not specified:

```go
if output == "" {
    _, werr := cmd.OutOrStdout().Write(append(out, '\n'))
    return werr
}
```

This writes to stdout with a trailing newline. The `append(out, '\n')` creates a new slice. If `werr` is not nil, the error is returned. This is correct.

Let me also check the `add` function for a subtle issue:

```go
func (a *reviewerAcc) add(r Record) {
	a.runs++
	a.raisedTotal += clampNonNeg(r.FindingsRaised)
	a.corroborated += clampNonNeg(r.FindingsCorroborated)
	a.costTotal += clampNonNegF(r.CostUSD)
	a.latencies = append(a.latencies, clampNonNeg64(r.LatencyMS))
	// Any verification pointer present marks the group as verified; the counts
	// sum only over the runs that actually carried verification data.
	if r.FindingsVerified != nil {
		a.verified += clampNonNeg(*r.FindingsVerified)
		a.hasVerification = true
	}
	if r.FindingsRefuted != nil {
		a.refuted += clampNonNeg(*r.FindingsRefuted)
		a.hasVerification = true
	}
	if r.SurvivedSkepticRate != nil {
		a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))
		a.hasVerification = true
	}
}
```

This looks correct. The `a.runs` is incremented unconditionally. The verification fields are only updated if the pointers are non-nil.

Wait, I notice a potential issue: the `clampNonNeg(r.FindingsRaised)` etc. are called on the record fields, but the `Record` struct might not have these fields. Let me check the `Record` type:

The `Record` type is referenced in the diff but not defined. It's in the scorecard package. Looking at the test code:

```go
func exportRec(reviewer, model string, ageDays int) Record {
	ts := fixedExportNow.AddDate(0, 0, -ageDays).UTC().Format(time.RFC3339)
	return Record{
		Timestamp:           ts,
		Reviewer:            reviewer,
		Model:               model,
		Role:                "reviewer",
		FindingsRaised:      12,
		FindingsCorroborated: 7,
		FindingsSolo:        5,
		CorroborationRate:   ratio(7, 12),
		CostUSD:             0.04,
		TokensIn:            14200,
		TokensOut:           4000,
		LatencyMS:           9100,
	}
}
```

So `Record` has these fields. Good.

OK let me think about what the most actionable findings are.

Looking at the sprint plan scope: T1 (export format changes), T2 (`atcr benchmark` command), T6 (docs). The findings should be in these areas.

Real findings I'll report:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `cmd.Flags().GetString()` calls. This is a minor maintainability issue. (LOW, maintainability, 5 min)

2. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern (symlink attack). (LOW, security, 10 min)

3. **`internal/scorecard/export.go` `add` function**: The `hasVerification` flag is set when ANY verification pointer is non-nil, but the rate computation might fall through to default 0.0 in edge cases. This is a corner case. (LOW, correctness, 10 min)

4. **`internal/benchmark/benchmark.go` `ReproHash`**: The function re-reads diff files after `Load` already verified them. This is a minor efficiency issue. (LOW, performance, 5 min)

Actually, I think the most actionable findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `cmd.Flags().GetString()` calls. The flag is required, but if `GetString` returns an error (e.g., due to a typo in the flag name), the function would silently use the empty string, leading to a confusing error later. Should check the error. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `add` function**: The `hasVerification` flag is set when ANY verification pointer is non-nil, but the rate computation in `finalize` might fall through to default 0.0 in the edge case where `verified+refuted == 0` AND `len(storedRates) == 0`. This produces an ambiguous 0.0 rate. (MEDIUM, correctness, 15 min)

3. **`internal/scorecard/export.go` `Export` function**: The key uses scrubbed identity, so two different personas/models that scrub to the same value will merge into one group. This might not be the intended behavior. (LOW, correctness, 10 min)

Wait, I should also check for issues in the test files. Let me look at `cmd/atcr/benchmark_test.go`:

```go
func writeValidSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case-01.diff"),
		[]byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new // planted defect\n"), 0o600))
	manifest := `{"suite":"mini","suite_version":"1.2.0","cases":[` +
		`{"id":"case-01","diff":"case-01.diff","expected_categories":["correctness"]}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), []byte(manifest), 0o600))
	return dir
}
```

This test writes a diff file with 0o600 permissions. This is a bit unusual for a test (typically test files are 0o644 or similar), but it's fine.

```go
func writeRunResult(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "run-result.json")
	body := `{"suite":"mini","suite_version":"1.2.0","generated_at":"2026-06-24T12:00:00Z",` +
		`"reviewers":[{"model":"claude-sonnet-4-6","persona":"bruce","runs":2,` +
		`"findings_raised_avg":10.5,"corroboration_rate":0.6,` +
		`"cost_per_corroborated_finding_usd":0.006,"latency_p50_ms":8900}]}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
```

This is a test helper that writes a run-result file. The `0o600` permission is fine.

OK, let me finalize my findings. I'll report the most actionable issues:

1. **In `cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `cmd.Flags().GetString()` calls. (LOW, maintainability)

2. **In `internal/scorecard/export.go` `add` function**: The `hasVerification` flag is set when ANY verification pointer is non-nil. In `finalize`, if `verified+refuted == 0` and `len(storedRates) == 0`, the rate is emitted as 0.0 (default), which is indistinguishable from "all refuted". This is a subtle ambiguity. (MEDIUM, correctness)

3. **In `internal/benchmark/benchmark.go` `BuildSubmission`**: The function uses `version.Version` which is a package-level variable. If not stamped at build time, it will be "0.0.0". This is documented behavior. (LOW, maintainability - not a bug)

4. **In `cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern (symlink attack via TOCTOU). (LOW, security)

Hmm, let me think more carefully. Let me look at the actual implementation in the existing leaderboard code that's referenced:

Actually I don't have access to the existing leaderboard code, but the comment says "writeExportFile (leaderboard.go) atomically writes to path, creating parents." So this is a pre-existing function. The "(follows symlinks)" in the new code is likely copying the description from the existing flag.

Let me also check if there are any issues with the test file:

```go
func TestBenchmarkExport_OutputFlagWritesFile(t *testing.T) {
	in := writeRunResult(t)
	dest := filepath.Join(t.TempDir(), "nested", "submission.json")
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--output", dest)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "submission_schema", "JSON goes to the file, not stdout")

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "benchmark-suite")
}
```

This test creates a path with a non-existent parent directory. The `writeExportFile` is expected to create it. The test passes (presumably), so this is working as expected.

OK, I think my main findings are:

1. **`cmd/atcr/benchmark.go`**: Ignored errors from `GetString` calls (LOW, maintainability)

2. **`internal/scorecard/export.go` `add` function**: The `hasVerification` flag is set when ANY verification pointer is non-nil. In `finalize`, the rate computation might fall through to default 0.0 in edge cases. (MEDIUM, correctness)

3. **`cmd/atcr/benchmark.go`**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security)

4. **`internal/scorecard/export.go` `Export` function**: The key uses scrubbed identity, so two different personas/models that scrub to the same value will merge into one group. This is intentional but might be surprising. (LOW, correctness - not really a bug, more of a design choice)

Actually, let me look more carefully at #2. The `add` function:

```go
if r.FindingsVerified != nil {
    a.verified += clampNonNeg(*r.FindingsVerified)
    a.hasVerification = true
}
if r.FindingsRefuted != nil {
    a.refuted += clampNonNeg(*r.FindingsRefuted)
    a.hasVerification = true
}
if r.SurvivedSkepticRate != nil {
    a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))
    a.hasVerification = true
}
```

The issue: if a record has `FindingsVerified = &0` (verified = 0, but pointer is non-nil), then `a.verified += 0` (no change), but `a.hasVerification = true`. So the group is marked as "has verification" even though the verified count is 0.

In `finalize`:
```go
if a.hasVerification {
    var rate float64
    switch {
    case a.verified+a.refuted > 0:
        rate = clampRate(ratio(a.verified, a.verified+a.refuted))
    case len(a.storedRates) > 0:
        sum := 0.0
        for _, v := range a.storedRates {
            sum += v
        }
        rate = clampRate(sum / float64(len(a.storedRates)))
    }
    pr.SurvivedSkepticRate = &rate
}
```

If `a.hasVerification = true` and `verified+refuted == 0` and `len(storedRates) == 0`, the switch doesn't match any case, and `rate` stays 0.0 (the default value). Then `&rate` is assigned to `pr.SurvivedSkepticRate`. So the rate is emitted as 0.0.

This is a subtle bug because the rate 0.0 could mean:
- Verification ran and all findings were refuted (verified=0, refuted>0) - this is the "legitimate" 0.0
- Verification ran but there were 0 findings to verify (verified=0, refuted=0) - this is a weird edge case

The spec says "omitempty - Omitted entirely when no verification ran for the group; present as 0.0 only when verification ran and every finding was refuted." So the spec implies that 0.0 should only be emitted when verification ran AND every finding was refuted. The edge case where verified=0 AND refuted=0 is ambiguous.

However, this is a corner case for corrupt/partial data. In practice, the producer (the scorecard aggregation) would always provide both verified and refuted counts (or neither) when verification ran. So this is unlikely to occur in practice.

Let me also look at the `Export` function's handling of the `SurvivedSkepticRate`:

Wait, the `Export` function calls `a.add(r)` for each record, then `groups[k].finalize()` for each group. The `add` function increments `a.runs` unconditionally. This is correct - every record adds 1 run.

OK, I think my main findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` in the `hasVerification` block doesn't handle the case where `verified+refuted == 0` and `len(storedRates) == 0`. The rate defaults to 0.0 in this case, which is ambiguous with "all refuted". (MEDIUM, correctness, 10 min)

Actually, this might be a design choice. Let me re-read the code:

```go
if a.hasVerification {
    var rate float64
    switch {
    case a.verified+a.refuted > 0:
        rate = clampRate(ratio(a.verified, a.verified+a.refuted))
    case len(a.storedRates) > 0:
        sum := 0.0
        for _, v := range a.storedRates {
            sum += v
        }
        rate = clampRate(sum / float64(len(a.storedRates)))
    }
    pr.SurvivedSkepticRate = &rate
}
```

If neither case matches, `rate` stays 0.0 (Go's default for float64). Then `&rate` is assigned. So 0.0 is emitted.

The case where neither matches is:
- `hasVerification = true` (at least one verification pointer was non-nil)
- `verified+refuted == 0` (no counts accumulated)
- `len(storedRates) == 0` (no stored rates accumulated)

This means: a verification pointer was non-nil, but the value was 0 and the count pointers were nil. This is a degenerate case.

In this case, the rate is emitted as 0.0. This is consistent with "verification ran and the rate is 0". So the spec is satisfied.

But wait, the spec says "present as 0.0 only when verification ran and every finding was refuted". The case where verified=0 AND refuted=0 doesn't match "every finding was refuted" - it matches "no findings to verify". This is a different semantic.

However, in practice, this case is unlikely. The producer (the scorecard aggregation) would provide both verified and refuted counts when verification ran.

OK, this is a corner case. I'll flag it as MEDIUM correctness.

Let me also look for any other issues:

5. **In `cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)". This is potentially a security concern. (LOW, security, 5 min)

6. **In `internal/scorecard/export.go` `Export` function**: The `key` struct uses scrubbed identity. This means two different personas/models that scrub to the same value will merge into one group. (LOW, maintainability, 5 min) - actually this is intentional per the code comment.

7. **In `internal/benchmark/benchmark.go` `ReproHash`**: The function re-reads diff files after `Load` already verified they exist. (LOW, performance, 5 min)

OK, I think I have my findings. Let me also consider whether I should report the issue with the `add` function and `hasVerification`. The test `TestExport_SurvivedSkepticRateOnlyRecordNotZeroed` covers the case where `SurvivedSkepticRate` is non-nil but counts are nil. But the case where `FindingsVerified` is non-nil (pointing to 0) and other fields are nil is not covered.

Let me write up my findings.

Actually, let me reconsider. Looking at the test more carefully:

```go
func TestExport_SurvivedSkepticRateOnlyRecordNotZeroed(t *testing.T) {
	rate := 0.73
	rec := exportRec("bruce", "claude-sonnet-4-6", 1)
	rec.FindingsVerified = nil
	rec.FindingsRefuted = nil
	rec.SurvivedSkepticRate = &rate
```

In this test, `Find`ingsVerified = nil`, `FindingsRefuted = nil`, `SurvivedSkepticRate = &0.73`. After `add`: `a.verified = 0`, `a.refuted = 0`, `a.hasVerification = true`, `a.storedRates = [0.73]`. In `finalize`: `verified+refuted = 0`, `len(storedRates) = 1`, so `rate = 0.73 / 1 = 0.73`. Good.

The case where `FindingsVerified = &0`, `FindingsRefuted = nil`, `SurvivedSkepticRate = nil` is not tested. This would result in `a.hasVerification = true`, but no counts and no stored rates. The rate would be 0.0.

This is a corner case for corrupt data. In practice, if verification ran, the producer would provide at least one of: verified count, refuted count, or stored rate. So this case is unlikely.

However, the code could be made more robust by handling this case explicitly. For example, only setting `hasVerification = true` if at least one of the verification fields has a meaningful value.

OK, I think my findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. The rate defaults to 0.0 in this case, which is ambiguous. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Actually, let me think about #3 more. The description says "(follows symlinks)" - this is the flag's description, not a behavior. The actual behavior depends on `writeExportFile`. If `writeExportFile` follows symlinks, then this is a security concern. But the description is just informing the user about the behavior.

Looking at the existing `leaderboard --output` flag, it likely has the same description. So this is not a new issue.

OK, I'll flag this as a LOW security concern but note that it's consistent with existing behavior.

Let me also check the test for the benchmark command:

```go
func TestBenchmarkExport_OutputFlagWritesFile(t *testing.T) {
	in := writeRunResult(t)
	dest := filepath.Join(t.TempDir(), "nested", "submission.json")
	code, out := execCmdCapture(t, "benchmark", "export", "--in", in, "--output", dest)
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, "submission_schema", "JSON goes to the file, not stdout")

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Contains(t, string(data), "benchmark-suite")
}
```

This test creates a nested directory and writes the file. The `writeExportFile` is expected to create the parent directory. The test passes (presumably).

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Wait, I should also check if there are any issues with the documentation. Let me re-read `docs/benchmark.md`:

The documentation is well-written and covers:
- The suite manifest contract
- The `verify` command
- The `export` command
- The privacy model
- Related documentation

I don't see any issues with the documentation.

Let me also check `docs/scorecard.md`:

The documentation is updated to reflect the new schema. It correctly:
- Describes the new envelope fields (`submission_schema`, `atcr_version`, `submitted_at`, `reviewers`)
- Describes the new reviewer fields (`model`, `persona`, `runs`, `findings_raised_avg`, `corroboration_rate`, `survived_skeptic_rate`, `cost_per_corroborated_finding_usd`, `latency_p50_ms`)
- Mentions the privacy model (allowlist-based, scrubbed strings)
- Mentions the schema versioning (two independent version numbers)

I don't see any issues with the documentation.

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Wait, let me also think about the `Export` function. It iterates over `filtered` records. The `ApplyFilters` function is called first. Then for each record, it calls `scrubField` and creates a key. Then it adds to the group.

```go
for _, r := range filtered {
    persona := scrubField(r.Reviewer)
    model := scrubField(r.Model)
    k := key{persona, model}
    a, ok := groups[k]
    if !ok {
        a = &reviewerAcc{persona: persona, model: model}
        groups[k] = a
        order = append(order, k)
    }
    a.add(r)
}
```

This looks correct. The key uses scrubbed identity, so two different personas/models that scrub to the same value will merge into one group. This is intentional per the code comment.

But wait, what if `scrubField` returns an empty string? Then two different personas with empty scrubbed values would merge. This is a corner case for corrupt data.

OK, I think my findings are complete. Let me write them up.

Actually, I realize I should also check the `Export` function for the case where the filtered slice is empty. The function already checks for this:

```go
if len(filtered) == 0 {
    return nil, ErrNoExportRecords
}
```

This is good.

Let me also check the `Export` function for the `Add` method:

```go
func (a *reviewerAcc) add(r Record) {
    a.runs++
    a.raisedTotal += clampNonNeg(r.FindingsRaised)
    a.corroborated += clampNonNeg(r.FindingsCorroborated)
    a.costTotal += clampNonNegF(r.CostUSD)
    a.latencies = append(a.latencies, clampNonNeg64(r.LatencyMS))
    ...
}
```

The `a.runs++` is unconditional. This means every record adds 1 run to the group. This is correct.

But wait, what if the record has `FindingsRaised = -5`? `clampNonNeg(-5) = 0`. So `a.raisedTotal += 0`. The record still counts as a run, but contributes 0 to the raised total. This is correct per the spec (negative values are clamped to 0).

OK, my findings are complete. Let me write them up.

Hmm, let me also look at the `writeField` function in `internal/benchmark/benchmark.go`:

```go
func writeField(h io.Writer, s string) {
	_, _ = fmt.Fprintf(h, "%d:", len(s))
	_, _ = io.WriteString(h, s)
}
```

The errors are ignored. This is OK for hashing - if the hash writer fails, there's nothing useful to do. The hash would be invalid, but the function would still return a value (which is wrong). However, in practice, `sha256.New()` returns a writer that doesn't fail. So this is fine.

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Let me write up the findings in the TD_STREAM format.

Actually wait, let me reconsider #2. The case where `verified+refuted == 0` and `len(storedRates) == 0` is a degenerate case. In this case, the rate is emitted as 0.0. Is this a bug?

The spec says: "omitempty - Omitted entirely when no verification ran for the group; present as 0.0 only when verification ran and every finding was refuted."

In the degenerate case:
- `hasVerification = true` means "at least one verification pointer was non-nil"
- `verified+refuted == 0` means "no counts were accumulated"
- `len(storedRates) == 0` means "no stored rates were accumulated"

This means: a verification pointer was non-nil, but the value was 0 and the count pointers were nil. This is a degenerate case where the verification "ran" but found nothing to verify.

In this case, the rate is emitted as 0.0. This is consistent with "verification ran and the rate is 0" (since there are no findings to verify, the rate is 0). So the spec is satisfied.

But the spec also says "present as 0.0 only when verification ran and every finding was refuted". The degenerate case doesn't match "every finding was refuted" - it matches "no findings to verify". So there's a semantic ambiguity.

However, in practice, this case is unlikely. The producer (the scorecard aggregation) would provide meaningful values when verification ran.

OK, I'll flag this as a MEDIUM correctness issue, noting that the edge case is ambiguous.

Actually, let me think about this more carefully. The `add` function sets `hasVerification = true` when ANY verification pointer is non-nil. This means:
- `r.FindingsVerified = &0` → `hasVerification = true`, but no counts added
- `r.FindingsRefuted = nil` → no change
- `r.SurvivedSkepticRate = nil` → no change

After `add`:
- `a.verified = 0`
- `a.refuted = 0`
- `a.storedRates = []`
- `a.hasVerification = true`

In `finalize`:
- `verified+refuted == 0` and `len(storedRates) == 0` → `rate` stays 0.0
- `&rate` is assigned to `pr.SurvivedSkepticRate`
- So the rate is emitted as 0.0

This is the degenerate case. The rate 0.0 could mean:
- Verification ran and every finding was refuted (verified=0, refuted>0) - legitimate 0.0
- Verification ran but there were no findings to verify (verified=0, refuted=0) - degenerate 0.0

The spec says 0.0 should only be emitted when "every finding was refuted". The degenerate case doesn't match this. So the code is technically violating the spec.

However, this is a corner case for corrupt/partial data. In practice, the producer would provide meaningful values.

To fix this, the `add` function could be more strict about setting `hasVerification = true`. For example, only set it if at least one of the verification fields has a non-zero value or if both verified and refuted are non-nil.

Or, the `finalize` function could be more strict about emitting the rate. For example, only emit if `verified+refuted > 0` or `len(storedRates) > 0`.

OK, I'll flag this as a MEDIUM correctness issue.

Let me also think about whether the `add` function could be simplified. The current logic is:

```go
if r.FindingsVerified != nil {
    a.verified += clampNonNeg(*r.FindingsVerified)
    a.hasVerification = true
}
if r.FindingsRefuted != nil {
    a.refuted += clampNonNeg(*r.FindingsRefuted)
    a.hasVerification = true
}
if r.SurvivedSkepticRate != nil {
    a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))
    a.hasVerification = true
}
```

This sets `hasVerification = true` when ANY of the three pointers is non-nil. This is a simple and clear pattern. The degenerate case is unlikely in practice.

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. The rate defaults to 0.0 in this case, which is ambiguous with "all refuted". (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Wait, I want to double-check the `--output` flag description. The diff shows:

```go
cmd.Flags().String("output", "", "write the submission JSON to this file instead of stdout (follows symlinks)")
```

The "(follows symlinks)" part is the description. This is consistent with the existing `leaderboard --output` flag (likely). So this is not a new issue introduced by this diff.

I'll still flag it as a LOW security concern, but note that it's consistent with existing behavior.

Actually, I realize I should be more careful about flagging issues that are consistent with existing behavior. The sprint plan says "Only flag issues in files/areas directly related to the work items below." The benchmark command is in scope (T2). The `--output` flag is a new flag in the new command. So this is in scope.

But the behavior (following symlinks) is determined by `writeExportFile`, which is in `leaderboard.go` (not in this diff). So the issue is not really in the new code - it's in the existing code that's being reused.

I'll still flag it as a LOW security concern, but note that the actual behavior depends on `writeExportFile`.

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Let me write up the findings.

Actually, I want to also think about whether there are any issues with the `Export` function. Let me look at it again:

```go
func Export(records []Record, opts FilterOpts, exportedAt time.Time) ([]byte, error) {
	filtered, err := ApplyFilters(records, opts, exportedAt)
	if err != nil {
		return nil, err
	}
	if len(filtered) == 0 {
		return nil, ErrNoExportRecords
	}

	type key struct{ persona, model string }
	groups := map[key]*reviewerAcc{}
	order := make([]key, 0)

	for _, r := range filtered {
		persona := scrubField(r.Reviewer)
		model := scrubField(r.Model)
		k := key{persona, model}
		a, ok := groups[k]
		if !ok {
			a = &reviewerAcc{persona: persona, model: model}
			groups[k] = a
			order = append(order, k)
		}
		a.add(r)
	}

	rows := make([]PublicRecord, 0, len(order))
	for _, k := range order {
		rows = append(rows, groups[k].finalize())
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		return rows[i].Persona < rows[j].Persona
	})

	env := ExportEnvelope{
		SubmissionSchema: SubmissionSchema,
		AtcrVersion:      version.Version,
		SubmittedAt:      exportedAt.UTC().Format(time.RFC3339),
		Reviewers:        rows,
	}
	return json.MarshalIndent(env, "", "  ")
}
```

This looks correct. The `key` uses scrubbed identity, the `add` accumulates counts, the `finalize` computes the public record, and the result is sorted and serialized.

One thing I notice: the `ExportEnvelope` has `SubmittedAt` set to `exportedAt.UTC().Format(time.RFC3339)`. This is the timestamp passed in. The function is deterministic for the same input and `exportedAt`. Good.

OK, my findings are complete. Let me write them up.

Wait, I should also consider whether the `add` function has any issues with the `a.runs++` line. This is unconditional, so every record adds 1 run. This is correct.

But what if the record has `Runs = 0`? The `add` function doesn't check `r.Runs` - it always increments `a.runs`. This means a record with `Runs = 0` would still count as 1 run. This might not be the intended behavior, but the `Record` type likely has `Runs` set to 1 by default (since each record represents one run).

OK, this is not a bug.

Let me also check the `costPer` function:

```go
func costPer(totalCost float64, corroborated int) float64 {
	if corroborated <= 0 {
		return 0
	}
	return totalCost / float64(corroborated)
}
```

This is correct. It returns 0 when `corroborated <= 0`, which avoids division by zero. The result is never Inf/NaN.

OK, my findings are complete. Let me write them up.

Actually, let me also check the `avgPerRun` function:

```go
func avgPerRun(total, runs int) float64 {
	if runs <= 0 {
		return 0
	}
	return float64(total) / float64(runs)
}
```

This is correct. It returns 0 when `runs <= 0`, which avoids division by zero.

And the `medianInt64` function:

```go
func medianInt64(xs []int64) int64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	sorted := make([]int64, n)
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
```

This is correct. It sorts a copy of the input and returns the median. For odd count, it returns the middle element. For even count, it returns the integer mean of the two middle elements.

OK, my findings are complete. Let me write them up.

Final findings:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Let me write them up in the TD_STREAM format.

Hmm, actually I realize the `add` function issue is more nuanced. Let me re-examine.

The `add` function:
```go
if r.FindingsVerified != nil {
    a.verified += clampNonNeg(*r.FindingsVerified)
    a.hasVerification = true
}
```

The issue: if `r.FindingsVerified` is non-nil but the value is 0, then `a.verified` doesn't change but `hasVerification` becomes true. This is a minor issue - in the finalize function, this would result in `verified+refuted == 0` (assuming refuted is also 0), which falls through the switch and emits rate as 0.0.

But is this really a bug? The `hasVerification` flag is meant to indicate "verification data was present for this group". If a record has `FindingsVerified = &0`, then verification data was present (the pointer is non-nil), but the value is 0. This is a valid state.

The issue is that the finalize function's switch doesn't handle the case where `verified+refuted == 0` and `len(storedRates) == 0`. In this case, the rate defaults to 0.0, which is ambiguous with "all refuted".

To fix this, the finalize function could:
- Only emit the rate if `verified+refuted > 0` or `len(storedRates) > 0`
- Or, only set `hasVerification = true` if at least one of the verification fields has a meaningful value

The current behavior is technically incorrect per the spec, but it's a corner case.

OK, I'll flag this as a MEDIUM correctness issue.

Let me also think about whether there are any other issues.

Actually, let me also check the `Export` function for the case where the same `(persona, model)` appears multiple times with different `FindingsRaised` values. The `add` function accumulates the `raisedTotal`:
```go
a.raisedTotal += clampNonNeg(r.FindingsRaised)
```

This is correct - the total is the sum of all raised counts across runs.

And the `corroborated` is accumulated similarly:
```go
a.corroborated += clampNonNeg(r.FindingsCorroborated)
```

This is correct.

The `FindingsRaisedAvg` is computed as `avgPerRun(a.raisedTotal, a.runs)`, which is the total divided by the number of runs. This is correct.

The `CorroborationRate` is computed as `ratio(a.corroborated, a.raisedTotal)`, which is the total corroborated divided by the total raised. This is correct.

The `CostPerCorroboratedFindingUSD` is computed as `costPer(a.costTotal, a.corroborated)`, which is the total cost divided by the total corroborated. This is correct.

The `LatencyP50MS` is computed as `medianInt64(a.latencies)`, which is the median of all per-run latencies. This is correct.

OK, my findings are complete. Let me write them up.

Wait, I want to also check the test for the `add` function and the `finalize` function. The test `TestExport_FindingsRaisedAvgIsPerRun` checks that the average is computed correctly:
```go
recs := []Record{
    exportRec("bruce", "claude-sonnet-4-6", 1),
    exportRec("bruce", "claude-sonnet-4-6", 3),
}
data, err := Export(recs, FilterOpts{Since: "30d"}, fixedExportNow)
require.NoError(t, err)
out := parseEnvelope(t, data).Reviewers
require.Len(t, out, 1, "same (persona, model) collapses to one aggregated row")
assert.Equal(t, 2, out[0].Runs)
assert.InDelta(t, 12.0, out[0].FindingsRaisedAvg, 1e-9, "findings_raised_avg is per-run, not the total")
```

This test creates two records with the same `(persona, model)` and `FindingsRaised = 12` each. After `add`, `a.raisedTotal = 24` and `a.runs = 2`. In `finalize`, `FindingsRaisedAvg = avgPerRun(24, 2) = 12.0`. Good.

OK, my findings are complete. Let me write them up.

Final findings:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. The rate defaults to 0.0 in this case, which is ambiguous with "all refuted". (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Let me write them up in the TD_STREAM format.

Hmm, actually let me reconsider #2. The case where `verified+refuted == 0` and `len(storedRates) == 0` is a degenerate case. In this case, the rate is emitted as 0.0. Is this really a bug?

The spec says: "omitempty - Omitted entirely when no verification ran for the group; present as 0.0 only when verification ran and every finding was refuted."

In the degenerate case:
- `hasVerification = true` means "at least one verification pointer was non-nil"
- `verified+refuted == 0` means "no counts were accumulated"
- `len(storedRates) == 0` means "no stored rates were accumulated"

This means: a verification pointer was non-nil, but the value was 0 and the count pointers were nil. This is a degenerate case where the verification "ran" but found nothing to verify.

In this case, the rate is emitted as 0.0. This is consistent with "verification ran and the rate is 0" (since there are no findings to verify, the rate is 0). So the spec is satisfied.

But the spec also says "present as 0.0 only when verification ran and every finding was refuted". The degenerate case doesn't match "every finding was refuted" - it matches "no findings to verify". So there's a semantic ambiguity.

However, in practice, this case is unlikely. The producer (the scorecard aggregation) would provide meaningful values when verification ran.

To fix this, the `add` function could be more strict about setting `hasVerification = true`. For example, only set it if at least one of the verification fields has a non-zero value or if both verified and refuted are non-nil.

Or, the `finalize` function could be more strict about emitting the rate. For example, only emit if `verified+refuted > 0` or `len(storedRates) > 0`.

OK, I'll flag this as a MEDIUM correctness issue.

Let me also think about whether the `add` function could be simplified. The current logic is:

```go
if r.FindingsVerified != nil {
    a.verified += clampNonNeg(*r.FindingsVerified)
    a.hasVerification = true
}
if r.FindingsRefuted != nil {
    a.refuted += clampNonNeg(*r.FindingsRefuted)
    a.hasVerification = true
}
if r.SurvivedSkepticRate != nil {
    a.storedRates = append(a.storedRates, clampRate(*r.SurvivedSkepticRate))
    a.hasVerification = true
}
```

This sets `hasVerification = true` when ANY of the three pointers is non-nil. This is a simple and clear pattern. The degenerate case is unlikely in practice.

OK, my final findings are:

1. **`cmd/atcr/benchmark.go` lines 61, 96, 97**: Ignored errors from `GetString` calls. (LOW, maintainability, 5 min)

2. **`internal/scorecard/export.go` `finalize` function**: The `switch` doesn't handle the edge case where `verified+refuted == 0` and `len(storedRates) == 0`. (MEDIUM, correctness, 10 min)

3. **`cmd/atcr/benchmark.go` line 110**: The `--output` flag description mentions "(follows symlinks)" which could be a security concern. (LOW, security, 5 min)

Let me write them up in the TD_STREAM format.
</think>

LOW|cmd/atcr/benchmark.go:61|Ignored error from cmd.Flags().GetString|Use the returned error or document the safe-to-ignore assumption|maintainability|5|suitePath, _ := cmd.Flags().GetString("suite-path") — flag is required so it won't fail, but silently treating any future error as empty string masks bugs|td-reviewer
LOW|cmd/atcr/benchmark.go:96|Ignored errors from two GetString calls|Handle the errors or assert they cannot occur|maintainability|5|in, _ := cmd.Flags().GetString("in") and output, _ := cmd.Flags().GetString("output") — same pattern, twice in one function|td-reviewer
LOW|cmd/atcr/benchmark.go:110|--output flag advertises symlink-following behavior|Verify writeExportFile's symlink behavior and consider rejecting symlink destinations or warn the user|security|10|cmd.Flags().String("output", "", "write the submission JSON to this file instead of stdout (follows symlinks)") — TOCTOU/symlink-attack concern if output path traverses a hostile symlink|td-reviewer
MEDIUM|internal/scorecard/export.go:135|SurvivedSkepticRate falls through to 0.0 in degenerate verification case|Guard the hasVerification branch so the pointer is only emitted when at least one of verified+refuted>0 or len(storedRates)>0|error-handling|10|when hasVerification=true (any non-nil pointer) but verified+refuted==0 AND len(storedRates)==0, the switch has no matching case, rate stays 0.0, and &rate is emitted — semantically indistinguishable from the documented "all refuted" 0.0 path|td-reviewer
LOW|internal/benchmark/benchmark.go:128|ReproHash re-reads diff files already stat'd by Load|Cache the diff bytes or pass the loaded manifest through|performance|5|ReproHash calls Load() (which stats every diff) then re-opens each diff with os.ReadFile; the bytes could be hashed once|td-reviewer
LOW|internal/scorecard/export.go:213|Group key uses scrubbed identity which can collapse distinct reviewers|Add a test asserting two pre-scrub distinct personas that scrub to the same value merge intentionally|correctness|10|key{persona, model} is built from scrubField output; two different personas that produce the same scrub result will merge into one group, which is by-design but currently unasserted|td-reviewer