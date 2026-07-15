---
id: mem-2026-07-15-aaa1e3
question: "Is the community persona openrouter/model-format lock in personas/community_test.go loader-enforced or test-only, and where exactly does it live?"
created: 2026-07-15
last_retrieved: ""
sprints: []
files: [personas/community_test.go, internal/registry/validate.go, .planning/sprints/completed/19.6_community_registry_hub/sprint-plan.md]
tags: [clarifications, epic-27.0_local_model_ollama_persona, architecture, testing, community-registry]
retrievals: 0
status: active
type: clarifications
---

# Is the community persona openrouter/model-format lock in per

## Decision

The community persona pool's "must route through openrouter" rule is enforced ONLY by two test-level assertions in personas/community_test.go — a provider-equality check at line 371 (commented "LOCKED Q3 / Phase 5 clarifications", looping over communityPersonas vs personas/community/index.json) and a model-namespacing check at line 484 in TestCommunityModel (requires "/" in every community persona's model string, looping over CommunityNames()). Neither is enforced by production/loader code: internal/registry/validate.go's ValidateCommunityPersonaYAML (lines 49-62) synthesizes a throwaway registry from whatever provider string is in the YAML and validates generically — it does not hard-code openrouter. The lock traces back to Epic 19.6's sprint-plan.md (lines 203, 215): "Routing key = openrouter for all 10 (LOCKED)... The content-lint allowlist... is therefore {openrouter} (may include synthetic as an also-allowed key)" — i.e. it was designed as an extendable allowlist constant, not immutable architecture, though "local" was never discussed as a member. Precedent check (tech-debt-captured.md:64) shows this project's convention when revisiting a LOCKED value has so far always been "leave it and capture as TD," never "edit the lock unilaterally" — any future work touching this lock needs explicit human sign-off, not just evidence that the change is mechanically safe.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- personas/community_test.go
- internal/registry/validate.go
- .planning/sprints/completed/19.6_community_registry_hub/sprint-plan.md
