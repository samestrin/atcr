# Skill Dispatcher & CONVENTIONS.md Extraction

**Priority: Important**

## Overview

Epic 20.1 extends the single dispatcher skill introduced in Epic 20.0 (`skill/SKILL.md`), which already routes `/atcr <command>` to a live `atcr` CLI invocation and already lists `atcr debt` in its command table at line 79. The new `/atcr debt resolve` route is not a new top-level dispatcher row — it is a subcommand of the existing `atcr debt` family, consistent with how SKILL.md already documents that top-level commands with subcommands (`debt`, `personas`, `models`, `benchmark`) expose their subcommands via `atcr <command> --help` rather than enumerating every verb inline. The dispatcher should either extend the `debt` row's description or add a dedicated subsection to cover `atcr debt resolve`, without inventing subcommand names beyond what is described in the discovery snapshot.

Alongside the dispatcher extension, this epic must extract the Prerequisites boilerplate currently embedded in `skill/SKILL.md` — the binary-on-PATH check, the git-worktree check, and (per the architecture notes) `.atcr/` path-safety rules — into a new shared file, `skill/CONVENTIONS.md`. This lets `skill/SKILL.md` and the new on-demand `skill/debt-resolve/SKILL.md` both reference the same conventions instead of duplicating text, mirroring the existing pattern where `skill/host-review.md` is a sibling on-demand file loaded by SKILL.md rather than inlined into it. The new `skill/debt-resolve/SKILL.md` implements `/atcr debt resolve`: it reads the `.atcr/`-scoped local TD store, selects items, and drives a resolution loop adapted to a repo-agnostic, `.planning/`-free context, referencing `skill/CONVENTIONS.md` for its prerequisites rather than restating them.

Both new files must be wired into the Go embed harness in `skill/skill.go`, which currently exposes SKILL.md and secondary skill files (`host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`) as build-time constants. Test coverage in `skill/skill_test.go` needs corresponding additions: the `dispatcherCommands` list at line 133 mirrors `newRootCmd` and already contains `debt`, so `debt resolve` as a subcommand of an existing entry likely does not require a new list entry — but new assertions are needed to confirm the subcommand is documented in SKILL.md and that CONVENTIONS.md is referenced (not duplicated) from both skill files. Public-facing documentation in `docs/skill-usage.md` must also be extended per AC4 to describe the new capability, following the structure/style precedent set by `docs/scorecard.md` for documenting a store format, CLI usage, and conventions.

## Key Concepts

**`/atcr debt resolve` is a subcommand extension, not a new dispatcher row.** `skill/SKILL.md` already lists `atcr debt` in its command table at line 79 and documents that commands with subcommands are discovered via `atcr <command> --help`. The new route should extend the `debt` row's description or add a subsection, without inventing subcommand names beyond what the discovery snapshot describes.
> Source: [codebase-discovery.json: related_files "skill/SKILL.md"]
> Source: [codebase-discovery.json: integration_points "skill/SKILL.md:debt"]

**Naming must avoid collision with the private, `.planning/`-scoped `atcr debt` family.** `atcr debt` (private, `.planning/`-scoped) already exists as a live, tested CLI surface with `list`/`add`/`dashboard` subcommands. This epic is not inventing a debt CLI from scratch, only a `.atcr/`-scoped, `.planning/`-free sibling/extension of it. `atcr debt resolve` is the natural public subcommand name, but the UX and naming must not be confused with the existing `.planning/`-scoped `atcr debt list/add/dashboard`.
> Source: [codebase-discovery.json: architecture_notes "atcr debt (private, .planning/-scoped) already exists..."]

**New on-demand skill file for the resolve route.** `skill/debt-resolve/SKILL.md` is a new secondary skill file, loaded on demand the same way `skill/host-review.md` is a sibling on-demand file loaded by SKILL.md rather than inlined into it. It reads the `.atcr/`-scoped local TD store, selects items (likely by severity/age or explicit user filter), and drives a RED→GREEN→ADVERSARIAL→REFACTOR resolution loop adapted to a repo-agnostic, `.planning/`-free context. It must reference `skill/CONVENTIONS.md` for binary-on-PATH and git-worktree checks rather than duplicating Prerequisites text. When applying fixes it should consume the stable `(symbolName)` anchor in `problem` (`docs/technical-debt-format.md`) to survive line-number drift across multiple resolution passes.
> Source: [codebase-discovery.json: related_files "skill/host-review.md"]
> Source: [codebase-discovery.json: integration_points "skill/SKILL.md:debt resolve"]
> Source: [codebase-discovery.json: files_to_create "skill/debt-resolve/SKILL.md"]

**CONVENTIONS.md extracts shared Prerequisites boilerplate.** The `skill/CONVENTIONS.md` extraction is a deliverable of this epic (an addendum to Epic 20.0). It should hold shared boilerplate currently embedded in `skill/SKILL.md`'s Prerequisites section — the binary-on-PATH check, the git-worktree check, and `.atcr/` path-safety rules — so that `skill/SKILL.md` and `skill/debt-resolve/SKILL.md` can both reference it instead of duplicating text.
> Source: [codebase-discovery.json: architecture_notes "The skill/CONVENTIONS.md extraction (Epic 20.0 addendum)..."]
> Source: [codebase-discovery.json: files_to_create "skill/CONVENTIONS.md"]

**Both new files must be embedded in the Go harness.** `skill/skill.go` is the Go embed harness exposing `SKILL.md` and secondary skill files as build-time constants. It currently embeds SKILL.md, `host-review.md`, `ambiguity-adjudication.md`, and `findings-format.md`. Any new secondary skill file (e.g. `skill/debt-resolve/SKILL.md`) must be embedded here, and any new on-demand pointer in SKILL.md must resolve to a sibling file. `skill/CONVENTIONS.md` must be added to the same embed set.
> Source: [codebase-discovery.json: related_files "skill/skill.go"]
> Source: [codebase-discovery.json: integration_gaps "CONVENTIONS.md embedding and test coverage"]

**Test coverage: `dispatcherCommands` likely unchanged, but new documentation assertions are needed.** `skill/skill_test.go` validates `skill/SKILL.md`'s structure, including the `dispatcherCommands` list at line 133 that mirrors `newRootCmd`. `debt` is already in the list; a new top-level command would require an addition, but `debt resolve` as a subcommand of the existing `debt` command likely does NOT change this list. New tests may be needed to assert the debt-resolve subcommand is documented in SKILL.md, and that CONVENTIONS.md is non-empty and referenced from both skill files rather than duplicated.
> Source: [codebase-discovery.json: related_files "skill/skill_test.go"]
> Source: [codebase-discovery.json: integration_gaps "CONVENTIONS.md embedding and test coverage"]

**Public documentation must describe the new capability, following the scorecard doc's structure.** `docs/skill-usage.md` is the existing public skill installation/usage guide that AC4 requires extending with the new `/atcr debt resolve` capability. `docs/scorecard.md` is the closest existing doc template for what this epic's AC1 (local TD store format documented) and AC4 (docs/skill-usage.md update) should mirror in style and structure — its coverage of format, CLI usage, privacy model, and schema-versioning conventions is the reference shape. `docs/technical-debt.md` documents the existing `.planning/`-scoped `atcr debt` command family and is a useful reference for explaining the distinction between private-pipeline debt and the new `.atcr/`-scoped local store, though it is not itself a required edit target for this category.
> Source: [codebase-discovery.json: related_files "docs/skill-usage.md"]
> Source: [codebase-discovery.json: related_files "docs/scorecard.md"]
> Source: [codebase-discovery.json: related_files "docs/technical-debt.md"]
> Source: [codebase-discovery.json: files_to_modify "docs/skill-usage.md"]

## Quick Reference

| File | Action | Reason |
|---|---|---|
| `skill/SKILL.md` | modify (extend) | Document the `/atcr debt resolve` subcommand — extend the `atcr debt` row description or add a subsection — and extract shared Prerequisites boilerplate out to `skill/CONVENTIONS.md` per Epic 20.0's addendum. |
| `skill/CONVENTIONS.md` | create | Shared boilerplate extracted from SKILL.md's Prerequisites section (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules), referenced by both `skill/SKILL.md` and `skill/debt-resolve/SKILL.md`. |
| `skill/debt-resolve/SKILL.md` | create | New public skill implementing `/atcr debt resolve` — adapts the RED→GREEN→ADVERSARIAL→REFACTOR cycle to a repo-agnostic, `.planning/`-free context; references `skill/CONVENTIONS.md` for shared prerequisites. |
| `skill/skill.go` | modify (trivial) | Embed the new `skill/debt-resolve/SKILL.md` and `skill/CONVENTIONS.md` files as build-time constants alongside the existing embedded secondary skill files. |
| `skill/skill_test.go` | modify (trivial) | Add assertions that `/atcr debt resolve` is documented in SKILL.md and that CONVENTIONS.md is referenced (non-empty, not duplicated); `dispatcherCommands` likely needs no new entry since `debt` is already present. |
| `docs/skill-usage.md` | modify (minor) | Document the new `/atcr debt resolve` capability per AC4. |

## Related Documentation

- `skill/SKILL.md` — the single dispatcher skill; already lists `atcr debt` at line 79 and documents the Prerequisites section this epic extracts
- `skill/skill.go` — Go embed harness exposing SKILL.md and secondary skill files as build-time constants
- `skill/skill_test.go` — Go test harness validating SKILL.md's structure, including the `dispatcherCommands` list at line 133
- `docs/skill-usage.md` — existing public skill installation/usage guide requiring the AC4 update
- `docs/scorecard.md` — closest existing doc template for format/CLI-usage/conventions documentation style
