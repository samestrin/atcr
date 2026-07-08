# Acceptance Criteria: `--model`/`--provider` Flag Registration and Argument Validation Guard

**Related User Story:** [03: Model-Aware Search and Discovery via `--model`/`--provider`](../user-stories/03-model-aware-search-and-discovery.md)
**Design References:** [cli-search-flags.md](../documentation/cli-search-flags.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra command (`cmd/atcr/personas.go`, `newPersonasSearchCmd`) | `cmd.Flags().String(...)` registration, following the `--scores` pattern on `newPersonasListCmd` |
| Test Framework | Go `testing` package with Cobra command execution harness (`cmd.Execute()` / `cmd.SetArgs()`) | Asserts `Args` relaxation and usage-error guard |
| Key Dependencies | `spf13/cobra` (existing dependency only) | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/personas.go` (line ~218 `newPersonasSearchCmd`) — modify: relax `Args` from `ExactArgs(1)` to `MaximumNArgs(1)`, register `--model`/`--provider` via `cmd.Flags().String(...)`, and add a `RunE` guard returning the canonical `usageError` string `"provide a keyword, --model, or --provider"` when — after trimming — keyword AND both flag values are all empty. Trim each flag value before evaluating it, so an empty/whitespace `--model ""`/`--provider ""` counts as absent (never a whole-index match).
- `cmd/atcr/personas_test.go` — create/modify: integration tests invoking the search command with no args/no flags (expect `usageError`) and with only `--model`/`--provider` and no positional keyword (expect success).
- `internal/personas/search.go` (`Search`) — reference: the search function that receives the flag values.


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
- **Then** the command returns the canonical `usageError` string `"provide a keyword, --model, or --provider"` (this exact string is pinned and reused by every guard path in this AC) instead of silently running an unfiltered/empty search

**Edge Case 2: Two positional arguments still rejected**
- **Given** `Args: cobra.MaximumNArgs(1)`
- **When** `atcr personas search foo bar` is run
- **Then** cobra's built-in argument-count validation rejects the call before `RunE` executes, matching pre-existing "too many args" behavior for other subcommands in this codebase

**Edge Case 3: Whitespace-only keyword with a flag set is treated as "no keyword" for the guard**
- **Given** keyword argument `"   "` and `--model deepseek` both supplied
- **When** the command runs
- **Then** the guard treats the trimmed-empty keyword as absent (does not error, since `--model` satisfies the "something was supplied" requirement) and filtering proceeds using only `--model`

**Edge Case 4: Empty/whitespace flag value is treated as ABSENT (no unfiltered whole-index match)**
- **Given** `atcr personas search --model ""` (or `--provider "   "`) with no positional keyword
- **When** the guard evaluates the inputs
- **Then** the flag value is trimmed and, being empty, is treated as ABSENT — it does NOT count toward "something was supplied". Because keyword, `--model`, and `--provider` are all effectively empty, the guard returns the canonical `usageError` (`"provide a keyword, --model, or --provider"`) and the command MUST NOT proceed to run an unfiltered whole-index match. The guard requires at least one NON-EMPTY value among {keyword, `--model`, `--provider`} after trimming.

## Error Conditions
**Error Scenario 1: Bare `atcr personas search` with no keyword and no flags**
- **Given** no positional argument and no `--model`/`--provider` flags
- **When** the command is invoked
- **Then** a `usageError` is returned carrying the pinned canonical string `"provide a keyword, --model, or --provider"` (identical to Edge Case 1 and Edge Case 4 — one string, reused)
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
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `Args` relaxed from `ExactArgs(1)` to `MaximumNArgs(1)` (or equivalent) on `newPersonasSearchCmd`
- [x] `--model`/`--provider` registered via `cmd.Flags().String(...)` following the `--scores` pattern
- [x] `RunE` guard returns the pinned canonical `usageError` string `"provide a keyword, --model, or --provider"` when — after trimming — keyword AND both `--model`/`--provider` are all empty
- [x] Empty/whitespace flag values (`--model ""`, `--provider "   "`) are trimmed and treated as ABSENT — the guard never lets an empty flag trigger an unfiltered whole-index match
- [x] `--model`-only and `--provider`-only invocations (no positional keyword) succeed

**Manual Review:**
- [ ] Code reviewed and approved
