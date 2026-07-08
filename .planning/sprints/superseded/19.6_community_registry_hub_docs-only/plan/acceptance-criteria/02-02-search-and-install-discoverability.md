# Acceptance Criteria: End-to-End Search and Install Discoverability

**Related User Story:** [02: Publish Model-Tuned Personas to the Community Registry Index](../user-stories/02-publish-personas-to-community-index.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | End-to-end CLI verification against the live `ATCR_PERSONAS_URL` community repo | No code change in this repo; exercises the existing, already-shipped `atcr personas search`/`install` commands |
| Test Framework | Manual CLI verification (live network calls to `raw.githubusercontent.com/atcr/personas/main`) | Cannot be automated in this repo's CI since it depends on the external repo's published state |
| Key Dependencies | `internal/personas/client.go` `FetchIndex`/`FetchPersonaYAML`; `internal/personas/search.go` `Search()`; the `atcr personas install`/`search` CLI subcommands documented in `docs/personas-install.md` | Existing, unmodified — reference only |

## Related Files
- `docs/personas-install.md` - reference only: documents the `search`/`install` subcommand contracts, error messages, and `ATCR_PERSONAS_URL` configuration this AC verifies against
- `internal/personas/client.go` - reference only: `FetchPersonaYAML(client, baseURL, name)` fetches `<baseURL>/<name>.yaml`; `ErrPersonaNotFound` is the error path exercised if an index entry from AC 02-01 is malformed or missing its YAML
- `internal/personas/search.go` - reference only: `Search()` performs the case-insensitive name/description substring match that `atcr personas search <keyword>` relies on to surface each new persona
- `atcr/personas` repo `index.json` and `<name>.yaml` files (external repo) - dependency only: this AC assumes AC 02-01's index entries and Story 1's YAML files are already published

### Related Files (from codebase-discovery.json)

- `docs/personas-install.md` — documents `search`/`install` subcommand contracts, error messages, and `ATCR_PERSONAS_URL` configuration
- `internal/personas/client.go` — `FetchPersonaYAML` fetches `<baseURL>/<name>.yaml`; returns `ErrPersonaNotFound`
- `internal/personas/search.go` — `Search()` performs case-insensitive name/description substring matching
- `cmd/atcr/personas.go` — registers the `atcr personas search`/`install` CLI surface

## Happy Path Scenarios
**Scenario 1: Search surfaces each new persona by name**
- **Given** the 3 new `index.json` entries from AC 02-01 are published to the live `atcr/personas` repo
- **When** `atcr personas search <persona-name-keyword>` is run for each of the 3 new personas' exact or partial `name`
- **Then** each command's output table lists the corresponding persona's `NAME`, `VERSION`, and `DESCRIPTION`, matching what `docs/personas-install.md`'s `search` output format documents

**Scenario 2: Search surfaces each new persona by description keyword**
- **Given** the same 3 published entries, each with a provider-identifying term in its `description` (e.g., "Claude", "GPT", "Gemini")
- **When** `atcr personas search <provider-keyword>` is run for each provider term
- **Then** the matching persona appears in the results (per `Search()`'s case-insensitive name-or-description substring match)

**Scenario 3: Install succeeds for each new persona**
- **Given** the 3 new personas are discoverable via `search`
- **When** `atcr personas install <namespace/name>` is run for each of the 3 new personas' full slug
- **Then** each command fetches `<ATCR_PERSONAS_URL>/<name>.yaml`, validates it against the registry schema, writes it to `~/.config/atcr/personas/` (or the OS-equivalent `os.UserConfigDir()/atcr/personas`), and prints `Installed persona "<slug>"` with exit code 0

**Scenario 4: Installed personas appear in `atcr personas list`**
- **Given** all 3 new personas have been installed
- **When** `atcr personas list` is run
- **Then** all 3 appear as rows with `SOURCE` = `community` and `VERSION` matching each entry's published `index.json` version

## Edge Cases
**Edge Case 1: Re-running install is idempotent**
- **Given** a new persona is already installed from a prior run
- **When** `atcr personas install <namespace/name>` is run again for the same persona
- **Then** the command reports the persona already present / succeeds without error (consistent with the documented idempotent-install behavior for bundle members in `docs/personas-install.md`), and does not corrupt the already-installed file

**Edge Case 2: Namespace/name with the `/` separator resolves to the expected fetch path**
- **Given** the 3 new personas use namespaced slugs (e.g., `provider/claude-reviewer`) per the existing naming convention
- **When** `search` and `install` are run using the full namespaced slug
- **Then** the `/` separator is accepted and maps to the `<ATCR_PERSONAS_URL>/<namespace>/<name>.yaml` fetch path with no traversal-guard rejection (per the letters/digits/`_`/`-`/`/` rule in `docs/personas-install.md`)

## Error Conditions
**Error Scenario 1: Search finds nothing when index entry or keyword is wrong**
- Error message: `No personas found matching "<keyword>"` (documented `search` no-match output)
- HTTP status / error code: N/A — indicates a defect in AC 02-01 (missing or misspelled `name`/`description`) requiring the index.json entry to be corrected before this AC can pass

**Error Scenario 2: Install fails because the YAML file is missing or unpublished**
- Error message: `persona "<slug>" not found in community repo` (from `ErrPersonaNotFound`, per `internal/personas/client.go` and `docs/personas-install.md`'s documented install error)
- HTTP status / error code: N/A (raw-content 404 mapped to this error) — indicates Story 1's YAML was not actually published at the path the index entry references; must be fixed before this AC can pass

**Error Scenario 3: Install fails schema validation**
- Error message: the registry's YAML validation error, install writes nothing (per `docs/personas-install.md`'s "Invalid persona YAML" error contract)
- HTTP status / error code: N/A — indicates Story 1's YAML does not conform to the registry schema and must be corrected in the external repo before this AC can pass

## Performance Requirements
- **Response Time:** N/A beyond the existing `search`/`install` HTTP fetch behavior already shipped and unmodified by this story
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — verification uses the public, unauthenticated default `ATCR_PERSONAS_URL`; no credentials are involved
- **Input Validation:** Relies on the existing, unmodified name-validation guard (letters/digits/`_`/`-`/`/`, no `..`/absolute paths) that already prevents `install`/`remove` from writing outside the personas directory — this AC verifies the 3 new slugs pass that guard, it does not change the guard itself

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** The 3 new persona slugs and at least one distinguishing search keyword per persona (name substring and description/provider substring); a clean or disposable `~/.config/atcr/personas/` directory (or an isolated `os.UserConfigDir()` override) to observe fresh installs without clobbering pre-existing local personas
**Mock/Stub Requirements:** None — this is an intentionally live, external-network verification against the published `atcr/personas` repo, run manually outside this repo's automated CI per the story's Constraints (this plan's TDD/sprint loop cannot execute or verify the external repo directly)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no code in this repo changes; existing `internal/personas` test suite continues to pass unmodified)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr personas search <keyword>` returns each of the 3 new personas by name-keyword and by description/provider-keyword against the live repo
- [ ] `atcr personas install <namespace/name>` succeeds for all 3 new personas, writing valid YAML to the local personas directory with exit code 0
- [ ] `atcr personas list` shows all 3 newly installed personas with `SOURCE` = `community`
- [ ] No error paths (`persona not found`, invalid YAML, index parse failure) are triggered during this verification

**Manual Review:**
- [ ] Code reviewed and approved
