# Sprint Design: Plan 29.0: Anti-Slop Persona (Simon) & Content Marketing

**Created:** July 16, 2026 09:51:05PM
**Plan:** [Plan 29.0: Anti-Slop Persona (Simon) & Content Marketing](plan.md)
**Plan Type:** Feature ✨
**Status:** Design Complete

---

## Original User Request

> Create a specialized, human-named persona (`simon`) designed exclusively to hunt down, flag, and strip out AI-generated code bloat (slop). Accompany this persona with a targeted blog post outline to use as a top-of-funnel marketing asset capitalizing on the growing industry frustration with LLM over-engineering. ATCR's multi-agent architecture is perfectly positioned to solve this — shipping a "Pre-Cooked Anti-Slop Agent" gives engineering teams a one-command solution to automatically catch and trim AI-authored bloat during CI.

**Referenced Resources:**
- [persona-yaml-and-prompt-authoring.md](documentation/persona-yaml-and-prompt-authoring.md)
  - **Summary**: Defines the `simon.yaml`/`simon.md` authoring contract — yaml.v3 strict-schema agent binding and the text/template prompt-rendering pattern every community persona follows.
  - **Key Points**: Strict `KnownFields(true)` decode with a fixed recognized-key set; prompt template restricted to 8 allow-listed bare tokens plus one `{{if .ToolsEnabled}}...{{end}}` block; single leading vendor-guidance citation comment.
- [test-gate-and-fixture-verification.md](documentation/test-gate-and-fixture-verification.md)
  - **Summary**: Explains how the `personas/community_test.go` roster and embedded-set gates use testify `assert`/`require` to verify a new persona's fixture, differentiation, category uniqueness, and index registration.
  - **Key Points**: `communityPersonas` roster (`Slug`/`VendorToken`/`Category`) drives `require.Len` parity checks that fail fatally on partial registration; `TestCommunityPersonas_Differentiation` enforces a 0.85 Jaccard similarity ceiling across all persona pairs.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Anti-Slop Persona Simon
**Complexity:** 4/12 (MODERATE)
**Timeline:** 4 days
**Phases:** 4
**Pattern:** Story 1: RGR → Story 2: RGR → Story 3: Content Refresh → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
community persona registry authoring pattern
Go test roster registration gate
persona index.json cross-file consistency
text/template prompt token allow-list
Jaccard similarity persona differentiation threshold
```

---

## Complexity Breakdown

- **Architecture:** 0/3 - Copies the existing `sonny.yaml`/`sonny.md` structural pattern verbatim; zero new Go code, no new registry mechanism, no new abstraction.
- **Integration:** 1/3 - All touch points (persona YAML+MD pair, fixture patch, Go test roster row, `index.json` entry) live inside one cohesive, already-proven subsystem (`personas/community/`); the blog outline edit is an independent, low-complexity content file with no code coupling.
- **Story/Task & Test:** 2/3 - 3 stories / 8 ACs with meaningful cross-file consistency gates (0.85 Jaccard differentiation ceiling, roster/index/fixture parity, verbatim category-word matching across 3 files).
- **Risk/Unknowns:** 1/3 - Minor, well-understood unknowns (category-word/task-tag collision, exact category word choice) that are explicitly pre-identified and mitigated in the plan's own risk tables.

**Time Formula:** 1 day per RGR-cycle story (Stories 1-2, each spanning persona authoring + registration) + 0.5 day for the content-only story (Story 3) + 1 day cumulative validation/adversarial pass, rounded up to the nearest whole day.
**Calculation:** 1 (Story 1) + 1 (Story 2) + 0.5 (Story 3) + 1 (Validation) = 3.5 → rounded up to 4 days.

---

## Recommended Flags

**Adversarial:** true
**Gated:** false
**Recommendation strength:** false
**Suggested command:** `/create-sprint @.planning/plans/active/29.0_anti_slop_persona/ --adversarial`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

This plan's raw complexity (4/12) sits under the adversarial complexity threshold, but its 4-phase structure meets the `phases >= 3` trigger — adversarial review is recommended primarily to catch the two highest-leverage, easy-to-miss defects this plan's own risk tables flag: a category-word/Jaccard collision, and a partial roster/index registration that fatally breaks the shared `personas` test package for any concurrent branch.

---

## Phase Structure

### Phase 1: Story 1 — Author the `simon` Persona Unit (RGR)
- **Duration:** 1 day
- **Items:** `personas/community/simon.yaml`, `personas/community/simon.md`
- **Focus:** Author the persona metadata binding and Go `text/template` prompt by editing direct copies of `sonny.yaml`/`sonny.md`. RED state is implicit — `simon` does not yet exist, so no `CommunityNames()` entry, no roster row. GREEN is reached when `simon.yaml` strict-decodes and `simon.md` passes `ValidateFetchedPersonaPrompt`, with the `## Focus` section hyper-focused on the four anti-slop targets and a new, unclaimed category word (`bloat`) embedded verbatim. Expected, documented gap: `TestCommunityAccessors` and `TestTemplateFixtureRunner_CommunityPersonasPass` go red the moment `simon.md` lands on disk (auto-discovered via `go:embed`) until Phase 2's registration closes the loop — this is intentional per AC 01-03, not a regression.
- **Covers:** AC 01-01, AC 01-02, AC 01-03

### Phase 2: Story 2 — Fixture Authoring & Test-Gate Integration (RGR)
- **Duration:** 1 day
- **Items:** `personas/community/testdata/simon_fixture.patch`, `personas/community_test.go` (`communityPersonas` roster row), `personas/community/index.json` (entry)
- **Focus:** Author the synthetic slop fixture, then land the roster row and `index.json` entry in the same atomic change (partial registration is never a passable intermediate state — it fails `require.Len` fatally for the whole `personas` package). This phase is what turns the Phase 1 red state green: `TestCommunityAccessors`, `TestCommunityPersonas_FixtureAndPromptCategory`, `TestCommunityPersonas_DistinctCategories`, `TestCommunityPersonas_DistinctTaskScoping`, `TestCommunityIndex_Registration`, and `TestTemplateFixtureRunner_CommunityPersonasPass` all pass with `simon` included.
- **Covers:** AC 02-01, AC 02-02, AC 02-03

### Phase 3: Story 3 — Blog Post Outline Verification & Refresh
- **Duration:** 0.5 day
- **Items:** `.planning/product/content/blog/slopfix-ai-code-bloat.md`
- **Focus:** Read-only verification pass against the Phase 1-2 shipped artifacts, followed by a scoped corrective edit: replace the invalid `atcr review --persona simon` CTA with the verified `atcr personas install simon` / `atcr personas test simon` commands, and reconcile any category-word or persona-behavior drift in sections 1, 3, and 4. No new authorship; sections 2 and the already-accurate hook/pitch/example structure are left untouched.
- **Covers:** AC 03-01, AC 03-02

### Phase 4: Validation & Adversarial Review
- **Duration:** 1 day
- **Items:** Full test-suite run, cross-file consistency re-check, risk-profile verification
- **Focus:** Run `go test ./personas/... ./internal/personas/... ./internal/registry/...` to confirm the complete 14-persona suite is green; manually run `atcr personas test simon` as the no-LLM structural smoke check; re-verify the Jaccard differentiation ceiling holds against all 13 existing personas; confirm the blog outline's grep-verifiable claims (no `--persona` flag references, at least one `atcr personas install/test simon` reference) per Story 3's Measurable criteria.
- **Covers:** All 8 ACs (regression confirmation), Definition of Done for the sprint

---

## Work Decomposition

### Story 1: Author the `simon` Persona Unit
**Testable elements:**
- `simon.yaml` strict-schema decode + non-placeholder provider/model binding → [AC 01-01](acceptance-criteria/01-01-simon-yaml-schema-binding.md) (Unit)
- `simon.md` canonical section order, template-token allow-list, and anti-slop Focus content → [AC 01-02](acceptance-criteria/01-02-simon-md-template-structure-focus.md) (Unit)
- Cross-file consistency of the `.yaml`/`.md` pair, auto-discovered by the existing registry test suite → [AC 01-03](acceptance-criteria/01-03-simon-authoring-contract-consistency.md) (Integration)

### Story 2: Fixture Authoring & Test-Gate Integration
**Testable elements:**
- `simon_fixture.patch` synthetic unified diff plants one unambiguous slop violation → [AC 02-01](acceptance-criteria/02-01-fixture-patch-authoring.md) (Unit)
- `communityPersonas` roster row (`Slug`/`VendorToken`/`Category`) → [AC 02-02](acceptance-criteria/02-02-community-roster-registration.md) (Unit)
- `index.json` entry parity with `simon.yaml` + full test-gate pass + manual CLI smoke → [AC 02-03](acceptance-criteria/02-03-index-registration-and-test-gate.md) (Integration + Manual E2E smoke)

### Story 3: Verify and Refresh the Blog Post Outline
**Testable elements:**
- Invalid CTA command replaced with verified `atcr personas install/test simon` → [AC 03-01](acceptance-criteria/03-01-cta-command-fix.md) (Manual + scripted grep)
- Category word and persona-behavior framing reconciled against shipped `simon.md` → [AC 03-02](acceptance-criteria/03-02-category-word-framing-alignment.md) (Manual + scripted grep/diff)

**Dependency order:** Story 1 → Story 2 (hard dependency: fixture/roster/index reference Story 1's shipped `simon.yaml`/`simon.md`) → Story 3 (hard dependency: reads Story 1-2's final shipped category word and CLI surface as source of truth). Stories 1 and 2 must merge to the default branch as a single unit — neither lands green in isolation.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files alongside source (standard Go convention; 359 existing test files in the repo follow this pattern) — no new test files are created by this plan; existing table-driven suites auto-discover `simon` via `go:embed`.

**Test File Placement Examples:**
- `personas/community_test.go` — modify only (append one `communityPersona` roster row); no new test file
- `internal/personas/community_fixture_test.go`, `internal/personas/test_test.go`, `internal/personas/community_schema_test.go`, `internal/registry/persona_test.go`, `internal/personas/search_test.go` — unmodified, auto-iterate `personas.CommunityNames()`

**Unit/Integration/E2E:**
- **Unit:** yaml.v3 strict-schema decode, no-placeholder-model, human-name regex, template-token allow-list, prompt-length cap — all auto-covered by existing table-driven tests once `simon.yaml`/`simon.md` exist (`go test ./internal/personas/... ./internal/registry/...`)
- **Integration:** roster/index/fixture cross-file parity, Jaccard differentiation ceiling, distinct-category and distinct-task-scoping gates (`go test ./personas/...`)
- **E2E:** No dedicated automated E2E. One manual no-LLM CLI smoke check: `atcr personas test simon` (exercises `TemplateFixtureRunner.RunFixture` with zero API-key/network cost)

**Test Environment Status:**
- Framework: `go test` + `testify/assert`/`require` — already established, in active use across 13 existing community personas
- Execution: `go test ./personas/... ./internal/personas/... ./internal/registry/...` (existing command, no new CI wiring required)
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (project-standard, 80% baseline per `.planning/.config/config.yaml`)

---

## Architecture

**Primitives:**
- `communityPersona{Slug, VendorToken, Category string}` — the hand-maintained Go roster row primitive (`personas/community_test.go:107`)
- `PersonaIndexEntry` — the JSON manifest primitive (`personas/community/index.json`), field-parity-checked against the YAML
- Persona YAML binding — `name`/`version`/`description`/`provider`/`model`/`persona`/`role`, strict-decoded via `yaml.v3`
- Go `text/template` prompt — 8 allow-listed bare tokens (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`, `{{.ToolsEnabled}}`) plus exactly one `{{if .ToolsEnabled}}...{{end}}` block

**Module Boundaries:**
- `personas/community.go` — the sole black-box boundary between on-disk persona files and the rest of the system, exposing `CommunityNames()`/`CommunityGet()`/`CommunityModel()`/`CommunityFixture()` over three `go:embed` directives (`community/*.md`, `community/*.yaml`, `community/testdata/*.patch`)
- `internal/registry` — the validation boundary (`ValidateCommunityPersonaYAML`, `ValidateFetchedPersonaPrompt`) that enforces the strict-schema and template-allow-list contracts before any persona reaches the review pipeline

**External Dependencies:**
- `gopkg.in/yaml.v3` — strict `KnownFields(true)` decode (already wrapped by `internal/registry`, no new dependency)
- `text/template` (stdlib) — prompt rendering (already wrapped, no new dependency)
- `github.com/stretchr/testify` — `assert`/`require` test assertions (already in use across all 13 existing personas)

**Replaceability:** `simon` is fully removable/replaceable by deleting its 2 files (`simon.yaml`, `simon.md`) plus the fixture and reverting 2 registration rows (roster + `index.json`) — no other code branches on the `simon` slug specifically, matching the existing persona-swap pattern used by all 13 predecessors.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| `simon.md` prompt template | Untrusted-tier community persona prompt fed verbatim to the LLM | Template-injection via a disallowed Go template action (`range`/`with`/`template`/`define`/pipeline/field-chain) smuggled into the rendered prompt | `ValidateFetchedPersonaPrompt` allow-list gate (`internal/registry/persona_test.go:305`) rejects any non-bare-token action before the prompt reaches a model |
| `personas/community/index.json` `path` field | Local manifest entry consumed by search/install tooling | Path traversal via a `..`-escaping or absolute `path` value | `verifyCommunityIndex` (`internal/personas/search_test.go:30`) rejects an absolute `Path` or a `..`-escaping join relative to `personasRoot` |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| `go:embed` resolution (`CommunityNames`/`CommunityGet`) | Called once per persona-registry read across 14 personas after this change (was 13) | No measurable latency change | Compile-time embed resolution; no runtime I/O added |
| `go test ./personas/... ./internal/personas/... ./internal/registry/...` | Full CI run, +1 persona's subtests | Stay within existing CI time budget | All checks are local file reads / JSON / YAML parses — no new network or LLM calls introduced |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Category word collision | `simon`'s `Category` matches one of the 13 already-claimed words, or drifts from the word embedded verbatim in `simon.md` | `TestCommunityPersonas_DistinctCategories` / `TestCommunityPersonas_FixtureAndPromptCategory` fail loudly (`t.Fatalf` / `require.Containsf`), blocking merge — never a silent pass |
| Partial registration | Roster row added without a matching `index.json` entry, or vice versa | `require.Len` fatal failure in `TestCommunityAccessors` / `TestCommunityIndex_Registration` blocks the entire `personas` test package, not just `simon`'s subtest |
| Provider/model prefix mismatch | `provider: local` without a `local/` model prefix, or a non-local provider carrying one | `TestCommunityIndex_Registration`'s provider-restricted rule fails explicitly, naming the drifted field |
| Blog CTA drift | Outline cites a CLI invocation that does not exist (e.g. a `--persona` flag `atcr review` never registers) | Caught only by AC 03-01's manual grep verification — no automated Go test gate covers Markdown content, so this depends on the Phase 3/4 process step, not tooling |

### Defensive Measures Required

- **Input Validation:** `yaml.v3` `KnownFields(true)` strict decode on `simon.yaml`; `ValidateFetchedPersonaPrompt` allow-list gate on `simon.md`; `verifyCommunityIndex` path-traversal guard on `index.json`'s `path` field.
- **Error Handling:** Every cross-file consistency failure (roster/index/category/vendor-token mismatch) surfaces as an explicit, named `require`/`t.Fatalf` test failure — never a silent skip, a swallowed error, or a panic.
- **Logging/Audit:** N/A — no runtime logging surface is introduced; all verification is compile-time or test-time.
- **Rate Limiting:** N/A — no new network or LLM-call surface; the fixture runner is a no-LLM structural check.
- **Graceful Degradation:** N/A by design — a missing or malformed `simon` unit fails the build's test gate loudly (fatal `require.Len`) rather than degrading gracefully at runtime, consistent with how all 13 existing personas are guarded.

---

## Risks

**Technical:**
- Category word or primary task-tag collides with one of the 13 already-claimed values → mitigated by cross-checking the full claimed-word list against `simon.md`'s literal text before writing the roster/index rows (explicit in Stories 1-2's own risk tables).
- `simon`'s Role+Focus language drifts above the 0.85 Jaccard similarity ceiling against an existing persona (especially `sonny`'s correctness/logic focus) → mitigated by grounding every sentence in AI-authorship-specific artifacts (tautological comments, unnecessary abstractions, defensive-overkill, dead/hallucinated code) rather than generic code-quality language.
- Partial registration (roster row without matching `index.json` entry, or vice versa) leaves the shared `personas` test package fatally broken for any concurrent work on the branch → mitigated by landing the fixture, roster row, and `index.json` entry as one atomic commit (Phase 2 constraint).

**TDD-Specific:**
- Stories 1 and 2 cannot each land green independently — Story 1 alone turns `TestCommunityAccessors` and `TestTemplateFixtureRunner_CommunityPersonasPass` red the moment `simon.md` is embedded — mitigated by developing in story order but treating Stories 1+2 as merging to the default branch as a single unit, running the full `go test ./personas/... ./internal/personas/... ./internal/registry/...` suite only after both land.
- Story 3 depends on Stories 1-2's *final shipped* category word and CLI surface, not the word anticipated in `plan.md` — mitigated by sequencing Story 3 strictly last and treating the shipped `simon.yaml`/`simon.md` as source of truth over planning documents.

---

**Next:** `/create-sprint @.planning/plans/active/29.0_anti_slop_persona/`
