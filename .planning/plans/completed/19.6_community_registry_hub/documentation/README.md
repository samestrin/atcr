# Plan Documentation References

**Created:** July 07, 2026 11:05:36AM
**Last Updated:** July 07, 2026
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, original-requirements.md, plan.md, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical

- [Community Persona Fetch & Distribution (net/http + YAML)](fetch-and-distribution.md) — repointing `RegistryBaseURL`, fetch-and-pin logic, `--offline` fallback, backward compatibility for existing `.atcr/personas/` workspaces, and the `index.json` generation workflow (AC1)
- [CLI Flag Wiring for Model-Aware Search (Cobra)](cli-search-flags.md) — adding `--model`/`--provider` flags to `atcr personas search` (AC2)

### Important

- [Persona YAML Schema & Struct Tags](persona-yaml-schema.md) — `yaml.v3` struct tags and strict decoding for the persona YAML authoring contract (AC3, AC7)
- [Testing Patterns: testify + httptest Mock Registry](testing-mock-registry.md) — mock-registry test patterns for `search_test.go` and end-to-end verification (AC6, AC7)
- [Human-Names Migration for Built-in Stragglers](human-names-migration.md) — migrating `sentinel`/`tracer`/`idiomatic` to `sasha`/`penny`/`ingrid` and enforcing the all-human-names convention (AC4, AC8)
- [Onboarding Hierarchy and Discover-by-Model Flow](onboarding-hierarchy.md) — leading with `atcr quickstart` (Synthetic), documenting the flat-rate/fronter/advanced provider ranking, and the discover-install-test-by-model flow (AC5)

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/yaml-v3.md`, `.planning/specifications/packages/standard-library.md`, `.planning/specifications/packages/cobra.md`, `.planning/specifications/packages/testify.md`
- **Plan Intent:** `../original-requirements.md`, `../plan.md`
- **Codebase Discovery:** `../codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
