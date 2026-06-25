# Acceptance Criteria: Personas Install Guide

**Related User Story:** [06: In-Repo Documentation for Persona Installation and Authoring](../user-stories/06-in-repo-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown | `docs/personas-install.md` — new file |
| CLI Reference Source | Go / Cobra | Subcommand flag definitions in `cmd/personas*.go` |
| Test Framework | Manual walkthrough | No automated test; verified by doc review and walkthrough |
| Key Dependencies | T2 (`atcr personas` CLI), T5 (bundle install) | Must be authored after T2 and T5 are merged |

## Related Files
- `docs/personas-install.md` - create: new installation guide covering all six subcommands and bundle installation
- `cmd/` (personas subcommand files) - reference: source of truth for flag names and install path constants used in the guide

### Related Files (from codebase-discovery.json)

- `docs/personas-install.md` — create: installation guide
- `cmd/atcr/personas.go` — source of truth for `install`, `remove`, `list`, `search`, `test`, `upgrade` flags
- `internal/personas/bundles.go` — bundle resolution for `install bundle/<name>` documentation
- `internal/personas/paths.go` — `PersonasDir()` install path
- `internal/personas/client.go` — `RegistryBaseURL` default and override mechanism

## Happy Path Scenarios

**Scenario 1: Developer installs a community persona using the guide**
- **Given** a developer has ATCR installed and has never used the `atcr personas` CLI
- **When** they follow `docs/personas-install.md` step-by-step to run `atcr personas install <slug>`
- **Then** the persona is downloaded to `~/.config/atcr/personas/` and the guide's expected output matches the actual CLI output exactly, with no source-code lookup required

**Scenario 2: Developer lists installed personas**
- **Given** at least one persona is installed at `~/.config/atcr/personas/`
- **When** they run `atcr personas list` as shown in the guide
- **Then** the output format described in the guide matches the actual CLI output

**Scenario 3: Developer runs the fixture test for an installed persona**
- **Given** a persona with a fixture file is installed
- **When** they run `atcr personas test <slug>` as shown in the guide
- **Then** the command executes successfully and the guide's description of pass/fail output is accurate

**Scenario 4: Developer installs a bundle**
- **Given** a named bundle exists in the configured registry
- **When** they run `atcr personas install bundle/<name>` as shown in the guide
- **Then** all personas in the bundle are installed to `~/.config/atcr/personas/` and the guide's bundle installation section accurately describes the command syntax

**Scenario 5: Developer upgrades an installed persona**
- **Given** a persona is already installed and a newer version exists in the registry
- **When** they run `atcr personas upgrade <slug>` as documented
- **Then** the persona is updated and the guide's description of upgrade behavior is accurate

**Scenario 6: Developer removes a persona**
- **Given** a persona is installed
- **When** they run `atcr personas remove <slug>` as documented
- **Then** the persona is removed from `~/.config/atcr/personas/` and the guide accurately describes the command

## Edge Cases

**Edge Case 1: Configurable registry base URL**
- **Given** an organization uses a private persona registry
- **When** they consult `docs/personas-install.md` for configuration instructions
- **Then** the guide explains how to set the registry base URL (environment variable or config flag) with a working example

**Edge Case 2: Searching the registry before installing**
- **Given** a developer does not know the exact persona slug
- **When** they consult the guide's `search` section and run `atcr personas search <keyword>`
- **Then** the guide's description of search output accurately matches the CLI

**Edge Case 3: Install path already contains a persona with the same slug**
- **Given** a persona is already installed
- **When** the developer attempts `atcr personas install <same-slug>` again
- **Then** the guide's "already installed" behavior description matches the actual CLI error or prompt

## Error Conditions

**Error Scenario 1: Unknown persona slug**
- **Given** the developer specifies a slug that does not exist in the registry
- **When** `atcr personas install <unknown-slug>` is run
- **Then** the guide documents the error message that the CLI produces (e.g., `persona "<slug>" not found in registry`)
- Error code: non-zero exit

**Error Scenario 2: Network unavailable**
- **Given** the developer has no network access
- **When** `atcr personas install <slug>` is run
- **Then** the guide notes the expected network-error message and suggests the configurable registry URL as a workaround

## Performance Requirements
- **Response Time:** Not applicable — this is a documentation file; no runtime performance requirement
- **Throughput:** Not applicable

## Security Considerations
- **Authentication/Authorization:** The guide must not document or imply that unauthenticated personas from untrusted registries are safe to run; it should note that personas are executed as part of the review pipeline
- **Input Validation:** No user-supplied input beyond CLI flags; no XSS or injection surface

## Test Implementation Guidance
**Test Type:** MANUAL (documentation walkthrough)
**Test Data Requirements:** A test ATCR environment with at least one available community persona slug in the configured registry
**Mock/Stub Requirements:** None — walkthrough is performed against the real CLI
**Verification Method:** A developer unfamiliar with the codebase follows the guide start-to-finish and installs a persona, lists it, tests its fixture, and removes it without consulting source code. Any step requiring source-code lookup is a failure.

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./...` — exercises registry loading; example files referenced in docs must remain valid)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `docs/personas-install.md` exists and covers all six subcommands: `install`, `remove`, `list`, `search`, `test`, `upgrade`
- [x] Bundle installation syntax (`atcr personas install bundle/<name>`) is documented with a working example
- [x] `~/.config/atcr/personas/` install path is explicitly documented
- [x] Configurable registry base URL is documented
- [x] No reference to the deprecated `docs/examples/registry.yaml` path appears in the file

**Manual Review:**
- [x] Code reviewed and approved
