## Overview
Plan 17.0 lets ATCR apply the fixes it generates instead of leaving that burden on the developer — parse the diff, apply it safely, validate locally, revert automatically on failure, and (opt-in via `--auto-fix`) push a branch and open/update a PR. Most of the underlying capability already ships (diff parsing, crash-safe backup/swap, local syntax validation, PR commenting); the net-new surface is the apply step, the revert wiring, the create-branch/PR half of GitHub orchestration, and the opt-in flag itself.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/17.0_auto_merged_fixes/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/17.0_auto_merged_fixes/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/17.0_auto_merged_fixes/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/17.0_auto_merged_fixes/`

## Timeline & Milestones
Estimated 10 days per the source epic. Milestones follow the six user-story themes: apply → validate → revert → branch/commit → PR → opt-in flag, each landing as an independently testable increment.

## Resource Requirements
Backend Team (Go). No new external services — extends the existing GitHub App/token integration already used by `internal/ghaction` (Epic 7.3).

## Expected Outcomes
- `atcr` can auto-fix at least 70% of the simple technical debt items it flags, with zero broken builds introduced in the test corpus.
- Developers get an opt-in, off-by-default `--auto-fix` flag; default behavior is unchanged when it is absent.
- A failed validation always reverts the working tree before any GitHub state is touched.

## Risk Summary
Primary risk is cross-system: a local revert (AC4) cannot undo a pushed branch or an already-opened PR, so the flow must sequence local validation strictly before any GitHub-mutating call. Secondary risks (validation-command generality, merge-conflict handling) are scoped down explicitly in plan.md's Risk Mitigation section.

## Documentation References
- **[CRITICAL]** [Patch Application (AC2)](documentation/patch-application.md)
- **[CRITICAL]** [Validation and Automatic Revert (AC3/AC4)](documentation/validation-and-revert.md)
- **[IMPORTANT]** [GitHub API Orchestration (AC5)](documentation/github-orchestration.md)
- **[REFERENCE]** [Opt-In Flag with Refuse-Without-Backend Gate (AC6)](documentation/cli-opt-in-gate.md)

See [documentation/README.md](documentation/README.md) for the full index and source attribution.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Sprint Design](sprint-design.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Package Recommendations](package-recommendations.md)
- [Documentation](documentation/)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
