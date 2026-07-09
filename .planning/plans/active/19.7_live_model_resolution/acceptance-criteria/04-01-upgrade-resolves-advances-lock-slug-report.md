# Acceptance Criteria: Upgrade Re-Resolves Binding and Advances Lock with Before→After Slug Reporting

**Related User Story:** [04: Reproducible Upgrade with Before→After Lock Reporting](../user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go library function extension (`internal/personas/upgrade.go`) + CLI reporting extension (`cmd/atcr/personas.go`) | No new command surface; extends the existing `atcr personas upgrade <name>` path |
| Test Framework | `testing` + `testify` (`assert`/`require`), table-driven, following `internal/personas/upgrade_test.go`'s existing style | Mirrors `TestIsNewer_MixedValidityTreatsAsUpToDate`'s table pattern |
| Key Dependencies | Story 3's hybrid resolver function, Story 2's lock field on the persona unit/`PersonaIndexEntry`, existing `isNewer()` (`upgrade.go:89`), `writePersonaUnit()` (`internal/personas/unit.go:95`) | No new external dependency; this AC is pure composition of already-planned internals |

### Related Files (from codebase-discovery.json)
- `internal/personas/upgrade.go:27` (`Upgrade()`) — modify: insert the resolver call immediately before the existing `isNewer`/write logic; extend `UpgradeResult` with resolved-slug before/after fields consumed by the CLI report.
- `internal/personas/upgrade_test.go` — modify: add cases asserting `Upgrade()` calls the resolver, compares resolved slugs (not just version strings), and writes the lock only on a passing version-advance comparison.
- `cmd/atcr/personas.go:371` (`runPersonaUpgrades()`) — modify: extend reporting to print the resolved-slug before→after line per persona, alongside the existing version report.
- `cmd/atcr/personas_test.go:538` (`TestPersonasUpgrade_Integration`) — modify: extend to assert the `name: old-slug → new-slug` line appears in stdout.
- `internal/personas/unit.go:95` (`writePersonaUnit`) — reference: the shared paired-write tail used by both `InstallUnit` and `Upgrade`; the resolved lock is persisted through here.
- `internal/personas/catalog.go` — reference: Story 3's hybrid resolver function invoked from `Upgrade()`.

## Happy Path Scenarios
**Scenario 1: Single-name upgrade advances the lock and reports the slug change**
- **Given** persona `anthony` is installed with a locked resolved slug of `opus-4.8`, and Story 3's resolver — invoked with `anthony`'s family/channel binding against the current catalog — now resolves to `5.0`
- **When** the maintainer runs `atcr personas upgrade anthony`
- **Then** `Upgrade()` calls the resolver before its existing `isNewer`/write logic, `isNewer` confirms `5.0` is a version advance over `opus-4.8`, the lock is written via `writePersonaUnit()`, and the command prints `anthony: opus-4.8 → 5.0` to stdout before exiting `0`

**Scenario 2: Resolved slug is unchanged**
- **Given** persona `anthony` is installed with a locked resolved slug of `5.0`, and the resolver re-resolves the same binding to `5.0` again
- **When** the maintainer runs `atcr personas upgrade anthony`
- **Then** no lock write occurs, and the command prints an explicit unchanged line (e.g. `anthony: 5.0 (unchanged)`) rather than omitting the persona from output

## Edge Cases
**Edge Case 1: Version string advances but resolved slug is identical**
- **Given** the persona's YAML `version` metadata field increments (e.g. persona-authoring metadata bump) but the resolver returns the same concrete slug as the current lock
- **When** `atcr personas upgrade <name>` runs
- **Then** the report reflects the resolved-slug comparison (unchanged) independently of the version-string comparison, since this story extends `isNewer` to compare resolved slugs, not only version strings — the two are reported distinctly, not conflated

**Edge Case 2: Persona has no prior lock entry (first upgrade after Story 2 migration)**
- **Given** an installed persona predates Story 2's lock field and has no previously resolved slug recorded
- **When** `atcr personas upgrade <name>` runs
- **Then** the before side of the report renders as an explicit placeholder (e.g. `(none)`) rather than an empty string or a crash, and the resolved slug is written as the new lock value

## Error Conditions
**Error Scenario 1: Resolver cannot resolve the persona's binding (e.g. vendor prefix yields zero catalog matches)**
- Error message: `"failed to resolve model binding for persona %q: %w"` (wrapping the resolver's underlying error)
- HTTP status / error code: N/A (library-level Go error); the CLI reports it via the existing `"failed to upgrade %s: %v (skipping)"` path (`personas.go:376`) and the command exits non-zero, consistent with existing fetch/validation failure handling

**Error Scenario 2: Resolved slug fails validation before it can be written**
- Error message: `"persona %q failed validation: %w"` (existing `registry.ValidateCommunityPersonaYAML` error, now also covering the resolved-slug field)
- HTTP status / error code: N/A; no lock write occurs and the previously installed persona is left untouched

## Performance Requirements
- **Response Time:** A single-name upgrade completes within the existing fetch + validate + resolve budget; resolver invocation adds at most one additional catalog fetch (already bounded by `client.go`'s `fetchTimeout` convention), no additional round trip per persona beyond what Story 3's resolver requires
- **Throughput:** N/A — this AC covers single-name invocation; `--all` fan-out throughput is covered by AC 04-03

## Security Considerations
- **Authentication/Authorization:** Reuses the existing `HTTPClient`/API-key injection already proven in `internal/personas/client.go` for the resolver's catalog fetch; no new credential handling introduced by this AC
- **Input Validation:** The resolved slug is passed through `registry.ValidateCommunityPersonaYAML` before any write, exactly as remote-fetched persona content already is (`upgrade.go:43`) — an invalid or malformed resolved slug never reaches the lock file

## Test Implementation Guidance
**Test Type:** UNIT (for `Upgrade()`/resolver composition) + INTEGRATION (for CLI report text, per existing `TestPersonasUpgrade_Integration` pattern with an `httptest`-backed fake client)
**Test Data Requirements:** A fixture persona YAML with a known prior locked slug; a fake/stub resolver (or catalog snapshot fixture per Story 3) returning a deterministic newer slug, an identical slug, and a no-match error case
**Mock/Stub Requirements:** `HTTPClient` fake already used in `upgrade_test.go`/`client_test.go`; a resolver seam (function value or small interface) that tests can substitute so this AC's tests do not depend on Story 3's concrete implementation landing first

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `Upgrade()` invokes the resolver before its existing `isNewer`/write logic and only writes the lock on a real, version-advancing slug change
- [ ] `atcr personas upgrade <name>` prints an explicit `name: old-slug → new-slug` line on change and an explicit unchanged line otherwise — no persona is silently omitted
- [ ] A persona with no prior lock entry renders a placeholder before-slug rather than erroring or printing an empty value
- [ ] Resolver and validation failures are reported per-persona without crashing the command or corrupting the existing installed persona

**Manual Review:**
- [ ] Code reviewed and approved
