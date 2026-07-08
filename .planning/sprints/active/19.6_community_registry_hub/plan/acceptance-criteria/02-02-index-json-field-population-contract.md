# Acceptance Criteria: index.json Field Population Contract

**Related User Story:** [02: Structured Model Metadata Schema](../user-stories/02-structured-model-metadata-schema.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | In-repo `index.json` + Go enforcement test | `personas/community/index.json` is authored IN-REPO (LOCKED decision Q4); a Go test is the AC7 gate |
| Test Framework | Go `testing` (hard `go test` gate) + Markdown documentation | `internal/personas/search_test.go` iterates every `index.json` entry and asserts `provider`/`model` non-empty AND equal to the source persona YAML |
| Key Dependencies | `encoding/json`, `gopkg.in/yaml.v3` (existing) | Reads `personas/community/index.json` and each entry's source persona YAML |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` — modify: add a subsection documenting the `index.json` entry schema (`name`, `version`, `description`, `path`, `provider`, `model`, `tasks`, `tags`) and extend the contribution checklist with an item requiring non-empty `provider`/`model` matching the persona YAML.
- `internal/personas/search.go` (`PersonaIndexEntry`) — reference: the single source of truth for documented field names/types.
- `internal/personas/search_test.go` — create/modify: the **AC7 enforcement gate** — a Go test that iterates every entry in `personas/community/index.json`, loads each entry's source persona YAML (via its `path`), and asserts `provider`/`model` are non-empty AND string-equal to the YAML's `provider`/`model`. The test fails if any library persona entry is missing or mismatched. Embedded built-in personas are exempt (they are not enumerated in the community index).
- `personas/community/index.json` — create: in-repo index authored/generated per the documented contract; this is the file the enforcement test reads.


## Happy Path Scenarios
**Scenario 1: Documented schema matches the Go struct**
- **Given** the `index.json` schema subsection added to `docs/personas-authoring.md`
- **When** a maintainer compares its documented field list (`name`, `version`, `description`, `path`, `provider`, `model`, `tasks`, `tags`) against `PersonaIndexEntry` in `internal/personas/search.go`
- **Then** every documented field name and JSON key matches the struct's `json` tags exactly, with `provider`/`model`/`tasks`/`tags` marked as populated-from-YAML and `tasks`/`tags` marked optional

**Scenario 2: Example entry round-trips through the struct**
- **Given** the example `index.json` entry shown in the new documentation subsection (containing `provider`/`model` values that match a sample persona YAML's `provider: anthropic` / `model: claude-sonnet-4-6`)
- **When** the example entry is decoded into `PersonaIndexEntry` in a test
- **Then** `Provider` equals `"anthropic"` and `Model` equals `"claude-sonnet-4-6"`, proving the documented contract is exercisable end-to-end within this repo's test suite

**Scenario 3: AC7 enforcement gate passes for a fully-populated in-repo index (LOCKED decision Q4)**
- **Given** `personas/community/index.json` authored in-repo, where every entry's `provider`/`model` are non-empty and equal the corresponding source persona YAML's `provider`/`model`
- **When** the Go enforcement test in `internal/personas/search_test.go` iterates all entries, loads each entry's source persona YAML via its `path`, and compares fields
- **Then** the test passes (`go test` green), because each entry's `Provider`/`Model` is non-empty and matches its YAML source; embedded built-in personas are not enumerated in the community index and are therefore exempt

## Edge Cases
**Edge Case 1: Persona YAML has no `tasks`/`tags` authored**
- **Given** the documentation's guidance that `tasks`/`tags` are optional and should be omitted (not emitted as empty arrays) when a persona's YAML does not declare them
- **When** a maintainer reads the schema subsection
- **Then** the guidance explicitly states publish tooling should omit the keys entirely rather than emit `"tasks":[]`/`"tags":[]`, consistent with the `omitempty` behavior on the Go struct

**Edge Case 2: Existing personas carried over from before this schema change**
- **Given** persona entries previously authored under the old four-field `index.json` shape
- **When** the in-repo `personas/community/index.json` is updated to the extended schema
- **Then** any such entry that lacks non-empty `provider`/`model` matching its source YAML will FAIL the AC7 enforcement test — the in-repo gate blocks merge until the entry is populated, rather than silently degrading discoverability (cross-referencing the backward-compatibility guarantee for decode-time permissiveness in AC 02-03)

## Error Conditions
**Error Scenario 1: index.json entry missing or mismatched provider/model FAILS the go test gate**
- **Given** an entry in the in-repo `personas/community/index.json` whose `provider`/`model` is empty, or does not equal its source persona YAML's `provider`/`model`
- **When** the AC7 enforcement test in `internal/personas/search_test.go` runs (i.e. on every `go test ./...` / CI run)
- **Then** the test FAILS with an assertion identifying the offending persona `path` and the expected-vs-actual field values — this is a hard `go test` gate, NOT editorial/manual review. A contributor cannot merge a library persona with missing or drifted metadata. Embedded built-ins are exempt (not enumerated in the community index).
- HTTP status / error code: N/A (test-time failure, non-zero `go test` exit)

## Performance Requirements
- **Response Time:** Not applicable — the enforcement is a build-time `go test` gate over in-repo `personas/community/index.json`, not a runtime path.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no auth surface in documentation.
- **Input Validation:** Documentation must not instruct contributors to embed executable content, secrets, or network instructions in `provider`/`model`/`tasks`/`tags` values — these are display/search metadata only, consistent with the existing security note in `docs/personas-authoring.md` about persona prompts.

## Test Implementation Guidance
**Test Type:** UNIT (hard `go test` enforcement gate over the in-repo `personas/community/index.json`; the documentation subsection is the human-facing companion but the gate itself is executable, not editorial)
**Test Data Requirements:** The real in-repo `personas/community/index.json` and each entry's source persona YAML; plus a negative-case fixture (an entry with empty or mismatched `provider`/`model`) proving the gate fails as designed
**Mock/Stub Requirements:** None — the test reads real repo files directly; no HTTP or network mocking

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `personas/community/index.json` is authored in-repo with `provider`/`model` populated for every library persona entry (LOCKED decision Q4)
- [ ] AC7 enforcement gate: a Go test in `internal/personas/search_test.go` iterates every `index.json` entry and asserts `provider`/`model` are non-empty AND equal the source persona YAML's `provider`/`model`; the test fails on any missing/mismatched library persona
- [ ] Embedded built-in personas are explicitly exempt from the gate (not enumerated in the community index)
- [ ] `docs/personas-authoring.md` documents the full `index.json` entry schema including `provider`/`model`/`tasks`/`tags`
- [ ] Contribution checklist (§4) includes an item requiring non-empty `provider`/`model` in the index entry (matching the source YAML)

**Manual Review:**
- [ ] Code reviewed and approved
