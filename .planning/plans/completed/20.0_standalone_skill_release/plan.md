## Metadata
**Last Modified:** 2026-07-11
**Plan Type:** feature

## Plan Overview
**Plan Type:** feature
**Plan Goal:** Release atcr's standalone skill (`skill/SKILL.md`) publicly as a single dispatcher skill under a unified `/atcr <command>` UX, document the `go install` path, prove the private-skill `--output-dir`/reconcile backward-compat contract stays stable, and ship the one missing distribution artifact (`install.sh`) — all without touching the external `claude-prompts` repo or taking on binary packaging/release automation.
**Target Users:** External OSS developers adopting atcr on any repository; the internal maintainer relying on continued private `.planning/` skill compatibility.
**Framework/Technology:** Go 1.24+ CLI (cobra), Markdown-based Agent Skill format (Claude Code skills), shell (`install.sh`)

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/20.0_standalone_skill_release/`

## Feature Analysis Summary
This plan converts the existing standalone `skill/SKILL.md` from a linear orchestration script into a single dispatcher skill responding to a `/atcr <command> <flags>` pattern, per the 2026-07-05 addendum override that unifies public and (eventually) private skill UX under one namespace. Two prior `/refine-epic` passes already resolved scope ambiguity: packaging/release automation is fully descoped to Epic 21.0, the "self-test" requirement is satisfied by the existing `atcr doctor` command (no new command), and the quick-start requirement is satisfied by `atcr quickstart` + `docs/skill-usage.md`. The two genuinely net-new deliverables are (1) `install.sh`, confirmed absent anywhere in the repo, and (2) a repo-local end-to-end test asserting the `docs/code-review-backend.md` `--output-dir` + reconcile contract remains stable, protecting the external `claude-prompts` private skills that depend on it without re-touching that external repo (already validated end-to-end by Epic 12.0).

## Technical Planning Notes
- `skill/SKILL.md`'s rewrite must stay within a ~500-line budget per the addendum; heavy instructions (host review, ambiguity adjudication, findings format) should move to secondary markdown files loaded on demand by the dispatcher.
- The private `claude-prompts` skills migration (Proposed Solution #3) is descoped to a manual operator action — this workspace cannot write to that external repository (confirmed by `/refine-epic`'s 2026-07-05 audit).
- `install.sh` has no prior art in this repo beyond `examples/ci-gate.sh`'s shell style; it should wrap the existing `go install github.com/samestrin/atcr/cmd/atcr@latest` path documented in README.md, not reinvent installation.
- The new backward-compat contract test should follow the existing `cmd/atcr/*_test.go` pairing convention (see `review_test.go`, `reconcile_test.go`) rather than a new test harness.
- AC1, most of AC2, and most of AC4's self-test/quick-start requirement are already satisfied by existing code/docs (`go install`, `atcr doctor`, `atcr quickstart`, `docs/skill-usage.md`) — this plan's real work is narrower than the epic's original framing.

## Documentation References
- **[CRITICAL]** [CLI Dispatcher Conventions](documentation/cli-dispatcher-conventions.md) — cobra command/subcommand conventions the dispatcher must mirror
- **[CRITICAL]** [Agent Skill Format & Progressive Disclosure](documentation/agent-skill-format.md) — SKILL.md frontmatter and secondary-file loading model governing the ~500-line rewrite
- **[IMPORTANT]** [Backward-Compatibility Contract Test Patterns](documentation/backward-compat-test-patterns.md) — Go stdlib/testify conventions and the reconcile id-or-path resolution contract for the AC3 test
- **[IMPORTANT]** [Install Script Conventions](documentation/install-script-conventions.md) — requirements and style for the net-new `install.sh` distribution artifact (AC4)
- **[REFERENCE]** [External Private-Skill Migration Descope](documentation/external-migration-descope.md) — why the `claude-prompts` skill migration is a manual operator follow-up, not an in-repo deliverable
- Full index: [documentation/README.md](documentation/README.md)

## Implementation Strategy
Decompose into 5 user stories: (1) `skill/SKILL.md` dispatcher rewrite, (2) repo-local `--output-dir`/reconcile backward-compat contract test, (3) `install.sh` authoring + README/docs linkage, (4) documentation verification pass (`docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md` citations stay accurate against the dispatcher rewrite), (5) explicit descope note/documentation for the external `claude-prompts` dispatcher migration as a manual follow-up action. Sequence: the contract test and doc verification can run independently of the dispatcher rewrite; the dispatcher rewrite is the highest-risk item since it changes the skill's user-facing surface.

## Recommended Packages
No high-ROI packages identified — this work is documentation, a shell script (`install.sh`), and a Go test reusing existing `testing`/testify patterns already in `go.mod`.

## User Story Themes
1. **Dispatcher Skill Rewrite** — convert `skill/SKILL.md` into a `/atcr <command>` router
2. **Backend Contract Backward-Compatibility Test** — repo-local end-to-end test for `--output-dir` + reconcile
3. **Install Script** — `install.sh` for external developers wrapping `go install`
4. **Documentation Accuracy Pass** — verify `docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md` stay accurate post-dispatcher-rewrite
5. **External Migration Descope Note** — document the `claude-prompts` dispatcher migration as a manual operator follow-up (no code changes in this repo)

## Planning Success Criteria
- The `atcr` binary installs and runs via `go install` on a clean repository with no `.planning` folder (AC1).
- `skill/SKILL.md` operates as a single dispatcher skill, storing all artifacts under `.atcr/reviews/<id>/` (AC2).
- A repo-local automated test proves the `--output-dir` + reconcile contract in `docs/code-review-backend.md` remains stable (AC3).
- `install.sh` is added and documented; `atcr doctor` + `atcr quickstart` + `docs/skill-usage.md` are cited as satisfying the self-test/quick-start requirement (AC4).
- No changes are made to the external `claude-prompts` repo; that migration is documented as a manual follow-up action.

## Risk Mitigation
- **Risk:** Dispatcher rewrite of `skill/SKILL.md` could regress existing orchestration behavior relied on by current adopters. **Mitigation:** preserve all existing orchestration steps and host-review/adjudication instructions verbatim in secondary files; the dispatcher only changes the entry surface, not the underlying flow.
- **Risk:** Scope creep back into binary packaging/release automation (explicitly out of scope per two prior epic decisions). **Mitigation:** `install.sh` wraps the existing `go install` path only; no goreleaser/tagging/release workflow is introduced.
- **Risk:** Ambiguity about "validating both public and private skills" reintroducing cross-repo work. **Mitigation:** AC3 is scoped to a repo-local test against the documented contract, per Decision D2; no external repo is touched.

## Next Steps
1. `/find-documentation @.planning/plans/active/20.0_standalone_skill_release/`
2. `/create-documentation @.planning/plans/active/20.0_standalone_skill_release/`
3. `/create-user-stories @.planning/plans/active/20.0_standalone_skill_release/`
4. `/create-acceptance-criteria @.planning/plans/active/20.0_standalone_skill_release/`
5. `/design-sprint @.planning/plans/active/20.0_standalone_skill_release/`
6. `/create-sprint @.planning/plans/active/20.0_standalone_skill_release/`
