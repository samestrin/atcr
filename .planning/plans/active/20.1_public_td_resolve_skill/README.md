## Overview
Standalone/public `atcr` users can review code but have no way to persist findings into a durable backlog or autonomously resolve them — the private `.planning/technical-debt/` + `/resolve-td` pipeline already has both. This plan closes that gap with a new `.atcr/`-scoped local TD store, a reconcile-time persistence hook, and a `/atcr debt resolve` skill route, so "public atcr" becomes a review-**and-fix** tool like its private counterpart.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/20.1_public_td_resolve_skill/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/20.1_public_td_resolve_skill/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/20.1_public_td_resolve_skill/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/20.1_public_td_resolve_skill/`

## Timeline & Milestones
TBD — sized during `/design-sprint` based on the 5 estimated user stories across 4 touched components (new store package, `skill/atcr-resolve/` + `skill/CONVENTIONS.md`, `cmd/atcr` reconcile hook, `docs/skill-usage.md`).

## Resource Requirements
Single-engineer implementation via the standard sprint pipeline (no new external dependencies — pure Go stdlib, consistent with the codebase's existing append-only-ledger precedents).

## Expected Outcomes
- A documented, `.atcr/`-scoped local technical-debt store that persists reconciled findings across review runs with zero `.planning/` dependency.
- A `/atcr debt resolve` skill route that autonomously resolves stored TD items, consuming the `justification`/back-reference fields already stamped by Epic 18.2.
- `skill/CONVENTIONS.md` extracted per Epic 20.0's addendum, shared by both public skills.
- Updated `docs/skill-usage.md` covering the new capability.

## Risk Summary
Three tracked risks (see `plan.md` Risk Mitigation): (1) apparent contradiction with Epic 19.4's move away from a `.atcr/`-scoped ledger — mitigated by documenting the differing audience; (2) concurrent-append line-tearing on the new ledger — mitigated by adopting the project's already-accepted TD-004 won't-fix stance; (3) the adapted resolve cycle diverging from `/resolve-td`'s proven behavior — mitigated by explicit grounding during `/design-sprint`.

## Documentation References
- **[CRITICAL]** [Agent Skills Format & Progressive Disclosure](documentation/agent-skills-format.md)
- **[CRITICAL]** [Append-Only JSONL Store Pattern](documentation/append-only-store-pattern.md)
- **[CRITICAL]** [Local TD Store Schema](documentation/local-td-store-schema.md)
- **[IMPORTANT]** [CLI Integration Points](documentation/cli-integration-points.md)
- **[IMPORTANT]** [Skill Dispatcher & CONVENTIONS.md Extraction](documentation/skill-dispatcher-conventions.md)

See [documentation/README.md](documentation/README.md) for the full index.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
