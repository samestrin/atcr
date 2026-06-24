# User Story 2: Personas CLI: Discovery and Lifecycle

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Go developer integrating ATCR into a new domain (e.g., security, Django)
**I want** to install, list, search, test, and upgrade community personas from the command line
**So that** I can extend ATCR's reviewer panel without editing YAML manually or reading source code

## Story Context

- **Background:** ATCR ships with 6 generalist built-in personas and (after Sprint A) 3 bonus domain personas. Vertical adoption requires a long tail of community-contributed personas. The `atcr personas` CLI is the primary interface for discovering and managing those community personas. It fetches from a configurable registry URL backed by the community repo, installs persona `.md` files to `~/.config/atcr/personas/`, and exposes install/remove/list/search/test/upgrade subcommands. All network I/O in tests uses `httptest.NewServer` — no live external calls in CI.
- **Assumptions:** The community persona registry serves raw YAML index and individual `.md` files over HTTPS. The default registry URL (`https://raw.githubusercontent.com/atcr/personas/main`) is a compile-time constant in `internal/personas`. Users have write access to `~/.config/atcr/personas/`. The `atcr personas test` subcommand reuses the fixture-based test harness introduced in Sprint A (T1).
- **Constraints:** The implementation must not introduce external dependencies beyond what already exists (`cobra`, `yaml.v3`, `net/http` stdlib). The subcommand count assertion in `cmd/atcr/main_test.go` (`TestRootCmd_HasExactlyFourteenSubcommands`) must be updated to 15 atomically with the registration of `newPersonasCmd()` to avoid a CI failure window. Registry URL must be overridable via an environment variable or flag for testability and enterprise use.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (bonus personas + fixture harness — `atcr personas test` reuses that infrastructure); Story 3 (Language field on `AgentConfig` — installed personas may declare `language`) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas install security/owasp` fetches the persona `.md` from the configured registry URL, writes it to `~/.config/atcr/personas/security/owasp.md`, and prints a confirmation. `atcr personas list` shows all built-in and installed personas with name, source (built-in vs. installed), and version. `atcr personas search <keyword>` queries the registry index and returns matching persona names and short descriptions. `atcr personas test <name>` runs the persona's fixture and reports pass/fail. `atcr personas upgrade <name>` checks the registry for a newer version and replaces the local file if one exists. `atcr personas remove <name>` deletes the installed file and confirms removal.
- **Measurable:** All 6 subcommands (`install`, `remove`, `list`, `search`, `test`, `upgrade`) exist as registered cobra sub-subcommands under `atcr personas`. Unit tests for each subcommand use `httptest.NewServer` and achieve pass on `go test ./...`. `TestRootCmd_HasExactlyFourteenSubcommands` is updated to 15 and passes. No live network calls occur in CI.
- **Achievable:** The new `internal/personas` package contains the HTTP client and file I/O logic, keeping `cmd/atcr/personas.go` as a thin cobra wiring layer. The HTTP fetch path mirrors the established pattern in `internal/verify/invoke_test.go`. No novel external dependencies are needed.
- **Relevant:** This CLI is the primary adoption lever for vertical markets. Without it, community personas require manual file placement and registry knowledge, blocking non-Go teams from extending ATCR.
- **Time-bound:** Delivered within Sprint B (9.0), which follows Sprint A's completion of the bonus personas and language routing foundation.

## Acceptance Criteria Overview

1. `atcr personas install <name>` fetches from the configured registry, writes to `~/.config/atcr/personas/`, and exits 0 on success; exits non-zero with a descriptive error when the persona is not found or the network is unavailable.
2. `atcr personas list` displays built-in and installed personas, distinguishing source and version; `atcr personas list --scores` additionally shows corroboration rate from existing scorecard data (wired in T6 but the flag must be defined here).
3. `atcr personas search <keyword>` queries the registry index endpoint and returns matching results; `atcr personas test <name>` runs the fixture for the named persona and reports pass/fail; `atcr personas upgrade <name>` checks for and applies updates; `atcr personas remove <name>` deletes the installed persona and confirms.
4. All HTTP fetch logic lives in `internal/personas` and is fully exercised by `httptest.NewServer`-backed unit tests — no live external calls in any test.
5. `TestRootCmd_HasExactlyFourteenSubcommands` is updated to 15 in the same commit that registers `newPersonasCmd()`, and the full test suite passes without modification to any unrelated test.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** New file `cmd/atcr/personas.go` with `newPersonasCmd()` returning a `*cobra.Command` with 6 sub-subcommands registered via `personasCmd.AddCommand(...)`. New package `internal/personas/` with `client.go` (HTTP fetch, configurable `RegistryBaseURL`), `store.go` (read/write `~/.config/atcr/personas/`), and `index.go` (registry index parsing). The `--registry` flag on the parent `personas` command overrides `RegistryBaseURL` and is inherited by all sub-subcommands via `PersistentFlags()`. Output goes through `cmd.OutOrStdout()` for testability. The `atcr personas list --scores` flag must be declared in this story even if the score-reading logic is wired in T6 — the flag declaration and a no-op/zero-value fallback belong here.
- **Integration Points:** `cmd/atcr/main.go:174-189` — `root.AddCommand(newPersonasCmd())`; `cmd/atcr/main_test.go` — subcommand count assertion update (14 → 15); `internal/personas/client_test.go` — `httptest.NewServer` pattern matching `internal/verify/invoke_test.go`; `personas/` package — `Get(name)` used by `atcr personas test` to resolve built-in persona fixtures; `internal/scorecard` — queried by `--scores` flag path (T6 integration point).
- **Data Requirements:** Registry index format: a YAML or JSON file listing persona names, versions, short descriptions, and raw file URLs. Installed persona format: a `.md` file at `~/.config/atcr/personas/<category>/<name>.md` with a YAML frontmatter block containing at minimum `name`, `version`, and optionally `language`. The `internal/personas` package must define these structs with `yaml.v3` tags.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Subcommand count test CI failure window if registration and test update land in separate commits | High | Update `TestRootCmd` count and register `newPersonasCmd()` in the same atomic commit; enforced by the TDD RED step requiring the count test to be updated before the command is wired |
| Registry URL shape changes between index design and T5 bundle resolution | Medium | Pin the registry URL shape to the `internal/personas.RegistryBaseURL` constant; T5 bundle resolver imports the same constant and the same `client.go` — a single change point |
| `~/.config/atcr/personas/` path not writable in restricted environments (CI, containers) | Medium | Accept an `--install-dir` flag (or `ATCR_PERSONAS_DIR` env var) that overrides the default install path; tests always use `t.TempDir()` as the install root |
| `atcr personas list --scores` flag declared here but wired in T6 creates a blank-output risk if T6 slips | Low | Return zero/empty score values with a `(no score data)` annotation when the scorecard map is nil; avoids a broken UX while T6 is pending |
| `go:embed` or file naming collision between built-in and installed persona names | Low | Built-in personas are resolved via `personas.Get(name)`; installed personas are resolved via `internal/personas.Store.Get(name)` — separate lookup paths with explicit precedence (built-in wins on name collision) |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
