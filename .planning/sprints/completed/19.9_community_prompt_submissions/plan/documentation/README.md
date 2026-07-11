# Plan Documentation References

**Created:** July 10, 2026 10:46:41AM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- **[CRITICAL]** [GitHub Fork + PR Integration via go-gh](gh-fork-pr-integration.md) — how `personas submit` shells out to `gh` (via `github.com/cli/go-gh/v2`) to fork, push, and open a PR under the invoking user's own GitHub identity.
- **[CRITICAL]** [Cobra Subcommand & Injectable-Seam Conventions](cobra-subcommand-patterns.md) — the existing `personas` cobra command-tree pattern and the injectable-seam testability convention `submit` must follow.
- **[CRITICAL]** [Local Fixture-Gate Reuse (TestPersona)](fixture-gate-reuse.md) — the existing `TestPersona`/`TemplateFixtureRunner` fixture gate and persona name/path validation `submit` must reuse before any GitHub call.
- **[IMPORTANT]** [Status/Provenance Separation and Atomic Persistence](status-provenance-and-atomic-writes.md) — why the new `submitted` status must stay orthogonal to the existing `Source` field, and which atomic-write helpers to reuse for any status marker.
- **[IMPORTANT]** [Personas Install & Authoring Doc Updates (AC4)](personas-docs-updates.md) — the required user-facing documentation changes in `docs/personas-install.md` and `docs/personas-authoring.md` for the new `submit` subcommand and the `submitted` → graduated curation model.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/go-gh.md`, `.planning/specifications/packages/cobra.md`, `.planning/specifications/git-strategy.md`, `skill/SKILL.md`
- **Codebase Discovery:** `.planning/plans/active/19.9_community_prompt_submissions/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
