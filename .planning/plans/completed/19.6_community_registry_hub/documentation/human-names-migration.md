# Human-Names Migration for Built-in Stragglers [IMPORTANT]

## Overview

atcr's persona authoring convention requires every persona to use a human first name (AC4/AC8). The active built-in set still contains three role-based names — `sentinel`, `tracer`, and `idiomatic` — that predate this rule. This plan migrates them into the in-repo community layout under human names (`sasha`, `penny`, `ingrid`) and generalizes `ingrid` beyond Go, eliminating all role-based names from the active persona set. The migration folds in Epic 23.0 so the rename is not double-implemented.

The migration is content and metadata, not a new runtime mechanism: the personas keep their existing lenses (security, performance, language-aware review) and their fixtures keep the same target categories; only the persona slug, prompt template, and fixture filename change.

## Key Concepts

- **All-human-names convention.** Every persona that ships in the active set — built-in or community — must be identified by a human first name. Role-based slugs (`sentinel`, `tracer`, `idiomatic`) are retired.
  > Source: [personas-authoring.md](../../../../../docs/personas-authoring.md) contribution checklist, original-requirements.md (AC4/AC8)

- **Straggler mapping.** The three role-named built-ins map to human names as follows:
  - `sentinel` → `sasha` (security / OWASP-flavored lens)
  - `tracer` → `penny` (performance / N+1, allocation, latency lens)
  - `idiomatic` → `ingrid` (idiomatic / clean-code lens, generalized beyond Go)
  > Source: plan.md (Theme 5), original-requirements.md (AC3/AC4)

- **Built-in vs community layout.** The migrated personas can either remain built-ins (updating `personas/personas.go` and the embedded file set) or move entirely to `personas/community/` (removing them from the binary). This is an open integration decision, but the same file-set changes are required in either case:
  - Rename the prompt template (`personas/<old>.md` → `personas/<new>.md` or `personas/community/<new>.md`).
  - Rename the fixture (`personas/testdata/<old>_fixture.patch` → `personas/testdata/<new>_fixture.patch` or `personas/community/testdata/<new>_fixture.patch`).
  - Update the YAML metadata to use the new slug and to carry `provider`/`model` structured metadata (AC2/AC7).
  - Update the canonical names slice in `personas/personas.go` if they stay built-in, or remove them from it if they become community-only.
  > Source: codebase-discovery.json ("Straggler migration scope" integration gap)

- **`ingrid` is generalized beyond Go.** The current `idiomatic` prompt is Go-specific. The replacement `ingrid` persona should be phrased as a general clean-code / idioms reviewer usable across languages, matching the human-name migration's intent.
  > Source: original-requirements.md (AC4)

- **No mixed naming state.** The rename and the new model-indexed library land together; there must never be a release where some active personas use role names and others use human names. This is why the straggler migration is folded into this plan rather than left to Epic 23.0 separately.
  > Source: original-requirements.md (Dependencies / Related)

## Migration Checklist

For each of `sentinel` → `sasha`, `tracer` → `penny`, `idiomatic` → `ingrid`:

- [ ] Rename prompt template file and update `{{.AgentName}}` references inside it.
- [ ] Rename fixture file; verify the target category word still appears in the prompt template.
- [ ] Author or update the persona YAML with the new slug, a `version`, `description`, and structured `provider`/`model` metadata.
- [ ] Add the persona to `personas/community/index.json` with the matching `provider`/`model`/`tasks`/`tags` fields.
- [ ] Update `personas/personas.go` canonical `names` slice and embedded-file expectations, or remove the slug if it becomes community-only.
- [ ] Update any docs or examples that reference the old role-based name (e.g. `docs/personas-install.md`, `docs/personas-authoring.md` worked examples).
- [ ] Run `atcr personas test <new-name>` and confirm the fixture passes.

## Related Documentation

- [Authoring a Persona](../../../../../docs/personas-authoring.md)
- [Installing Community Personas](../../../../../docs/personas-install.md)
- [Built-in persona registry](../../../../../personas/personas.go)
- [../../../../specifications/packages/yaml-v3.md](../../../../specifications/packages/yaml-v3.md)
