# Acceptance Criteria: Documented Persona Placement and Index Entry Creation

**Related User Story:** [04: Maintainer Graduation into the Vetted Library](../user-stories/04-maintainer-graduation-into-vetted-library.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no new code) | New "Graduating a submitted persona" section in `docs/personas-authoring.md`, placed after `## 4. Contribution checklist` (docs/personas-authoring.md:162) and before/adjacent to `## 5. The community index entry` (docs/personas-authoring.md:178) |
| Test Framework | None (docs-only); existing `go test` gate over `personas/community/index.json` (referenced, not modified) | The existing index-consistency gate described in docs/personas-authoring.md:178-210 already enforces `provider`/`model` match at merge time — this AC documents how a maintainer satisfies that gate during graduation, it does not add a new gate |
| Key Dependencies | `internal/personas/search.go:14` `PersonaIndexEntry` struct (schema reference only) | No Go package, dependency, or schema change |

## Related Files
- `docs/personas-authoring.md` - modify: add a numbered "Graduating a submitted persona" procedure section covering (a) moving/copying the persona YAML (and its fixture, per `## 3. The fixture`) into `personas/community/`, and (b) adding a `PersonaIndexEntry`-shaped entry to `personas/community/index.json` with `name`/`version`/`description`/`path`/`provider`/`model` matching the persona's own YAML frontmatter
- `internal/personas/search.go` - reference only (no change): `PersonaIndexEntry` (line 14) is cited verbatim as the exact shape the new `index.json` entry must match, including which fields are gate-enforced (`provider`, `model`) versus optional (`tasks`, `tags`, `binding`)
- `personas/community/index.json` - reference only (no change): existing entries (e.g. `anthony`, `sonny`) are cited as the worked example of a correctly-shaped entry the documentation points to

## Happy Path Scenarios
**Scenario 1: Maintainer adds a new persona and index entry during graduation**
- **Given** a `submitted` PR contains a fixture-passing persona `reviewer/perf` with YAML frontmatter `provider: openrouter`, `model: anthropic/claude-sonnet-5`
- **When** the maintainer follows the documented graduation procedure
- **Then** the persona file lands at `personas/community/reviewer/perf.yaml` and a new `PersonaIndexEntry` is added to `personas/community/index.json` with `path: "reviewer/perf.yaml"`, `provider: "openrouter"`, and `model: "anthropic/claude-sonnet-5"` exactly matching the YAML

**Scenario 2: Documentation cites the exact schema fields required**
- **Given** a maintainer is reading `docs/personas-authoring.md` for the first time during graduation
- **When** they reach the new graduation section
- **Then** the section explicitly lists `name`, `version`, `description`, `path`, `provider`, `model` as the fields to populate, cross-references `internal/personas/search.go:14` as the authoritative schema, and states that `provider`/`model` must exactly match the persona YAML (per the existing `go test` consistency gate documented in `## 5. The community index entry`)

## Edge Cases
**Edge Case 1: Persona targets a subdirectory path**
- **Given** the submitted persona's slug implies a nested path (e.g. `security/owasp`)
- **When** the maintainer follows the graduation procedure
- **Then** the documentation instructs the maintainer to preserve the nested `path` value (`security/owasp.yaml`) consistent with existing community entries, not flatten it to the repo root

**Edge Case 2: Persona version differs from a prior submission of the same name**
- **Given** a persona with the same `name` already exists in `personas/community/index.json` at a lower `version`
- **When** the maintainer graduates an updated submission
- **Then** the documentation directs the maintainer to update the existing entry's `version`/`description`/`path`/`provider`/`model` in place rather than appending a duplicate entry

## Error Conditions
**Error Scenario 1: `provider`/`model` omitted or mismatched in the new index entry**
- Error message: existing `go test` gate failure output — a Go test iterating `personas/community/index.json` fails when an entry's `provider`/`model` is empty or drifts from the source YAML (docs/personas-authoring.md:210), e.g. `index entry "reviewer/perf": provider/model mismatch with source YAML`
- HTTP status / error code: N/A (CI test failure, non-zero exit from `go test ./...`); the documentation states this is the same pre-existing gate, not a new one introduced by this story

## Performance Requirements
- **Response Time:** N/A — this is a documentation artifact, not an executable path; no runtime performance requirement applies.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** None — graduation is performed by a maintainer with existing repo write access via the standard GitHub PR-merge workflow; no new credential or permission surface is introduced.
- **Input Validation:** The documentation reiterates the existing constraint (docs/personas-authoring.md:210) that `provider`/`model`/`tasks`/`tags` are display/search metadata only and must never contain executable content, secrets, or network instructions — consistent with the pre-existing index-entry rule, not a new one.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** A sample `submitted` PR (real or hypothetical) with a persona YAML carrying non-empty `provider`/`model`, used to manually walk the documented procedure end-to-end and confirm the resulting `index.json` entry matches `PersonaIndexEntry`'s field set.
**Mock/Stub Requirements:** None — verification is a manual read-through confirming the documented steps produce an entry that would pass the existing `go test` index-consistency gate; no new automated test is required by this story.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (pre-existing `go test` suite, including the index-consistency gate, remains green — no test is added or modified by this AC)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` contains a "Graduating a submitted persona" section listing persona-file placement and `index.json` entry creation as explicit numbered steps
- [ ] The section cites `internal/personas/search.go:14` and lists all `PersonaIndexEntry` fields (`name`, `version`, `description`, `path`, `provider`, `model`, plus optional `tasks`/`tags`/`binding`)
- [ ] The section states `provider`/`model` must exactly match the persona's own YAML frontmatter, consistent with the existing enforcement gate described in `## 5. The community index entry`

**Manual Review:**
- [ ] Code reviewed and approved
