# Cobra Subcommand & Injectable-Seam Conventions

`[CRITICAL]`

## Overview

The `personas` command tree in `cmd/atcr/personas.go` is organized around a single parent command, `newPersonasCmd`, which registers each verb (install, list, search, remove, test, upgrade) as its own `newPersonas<Verb>Cmd() *cobra.Command` function via `cmd.AddCommand(...)`.

> Source: cmd/atcr/personas.go:94

Each subcommand's `RunE` closure calls into the `internal/personas` package for the actual logic and formats output with `fmt.Fprintf` to `cmd.OutOrStdout()` or `cmd.ErrOrStderr()`, keeping the cobra layer thin and the business logic testable independently of the CLI wiring.

> Source: cmd/atcr/personas.go (existing_patterns: "Cobra subcommand registration")

The new `submit` subcommand introduces an external dependency the existing six verbs do not have: a call out to the `gh` CLI (or a GitHub client) to open a pull request. Because CLI tests must not shell out to a real binary or hit the network, this dependency needs the same injectable-seam treatment already used for `personasDir`, `personasClient`, and `personasFixtureRunner` — package-level vars holding the production implementation, swapped for stubs in tests via `withFixtureRunner`-style helpers. Following this convention lets `submit`'s tests assert PR-URL output and precondition failures without any real `gh` invocation.

> Source: cmd/atcr/personas.go, cmd/atcr/personas_test.go (existing_patterns: "Injectable seams for testability")

## Key Concepts

- The `personas` parent command (`newPersonasCmd`) is where all subcommands are registered; `submit` is added here via `cmd.AddCommand(newPersonasSubmitCmd())`, alongside the existing six subcommands.
  > Source: cmd/atcr/personas.go:94

- `newPersonasTestCmd` is the existing subcommand that calls `TestPersona`/`personasFixtureRunner` — this is the exact fixture-gate call `submit` should reuse (via `commpersonas.TestPersona(name, personasFixtureRunner)`) before making any GitHub call, so a persona is validated locally before a PR is opened.
  > Source: cmd/atcr/personas.go:298

- Cobra subcommand registration convention: each verb is its own `newPersonas<Verb>Cmd() *cobra.Command` function, added via `cmd.AddCommand(...)` in `newPersonasCmd()`; `RunE` closures call into `internal/personas` and format output with `fmt.Fprintf` to `cmd.OutOrStdout()`/`cmd.ErrOrStderr()`. `submit`'s cobra wiring, argument validation (`usageArgs(cobra.ExactArgs(1))`), and output formatting should follow this same pattern.
  > Source: cmd/atcr/personas.go (existing_patterns: "Cobra subcommand registration")

- Injectable seams for testability: package-level vars (`personasDir`, `personasClient`, `personasFixtureRunner`) hold the production implementation but are swapped in tests via `withFixtureRunner`-style helpers, keeping CLI tests free of real network/filesystem calls. Any new external dependency `submit` introduces (a `gh` CLI invoker, a GitHub client) should be an injectable package var/interface so its tests can stub it out rather than shelling out for real.
  > Source: cmd/atcr/personas.go, cmd/atcr/personas_test.go (existing_patterns: "Injectable seams for testability")

- `executeSplit` is the existing CLI test helper that separates stdout/stderr; `submit`'s tests can follow this pattern to assert PR-URL output on stdout and diagnostic precondition errors on stderr.
  > Source: cmd/atcr/personas_test.go:35

- Test naming follows `Test<Subject>_<Scenario>`, e.g. `TestPersonasTest_DefaultRunnerBuiltinFixture`, and lives in `cmd/atcr/personas_test.go` and `internal/personas/*_test.go`; the framework is Go's standard `testing` package (testify is not used in these files).
  > Source: cmd/atcr/personas_test.go:669

## Quick Reference

| File | Symbol / Pattern | Purpose |
|---|---|---|
| cmd/atcr/personas.go:94 | `newPersonasCmd` | Parent command that registers all `personas` subcommands; `submit` is added here |
| cmd/atcr/personas.go:298 | `newPersonasTestCmd` | Existing subcommand calling `TestPersona`/`personasFixtureRunner` — the fixture gate `submit` should reuse before opening a PR |
| cmd/atcr/personas.go | Cobra subcommand registration pattern | Each verb is a `newPersonas<Verb>Cmd()` added via `cmd.AddCommand(...)`; `RunE` calls `internal/personas`, formats via `fmt.Fprintf` to `cmd.OutOrStdout()`/`cmd.ErrOrStderr()` |
| cmd/atcr/personas.go, cmd/atcr/personas_test.go | Injectable seams (`personasDir`, `personasClient`, `personasFixtureRunner`) | Package-level vars swapped in tests via `withFixtureRunner`-style helpers to avoid real network/filesystem calls |
| cmd/atcr/personas_test.go:35 | `executeSplit` | CLI test helper separating stdout/stderr; model for asserting `submit`'s PR-URL output vs. diagnostic errors |
| cmd/atcr/personas_test.go:669 | `TestPersonasTest_DefaultRunnerBuiltinFixture` | Example test following the `Test<Subject>_<Scenario>` naming convention |
| cmd/atcr/main_test.go | `execute(t, args...)` | Shared combined stdout+stderr CLI test helper and subcommand-count assertions; reference-only, `submit` tests may use either `execute` or `executeSplit` |

## Related Documentation

- [cmd/atcr/personas.go](cmd/atcr/personas.go)
- [cmd/atcr/personas_test.go](cmd/atcr/personas_test.go)
- [cmd/atcr/main_test.go](cmd/atcr/main_test.go)
