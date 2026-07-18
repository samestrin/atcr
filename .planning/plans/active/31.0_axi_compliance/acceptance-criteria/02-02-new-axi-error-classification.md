# Acceptance Criteria: New AXI-Introduced Errors Classify Into the Existing Contract

**Related User Story:** [02: Reconcile and Document the AXI Exit-Code Contract](../user-stories/02-reconcile-and-document-axi-exit-code-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (cobra) — error classification layer | Reuses `codedError` pattern, no new type |
| Test Framework | `go test` / `testify` | Matches `cmd/atcr/main_test.go` conventions |
| Key Dependencies | Standard `errors` package (`errors.As`, `%w` wrapping) | No new dependencies |

## Related Files
- `cmd/atcr/main.go` - modify: no new exit-code constants added; any new AXI error source (e.g., malformed `ATCR_AXI_MAX_LINES` pagination env var) is wrapped via the existing `usageError()`/`authError()` helpers (lines 142-153) exactly as pre-existing config errors are (e.g., `LOG_LEVEL`/`--log-format` validation per `main_test.go:237,251`).
- `cmd/atcr/review.go` - modify: `--axi` flag-combination validation (e.g., mutually-exclusive or unsupported format combinations) follows the existing `usageError(errors.New(...))` pattern already used for other flag-group violations (e.g., `review.go:205,260,290,299`).
- `cmd/atcr/main_test.go` - modify: add table-driven test cases covering each new AXI-introduced error source, asserting `exitCode(err) == 2` (or `1` where appropriate per the operator-fixable vs. review-outcome distinction), following the existing pattern at `main_test.go:151,160,165`.
- `docs/ci-integration.md` - reference: exit-semantics table (lines 11-19); no new row is added since no new code is introduced — only existing rows 1/2 continue to cover these cases.

## Happy Path Scenarios
**Scenario 1: Valid `ATCR_AXI_MAX_LINES` value accepted**
- **Given** `ATCR_AXI_MAX_LINES` is set to a valid positive integer
- **When** `atcr review --axi` is run
- **Then** the value is applied to pagination with no error, and the exit code reflects the review outcome only (0/1), not a usage error

**Scenario 2: Supported `--axi` + `--fail-on` flag combination**
- **Given** `--axi` is combined with `--fail-on high`, a supported combination
- **When** `atcr review --axi --fail-on high` is run
- **Then** the command proceeds normally with no usage error introduced by the combination itself

## Edge Cases
**Edge Case 1: Malformed `ATCR_AXI_MAX_LINES` value**
- **Given** `ATCR_AXI_MAX_LINES=not-a-number` (or a non-positive integer) is set in the environment
- **When** `atcr review --axi` is run
- **Then** the process exits `2` (usage error) via `usageError()` — not `1` (generic failure) — because this is an operator-fixable configuration problem, per the Potential Risks table in the user story

**Edge Case 2: Unsupported format combination with `--axi`**
- **Given** `--axi` is combined with an incompatible existing flag (e.g., a format flag `--axi` is not designed to compose with, if any exists)
- **When** the combination is passed
- **Then** the process exits `2` (usage error) with a clear message naming the conflicting flags, following the existing mutually-exclusive-flag pattern (e.g., `review.go:108,205`)

**Edge Case 3: AXI output rendering fails after a successful review**
- **Given** the underlying review/reconcile succeeded but AXI-mode TOON/JSON serialization encounters an internal rendering fault (not operator-fixable)
- **When** `atcr report --axi` is run
- **Then** the process exits `1` (generic failure), not `2` — because this is not an operator-fixable configuration problem, distinguishing render-time faults from config errors per the story's error-classification rule

## Error Conditions
**Error Scenario 1: Invalid pagination env var**
- Error message: `"invalid ATCR_AXI_MAX_LINES value: \"not-a-number\" (must be a positive integer)"` (or equivalent, following existing message conventions, e.g. `main.go` LOG_LEVEL validation)
- HTTP status / error code: CLI exit code `2`

**Error Scenario 2: Unsupported `--axi` flag combination**
- Error message: `"--axi and <conflicting-flag> are mutually exclusive"` (pattern matches `review.go:108`)
- HTTP status / error code: CLI exit code `2`

**Error Scenario 3: Internal AXI rendering fault**
- Error message: propagated from the underlying rendering error, wrapped with context (e.g., `"axi output rendering failed: %w"`), left unwrapped by `usageError`/`authError` so it defaults to `exitFailure`
- HTTP status / error code: CLI exit code `1`

## Performance Requirements
- **Response Time:** Env-var and flag validation must fail fast (before any review/reconcile work begins), consistent with existing usage-error checks that run in `PersistentPreRunE`/early `RunE` validation.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** No auth-relevant surface introduced by pagination/format validation; must not be misclassified as `exitAuth` (3).
- **Input Validation:** All new AXI-introduced inputs (env vars, flag combinations) must be validated and explicitly classified (`usageError` for operator-fixable config, unwrapped error for internal/review-outcome faults) rather than left to an unclassified default — closing the exact gap flagged in the story's Potential Risks table (misclassifying config errors as review-outcome failures).

## Test Implementation Guidance
**Test Type:** UNIT — table-driven tests in `cmd/atcr/main_test.go` and/or a new `cmd/atcr/axi_test.go` if AXI-specific validation logic is isolated into its own file
**Test Data Requirements:** Enumerated list of every new AXI-introduced error source (pagination env var, unsupported flag combinations, internal rendering faults) with expected exit code for each
**Mock/Stub Requirements:** None required for env-var/flag validation (pure unit tests); rendering-fault scenario may need a stub/fake AXI formatter that returns a forced error to exercise the `exitFailure` path without a real serialization bug

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Every new AXI-introduced error source is enumerated and each maps to `usageError` (exit 2) or leaves the error unwrapped (exit 1, generic failure) per the operator-fixable vs. not distinction
- [ ] No new exit code (e.g., a repurposed or new "4") is introduced for any AXI error condition
- [ ] Malformed `ATCR_AXI_MAX_LINES` and any unsupported `--axi` flag combination are covered by explicit test cases asserting exit code `2`
- [ ] An internal (non-operator-fixable) AXI rendering fault is covered by a test case asserting exit code `1`

**Manual Review:**
- [ ] Code reviewed and approved
