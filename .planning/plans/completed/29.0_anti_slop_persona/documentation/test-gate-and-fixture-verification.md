# Test-Gate & Fixture Verification
**Priority:** [CRITICAL]

## Overview

atcr's community-persona authoring contract is enforced entirely by `go test`, using `testify`'s `assert` and `require` packages rather than manual review. The Integration Notes in the package doc specify the split atcr relies on: `assert` for most test assertions, and `require` for critical preconditions "where test logic cannot continue on failure" (`> Source: testify.md#Integration Notes (atcr)`). Adding the `simon` persona means it must survive both assertion styles across two layers of gates before `go test ./personas/... ./internal/personas/... ./internal/registry/...` can stay green.

The first layer is the hand-maintained roster in `personas/community_test.go`. A Go slice `communityPersonas` (personas/community_test.go:117) is the authoritative list the per-persona gate tests iterate over — checking fixture presence, category-word-in-template, slug consistency, Role+Focus Jaccard differentiation (<=0.85 threshold against every other persona), distinct finding-category words, distinct primary index task tags, the 7-column prompt structural contract, and required-values/dual-tools-state rendering (`> Source: codebase-discovery.json#Go test-enforced authoring contract (personas/community_test.go)`). `TestCommunityAccessors` (personas/community_test.go:175) asserts `len(CommunityNames()) == len(communityPersonas)` via `require.Len`, so `simon.md` without a matching roster entry fails red on a length mismatch — and because it is a `require` call, the failure is fatal: the roster-driven subtests never run, which is exactly the "critical precondition the rest of the test cannot proceed without" case the package doc reserves `require` for (`> Source: testify.md#require Package`).

The second layer is a set of embedded-set gates that iterate `personas.CommunityNames()` directly (the embedded directory listing) and therefore require no roster entry of their own: `internal/personas/community_fixture_test.go`, `internal/personas/test_test.go`, `internal/personas/community_schema_test.go`, and `internal/registry/persona_test.go:305` (`> Source: codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed)`). Both layers use table-driven `t.Run(p.Slug, ...)` / `t.Run(name, ...)` subtests, the pattern the package doc calls out as benefiting from assert's clear failure messages (`> Source: testify.md#Integration Notes (atcr)`).

## Key Concepts

- `assert` functions are non-fatal — they return `bool` and let the test continue after a failure is recorded, which is the default style for atcr unit test assertions.
  > Source: testify.md#assert Package
- `require` functions share the same signatures as `assert` but call `t.FailNow()` on failure, terminating the test immediately — used for critical preconditions the rest of the test cannot proceed without.
  > Source: testify.md#require Package
- `require` must be called from the goroutine running the test function to avoid race conditions.
  > Source: testify.md#require Package
- The roster-driven gate needs a new `communityPersonas` entry (Slug: "simon", plus a VendorToken substring of the chosen model id and a distinct lowercase Category) or `TestCommunityAccessors`'s `len(CommunityNames()) == len(communityPersonas)` check fails 14-vs-13.
  > Source: codebase-discovery.json#Go test-enforced authoring contract (personas/community_test.go)
- Embedded-set gates (community_fixture_test.go, test_test.go, community_schema_test.go, internal/registry/persona_test.go:305) require no roster entry — they iterate `CommunityNames()` directly, so simon is auto-covered once its fixture exists.
  > Source: codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed)
- 13 existing community personas already claim the category words coupling, logic, contract, validation, race, leak, complexity, type, dependency, observability, secret, duplication, invariant; `TestCommunityPersonas_DistinctCategories` fails the whole suite on a collision, so simon's category (e.g. "bloat") must be new and must also appear in simon.md's prompt text per `TestCommunityPersonas_FixtureAndPromptCategory` (personas/community_test.go:202).
  > Source: codebase-discovery.json#Existing categories already claimed (must not collide)
- `TestCommunityPersonas_DistinctTaskScoping` asserts each persona's `tasks[0]` in index.json is unique, so simon needs a fresh primary task tag (e.g. "bloat-review" or "slop-review") not already claimed by another persona.
  > Source: codebase-discovery.json#Existing primary index task tags already claimed (must not collide)

## Code Examples

The following examples are quoted verbatim from testify.md and illustrate the `assert`/`require` calling conventions the community-persona gates rely on.

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSomething(t *testing.T) {
    // Option 1: Direct function calls
    assert.Equal(t, 123, 123, "they should be equal")
    assert.Nil(t, someObject)

    // Option 2: Create an assert object (recommended for multiple assertions)
    assert := assert.New(t)

    assert.Equal(123, 123, "they should be equal")
    assert.NotEqual(123, 456, "they should not be equal")

    if assert.NotNil(someObject) {
        // Safe to use someObject here since it's not nil
        assert.Equal("ExpectedValue", someObject.Value)
    }
}
```
> Source: testify.md#assert Package

```go
import (
    "testing"
    "github.com/stretchr/testify/require"
)

func TestSomethingCritical(t *testing.T) {
    require := require.New(t)

    // If this fails, the test stops immediately
    require.NotNil(object, "object must exist to continue")
    require.Equal("value", object.Field)
}
```
> Source: testify.md#require Package

## Quick Reference

| Gate | Location | What it checks |
|------|----------|-----------------|
| Roster length parity | `personas/community_test.go:175` (`TestCommunityAccessors`) | `len(CommunityNames()) == len(communityPersonas)` — simon must be appended to the roster slice |
| Fixture + category-in-template | `personas/community_test.go:202` (`TestCommunityPersonas_FixtureAndPromptCategory`) | simon's fixture exists and its category word appears in simon.md's prompt text |
| Distinct categories | `personas/community_test.go` (`TestCommunityPersonas_DistinctCategories`) | simon's category word does not collide with the 13 existing community personas |
| Distinct task scoping | `personas/community_test.go` (`TestCommunityPersonas_DistinctTaskScoping`) | simon's primary `tasks[0]` index tag is unique |
| Role+Focus differentiation | `personas/community_test.go:272` (`TestCommunityPersonas_Differentiation`) | Jaccard similarity <=0.85 between simon and every other persona's Role+Focus |
| Prompt structural contract | `personas/community_test.go:417` (`TestCommunityPersonas_PromptStructure`) | required template tokens present, `## Role` + `## Output Format` headings, 7-column contract byte-for-byte in the Output Format section, exactly one vendor-guidance citation |
| Dual-tools-state rendering | `personas/community_test.go:450` (`TestCommunityPersonas_RendersInBothToolStates`) | the `{{if .ToolsEnabled}}` block renders cleanly with tools on and off, no unrendered actions in either state |
| Required-values rendering | `personas/community_test.go:470` (`TestCommunityPersonas_RequiredValuesRender`) | each required field's distinctive marker value actually reaches the rendered output (a token in a dead branch or comment would fail) |
| Fixture runner pass count | `internal/personas/community_fixture_test.go` (`TestTemplateFixtureRunner_CommunityPersonasPass`) | exactly 1 passing fixture case per persona, iterated via `CommunityNames()` |
| Bound-model assertion | `internal/personas/test_test.go` (`TestRunFixture_CommunityAssertsBoundModel`) | simon's fixture asserts against its pinned model |
| Strict schema / no placeholder / human names / lock-zero migration | `internal/personas/community_schema_test.go` | `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames`, `TestPinnedModelIsLockZeroMigration` |
| Embedded prompt validation | `internal/registry/persona_test.go:305` (`TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass`) | simon.md's fetched prompt validates like every other embedded community persona |

> Source: codebase-discovery.json#Go test-enforced authoring contract (personas/community_test.go); codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed)

## Related Documentation
- [../plan.md](../plan.md)
- `.planning/specifications/packages/testify.md`
