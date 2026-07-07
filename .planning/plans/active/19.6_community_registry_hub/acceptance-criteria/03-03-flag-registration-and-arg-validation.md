# Acceptance Criteria: `--model`/`--provider` Flag Registration and Argument Validation Guard

**Related User Story:** [03: Model-Aware Search and Discovery via `--model`/`--provider`](../user-stories/03-model-aware-search-and-discovery.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra command (`cmd/atcr/personas.go`, `newPersonasSearchCmd`) | `cmd.Flags().String(...)` registration, following the `--scores` pattern on `newPersonasListCmd` |
| Test Framework | Go `testing` package with Cobra command execution harness (`cmd.Execute()` / `cmd.SetArgs()`) | Asserts `Args` relaxation and usage-error guard |
| Key Dependencies | `spf13/cobra` (existing dependency only) | No new third-party dependency |

## Related Files
- `cmd/atcr/personas.go` - modify: `newPersonasSearchCmd` (line ~218) — relax `Args: usageArgs(cobra.ExactArgs(1))` to `usageArgs(cobra.MaximumNArgs(1))` (or equivalent), register `--model`/`--provider` as `cmd.Flags().String(name, "", usage)`, and add a `RunE` guard mirroring the existing empty-keyword check (line ~225-227) that returns a `usageError` when the keyword is empty AND both `--model`/`--provider` are unset
- `cmd/atcr/personas_test.go` - create/modify: integration tests invoking the search command with no args and no flags, expecting a `usageError`; and with only `--model`/`--provider` and no positional keyword, expecting success

## Happy Path Scenarios
**Scenario 1: `--model`-only invocation succeeds with no positional keyword**
- **Given** the relaxed `Args: cobra.MaximumNArgs(1)` on `newPersonasSearchCmd`
- **When** `atcr personas search --model deepseek` is run (no positional argument)
- **Then** the command runs successfully, filtering by the structured `Model` field alone, with no argument-count error

**Scenario 2: `--provider`-only invocation succeeds with no positional keyword**
- **When** `atcr personas search --provider anthropic` is run (no positional argument)
- **Then** the command runs successfully, filtering by the structured `Provider` field alone

**Scenario 3: Positional keyword plus flags together still validate as at most one positional arg**
- **When** `atcr personas search deepseek --model deepseek-coder` is run
- **Then** cobra accepts exactly one positional arg per `MaximumNArgs(1)` and both the keyword and the flag filters apply

## Edge Cases
**Edge Case 1: No keyword and no flags produces a usage error, not an empty result**
- **Given** the story's Risk #1 (relaxing `ExactArgs(1)` could silently accept a no-argument, no-flag call)
- **When** `atcr personas search` is run with zero positional args and neither `--model` nor `--provider` set
- **Then** the command returns a `usageError` (e.g. `"provide a keyword, --model, or --provider"`) instead of silently running an unfiltered/empty search

**Edge Case 2: Two positional arguments still rejected**
- **Given** `Args: cobra.MaximumNArgs(1)`
- **When** `atcr personas search foo bar` is run
- **Then** cobra's built-in argument-count validation rejects the call before `RunE` executes, matching pre-existing "too many args" behavior for other subcommands in this codebase

**Edge Case 3: Whitespace-only keyword with a flag set is treated as "no keyword" for the guard**
- **Given** keyword argument `"   "` and `--model deepseek` both supplied
- **When** the command runs
- **Then** the guard treats the trimmed-empty keyword as absent (does not error, since `--model` satisfies the "something was supplied" requirement) and filtering proceeds using only `--model`

## Error Conditions
**Error Scenario 1: Bare `atcr personas search` with no keyword and no flags**
- **Given** no positional argument and no `--model`/`--provider` flags
- **When** the command is invoked
- **Then** a `usageError` is returned with a message clearly stating a keyword or `--model`/`--provider` is required
- HTTP status / error code: N/A (CLI exit code non-zero via existing `usageError` wrapping convention)

**Error Scenario 2: More than one positional argument**
- **Given** two or more positional args
- **When** the command is invoked
- **Then** cobra's `MaximumNArgs(1)` validation fails the call with its standard "accepts at most 1 arg(s), received N" message before `RunE` runs

## Performance Requirements
- **Response Time:** Flag parsing and argument-count validation are O(1) cobra framework operations; no measurable added latency.
- **Throughput:** N/A (single-user CLI invocation).

## Security Considerations
- **Authentication/Authorization:** N/A — flag/argument validation only, no privilege boundary.
- **Input Validation:** The new `RunE` guard explicitly closes the gap identified in the story's Risk table (Medium impact: silent empty-result on flag-only relaxation) by requiring at least one of keyword/`--model`/`--provider` to be non-empty.

## Test Implementation Guidance
**Test Type:** INTEGRATION (Cobra command execution via `cmd.SetArgs()` + `cmd.Execute()`)
**Test Data Requirements:** No index/network fixture needed for the pure argument-validation cases (guard fires before `Search()` is called); combine with AC 03-01's mock index for the flag-only-success cases
**Mock/Stub Requirements:** None required for the validation-only paths; reuse `httptest.NewServer` + `ATCR_PERSONAS_URL` override for the flag-only success scenarios that reach `Search()`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `Args` relaxed from `ExactArgs(1)` to `MaximumNArgs(1)` (or equivalent) on `newPersonasSearchCmd`
- [ ] `--model`/`--provider` registered via `cmd.Flags().String(...)` following the `--scores` pattern
- [ ] `RunE` guard returns `usageError` when keyword is empty/whitespace AND both `--model`/`--provider` are unset
- [ ] `--model`-only and `--provider`-only invocations (no positional keyword) succeed

**Manual Review:**
- [ ] Code reviewed and approved
