# Acceptance Criteria: `--sync-cloud` Flag Registration

**Related User Story:** [04: `--sync-cloud` Authenticated Push](../user-stories/04-sync-cloud-authenticated-push.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`cmd/atcr`) | New `addSyncCloudFlags` helper following the existing `addRangeFlags` pattern |
| Test Framework | `go test` (standard `testing`, `spf13/cobra` test harness) | Test file: `cmd/atcr/flags_test.go` |
| Key Dependencies | `github.com/spf13/cobra`, Go stdlib `errors` | Matches the existing `addRangeFlags`/`validateRangeFlags` dependency set |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/flags.go` - modify: add `addSyncCloudFlags(cmd *cobra.Command)` alongside the existing `addRangeFlags` (`cmd/atcr/flags.go:14`), registering a `--sync-cloud` bool flag and an optional `--cloud-endpoint` string override flag (defaulting to the documented `atcr.dev/dashboard` endpoint), chaining into `cmd.PreRunE` using the same "chain rather than assign" convention used by `addRangeFlags`.
- `cmd/atcr/review.go` - modify: call `addSyncCloudFlags(cmd)` at flag-registration time for the `review` subcommand (alongside the existing `addRangeFlags(cmd)` call in the `review` subcommand constructor).
- `cmd/atcr/reconcile.go` - modify: call `addSyncCloudFlags(cmd)` at flag-registration time for the `reconcile` subcommand (`cmd/atcr/reconcile.go:47` area, alongside the existing `--no-scorecard` flag registration).
- `cmd/atcr/flags_test.go` - create or modify: unit tests asserting both subcommands expose `--sync-cloud` and `--cloud-endpoint`.

## Happy Path Scenarios
**Scenario 1: `--sync-cloud` flag is present on `review`**
- **Given** the `atcr review` command is constructed via `newRootCmd`
- **When** `cmd.Flags().Lookup("sync-cloud")` is queried
- **Then** the flag exists, defaults to `false`, and is a bool flag

**Scenario 2: `--sync-cloud` flag is present on `reconcile`**
- **Given** the `atcr reconcile` command is constructed via `newRootCmd`
- **When** `cmd.Flags().Lookup("sync-cloud")` is queried
- **Then** the flag exists, defaults to `false`, and is a bool flag

**Scenario 3: `--cloud-endpoint` override flag is present**
- **Given** either `review` or `reconcile` is constructed
- **When** `cmd.Flags().Lookup("cloud-endpoint")` is queried
- **Then** the flag exists as a string flag defaulting to the documented `atcr.dev/dashboard` endpoint, allowing tests to point at a mock server

## Edge Cases
**Edge Case 1: `--cloud-endpoint` set without `--sync-cloud`**
- **Given** `--cloud-endpoint` is set but `--sync-cloud` is not passed
- **When** the command's `PreRunE` chain runs
- **Then** no validation error occurs — `--cloud-endpoint` is inert unless `--sync-cloud` is also set (no push is attempted)

**Edge Case 2: `PreRunE` chaining preserves prior hooks**
- **Given** `addRangeFlags` has already installed a `PreRunE` hook on the command (both `review` and `reconcile` also register range flags)
- **When** `addSyncCloudFlags` installs its own `PreRunE` hook afterward
- **Then** both hooks run in sequence (neither is silently overwritten), matching the existing `prev := cmd.PreRunE` chaining idiom in `cmd/atcr/flags.go`

## Error Conditions
**Error Scenario 1: Flag registered twice**
- Not applicable at the CLI-usage level — this is a build-time/wiring concern; `addSyncCloudFlags` must only be called once per command during `newRootCmd` construction. Covered by a code-level review check rather than a runtime error message.

## Performance Requirements
- **Response Time:** Flag registration is a one-time cost at command construction; no measurable runtime overhead per invocation.
- **Throughput:** N/A — registration only.

## Security Considerations
- **Authentication/Authorization:** N/A at the flag-registration level; auth is handled in AC 04-02/04-03/04-04.
- **Input Validation:** `--cloud-endpoint` accepts any string at registration time; URL well-formedness is validated at push time (AC 04-02), not at flag-parse time, to keep this AC narrowly scoped to wiring.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** None beyond constructed `cobra.Command` instances for `review` and `reconcile`.
**Mock/Stub Requirements:** None — pure flag-registration assertions via `cmd.Flags().Lookup(...)`.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `addSyncCloudFlags` is defined in `cmd/atcr/flags.go` following the `addRangeFlags` PreRunE-chaining convention
- [ ] `--sync-cloud` and `--cloud-endpoint` are registered on both `review` and `reconcile`
- [ ] Existing `PreRunE` hooks from `addRangeFlags` are preserved (not overwritten) when `addSyncCloudFlags` also installs a hook

**Manual Review:**
- [ ] Code reviewed and approved
