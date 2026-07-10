# Acceptance Criteria: `atcr models check` Command Registration and Human-Readable Drift Report

**Related User Story:** [05: `atcr models check` Drift Report](../user-stories/05-atcr-models-check-drift-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra command family (`cmd/atcr/models.go`) | New top-level `models` command with `check` as first subcommand, mirroring `cmd/atcr/personas.go` conventions |
| Test Framework | Go `testing` + `testify` (`assert`/`require`) | Table-driven, following `cmd/atcr/personas_test.go`'s existing style |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas` (`ListTiers`), `text/tabwriter` | No new external dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/models.go` — create: define `newModelsCmd()` (top-level, no `RunE` of its own) and `newModelsCheckCmd()` (`check` subcommand); `check`'s `RunE` enumerates installed personas via `internal/personas.ListTiers`, reads each persona's resolved lock, diffs it against the catalog snapshot, and renders a human-readable table/text report via `cmd.OutOrStdout()`.
- `cmd/atcr/models_test.go` — create: integration tests asserting `atcr models check` is reachable, enumerates installed personas the same way `atcr personas list` does, and renders the three condition types in the documented human-readable line format.
- `cmd/atcr/main.go:202` (`newRootCmd` `AddCommand` list) — modify: register `newModelsCmd()` alongside `newPersonasCmd()`, `newDoctorCmd()`, `newDebtCmd()`, etc.
- `internal/personas/list.go:53` (`ListTiers`) — reference: reused directly for installed-persona enumeration/dedup; not modified by this AC.
- `cmd/atcr/personas.go` — reference: sibling command conventions for output streams, flag handling, and table rendering to mirror.
- `internal/personas/testdata/catalog_snapshot.json` — create: the checked-in deterministic catalog fixture used by the default comparison path.

## Happy Path Scenarios
**Scenario 1: `models check` is registered and runs with no arguments**
- **Given** the built root Cobra command tree
- **When** `atcr models check` is invoked with no flags
- **Then** the command is found (no "unknown command" error), it enumerates installed personas via the same tier-walking/dedup logic as `atcr personas list`, and produces human-readable output on `cmd.OutOrStdout()`

**Scenario 2: Newer-family-member drift renders in the documented line format**
- **Given** an installed persona `anthony` whose resolved lock is `anthropic/claude-opus-4.8` and the catalog snapshot's `anthropic/claude-opus` family has a newer stable-channel member `anthropic/claude-opus-5.0` (family membership for the newer-member comparison is derived from the persona's binding family/channel when present, falling back to the locked slug's vendor+family prefix — so an alias-covered persona is compared against the same family the resolver would upgrade it within)
- **When** `atcr models check` runs
- **Then** stdout contains a line matching `anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0 (newer member)`

**Scenario 3: Deprecation and missing-slug conditions render in the documented line formats**
- **Given** an installed persona `gene` locked to a slug whose catalog entry has a non-null `expiration_date` of `2026-09-01`, and an installed persona `milo` locked to a slug absent from the catalog snapshot
- **When** `atcr models check` runs
- **Then** stdout contains `gene: google/gemini-pro-1.5 has expiration 2026-09-01 (deprecation)` and `milo: openai/gpt-4o-missing no longer in catalog (missing)`

**Scenario 4: No drift, deprecation, or missing conditions found**
- **Given** every installed persona's resolved lock matches the newest stable-channel catalog entry for its family, with no non-null `expiration_date` and no missing slug
- **When** `atcr models check` runs
- **Then** stdout reports the exact canonical message `No drift, deprecation, or missing-slug conditions found.` (a single stable, scriptable string) rather than empty output, so the absence of drift is visibly confirmed rather than silently implied

## Edge Cases
**Edge Case 1: No personas installed**
- **Given** no community personas are installed (built-ins only, none of which carry a resolved lock requiring comparison)
- **When** `atcr models check` runs
- **Then** the command completes without error and reports that there is nothing to check, rather than iterating zero personas silently or panicking on an empty slice

**Edge Case 2: A persona has multiple conditions simultaneously**
- **Given** a persona whose locked slug both has a non-null `expiration_date` and is also behind a newer family member
- **When** `atcr models check` runs
- **Then** both conditions are reported for that persona as one line per condition (one line per (persona, condition) pair — matching the one-object-per-condition JSON contract in AC 05-02), and neither condition is silently dropped in favor of the other

**Edge Case 3: Persona enumeration matches `atcr personas list` exactly**
- **Given** an installed persona set spanning built-in, community, and project tiers with a name collision across tiers
- **When** both `atcr personas list` and `atcr models check` run against the same installed state
- **Then** the set of persona names each command considers is identical (same tier-precedence, same dedup-by-name behavior from `ListTiers`)

## Error Conditions
**Error Scenario 1: A persona's resolved lock cannot be read (corrupt or missing lock field)**
- Error message: `"failed to read resolved lock for persona %q: %w"`
- HTTP status / error code: N/A (CLI); the offending persona is reported as a per-persona failure line on `cmd.ErrOrStderr()` and excluded from the drift table, without aborting the check for other personas

## Performance Requirements
- **Response Time:** `atcr models check` completes in well under 1 second for a typical installed-persona count (tens of personas) since it only reads local lock data and an in-repo JSON snapshot — no network I/O in the default path.
- **Throughput:** O(n) in the number of installed personas; no repeated catalog re-parsing per persona (the snapshot is loaded once and reused).

## Security Considerations
- **Authentication/Authorization:** Not applicable — read-only, local-only operation in the default path; no credentials are read or required.
- **Input Validation:** Persona names and slugs read from installed lock files are treated as opaque strings for comparison and display; no shell/command execution or path construction from these values beyond existing `ListTiers` file-walking safeguards.

## Test Implementation Guidance
**Test Type:** INTEGRATION (Cobra command execution via `cmd.SetOut`/`cmd.SetArgs`, following `cmd/atcr/personas_test.go`'s pattern) + UNIT (drift classification logic in isolation)
**Test Data Requirements:** Fixture installed personas (via a temp community/project dir) with resolved locks representing: up-to-date, newer-member-available, deprecated, missing-from-catalog, and multi-condition states; a fixture catalog snapshot JSON covering the same families/slugs.
**Mock/Stub Requirements:** No network mocking needed for this AC (default path never calls the network); filesystem fixtures only, reusing `ListTiers`' existing temp-dir test conventions.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr models check` is registered as a Cobra subcommand reachable from the root command, alongside `personas`, `doctor`, `debt`
- [ ] Persona enumeration reuses `ListTiers` and matches `atcr personas list`'s persona set exactly
- [ ] All three condition types (newer member, deprecation, missing) render in the documented human-readable line formats, including multi-condition personas
- [ ] A no-issues state and a no-installed-personas state both produce explicit, non-empty confirmation output rather than blank stdout

**Manual Review:**
- [ ] Code reviewed and approved
