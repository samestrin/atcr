# Acceptance Criteria: New AXI-Introduced Errors Classify Into the Existing Contract

**Related User Story:** [02: Reconcile and Document the AXI Exit-Code Contract](../user-stories/02-reconcile-and-document-axi-exit-code-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (cobra) — error classification layer | Reuses `codedError` pattern, no new type |
| Test Framework | `go test` / `testify` | Matches `cmd/atcr/main_test.go` conventions |
| Key Dependencies | Standard `errors` package (`errors.As`, `%w` wrapping) | No new dependencies |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` - modify: no new exit-code constants added; any new AXI error source is wrapped via the existing `usageError()`/`authError()` helpers (lines 142-153) exactly as pre-existing config errors are (e.g., `LOG_LEVEL`/`--log-format` validation per `main_test.go:237,251`). Note: a malformed `ATCR_AXI_MAX_LINES` value is deliberately NOT an error source — AC 03-03 owns it as a fail-open warn-and-default path (exit code unaffected), matching the `telemetryEnabledFromEnv` precedent recorded in `codebase-discovery.json`.
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
**Edge Case 1: Malformed `ATCR_AXI_MAX_LINES` value (reconciled with AC 03-03)**
- **Given** `ATCR_AXI_MAX_LINES=not-a-number` (or a non-positive integer) is set in the environment
- **When** `atcr review --axi` is run
- **Then** no usage error is raised: the value falls open to the default 500-line cap with exactly one `stderr` warning, and the exit code continues to reflect the review outcome only — AC 03-03 owns this fail-open contract (mirroring `telemetryEnabledFromEnv`), so the exit-code point verified here is that the env-var path introduces no new code and never hijacks `exitUsage`/`exitFailure`

**Edge Case 2: Unsupported format combination with `--axi`**
- **Given** `--axi` is combined with an incompatible existing flag (e.g., a format flag `--axi` is not designed to compose with, if any exists)
- **When** the combination is passed
- **Then** the process exits `2` (usage error) with a clear message naming the conflicting flags, following the existing mutually-exclusive-flag pattern (e.g., `review.go:108,205`)

**Edge Case 3: AXI output rendering fails after a successful review**
- **Given** the underlying review/reconcile succeeded but AXI-mode TOON/JSON serialization encounters an internal rendering fault (not operator-fixable)
- **When** `atcr report --axi` is run
- **Then** the process exits `1` (generic failure), not `2` — because this is not an operator-fixable configuration problem, distinguishing render-time faults from config errors per the story's error-classification rule

## Error Conditions
**Error Scenario 1: Invalid pagination env var (non-error, fail-open per AC 03-03)**
- **Given** `ATCR_AXI_MAX_LINES=not-a-number` is set in the environment
- **When** `atcr review --axi` resolves the pagination cap
- **Then** the cap falls open to 500, exactly one `stderr` warning line is emitted, and the exit code is unaffected (no usage error is raised) — the full contract and message shape are owned by AC 03-03
- Warning text (stderr, not an error message): e.g. `warning: unrecognized ATCR_AXI_MAX_LINES value "not-a-number"; using default 500` (owned by AC 03-03)
- HTTP status / error code: none — no error path is entered; the exit code is unchanged

**Error Scenario 2: Unsupported `--axi` flag combination**
- **Given** `--axi` is combined with a flag it is not designed to compose with
- **When** flag-combination validation runs (early `RunE` validation, before review work begins)
- **Then** the process exits `2` (usage error) with a message naming the conflicting flags
- Error message: `"--axi and <conflicting-flag> are mutually exclusive"` (pattern matches `review.go:108`)
- HTTP status / error code: CLI exit code `2`

**Error Scenario 3: Internal AXI rendering fault**
- **Given** the underlying review/reconcile succeeded but AXI-mode serialization hits an internal rendering fault
- **When** `atcr report --axi` attempts to render the payload
- **Then** the process exits `1` (generic failure) with the rendering error wrapped for context
- Error message: propagated from the underlying rendering error, wrapped with context (e.g., `"axi output rendering failed: %w"`), left unwrapped by `usageError`/`authError` so it defaults to `exitFailure`
- HTTP status / error code: CLI exit code `1`

## Performance Requirements
- **Response Time:** Env-var and flag validation must complete and reject invalid input before any review/reconcile work begins, consistent with existing usage-error checks that run in `PersistentPreRunE`/early `RunE` validation.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** No auth-relevant surface introduced by pagination/format validation; must not be misclassified as `exitAuth` (3).
- **Input Validation:** All new AXI-introduced inputs must be validated and explicitly classified (`usageError` for operator-fixable flag/config errors, unwrapped error for internal/review-outcome faults) rather than left to an unclassified default — closing the exact gap flagged in the story's Potential Risks table (misclassifying config errors as review-outcome failures). Deliberate exception: `ATCR_AXI_MAX_LINES` misconfiguration is classified as a non-error fail-open path (warning + default, owned by AC 03-03), consistent with the codebase's `telemetryEnabledFromEnv` posture — it must therefore never surface as exit 1 or 2.

## Test Implementation Guidance
**Test Type:** UNIT — table-driven tests in `cmd/atcr/main_test.go` and/or a new `cmd/atcr/axi_test.go` if AXI-specific validation logic is isolated into its own file
**Test Data Requirements:** Enumerated list of every new AXI-introduced error source with the expected outcome for each: pagination env var (fail-open warning, exit code unaffected — owned by AC 03-03), unsupported flag combinations (exit `2`), internal rendering faults (exit `1`)
**Mock/Stub Requirements:** None required for env-var/flag validation (pure unit tests); rendering-fault scenario may need a stub/fake AXI formatter that returns a forced error to exercise the `exitFailure` path without a real serialization bug

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Every new AXI-introduced error source is enumerated and each maps to `usageError` (exit 2) or leaves the error unwrapped (exit 1, generic failure) per the operator-fixable vs. not distinction
- [x] No new exit code (e.g., a repurposed or new "4") is introduced for any AXI error condition
- [x] Malformed `ATCR_AXI_MAX_LINES` is covered by a test asserting the fail-open contract (default 500, exactly one stderr warning, exit code unaffected) per AC 03-03; unsupported `--axi` flag combinations are covered by explicit test cases asserting exit code `2`
- [x] An internal (non-operator-fixable) AXI rendering fault is covered by a test case asserting exit code `1`

**Manual Review:**
- [x] Code reviewed and approved
