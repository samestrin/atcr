# Acceptance Criteria: `submitted` Status Is Not a Fourth `Source` Value

**Related User Story:** [03: `submitted` Status Distinct from `Source`/Provenance](../user-stories/03-submitted-status-distinct-from-source.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct/type addition | New type lives alongside `PersonaMeta`, not inside it |
| Test Framework | Go `testing` package (`go test ./internal/personas/...`) | Table-driven tests, matching existing style in the package |
| Key Dependencies | None new — uses only stdlib (`strings`) already imported in `list.go` | |

## Related Files
- `internal/personas/list.go` - modify: add a new `SubmissionStatus` (or similarly named) type/struct distinct from `PersonaMeta`; `PersonaMeta.Source` field (line 19-24), `List` (line 38), `ListTiers` (line 56), `listProject` (line 87), `listCommunity` (line 201) remain byte-for-byte unchanged in signature and behavior
- `internal/personas/list_test.go` - modify: add a regression test asserting `Source` only ever takes `"built-in"`, `"community"`, or `"project"` across `List`/`ListTiers` output, including after a submission marker exists on disk

## Happy Path Scenarios
**Scenario 1: Existing Source values are unaffected by the new concept's existence**
- **Given** a personas directory with one built-in, one community, and one project persona, and no `submitted` marker present anywhere
- **When** `List` and `ListTiers` are called
- **Then** every returned `PersonaMeta.Source` is exactly `"built-in"`, `"community"`, or `"project"` and the new `submitted`-tracking type is not referenced by any of them

**Scenario 2: A submitted persona still reports its original Source**
- **Given** a community persona that has an associated `submitted` marker (written by the Story 2 submit flow) recording it as fixture-passing-but-unvetted
- **When** `List` is called for that persona
- **Then** `PersonaMeta.Source` for that row is still `"community"` (or `"project"`, whichever tier it resolves from) — never a new fourth value, and the `submitted` state is obtainable only through the separate type/marker introduced by this story

## Edge Cases
**Edge Case 1: A persona with an orphaned/unread submitted marker (submit path was interrupted) still lists normally**
- **Given** a `submitted` marker file exists for a persona but is malformed or partially written
- **When** `List`/`ListTiers` run
- **Then** the existing `PersonaMeta` rows are produced identically to a run without any marker present — reading or parsing the marker is out of scope for `List`'s own return value and must not panic or alter `Source`

**Edge Case 2: Multiple personas across all three tiers coexist with mixed submitted/non-submitted state**
- **Given** built-in, community, and project personas are all present, some with `submitted` markers and some without
- **When** `List`/`ListTiers` run
- **Then** the `Source` column partitions exactly as before (no persona's `Source` shifts because it happens to have a `submitted` marker)

## Error Conditions
**Error Scenario 1: A hypothetical caller attempts to set `Source` to `"submitted"`**
- Error message: caught at compile time / code review — this is a static invariant, not a runtime error path; the regression test in Scenario 1/2 fails at test time if violated
- HTTP status / error code: N/A (library-internal invariant, not an HTTP-facing API)

## Performance Requirements
- **Response Time:** No measurable change to `List`/`ListTiers` runtime — the new type must not add directory walks, YAML parses, or file reads inside these functions' existing paths (its own read path, if any, is exercised only by the submit/status code, not by `List`)
- **Throughput:** N/A (local CLI listing, not a service)

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem read, no auth boundary crosses this AC
- **Input Validation:** The new type must not introduce a new string field that free-form user input can set to collide with `"built-in"`/`"community"`/`"project"`; if it exposes a status string at all, it must use its own field name, never overload `Source`

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A temp personas dir fixture with one community YAML and one project `.md` persona (reuse existing test helpers in `internal/personas/list_test.go` for temp-dir setup); no `submitted` marker infrastructure is required to exist yet for this AC's tests to pass — they assert the invariant holds in the presence or absence of that infrastructure
**Mock/Stub Requirements:** None — pure filesystem fixtures, no network or external process mocking needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A new type distinct from `PersonaMeta` exists to carry `submitted` status/metadata
- [ ] `PersonaMeta.Source`'s field comment, type, and value set (`"built-in"|"community"|"project"`) are unchanged
- [ ] Signatures of `List`, `ListTiers`, `listCommunity`, `listProject` are unchanged
- [ ] A regression test asserts no `Source` value outside the three existing strings is ever observed, including when a `submitted` marker exists

**Manual Review:**
- [ ] Code reviewed and approved
