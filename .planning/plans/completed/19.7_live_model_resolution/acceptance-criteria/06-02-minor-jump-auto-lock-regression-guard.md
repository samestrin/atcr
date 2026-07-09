# Acceptance Criteria: Minor-Version Jump Continues to Auto-Lock With Zero Added Friction (Regression Guard)

**Related User Story:** [06: Major-Bump Re-Validation Gate](../user-stories/06-major-bump-re-validation-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go regression-guard test suite over `internal/personas/upgrade.go`'s classification branch | Proves the major-jump gate (AC 06-01) is additive and does not alter existing minor-path behavior |
| Test Framework | `testing` + `testify` (`assert`/`require`) | Matches existing `upgrade_test.go` conventions |
| Key Dependencies | `golang.org/x/mod/semver` (`semver.Major`, `semver.IsValid`) | Same normalized version strings `isNewer` already builds; no new parsing logic |

### Related Files (from codebase-discovery.json)
- `internal/personas/upgrade.go` — modify: ensure the major/minor classification (`semver.Major(local) != semver.Major(remote)`) is the sole branch point; the minor/no-change path falls straight through to the existing `isNewer`-driven auto-lock write, unchanged from pre-Story-06 behavior.
- `internal/personas/upgrade_test.go` — modify: add cases for (minor bump, no gate triggered — auto-locks with no fixture re-run and no verify flag) and (same major, no version change — no gate, no flag, no write per existing `isNewer` up-to-date semantics).
- `internal/personas/test.go` — reference only, unchanged: the `FixtureRunner`/`TemplateFixtureRunner` seam must be provably uninvoked on this path.
- `internal/personas/upgrade.go:89` (`isNewer`) — reference: existing semver-aware comparison; the minor-path reuses its existing behavior unchanged.
- `documentation/semver-version-comparison.md` — reference: explains how `semver.Major` classification builds on the same normalized version strings `isNewer` uses.

## Happy Path Scenarios

**Scenario 1: Minor bump (e.g. `4.8` → `4.9`) auto-locks exactly as before, no fixture re-run, no flag**
- **Given** a persona is locked at `v4.8.0` and the resolver returns a candidate remote version `v4.9.0` (`semver.Major(local) = semver.Major(remote) = "v4"`)
- **When** `atcr personas upgrade` runs for that persona
- **Then** `isNewer` reports true, the lock advances to `v4.9.0` via the existing auto-lock path, `TemplateFixtureRunner.RunFixture()` is never invoked, and the upgrade report contains no "prompt tuned for the prior major — verify" flag for that persona

**Scenario 2: Same major, no version change — no gate, no write, matches existing up-to-date semantics**
- **Given** a persona is locked at `v4.9.0` and the resolver returns the same `v4.9.0` (or a version `isNewer` treats as not-newer)
- **When** `atcr personas upgrade` runs for that persona
- **Then** `res.UpToDate` is true, no lock write occurs, `TemplateFixtureRunner.RunFixture()` is never invoked, and no verify flag appears — behavior is byte-for-byte identical to the pre-Story-06 `Upgrade()` path

## Edge Cases

**Edge Case 1: Malformed/pre-release version strings must not misclassify as a major jump when `isNewer` treats them as not comparable**
- **Given** a local version `"latest"` and a remote version `"v1.0.0"` (mixed validity — one side is not valid semver)
- **When** the major/minor classification runs alongside `isNewer`
- **Then** the classification reuses the same `"v"`-normalized, `semver.IsValid`-checked strings `isNewer` already constructs (not an independent re-parse), so the gate degrades safely: no lock write occurs (matching `isNewer`'s existing mixed-validity "treat as up-to-date" behavior) and the major-jump gate does not spuriously fire on incomparable inputs

**Edge Case 2: A pre-release major-looking string (e.g. `v5.0.0-rc.1`) does not bypass the gate via string-only comparison**
- **Given** local `v4.9.2` and remote `v5.0.0-rc.1` (both valid semver, differing major prefixes)
- **When** the classification runs
- **Then** `semver.Major` still reports `"v4"` vs `"v5"` — the gate fires as a major jump and requires fixture re-pass, confirming the classification derives from `semver.Major` on validated strings rather than any looser heuristic that pre-release suffixes might defeat

## Error Conditions

**Error Scenario 1: A future change accidentally routes a minor bump through the fixture gate (regression)**
- Error message: test failure — e.g. `assert.False(t, fixtureRunnerCalled, "minor bump must not invoke TemplateFixtureRunner")` — this AC's test suite is the guard, not a runtime error path
- HTTP status / error code: N/A (CI test failure, not a user-facing error)

**Error Scenario 2: A future change accidentally treats a failing major-jump fixture as non-blocking for `--all`/batch runs (cross-check with AC 06-01)**
- Error message: test asserts that in a batch run mixing minor and major personas, a failing major-jump fixture leaves that persona's lock unchanged while the minor-jump personas in the same batch still auto-lock normally
- HTTP status / error code: N/A (CI test failure, not a user-facing error)

## Performance Requirements
- **Response Time:** The minor/no-change path must incur zero added latency versus pre-Story-06 `Upgrade()` — no fixture render, no additional classification cost beyond the O(1) `semver.Major` string comparison.
- **Throughput:** Batch (`--all`) upgrade runs containing predominantly minor bumps must show no measurable throughput regression from this story's changes.

## Security Considerations
- **Authentication/Authorization:** None — this AC is a pure regression guard over existing local classification/write logic; no new network or auth surface.
- **Input Validation:** Reuses `isNewer`'s existing `semver.IsValid` normalization; this AC's tests explicitly cover malformed/pre-release version strings to prove no new inconsistent parsing path is introduced by the major-jump classification.

## Test Implementation Guidance
**Test Type:** UNIT (in `internal/personas/upgrade_test.go`), asserting non-invocation of the fixture runner via a spy/counter stub implementing `FixtureRunner`.
**Test Data Requirements:** Local/remote version pairs covering: minor bump (`v4.8.0`→`v4.9.0`), no-change (`v4.9.0`→`v4.9.0`), mixed-validity (`"latest"`→`"v1.0.0"`), and pre-release major jump (`v4.9.2`→`v5.0.0-rc.1`).
**Mock/Stub Requirements:** A call-counting `FixtureRunner` stub (embeds or wraps the same interface used in AC 06-01's tests) whose `RunFixture` call count is asserted to be zero for every scenario in this AC.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Minor bump auto-locks with zero added friction: no fixture re-run, no verify flag
- [ ] Same-major/no-change transition matches pre-existing `isNewer` up-to-date behavior exactly (no write, no flag)
- [ ] Malformed/mixed-validity version strings do not spuriously trigger the major-jump gate
- [ ] Pre-release major-version strings are classified as major via `semver.Major`, not defeated by suffix noise

**Manual Review:**
- [ ] Code reviewed and approved
