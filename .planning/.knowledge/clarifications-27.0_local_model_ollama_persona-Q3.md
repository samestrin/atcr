---
id: mem-2026-07-15-da18d8
question: "What test constraints does personas/community_test.go enforce on category/task uniqueness when adding a new community persona?"
created: 2026-07-15
last_retrieved: ""
sprints: []
files: [personas/community_test.go, personas/community/index.json]
tags: [clarifications, epic-27.0_local_model_ollama_persona, testing, community-registry]
retrievals: 0
status: active
type: clarifications
---

# What test constraints does personas/community_test.go enforc

## Decision

TestCommunityPersonas_DistinctCategories (personas/community_test.go:296-305) and TestCommunityPersonas_DistinctTaskScoping (personas/community_test.go:310-321) require every community persona's category and primary-task tag to be unique across the whole roster — no two personas may share a review category or task-scoping value. As of Sprint 19.6's 10 personas, taken categories include: coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability; taken task-scoping values include: architecture-review, correctness-review, api-review, validation-review, concurrency-review, resource-review, performance-review, type-safety-review, dependency-review, observability-review. Any new community persona must be assigned a category/task pair not already in this list, verifiable by reading personas/community/index.json and personas/community_test.go directly before proposing new persona names.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- personas/community_test.go
- personas/community/index.json
