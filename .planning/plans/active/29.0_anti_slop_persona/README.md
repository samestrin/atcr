# Plan 29.0: Anti-Slop Persona (Simon) & Content Marketing

## Overview
Ship a new community persona, `simon`, purpose-built to detect AI-generated code bloat (tautological comments, unnecessary abstractions, defensive-programming overkill, dead/hallucinated code), fully integrated into ATCR's existing persona-authoring test gate, alongside a marketing outline pitching it as the free alternative to paid "slop cleanup" services.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/29.0_anti_slop_persona/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/29.0_anti_slop_persona/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/29.0_anti_slop_persona/`

## Timeline & Milestones
Simple-complexity feature (3 user stories, single subsystem — the persona registry). No cross-system work, no gated decisions. Expected to move through the pipeline in a single short sprint.

## Resource Requirements
No new dependencies. Touches `personas/community/` (new persona files), `personas/community_test.go` + `personas/community/index.json` (registration), and `.planning/product/content/blog/` (review/refresh of an already-existing outline).

## Expected Outcomes
`atcr review --persona simon` (or `atcr personas install simon`) becomes available to any ATCR user, giving teams a one-command lens dedicated to catching AI-authored bloat before it reaches review. The `simon` persona is fully covered by the project's own fixture-integrity test gate, so it can't regress silently. The paired blog outline gives the maintainer a ready-to-publish narrative connecting the "Slopfix $10k/week" industry pain point to this free, automated solution.

## Risk Summary
Main risk is a silent integration gap: `simon` could be dropped into `personas/community/` and work at runtime while remaining invisible to the fixture/differentiation/index-registration test suite if the `personas/community_test.go` roster entry is skipped. The plan calls this out explicitly as its own user story (Theme 2) to prevent it. Secondary risk is category-word or prompt-similarity collision with one of the 13 existing personas, mitigated by picking a distinct category word (e.g. `bloat`) up front.

## Documentation References
**Critical**
- [persona-yaml-and-prompt-authoring.md](documentation/persona-yaml-and-prompt-authoring.md) — `simon.yaml`/`simon.md` authoring contract (yaml.v3 strict schema, text/template prompt rendering)
- [test-gate-and-fixture-verification.md](documentation/test-gate-and-fixture-verification.md) — roster + embedded-set test gates (testify assert/require)

See [documentation/README.md](documentation/README.md) for the full index.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation](documentation/)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
