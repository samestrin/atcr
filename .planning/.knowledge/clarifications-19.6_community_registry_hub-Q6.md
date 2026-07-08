---
id: mem-2026-07-08-989ead
question: "Should the AC7 verifyCommunityIndex gate assert path == name + '.yaml', or is name/path coupling verified elsewhere?"
created: 2026-07-08
last_retrieved: ""
sprints: [19.6_community_registry_hub]
files: [internal/personas/search_test.go, personas/community_test.go, personas/community/index.json]
tags: [clarifications, sprint-19.6_community_registry_hub, testing, personas, go]
retrievals: 0
status: active
type: clarifications
---

# Should the AC7 verifyCommunityIndex gate assert path == name

## Decision

No — do not add the assertion to verifyCommunityIndex; it is intentionally scoped to provider/model discovery metadata only (AC 02-02). The name/path coupling TD-003 wanted was already added in Phase 5 at personas/community_test.go's TestCommunityIndex_Registration, which asserts e.Name == p.Slug and e.Path == p.Slug + ".yaml" for every persona in the authoring list (AC 04-05). All 10 shipped community/index.json entries already satisfy this. TD-003 should be considered resolved via that test, not via a change to verifyCommunityIndex.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/search_test.go
- personas/community_test.go
- personas/community/index.json
