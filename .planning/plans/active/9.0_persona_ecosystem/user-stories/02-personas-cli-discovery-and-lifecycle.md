# User Story 2: Personas CLI: Discovery and Lifecycle

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Go developer or platform lead managing team review configuration
**I want** a first-class CLI (`atcr personas`) with `install`, `remove`, `list`, `search`, `test`, and `upgrade` subcommands that fetch from a community repo
**So that** my team can discover, install, validate, and keep current domain-specific personas without reading source code or manually managing YAML files

## Story Context

- **Background:** ATCR ships with 6 generalist built-in personas and (after Story 1) 3 domain bonus personas. Beyond those 9, vertical-market teams need security, framework, or language personas that do not ship with the binary. Today the only path is writing raw YAML config, which requires reading internal source code and creates a high adoption barrier. The `atcr personas` CLI provides a discoverable, lifecycle-managed alternative backed by a community repo fetched over HTTP.
- **Assumptions:** A community repo raw endpoint exists at `https://raw.githubusercontent.com/atcr/personas/main` (configurable). Installed personas land in `~/.config/atcr/personas/` and are loaded by the existing registry at startup. The registry already supports loading from that directory. HTTP tests use `httptest.NewServer` — no live network calls reach CI.
- **Constraints:** All HTTP fetch logic must be testable via `httptest.NewServer`. The `atcr personas` subcommand must be registered atomically with the `TestRootCmd_HasExactlyFourteenSubcommands` test update (bumped to 15) to avoid a CI failure window. No new external dependencies — `github.com/spf13/cobra` and `net/http` (stdlib) are sufficient.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (bonus personas + binary embed foundation); existing Cobra CLI scaffold in `cmd/atcr/`; existing registry loading from `~/.config/atcr/personas/` |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas install security/owasp` fetches the persona YAML from the community repo, writes it to `~/.config/atcr/personas/security/owasp.yaml`, and makes it immediately available in the registry without restarting the tool.
- **Measurable:** All 6 subcommands (`install`, `remove`, `list`, `search`, `test`, `upgrade`) pass their unit tests using `httptest.NewServer`; `TestRootCmd_HasExactlyFourteenSubcommands` updated to 15 and green; zero live HTTP calls in CI.
- **Achievable:** Greenfield `internal/personas` package with a configurable `RegistryBaseURL` constant; Cobra subcommands follow the patterns already established in `cmd/atcr/`; no new external dependencies required.
- **Relevant:** Removes the primary adoption barrier for vertical-market teams — teams gain domain review coverage without reading internal source code or crafting raw YAML.
- **Time-bound:** Delivered within Sprint B (after Sprint A lands T8 and T1); all subcommands functional and tested before Sprint B PR is opened.

## Acceptance Criteria Overview

1. `atcr personas install <name>` fetches the named persona from the configured repo URL, writes the YAML to `~/.config/atcr/personas/<name>.yaml`, and exits 0 on success; exits non-zero with a descriptive error if the persona is not found or the fetch fails.
2. `atcr personas list` prints all installed personas (built-in + community) with name, version, and source columns; `--scores` flag deferred to Story 5 (T6).
3. `atcr personas search <keyword>` queries the community repo index and prints matching persona names and descriptions; results are deterministic in tests via `httptest.NewServer`.
4. `atcr personas remove <name>` deletes `~/.config/atcr/personas/<name>.yaml` and exits 0; exits non-zero with a descriptive error if the persona is not installed.
5. `atcr personas test <name>` runs the persona's fixture and reports pass/fail to stdout; exit code mirrors test outcome.
6. `atcr personas upgrade <name>` (or `--all`) re-fetches the persona(s) and overwrites the local file only when the remote version is newer; a `--dry-run` flag prints what would change without writing.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** New `internal/personas` package owns all install/list/search/upgrade logic. A `RegistryBaseURL` constant (default: `https://raw.githubusercontent.com/atcr/personas/main`) is the single configuration point; override via env var `ATCR_PERSONAS_URL` or a flag. `newPersonasCmd()` in `cmd/atcr/personas.go` registers all 6 sub-subcommands and is added to root in the same commit that bumps `TestRootCmd_HasExactlyFourteenSubcommands` to 15. All HTTP interaction is gated behind an injectable `http.Client` so tests can substitute `httptest.NewServer` without touching production paths.
- **Integration Points:** Registry startup scan of `~/.config/atcr/personas/` (already implemented); Cobra root command in `cmd/atcr/main.go`; `TestRootCmd_HasExactlyFourteenSubcommands` in `cmd/atcr/main_test.go`; `atcr personas test` delegates to the same fixture runner used by `sentinel`/`tracer`/`idiomatic` in Story 1.
- **Data Requirements:** Community repo exposes a JSON index at `<RegistryBaseURL>/index.json` listing available personas (name, version, description, path). Each persona is a single YAML file at `<RegistryBaseURL>/<name>.yaml`. The local store uses the same YAML schema as built-in personas; no new schema fields are required for the CLI itself.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Subcommand count test CI failure window (registering `newPersonasCmd` and bumping the count in separate commits) | High | Register `newPersonasCmd()` and update `TestRootCmd_HasExactlyFourteenSubcommands` to 15 in the same atomic commit |
| Community repo endpoint unavailable in CI causing flaky tests | High | All HTTP interaction uses an injected `http.Client`; CI tests exclusively use `httptest.NewServer` — no live network calls |
| `~/.config/atcr/personas/` directory missing on fresh install | Medium | `install` creates the directory (and any `<name>/` subdirectory) with `os.MkdirAll` before writing; `list` treats a missing directory as an empty set (no error) |
| Version comparison logic brittle across semver vs. date-stamped releases | Medium | Use `golang.org/x/mod/semver` (already a transitive dependency) for structured comparison; fall back to string equality if the version field is non-semver |
| Persona YAML from community repo fails existing registry validation | Low | `install` runs `validateAgent` against the fetched YAML before writing to disk; returns a descriptive error without writing if validation fails |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
