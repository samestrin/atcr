# Acceptance Criteria: `--all` Fan-Out and `--dry-run` Report the Full Before→After Slug Set Without Writing

**Related User Story:** [04: Reproducible Upgrade with Before→After Lock Reporting](../user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | CLI flag-behavior extension (`cmd/atcr/personas.go`) over the existing `runPersonaUpgrades()` loop | Existing `--all`/`--dry-run` flag semantics (`personas.go:363-364`) are unchanged; this AC extends what the shared report line contains |
| Test Framework | `testing` + `testify`, integration-style CLI execution tests following `TestPersonasUpgrade_AllEmpty` (`personas_test.go:642`) and `TestPersonasUpgrade_Integration` (`personas_test.go:538`) patterns | |
| Key Dependencies | Story 3's hybrid resolver, Story 2's lock field, existing `installedCommunityNames()` (`personas.go:396`) and `writePersonaUnit()` (`internal/personas/unit.go:95`) | No new flags or command are introduced |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/personas.go:371` (`runPersonaUpgrades()`) — modify: compute and print the before→after (or unchanged) resolved-slug line for every persona in `names`, sharing one report-computation code path between `--dry-run` and real-run modes per the story's risk mitigation (differ only in whether the final write executes).
- `internal/personas/upgrade.go:61-63` (`Upgrade()` `dryRun` branch) — modify: return the fully computed `UpgradeResult` — including the resolved before/after slug — before the `writePersonaUnit()` call, so dry-run and real-run share identical computation up to the write.
- `cmd/atcr/personas_test.go` — modify: extend `TestPersonasUpgrade_AllEmpty` sibling coverage with a populated multi-persona `--all` case, and add a `--dry-run` case asserting no lock file bytes change on disk after the run.
- `internal/personas/upgrade_test.go` — modify: add a test asserting `dryRun=true` produces the same `UpgradeResult` (from/to slugs) as `dryRun=false` would, except no file write occurs.
- `internal/personas/unit.go:95` (`writePersonaUnit`) — reference: the shared paired-write tail that is skipped under `--dry-run`.
- `cmd/atcr/personas.go:356` (`installedCommunityNames()`) — reference: the existing helper used by `--all` to enumerate installed community personas.

## Happy Path Scenarios
**Scenario 1: `--all` reports one line per installed persona in a single run**
- **Given** three community personas are installed, two with resolvable slug changes and one already at its resolved slug
- **When** the maintainer runs `atcr personas upgrade --all`
- **Then** the command prints exactly one before→after (or unchanged) line per persona — e.g. `anthony: opus-4.8 → 5.0`, `delia: deepseek/r2-0623 → deepseek/r2-0714`, `quinn: qwen/qwen3-32b (unchanged)` — and writes the lock for the two that changed

**Scenario 2: `--dry-run` shows the identical report with zero writes**
- **Given** the same three-persona install state as Scenario 1
- **When** the maintainer runs `atcr personas upgrade --all --dry-run`
- **Then** the printed report is identical in content to the real-run report (same before/after slugs, same set of personas), but no persona unit file on disk changes (verified by byte-for-byte comparison or mtime check before/after)

## Edge Cases
**Edge Case 1: No community personas installed with `--all`**
- **Given** zero installed community personas
- **When** the maintainer runs `atcr personas upgrade --all` (with or without `--dry-run`)
- **Then** the existing `"No community personas installed"` message (`personas.go:356`) is printed and the command exits `0` — unchanged from current behavior, confirming this story's additions did not regress the empty-set path

**Edge Case 2: `--all --dry-run` where one persona would fail resolution**
- **Given** one of several installed personas has a binding the resolver cannot resolve (e.g. transient catalog fetch failure)
- **When** the maintainer runs `atcr personas upgrade --all --dry-run`
- **Then** the failing persona is reported via the existing per-persona failure line (`"failed to upgrade %s: %v (skipping)"`, `personas.go:376`) and the run continues to report the remaining personas' before→after lines; the command still exits non-zero overall (existing `failed` aggregation, `personas.go:389`) even though it was a dry run

## Error Conditions
**Error Scenario 1: `--dry-run` combined with a persona whose major-version resolution would trip Story 6's fixture-repass gate**
- Error message: report line explicitly states the would-be new slug and the reason it is blocked, e.g. `anthony: opus-4.8 → 5.0 (blocked: fixture repass required for major-version bump)`
- HTTP status / error code: N/A; command exits non-zero to signal a blocked-but-reportable condition, and no lock write occurs (real run or dry run)

**Error Scenario 2: `--all` and a persona name are both specified**
- Error message: `"cannot specify both a persona name and --all"` (existing `usageError`, `personas.go:343`)
- HTTP status / error code: N/A (CLI usage error, unchanged by this story — regression-covered by existing `TestPersonasUpgrade_ConflictExitsUsage`, `personas_test.go:626`)

## Performance Requirements
- **Response Time:** `--all` fan-out remains sequential per the existing loop in `runPersonaUpgrades()`; per-persona time budget matches AC 04-01 (one resolver/catalog fetch per persona, no batching optimization required by this story)
- **Throughput:** A `--dry-run` invocation performs identical fetch/resolve work to a real run (report parity requirement) — it must not skip the resolver call to "go faster," since that would violate the shared-computation guarantee this AC tests for

## Security Considerations
- **Authentication/Authorization:** No change from AC 04-01/04-02 — `--dry-run` still requires the same `HTTPClient`/API-key access as a real run, since it must fetch and resolve identically, only skipping the final write
- **Input Validation:** Dry-run mode must not weaken validation — `registry.ValidateCommunityPersonaYAML` still runs against fetched remote content before the resolved slug is reported, so a dry-run report never surfaces an unvalidated slug as if it were trustworthy

## Test Implementation Guidance
**Test Type:** INTEGRATION (full `atcr personas upgrade --all [--dry-run]` CLI execution, following `TestPersonasUpgrade_AllEmpty` and `TestPersonasUpgrade_Integration` conventions) + UNIT (dry-run/real-run `UpgradeResult` parity in `upgrade_test.go`)
**Test Data Requirements:** A temp personas directory with 2-3 installed fixture persona units at known prior locked slugs; a fake `HTTPClient`/resolver stub returning deterministic new slugs, one unchanged case, and one resolver-failure case
**Mock/Stub Requirements:** Reuse the fake `HTTPClient` conventions from `internal/personas/client_test.go`/`upgrade_test.go`; assert no filesystem mutation in dry-run via `os.Stat` mtime/size comparison or content hash before and after the run

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--all` prints one before→after (or unchanged) resolved-slug line per installed persona in a single run
- [ ] `--dry-run` produces a report identical in content to what a real run would produce, while leaving every persona unit file on disk byte-for-byte unchanged
- [ ] The empty-install-set and flag-conflict edge cases (`--all` with no personas installed; `--all` plus a name) remain regression-covered and unchanged from current behavior
- [ ] A blocked major-version bump (Story 6 fixture-repass gate) is reported with its would-be slug and blocking reason in both dry-run and real-run modes, without writing the lock

**Manual Review:**
- [ ] Code reviewed and approved
