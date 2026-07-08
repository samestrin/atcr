# Acceptance Criteria: init/quickstart Fetch-and-Pin Community Personas

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)
**Design References:** [fetch-and-distribution.md](../documentation/fetch-and-distribution.md), [persona-yaml-schema.md](../documentation/persona-yaml-schema.md), [testing-mock-registry.md](../documentation/testing-mock-registry.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command logic (Cobra) | `cmd/atcr/init.go`, `cmd/atcr/quickstart.go` |
| Test Framework | Go `testing` + `httptest.NewServer` | mock registry server, injected via `ATCR_PERSONAS_URL` and `personasClient` |
| Key Dependencies | `internal/personas.Install`, `internal/personas.FetchIndex` | existing fetch/validate/atomic-write machinery, no new subsystem |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go` (`runInit`, lines 76-78 force/anyExist gate, lines 96-118 O_EXCL write guard) — modify: replace the embedded built-in copy loop with fetch-and-pin calls into `internal/personas.Install`, using the fetched YAML's `version` field as the recorded pin.
- `cmd/atcr/quickstart.go` (`runQuickstart`) — modify: inherit the fetch-and-pin behavior through its `runInit` call before proceeding to synthetic-provider setup.
- `internal/personas/client.go` (line 24 `RegistryBaseURL`, `BaseURL()`) — modify: repoint the default community base URL to `samestrin/atcr` in-repo path.
- `internal/personas/install.go` — reference: fetches and validates a single persona YAML before atomic write; called by the fetch-and-pin path.
- `internal/personas/upgrade.go` — reference: compares installed version to remote version; continues to work once personas are installed via fetch.
- `internal/personas/list.go` (`PersonaMeta`, source labeling, lines 38-47, 117-172) — reference: reads the installed YAML's `version` field and labels source `community`.
- `cmd/atcr/personas.go` (line 81 `personasClient`) — reference: existing injection point for mock-registry tests.
- `personas/community/index.json` — create: in-repo canonical index.
- `personas/community/<slug>.yaml` — create: community persona YAML files fetched by `init`/`quickstart`.
- `docs/personas-install.md` — modify: document fetch-and-pin behavior and pinned versions.
- `cmd/atcr/init_test.go` / `cmd/atcr/quickstart_test.go` — modify: add `httptest.NewServer` + `ATCR_PERSONAS_URL` tests asserting fetch-and-pin with version recording.


## Happy Path Scenarios
**Scenario 1: `atcr init` fetches and pins from the community registry**
- **Given** a mock registry server (`httptest.NewServer`) exposing `index.json` and per-persona YAML with `version: "1.2.0"`, referenced via `ATCR_PERSONAS_URL`
- **When** `atcr init` runs without `--offline` in an empty workspace
- **Then** each roster persona is installed under `.atcr/personas/<name>.yaml` (or `.md`, per existing file convention) sourced from the mock registry, and `atcr personas list` reports `version: 1.2.0` and `source: community` for each

**Scenario 2: `atcr quickstart` inherits the fetch-and-pin behavior**
- **Given** the same mock registry as Scenario 1
- **When** `atcr quickstart` runs without `--offline` in an empty workspace
- **Then** `runQuickstart`'s call into `runInit` installs the same fetched, version-pinned personas before the synthetic-provider setup proceeds

**Scenario 3: `atcr personas upgrade` advances the pin after a fetch-and-pin install**
- **Given** a workspace whose personas were installed via fetch-and-pin at `version: "1.2.0"`, and the mock registry now serves `version: "1.3.0"`
- **When** `atcr personas upgrade --all` runs
- **Then** each persona is upgraded to `1.3.0` and the new version is recorded, exercising the existing `Upgrade` comparison logic unchanged

**Scenario 4: Loading state — `atcr init` reports each persona as it is fetched and pinned**
- **Given** a mock registry with three roster personas and an empty workspace
- **When** `atcr init` runs without `--offline`
- **Then** stdout or stderr emits a line per persona indicating the fetch/install progress (e.g. "Installed <name>" or equivalent), and the command exits 0 only after all roster personas are processed

**Scenario 5: Empty registry index yields a clear, non-silent failure**
- **Given** a mock registry whose `index.json` contains an empty array `[]`
- **When** `atcr init` runs without `--offline`
- **Then** the command exits non-zero with a descriptive error (e.g. "community registry index is empty" or "no personas found in community index"), and no persona files are written

## Edge Cases
**Edge Case 1: Registry index lists fewer personas than the built-in roster**
- **Given** a mock registry whose `index.json` omits one roster persona
- **When** `atcr init` runs without `--offline`
- **Then** the available personas are installed and pinned, and the missing one is either skipped with a clear warning or causes a descriptive failure (single documented behavior, exercised by test)

**Edge Case 2: Community YAML lacks a `version` field**
- **Given** a mock registry persona YAML with no `version` key
- **When** `atcr init` fetches and installs it
- **Then** the installed persona shows version `"-"` (matching existing `personaFileMeta`/`versionOf` fallback behavior) rather than crashing or leaving the persona unversioned in an inconsistent way

## Error Conditions
**Error Scenario 1: Community YAML fails registry validation**
- Error message: `persona "<name>" failed validation: <validation detail>"` (matching existing `registry.ValidateAgentYAML` wrapping in `Install`)
- HTTP status / error code: N/A (local validation failure after a successful fetch); `atcr init` exits non-zero and writes nothing for that persona

## Performance Requirements
- **Response Time:** `atcr init`/`atcr quickstart` complete within the existing `fetchTimeout` (30s) per persona fetch; total wall time bounded by roster size × per-fetch timeout in the worst case.
- **Throughput:** One fetch per roster persona (or one index + N YAML fetches); no batching requirement beyond what `Install`/`InstallBundle` already provide.

## Security Considerations
- **Authentication/Authorization:** None required; fetch remains anonymous HTTPS GET as with existing `personas install`.
- **Input Validation:** Every fetched persona YAML passes `registry.ValidateAgentYAML` before any disk write (reusing `Install`'s existing validate-before-write ordering), so malformed or malicious community content never reaches `.atcr/personas/`.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Mock `index.json` + per-persona YAML fixtures with distinct `version` values; a second fixture set with a bumped version for the upgrade scenario.
**Mock/Stub Requirements:** `httptest.NewServer` for the registry; `ATCR_PERSONAS_URL` env override; `personasClient` package var swap where a direct `HTTPClient` injection is more convenient than env override.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `runInit` installs personas via fetch-and-pin against `commpersonas.BaseURL()` instead of embedded built-ins (default path)
- [ ] `runQuickstart` inherits the same behavior through its `runInit` call
- [ ] Recorded pin matches the fetched YAML's `version` field, readable via `atcr personas list`
- [ ] `atcr personas upgrade` continues to compare against and advance the recorded pin with no logic change required

**Manual Review:**
- [ ] Code reviewed and approved
