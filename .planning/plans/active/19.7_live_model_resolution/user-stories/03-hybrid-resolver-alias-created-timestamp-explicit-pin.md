# User Story 3: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr maintainer curating persona `model` bindings across ten personas from four vendors
**I want** a single resolver that turns a persona's family/channel binding into a concrete slug using the correct strategy per vendor (provider `-latest` alias, `created`-timestamp newest-in-vendor-prefix, or an explicit pin)
**So that** every persona resolves to a currently-valid model without hand-editing slugs, and any persona I do pin explicitly is guaranteed to never silently float onto a newer model

## Story Context

- **Background:** Story 1's catalog spike determines whether OpenRouter's `~`-prefixed `-latest` aliases are actually completion-routable, and Story 2 defines the family/channel binding data model and lock field that this resolver populates. This story is the resolution engine itself — the logic invoked exclusively from `Upgrade()` (`internal/personas/upgrade.go:27`) that takes a persona's declared family/channel and produces the concrete slug written into the lock. Of the 10 personas, 7 have a provider-owned `-latest` alias (Anthropic, OpenAI, Google, Moonshot) where the provider itself resolves to the newest member — near-zero resolver maintenance. The remaining 3 (delia→DeepSeek, quinn→Qwen, glenna→GLM) have no `-latest` alias in the catalog and require this story's `created`-timestamp newest-in-vendor-prefix strategy, or fall back to an explicit-slug pin as an always-available escape hatch.
- **Assumptions:** Story 1 has confirmed (or refuted) alias routability and defined the `@stable` exclusion heuristic (preview/beta/exp tokens, `expiration_date`); Story 2's family/channel binding schema and lock field already exist on `PersonaIndexEntry` (or the on-disk persona unit) for this resolver to read from and write into. The OpenRouter catalog client (mirroring `internal/personas/client.go:99`'s `fetch()` helper) is assumed available or built alongside this story to supply the model list the `created`-timestamp comparison operates over.
- **Constraints:** The resolver must be invoked only from the explicit upgrade path (`internal/personas/upgrade.go:27`), never on the review hot path — this is a hard architectural rule carried over from Epic 19.6's C3 reproducibility posture (`internal/registry/persona.go:47`'s `ResolvePersona()` chain must never be touched or re-implemented by this logic). An explicit-slug pin is a first-class, always-available third path — it must never be re-resolved or floated regardless of which channel (`@stable`/`@latest`) is otherwise in effect for that persona. The GLM persona (glenna) resolves against catalog slug `z-ai/glm-5.2` — the vendor prefix for the `created`-timestamp newest-in-vendor-prefix comparison is **`z-ai/`, not `glm/`** (no `glm/` namespace exists in the catalog); this correction was previously caught during `/refine-epic` and must not regress.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | User Story 1 (Catalog Routability Spike), User Story 2 (Family/Channel Binding & Resolved Lock) |

## Success Criteria (SMART Format)

- **Specific:** A resolver function accepts a persona's family/channel binding (or explicit pin) and the fetched catalog model list, and deterministically returns exactly one concrete slug using one of three strategies: alias passthrough (7 personas), `created`-timestamp newest-in-vendor-prefix scan (delia/quinn/glenna), or explicit-pin passthrough (any persona, any channel).
- **Measurable:** All 10 personas resolve to a non-empty slug against the checked-in catalog snapshot fixture; unit tests cover each of the 3 strategies plus the `z-ai/` prefix case for glenna specifically (not `glm/`); an explicit-pin test asserts the returned slug is identical across two catalog snapshots where the vendor-prefix newest member differs, proving it never floats.
- **Achievable:** Builds directly on Story 2's binding schema and the catalog client either delivered by Story 1's spike or built in parallel; the `created`-timestamp comparison is a straightforward sort/filter over already-parsed catalog JSON, no new external dependency.
- **Relevant:** This is the core value proposition of the epic — turning a static per-persona slug into a self-maintaining binding that rides each vendor's capability curve without manual edits, while preserving an escape hatch for personas that need a hand-pinned version.
- **Time-bound:** Resolver logic, its unit tests against the catalog snapshot fixture, and the `z-ai/` prefix regression test land within this story's implementation slice, ready for Story 4 to wire into `atcr personas upgrade`.

## Acceptance Criteria Overview

1. The 7 alias-covered personas (Anthropic ×2, OpenAI ×3, Google ×2, Moonshot ×1) resolve by returning the provider's `~`-prefixed `-latest` alias slug unchanged — the resolver performs no catalog scan for these, the provider owns resolution.
2. delia (DeepSeek), quinn (Qwen), and glenna (GLM) resolve via a `created`-timestamp newest-in-vendor-prefix scan over the catalog snapshot, filtered by each persona's correct vendor prefix — critically `z-ai/` for glenna, never `glm/` — with the `@stable` heuristic (from Story 1) excluding preview/beta/exp-tokened and expired entries unless the channel is `@latest`.
3. Any persona bound to an explicit-slug pin resolves to that exact slug regardless of channel or catalog contents, and this pin is never overwritten or re-resolved by either the alias or `created`-timestamp strategies.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.7_live_model_resolution/`_

## Technical Considerations

- **Implementation Notes:** Add the resolver as a new function (or small set of functions) in `internal/personas`, likely alongside or inside the catalog client introduced by Story 1/2 (e.g. `internal/personas/catalog.go` or a new `resolve.go`). It takes the persona's binding (family + channel, or explicit pin flag) and the parsed catalog model list, and returns a concrete slug plus enough metadata (e.g. resolved `created` timestamp) for Story 4's before→after upgrade report. Strategy selection is a simple switch: explicit pin short-circuits first; else check the 7-persona alias table; else fall back to the `created`-timestamp newest-in-vendor-prefix scan for delia/quinn/glenna.
- **Integration Points:** Called exclusively from `internal/personas/upgrade.go:27`'s `Upgrade()`, inserted before the existing `isNewer`/write logic per the documented insertion point — resolution happens once per explicit upgrade invocation, never on the review hot path (`internal/registry/persona.go:47` is off-limits). Consumes Story 1's catalog client and `@stable`/`@latest` heuristic, and Story 2's family/channel binding schema on `PersonaIndexEntry` (`internal/personas/search.go:14`) plus the lock field it populates via `writePersonaUnit()` (`internal/personas/unit.go:95`).
- **Data Requirements:** Reads catalog model entries with `id`/`canonical_slug`/`created`/`expiration_date` fields (per `documentation/openrouter-catalog-api.md`) from the catalog client's fetched (or checked-in snapshot, in tests) model list. The vendor-prefix table for the `created`-timestamp strategy must map delia→`deepseek/`, quinn→`qwen/`, glenna→`z-ai/` — sourced from `personas/community/index.json`'s actual entries, not assumed from persona display names.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Resolver assumes a `glm/` vendor prefix for glenna instead of the catalog's actual `z-ai/` namespace, silently failing to resolve or returning zero matches | High | Explicit unit test asserts the newest-in-vendor-prefix scan for glenna filters on `z-ai/`; this exact mistake was previously caught during `/refine-epic` against `personas/community/index.json` and must be encoded as a regression test, not just a code comment |
| An explicit-slug pin is accidentally routed through the `created`-timestamp or alias strategy on a channel re-evaluation, causing it to float when it should never change | High | Strategy selection checks for an explicit pin first and short-circuits before any catalog lookup; a dedicated test resolves the same pinned persona against two different catalog snapshots and asserts identical output |
| The `@stable` heuristic (preview/beta/exp exclusion, `expiration_date` honoring) is inconsistently applied between the alias path (provider-owned, heuristic is moot) and the `created`-timestamp path (heuristic is load-bearing), causing DeepSeek/Qwen/GLM to occasionally resolve to an expiring or preview model | Medium | Reuse Story 1's single `@stable` filter function verbatim in the `created`-timestamp scan rather than reimplementing the exclusion logic locally; add a fixture case with a preview-tokened newest entry to prove it is skipped under `@stable` |
| Catalog snapshot fixture drifts from the live OpenRouter schema over time, masking a real vendor-prefix or `created` field format change from CI | Medium | Per the epic's documented mitigation, rely on the plan-level refresh command (Story covering AC8) to regenerate the snapshot on demand; this story's tests assert against the current checked-in fixture, not live network |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
