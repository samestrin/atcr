# Existing Codebase Patterns to Reuse (Resolver, Retry, Version Compare)

**Priority:** CRITICAL

## Overview

Epic 19.6 already built the entire machinery this plan's resolver layer needs to stand on. `internal/personas/client.go` owns a retrying, timeout-bounded, size-capped HTTP fetch helper behind an injectable `HTTPClient` interface, exercised end-to-end against `httptest.NewServer` with zero live network in CI. `internal/personas/upgrade.go` already re-fetches, semver-compares, and conditionally writes persona updates. `internal/personas/search.go` already extended its index-entry schema additively (`omitempty`, permissive decode) once, for `Provider`/`Model`/`Tasks`/`Tags`. None of this needs to be reinvented — the new OpenRouter catalog client, the lock field on a persona's model binding, and the major-bump re-validation gate are all extensions of patterns that already exist and are already tested.

The one architectural rule that must not be violated: the resolver/lock layer is strictly upstream of `internal/registry.ResolvePersona` (persona.go:47). `ResolvePersona` decides which persona *prompt* text wins across the project > registry/pinned-community > `_base` > embedded tiers — a chain 19.6 built and hardened. This epic's resolver decides which *model string* a family/channel binding maps to, and it must produce a lock consumed at review time, never touching or re-implementing prompt resolution. Keep the two concerns in separate code paths; no resolver/lock logic belongs inside `persona.go`.

Secondarily, this epic must close a real drift risk already visible in 19.6's TD history: `cmd/atcr/init.go` and `cmd/atcr/quickstart.go` independently call the same `installCommunityPersonas` installer with an identical, currently-disjoint roster (9 model-agnostic built-ins vs. 10 model-indexed library personas). That is the same two-call-site drift pattern that produced TD-006/TD-007 previously — AC7 must fix it in exactly one place.

## Key Concepts

- **The `fetch()` retry helper is the catalog-client template.** `internal/personas/client.go:99` implements the GET-with-retry shape (context timeout, exponential backoff on transport errors/429/5xx, body-size cap, 404 mapping) that a new OpenRouter catalog client should copy structurally rather than reinvent. If URL-building differs enough to warrant sharing code, extract a helper — do not fork the retry logic into a second, divergent implementation.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/client.go:99]

- **`Upgrade()` is the exact seam the resolver call plugs into.** `internal/personas/upgrade.go:27` already re-fetches, validates, and version-compares before writing a persona update. The family/channel-to-concrete-slug resolver call belongs immediately before the existing `isNewer`/write logic in this function, so resolution happens only at upgrade time — never on the review hot path, matching 19.6's C3 reproducibility posture.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/upgrade.go:27]

- **`isNewer()` is the structural template for the major-bump re-validation gate.** `internal/personas/upgrade.go:89` is the existing semver-aware comparison; AC6's fixture-repass + re-tune-flag gate is triggered specifically when `semver.Major(remote) != semver.Major(local)`, distinct from the minor-advance path that already auto-writes today.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/upgrade.go:89]

- **The lock field must follow the existing additive-schema convention, not a new one.** `internal/personas/search.go:14` (`PersonaIndexEntry`) already added `Provider`/`Model` as `omitempty` fields decoded permissively (no `KnownFields`/`DisallowUnknownFields`), so old-shape `index.json` payloads still decode. Any new logical family/channel binding field(s) this epic adds must follow the identical convention.
  > Source: [codebase-discovery.json > existing_patterns > Additive, omitempty schema extension for index.json]
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/search.go:14]

- **`ResolvePersona` is off-limits — the lock layer sits upstream of it, never inside it.** `internal/registry/persona.go:47` is the single prompt-resolution chain (project > registry/pinned-community > `_base` > embedded). This epic's model-binding resolver is a separate, upstream concern and must not be merged into or touch this function.
  > Source: [codebase-discovery.json > semantic_matches > internal/registry/persona.go:47]
  > Source: [codebase-discovery.json > architecture_notes]

- **The two-call-site roster drift is a known risk class, and AC7 must fix it once.** `cmd/atcr/init.go:96` (`installCommunityPersonas`) is called from both `init.go:47` and `quickstart.go:102`, each looping over `builtins.Names()` against the fetched index, producing the disjoint 9-vs-10 roster (TD-011) and the skip-warning at `init.go:129`. Fixing this in one shared location avoids repeating the exact drift pattern that produced TD-006/TD-007 in 19.6.
  > Source: [codebase-discovery.json > semantic_matches > cmd/atcr/init.go:96]
  > Source: [codebase-discovery.json > architecture_notes]

- **Injectable `HTTPClient` + `httptest` is the required testing discipline for any new network-facing client.** `internal/personas.HTTPClient` is a minimal `Do(req) (*http.Response, error)` interface; production code passes `*http.Client`, tests pass an `httptest.NewServer`-backed client, so every fetch path is exercised in CI with zero real network calls. A new `CatalogClient` must follow this same seam.
  > Source: [codebase-discovery.json > existing_patterns > Injectable HTTPClient + httptest zero-live-network testing]

- **Fixture-gated persona content changes remain a hard gate, independent of the resolver.** Every persona ships a committed `.patch` fixture exercised by `TemplateFixtureRunner`; prompt/template changes cannot ship without passing it. This is orthogonal to the model-binding lock but must not be weakened by this epic's changes.
  > Source: [codebase-discovery.json > existing_patterns > Fixture-gated persona content changes]

- **Per-package retry helpers, not a shared one, are this codebase's convention.** `internal/ghaction/client.go:39` (`retriableStatus`) is a second, independent retry/backoff client for GitHub's API, confirming that a new OpenRouter catalog client should have its own retry logic in `internal/personas` rather than trying to extract a cross-package shared retry utility.
  > Source: [codebase-discovery.json > semantic_matches > internal/ghaction/client.go:39]

- **`ListTiers()` is the template for an installed-persona drift enumeration.** `internal/personas/list.go:53` already walks and dedups project > community > built-in tiers of persona files; `atcr models check` (AC5) needs an analogous enumeration, but over installed personas rather than files, to report per-persona drift/deprecation.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/list.go:53]

- **`cmd/atcr/main.go:202` is where the new `atcr models` command family is registered.** `newPersonasCmd()` is added to the root command's `AddCommand` list at this line. The net-new `newModelsCmd()` (and its `check` subcommand) must be registered in the same list, following the existing convention of grouping related top-level commands.
  > Source: [codebase-discovery.json > integration_points > cmd/atcr/main.go:202]

- **`internal/personas/unit.go:95` is the shared write path for any on-disk lock.** `writePersonaUnit()` is the paired-write tail used by both `InstallUnit` and `Upgrade`. If the resolved lock is persisted inside the on-disk persona unit (YAML), it flows through here so both install and upgrade paths write it consistently.
  > Source: [codebase-discovery.json > integration_points > internal/personas/unit.go:95]

- **The locked model slug reaches the wire through `internal/registry/config.go` and `internal/fanout/review.go`.** `AgentConfig.Model` (`internal/registry/config.go`) is the downstream field that ultimately consumes the resolved lock at review time; `renderAgent` (`internal/fanout/review.go`) builds the `llmclient.Invocation` from that config. The resolver must never be invoked on the review path — the lock is read as a static value at these points.
  > Source: [codebase-discovery.json > architecture_notes]
  > Source: [codebase-discovery.json > related_files > internal/registry/config.go]
  > Source: [codebase-discovery.json > related_files > internal/fanout/review.go]

- **The 19.6 AC7 YAML↔index match gate must not be silently broken by a new lock field.** `personas/community_test.go` (`TestCommunityIndex_Registration`) and `internal/personas/search_test.go` (`TestCommunityIndex_ProviderModelMatchesYAML`, `TestVerifyCommunityIndex_FailsOnMismatch`) enforce that `index.json` provider/model/description equals the source YAML. A resolved lock field must either be exempt from that exact-match check or be kept in sync by the same generation path that writes `index.json`.
  > Source: [codebase-discovery.json > architecture_notes]
  > Source: [codebase-discovery.json > related_files > personas/community_test.go]
  > Source: [codebase-discovery.json > related_files > internal/personas/search_test.go]

## Code Examples

No literal code snippets exist in the source discovery data — only file:line references and prose descriptions. Do not fabricate Go function bodies from this document; open and read each file directly before implementing:

- `internal/personas/client.go:99` — the `fetch()` retry/backoff/timeout/size-cap helper to use as the catalog-client template.
- `internal/personas/upgrade.go:27` — the `Upgrade()` function; the resolver call's insertion point, before the existing `isNewer`/write logic.
- `internal/personas/upgrade.go:89` — the `isNewer()` semver comparison; structural template for the major-bump re-validation trigger.
- `internal/personas/search.go:14` — the `PersonaIndexEntry` struct; where the new logical family/channel binding field(s) get added, additively.
- `internal/registry/persona.go:47` — the `ResolvePersona()` function; the prompt-resolution chain this epic must not touch.
- `internal/personas/list.go:53` — the `ListTiers()` function; template for enumerating installed personas for drift reporting.
- `cmd/atcr/init.go:96` — `installCommunityPersonas()`, the shared installer called from two sites.
- `cmd/atcr/init.go:127` — the roster loop over `builtins.Names()` that produces the disjoint 9-vs-10 roster; the skip-warning fires at `init.go:129`.
- `internal/ghaction/client.go:39` — `retriableStatus()`, a second independent per-package retry client, confirming the per-package convention.

## Quick Reference

| File | Line | Symbol | Reuse For |
|------|------|--------|-----------|
| internal/personas/client.go | 99 | fetch() | Template for OpenRouter catalog client's retry/backoff/timeout/size-cap GET logic |
| internal/personas/upgrade.go | 27 | Upgrade() | Insertion point for the resolver call, before existing isNewer/write logic |
| internal/personas/upgrade.go | 89 | isNewer() | Structural template for major-bump re-validation gate (semver.Major comparison) |
| internal/personas/search.go | 14 | PersonaIndexEntry | Struct to extend additively (omitempty) with the new logical binding field(s) |
| internal/registry/persona.go | 47 | ResolvePersona() | Boundary — must NOT be touched by resolver/lock logic |
| internal/personas/list.go | 53 | ListTiers() | Template for enumerating installed personas for `atcr models check` (AC5) |
| cmd/atcr/init.go | 96 | installCommunityPersonas() | Shared installer; single fix point for AC7's disjoint-roster bug |
| cmd/atcr/init.go | 127 | roster loop | Where builtins.Names() vs. fetched index disjoint roster (TD-011) originates |
| internal/ghaction/client.go | 39 | retriableStatus() | Confirms per-package retry helpers are this codebase's convention, not shared |
| cmd/atcr/main.go | 202 | newRootCmd AddCommand | Register `newModelsCmd()` alongside `newPersonasCmd()` |
| internal/personas/unit.go | 95 | writePersonaUnit() | Shared paired-write tail; persisted lock (if any) flows through here |
| internal/registry/config.go | — | AgentConfig.Model | Downstream consumer of the resolved lock at review time |
| internal/fanout/review.go | — | renderAgent | Builds the wire invocation from AgentConfig.Model |
| personas/community_test.go | — | TestCommunityIndex_Registration | 19.6 AC7 gate: a new lock field must not break YAML↔index agreement |
| internal/personas/search_test.go | — | TestCommunityIndex_ProviderModelMatchesYAML / TestVerifyCommunityIndex_FailsOnMismatch | 19.6 AC7 YAML↔index match gates |
| internal/personas/search_test.go | — | TestPersonaIndexEntry_UnknownKeysIgnored | Back-compat test template for permissive decode of new lock fields |

## Related Documentation

- codebase-discovery.json (this plan's generated discovery file)
- internal/personas/client.go, internal/personas/upgrade.go, internal/personas/search.go, internal/registry/persona.go, internal/personas/list.go, cmd/atcr/init.go
