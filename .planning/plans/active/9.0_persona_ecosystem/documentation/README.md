# Plan Documentation References

**Created:** June 24, 2026
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### [CRITICAL] Must Read Before Coding

- [Bonus Built-In Personas](bonus-personas.md) — `sentinel`, `tracer`, `idiomatic` personas, embedded registration, and fixture expectations (T1)
- [Cobra CLI Patterns](cobra-cli-patterns.md) — Cobra subcommand architecture for `atcr personas` CLI and 6 sub-subcommands (T2)
- [YAML Bundle Manifests](yaml-bundle-manifests.md) — YAML v3 bundle manifest parsing and `AgentConfig.Language` field (T5/T8)
- [Skeptic Routing & Verification](skeptic-routing-verification.md) — `SelectEligibleSkeptics` extension for language-aware routing (T8)

### [IMPORTANT] Review During Development

- [HTTP & Standard Library Testing](http-stdlib-testing.md) — `net/http` + `httptest.NewServer` patterns for community repo fetch testing (T2)
- [Per-Persona Corroboration Scores](scorecard-corroboration.md) — scorecard aggregation and `atcr personas list --scores` wiring (T6)

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/cobra.md`, `.planning/specifications/packages/yaml-v3.md`, `.planning/specifications/packages/standard-library.md`, `.planning/specifications/design-concepts/adversarial-verification-interface.md`
- **Codebase Discovery:** `.planning/plans/active/9.0_persona_ecosystem/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
