# Plan 19.6: Default Model-Tuned Community Personas

## Overview
Ships 1-2 default, model-tuned reviewer personas (e.g. Claude-tuned, GPT-4-tuned) through atcr's existing community-persona channel (`atcr personas install`), and updates this repo's docs to recommend installing them during first-time setup. Persona content itself is authored and published in the separate `atcr/personas` repo — this repo's own Definition of Done is limited to the documentation update.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/19.6_community_registry_hub/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/19.6_community_registry_hub/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`

## Timeline & Milestones
Small, content-driven epic: (1) author + fixture-test 1-2 model-tuned personas in `atcr/personas`, (2) publish via that repo's `index.json`, (3) update `docs/personas-install.md` + `README.md` Quickstart in this repo. No fixed dates — gated on the external repo PR landing before the in-repo doc update can reference live persona names with confidence.

## Resource Requirements
Single contributor familiar with both atcr's persona authoring contract (`docs/personas-authoring.md`) and each target model's official prompting guide (Anthropic's Claude guidelines, OpenAI's GPT-4 guidelines).

## Expected Outcomes
A new user runs `atcr personas search` / `atcr personas install <name>` and gets a well-tuned reviewer persona out of the box, without hand-authoring prompt phrasing themselves.

## Risk Summary
The only material risk is the cross-repo split: Tasks 1-2 (persona content) land in `atcr/personas`, outside this plan's TDD loop, so this plan cannot directly verify them — only the in-repo doc recommendation (Task 3) is this plan's own Definition of Done.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
