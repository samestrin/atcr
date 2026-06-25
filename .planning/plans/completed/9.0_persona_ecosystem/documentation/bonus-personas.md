# Bonus Built-In Personas [CRITICAL]

## Overview

Sprint 9.0 ships three domain-specific personas bundled with the ATCR binary, expanding the reviewer panel from six generalists to nine. These personas require no install step and are embedded alongside the existing personas via `go:embed *.md` in `personas/personas.go`.

| Persona | Focus | Expected Finding Categories |
|---------|-------|----------------------------|
| `sentinel` | Security | OWASP Top 10, injection, auth bypass, secrets leakage, insecure defaults |
| `tracer` | Performance | N+1 queries, memory leaks, algorithmic complexity, unnecessary allocations |
| `idiomatic` | Go idioms | Error handling conventions, goroutine leaks, interface abuse, stdlib misuse |

> Source: [original-requirements.md / Phase 1 — Bonus built-in personas]

## Key Concepts

### Embedded Persona Registration

The `personas/personas.go:names` slice enumerates the embedded personas in canonical order. It grows from six entries to nine when `sentinel`, `tracer`, and `idiomatic` are added. The `//go:embed *.md` directive automatically includes any new `.md` files placed in `personas/`, but only names listed in `names` are exposed through `Names()` and `Get()`.

> Source: [codebase-discovery.json / Pattern "Embedded Persona Files"]

### Test Fixtures

Each bonus persona is accompanied by a small diff fixture in `personas/testdata/` and a test that verifies the persona produces at least one expected finding category when run against that fixture. The fixtures are plain `.patch` or `.diff` files committed to the repo; CI invokes the persona test path (ultimately `atcr personas test <name>` once the T2 CLI lands, or an equivalent internal test during Sprint A).

| Persona | Fixture | Target Pattern |
|---------|---------|----------------|
| `sentinel` | `personas/testdata/sentinel_fixture.patch` | SQL injection or hardcoded secret |
| `tracer` | `personas/testdata/tracer_fixture.patch` | N+1 query or unbounded allocation |
| `idiomatic` | `personas/testdata/idiomatic_fixture.patch` | Ignored error or goroutine leak |

> Source: [codebase-discovery.json / files_to_create: personas/testdata/*_fixture.patch]

### Canonical Order

The existing order is generalist-first, style-last: `bruce`, `greta`, `kai`, `mira`, `dax`, `otto`. The three domain personas slot immediately before `otto` so the order becomes:

```
bruce, greta, kai, mira, dax, sentinel, tracer, idiomatic, otto
```

> Source: [personas/personas.go / names slice comment]

## Code Examples

### Declaring an embedded persona

```go
//go:embed *.md
var files embed.FS

var names = []string{
    "bruce", "greta", "kai", "mira", "dax",
    "sentinel", "tracer", "idiomatic",
    "otto",
}
```

> Source: [codebase-discovery.json / Reusable component "go:embed persona files"]

### Persona file structure

Each new persona is a markdown file following the same text/template structure as `personas/bruce.md`. The system prompt section declares the persona's lens, severity rubric, and output format; the rest of the file is rendered with payload variables by the fan-out engine.

> Source: [personas/bruce.md]

## Quick Reference

| Item | Detail |
|------|--------|
| Persona directory | `personas/` |
| Registration file | `personas/personas.go` |
| Count change | 6 → 9 |
| Test to update | `personas/personas_test.go:TestNames_ReturnsAllSix` → `TestNames_ReturnsAllNine` |
| Fixture directory | `personas/testdata/` |
| Fixture format | `.patch` or `.diff` |
| CI verification | Each bonus persona produces its expected category on its fixture |
| Sprint order | T1 follows T8 so personas can optionally declare `language:` once `AgentConfig.Language` exists |

## Related Documentation

- `personas/personas.go` — embedded persona registry and `names` slice
- `personas/personas_test.go` — update expected count from 6 to 9
- `personas/bruce.md` — template structure to follow for new personas
- [YAML Bundle Manifests](yaml-bundle-manifests.md) — `AgentConfig.Language` field that bonus personas may optionally use
- [Skeptic Routing & Verification](skeptic-routing-verification.md) — how language-scoped personas are preferred in verification
