# Plan Documentation References

**Created:** July 16, 2026 08:42:57PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical
- [persona-yaml-and-prompt-authoring.md](persona-yaml-and-prompt-authoring.md) — How to structure `simon.yaml` (agent binding, yaml.v3 strict-schema decode) and `simon.md` (text/template prompt rendering) per the community persona 3-file unit pattern.
- [test-gate-and-fixture-verification.md](test-gate-and-fixture-verification.md) — How the `personas/community_test.go` roster and embedded-set gates use testify `assert`/`require` to verify simon's fixture, differentiation, category uniqueness, and index registration.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/yaml-v3.md`, `.planning/specifications/packages/standard-library.md`, `.planning/specifications/packages/testify.md`
- **Codebase Discovery:** `.planning/plans/active/29.0_anti_slop_persona/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

Note: `.planning/specifications/packages/go-gitdiff.md` was scored as a candidate source during `/find-documentation` but dropped during categorization — codebase-discovery.json shows the community-persona fixture harness (`TemplateFixtureRunner`) renders `simon_fixture.patch` as raw template payload text rather than parsing it structurally via go-gitdiff, so it was not sufficiently grounded to include here.

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
