# Plan 19.6: Community-Canonical Model-Indexed Personas

## Overview
Repoints the community-persona fetch URL from `atcr/personas` to `samestrin/atcr` (canonical, in-repo), adds structured `provider`/`model` metadata so `atcr personas search` can find a persona by the model a user already holds, ships a human-named model-indexed persona library (frontier + flat-rate providers), migrates the three remaining role-named built-ins to human names, and rewrites onboarding docs to lead with the monetizing Synthetic quickstart path while keeping frontier personas opt-in.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/19.6_community_registry_hub/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/19.6_community_registry_hub/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`

## Timeline & Milestones
Multi-sprint feature (complexity Very-Complex, 7 estimated user stories, ~17 discrete tasks across 6 components — exceeds the `/execute-epic` scope guard, so this runs the full sprint pipeline). `/design-sprint` should determine whether the code tracks (fetch-and-pin, structured metadata, search filters) and the content tracks (persona library authoring, human-names migration) split into separate sprint phases given their different risk profiles.

## Resource Requirements
Go/Cobra CLI development in `internal/personas` and `cmd/atcr`; content authoring requiring research into each target model's own vendor prompting guidance (Anthropic, OpenAI, Google, DeepSeek, Qwen, Kimi, GLM, …); documentation updates to `README.md`, `docs/personas-authoring.md`, `docs/personas-install.md`.

## Expected Outcomes
A new user with a specific model's API key can run `atcr personas search <model>` and install a persona tuned to that model's own prompting conventions; the fetch source is canonical and in-repo rather than compiled into the binary; no role-based persona names remain in the active set; and `README.md`'s Quickstart leads with the flat-rate Synthetic path, keeping frontier "majors" personas positioned as opt-in.

## Risk Summary
Live install against the real `samestrin/atcr` URL is untestable until the repo is public — mitigated by the existing `httptest.NewServer`/`ATCR_PERSONAS_URL` mock pattern (AC6 scopes verification accordingly). Authoring 8+ genuinely model-tuned personas is a large content workload that should be phased separately from the schema/network code in `/design-sprint`. Schema extension to `PersonaIndexEntry`/`index.json` must stay additive to preserve backward compatibility with existing installed personas.

## Documentation References
- **[CRITICAL]** [Community Persona Fetch & Distribution (net/http + YAML)](documentation/fetch-and-distribution.md)
- **[CRITICAL]** [CLI Flag Wiring for Model-Aware Search (Cobra)](documentation/cli-search-flags.md)
- **[IMPORTANT]** [Persona YAML Schema & Struct Tags](documentation/persona-yaml-schema.md)
- **[IMPORTANT]** [Testing Patterns: testify + httptest Mock Registry](documentation/testing-mock-registry.md)
- **[IMPORTANT]** [Human-Names Migration for Built-in Stragglers](documentation/human-names-migration.md)
- **[IMPORTANT]** [Onboarding Hierarchy and Discover-by-Model Flow](documentation/onboarding-hierarchy.md)
- Full index: [documentation/README.md](documentation/README.md)

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation](documentation/)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
