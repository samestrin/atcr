# Plan Documentation References

**Created:** July 11, 2026 08:59:38PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- **[CRITICAL]** [Agent Skills Format & Progressive Disclosure](agent-skills-format.md) — SKILL.md YAML frontmatter requirements, Level 1/2/3 progressive disclosure, directory structure conventions for the new `skill/debt-resolve/SKILL.md` and `skill/CONVENTIONS.md`.
- **[CRITICAL]** [Append-Only JSONL Store Pattern](append-only-store-pattern.md) — the `internal/scorecard`/`internal/history` atomic-append JSONL precedents the new `.atcr/`-scoped local TD store must copy.
- **[CRITICAL]** [Local TD Store Schema](local-td-store-schema.md) — concrete v1 record schema, file layout, identity/deduplication rules, and CLI contract for the new `.atcr/debt/` JSONL store (AC1).
- **[IMPORTANT]** [CLI Integration Points](cli-integration-points.md) — the `atcr reconcile` persistence hook, `atcr debt` subcommand family, already-live `justification`/`SourceReport` fields, and the symbol-anchor contract this epic wires into.
- **[IMPORTANT]** [Skill Dispatcher & CONVENTIONS.md Extraction](skill-dispatcher-conventions.md) — extending `skill/SKILL.md`'s dispatcher, extracting shared Prerequisites boilerplate, and the doc files (`docs/skill-usage.md`, `docs/scorecard.md`) to mirror.
- **[REFERENCE]** [Source Index](source.md) — generated source-attribution index for this documentation set.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview
  - https://support.claude.com/en/articles/12512176-what-are-skills
- **Codebase Discovery:** .planning/plans/active/20.1_public_td_resolve_skill/codebase-discovery.json
- **Specifications:** .planning/specifications/ (no non-excluded specs scored >= 5 relevance for this plan — see `source.md`)

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
