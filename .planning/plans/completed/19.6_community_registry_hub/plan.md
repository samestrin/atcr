# Plan 19.6: Community-Canonical Model-Indexed Personas

## Metadata
- **Last Modified:** 2026-07-07 10:58:29
- **Plan Type:** feature
- **Plan Number:** 19.6
- **Status:** Draft - Design Refined

## Plan Overview
**Plan Type:** feature
**Plan Goal:** Make the in-repo community-persona channel (fetched from `samestrin/atcr`, not compiled into the binary) the canonical source of reviewer personas, add structured `provider`/`model` metadata so a user can discover a persona by the model they already have, and ship a human-named, model-indexed persona library covering both frontier providers and strong flat-rate open models — paired with onboarding docs that lead with the monetizing Synthetic path and keep frontier personas opt-in.
**Target Users:** New atcr users picking a first persona pack by the model they hold an API key for; existing users upgrading pinned community personas; persona contributors authoring model-tuned content; the maintainer curating the in-repo library and onboarding funnel.
**Framework/Technology:** Go (stdlib `net/http`/`encoding/json` + `spf13/cobra` CLI), the existing `internal/personas` fetch/install/search/upgrade package, `internal/quickstart` (reference-only), Markdown persona prompt templates under `personas/`.

## Documentation References
- **[CRITICAL]** [Community Persona Fetch & Distribution (net/http + YAML)](documentation/fetch-and-distribution.md) — repointing `RegistryBaseURL`, fetch-and-pin logic, `--offline` fallback, backward compatibility for existing `.atcr/personas/` workspaces, and the `index.json` generation workflow
- **[CRITICAL]** [CLI Flag Wiring for Model-Aware Search (Cobra)](documentation/cli-search-flags.md) — `--model`/`--provider` flags on `atcr personas search`
- **[IMPORTANT]** [Persona YAML Schema & Struct Tags](documentation/persona-yaml-schema.md) — `yaml.v3` struct tags and strict decoding for persona authoring
- **[IMPORTANT]** [Testing Patterns: testify + httptest Mock Registry](documentation/testing-mock-registry.md) — mock-registry test patterns for AC6/AC7
- **[IMPORTANT]** [Human-Names Migration for Built-in Stragglers](documentation/human-names-migration.md) — migrating `sentinel`/`tracer`/`idiomatic` to `sasha`/`penny`/`ingrid` and enforcing the all-human-names convention
- **[IMPORTANT]** [Onboarding Hierarchy and Discover-by-Model Flow](documentation/onboarding-hierarchy.md) — leading with `atcr quickstart` (Synthetic), documenting the provider onboarding hierarchy, and the discover-install-test-by-model flow
- Full index: [documentation/README.md](documentation/README.md)

## Objectives
1. Repoint the default community-persona fetch URL to `samestrin/atcr` and make the fetched community layer the canonical source of model-specific personas, with fetch-and-pin versioning, `--offline` behavior, and backward compatibility for existing on-disk personas.
2. Extend the persona index schema and search with structured `provider`/`model` metadata so users can discover a persona by the model they already hold an API key for.
3. Author and publish a model-indexed, human-named persona library in the product repo, covering both frontier providers and strong flat-rate open models, with each persona bound to a specific `provider`+`model` and phrased per that model's vendor prompting guide.
4. Migrate the remaining role-named built-in personas (`sentinel`, `tracer`, `idiomatic`) to human names (`sasha`, `penny`, `ingrid`) in the community layout, eliminating all role-based names from the active persona set.
5. Update onboarding documentation to lead with the monetizing Synthetic `atcr quickstart` path, document DashScope as a secondary flat-rate option, reference Chutes and Featherless with caveats, note LiteLLM as an advanced proxy, and position frontier/majors personas as opt-in "bring your own key."
6. Enforce the model-in-structured-metadata and all-human-names conventions through the fixture test and persona authoring documentation.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 7 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Generated

## Feature Analysis Summary
Epic 9.0 already built the full community-persona distribution mechanism (`atcr personas install/search/list/upgrade/remove`, fetch against an injectable `HTTPClient`, `PersonaIndexEntry`-driven search) — this plan does not rebuild that plumbing, it repoints and extends it. `internal/personas/client.go`'s `RegistryBaseURL` is a single hardcoded constant pointing at `atcr/personas`; repointing it to `samestrin/atcr` plus adding version-pin tracking and offline behavior satisfies AC1 with a small, well-isolated change. `PersonaIndexEntry` (`internal/personas/search.go`) currently carries only `Name`/`Version`/`Description`/`Path` and `Search()` does substring matching on two of those fields — AC2's model-aware discovery is additive (`encoding/json` ignores unknown fields, so old `index.json` payloads keep working). The heaviest work is content: AC3 requires authoring a full model-indexed persona library (prompt + YAML + fixture per persona, each phrased to that model's own vendor prompting guide) and migrating the three human-name stragglers (`sentinel`→`sasha`, `tracer`→`penny`, `idiomatic`→`ingrid`) into the same layout, folding in Epic 23.0 so there is never a mixed naming state. AC5's onboarding-hierarchy docs are a straightforward but detail-heavy rewrite of `README.md`'s Quickstart section and the persona docs to rank Synthetic > DashScope > Chutes/Featherless > LiteLLM(advanced) > majors(opt-in).

## Technical Planning Notes
- **Fetch URL repoint is a one-constant change with a wide blast radius**: `internal/personas/client.go:24`'s `RegistryBaseURL` is the single choke point every subcommand (install/search/list/upgrade) reads through `BaseURL()` — no other file needs to change for the URL itself, but the fetch-and-pin version-tracking logic (AC1) and `--offline` stub need new code in the same package.
- **`PersonaIndexEntry` extension is backward-compatible by construction**: `encoding/json.Unmarshal` silently ignores unknown fields, so adding `Provider`/`Model` (+`Tasks`/`Tags`) to the struct and to newly-generated `index.json` entries does not break existing on-disk personas or older index payloads (AC1's backward-compatibility requirement).
- **The persona-authoring contract already enforces most of what AC3/AC7 need**: `docs/personas-authoring.md` already requires `provider`+`model` as strictly-validated required fields and a fixture proving the target category — AC2/AC7's "bound model in structured metadata, asserted by the fixture test" is an incremental addition to an existing contract and test, not new machinery.
- **Quickstart's round-robin binding (`internal/quickstart/manifest.go`) is explicitly out of scope** — the model-indexed library is discovered/installed via `atcr personas search`/`install`, a separate mechanism from quickstart's roster generation; do not conflate the two.
- **Live fetch against the real URL cannot be exercised until `samestrin/atcr` is public** — every AC1/AC2/AC6 test must run against `httptest.NewServer` with `ATCR_PERSONAS_URL` overridden, following the exact pattern `internal/personas/client.go` already uses in its existing test suite.
- **Persona resolution is locked by Clarification C1/C2/C3 (original-requirements.md) and owned by AC 01-06** — community personas may carry custom Markdown prompts that MUST resolve via a **single** `ResolvePersona` precedence chain; the prompt travels with the persona as one self-contained unit (no second delivery path); the two-format/two-directory split **converges** rather than deepens. `/design-sprint` owns only the exact precedence ordering and whether built-in `.md` reformatting lands here or in a fast-follow — **not** whether custom prompts resolve.

## Implementation Strategy
Work decomposes into four largely independent tracks that share one integration point (the extended `PersonaIndexEntry` schema): (1) distribution — repoint `RegistryBaseURL`, add fetch-and-pin version tracking and `--offline` behavior in `internal/personas/client.go`; (2) discovery — extend `PersonaIndexEntry` and `Search()` in `internal/personas/search.go`, then wire `--model`/`--provider` flags onto `cmd/atcr/personas.go`'s `newPersonasSearchCmd`; (3) content — author each model-indexed persona (YAML + Markdown prompt + fixture) per `docs/personas-authoring.md`, migrate the three stragglers into the same human-named layout, and populate the in-repo community-persona index; (4) docs — rewrite `README.md`'s Quickstart and `docs/personas-install.md`/`docs/personas-authoring.md` per the onboarding hierarchy and the new structured-metadata/human-names conventions. Given the component count (6) and task count (~17) both exceed `/execute-epic`'s scope guard, this plan proceeds through the full sprint pipeline (`/create-user-stories` → `/create-acceptance-criteria` → `/design-sprint` → `/create-sprint`) rather than the lightweight one-shot path, and `/design-sprint` should evaluate whether tracks 1-2 (code) and track 3 (content) warrant separate phases given their different risk profiles (schema/network code vs. prompt-authoring judgment calls).

## Recommended Packages
No high-ROI packages identified — this plan is pure Go stdlib (`net/http`, `encoding/json`) plus existing dependencies (`spf13/cobra` already used for all CLI wiring) and content authoring (Markdown prompt templates, YAML persona files). No new dependency is warranted.

## User Story Themes

### Theme 1 — Community-Canonical Fetch-and-Pin Distribution
Repoint the default community-persona fetch URL from `atcr/personas` to `samestrin/atcr` (+ in-repo path), add version-pin tracking so `init`/`quickstart` fetch-and-pin reproducibly and `atcr personas upgrade` advances the pin, define `--offline` stub behavior, and preserve backward compatibility for users with existing on-disk personas. (AC1)

### Theme 2 — Structured Model Metadata Schema
Add `provider`/`model` (and `tasks`/`tags` as warranted) to the `PersonaIndexEntry` struct and the `index.json` entry generation, keeping the change additive/backward-compatible with existing index payloads. (AC2, half of AC7)

### Theme 3 — Model-Aware Search and Discovery
Extend `Search()` to match structured `provider`/`model` fields and add `--model`/`--provider` filters to `atcr personas search`, so "I have model X → find its persona" resolves from structured data, never from a model name embedded in free text. (AC2, AC6)

### Theme 4 — Model-Indexed Persona Library Authoring
Author each library persona (YAML + Markdown prompt + fixture, per `docs/personas-authoring.md`) bound to a specific `provider`+`model`, phrased per that model's own vendor prompting guide, covering both frontier providers (Claude/GPT/Gemini) and strong flat-rate open models (DeepSeek/Qwen/Kimi/GLM/…), and add each to the in-repo community-persona index. (AC3, AC6)

### Theme 5 — Human-Names Migration for Built-in Stragglers
Migrate `sentinel`→`sasha`, `tracer`→`penny`, `idiomatic`→`ingrid` into the same in-repo community layout as the new library, generalizing `ingrid` beyond Go, so no role-based names remain anywhere in the active persona set. Reconciles with Epic 23.0 so the rename is not double-implemented. (AC4)

### Theme 6 — Authoring Contract Enforcement
Update `docs/personas-authoring.md` to document the model-in-structured-metadata convention and the all-human-names convention as forward-looking rules, and extend the fixture test to assert the bound model appears in structured metadata for every persona. (AC7, AC8)

### Theme 7 — Onboarding-Hierarchy Documentation
Rewrite `README.md`'s Quickstart and the persona docs to lead with `atcr quickstart` (Synthetic), document DashScope as a secondary flat-rate option, reference Chutes then Featherless with their caveats, note LiteLLM as an advanced aggregation proxy, position frontier/majors personas as opt-in "bring your own key," and document the discover-and-install-by-model flow end to end. (AC5)

## Planning Success Criteria
- Default fetch URL points at `samestrin/atcr`; fetch-and-pin, offline stub, and backward compatibility are all verified against a mock registry (AC1, AC6)
- `atcr personas search` finds a persona by its bound model from structured `index.json` data, not free-text (AC2)
- A model-indexed, human-named persona library exists with passing fixtures and no role-based names remain anywhere in the active set (AC3, AC4)
- `go test ./...` passes with the authoring contract enforced by the fixture test (AC7)
- `README.md` and persona docs lead with the monetizing Synthetic path and position frontier personas as opt-in (AC5, AC8)

## Risk Mitigation
- **Risk:** Live install against the real `samestrin/atcr` URL cannot be tested until the repo goes public. **Mitigation:** every test exercises the existing `httptest.NewServer` + `ATCR_PERSONAS_URL` override pattern already proven in `internal/personas`'s test suite (AC6 explicitly scopes end-to-end verification to a mock/local registry).
- **Risk:** Authoring ~8+ model-tuned personas (frontier + flat-rate) each requiring genuine research into that model's own vendor prompting guide is a large, judgment-heavy content workload that could stall a linear TDD sprint. **Mitigation:** `/design-sprint` should isolate persona-content authoring (Theme 4/5) into its own phase(s), separate from the schema/network code (Themes 1-3), so content review cadence doesn't block code merge cadence.
- **Risk:** Extending `PersonaIndexEntry` and `index.json` generation could silently break existing installed personas or older index payloads if the change isn't additive. **Mitigation:** rely on `encoding/json`'s unknown-field-ignoring default (already the existing behavior) and add an explicit backward-compatibility test asserting an old-shape `index.json` still parses and searches correctly.

## Next Steps
1. `/find-documentation @.planning/plans/active/19.6_community_registry_hub/`
2. `/create-documentation @.planning/plans/active/19.6_community_registry_hub/`
3. `/create-user-stories @.planning/plans/active/19.6_community_registry_hub/`
4. `/create-acceptance-criteria @.planning/plans/active/19.6_community_registry_hub/`
5. `/design-sprint @.planning/plans/active/19.6_community_registry_hub/`
6. `/create-sprint @.planning/plans/active/19.6_community_registry_hub/`
