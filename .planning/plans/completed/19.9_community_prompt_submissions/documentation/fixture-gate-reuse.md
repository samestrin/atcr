# Local Fixture-Gate Reuse (TestPersona)

`[CRITICAL]`

## Overview

The `personas submit <name>` subcommand must not open a pull request for a persona that fails its own fixture check. The codebase already has this gate: `TestPersona` in `internal/personas/test.go:195` runs a persona's committed patch fixture through a `FixtureRunner` and reports a pass/fail outcome, and `TemplateFixtureRunner` (`internal/personas/test.go:33`) is the production implementation `personas test` uses — it resolves built-in, on-disk installed, or embedded community-library fixtures and renders the persona template against the committed fixture with zero network and zero LLM calls.

`submit` must call this exact gate — reusing `TestPersona` plus `TemplateFixtureRunner` — before any GitHub call (fork/PR work), because the fixture check is local-only and cheap, while forking and opening a PR are neither. If the fixture is failing or absent, submission must block with a clear error per AC1, matching the manual "fixture presence/pass" step already documented in the contribution checklist (`docs/personas-authoring.md:162`) but enforcing it automatically instead of relying on the contributor to have run it by hand.

Before `submit` can even reach the fixture gate, it must validate the persona `<name>` argument and resolve it to an on-disk path using the same helpers already shared by install/search/remove: `validatePersonaName` (`internal/personas/paths.go:42`) enforces the `[a-zA-Z0-9_/-]` character allowlist and rejects `..` sequences and absolute paths, and `personaPath` (`internal/personas/paths.go:72`) maps a validated name to its `.yaml` path under a personas directory, confirming the cleaned result stays inside that directory. Reusing these instead of reimplementing path-traversal protection keeps `submit` consistent with the rest of the `personas` command family and closes off a class of path-traversal bugs before the fixture gate or any fork/PR logic ever runs.

## Key Concepts

- `TestPersona(name string, runner FixtureRunner) (FixtureOutcome, error)` is the exact call signature `submit`'s local gate must invoke before allowing a PR to open; a failing or absent fixture must block submission per AC1.
  > Source: internal/personas/test.go:195

- `TemplateFixtureRunner` is the production `FixtureRunner` used by `personas test`; it resolves built-in vs. on-disk installed vs. embedded community-library fixtures without any LLM or network call.
  > Source: internal/personas/test.go:33

- `TemplateFixtureRunner.RunFixture` renders the persona template against its committed patch fixture and asserts no unrendered `{{ }}` markers remain — this is a zero-network, zero-LLM-call check, which is exactly why it belongs before any GitHub interaction.
  > Source: internal/personas/test.go (existing_patterns: "FixtureRunner interface + local-only render check")

- `validatePersonaName` centralizes name validation via regex `[a-zA-Z0-9_/-]` and rejects `..` and absolute paths; `submit` must validate the provided `<name>` before resolving its on-disk path or forking, reusing this same guard already applied to install/search/remove.
  > Source: internal/personas/paths.go:42

- `personaPath` maps a validated persona name to its `.yaml` path under a personas directory and confirms the cleaned result stays within that directory; `submit` uses this (directly or via `PersonasDir`) to locate the unit to copy into the fork.
  > Source: internal/personas/paths.go:72

- `internal/registry/persona.go`'s `validateName` mirrors the same name-validation rules for the resolver, so it is an equivalent guard `submit` could rely on for the same purpose.
  > Source: internal/personas/paths.go (existing_patterns: "Persona name validation and safe path resolution"), internal/registry/persona.go

- The manual "Contribution checklist" (YAML validity, fixture presence/pass, no secrets) is the existing human process that `personas submit`'s automated local gate effectively codifies for AC1.
  > Source: docs/personas-authoring.md:162

## Code Examples

Fixture gate call `submit` must run before any fork/PR work:

```go
outcome, err := commpersonas.TestPersona(name, personasFixtureRunner)
if err != nil || outcome.Passed != outcome.Total {
    return fmt.Errorf("fixture gate failed for %q", name)
}
```

Name validation and path resolution `submit` must reuse before locating the unit to submit:

```go
dir, err := commpersonas.PersonasDir()
if err != nil {
    return err
}
if !commpersonas.ValidName(name) {
    return fmt.Errorf("invalid persona name %q", name)
}
// Path resolution (<name>.yaml plus optional co-located <name>.md) is handled
// inside internal/personas, reusing personaPath(dir, name) so the cleaned
// result stays within the personas directory.
```

## Quick Reference

| File | Symbol | Signature / Purpose |
|------|--------|----------------------|
| internal/personas/test.go:195 | `TestPersona` | `TestPersona(name string, runner FixtureRunner) (FixtureOutcome, error)` — runs the fixture gate for a persona; failing/absent fixture must block submission |
| internal/personas/test.go:33 | `TemplateFixtureRunner` | Production `FixtureRunner`; resolves built-in/installed/embedded fixtures, renders template with no network/LLM call |
| internal/personas/paths.go:42 | `validatePersonaName` | Validates `name` against `[a-zA-Z0-9_/-]`, rejects `..` and absolute paths |
| internal/personas/paths.go:72 | `personaPath` | Maps a validated name to its `.yaml` path under a personas directory; guards result stays within that directory |
| internal/registry/persona.go | `validateName` | Mirrors the same name-validation rules for the resolver |
| docs/personas-authoring.md:162 | "Contribution checklist" | Manual pre-PR checklist (YAML validity, fixture presence/pass, no secrets) that the automated gate codifies |

## Related Documentation

- internal/personas/test.go
- internal/personas/paths.go
- internal/registry/persona.go
- docs/personas-authoring.md
