# Plan 33.0: Final Code Review + Documentation Sweep

## Overview
First epic in the launch cluster (`33.0` review + docs -> `33.1` launch content -> `33.2` go-public + atcr.dev). Runs a comprehensive multi-agent + adversarial code review over the production codebase, fixes CRITICAL/HIGH findings, routes MEDIUM/LOW to technical debt, then audits all user-facing documentation (README, docs/, CLI help, schemas) against the finalized code so the repo and `atcr.dev` are ready to go public.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/33.0_final_documentation_sweep/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/33.0_final_documentation_sweep/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/33.0_final_documentation_sweep/`

## Timeline & Milestones
Estimated 2-3 days per the original request: code review + fixes first, documentation sweep second (docs describe the *finalized* code, not the other way around).

## Resource Requirements
Single maintainer (Sam Estrin); no external stakeholders. Uses atcr's own multi-agent reviewer (dogfooding) plus a manual adversarial pass.

## Expected Outcomes
- CRITICAL/HIGH code-review findings fixed; MEDIUM/LOW captured as technical debt.
- No secrets, credentials, or embarrassing artifacts in the codebase or history.
- No legacy persona names (`sentinel`, `tracer`, `idiomatic`) remain anywhere in docs or CLI help.
- All features through Epic 23.0 fully and accurately documented.
- `docs/` validated and ready for import into the `atcr.dev` website repository.

## Risk Summary
Open-ended review scope could exceed the 2-3 day estimate; mitigated by scoping to the named components and a CRITICAL/HIGH-only fix bar. Legacy-persona-name grepping risks false positives against legitimate Go "sentinel error" idiom usage; mitigated by relying on the existing identifier-scoped guard test (`personas/retired_slugs_test.go`) as the authoritative check.

## Documentation References
- **[CRITICAL]** [multi-agent-review-workflow.md](documentation/multi-agent-review-workflow.md)
- **[CRITICAL]** [technical-debt-triage-resolution.md](documentation/technical-debt-triage-resolution.md)
- **[IMPORTANT]** [persona-naming-doc-accuracy.md](documentation/persona-naming-doc-accuracy.md)

See [documentation/README.md](documentation/README.md) for details.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)
