# Acceptance Criteria: Retired-Slug Verification (No Remaining `sentinel`/`tracer`/`idiomatic` References)

**Related User Story:** [05: Human-Names Migration for Built-in Stragglers](../user-stories/05-human-names-migration-for-built-in-stragglers.md)
**Design References:** [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | CLI verification command (`atcr personas test <name>`) + scoped repository search | No new tooling; reuses the existing fixture-test command per persona |
| Test Framework | Go `testing` (unit/integration), plus a manual/CI scoped `grep`/`rg` pass over persona-specific paths | Scope: `personas/` (incl. `personas/personas_test.go`), `internal/personas/*_test.go`, `docs/personas-*.md`, `README.md`, and `personas/community/index.json` (if used) — per the story's Risk mitigation, EXCEPT the documented arbitrary-placeholder test cases (Edge Case 2) |
| Key Dependencies | Existing `internal/personas.TestPersona`/`TemplateFixtureRunner`; `atcr personas test` CLI command | No new dependency |

### Related Files (from codebase-discovery.json)
- `personas/personas.go` (names slice ~line 20, embedded file guard) — reference: post-migration `names` slice is the authoritative registration source for the verification scan.
- `internal/personas/test.go` (`TemplateFixtureRunner`, `TestPersona`) — reference: verification entry point exercised by `atcr personas test sasha`/`penny`/`ingrid`.
- `docs/personas-authoring.md` — reference: scoped-search target confirming no worked examples still cite `sentinel`/`tracer`/`idiomatic` as active slugs.
- `docs/personas-install.md` — reference: scoped-search target confirming the built-in persona list and worked CLI output no longer reference the retired slugs.
- `personas/community/index.json` — reference: if the community-only path was chosen, verify no retired-slug entries exist.


## Happy Path Scenarios
**Scenario 1: `atcr personas test` passes for all three new slugs**
- **Given** the rename from AC 05-01 and AC 05-02 has landed (`sasha`, `penny`, `ingrid` registered with renamed templates and fixtures)
- **When** `atcr personas test sasha`, `atcr personas test penny`, and `atcr personas test ingrid` are each run
- **Then** each command exits 0 and reports its fixture passing (`Passed: 1, Total: 1`)

**Scenario 2: Scoped search finds zero active-slug references**
- **Given** a search scoped to `personas/*.md`, `personas/testdata/*.patch`, `personas/personas.go`, `personas/personas_test.go`, `internal/personas/*_test.go` (where AC 05-01 edits the `names`-slice / fixture-path assertions — a stale slug in a test file must not be invisible), `docs/personas-authoring.md`, `docs/personas-install.md`, `README.md` (task-required — its built-in persona mentions must not retain retired slugs), and `personas/community/index.json` (if the community-only path was chosen for these personas), while EXCLUDING the documented arbitrary-placeholder test cases of Edge Case 2
- **When** the search is run for the bare tokens `sentinel`, `tracer`, `idiomatic` used as persona identifiers (e.g., as a `names` slice entry, a `.md` filename stem, a `_fixture.patch` filename stem, or a documented persona slug)
- **Then** zero matches are found in those paths

**Scenario 3: Full build/init smoke check passes**
- **Given** the complete migration (all three personas renamed, `names` slice updated)
- **When** `go build ./...` is run followed by any `atcr` subcommand invocation (triggering the `personas` package `init()`)
- **Then** the build succeeds and no init-time panic occurs, confirming the embedded-file-count/name guard is satisfied

## Edge Cases
**Edge Case 1: False positives from Go's unrelated `sentinel`-error-value idiom**
- **Given** the codebase contains unrelated uses of the word "sentinel" as a Go idiom term (e.g., `internal/verify/skeptic.go`, `internal/registry/attribution.go`, and this story's own prompt text discussing "sentinel errors" as a Go concept), per the story's documented Risk
- **When** the verification search is scoped to persona-identification paths only (not a blind repository-wide grep)
- **Then** these unrelated occurrences are excluded from the "zero remaining references" check — the check targets persona slugs/filenames, not the bare English/Go-jargon word

**Edge Case 2: Placeholder test data using old names as arbitrary strings**
- **Given** `internal/personas/list_test.go` uses `"sentinel"`/`"tracer"`/`"idiomatic"` as arbitrary placeholder persona names in sorting/formatting unit tests unrelated to the actual built-in personas (e.g., `map[string]float64{"sentinel": 0.72}` used purely as test fixture data)
- **When** the retired-slug verification scan is scoped per the story's Risk mitigation (persona registration/doc paths only, not arbitrary test fixtures)
- **Then** these placeholder occurrences are out of scope for this AC and are not required to change, since they do not reference the real built-in personas or their resolution

**Edge Case 3: Community index path (if chosen) still carries stale entries**
- **Given** the implementation chose the community-only path (moving personas to `personas/community/index.json` instead of staying built-in)
- **When** the scoped search checks `personas/community/index.json`
- **Then** no entry exists with `"name"` equal to `sentinel`, `tracer`, or `idiomatic` — only `sasha`, `penny`, `ingrid` entries are present

## Error Conditions
**Error Scenario 1: `atcr personas test <old-slug>` after migration**
- **Given** the migration has landed and the old slugs are fully retired
- **When** `atcr personas test sentinel` (or `tracer`/`idiomatic`) is run
- **Then** the command errors with an "unknown persona" or equivalent not-found message — the old slug does not silently resolve as an alias to the new persona
- HTTP status / error code: N/A (CLI non-zero exit code)

**Error Scenario 2: Verification scan flags a leftover reference**
- **Given** an incomplete migration where, for example, `docs/personas-install.md` still lists `sentinel` in its built-in persona table
- **When** the scoped search is run as part of this AC's verification step
- **Then** the match is reported (file:line), blocking the story from being considered complete until the reference is updated or removed

## Performance Requirements
- **Response Time:** `atcr personas test <name>` fixture verification is a local, non-network operation (per existing `TemplateFixtureRunner` behavior); each invocation completes in well under 1 second.
- **Throughput:** N/A (manual/CI verification step, not a runtime request path).

## Security Considerations
- **Authentication/Authorization:** N/A — read-only verification against local files and embedded data.
- **Input Validation:** N/A — this AC is a verification/QA step, not a user-input-processing code path.

## Test Implementation Guidance
**Test Type:** INTEGRATION (`atcr personas test <name>` CLI invocations) + manual scoped-search verification (documented as a checklist step, not necessarily automated into a unit test, since it spans code and Markdown docs)
**Test Data Requirements:** Fully migrated repository state (AC 05-01, AC 05-02, and AC 05-04 landed) before this AC's checks can pass
**Mock/Stub Requirements:** None — exercises the real CLI and real filesystem/embedded content

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr personas test sasha`, `atcr personas test penny`, `atcr personas test ingrid` each pass
- [ ] `atcr personas test sentinel`/`tracer`/`idiomatic` each fail with an unknown-persona error (old slugs are not aliased)
- [ ] A scoped search of `personas/` (incl. `personas/personas_test.go`), `internal/personas/*_test.go` (excluding the Edge Case 2 arbitrary-placeholder fixtures), `docs/personas-*.md`, `README.md`, and `personas/community/index.json` (if applicable) for the retired slugs as persona identifiers returns zero matches
- [ ] `go build ./...` succeeds with no init-time panic

**Manual Review:**
- [ ] Code reviewed and approved
