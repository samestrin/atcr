# Sprint Design: Live Model Resolution, Lockfile & Drift Detection

**Created:** July 08, 2026 06:58:31PM
**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](.)
**Plan Type:** Feature Development
**Status:** Design Complete

---

## Original User Request

> Layer live, auto-updating model resolution over the persona `model` bindings that Epic 19.6 shipped, so a user rides each vendor family's capability curve as models evolve **without hand-editing slugs** — while keeping reviews **reproducible by default**. A persona binds to a model *family/channel*; a resolved **lock** records the concrete slug that actually runs; and the model only changes on an explicit `atcr personas upgrade`, never silently mid-review. This is a resolution layer *on top of* 19.6's static slugs, not a replacement — 19.6's pinned `model` becomes the initial lock.

**Referenced Resources:**
- [OpenRouter Catalog & Completions API](documentation/openrouter-catalog-api.md)
  - **Summary:** Documents the `/api/v1/models` schema (`id`, `canonical_slug`, `created`, `expiration_date`, no explicit stable/GA/preview flag) and the completions endpoint's alias-resolution behavior, grounded in both the epic's own catalog spike and OpenRouter's official docs.
  - **Key Points:** No schema field signals stability — a heuristic is required; `expiration_date` is the only deprecation signal; the quickstart's own example (`"model": "~openai/gpt-latest"`) is strong evidence `~`-aliases route server-side, though not a substitute for AC1's own authenticated spike call.
- [Existing Codebase Patterns to Reuse](documentation/existing-resolver-patterns.md)
  - **Summary:** Catalogs the exact file:line seams this epic must extend — `fetch()`'s retry template, `Upgrade()`'s resolver-insertion point, the additive-schema convention, and the `ResolvePersona` boundary that must never be touched.
  - **Key Points:** `internal/registry.ResolvePersona` (persona.go:47) resolves prompt text, not model bindings — this epic's resolver is a wholly separate, upstream concern; `cmd/atcr/main.go:202` is where `newModelsCmd()` registers; two-call-site drift (TD-006/TD-007) is a named regression risk for AC7.
- [Catalog Snapshot Fixture Discipline](documentation/catalog-snapshot-fixture.md)
  - **Summary:** Specifies the zero-live-network CI discipline: a checked-in `internal/personas/testdata/catalog_snapshot.json` served via `httptest.NewServer`, with an opt-in refresh command as the only live-network touchpoint.
  - **Key Points:** Fixture must cover every resolver branch (aliases, `created`-timestamp candidates, expiring models, preview tokens, all 10 pinned slugs); refresh is maintainer-initiated only, never CI-invoked.
- [`atcr models check` Command Design](documentation/models-check-command.md)
  - **Summary:** Specifies the net-new `atcr models check [--json]` command: three drift conditions (newer-member, deprecation, missing), an exit-code contract (0/1/2), and registration alongside the `personas` command family.
  - **Key Points:** Diagnostic-only, never invoked on the review path; determinism comes from defaulting to the checked-in snapshot rather than a live catalog call.
- [Semantic Version Comparison](documentation/semver-version-comparison.md)
  - **Summary:** Documents `golang.org/x/mod/semver`'s existing usage in `internal/personas/upgrade.go`'s `isNewer`, and how AC6's major/minor gate extends it via `semver.Major`.
  - **Key Points:** `isNewer`'s exact normalization/validity logic must be reused unmodified; a passing fixture on a major jump proves rendering only, never tuning quality — the human "verify" flag is unconditional.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Live Model Resolution
**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 15 days
**Phases:** 8
**Pattern:** Research & Spike → Foundation → Core Resolution → Upgrade Integration → Discovery Command → Validation Gate → Roster Reconciliation → Integration & Docs

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
resolver layer upstream of persona resolution
third-party API client retry pattern Go
semver major version comparison Go
CLI command family registration Cobra
checked-in API fixture zero network testing
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - A new resolution/lock layer is a genuinely new pattern (not existing-pattern reuse), but it is deliberately layered on top of 19.6's proven fetch-and-pin/additive-schema machinery without touching `ResolvePersona` — not a major overhaul.
- **Integration:** 3/3 - Spans 5 components (`internal/personas`, `internal/registry`, `cmd/atcr`, `personas/community`, `docs/`) plus one external system boundary (OpenRouter's live API) — complex multi-system by the plan's own COMPONENT_COUNT.
- **Story/Task & Test:** 3/3 - 8 stories, 25 acceptance criteria, three distinct test types (manual spike, unit, integration) — 3+ extensive.
- **Risk/Unknowns:** 2/3 - AC1's alias-routability question and the `@stable`/`expiration_date` interaction (flagged ambiguity in AC 03-04) are real unknowns, but the hybrid resolver's fallback path means no unknown blocks the epic outright (`HAS_GATED_WORK: false` per the plan's own refinement) — some unknowns, not significant/architecture-threatening ones.

**Time Formula:** SUM(per-phase estimate), scaled to Very-Complex 8-phase pattern (13+ day floor)
**Calculation:** 1 (spike) + 2 (schema/lock) + 3.5 (hybrid resolver) + 2 (upgrade integration) + 2 (models check) + 1 (major-bump gate) + 1.5 (roster reconciliation) + 2 (fixture/refresh/docs) = **15 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** strong (complexity 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/19.7_live_model_resolution/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Research & Spike (1 day)
**Story:** 1 — Catalog Routability Spike & Stable-Channel Heuristic
**Focus:** One authenticated completion call against a `~…-latest` alias to confirm real routability; define the `@stable` preview/beta/exp-exclusion heuristic against the live schema; pin the `z-ai/` (not `glm/`) vendor prefix for glenna. Design spike only — no shipped resolver code yet, matching 19.6 Phase 1's precedent.

### Phase 2: Foundation — Family/Channel Binding & Lock Schema (2 days)
**Story:** 2 — Family/Channel Binding & Resolved Lock
**Focus:** Extend `PersonaIndexEntry`/`AgentConfig` additively with the family/channel binding field (`omitempty`, permissive decode, per 19.6 convention); confirm the existing `Model` field is the lock consumed at review time (no new field, zero migration); verify 19.6's AC7 exact-match gate is unaffected.

### Phase 3: Core Resolution — Hybrid Resolver (3.5 days)
**Story:** 3 — Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)
**Focus:** The heaviest phase — build the OpenRouter catalog client (mirroring `client.go`'s `fetch()`/`HTTPClient` seam), the alias-bind path for 7 personas, the `created`-timestamp newest-in-vendor-prefix resolver for DeepSeek/Qwen/`z-ai/` (GLM), the explicit-pin escape hatch, and the `@stable`/`@latest` channel logic.

### Phase 4: Upgrade Integration (2 days)
**Story:** 4 — Reproducible Upgrade with Before→After Lock Reporting
**Focus:** Wire the Phase 3 resolver into `Upgrade()` immediately before the existing `isNewer`/write logic; extend `atcr personas upgrade`'s reporting to show before→after resolved slug; prove zero endpoint calls occur outside this explicit path.

### Phase 5: Discovery — `atcr models check` (2 days)
**Story:** 5 — `atcr models check` Drift Report
**Focus:** Net-new `cmd/atcr/models.go` command family; enumerate installed personas' locked slugs (via a `ListTiers`-style pattern); report newer-member/deprecation/missing conditions with `--json` and the 0/1/2 exit-code contract; default to the checked-in snapshot for determinism.

### Phase 6: Validation Gate — Major-Bump Re-Validation (1 day)
**Story:** 6 — Major-Bump Re-Validation Gate
**Focus:** Layer `semver.Major(local) != semver.Major(remote)` on top of the existing `isNewer` normalization to classify major vs. minor jumps; gate major jumps on `TemplateFixtureRunner` re-passing and an unconditional human-facing "verify" flag; minor jumps auto-lock unchanged.

### Phase 7: Roster Reconciliation — init/quickstart (1.5 days)
**Story:** 7 — init/quickstart Roster Reconciliation
**Focus:** Independent of Phases 1-6 (no hard dependency) — closes 19.6's deferred TD-011 HIGH per the **locked Option B decision**: derive the fetch-and-pin roster from the community index's own fetched entries instead of the hardcoded `builtins.Names()` list, fixed once in a shared location to avoid the TD-006/TD-007 two-call-site drift pattern. May run in parallel with Phases 2-6 if sprint scheduling favors it.

### Phase 8: Integration & Docs (2 days)
**Story:** 8 — Catalog Snapshot Fixture, Refresh Command & Documentation
**Focus:** Depends on Phases 2, 3, and 5 landing first. Author the checked-in catalog snapshot fixture covering every resolver branch; build the `atcr models refresh` command (maintainer-initiated, never CI-invoked); update `docs/personas-authoring.md`/`docs/personas-install.md` with the family/channel/lock model and reproducible-vs-upgrade behavior.

---

## Work Decomposition

| Phase | Story | ACs | Test Types |
|-------|-------|-----|------------|
| 1 | 01: Catalog Routability Spike & Stable-Channel Heuristic | [01-01](acceptance-criteria/01-01-latest-alias-routability-confirmed.md), [01-02](acceptance-criteria/01-02-stable-channel-heuristic-z-ai-prefix.md) | Manual ×2 |
| 2 | 02: Family/Channel Binding & Resolved Lock | [02-01](acceptance-criteria/02-01-family-channel-binding-schema-extension.md), [02-02](acceptance-criteria/02-02-review-path-reads-locked-slug-zero-endpoint-calls.md), [02-03](acceptance-criteria/02-03-pinned-model-seeds-initial-lock-zero-migration.md) | Unit ×2, Integration ×1 |
| 3 | 03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin) | [03-01](acceptance-criteria/03-01-alias-passthrough-seven-personas.md) … [03-05](acceptance-criteria/03-05-latest-channel-includes-preview.md) | Unit ×5 |
| 4 | 04: Reproducible Upgrade with Before→After Lock Reporting | [04-01](acceptance-criteria/04-01-upgrade-resolves-advances-lock-slug-report.md), [04-02](acceptance-criteria/04-02-resolution-isolated-to-upgrade-path.md), [04-03](acceptance-criteria/04-03-dry-run-reports-without-writing.md) | Integration ×1, Unit ×2 |
| 5 | 05: `atcr models check` Drift Report | [05-01](acceptance-criteria/05-01-command-registration-human-readable-drift-report.md) … [05-04](acceptance-criteria/05-04-deterministic-catalog-snapshot-default.md) | Integration ×1, Unit ×3 |
| 6 | 06: Major-Bump Re-Validation Gate | [06-01](acceptance-criteria/06-01-major-jump-fixture-gate-and-verify-flag.md), [06-02](acceptance-criteria/06-02-minor-jump-auto-lock-regression-guard.md) | Unit ×2 |
| 7 | 07: init/quickstart Roster Reconciliation | [07-01](acceptance-criteria/07-01-working-nonempty-community-roster.md), [07-02](acceptance-criteria/07-02-no-misleading-skip-warnings.md), [07-03](acceptance-criteria/07-03-shared-reconciliation-point-and-backward-compat.md) | Integration ×2, Unit ×1 |
| 8 | 08: Catalog Snapshot Fixture, Refresh Command & Documentation | [08-01](acceptance-criteria/08-01-checked-in-catalog-snapshot-coverage.md), [08-02](acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md), [08-03](acceptance-criteria/08-03-docs-family-channel-lock-and-reproducibility.md) | Unit ×1, Integration ×1, Docs ×1 |

**Dependency graph:** 1 → 2 → 3 → {4, 5} → 6 (needs 3+4); 7 is independent (no hard dependency, may run in parallel); 8 needs 2+3+5.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files in the same package as the code under test (Go convention, matching 19.6's precedent).

**Test File Placement Examples:**
- New: `internal/personas/catalog.go` / `internal/personas/catalog_test.go` (Phase 3 hybrid resolver + client)
- New: `cmd/atcr/models.go` / `cmd/atcr/models_test.go` (Phase 5 command, extended in Phase 8 with `refresh`)
- New: `internal/personas/testdata/catalog_snapshot.json` (Phase 8 fixture)
- Extend: `internal/personas/upgrade.go` / `upgrade_test.go` (Phase 4 resolver insertion + major-bump gate in Phase 6)
- Extend: `internal/personas/search.go` / `search_test.go` (Phase 2 additive binding field)
- Extend: `cmd/atcr/init.go` / `init_test.go`, `cmd/atcr/quickstart.go` / `quickstart_test.go` (Phase 7 roster reconciliation)
- Extend: `docs/personas-authoring.md`, `docs/personas-install.md` (Phase 8 documentation)

**Unit/Integration/E2E:** 16 unit ACs (resolver logic, schema, exit codes, semver gate), 6 integration ACs (upgrade command, models-check command, init/quickstart roster), 2 manual ACs (Phase 1's one-time authenticated spike call — not automatable), 1 documentation AC. No E2E tier — consistent with 19.6's CLI-scoped testing (no browser/UI surface). Coverage target: ≥80% baseline per project config, with the 5 ACs flagged "High complexity" in `test-planning-matrix.md` (02-02, 03-02, 03-04, 04-02, 06-01) receiving the most adversarial test design.

**Test Environment Status:**
- Framework: Go stdlib `testing` + `testify/assert`/`require` (confirmed project standard)
- Execution: `go test ./...` (project `testing.cmd`); zero live network in CI enforced by the `httptest.NewServer` + checked-in-snapshot discipline
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (project `coverage_cmd`, 80% baseline)

---

## Architecture

**Primitives:**
- Family/channel binding (logical, e.g. `anthropic/claude-opus@stable`) — the persona's declared intent
- Resolved lock (concrete slug, e.g. `anthropic/claude-opus-4.8`) — what reviews actually consume, stored in the existing `AgentConfig.Model`/`PersonaIndexEntry.Model` field
- Catalog model entry (`id`, `canonical_slug`, `created`, `expiration_date`) — the external OpenRouter shape the resolver reads

**Module Boundaries:**
- `internal/personas/catalog.go` (new) — OpenRouter catalog client + hybrid resolver; the only code that talks to the external API
- `internal/registry.ResolvePersona` (`persona.go:47`, unchanged) — the prompt-resolution chain; strictly downstream of and untouched by the resolver
- `cmd/atcr/models.go` (new) — CLI surface for `check`/`refresh`; consumes the resolver/lock, never re-implements it

**External Dependencies:**
- OpenRouter `/api/v1/models` (read-only) — wrapped behind `internal/personas.HTTPClient`, the same injection seam `client.go`'s `fetch()` already uses, so it is swappable for `httptest.NewServer` in every test
- `golang.org/x/mod/semver` (already vendored) — no new dependency; AC6's major/minor gate reuses `isNewer`'s existing normalization

**Replaceability:** The catalog client is swappable via `HTTPClient` injection (proven pattern from 19.6). The three resolver strategies (alias-bind, created-timestamp, explicit-pin) are independent, individually testable code paths within one hybrid resolver function — any one strategy can be revised without touching the others. `atcr models check`/`refresh` are thin CLI wrappers over the resolver + catalog client, replaceable without touching either.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|---------------------|
| Untrusted catalog data ingestion | Every field read from OpenRouter's `/api/v1/models` response (slugs, `created`, `expiration_date`) | A malicious or compromised catalog response injects control characters or oversized values into a slug string that later reaches `AgentConfig.Model` and, downstream, an actual LLM API call | Reuse `fetch()`'s existing body-size cap and timeout; validate slug strings are plain, printable identifiers before writing to the lock (mirroring 19.6's TD-008 control-char sanitization precedent) |
| AC1 spike's API key handling | The one-time authenticated completion call in Phase 1 | The maintainer's `OPENROUTER_API_KEY` is echoed to a log, terminal history, or committed fixture by mistake | Treat the key exactly as 19.6's `quickstart.go` API-key handling treats Synthetic keys — never print it, never commit it; the finding recorded is the outcome (routable: yes/no), not the raw request/response |
| Resolved lock reaching the review-path wire | `internal/registry/config.go`'s `AgentConfig.Model` → `internal/fanout/review.go`'s `renderAgent` | A malformed or adversarial slug written to the lock during `upgrade` propagates unchecked into a live review's LLM invocation | The resolver validates a catalog-derived slug before it is written as a lock (same validation the fresh-install path already applies to `Model`/`Provider`) — no new trust boundary is opened at the review-consumption point |
| 19.6 AC7 exact-match gate interaction | `personas/community_test.go`, `internal/personas/search_test.go`'s `TestCommunityIndex_ProviderModelMatchesYAML`/`TestVerifyCommunityIndex_FailsOnMismatch` | A new family/channel binding field accidentally weakens or bypasses the existing YAML↔index parity gate | Confirmed in Phase 2 grounding: the gate checks only `Provider`/`Model`, so a new `Binding` field is naturally exempt — verified by AC 02-03, not assumed |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| `atcr personas upgrade` resolution call | 1 catalog fetch per invocation (single-user CLI, not concurrent) | No added latency beyond the existing single-fetch pattern | Reuse `fetch()`'s 30s timeout + exponential backoff unchanged; resolution happens only here, never on the review hot path |
| `atcr models check` enumeration | Typical install: <20 personas | Sub-second for a local snapshot read; bounded by one fetch if live-catalog mode is ever used | Default to the checked-in snapshot (zero network) for determinism and speed; `ListTiers`-style dedup-by-name enumeration is already proven at this scale in 19.6 |
| `init`/`quickstart` roster-derived install | 1 index fetch, N persona installs (N = index size, today 10) | No new network round-trips versus today's (broken) fixed-roster attempt | Roster derivation happens from the single existing `FetchIndex` call already made inside `installCommunityPersonas` — no additional fetch introduced |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Vendor-prefix ambiguity | GLM catalog entries use `z-ai/`, not `glm/` | Resolver keys strictly on `z-ai/` for glenna; regression test asserts no `glm/` namespace assumption anywhere |
| Missing/null catalog fields | A model entry lacks `created`, or `expiration_date` is null | Resolver treats missing `created` as ineligible for "newest" selection; null `expiration_date` means not-deprecated, per the documented schema semantics |
| Alias non-routability (AC1 negative outcome) | The `~`-prefixed alias form turns out not to route in a real completion call | Hybrid resolver falls back cleanly to `created`-timestamp/explicit-pin for the affected persona(s) — no epic-blocking failure, per the plan's own `HAS_GATED_WORK: false` finding |
| Non-semver vendor version strings | A vendor ships a version string `isNewer`/`semver.IsValid` cannot parse | AC6's major-bump gate follows `isNewer`'s existing precedent: non-comparable versions are NOT treated as a major-bump trigger (avoids false-positive re-tune flags) |
| `@stable`/`@latest` boundary ambiguity | A model is both preview-tagged AND has a non-null `expiration_date` | **Flagged by AC 03-04's own review as unresolved in the story text** — must be explicitly decided during Phase 3 implementation (does `@latest` bypass one exclusion or both?), not left implicit |
| Mid-run roster/index desync | Catalog fetch succeeds but a roster member is momentarily absent, or fails partway through a multi-persona install | Existing all-or-nothing rollback and skip-then-continue behavior (`installCommunityPersonas`) is preserved unchanged under the reconciled (index-derived) roster |

### Defensive Measures Required

- **Input Validation:** Catalog-fetched slugs/aliases are validated as plain, printable identifiers before being written to the lock or passed to `AgentConfig.Model`; the existing `fetch()` body-size cap and timeout bound every catalog request.
- **Error Handling:** Resolver failures (network error, malformed catalog response, non-comparable version) produce a clear, actionable CLI error and abort — never a silent fallback to a stale or wrong model, per the epic's "no silent runtime model change" requirement.
- **Logging/Audit:** `atcr personas upgrade` and `atcr models check` print exactly what changed or was found; no silent state mutation anywhere in the resolution path.
- **Rate Limiting:** N/A — single-user CLI invocations, no concurrent request load against OpenRouter.
- **Graceful Degradation:** A failed catalog fetch during `upgrade` aborts cleanly with a descriptive error (mirroring 19.6's `--offline` hint pattern) rather than partially advancing the lock or falling back silently.

---

## Risks

**Technical:**
- OpenRouter's `~`-prefixed aliases may not be completion-routable → the hybrid resolver's `created`-timestamp/explicit-pin fallback covers a negative AC1 result without blocking the epic.
- A checked-in catalog snapshot fixture drifts from the real OpenRouter schema over time → the Phase 8 refresh command regenerates it on demand; document the refresh cadence.
- `init.go`/`quickstart.go` fixed independently for AC7 would repeat the TD-006/TD-007 drift pattern → **already mitigated**: the fix is locked to a single shared roster-derivation point (plan.md Clarifications).
- The `@stable`/`expiration_date`/preview-token interaction is ambiguous in the current story text (AC 03-04) → resolve explicitly during Phase 3 implementation, not left to interpretation.

**TDD-Specific:**
- Phase 1 (spike) produces a finding, not shipped code — do not skip writing it into `documentation/openrouter-catalog-api.md` before Phase 3 begins, or Phase 3's alias-bind design has no recorded justification.
- Phase 3's hybrid resolver has three independently-testable strategies (alias/created-timestamp/explicit-pin) — write RED tests per strategy, not one monolithic resolver test, so a regression in one strategy doesn't mask a pass in another.
- Phase 6's major-bump gate must be tested against `isNewer`'s exact existing normalization — a parallel/divergent implementation risks the two functions disagreeing on edge cases (non-semver strings, `v`-prefix handling).

---

**Next:** `/create-sprint @.planning/plans/active/19.7_live_model_resolution/`
