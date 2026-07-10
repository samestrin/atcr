# Plan 19.9: Community Prompt Submissions (Intake & Curation)

## Metadata
- **Plan Type:** feature
- **Last Modified:** 2026-07-10

## Plan Overview
**Plan Type:** feature
**Plan Goal:** Give a user who has locally tuned a reviewer persona a one-command path — `atcr personas submit <name>` — to fork the canonical repo, run the same fixture gate that already backs `personas test`, and open a PR, turning "I improved my local copy" into "I opened a PR." Land the submission with a `submitted` status that is orthogonal to the existing `built-in`/`community` provenance field, so a maintainer can battle-test and **graduate** it into the vetted `personas/community/` library without any marketplace, website, or hosted registry.
**Target Users:** atcr users who have tuned a community persona in production (over time, against real reviews) and want to contribute the refined prompt back to `samestrin/atcr`; the project maintainer, who curates submissions into the vetted library.
**Framework/Technology:** Go 1.25, Cobra CLI (`cmd/atcr/personas.go`), the `gh` CLI (new integration point — no existing `gh` usage in the codebase) for the fork+PR flow.

## Objectives

1. Provide a one-command submission path (`atcr personas submit <name>`) that runs the existing local fixture gate and, on success, opens a fork+PR to `samestrin/atcr` via the `gh` CLI.
2. Introduce a `submitted` status axis that is orthogonal to the existing `built-in`/`community` `Source` field, preserving provenance while marking a prompt as fixture-passing but unvetted.
3. Support maintainer graduation of `submitted` personas into the vetted `personas/community/` library through the existing human-review PR process.
4. Keep the entire flow GitHub-PR-native and avoid introducing any marketplace, website, hosted registry, or ranking surface.
5. Maintain test coverage (`go test ./...`) and update persona documentation to describe the new subcommand and the two-tier curation model.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`

## Feature Analysis Summary

The intake pipeline already exists in shape — a PR to `samestrin/atcr`, gated by the 19.6 fixture gate plus human review — but today it requires a contributor to manually fork, branch, run `personas test` themselves, and open a PR by hand. This plan automates that path behind a single subcommand that reuses existing, already-tested infrastructure (`commpersonas.TestPersona` / `TemplateFixtureRunner`, `internal/personas/test.go`) rather than introducing a second gate. The harder half of the problem is curation, not intake: a submission that passes the fixture gate is *fixture-passing*, not *vetted* — it has not been battle-tested against real diffs the way the existing built-in and community-library personas have. This plan introduces a `submitted` status axis, deliberately kept separate from the existing `Source` field (`built-in`|`community`, `internal/personas/list.go:22`) to avoid overloading provenance with vetting state — a submission's `Source` stays `community` throughout; only its status changes when a maintainer graduates it. No marketplace, ranking UI, or hosted registry is introduced anywhere in this plan; the entire flow rides GitHub's own PR mechanism, per the 19.6 out-of-scope constraint this epic must honor.

## Technical Planning Notes

- `personas submit <name>` slots into the existing `cmd/atcr/personas.go` cobra command tree (`install`/`list`/`search`/`remove`/`test`/`upgrade`) as a seventh subcommand, following the same `newPersonas<Verb>Cmd()` + injectable-seam pattern already used for `personasDir`/`personasClient`/`personasFixtureRunner`.
- The local fixture gate is not new work — `submit` calls `commpersonas.TestPersona(name, personasFixtureRunner)`, the exact function `personas test` already calls, and blocks submission on any non-passing outcome (AC1).
- The fork+PR mechanism is new: no code in this repo currently shells out to `gh` (verified via full-repo grep). The existing `internal/ghaction.Client` (Epic 17.0's --auto-fix bot) is a poor fit — it authenticates with one fixed bot Token against one configured repo, not an arbitrary user's own fork — so `submit` needs its own `gh`-CLI-based integration, ideally wrapped in an injectable interface so tests can stub it out.
- `submitted` status must be implemented as a field/concept distinct from `Source` (which stays a closed `built-in`|`community` value read/written in several places: `List`, `ListTiers`, `listCommunity`, `listProject`) — per the epic's own terminology-collision resolution, adding it as a third `Source` value would break that field's existing meaning.
- Docs (`docs/personas-install.md`'s "six subcommands" section, `docs/personas-authoring.md`'s contribution checklist) need updating to document the seventh subcommand and the two-tier `submitted` → graduated curation model (AC4).

## Documentation References

See [documentation/README.md](documentation/README.md) for the full grounded documentation index generated from `codebase-discovery.json` and `.planning/specifications/`:

- **[CRITICAL]** [GitHub Fork + PR Integration via go-gh](documentation/gh-fork-pr-integration.md)
- **[CRITICAL]** [Cobra Subcommand & Injectable-Seam Conventions](documentation/cobra-subcommand-patterns.md)
- **[CRITICAL]** [Local Fixture-Gate Reuse (TestPersona)](documentation/fixture-gate-reuse.md)
- **[IMPORTANT]** [Status/Provenance Separation and Atomic Persistence](documentation/status-provenance-and-atomic-writes.md)
- **[IMPORTANT]** [Personas Install & Authoring Doc Updates (AC4)](documentation/personas-docs-updates.md)

## Implementation Strategy

Implement `submit` as a thin cobra subcommand that (1) resolves the local persona, (2) calls the existing `TestPersona` fixture gate and aborts with a clear error on any failure, (3) shells out to `gh` (fork, branch, commit, `gh pr create`) behind an injectable seam so the CLI layer stays testable without live GitHub calls, and (4) stamps the submission with a `submitted` status plus attribution metadata that a maintainer later flips during graduation. Graduation itself is a maintainer-side action (promoting a `submitted` persona into `personas/community/`), not a new automated code path — it reuses the existing human-review PR-merge process, keeping this plan's actual code surface small (one new subcommand, one status/attribution concept) while the curation *process* around it is documented rather than built.

## Recommended Packages
github.com/cli/go-gh/v2

## User Story Themes

### Theme 1 — Local Fixture-Gate Reuse and Submission Blocking
A user runs `atcr personas submit <name>` on a persona whose fixture fails or is absent; the command reuses `commpersonas.TestPersona`/`TemplateFixtureRunner` and blocks submission with a clear, actionable error — no fork or PR is attempted (AC1).

### Theme 2 — Fork + PR Automation via `gh`
A user runs `atcr personas submit <name>` on a fixture-passing persona; the command forks `samestrin/atcr` (if not already forked), pushes a branch with the persona's files, and opens a PR via `gh`, reporting the PR URL back to the user (AC1).

### Theme 3 — `submitted` Status Distinct from `Source`/Provenance
A submitted persona's `Source` remains `community` while a new, orthogonal `submitted` status marks it as fixture-passing-but-unvetted; the status is carried alongside attribution/provenance metadata and does not collide with the existing `built-in`|`community` field or its consumers (AC2).

### Theme 4 — Maintainer Graduation into the Vetted Library
A maintainer reviewing a `submitted` PR can promote it into the vetted `personas/community/` library through the existing human-review PR-merge process; graduation flips the status, not the provenance (AC2).

### Theme 5 — Documentation of the Submit Flow and Two-Tier Model
`docs/personas-install.md` documents the seventh `submit` subcommand in the existing per-command format; `docs/personas-authoring.md` cross-references the automated local gate against its manual contribution checklist and explains the `submitted` → graduated curation model (AC4).

## Planning Success Criteria

- `atcr personas submit <name>` runs the fixture gate locally (reusing existing `TestPersona` infrastructure) and blocks on any failure with a clear error (AC1).
- A passing submission opens a fork+PR to `samestrin/atcr` via `gh`, with no new hosting or marketplace surface introduced (AC1, AC3).
- Submitted personas carry a `submitted` status that is distinct from and does not overload the existing `Source` (`built-in`|`community`) field, plus attribution/provenance metadata (AC2).
- Maintainer graduation promotes a submitted persona into the vetted `personas/community/` library, flipping status only (AC2).
- `go test ./...` passes; `docs/personas-install.md` and `docs/personas-authoring.md` document the submit flow and the two-tier curation model (AC4).

## Risk Mitigation

- **Risk:** `gh` CLI absent or unauthenticated on the user's machine causes a confusing failure mid-submission. **Mitigation:** a precondition check (mirroring `skill/SKILL.md`'s existing "atcr binary must be on PATH" pattern) that halts with a clear, actionable message before any fork/branch/commit work begins.
- **Risk:** the new `submitted` status gets implemented as a third `Source` value, breaking the several existing call sites (`List`, `ListTiers`, `listCommunity`, `listProject`) that treat `Source` as a closed `built-in`|`community` field. **Mitigation:** codebase-discovery.json's architecture_notes flag this explicitly; implementation must add a separate field/concept, not extend `Source`'s value set.
- **Risk:** `submit`'s `gh` integration is hard-wired (bare `exec.Command` calls inline in the cobra `RunE`), making it untestable without live network/GitHub access in CI. **Mitigation:** follow the codebase's existing injectable-seam pattern (`personasDir`, `personasClient`, `personasFixtureRunner`) so tests stub the `gh` wrapper.

## Next Steps
1. `/find-documentation @.planning/plans/active/19.9_community_prompt_submissions/`
2. `/create-documentation @.planning/plans/active/19.9_community_prompt_submissions/`
3. `/create-user-stories @.planning/plans/active/19.9_community_prompt_submissions/`
4. `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`
5. `/design-sprint @.planning/plans/active/19.9_community_prompt_submissions/`
6. `/create-sprint @.planning/plans/active/19.9_community_prompt_submissions/`
