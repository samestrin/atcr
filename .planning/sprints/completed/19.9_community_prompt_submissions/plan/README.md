# Plan 19.9: Community Prompt Submissions (Intake & Curation)

## Overview

Establishes a GitHub-native intake and curation flow so users can contribute locally-tuned reviewer personas back to the canonical library via a single `atcr personas submit <name>` command — reusing the existing fixture gate and shelling out to `gh` for the fork+PR, with no marketplace, website, or hosted registry. A new `submitted` status, orthogonal to the existing `Source` (`built-in`|`community`) field, marks fixture-passing-but-unvetted submissions until a maintainer graduates them into the vetted `personas/community/` library.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/19.9_community_prompt_submissions/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/19.9_community_prompt_submissions/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.9_community_prompt_submissions/`

## Timeline & Milestones

TBD — estimated in `/design-sprint` once user stories and acceptance criteria are decomposed.

## Resource Requirements

Single-developer implementation (Go/Cobra CLI change plus documentation); no additional infrastructure. Requires the `gh` CLI to be installed and authenticated in any environment exercising the submit flow's integration tests.

## Expected Outcomes

- `atcr personas submit <name>` becomes the seventh `personas` subcommand, automating fork+PR for a locally-tuned persona that passes its fixture.
- A `submitted` status distinct from `Source` tracks unvetted submissions until maintainer graduation.
- No new hosting, marketplace, or ranking surface — the flow is entirely GitHub-PR-native, honoring the Epic 19.6 out-of-scope constraint.
- `docs/personas-install.md` and `docs/personas-authoring.md` document the new subcommand and the two-tier curation model.

## Risk Summary

- `gh` CLI absence/auth failure on the user's machine — mitigated with an upfront precondition check.
- `submitted` status implemented as a third `Source` value would break existing `built-in`|`community` consumers — mitigated by keeping it a separate field/concept (see codebase-discovery.json architecture_notes).
- An inline, non-injectable `gh` integration would be untestable in CI — mitigated by following the codebase's existing injectable-seam pattern.

## Documentation References

See [documentation/README.md](documentation/README.md) for the full index. Summary by priority:

- **[CRITICAL]** [GitHub Fork + PR Integration via go-gh](documentation/gh-fork-pr-integration.md)
- **[CRITICAL]** [Cobra Subcommand & Injectable-Seam Conventions](documentation/cobra-subcommand-patterns.md)
- **[CRITICAL]** [Local Fixture-Gate Reuse (TestPersona)](documentation/fixture-gate-reuse.md)
- **[IMPORTANT]** [Status/Provenance Separation and Atomic Persistence](documentation/status-provenance-and-atomic-writes.md)
- **[IMPORTANT]** [Personas Install & Authoring Doc Updates (AC4)](documentation/personas-docs-updates.md)

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Package Recommendations](package-recommendations.md)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Sprint Design](sprint-design.md)
