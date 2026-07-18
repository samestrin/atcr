# Acceptance Criteria: AXI Mode Preserves Existing Exit-Code Semantics

**Related User Story:** [02: Reconcile and Document the AXI Exit-Code Contract](../user-stories/02-reconcile-and-document-axi-exit-code-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command (cobra) | `cmd/atcr` binary, `RunE` error-return pattern |
| Test Framework | `go test` / `testify` (`assert`, `require`) | Matches existing style in `cmd/atcr/main_test.go` |
| Key Dependencies | `github.com/spf13/cobra`, standard `errors` package (`errors.As`) | No new dependencies required |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` - modify: no functional change expected to `exitFailure`/`exitUsage`/`exitAuth` constants or `exitCode()` (lines 126-165); AC verifies AXI-mode error paths continue to resolve through this single dispatch function with no `--axi`-specific branch added.
- `cmd/atcr/review.go` - modify: confirm `--axi` flag parsing/output-rendering error paths wrap errors with the existing `usageError()`/`authError()` helpers (same pattern as existing calls, e.g. `cmd/atcr/review.go:108-123`) rather than returning bare errors that fall through to `exitFailure`.
- `cmd/atcr/main_test.go` - modify: add/extend exit-code assertions covering `--axi` invocations of `atcr review`, `atcr report`, and `atcr reconcile --fail-on`, following the existing table-driven pattern (e.g. `main_test.go:92-133`).
- `docs/ci-integration.md` - reference: exit-semantics table (lines 11-19) is the canonical human-facing contract this AC verifies AXI mode does not diverge from.

## Happy Path Scenarios
**Scenario 1: Clean review under `--axi` exits 0**
- **Given** a review range with no findings at/above the configured `--fail-on` threshold
- **When** `atcr review --axi --fail-on high` is run
- **Then** the process exits with code `0`, identical to the non-`--axi` invocation of the same command against the same inputs

**Scenario 2: Gate-failure under `--axi` exits 1**
- **Given** a reconciled review with findings at/above the `--fail-on` threshold
- **When** `atcr reconcile --fail-on high --axi` is run
- **Then** the process exits with code `1`, identical to the non-`--axi` invocation

**Scenario 3: `atcr verify` cross-validation (no `--axi` needed)**
- **Given** `atcr verify`'s existing 0/1/2 = success/gate-failure/usage-error mapping (independently arrived at per `documentation/exit-code-cli-mcp-precedent.md`)
- **When** its exit-code table is compared against `atcr review`/`atcr reconcile`'s contract
- **Then** the mappings are confirmed identical, serving as cross-validation evidence cited in the Story-Specific DoD item for this AC

## Edge Cases
**Edge Case 1: `--axi` combined with `--fail-on` and a partial-success run**
- **Given** some review agents fail but at least one succeeds (partial success, per `docs/ci-integration.md`'s "Partial success is not failure" note)
- **When** `atcr review --axi --fail-on high` completes with `partial: true` and no surviving findings at/above threshold
- **Then** the process still exits `0` — `--axi` output formatting must not alter the partial-success-is-not-failure rule

**Edge Case 2: `atcr report --axi` against an existing reconciled review with zero findings**
- **Given** a prior review produced zero findings
- **When** `atcr report --axi` is run against that review's ID
- **Then** the process exits `0`, matching plain-text `atcr report` behavior for the same input

## Error Conditions
**Error Scenario 1: Usage/config error under `--axi`**
- **Given** an empty commit range or invalid `--fail-on` severity value passed alongside `--axi`
- **When** `atcr review --axi --fail-on bogus` is run
- **Then** the process exits with code `2` (usage error), via the existing `usageError()` wrapping — not a new or different code
- Error message: propagated verbatim from the existing usage-error path (e.g., `"invalid --fail-on severity: bogus"`)
- HTTP status / error code: N/A (CLI exit code `2`)

**Error Scenario 2: Auth failure under `--axi` with `--sync-cloud`**
- **Given** `ATCR_API_KEY` is unset or the remote returns 401/403
- **When** `atcr review --axi --sync-cloud` is run
- **Then** the process exits with code `3` (auth error), identical to non-`--axi` behavior
- Error message: `"ATCR_API_KEY is not set"` (or equivalent 401/403-derived message)
- HTTP status / error code: CLI exit code `3`

## Performance Requirements
- **Response Time:** Exit-code resolution adds no measurable overhead — it is a single `errors.As` type switch already on the hot path (`cmd/atcr/main.go:156-165`); no new I/O or computation introduced.
- **Throughput:** N/A — process-exit behavior, not a throughput-sensitive path.

## Security Considerations
- **Authentication/Authorization:** No change to the auth-error boundary; `--axi` must not create a bypass where an auth failure is misclassified as `exitUsage`/`exitFailure` and silently treated as retryable by an orchestrator.
- **Input Validation:** `--axi`-mode flag validation errors must be classified as `usageError` (exit 2), not left to fall through to the generic `exitFailure` (1), consistent with the Potential Risks table in the user story. Exception: `ATCR_AXI_MAX_LINES` is validated fail-open (warning + default 500, exit code unaffected) per AC 03-03 and AC 02-02's reconciled Edge Case 1 — it is not an exit-code event.

## Test Implementation Guidance
**Test Type:** UNIT (primary, via `cmd/atcr/main_test.go`) + INTEGRATION (subcommand-level exit-code assertions using the `execute()` test helper already defined at `main_test.go:25-34`)
**Test Data Requirements:** A fixture review/reconcile scenario producing a clean result, a gate-failure result, and an injected usage/config error (e.g., malformed `--fail-on` value), reused across `--axi` and non-`--axi` variants for direct comparison.
**Mock/Stub Requirements:** No network mocks required for the exit-code-only scenarios (usage/gate-failure); the auth-error scenario may reuse existing test doubles for `ATCR_API_KEY`/remote 401/403 simulation already present in the test suite.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A test asserts `atcr review --axi`, `atcr report --axi`, and `atcr reconcile --fail-on --axi` return exit codes `0`/`1`/`2`/`3` matching their non-`--axi` counterparts for equivalent inputs
- [ ] No `--axi`-specific branch exists in `exitCode()` (`cmd/atcr/main.go:156`) — confirmed by code inspection
- [ ] `atcr verify`'s exit-code table is cited as cross-validation evidence in test comments or documentation
- [ ] Partial-success-is-not-failure behavior is confirmed unaffected by `--axi`

**Manual Review:**
- [ ] Code reviewed and approved
