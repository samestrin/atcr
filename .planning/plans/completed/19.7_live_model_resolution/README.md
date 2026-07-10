# Plan 19.7: Live Model Resolution, Lockfile & Drift Detection

## Overview
Layers a live, auto-updating model resolution system over the persona `model` bindings Epic 19.6 shipped: a persona binds to a model family/channel, atcr resolves and records the concrete slug as a lock, and reviews always run the lock — never a live-resolved value — so reproducibility is preserved. Resolution only happens on an explicit `atcr personas upgrade`. Also ships `atcr models check [--json]` (a deterministic drift/deprecation report) and closes Epic 19.6's deferred HIGH by reconciling the disjoint init/quickstart fetch-and-pin roster against the shipped community index.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/19.7_live_model_resolution/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/19.7_live_model_resolution/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/19.7_live_model_resolution/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.7_live_model_resolution/`

## Timeline & Milestones
Complexity scored 10/12 (Very-Complex) by `/design-sprint`: 8 phases (Research & Spike → Foundation → Core Resolution → Upgrade Integration → Discovery Command → Validation Gate → Roster Reconciliation → Integration & Docs), 15 days total. See [sprint-design.md](sprint-design.md) for the full phase breakdown, complexity scoring, and risk analysis. `--gated` is strongly recommended for `/create-sprint`.

## Resource Requirements
Backend/CLI engineering time only (Go); no new infrastructure, no new third-party dependency, no design/UX resourcing (no visual surface). One authenticated OpenRouter API call is required for the AC1 spike; all subsequent resolver tests run against a checked-in catalog snapshot fixture with zero live network in CI.

## Expected Outcomes
- Persona model bindings float with each vendor family's capability curve without manual slug edits, while reviews stay reproducible by default via an explicit, reported lock.
- A deterministic `atcr models check` command gives users (and Epic 19.8's downstream mechanical agent) machine-readable drift/deprecation visibility.
- Epic 19.6's deferred HIGH (TD-011, disjoint init/quickstart roster) is closed — online `init`/`quickstart` deliver a working, non-noisy persona set.

## Risk Summary
Alias routability is unconfirmed until the AC1 spike runs (mitigated by a resolver design that doesn't depend on the outcome); a checked-in catalog snapshot can drift from the live API over time (mitigated by an explicit refresh command); and the two independent init/quickstart call sites risk being fixed inconsistently for AC7 (mitigated by routing the fix through one shared reconciliation point). See `plan.md` Risk Mitigation for full detail.

## Documentation References
- **[CRITICAL]** [OpenRouter Catalog & Completions API](documentation/openrouter-catalog-api.md)
- **[CRITICAL]** [Existing Codebase Patterns to Reuse](documentation/existing-resolver-patterns.md)
- **[IMPORTANT]** [Semantic Version Comparison](documentation/semver-version-comparison.md)
- Full index: [documentation/README.md](documentation/README.md)

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation](documentation/)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Sprint Design](sprint-design.md)
