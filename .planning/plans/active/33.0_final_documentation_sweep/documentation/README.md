# Plan Documentation References

**Created:** July 22, 2026 02:21:41PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- **[CRITICAL]** [multi-agent-review-workflow.md](multi-agent-review-workflow.md) — atcr's own dispatcher (review/reconcile/verify/report orchestration) for dogfooding the multi-agent reviewer against its own codebase.
- **[CRITICAL]** [technical-debt-triage-resolution.md](technical-debt-triage-resolution.md) — the RED→GREEN→ADVERSARIAL→REFACTOR severity-triage cycle for fixing CRITICAL/HIGH findings and routing MEDIUM/LOW findings to `.planning/technical-debt/README.md`.
- **[IMPORTANT]** [persona-naming-doc-accuracy.md](persona-naming-doc-accuracy.md) — the retired-slug guard test, real vs. legitimate "sentinel"/"idiomatic" usages, and the docs/ website-export pattern for the persona-cleanup and code-to-docs audit tasks.
- **[REFERENCE]** [source.md](source.md) — index of source files and discovery artifacts used to generate this plan's documentation set.


---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `skill/SKILL.md`, `skill/debt-resolve/SKILL.md`
- **Codebase Discovery:** /Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/33.0_final_documentation_sweep/codebase-discovery.json
- **Specifications:** .planning/specifications/

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
