# User Story 4: Maintainer Graduation into the Vetted Library

**Plan:** [19.9: Community Prompt Submissions (Intake & Curation)](../plan.md)

## User Story

**As a** atcr project maintainer reviewing a `submitted` persona's PR
**I want** a documented, repeatable process to promote a fixture-passing-but-unvetted persona into the vetted `personas/community/` library once I've battle-tested it
**So that** the two-tier curation model (submitted -> graduated) closes the loop end-to-end without introducing any new automated promotion pathway, ranking system, or hosted approval surface

## Story Context

- **Background:** Stories 1-3 deliver the automated intake half of this plan: a contributor runs `atcr personas submit <name>`, the local fixture gate blocks unvetted/broken personas (Theme 1), a passing persona is pushed via a fork+PR opened through `gh` (Theme 2), and the PR carries a `submitted` status plus attribution metadata that stays orthogonal to the existing `Source` (`built-in`|`community`) field (Theme 3). This story is the other half: what a maintainer does with that PR once it lands. Epic 19.6 already established a human-review PR-merge gate for all community-persona intake — this story does not build a new gate, it documents how that existing gate is exercised specifically against a `submitted` PR, and what "graduation" means as a concrete set of repo edits (move/copy the persona file into `personas/community/`, add a matching `personas/community/index.json` entry per the `PersonaIndexEntry` schema at `internal/personas/search.go:14`, and clear the `submitted` status marker introduced in Story 3).
- **Assumptions:** A `submitted` PR opened by Theme 2 already exists in the canonical repo, carrying the persona's YAML/template files plus the `submitted` status/attribution marker from Theme 3. The maintainer has already battle-tested the persona (out of scope for this story — "battle-tested" is a human editorial judgment, not an automated check). Graduation happens entirely through the existing GitHub PR-review-and-merge workflow (review comments, requested changes, approve, merge) — no new CLI command, API endpoint, or automated promotion script is introduced. The `index.json` entry's `provider`/`model` fields must match the persona's own YAML frontmatter, mirroring how existing `personas/community/` entries are structured today.
- **Constraints:** Must not introduce any new automated promotion pathway, ranking/approval UI, or hosted registry surface (AC3's out-of-scope constraint is binding on this story too). Graduation must flip only the `submitted` status marker — it must never touch or reinterpret the persona's `Source` field, which stays `community` before and after graduation (per Theme 3's separation). The graduation steps must be expressible as ordinary git operations a maintainer performs while merging/editing the PR (or as a small maintainer checklist), not as new product code requiring its own tests beyond documentation accuracy.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Story 3 (submitted status/provenance separation must exist before graduation can clear it) |

## Success Criteria (SMART Format)

- **Specific:** A documented maintainer graduation procedure exists that, given a `submitted` PR, describes moving the persona into `personas/community/`, adding a matching `index.json` entry (name/version/description/path/provider/model consistent with the persona's YAML), and clearing the `submitted` status marker without altering `Source` — entirely through the existing PR-review-and-merge process.
- **Measurable:** The procedure is captured in `docs/personas-authoring.md` (cross-referenced against its existing contribution checklist) as a numbered checklist a maintainer can follow step-by-step, and covers all three required edits (index entry, persona placement, status marker removal) plus the constraint that `Source` is never touched.
- **Achievable:** No new code is required — Story 3 already defines the `submitted` marker's shape and where it lives; this story only writes down the manual steps to clear it and adds the sibling `index.json` entry, both of which are edits a maintainer already knows how to make to this repo.
- **Relevant:** Closes AC2's curation loop — without a documented graduation path, `submitted` personas would accumulate with no defined route into the vetted library, leaving the two-tier model half-built.
- **Time-bound:** Documentation-only story completed alongside Theme 5 (Story 5) in the same sprint, after Story 3's status/provenance marker lands and before the sprint's documentation pass is considered complete for AC4.

## Acceptance Criteria Overview

1. A maintainer graduation procedure is documented describing how to promote a `submitted` PR's persona into `personas/community/`, including adding a matching `PersonaIndexEntry` to `personas/community/index.json` with provider/model consistent with the persona's YAML.
2. The procedure explicitly states that graduation clears/removes the `submitted` status marker introduced in Story 3 without modifying the persona's `Source` field.
3. The procedure confirms graduation is performed entirely via the existing human-review PR-merge process (review, requested changes, approve, merge/edit) with no new CLI command, script, or automated promotion pathway introduced.
4. The documentation cross-references this graduation procedure from the existing contribution checklist context so a maintainer encountering a `submitted` PR can find it.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`_

## Technical Considerations

- **Implementation Notes:** This is a documentation/process story, not a new code path. Add a "Graduating a submitted persona" section to `docs/personas-authoring.md` alongside the existing contribution checklist (docs/personas-authoring.md:162 area), written as a maintainer-facing numbered checklist: (1) verify the PR's fixture gate passed (already enforced by Story 1/2's automated `submit` flow, re-confirm via CI on the PR), (2) battle-test the prompt against real reviews (editorial judgment, not automated), (3) move/copy the persona file(s) into `personas/community/`, (4) add a `PersonaIndexEntry` to `personas/community/index.json` with `name`/`version`/`description`/`path`/`provider`/`model` matching the persona's own YAML frontmatter, (5) clear the Story 3 `submitted` status marker for that persona, (6) confirm the persona's `Source` is untouched (remains `community`), (7) merge the PR. No `RunE`, no new Go package, no new test file — verification is that the doc accurately describes the existing schema and process.
- **Integration Points:** References the `PersonaIndexEntry` schema (`internal/personas/search.go:14`) as the exact shape of the required `index.json` entry. References Story 3's `submitted` status marker mechanism (its storage location/format) as the thing graduation must clear. Reuses Epic 19.6's existing human-review PR-merge gate — no new GitHub App, webhook, or bot integration is introduced.
- **Data Requirements:** None beyond the existing `personas/community/index.json` array shape and the Story 3 status-marker format; this story defines no new schema, only the manual maintainer procedure for editing existing schemas correctly during graduation.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A maintainer graduates a persona but forgets to add the matching `personas/community/index.json` entry, leaving it invisible to `personas search`/`list` despite being physically present in the vetted directory | Medium | Document the `index.json` entry as a required, explicit checklist step (not an aside) and note that its `provider`/`model` fields must be verified against the persona's own YAML before merge |
| A maintainer clears the `submitted` marker but also edits/removes the `Source` field out of habit (conflating the two axes this plan deliberately keeps separate) | Medium | State explicitly and prominently in the documented procedure that `Source` must never change during graduation — it stays `community` before and after — reinforcing Theme 3's separation |
| Future contributors interpret "graduation" as requiring new tooling and build an unwanted automated promotion script, reintroducing scope this plan explicitly excludes (AC3) | Low | Document graduation as an explicitly manual, PR-native process and state the out-of-scope boundary (no ranking UI, no automated promotion, no hosted approval surface) directly in the docs section |

---

**Created:** July 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
