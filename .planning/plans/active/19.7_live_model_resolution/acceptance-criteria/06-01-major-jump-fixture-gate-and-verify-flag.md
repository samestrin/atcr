# Acceptance Criteria: Major-Version Jump Gates the Lock on Fixture Re-Pass and Always Surfaces a Verify Flag

**Related User Story:** [06: Major-Bump Re-Validation Gate](../user-stories/06-major-bump-re-validation-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go gate function inside `internal/personas/upgrade.go` | Slots in at the existing lock-write decision point, alongside `isNewer` |
| Test Framework | `testing` + `testify` (`assert`/`require`) | Matches existing `upgrade_test.go` conventions |
| Key Dependencies | `golang.org/x/mod/semver` (`semver.Major`, already vendored via `isNewer`); `TemplateFixtureRunner` (existing, unchanged) | No new dependency; reuses `internal/personas/test.go`'s `FixtureRunner` interface |

## Related Files
- `internal/personas/upgrade.go` - modify: add a `semver.Major(local) != semver.Major(remote)` classification immediately alongside `isNewer`, and gate the call to `writePersonaUnit()` on a `TemplateFixtureRunner.RunFixture()` re-pass when the transition classifies as major; on fixture failure, skip the write and report the block + reason
- `internal/personas/upgrade_test.go` - modify: add cases for (major bump, fixture passes → lock advances + verify flag present), (major bump, fixture fails → lock does NOT advance + block reason reported + verify flag still present)
- `internal/personas/test.go` - reference only, unchanged: `TemplateFixtureRunner`/`FixtureRunner` is reused as-is; no new fixture format or mechanism is introduced by this AC
- `cmd/atcr/personas.go` - modify: extend the upgrade report to carry the fixture-block reason and the "prompt tuned for the prior major — verify" flag per persona

**Minimum 2 files per AC**

## Happy Path Scenarios

**Scenario 1: Major jump with a passing fixture advances the lock and still flags for verify**
- **Given** a persona is locked at `v4.9.2` and the resolver returns a candidate remote version `v5.0.0` (`semver.Major(local) = "v4"`, `semver.Major(remote) = "v5"`)
- **When** `atcr personas upgrade` runs for that persona and `TemplateFixtureRunner.RunFixture()` re-passes the persona's committed `.patch` fixture
- **Then** the lock advances to `v5.0.0`, `writePersonaUnit()` is called, and the upgrade report includes the "prompt tuned for the prior major — verify" flag for that persona, unconditionally, alongside the fixture-pass result

**Scenario 2: Major jump with a failing fixture blocks the lock write and reports why**
- **Given** the same major-boundary crossing (`v4` → `v5`) for a persona
- **When** `TemplateFixtureRunner.RunFixture()` fails to re-pass the persona's fixture (a rendered `{{` marker survives, or the fixture case does not pass)
- **Then** the lock is NOT advanced (the on-disk unit remains at `v4.9.2`), `writePersonaUnit()` is never called, and the upgrade report states the block, the fixture-failure reason, and the "prompt tuned for the prior major — verify" flag

**Scenario 3: `--all`/batch upgrade runs apply the gate per-persona, not globally**
- **Given** a batch upgrade run covers multiple personas, one crossing a major boundary and others not
- **When** the upgrade command processes all of them in one invocation
- **Then** the major-jump persona is individually gated on its own fixture re-pass (per Scenario 1/2), while other personas' lock decisions are unaffected by this persona's gate outcome

## Edge Cases

**Edge Case 1: A passing fixture on a major jump is NOT sufficient alone — the verify flag surfaces regardless of fixture outcome**
- **Given** a major-version transition where `TemplateFixtureRunner.RunFixture()` passes cleanly (the template renders with no unresolved `{{` markers and the fixture's `AgentName` substitution succeeds)
- **When** the upgrade report is generated for that persona
- **Then** the "prompt tuned for the prior major — verify" flag is present regardless of the fixture outcome — a passing fixture proves ONLY that the template still renders under the new major version; it does NOT prove the prompt is still well-tuned for the new major's behavior, capabilities, or quirks, and that human tuning judgment cannot be inferred from a render-only pass. The flag must never be omitted or suppressed merely because the fixture passed.

**Edge Case 2: A persona crossing a major boundary with no fixture at all (`HasFixture: false`)**
- **Given** a persona has no committed `.patch` fixture (e.g. a built-in with no embedded fixture)
- **When** a major-version jump is classified for that persona
- **Then** the gate treats an absent fixture as non-passing for gating purposes (the lock does not silently advance on an untestable major jump) and the report states no fixture exists, alongside the mandatory verify flag

**Edge Case 3: Fixture re-run only occurs on a classified major jump, never eagerly**
- **Given** a same-major transition (minor bump or no version change)
- **When** the upgrade flow evaluates the persona
- **Then** `TemplateFixtureRunner.RunFixture()` is never invoked for that persona — the fixture check is strictly conditional on the major-jump classification, avoiding unnecessary fixture execution cost on the common path

## Error Conditions

**Error Scenario 1: Fixture execution itself errors (not merely fails)**
- Error message: `render persona %q: %w` (surfaced from `TemplateFixtureRunner.RunFixture` via `renderFixture`), wrapped by the upgrade path as: `persona %q: major-version fixture re-check failed: %w`
- HTTP status / error code: N/A (CLI exit code 1; the command reports the error and does not advance the lock)

**Error Scenario 2: Lock write attempted despite a failed gate (defensive regression check)**
- Error message: internal invariant — a test asserts `writePersonaUnit()` is never called when the major-jump fixture check fails; if violated, the persona's on-disk `.yaml`/`.md` files must be asserted unchanged by the test
- HTTP status / error code: N/A (test-level assertion, not a runtime error path)

## Performance Requirements
- **Response Time:** The added `semver.Major` comparison is O(1) string prefix extraction; negligible overhead versus the existing `isNewer` call. `TemplateFixtureRunner.RunFixture()` only runs on classified major jumps (a small minority of upgrade invocations), so no measurable overhead is added to minor-bump or no-op upgrade paths.
- **Throughput:** No degradation to `--all`/batch upgrade throughput for personas not crossing a major boundary; batch runs remain linear in persona count with the fixture check adding one bounded render per major-jump persona only.

## Security Considerations
- **Authentication/Authorization:** No new auth surface; this AC operates entirely on already-fetched, already-validated remote persona YAML/markdown and the already-committed local fixture — no additional network calls are introduced.
- **Input Validation:** The major/minor classification consumes the same `"v"`-normalized, `semver.IsValid`-checked strings `isNewer` already constructs — no independent re-parsing of untrusted version strings is introduced, avoiding a second inconsistent validation path.

## Test Implementation Guidance
**Test Type:** UNIT (in `internal/personas/upgrade_test.go`), using an injectable `FixtureRunner` stub/mock (mirroring the existing `FixtureRunner` interface in `internal/personas/test.go`) to control pass/fail outcomes deterministically without a real LLM call.
**Test Data Requirements:** Local persona fixture files at `v4.9.2` (or similar `v4.x`) and remote fixtures at `v5.0.0` (major jump) plus a committed `.patch` fixture payload usable by the stubbed runner; reuse `installFixture`/`testServer` helpers already present in `upgrade_test.go`.
**Mock/Stub Requirements:** A stub implementing `FixtureRunner` (`RunFixture(name string) (FixtureOutcome, error)`) configurable to return `{HasFixture: true, Passed: 1, Total: 1}` (pass), `{HasFixture: true, Passed: 0, Total: 1}` (fail), and `{HasFixture: false}` (no fixture), injected into the upgrade call path in place of `TemplateFixtureRunner` for test isolation.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Major jump + passing fixture: lock advances, verify flag present in report
- [ ] Major jump + failing fixture: lock does NOT advance, block reason reported, verify flag still present
- [ ] Verify flag appears unconditionally on every major-jump transition, independent of fixture pass/fail
- [ ] `TemplateFixtureRunner.RunFixture()` is invoked only when the transition classifies as major, never on minor/no-change transitions

**Manual Review:**
- [ ] Code reviewed and approved
