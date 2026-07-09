# Acceptance Criteria: No Misleading "Not Found in Community Index" Warnings

**Related User Story:** [07: init/quickstart Roster Reconciliation](../user-stories/07-init-quickstart-roster-reconciliation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI init/quickstart flow (`cmd/atcr` package), `installCommunityPersonas` warning path (`cmd/atcr/init.go:129`) | The skip-warning line itself is unchanged mechanically; this AC targets the outcome — it must not fire for any name in the roster actually requested against the real index |
| Test Framework | `testing` + `testify/require` + captured `io.Writer` for `errOut` | Mirrors `TestInstallCommunityPersonas_MissingRosterSkipsWithWarning`'s existing assertion style, inverted to assert absence |
| Key Dependencies | `internal/personas` (`FetchIndex`), `personas` (`builtins.Names()`) | No new dependencies required |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go:47` — modify: whatever roster/index source `init.go:47` supplies to `installCommunityPersonas` must not contain names absent from `personas/community/index.json`.
- `cmd/atcr/init.go:129` — reference: the existing `"persona %q not found in community index — skipping"` warning path that must not fire for any name in the reconciled roster.
- `cmd/atcr/quickstart.go:102` — modify: `quickstart.go:102` must supply the same reconciled roster, so the same zero-warning guarantee holds for quickstart.
- `cmd/atcr/init_test.go` — modify: add a test asserting `errOut` contains zero `"not found in community index — skipping"` occurrences when `installCommunityPersonas` runs with the reconciled roster against the real index.
- `cmd/atcr/quickstart_test.go` — modify: add the equivalent zero-warning assertion for the quickstart call site.
- `personas/community/index.json:1` — reference: the shipped 10-entry model-indexed community catalog used to validate the zero-warning assertion.
- `cmd/atcr/init.go:139` — reference: the existing never-overwrite guard whose message must not be conflated with the skip-warning targeted by this AC.

## Happy Path Scenarios
**Scenario 1: Online `init` prints zero skip-warnings**
- **Given** a clean directory and the real, unmodified `personas/community/index.json`
- **When** `atcr init` runs without `--offline`
- **Then** `errOut` contains zero occurrences of `persona %q not found in community index — skipping` for any name in the roster actually requested

**Scenario 2: Online `quickstart` prints zero skip-warnings**
- **Given** a clean directory and the real, unmodified `personas/community/index.json`, `atcr quickstart` running with community fetch enabled
- **When** the quickstart flow reaches its `installCommunityPersonas` call
- **Then** `errOut` contains zero occurrences of the same skip-warning text

## Edge Cases
**Edge Case 1: A name is legitimately absent from the index (future regression, not this story's roster)**
- **Given** a roster is intentionally extended in the future to include a name the index does not (yet) publish
- **When** `installCommunityPersonas` runs
- **Then** the skip-warning path still exists and still fires for that genuinely-absent name — this AC only guarantees zero warnings for the roster THIS story ships, not an unconditional removal of the warning mechanism

**Edge Case 2: The never-overwrite warning is distinct and must not be conflated**
- **Given** a persona is already installed on disk from a prior run
- **When** `installCommunityPersonas` re-runs
- **Then** the "already installed — leaving it untouched" message at `init.go:139` may still print — this AC targets only the "not found in community index — skipping" text, not the pre-existing never-overwrite notice

## Error Conditions
**Error Scenario 1: Test assertion regresses to false-positive if scoped too broadly**
- Error message: N/A — this is a test-design risk noted in the story's risk table, not a runtime error
- Mitigation: the zero-skip-warning test asserts against the exact roster this story installs by design, not against "any roster," so a future intentional warning for a genuinely new/missing name is not silently masked

## Performance Requirements
- **Response Time:** No change — warning suppression is a byproduct of roster/index alignment, not new I/O
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** N/A — no new input surface; this AC governs stderr output content only

## Test Implementation Guidance
**Test Type:** INTEGRATION (existing `httptest.Server`-backed pattern in `cmd/atcr/init_test.go` and `cmd/atcr/quickstart_test.go`)
**Test Data Requirements:** The real `personas/community/index.json` content (or an exact-shape fixture) so the negative assertion ("zero warnings") is proven against production data
**Mock/Stub Requirements:** `httptest.NewServer` serving the index; capture `errOut` into a `bytes.Buffer` and assert `!strings.Contains(errOut.String(), "not found in community index")`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A test proves zero skip-warnings for online `init` against the real index
- [ ] A test proves zero skip-warnings for online `quickstart` against the real index
- [ ] The never-overwrite warning (`init.go:139`) is confirmed unaffected/unconflated by this change

**Manual Review:**
- [ ] Code reviewed and approved
