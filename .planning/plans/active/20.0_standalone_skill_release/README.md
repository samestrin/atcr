## Overview
Release atcr's standalone skill (`skill/SKILL.md`) publicly as a single dispatcher skill under a unified `/atcr <command>` UX, prove the private-skill `--output-dir`/reconcile backward-compat contract stays stable, and ship `install.sh` — the one confirmed missing distribution artifact. Binary packaging/release automation is out of scope (Epic 21.0); migrating the external `claude-prompts` skills is a manual follow-up action outside this workspace's write access.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/20.0_standalone_skill_release/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/20.0_standalone_skill_release/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/20.0_standalone_skill_release/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/20.0_standalone_skill_release/`

## Timeline & Milestones
Estimated 1 week (per original epic request). Five user stories: dispatcher skill rewrite, backend contract backward-compat test, install.sh, documentation accuracy pass, and an external-migration descope note.

## Resource Requirements
1 backend developer (Sam); existing Go 1.24+ toolchain; no new external dependencies.

## Expected Outcomes
- `atcr` installable and runnable via `go install` on a clean repo with no `.planning` folder.
- `skill/SKILL.md` rewritten as a single dispatcher skill (`/atcr <command>`).
- A repo-local automated test locking in the `--output-dir` + reconcile contract that private-skill consumers depend on.
- `install.sh` shipped and documented for external developers.

## Risk Summary
Primary risk is regressing existing skill orchestration behavior during the dispatcher rewrite (mitigated by preserving all instructions verbatim in secondary files) and scope creep back into release/packaging automation or cross-repo work (both explicitly descoped by prior `/refine-epic` decisions).

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)

## Documentation References
- **[CRITICAL]** [CLI Dispatcher Conventions](documentation/cli-dispatcher-conventions.md)
- **[CRITICAL]** [Agent Skill Format & Progressive Disclosure](documentation/agent-skill-format.md)
- **[IMPORTANT]** [Backward-Compatibility Contract Test Patterns](documentation/backward-compat-test-patterns.md)
- **[IMPORTANT]** [Install Script Conventions](documentation/install-script-conventions.md)
- **[REFERENCE]** [External Private-Skill Migration Descope](documentation/external-migration-descope.md)
- Full index: [documentation/README.md](documentation/README.md)
