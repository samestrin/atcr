---
id: mem-2026-07-15-49340e
question: "Does adding a new community persona to personas/community/ ever require a Go test-code change, despite the //go:embed mechanism needing none?"
created: 2026-07-15
last_retrieved: ""
sprints: []
files: [personas/community_test.go, personas/community.go, internal/personas/community_fixture_test.go]
tags: [clarifications, epic-27.0_local_model_ollama_persona, testing, community-registry]
retrievals: 0
status: active
type: clarifications
---

# Does adding a new community persona to personas/community/ e

## Decision

Yes. The //go:embed accessors in personas/community.go (CommunityNames, CommunityGet, CommunityFixture, CommunityModel, lines 17-52) are fully dynamic and need zero code changes for a new persona — this is the part of "no Go code changes needed" that's true. But personas/community_test.go maintains a SEPARATE, hand-maintained communityPersonas Go slice (lines 118-129) that is NOT derived from the embed or from index.json. TestCommunityAccessors (line 175) and TestCommunityIndex_Registration (lines 339-340) both cross-check counts between the dynamic CommunityNames()/index.json data and this static slice, and will fail the moment a new persona is embedded without a matching roster entry. Every per-persona test in that file (FixtureAndPromptCategory, SlugConsistency, Differentiation, DistinctCategories, PromptStructure, etc.) iterates communityPersonas directly, so an unregistered new persona silently gets zero test coverage even where it doesn't hard-fail. Conclusion: any plan claiming "no Go code changes needed" for a new community persona is correct only about the embed mechanism, not about this test roster — budget for a one-line edit to personas/community_test.go's communityPersonas slice per new persona.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- personas/community_test.go
- personas/community.go
- internal/personas/community_fixture_test.go
