We are given a diff and a sprint plan. We must only report issues that are in scope for the sprint plan.

The sprint plan is about Epic 4.2: Configuration Validation. The scope is defined as:

### In Scope
- `internal/registry/validate.go` with `Validate()` method on `Config`.
- Validation of required fields, enum values, type constraints, and semantic rules.
- Clear error messages with field names and expected values.
- Integration in `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `cmd/atcr/verify.go` (all commands that load config).
- Unit tests for each validation category.
- Integration test: invalid config → clear error message before execution.

However, note the clarifications:

**Key Decisions:**
- The epic's "Core Design" (`Config.Validate()`, `Registry.DefaultProvider`, `Registry.DefaultPayloadMode`, `Registry.Personas`) is treated as **illustrative intent only**. None of those types/fields exist. No `Config` type is added.
- The real model is `registry.Registry` (`internal/registry/config.go:148`) holding `Providers map[string]Provider` + `Agents map[string]AgentConfig`; personas are `.md` files, not YAML structs.
- Validation already lives in `(*Registry).validate()` (`config.go:202`) and `ValidateFallbacks()` (`graph.go:30`), invoked at load by `LoadRegistry` (`config.go:191-196`) and `validateMerged` (`overlay.go:223`). A parallel `Config.Validate()` would duplicate working validation against fields that do not exist.
- **The single genuine deliverable is AC6: error accumulation.** `validate()` and `ValidateFallbacks()` currently return on the first error. They will be refactored to accumulate every error and report them together via `errors.Join`, with deterministic ordering (sorted provider/agent names) so output is stable.
- `attribute()` / `validateMerged()` (`overlay.go:223`, `attribution.go:48`) must become multi-error aware so each error in a merged project+user config is still attributed to the file that defined the offending entry.

**Scope Boundaries:**
- IN scope: accumulate-all-errors refactor of `validate()` + `ValidateFallbacks()`; multi-error attribution in `attribute()`; tests asserting all-at-once reporting per AC category against the real structs.
- OUT of scope: a new `Config` type; renaming error messages to the fictional `registry.default_provider` schema; provider→API-key-env presence check at load (deliberately deferred — keys resolve at invoke time); an `atcr doctor` config check (epic Open Question #2 — not requested).

Therefore, we are only to look for issues related to:
1. The accumulate-all-errors refactor in `validate()` and `ValidateFallbacks()`.
2. Multi-error attribution in `attribute()` (in `attribution.go`).
3. Tests that assert the all-at-once reporting.

Now, let's examine the diff:

The diff includes changes in:
- `cmd/atcr/review_test.go` (adding an integration test for invalid config)
- `docs/registry.md` (a documentation change)
- `internal/registry/accumulate_test.go` (new file with tests for accumulation)
- `internal/registry/attribution.go` (changes to the `attribute` function)
- `internal/registry/config.go` (changes to `validate`, `validateProvider`, `validateAgent`, and adding `sortedKeys`)
- `internal/registry/graph.go` (changes to `ValidateFallbacks` and `walkFallbacks`)

We must check if these changes are in scope and if they introduce any issues.

We are to output only the issues we find in the TD_STREAM format.

Let's go through each changed file and see if there are any issues that are in scope.

1. `cmd/atcr/review_test.go`:
   - Added a test `TestReviewCmd_InvalidConfigReportsAllErrors`.
   - This test is an integration test for the config validation (AC6, AC5, AC7, AC9). It is in scope.
   - We must check if the test is correct and if there are any issues in the test code.

   Looking at the test:
   ```go
   func TestReviewCmd_InvalidConfigReportsAllErrors(t *testing.T) {
       isolate(t)
       initGitRepoWithChange(t)

       home, err := os.UserHomeDir()
       require.NoError(t, err)
       regDir := filepath.Join(home, ".config", "atcr")
       require.NoError(t, os.MkdirAll(regDir, 0o755))
       require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`timeout_secs: -1
       payload_mode: bogus
       providers:
         testprov:
           api_key_env: ATCR_TEST_REVIEW_KEY
       agents:
         bruce:
           provider: testprov
           min_severity: BOGUS
       `), 0o644))
       require.NoError(t, os.MkdirAll(".atcr", 0o755))
       require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))

       code, out := execCmdCapture(t, "review", "--base", "HEAD^")
       require.Equal(t, 2, code, "AC5: invalid config is a usage error (exit 2)")
       // AC6: all faults reported together, not one-at-a-time.
       assert.Contains(t, out, "timeout_secs", "AC3 fault must surface")
       assert.Contains(t, out, "payload_mode", "AC2 fault must surface")
       assert.Contains(t, out, "required field 'model'", "AC1 fault must surface")
       assert.Contains(t, out, "min_severity", "AC2/AC4 fault must surface")
   }
   ```

   The test writes a user registry (in `$HOME/.config/atcr/registry.yaml`) and a project registry (in `.atcr/config.yaml`).

   The user registry has:
     - timeout_secs: -1 (invalid, should be >0)
     - payload_mode: bogus (invalid enum)
     - providers: testprov with api_key_env: ATCR_TEST_REVIEW_KEY (valid)
     - agents: bruce with provider: testprov and min_severity: BOGUS (invalid enum for min_severity)

   The project registry has:
     - agents: [bruce] (so it references the agent 'bruce' from the user registry)

   The test expects:
     - Exit code 2 (usage error) -> AC5.
     - The output to contain:
          "timeout_secs" (AC3: type/range)
          "payload_mode" (AC2: enum)
          "required field 'model'" (AC1: required field for agent 'bruce' missing model)
          "min_severity" (AC2/AC4: invalid enum for min_severity)

   However, note that the agent 'bruce' is missing the `model` field (required) and has an invalid `min_severity`.

   The test seems to be checking for the presence of these strings in the output.

   But note: the test writes the user registry to `$HOME/.config/atcr/registry.yaml`. However, the test uses `os.UserHomeDir()` which might be slow or might not be available in the test environment? But that's not an issue we are to flag because it's a test utility and the sprint plan doesn't forbid it.

   However, we must check if the test is actually testing the accumulation of errors. The test expects multiple errors to be present in the output.

   There is a potential issue: the test writes to the user's home directory. This might be problematic in some environments (e.g., if the test runs as a user without a home directory, or if we don't want to pollute the home directory). But the test uses `isolate(t)` which should set up a temporary home? Let's see what `isolate` does.

   We don't have the code for `isolate`, but given the context of the other tests, it likely sets up a temporary environment. However, the test uses `os.UserHomeDir()` and then writes to `$HOME/.config/atcr`. If `isolate` does not change the home directory, then this test is writing to the real home directory, which is bad.

   But note: the sprint plan does not require us to check test correctness beyond the scope of the epic? However, we are allowed to flag issues in the test code if they are in scope and are real issues.

   However, the problem is that writing to the real home directory is a side effect that might break other tests or leave state. This is a test flaw.

   But wait: the test uses `isolate(t)`. We must assume that `isolate` sets up a temporary environment, including the home directory? Let's look at the other tests in the same file.

   We don't have the full file, but we can see that the test `TestRunReview_ProjectConfigGateActivatedWithoutFlag` (which is already in the file) uses `isolate(t)` and then creates a directory `.atcr` in the current directory. It does not use the home directory.

   Therefore, the test `TestReviewCmd_InvalidConfigReportsAllErrors` might be flawed because it writes to the real home directory.

   However, note that the test writes to `$HOME/.config/atcr/registry.yaml`. If `isolate` does not change the home directory, then this is a problem.

   But we don't have the definition of `isolate`. Since we are not given the entire test file, we cannot be sure.

   Given the ambiguity, and since the sprint plan is about the accumulation of errors, we might not want to flag this as an issue because it might be that `isolate` does set up a temporary home. However, the test does not set the environment variable `HOME` or use a temporary directory for the home.

   Alternatively, we can note that the test is writing to a fixed location in the home directory, which is not safe for parallel tests. But again, we don't know the context.

   Since we are not given the helper functions, we cannot be certain. We will skip this for now unless we see a clear issue.

   Another issue: the test writes the user registry with a provider `testprov` that has `api_key_env: ATCR_TEST_REVIEW_KEY`. This environment variable might not be set, but the validation for the provider does not check the presence of the environment variable at load time (as per the clarifications: provider→API-key-env presence check at load is deliberately deferred). So that is okay.

   The test expects the error about the missing model for agent 'bruce'. However, note that the agent 'bruce' is defined in the user registry, and the project registry only references it. The validation should catch the missing model in the user registry.

   The test expects the string "required field 'model'". In the code, the error for missing model in an agent is:
        agentErrf(name, "agent '%s': required field 'model' is missing", name)

   So the error message will be: "agent 'bruce': required field 'model' is missing"

   The test checks for the substring "required field 'model'", which is present.

   Similarly, for the invalid min_severity, the error is:
        agentErrf(name, "agent '%s': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW", name)

   So the error message will be: "agent 'bruce': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW"

   The test checks for the substring "min_severity", which is present.

   However, note that the test also expects to see "timeout_secs" and "payload_mode". These are settings-level errors.

   The test seems correct in its assertions.

   But note: the test writes the user registry to the home directory. If the test is run in an environment without a home directory (like in some CI containers) or if the home directory is not writable, the test will fail. This is a test reliability issue.

   However, the sprint plan does not explicitly require tests to be hermetic or to avoid the home directory. But it is a good practice.

   Given that we are to focus on the scope of the epic (accumulation of errors) and the test is testing that, and we don't have evidence that `isolate` doesn't set up a temporary home, we will not flag this test.

   However, we note that the test uses `os.UserHomeDir()` and then writes to a subdirectory. This is risky. But without more context, we cannot say for sure.

   Let's move on.

2. `docs/registry.md`:
   - Changed a line to add: "All faults in a config are reported together (each naming the file that defined the offending entry), so you fix them in a single pass rather than one error per run."
   - This is documentation and in scope (as per the sprint plan: clear error messages). However, we are not to flag documentation issues unless they are incorrect? The sprint plan doesn't mention documentation as in scope for code changes, but the change is in the diff.

   We are only to flag issues (bugs, etc.). This change is just adding a sentence to the documentation. It seems correct and in line with the epic.

   No issue here.

3. `internal/registry/accumulate_test.go`:
   - This is a new file with tests for the accumulation of errors. It is in scope (unit tests for each validation category).

   We must check if the tests are correct and if there are any issues in the test code.

   Let's look at the first test: `TestValidate_AccumulatesAllErrors`.

   It creates a registry with:
        Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
        Agents: map[string]AgentConfig{
            "alpha": {Provider: "openai", Model: ""},                        // missing model
            "bravo": {Provider: "openai", Model: "m", MinSeverity: "BOGUS"}, // invalid enum
        },
        PayloadMode: "bogus",    // invalid enum
        TimeoutSecs: intPtr(-1), // out of range

   Then it calls `reg.validate()` and checks that the error message contains:
        "timeout_secs"
        "payload_mode"
        "alpha"
        "model"
        "bravo"
        "min_severity"

   This seems correct.

   However, note that the test uses `intPtr` which is not defined in the test file. We must assume it is a helper function defined elsewhere in the test file or imported.

   We don't see the definition of `intPtr` in the given diff for this file. But note that the test file might have helper functions above. We are only given the diff for the new file.

   Since we are not given the entire test file, we cannot be sure. However, the test is in the same package and likely has access to helpers.

   We will assume it is defined.

   The test `TestValidate_DeterministicOrder` checks that the error messages are in a deterministic order (sorted by agent name). It builds a registry with agents named "zulu", "alpha", "mike", "bravo", "yankee" (all with missing model) and checks that the error messages appear in the order: alpha, bravo, mike, yankee, zulu.

   This test is important for the accumulation feature to have stable output.

   The test `TestValidate_ValidRegistryReturnsNil` checks that a valid registry returns nil.

   The test `TestValidateFallbacks_AccumulatesMultipleDangling` checks that two dangling fallbacks are reported together.

   The test `TestValidateFallbacks_AccumulatesDanglingAndCycle` checks that a dangling fallback and a cycle are reported together.

   The test `TestValidateFallbacks_LeadInIntoReportedCycleNoPanic` checks that a lead-in into a cycle does not cause a panic and that only one cycle is reported.

   The test `TestValidateFallbacks_LeadInLeftGrayThenRevisited` is a regression test for a panic.

   The test `TestValidateFallbacks_TwoIndependentCycles` checks that two independent cycles are both reported.

   The test `TestValidateFallbacks_ValidReturnsNil` checks the happy path.

   The test `TestAttribute_MultiErrorPerEntryFiles` tests that when there are errors in both the user and project registry, each error is attributed to the correct file.

   The test `TestAttribute_SingleEntryStillAttributed` tests that a single error is still attributed to its file.

   The test `TestLoadRegistry_ReportsAllErrorsAtOnce` is an integration test for the load path.

   The test `TestAttribute_SettingsFaultGetsUserLabel` tests that a settings-level fault (like payload_mode) is attributed to the user registry file.

   All these tests seem to be in scope and are testing the accumulation and attribution.

   We must check if there are any issues in the test code.

   One potential issue: in `TestValidate_AccumulatesAllErrors`, the test checks for the string "alpha" and "model" separately. But note that the error for the agent "alpha" missing model is:
        agentErrf("alpha", "agent '%s': required field 'model' is missing", "alpha")
   which becomes: "agent 'alpha': required field 'model' is missing"

   So the string "alpha" appears and the string "model" appears. This is okay.

   However, note that the test also checks for the string "bravo" and "min_severity" for the agent "bravo". The error for bravo is:
        agentErrf("bravo", "agent '%s': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW", "bravo")
   which becomes: "agent 'bravo': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW"

   So the string "bravo" and "min_severity" appear.

   The test also checks for "timeout_secs" and "payload_mode".

   This seems correct.

   Another potential issue: in `TestValidate_DeterministicOrder`, the test checks the order of the agent names in the error string. It expects the agents to be in sorted order: alpha, bravo, mike, yankee, zulu.

   The test uses:
        idxAlpha := strings.Index(first, "alpha")
        idxBravo := strings.Index(first, "bravo")
        ... and then checks that idxAlpha < idxBravo < idxMike < idxYankee < idxZulu.

   This is correct.

   However, note that the error string might contain the agent name multiple times? For example, if an agent has multiple errors, the agent name might appear multiple times. But in this test, each agent has exactly one error (missing model). So the agent name appears once per agent.

   The test is valid.

   We do not see any obvious issues in the test file.

4. `internal/registry/attribution.go`:
   - Changed the `attribute` function to handle accumulated errors (errors.Join).

   The change:
   ```go
   func (r *Registry) attribute(err error) error {
+	if joined, ok := err.(interface{ Unwrap() []error }); ok {
+		children := joined.Unwrap()
+		out := make([]error, len(children))
+		for i, c := range children {
+			out[i] = r.attribute(c)
+		}
+		return errors.Join(out...)
+	}
   // ... rest unchanged
   ```

   This is exactly what is needed for the accumulation: when we have an error that is a join of errors, we attribute each child error and then join them again.

   However, note that the interface used is `{ Unwrap() []error }`. This is the interface that `errors.Join` implements? Actually, `errors.Join` returns an error that implements the `interface{ Unwrap() []error }` (since Go 1.20). But note that the code might be running on an older Go version? We don't know.

   However, the sprint plan does not specify the Go version. But given that the project is using Go modules and the diff shows other modern Go features, we assume it's acceptable.

   But note: the existing code in `attribution.go` before the change was:
   ```go
   func (r *Registry) attribute(err error) error {
       var ee *entryError
       if !errors.As(err, &ee) {
           // Non-entry failures are top-level settings faults (payload_mode,
           // verify.min_severity, verify.votes, verify.max_parallel). These
           // errors are not tied to a specific file, so we prefix them with
           // the user registry file if they come from there, or the project
           // registry file if they come from there.
           if r.stamped(SourceUser) {
               return fmt.Errorf("%s: %s", userRegistryLabel, err)
           }
           if r.stamped(SourceProject) {
               return fmt.Errorf("%s: %s", projectRegistryLabel, err)
           }
           return err
       }
       // ... rest for entryError
   }
   ```

   The new code handles the joined error by unwrapping and attributing each child.

   However, there is a potential issue: what if the error is not a *Registry* error but a joined error that contains non-entry errors and entry errors? The new code will recursively attribute each child.

   This seems correct.

   But note: the function `r.stamped(SourceUser)` and `r.stamped(SourceProject)` are used in the non-entry error branch. We must check if these methods are defined and work correctly.

   We don't see the definition of `stamped` in the diff, but it is likely in the same file or in `config.go`. We are not given that.

   However, the change in `attribution.go` seems to be in line with the requirement.

   We do not see an obvious issue.

5. `internal/registry/config.go`:
   - Many changes: added imports, changed `validate` to accumulate errors, added `validateProvider` and `validateAgent` helper functions, added `sortedKeys`.

   We must check if the accumulation is done correctly and if there are any bugs.

   Let's look at the new `validate` function:

   ```go
   func (r *Registry) validate() error {
+	var errs []error
+
+	// Settings-level checks, in fixed source order.
+	if r.TimeoutSecs != nil && (*r.TimeoutSecs <= 0 || *r.TimeoutSecs > MaxTimeoutSecs) {
+		errs = append(errs, fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs))
+	}
+	if r.PayloadByteBudget != nil && *r.PayloadByteBudget < 0 {
+		errs = append(errs, fmt.Errorf("payload_byte_budget must be >= 0 (0 = unlimited), got %d", *r.PayloadByteBudget))
+	}
+	if r.MaxParallel != nil && *r.MaxParallel < 0 {
+		errs = append(errs, fmt.Errorf("max_parallel must be >= 0 (0 = unbounded), got %d", *r.MaxParallel))
+	}
+	if !payloadModeValid(r.PayloadMode) {
+		errs = append(errs, fmt.Errorf("invalid payload_mode '%s': must be one of diff, blocks, files", strings.TrimSpace(r.PayloadMode)))
+	}
+	// verify.min_severity (Epic 3.0): an empty value defaults to MEDIUM at load;
+	// any non-empty value must be a canonical review severity. Error wording lists
+	// the levels low→high so a typo (e.g. "BLOCKER") is corrected quickly.
+	if normalized := stream.NormalizeSeverity(r.Verify.MinSeverity); normalized != "" && !reviewSeverities[normalized] {
+		errs = append(errs, fmt.Errorf("invalid verify.min_severity %q: must be LOW, MEDIUM, HIGH, or CRITICAL", r.Verify.MinSeverity))
+	}
+	if r.Verify.Votes < 0 {
+		errs = append(errs, fmt.Errorf("verify.votes must be >= 0 (0 = default), got %d", r.Verify.Votes))
+	}
+	if r.Verify.MaxParallel < 0 {
+		errs = append(errs, fmt.Errorf("verify.max_parallel must be >= 0 (0 = default 4), got %d", r.Verify.MaxParallel))
+	}
+	for _, name := range sortedKeys(r.Providers) {
+		errs = append(errs, validateProvider(name, r.Providers[name])...)
+	}
+	for _, name := range sortedKeys(r.Agents) {
+		errs = append(errs, r.validateAgent(name, r.Agents[name])...)
+	}
+
+	return errors.Join(errs...)
+}
   ```

   This looks correct. It collects all the errors and then returns `errors.Join(errs...)`. If there are no errors, `errors.Join` returns nil.

   The helper functions `validateProvider` and `validateAgent` return a slice of errors, which are then appended.

   We must check if the error messages are the same as before (except for the accumulation). The sprint plan says that the error messages should remain the same for compatibility.

   Let's compare one: the timeout check.

   Old:
        return fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs)

   New:
        errs = append(errs, fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs))

   Same.

   The helper function `sortedKeys` is added to get the keys in sorted order for deterministic iteration.

   Now, let's look at `validateProvider`:

   ```go
   // validateProvider returns every fault found in a single provider entry (Epic
   // 4.2 / AC6 — accumulate rather than short-circuit).
   func validateProvider(name string, p Provider) []error {
+	var errs []error
+	if strings.TrimSpace(name) == "" {
+		errs = append(errs, providerErrf(name, "providers.%s: provider name must not be empty", name))
+	}
+	if p.APIKeyEnv == "" {
+		errs = append(errs, providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name))
+	} else if !envVarName.MatchString(p.APIKeyEnv) {
+		errs = append(errs, providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv))
+	}
+	if p.BaseURL != "" {
+		u, err := url.Parse(p.BaseURL)
+		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
+			errs = append(errs, providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name))
+		} else if u.User != nil {
+			errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
+		}
+	}
+	return errs
+}
   ```

   This is the same as the old code but instead of returning on the first error, it appends to `errs` and returns the slice.

   Similarly for `validateAgent`.

   However, note in `validateAgent` there is a comment:
        // unknown-provider reference check is suppressed when provider is empty so a missing-provider agent reports
        // only the "required field" fault, not a spurious "references unknown provider ”".

   The code:
        if a.Provider == "" {
            errs = append(errs, agentErrf(name, "agent '%s': required field 'provider' is missing", name))
        } else if _, ok := r.Providers[a.Provider]; !ok {
            errs = append(errs, agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider))
        }

   This is correct: if the provider is empty, we don't check for unknown provider (to avoid a cascading error).

   Now, we must check if there are any mistakes in the conversion.

   One potential issue: in the old `validate` function, there was a check for the provider's `BaseURL` that included:
        if u.User != nil {
            return providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name)
        }

   In the new code, it is:
        } else if u.User != nil {
            errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
        }

   This is inside the same block as the URL parsing. So if the URL is invalid (scheme not http/https or host empty), we add an error for that and then skip the userinfo check? Actually, no: the code structure is:

        if p.BaseURL != "" {
            u, err := url.Parse(p.BaseURL)
            if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
                errs = append(errs, providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name))
            } else if u.User != nil {
                errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
            }
        }

   This is correct: if the URL is invalid, we add the invalid URL error and then skip the userinfo check (because it's in an else if). If the URL is valid, then we check for userinfo.

   This matches the old code.

   Another potential issue: in the old code, the validation of the agent's `Scope` field had a check for control characters:

        if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
            return agentErrf(name, "agent '%s': scope entries must not contain control characters", name)
        }

   In the new code, it is:

        } else if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
            errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
        }

   This is inside a loop over `a.Scope` and is correct.

   However, note that the old code returned on the first error, and the new code appends and continues. This is the accumulation.

   We do not see any obvious mistakes.

   But note: the function `sortedKeys` is defined at the end of the file:

   ```go
   // sortedKeys returns a map's string keys in ascending order, so validation walks
   // providers and agents deterministically regardless of Go's randomized map
   // iteration.
   func sortedKeys[V any](m map[string]V) []string {
+	keys := make([]string, 0, len(m))
+	for k := range m {
+		keys = append(keys, k)
+	}
+	sort.Strings(keys)
+	return keys
+}
   ```

   This is correct.

   However, note that the function is generic. This requires Go 1.18 or later. We assume the project uses a recent Go version.

   We do not see any issues in `config.go`.

6. `internal/registry/graph.go`:
   - Changes to `ValidateFallbacks` and `walkFallbacks`.

   The change in `ValidateFallbacks`:

   ```go
   func (r *Registry) ValidateFallbacks() error {
-	// Deterministic iteration so error messages are stable.
-	names := make([]string, 0, len(r.Agents))
-	for name := range r.Agents {
-		names = append(names, name)
-	}
-	sort.Strings(names)
+	names := sortedKeys(r.Agents)
+
+	var errs []error
+
+	for _, name := range names {
+		fb := r.Agents[name].Fallback
+		if fb == "" {
+			continue
+		}
+		if _, ok := r.Agents[fb]; !ok {
+			errs = append(errs, agentSentinelErr(name, ErrDanglingFallback,
+				fmt.Sprintf("%s: agent '%s' fallback references unknown agent '%s'", ErrDanglingFallback, name, fb)))
+		}
+	}
+
+	// ... cycle detection part ...
+
+		return errors.Join(errs...)
+   }
   ```

   And the cycle detection part is changed similarly to accumulate errors.

   Also, in `walkFallbacks`, there is a change to the cycle detection: when a cycle is found, it now blackens every node in the path (including the lead-in) to avoid a panic when a lead-in is revisited.

   The change in `walkFallbacks` when a cycle is detected:

   ```go
   if color[next] == gray {
		// Trim the lead-in so the reported path starts at the repeated node.
		// Build the cycle into a fresh slice BEFORE blackening so it never
		// aliases path's backing array, then blacken EVERY node this walk
		// visited — lead-in nodes included, not just the trimmed cycle. Under
		// accumulation ValidateFallbacks keeps scanning, and a later root that
		// edges into a leftover-gray lead-in node would otherwise reach the
		// panic below. The single-outgoing-edge invariant makes this safe: a
		// lead-in node has one edge (into this cycle) and cannot start another
		// cycle, so marking it fully-explored loses no future detection.
		// Because ValidateFallbacks only walks white roots and colors nodes
		// gray on the current path, next is always in path — the loop cannot
		// complete without matching.
		for i, n := range path {
			if n == next {
				cycle := make([]string, 0, len(path)-i+1)
				cycle = append(cycle, path[i:]...)
				cycle = append(cycle, next)
				for _, visited := range path {
					color[visited] = black
				}
				return cycle, true
			}
		}
		// Unreachable: next is gray, hence already on the current path.
   }
   ```

   This change is to fix a potential panic when a lead-in node is left gray and then revisited by a different root. The comments explain it well.

   We must check if this change is correct and if it introduces any issues.

   The accumulation in `ValidateFallbacks` requires that we continue scanning even after finding a cycle or a dangling fallback. The change in `walkFallbacks` ensures that when a cycle is detected, we mark all nodes in the current path (including the lead-in) as black (fully visited) so that if we later start from a node that points to a lead-in node that is part of a cycle we've already seen, we won't try to walk it again and potentially cause a panic.

   This seems correct and is in response to the regression test.

   We do not see any obvious issues.

Now, we must look for any issues that are in scope and that are bugs.

Let's consider the following:

In `internal/registry/config.go`, in the `validate` function, we have:

```go
	if r.TimeoutSecs != nil && (*r.TimeoutSecs <= 0 || *r.TimeoutSecs > MaxTimeoutSecs) {
		errs = append(errs, fmt.Errorf("timeout_secs must be within 1..%d", MaxTimeoutSecs))
	}
```

What if `r.TimeoutSecs` is nil? We skip. That's correct because the field is optional.

But note: the old code did the same.

However, note that the error message says "must be within 1..%d". But if the value is negative, we say it must be within 1..MaxTimeoutSecs, which is correct. If it's too large, same.

But what if the value is 0? The condition is `*r.TimeoutSecs <= 0`, so 0 is caught and we say it must be within 1..MaxTimeoutSecs. However, 0 is not within 1..MaxTimeoutSecs, so the message is correct.

But note: the comment in the old code for the similar check in the agent validation said: "timeout_secs must be within 1..%d". So it's consistent.

However, there is a potential issue: the error message for timeout_secs being 0 is the same as for being negative or too large. But that's acceptable.

Another issue: in the `validate` function, we check `r.PayloadByteBudget` and `r.MaxParallel