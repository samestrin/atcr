# Acceptance Criteria: index.json Entries Added for the 3 New Personas

**Related User Story:** [02: Publish Model-Tuned Personas to the Community Registry Index](../user-stories/02-publish-personas-to-community-index.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | JSON content addition (`index.json`) in the external `atcr/personas` repo | No code, schema, or hosting change in this repo (atcr) |
| Test Framework | Manual CLI verification (`atcr personas search`) against a local copy of the edited index, then against the live published repo | No automated test in this codebase exercises the external repo's content |
| Key Dependencies | `internal/personas/client.go` `FetchIndex(client, baseURL)`, `internal/personas/search.go` `PersonaIndexEntry` struct and `Search()` matcher | Existing, unmodified — reference only |

## Related Files
- `atcr/personas` repo `index.json` (external repo, not in this codebase) - modify: add 3 new entries, one each for the Anthropic, OpenAI, and Google personas authored in Story 1
- `internal/personas/search.go` - reference only: defines the `PersonaIndexEntry{Name, Version, Description, Path}` schema each new entry must conform to and the case-insensitive name/description matcher `Search()` uses
- `internal/personas/client.go` - reference only: `FetchIndex` parses `index.json` into `[]PersonaIndexEntry`; `ErrIndexNotFound` is returned only when the whole file is missing, not for a malformed individual entry
- `docs/personas-install.md` - reference only: documents the `ATCR_PERSONAS_URL` default and the `<name>.yaml` path convention each entry's `path`/`name` must resolve against

### Related Files (from codebase-discovery.json)

- `internal/personas/search.go` — defines `PersonaIndexEntry` schema and `Search()` matcher
- `internal/personas/client.go` — `FetchIndex` parses `index.json` into `[]PersonaIndexEntry`
- `docs/personas-install.md` — documents `ATCR_PERSONAS_URL` default and `<name>.yaml` path convention
- `cmd/atcr/personas.go` — registers `atcr personas search`/`install` subcommands that consume `index.json`

## Happy Path Scenarios
**Scenario 1: Three new entries added, one per provider-tuned persona**
- **Given** Story 1 has published 3 persona YAML files to the external `atcr/personas` repo (one Anthropic-tuned, one OpenAI-tuned, one Google-tuned), each already passing its own fixture validation
- **When** the repo maintainer adds one `index.json` entry per persona, each populated with `name`, `version`, `description`, and `path` fields matching the existing `PersonaIndexEntry` schema
- **Then** `index.json` contains exactly 3 new entries (in addition to any pre-existing entries), each entry's `name`/`path` resolving to `<ATCR_PERSONAS_URL>/<name>.yaml` for its corresponding Story 1 YAML file

**Scenario 2: Each entry's description is distinct and matchable**
- **Given** the 3 new entries are present in `index.json`
- **When** each entry's `description` field is written
- **Then** each description contains at least one keyword identifying its target model/provider (e.g., "Claude", "GPT", "Gemini") so `atcr personas search <provider-keyword>` can distinguish the 3 new personas from each other and from pre-existing entries

**Scenario 3: Existing entries are untouched**
- **Given** `index.json` already lists other, previously-published community personas
- **When** the 3 new entries are appended
- **Then** the pre-existing entries' `name`, `version`, `description`, and `path` fields are byte-for-byte unchanged, and the file remains valid JSON parseable by `FetchIndex`

## Edge Cases
**Edge Case 1: Persona name collides with an existing entry**
- **Given** one of the 3 new persona names happens to match an already-published entry's `name`
- **When** the new entry is added
- **Then** the new entry uses a distinct, namespaced name (per `docs/personas-install.md`'s letters/digits/`_`/`-`/`/` rule) so no two entries in `index.json` share the same `name`, avoiding ambiguous `install` resolution

**Edge Case 2: Description keyword overlaps across providers**
- **Given** all 3 personas are "reviewer" personas and may share generic terms like "review" or "code" in their descriptions
- **When** a user searches a generic keyword
- **Then** all 3 (and potentially other) entries legitimately match — this is expected `Search()` substring-match behavior, not a defect; each entry's description SHOULD additionally include a provider-specific term so a targeted keyword search can isolate one persona from the other two

## Error Conditions
**Error Scenario 1: Malformed JSON breaks index parsing for all personas**
- Error message: `failed to parse community repo index: <json unmarshal error>` (from `internal/personas/client.go` `FetchIndex`)
- HTTP status / error code: N/A (client-side JSON decode error, not an HTTP status) — mitigated by validating the edited `index.json` with a local JSON parse and a local `atcr personas search` dry run (pointing `ATCR_PERSONAS_URL` at a local file server or a feature branch of the repo) before publishing to `main`

**Error Scenario 2: Entry references a YAML path that does not exist yet**
- Error message: N/A at index-edit time (index.json itself is valid JSON) — surfaced later at install time by `atcr personas install`: `persona "<slug>" not found in community repo`
- HTTP status / error code: N/A — mitigated by sequencing this story strictly after Story 1's YAML files are published and validated (per Story Context/Constraints), never adding an index entry before its corresponding YAML exists

## Performance Requirements
- **Response Time:** N/A — this AC covers static JSON content only; no new code path or latency is introduced beyond the existing `FetchIndex` HTTP GET
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — `index.json` is a public, unauthenticated raw-content file per the existing `ATCR_PERSONAS_URL` contract; write access is controlled by the maintainer's GitHub commit/PR permissions on the external repo, outside this codebase's scope
- **Input Validation:** Each new entry's `name` must conform to the existing name-validation rule enforced at install time (letters, digits, `_`, `-`, `/`; no `..` or absolute paths, per `docs/personas-install.md`) so that a later `atcr personas install` for the new entry is not rejected by the existing traversal guard

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** The 3 finalized `index.json` entries (name/version/description/path per persona) and the corresponding Story 1 YAML files already published at their target paths in the external repo
**Mock/Stub Requirements:** None in this codebase; verification is performed by pointing `ATCR_PERSONAS_URL` at either a local clone/branch of the `atcr/personas` repo or the live published `main` branch and running `atcr personas search <keyword>` to confirm all 3 entries parse and appear

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no code in this repo changes; existing `internal/personas` test suite continues to pass unmodified)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `index.json` in the external `atcr/personas` repo contains exactly 3 new entries, one per Story 1 persona (Anthropic, OpenAI, Google)
- [ ] Each new entry has non-empty `name`, `version`, `description`, and `path` fields conforming to `PersonaIndexEntry`
- [ ] Each new entry's `path`/`name` resolves to an already-published, Story-1-validated YAML file at `<ATCR_PERSONAS_URL>/<name>.yaml`
- [ ] Pre-existing entries in `index.json` are unchanged and the file remains valid JSON

**Manual Review:**
- [ ] Code reviewed and approved
