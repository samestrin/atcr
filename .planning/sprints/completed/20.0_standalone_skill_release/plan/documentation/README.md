# Plan Documentation References

**Created:** July 11, 2026 01:20:47PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- **[CRITICAL]** [CLI Dispatcher Conventions](cli-dispatcher-conventions.md) — atcr's existing cobra command/subcommand structure the `/atcr <command>` dispatcher must mirror
- **[CRITICAL]** [Agent Skill Format & Progressive Disclosure](agent-skill-format.md) — SKILL.md frontmatter, the three-level loading model, and secondary-file conventions governing the ~500-line dispatcher rewrite
- **[IMPORTANT]** [Backward-Compatibility Contract Test Patterns](backward-compat-test-patterns.md) — Go stdlib/testify test conventions and the reconcile id-or-path resolution contract the new AC3 test must follow
- **[IMPORTANT]** [Install Script Conventions](install-script-conventions.md) — requirements and style for the net-new `install.sh` distribution artifact (AC4)
- **[REFERENCE]** [External Private-Skill Migration Descope](external-migration-descope.md) — why the `claude-prompts` skill migration is a manual operator follow-up, not an in-repo deliverable
- **[REFERENCE]** [Source Index](source.md) — pointers to the global specifications and source documents used to generate this documentation set

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/cobra.md`
  - `.planning/specifications/packages/standard-library.md`
  - `.planning/specifications/packages/testify.md`
  - `.planning/specifications/design-concepts/adversarial-verification-interface.md`
  - https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview
  - `.planning/plans/active/20.0_standalone_skill_release/original-requirements.md`
  - `skill/SKILL.md`
  - `examples/ci-gate.sh`
- **Codebase Discovery:** `.planning/plans/active/20.0_standalone_skill_release/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
