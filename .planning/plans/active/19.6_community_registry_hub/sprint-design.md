# Sprint Design: Community-Canonical Model-Indexed Personas

**Created:** July 07, 2026 12:14:28PM
**Plan:** [Plan 19.6: Community-Canonical Model-Indexed Personas](/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.6_community_registry_hub/)
**Plan Type:** Feature
**Status:** Design Complete

---

## Original User Request

> Make the community persona channel the canonical source of reviewer personas — fetched from the product repo (`samestrin/atcr`) rather than compiled into the binary — and ship a model-indexed, human-named persona library that a user can discover by the model they already have ("I have DeepSeek → find the DeepSeek persona → install it"). Pair this with onboarding documentation that leads first-time setup with the monetizing flat-rate path (`atcr quickstart` → Synthetic) and treats frontier-provider personas as opt-in, out of the default funnel.

**Referenced Resources:**

- [Community Persona Fetch & Distribution](documentation/fetch-and-distribution.md)
  - **Summary:** Covers the injectable-`HTTPClient` fetch pattern, the one-constant `RegistryBaseURL` repoint, fetch-and-pin/`--offline` behavior for AC1, and the C1/C2/C3 custom-prompt convergence and resolution model.
  - **Key Points:** `BaseURL()`'s env-override-else-constant pattern is untouched; `PersonaIndexEntry` is additive/backward-compatible by construction; custom prompts must resolve via a single `ResolvePersona` precedence chain, not a second delivery path.
- [CLI Flag Wiring for Model-Aware Search](documentation/cli-search-flags.md)
  - **Summary:** Documents the Cobra flag-registration pattern for adding `--model`/`--provider` to `atcr personas search`.
  - **Key Points:** Follow the existing `--scores`-on-`newPersonasListCmd` pattern; `RunE` returns errors, never calls `os.Exit`; output routes through `cmd.OutOrStdout()`.
- [Persona YAML Schema & Struct Tags](documentation/persona-yaml-schema.md)
  - **Summary:** `gopkg.in/yaml.v3` struct-tag conventions for extending `PersonaIndexEntry`, and the strict-vs-permissive decode split between the persona loader and the index reader.
  - **Key Points:** `Decoder.KnownFields(true)` stays on the persona-loading path only; the index-entry decoder stays permissive so `Provider`/`Model`/`Tasks`/`Tags` are additive.
- [Testing Patterns: testify + httptest Mock Registry](documentation/testing-mock-registry.md)
  - **Summary:** The `httptest.NewServer` + `ATCR_PERSONAS_URL` override pattern every AC1/AC2/AC6 test must reuse, plus table-driven `t.Run` + testify `assert`/`require` conventions.
  - **Key Points:** Zero live network calls in CI; `internal/personas/search_test.go` is new and extends the existing `personas_test.go` table-driven style.
- [Human-Names Migration for Built-in Stragglers](documentation/human-names-migration.md)
  - **Summary:** The `sentinel→sasha`, `tracer→penny`, `idiomatic→ingrid` mapping and the atomic four-part rename checklist (template, fixture, YAML, registration).
  - **Key Points:** No mixed-naming state is allowed at any point; `ingrid` generalizes beyond Go; folds in Epic 23.0 so that epic is superseded, not re-implemented.
- [Onboarding Hierarchy and Discover-by-Model Flow](documentation/onboarding-hierarchy.md)
  - **Summary:** The locked 5-tier onboarding order (Synthetic → DashScope → Chutes/Featherless → LiteLLM → frontier opt-in) and the exact discover-install-verify bash flow.
  - **Key Points:** Pure documentation change to `README.md`/`docs/personas-install.md`/`docs/personas-authoring.md`; no CLI behavior change beyond documenting what Themes 2/3/4 ship.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Community Registry Hub
**Complexity:** 11/12 (VERY COMPLEX)
**Timeline:** 15 days
**Phases:** 7
**Pattern:** Research & Spike → Foundation → Core Resolution → Discovery → Content Authoring → Contract & Docs → Integration & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go HTTP client fetch injectable pattern
YAML struct tag additive schema extension
Cobra CLI flag registration pattern
persona resolution precedence chain design
untrusted fetched prompt security guardrails
```

---

## Complexity Breakdown

- **Architecture:** 3/3 - Introduces a genuinely new resolution architecture (single `ResolvePersona` precedence chain across 3 sources), converges two existing formats into one self-contained persona unit, and adds an untrusted-input security boundary (length cap + hard fixture gate) around fetched prompts — this is beyond "new patterns," it changes how personas are delivered and resolved system-wide.
- **Integration:** 3/3 - Touches 6+ files/packages across 3 layers: `internal/personas/*` (client, search, install, list, upgrade, test), `cmd/atcr/*` (personas.go, init.go, quickstart.go), `personas/*` (registry + content), plus `docs/` and `README.md`.
- **Story/Task & Test:** 3/3 - 7 stories, 29 linked ACs (+1 orphaned, see Risks) spanning unit, integration, and one mock-registry E2E test; one XL story requiring genuine per-model vendor-guidance research across 7+ personas.
- **Risk/Unknowns:** 2/3 - The highest-risk architectural questions (custom-prompt resolution, unit convergence, untrusted-input guardrails) are already locked by Clarifications C1/C2/C3, leaving mostly well-scoped implementation risk (live-URL testability, content-authoring judgment calls) rather than open-ended unknowns.

**Time Formula:** Σ per-phase estimates (each phase sized by story effort + AC count + new-architecture surcharge for phases touching the resolution chain)
**Calculation:** 1 + 1.5 + 3.5 + 1.5 + 5 + 1.5 + 1 = 15 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** strong (complexity 11/12)
**Suggested command:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.6_community_registry_hub/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Research & Spike — Resolution Chain Design (1 day)
**Focus:** Locate the current review-time persona-to-prompt resolution call site (the codebase-discovery snapshot covers install/search/list/upgrade but not where `AgentConfig.Persona` is turned into prompt text today), confirm the concrete `ResolvePersona` function signature, decide the self-contained unit's on-disk shape (inline YAML field vs. co-located file), and confirm the exact precedence ordering and length-cap constant to mirror.
**Items:** Design spike only — no shipped code. Output: a short design note (interface signature + precedence order + cap value) that Phase 3 implements against.
**Design decision locked here:** Precedence order = project `.atcr/personas` override > pinned community (`~/.config/atcr/personas`) > embedded built-in (the C2-recommended default; no reason found in codebase discovery to deviate). Built-in `.md` reformatting into the new unit format is **deferred to a bounded fast-follow** — built-ins resolve through the same `ResolvePersona` chain as embedded copies of the unit format (via a thin adapter, not a physical file-format rewrite) so no divergent second format is introduced, while avoiding a 9-persona reformat inside an already-XL sprint.

### Phase 2: Foundation — Schema Extension + Registry Repoint (1.5 days)
**Focus:** Land the additive `PersonaIndexEntry` schema extension and the one-constant URL repoint — the two changes every other phase depends on.
**Items:** Story 2 (02-01, 02-02, 02-03), Story 1 AC 01-01.
**Test types:** Unit (struct tags, index population, old-shape backward-compat decode).

### Phase 3: Core Resolution — Fetch-and-Pin + ResolvePersona Chain (3.5 days)
**Focus:** The heaviest code phase — implement fetch-and-pin in `init`/`quickstart`, the `--offline` fallback, and the new single-precedence-chain resolver with untrusted-input guardrails (length cap, hard fixture gate, pin-for-reproducibility).
**Items:** Story 1 remainder (01-02, 01-03, 01-04, 01-05, 01-06).
**Test types:** Integration (mock-registry fetch-and-pin, offline fallback, fetch-failure error path) + Unit (precedence-chain ordering, length-cap rejection) + E2E (existing-workspace preservation, source labeling).

### Phase 4: Discovery — Model-Aware Search (1.5 days)
**Focus:** Structured `--model`/`--provider` filtering with zero free-text fallback, backward-compatible keyword search, flag/arg validation.
**Items:** Story 3 (03-01, 03-02, 03-03). **Resolve the orphaned `03-04` AC file first (see Risks) before or during this phase** so search-table column rendering has a linked AC.
**Test types:** Integration (flag registration, table rendering) + Unit (structured-field-only matching, near-miss substring cases).

### Phase 5: Content Authoring — Persona Library + Human-Names Migration (5 days)
**Focus:** Isolated from the schema/network code per the plan's own risk mitigation — content review cadence (genuine vendor-guidance research) must not block code merge cadence. Runs after Phase 2 (schema) and coordinates with Phase 3's resolution chain for prompt delivery.
**Items:** Story 4 (04-01 through 04-07: 3 frontier flagship+fallback pairs, 4 flat-rate open models, prompt-structure compliance, fixtures, index registration, schema/naming compliance, task-scoping differentiation) + Story 5 (05-01 through 05-04: atomic `sentinel→sasha`/`tracer→penny` rename, `ingrid` generalization, retired-slug verification, doc updates).
**Test types:** Unit (schema validation, fixture pass, naming compliance) + Integration (retired-slug repo-wide verification scoped to persona paths).

### Phase 6: Contract Enforcement + Onboarding Docs (1.5 days)
**Focus:** Close the two remaining documentation/enforcement gaps and rewrite onboarding docs — sequenced after Phases 4 and 5 so cited flags/persona names are accurate.
**Items:** Story 6 (06-01, 06-02, 06-03) + Story 7 (07-01, 07-02, 07-03).
**Test types:** Unit (fixture test asserts bound-model metadata) + Manual (doc-content review against `documentation/onboarding-hierarchy.md`'s locked tier language).

### Phase 7: Integration & Validation (1 day)
**Focus:** Full-suite pass and cross-story integration proof.
**Items:** `go test ./...`, `golangci-lint run`, `go vet ./...`, `go fmt ./...`; end-to-end mock-registry discover-by-model flow (search → install → list → test) exercising the full AC6 flow; confirm Story 4's authored personas' custom prompts actually resolve via Story 1/3's `ResolvePersona` chain (cross-phase integration check, not just per-story unit tests).
**Test types:** Full suite + E2E.

---

## Work Decomposition

| Story | Theme | Effort | ACs | Phase |
|-------|-------|--------|-----|-------|
| 1: Community-Canonical Fetch-and-Pin Distribution | Distribution + Resolution | M | 01-01..01-06 (6) | 2 (01-01), 3 (01-02..01-06) |
| 2: Structured Model Metadata Schema | Schema | S | 02-01..02-03 (3) | 2 |
| 3: Model-Aware Search and Discovery | Discovery | M | 03-01..03-03 (3, +orphaned 03-04) | 4 |
| 4: Model-Indexed Persona Library Authoring | Content | XL | 04-01..04-07 (7) | 5 |
| 5: Human-Names Migration for Built-in Stragglers | Content | M | 05-01..05-04 (4) | 5 |
| 6: Authoring Contract Enforcement | Docs/Enforcement | S | 06-01..06-03 (3) | 6 |
| 7: Onboarding-Hierarchy Documentation | Docs | M | 07-01..07-03 (3) | 6 |

**Dependency graph:** Story 2 → Story 3; Story 2 → Story 4 (schema needed to register); Story 1 ↔ Story 4/5 (resolution chain is the delivery mechanism for both new and migrated personas); Story 1 + Story 4 → Story 6 (enforcement needs real personas to assert against); Story 3 + Story 4 → Story 7 (docs cite real flags/names). This is a strict topological ordering, not story-per-phase — Phases 2-6 above reflect it.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files in the same package as the code under test (Go convention, per `.planning/specifications/coding-standards.md`).
**Test File Placement Examples:**
- `internal/personas/search_test.go` (new) — Provider/Model/Tasks/Tags filtering, backward-compat old-shape decode
- `internal/personas/client_test.go` (extend) — repointed URL, fetch-and-pin, offline fallback
- `internal/personas/test.go`'s existing test file (extend) — fixture assertion for bound-model metadata
- `cmd/atcr/personas_test.go` (extend) — `--model`/`--provider` flag registration, `renderPersonaSearch` column output
- `cmd/atcr/init_test.go` / `cmd/atcr/quickstart_test.go` (extend) — fetch-and-pin end-to-end against mock registry

**Unit/Integration/E2E:** Unit tests use `go test` (`testing` package) + `testify/assert`/`require`, table-driven with `t.Run` subtests. Integration tests exercise `httptest.NewServer` with `ATCR_PERSONAS_URL` overridden — zero live network calls. The one E2E test (AC 01-05/AC6) drives the full discover-search-install-list-test flow against the mock registry. Coverage baseline: 80% (`go test -coverprofile=coverage.out ./...`, per project config).

**Test Environment Status:**
- Framework: `testing` + `testify` — established, already in use across `internal/personas/*_test.go`
- Execution: `go test ./...` — configured, gated by the `Go CI` GitHub Actions workflow
- Coverage Tools: `go test -coverprofile=coverage.out ./...`, baseline 80% — established

---

## Architecture

**Primitives:**
- `PersonaIndexEntry` (extended: `Name`/`Version`/`Description`/`Path` + new `Provider`/`Model`/`Tasks`/`Tags`, all with `omitempty`)
- Persona unit (a single installable artifact carrying binding metadata + its prompt, inline or co-located — the C2 convergence target)
- `ResolvePersona` result (winning source + resolved prompt text, deterministic across the 3-source precedence chain)
- `FixtureOutcome` (existing, extended to assert bound-model metadata for community personas)

**Module Boundaries:**
- `internal/personas` stays the sole black-box owner of persona acquisition, resolution, and validation (fetch/install/search/list/upgrade/test/resolve) — `cmd/atcr` remains thin `RunE` delegation with no persona logic of its own.
- `ResolvePersona` is exposed as a single documented function so review-time callers depend only on its signature, never on which of the 3 sources won — new sources can be added later without a caller-side change.

**External Dependencies:**
- `net/http` — already wrapped via the `HTTPClient` interface in `internal/personas/client.go`; no new wrapper needed.
- `gopkg.in/yaml.v3` — already the project's YAML library; strict (`KnownFields(true)`) on the persona-load path, permissive on the index-entry path.
- `spf13/cobra` — existing CLI framework; `--model`/`--provider` follow the established `Flags().String(...)` pattern.

**Replaceability:** Because `ResolvePersona` is a single seam, the entire resolution mechanism (built-in embed, community fetch-and-pin, project override) can be swapped or extended without touching `cmd/atcr` or `internal/personas`' public API surface.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| Fetched custom persona prompts | `internal/personas` install + `ResolvePersona` | Prompt injection via a malicious/compromised community persona; oversized-prompt DoS; leftover `{{ }}` template injection | Length cap mirroring `MaxExecutorSystemPromptLen` (`internal/registry/config.go`); hard fixture gate before ship/resolve; strict YAML decode (`KnownFields(true)`) on the persona-load path |
| `index.json` / YAML source drift | `personas/community/index.json` generation | Mismatched `provider`/`model` claims mislead a user into installing a wrongly-tuned persona (integrity, not injection) | Generation script + CI validation so the index is always reproducible from YAML sources |
| Community registry fetch endpoint | `internal/personas/client.go` (`RegistryBaseURL`, `BaseURL`) | MITM if not HTTPS; malicious `index.json` exploiting the decode path | HTTPS-only raw-content URL; `encoding/json`'s safe unknown-field tolerance; no code execution from fetched content at any point |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|-----------------|--------|----------|
| Persona fetch during `init`/`quickstart` | Single-user CLI, sequential index + N persona YAML fetches, once per onboarding | Onboarding stays responsive; existing retry/backoff (429/500/502/503/504, ~500ms initial, 1.5x backoff) already covers transient failures | Reuse the existing retry policy; keep transport timeout (`http.Client{Timeout}`) distinct from the operation-level context deadline |
| `ResolvePersona` chain lookup | Once per agent per `atcr review` invocation | Negligible added latency — no network calls | Resolve locally over already-installed content only, per Clarification C1's explicit no-added-network-calls requirement |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Name collisions | Same name present as built-in, installed community, and project override simultaneously | Exactly one source wins per the documented precedence chain, deterministically — no panic, no ambiguous double-load |
| Old-shape `index.json` | Index generated before the schema extension lands | Decodes cleanly against the extended struct with zero-value new fields, no decode error |
| Partial straggler rename | Template/fixture renamed but `personas/personas.go`'s `names` slice left stale | Existing init-time panic guard fails fast at binary startup — not a silent degradation |
| Oversized or fixture-failing custom prompt | A fetched persona's prompt exceeds the length cap, or fails its render/category fixture | Rejected at load/install time with a descriptive error; never silently truncated, never resolves |
| Existing hand-edited `.atcr/personas/` workspace | Rerun `init --force` against a workspace with a modified `.md` file | File is byte-for-byte unchanged after the rerun; missing community personas install alongside it |
| Free-text vs. structured `--model` match | A persona's `Description` mentions a model name that differs from its structured `Model` field | Not returned when `--model` is used — structured field only, no free-text fallback |

### Defensive Measures Required

- **Input Validation:** Persona prompt length cap (mirrors `MaxExecutorSystemPromptLen`); strict YAML decode (`KnownFields(true)`) on the persona-load path only; permissive decode on the index-entry path so it stays additive.
- **Error Handling:** Descriptive, non-zero-exit errors on fetch failure (no silent fallback) unless `--offline` is passed; wrap errors with `fmt.Errorf("...: %w", err)` per `.planning/specifications/coding-standards.md`.
- **Logging/Audit:** No dedicated audit log requirement identified beyond existing CLI stderr messaging.
- **Rate Limiting:** N/A — single-user CLI; existing retry/backoff policy for transient registry-fetch failures is sufficient.
- **Graceful Degradation:** `--offline` flag skips the community fetch entirely and falls back to embedded built-ins with zero network calls.

---

## Risks

**Technical:**
- Live install against the real `samestrin/atcr` URL is untestable until the repo is public → every test uses the existing `httptest.NewServer`/`ATCR_PERSONAS_URL` pattern; AC6 explicitly scopes verification to a mock/local registry.
- Extending `PersonaIndexEntry`/`index.json` could silently break existing installed personas if not additive → rely on `encoding/json`'s unknown-field tolerance plus an explicit backward-compatibility decode test (AC 02-03).
- The current review-time persona-to-prompt resolution call site is not fully mapped by the codebase-discovery snapshot (which covers install/search/list/upgrade but not the `AgentConfig.Persona`-to-prompt-text path) → addressed by Phase 1's dedicated Research & Spike phase before any resolution-chain code is written.
- **AC file `03-04-search-table-provider-model-columns.md` exists on disk but is no longer referenced in Story 3's AC table** (dropped during the 2026-07-07 clarification-propagation edit, commit `958d6302`) → flagged for the user to resolve via `/refine-user-stories` or a manual re-link before `/create-sprint`; Phase 4 above treats it as pending resolution rather than silently ignoring it.

**TDD-Specific:**
- Authoring 8+ genuinely model-tuned personas (real vendor-guidance research, not templated substitution) is a large, judgment-heavy content workload that could stall a linear TDD sprint → isolated into its own Phase 5, separate from the schema/network code, so content review cadence doesn't block code merge cadence (per plan.md's own risk mitigation).
- Extending `TemplateFixtureRunner` for community personas could accidentally weaken the existing built-in fixture pass/fail contract → keep the `isBuiltin(name)` branch fully separate; add the community-specific model-metadata assertion as an additive path only (Phase 6, AC 06-03).
- A persona's fixture category word could leak in only from the injected diff rather than being authored into the prompt template itself, silently passing the fixture test without proving intent → manual per-persona verification during Phase 5 authoring, not just the automated pass (per `docs/personas-authoring.md`'s documented test behavior).

---

**Next:** `/create-sprint @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/19.6_community_registry_hub/ --gated`
