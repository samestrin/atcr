# Personas Install & Authoring Doc Updates (AC4)

`[IMPORTANT]`

## Overview

AC4 requires the user-facing persona documentation to describe the new `atcr personas submit <name>` subcommand and the two-tier `submitted` → graduated curation model. This document maps those requirements to the exact files and sections that must change during implementation.

> Source: [plan.md](../plan.md) — Objectives 4–5, Planning Success Criteria, Theme 5
> Source: [codebase-discovery.json](../codebase-discovery.json) — `files_to_modify` (`docs/personas-install.md`, `docs/personas-authoring.md`)
> Source: [docs/personas-install.md](../../../../docs/personas-install.md)
> Source: [docs/personas-authoring.md](../../../../docs/personas-authoring.md)

---

## 1. `docs/personas-install.md` — add the seventh subcommand

### What to change

- Update the heading `## The six subcommands` to `## The seven subcommands`.
- Add a new `### atcr personas submit <name>` section after the existing `test` section (or in logical order before `upgrade`), following the same format as the other subcommand sections:
  - One-line description.
  - Example invocation and output.
  - Error cases.

### Content to capture

```markdown
### `atcr personas submit <name>`

Submits a locally-tuned persona back to the canonical repository as a pull request. The command first runs the same fixture gate used by `atcr personas test`; if the fixture fails or is absent, submission stops before any GitHub call. On a passing fixture, it forks `samestrin/atcr` under your GitHub identity, pushes a branch with the persona files, and opens a PR.

```bash
atcr personas submit penny
# PASS: penny (1/1 cases)
# Opened pull request https://github.com/samestrin/atcr/pull/123
```

**Errors:**
- Invalid persona name → same validation error as `install`/`remove`.
- Failing or missing fixture → `fixture gate failed for "<name>": …`; no fork is attempted.
- `gh` CLI missing or unauthenticated → actionable error before any fork/branch work.
```

### Cross-references to add

- In the quick walkthrough at the bottom of the page, add a step 5 for `submit` after `test` and before `upgrade`/`remove`, or add a separate "contribute a tuned persona" mini-flow.
- Update any sentence that counts subcommands (e.g., the intro line "This guide covers every `atcr personas` subcommand" is fine as-is; just ensure the body now lists seven).

---

## 2. `docs/personas-authoring.md` — document the automated gate and curation model

### What to change

- In [section 4. Contribution checklist](../../../../docs/personas-authoring.md#4-contribution-checklist), add a cross-reference explaining that `atcr personas submit` automates the "Fixture test passes" item and blocks submission if it fails.
- Add a new section (after the community index entry section, or as a sub-section of the checklist) describing the `submitted` → graduated two-tier model.

### Content to capture

Add to the contribution checklist prose:

```markdown
> **Automated gate.** Running `atcr personas submit <name>` enforces the fixture-pass item above automatically: the command calls the same `TestPersona` / `TemplateFixtureRunner` path as `atcr personas test` and refuses to open a PR unless the fixture passes.
```

New section:

```markdown
## 6. Submission and curation

A persona that passes its fixture can be submitted with `atcr personas submit <name>`. The command opens a GitHub PR from your fork; the persona lands with a `submitted` status, meaning it is **fixture-passing but unvetted**.

- **Provenance stays `community`.** The existing `Source` field (`built-in` | `community`) is not overloaded with vetting state.
- **`submitted` is a status, not a source.** It marks the prompt as ready for maintainer review but not yet battle-tested against real diffs.
- **Graduation is a maintainer action.** A maintainer promotes a `submitted` persona into the vetted `personas/community/` library through the normal PR-merge process. Graduation flips the status; it does not change the provenance.
```

> **Note on section numbering.** The current doc ends with section 6 ("Model family/channel bindings and resolved locks"). If the new curation section is inserted before it, renumber accordingly; if appended, it becomes section 7. Either ordering is acceptable as long as the table of contents/anchors remain consistent.

---

## 3. Consistency checks

When applying these updates, verify:

- The persona directory path in `docs/personas-install.md` still matches `commpersonas.PersonasDir()` (currently `~/.config/atcr/personas/` on both Linux and macOS; the resolver deliberately uses this path rather than `os.UserConfigDir()`).
- The output examples use stdout/stderr conventions consistent with the other subcommand sections.
- The term "community-contributed" is used for provenance, and "submitted" is used for the unvetted status tier (per the terminology-collision resolution in [original-requirements.md](../original-requirements.md)).

---

## Related Documentation

- [GitHub Fork + PR Integration via go-gh](gh-fork-pr-integration.md)
- [Local Fixture-Gate Reuse (TestPersona)](fixture-gate-reuse.md)
- [Status/Provenance Separation and Atomic Persistence](status-provenance-and-atomic-writes.md)
- [Cobra Subcommand & Injectable-Seam Conventions](cobra-subcommand-patterns.md)
- `docs/personas-install.md`
- `docs/personas-authoring.md`
