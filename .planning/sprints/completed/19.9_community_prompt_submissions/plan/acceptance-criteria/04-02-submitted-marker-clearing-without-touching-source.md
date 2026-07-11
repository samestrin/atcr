# Acceptance Criteria: Submitted Marker Clearing Without Touching Source

**Related User Story:** [04: Maintainer Graduation into the Vetted Library](../user-stories/04-maintainer-graduation-into-vetted-library.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no new code) | Same "Graduating a submitted persona" section in `docs/personas-authoring.md`; this AC covers its status-marker-clearing sub-step |
| Test Framework | None (docs-only); references Story 3's existing unit test asserting `Source` never takes a fourth value | No new test file is added by this story; Story 3 already owns the `Source`-invariant test |
| Key Dependencies | Story 3's `submitted` marker mechanism (`internal/personas/list.go`, `PersonaMeta.Source`) - reference only | This AC must not describe or imply any code change to `PersonaMeta` or `Source` |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` (modify) — add the sub-step "clear the `submitted` status marker for this persona" to the graduation procedure, immediately followed by an explicit statement that `Source` (`built-in`/`community`/`project`) is never edited during graduation
- `internal/personas/list.go` (reference only) — `PersonaMeta.Source` (line 22) and its three valid values are cited as the field that must remain untouched; graduation clears Story 3's separate `submitted` marker, never this field
- `.planning/plans/active/19.9_community_prompt_submissions/user-stories/03-submitted-status-distinct-from-source.md` (reference only) — the graduation documentation must describe clearing whichever marker shape Story 3 lands (sidecar file, frontmatter key, or submission-specific struct field), matching its actual storage location/format rather than assuming one

## Design References
- [Status/Provenance Separation and Atomic Persistence](../documentation/status-provenance-and-atomic-writes.md) — the `Source`/`submitted` orthogonality that graduation must preserve
- [Personas Install & Authoring Doc Updates (AC4)](../documentation/personas-docs-updates.md) — the graduation section content and the explicit warning that `Source` stays `community`

## Happy Path Scenarios
**Scenario 1: Maintainer clears the marker during graduation, `Source` stays `community`**
- **Given** a persona `reviewer/perf` carries a Story 3 `submitted` marker (attribution + fixture-pass indicator) and, once graduated, would be listed with `Source: "community"`
- **When** the maintainer follows the documented graduation procedure's marker-clearing step
- **Then** the `submitted` marker is removed/cleared per Story 3's persistence mechanism, and the documentation confirms `personas list` continues to report `Source: "community"` for this persona both before and after graduation, unchanged

**Scenario 2: Documentation states the two axes are independent**
- **Given** a maintainer reads the graduation section for the first time
- **When** they reach the marker-clearing step
- **Then** the documentation explicitly states, in the same step or immediately adjacent to it, that clearing the `submitted` marker is orthogonal to `Source` and that `Source` must never be edited, added, or removed as part of graduation

## Edge Cases
**Edge Case 1: Marker persists as a sidecar file outside `personas/community/`**
- **Given** Story 3 implements the `submitted` marker as a sidecar artifact stored outside `personas/community/` (per Story 3's Constraints: "lives outside `personas/community/`")
- **When** the maintainer graduates the persona
- **Then** the documentation instructs the maintainer to locate and remove/clear that specific sidecar artifact (not merely stop referencing it), preventing an orphaned `submitted` marker from persisting after graduation

**Edge Case 2: Persona has no fixture-pass marker recorded (drifted/manual submission)**
- **Given** a `submitted` PR was opened outside the normal `atcr personas submit` flow and lacks the expected marker
- **When** the maintainer follows the graduation procedure
- **Then** the documentation notes this as a maintainer judgment call — proceed only if the PR's CI still confirms the fixture gate passed — without treating the missing marker as blocking graduation by itself, since "battle-tested" review is explicitly the human gate (per the story's Assumptions)

## Error Conditions
**Error Scenario 1: Maintainer accidentally edits `Source` while clearing the marker**
- Error message: N/A — this is a documentation/process safeguard, not a runtime error; the mitigation is the explicit written warning ("Source must never change during graduation — it stays `community` before and after")
- HTTP status / error code: N/A; if such an edit occurred and were caught downstream, Story 3's existing unit test asserting `Source` only ever takes `"built-in"|"community"|"project"` would fail `go test`, surfacing the mistake through the pre-existing test rather than a new one added by this story

## Performance Requirements
- **Response Time:** N/A — documentation artifact; the marker-clearing action itself is a maintainer file edit performed once per graduated persona, not a runtime operation with a latency requirement.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** None beyond the maintainer's existing repo write access used for the PR merge itself; no new permission surface.
- **Input Validation:** N/A — no user input is parsed by this documentation change; the only "input" is the maintainer's manual edit of an existing marker artifact per Story 3's documented shape.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** A hypothetical or real `submitted` PR whose persona carries Story 3's marker, used to manually confirm the documented clearing step correctly targets Story 3's actual marker storage location/format (not a stale assumption written before Story 3 lands).
**Mock/Stub Requirements:** None — verification is that the documented step, once Story 3 ships, accurately names the marker's real location/format; Story 3's own unit test (`Source` invariant) is the automated backstop this story relies on rather than duplicates.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (Story 3's pre-existing `Source`-invariant test remains green; no new test added by this AC)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Graduation section includes an explicit "clear the `submitted` marker" step that names Story 3's actual marker storage mechanism
- [x] The same section states, in immediate proximity to the marker-clearing step, that `Source` is never touched and remains `community` before and after graduation
- [x] The documentation is updated (if needed) once Story 3 lands, so the marker-clearing step matches Story 3's actual implementation shape rather than a placeholder assumption

**Manual Review:**
- [ ] Code reviewed and approved
