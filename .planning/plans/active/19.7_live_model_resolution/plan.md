# Plan 19.7: Live Model Resolution, Lockfile & Drift Detection

## Metadata
- **Plan Type:** feature
- **Last Modified:** 2026-07-08T23:08:15Z
- **Source Epic:** `.planning/epics/active/19.7_live_model_resolution.md`

## Plan Overview
**Plan Goal:** Layer a live, auto-updating model resolution system over the persona `model` bindings Epic 19.6 shipped, so personas automatically ride each vendor family's capability curve without hand-edited slugs, while keeping reviews reproducible by default via an explicit lock that only advances on `atcr personas upgrade`. This also closes 19.6's deferred HIGH: reconciling the disjoint init/quickstart fetch-and-pin roster against the shipped community index.
**Target Users:** atcr maintainers curating the community persona library; end users running `atcr personas upgrade`/`atcr models check` to keep installed personas current and drift-free; Epic 19.8's downstream mechanical monitoring agent (a consumer of `atcr models check`'s deterministic output).
**Framework/Technology:** Go stdlib `net/http` + `encoding/json` (OpenRouter `/api/v1/models` catalog client, mirroring `internal/personas/client.go`'s injectable-`HTTPClient` + retry/backoff pattern); `golang.org/x/mod/semver` (already used for version comparison in `upgrade.go`); `spf13/cobra` (new `atcr models check` command); `gopkg.in/yaml.v3` (persona YAML, unchanged).

## Objectives
1. Layer live model resolution on top of Epic 19.6's static persona `model` bindings so a persona rides each vendor family's capability curve as models evolve, without hand-editing slugs.
2. Preserve reproducibility by default: a persona declares a logical family/channel binding; reviews run a resolved concrete slug recorded in a lock; the lock advances only on an explicit `atcr personas upgrade`, never silently mid-review.
3. Seed every persona's initial lock from 19.6's existing pinned `model` value with zero migration.
4. Confirm, via a real authenticated completion call, whether `~`-prefixed provider `-latest` aliases are completion-routable before the resolver commits to alias-bindings.
5. Define a `@stable` channel heuristic against OpenRouter's live schema that excludes preview/beta/exp tokens and honors `expiration_date`, while allowing an `@latest` channel that includes preview models.
6. Implement a hybrid resolver: alias-bind the 7 alias-covered personas; resolve DeepSeek/Qwen/GLM via a `created`-timestamp newest-in-vendor-prefix resolver or an explicit pin; ensure an explicit-slug pin never floats regardless of channel.
7. Make `atcr personas upgrade` re-resolve family/channel bindings, advance the lock, and print a before→after slug report per persona (e.g., `anthony: opus-4.8 → 5.0`).
8. Deliver `atcr models check [--json]` as a deterministic drift/deprecation/missing-slug report with machine-readable output and meaningful exit codes.
9. Gate major-version model jumps on the persona's existing fixture still passing and surface a "prompt tuned for the prior major — verify" flag; let minor jumps auto-lock.
10. Reconcile `init`/`quickstart` model enablement against the real community index to close 19.6's deferred HIGH (TD-011): deliver a working, non-noisy community persona set backward-compatible with existing on-disk personas.
11. Keep CI zero-live-network by backing all resolver/catalog tests with a checked-in catalog snapshot fixture and a refresh command.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 8 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/19.7_live_model_resolution/`

## Feature Analysis Summary
Epic 19.6 already proved the fetch-and-pin + explicit-upgrade discipline (locked Clarification C3): resolution never touches the review hot path, and an explicit `atcr personas upgrade` is the only path that advances a persona's version. This epic extends that exact posture from "static slug" to "resolved slug from a family/channel binding" — the resolver is strictly upstream of `internal/registry.ResolvePersona`'s prompt-resolution chain and must never modify it. A catalog spike (AC1) against OpenRouter's `/api/v1/models` confirms whether `~`-prefixed `-latest` aliases are actually completion-routable before the hybrid resolver design commits to them for 7 of 10 personas; DeepSeek/Qwen/GLM (no alias) fall back to a `created`-timestamp newest-in-vendor-prefix resolver or an explicit pin. A major-version jump additionally gates on the existing `TemplateFixtureRunner` still passing and surfaces a re-tune flag, while a minor jump auto-locks — reusing `upgrade.go`'s existing `isNewer` semver-comparison machinery. Separately, this epic closes 19.6's deferred HIGH (TD-011): `init`/`quickstart` currently fetch-and-pin `builtins.Names()` (9 model-agnostic built-ins) against a community index that only publishes 10 disjoint model-indexed personas, so online `init`/`quickstart` today pin zero community personas and emit 9 misleading skip warnings.

## Technical Planning Notes
- The resolved lock is additive to `PersonaIndexEntry` (`internal/personas/search.go`), following 19.6's `omitempty` + permissive-decode convention so old-shape `index.json` payloads keep decoding.
- A new `internal/personas/catalog.go` mirrors `client.go`'s `fetch()` retry/backoff/timeout/body-cap helper and reuses its `HTTPClient` injection seam, so the OpenRouter catalog client is testable via `httptest.NewServer` with zero live network in CI (AC8) — matching the project's existing convention of a per-package retry helper (confirmed distinct from `internal/llmclient`'s and `internal/ghaction`'s own separate retry implementations).
- `init.go:47`/`quickstart.go:102` both independently call `installCommunityPersonas(..., builtins.Names(), ...)` — AC7's fix must route through one shared reconciliation point to avoid the two-call-site drift pattern already seen in 19.6's TD-006/TD-007 history.
- `atcr models check [--json]` is a net-new `cmd/atcr/models.go` command (no `models.go`/`newModelsCmd` exists today), registered alongside the existing `personas` subcommand family and reusing its flag/output conventions.
- No new third-party dependency is warranted: stdlib `net/http`/`encoding/json` plus the already-vendored `golang.org/x/mod/semver` cover every technical need.

## Documentation References
Grounded reference docs for implementation — see [documentation/README.md](documentation/README.md) for the full index:
- **[CRITICAL]** [OpenRouter Catalog & Completions API](documentation/openrouter-catalog-api.md) — model schema, missing stability flag, `expiration_date`, `~`-alias resolution.
- **[CRITICAL]** [Existing Codebase Patterns to Reuse](documentation/existing-resolver-patterns.md) — the `fetch()`/`Upgrade()`/`isNewer()` reuse seams, the `ResolvePersona` boundary, command registration, lock persistence, and the AC7 drift risk.
- **[IMPORTANT]** [Catalog Snapshot Fixture Discipline](documentation/catalog-snapshot-fixture.md) — checked-in fixture, `httptest` zero-live-network testing, and the refresh command (AC8).
- **[IMPORTANT]** [`atcr models check` Command Design](documentation/models-check-command.md) — drift/deprecation/missing-slug reporting, exit codes, and `--json` output shape (AC5).
- **[IMPORTANT]** [Semantic Version Comparison](documentation/semver-version-comparison.md) — `golang.org/x/mod/semver` API and AC6's major/minor gate.

## Clarifications

### AC7 Roster Reconciliation — LOCKED (recorded 2026-07-08)

Proposed Solution item #8 offered two options for closing TD-011 (init/quickstart's disjoint fetch-and-pin roster) and deferred the choice to design-sprint/implementation judgment. This is now locked:

**Decision: Option B — align the fetch-and-pin roster with what the community index publishes.** The effective roster for online `init`/`quickstart` becomes the set of persona names published in the fetched `personas/community/index.json` itself (all entries, e.g. anthony/sonny/gene/milo/gia/flint/delia/quinn/celeste/glenna today) — not the hardcoded `builtins.Names()` list of 9 embedded, model-agnostic built-ins. Because the roster is derived from the index at fetch time rather than hardcoded, it self-heals as the index grows or changes; no per-release roster maintenance is required.

**Rejected: Option A** (publishing built-in lenses into the community channel behind a model-agnostic gate carve-out) — rejected because it requires inventing a null/agnostic-model case in a schema whose entire purpose is model-binding (fighting this epic's own family/channel model), and it duplicates content that already ships correctly via the embedded built-in scaffold (`runInit`/`initTargets`), which is unrelated to and unaffected by this decision.

**Scope:** No change to `personas/community/index.json`'s schema or existing entries. `builtins.Names()` and the embedded built-in `.md` scaffold step remain exactly as they are — this decision only changes what roster argument `installCommunityPersonas` (`cmd/atcr/init.go:96`, called from `init.go:47` and `quickstart.go:102`) is reconciled against.

## Implementation Strategy
Land the catalog spike and stable-channel heuristic first (AC1) since the hybrid resolver's alias-bind path depends on its outcome, though the `created`-timestamp/explicit-pin fallback means the epic is not blocked either way. Then land the family/channel binding schema + lock (AC2), seeded from 19.6's existing pinned `model` values with zero migration. Build the hybrid resolver (AC3) and wire it into `atcr personas upgrade` (AC4) so resolution happens exactly once per explicit upgrade invocation, never on the review hot path. Add `atcr models check` (AC5) as the deterministic drift-report primitive Epic 19.8 will consume. Layer the major/minor re-validation gate (AC6) on top of the existing fixture runner. Finally, reconcile the init/quickstart roster against the real community index (AC7) and document the full family/channel/lock model plus reproducible-vs-upgrade behavior (AC8).

## Recommended Packages
No high-ROI packages identified — stdlib `net/http`/`encoding/json` and the already-used `golang.org/x/mod/semver` cover every need, consistent with this project's no-new-dependency convention.

## User Story Themes

### Theme 1 — Catalog Routability Spike & Stable-Channel Heuristic
One authenticated completion call against a `~…-latest` alias confirms (or refutes) real routability before the resolver design commits to aliases; defines the `@stable` preview/beta/exp-exclusion heuristic against OpenRouter's live schema, and pins the `z-ai/` (not `glm/`) vendor prefix for glenna.

### Theme 2 — Family/Channel Binding & Resolved Lock
A persona declares a logical family/channel binding; atcr records the resolved concrete slug as a lock. Reviews run the lock deterministically with no endpoint call on the review path. 19.6's pinned `model` values seed the initial lock with zero migration.

### Theme 3 — Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)
The 7 alias-covered personas bind to provider `-latest` aliases (provider owns resolution); DeepSeek/Qwen/GLM resolve via a `created`-based newest-in-vendor-prefix resolver or an explicit pin; an explicit-slug pin never floats regardless of channel.

### Theme 4 — Reproducible Upgrade with Before→After Lock Reporting
`atcr personas upgrade` re-resolves and advances the lock, printing exactly what changed per persona (e.g. `anthony: opus-4.8 → 5.0`); the models endpoint is touched only during this explicit, user-initiated command — never silently mid-review.

### Theme 5 — `atcr models check` Drift Report
A net-new deterministic command reporting newer-family-member drift, deprecation (`expiration_date`), and missing-slug conditions, with `--json` machine-readable output and meaningful exit codes — both a user-facing command and the seam Epic 19.8's mechanical agent wraps.

### Theme 6 — Major-Bump Re-Validation Gate
A minor version advance (4.8→4.9) auto-locks; a major jump (4.x→5.x) requires the persona's existing fixture to still pass and surfaces a "prompt tuned for the prior major — verify" flag before the lock advances, reusing `TemplateFixtureRunner` unchanged.

### Theme 7 — init/quickstart Roster Reconciliation
Closes 19.6's deferred HIGH (TD-011): rebuilds init/quickstart model enablement so online `init`/`quickstart` deliver a working, non-noisy persona set — either publishing built-in lenses into the community channel behind a model-agnostic gate carve-out, or aligning the fetch-and-pin roster with what the index actually publishes — backward-compatible with existing on-disk personas.

### Theme 8 — Catalog Snapshot Fixture, Refresh Command & Documentation
Provides the checked-in OpenRouter catalog snapshot fixture that makes Stories 3 and 5 deterministic in CI, the on-demand `atcr models refresh` command that regenerates it as the live catalog drifts, and the user-facing documentation updates (`docs/personas-authoring.md`, `docs/personas-install.md`) that explain the family/channel/lock model and the reproducible-vs-upgrade behavior (AC8).

## Success Criteria
- Alias routability confirmed/refuted by a real completion call; `@stable` heuristic defined against the live catalog schema (AC1).
- A persona's family/channel binding resolves to a lock that reviews run deterministically, with zero endpoint calls on the review path (AC2, AC3).
- `atcr personas upgrade` reports the before→after resolved slug per persona; no silent runtime model change ever occurs (AC4).
- `atcr models check [--json]` reports drift/deprecation/missing-slug with machine-readable output and exit codes (AC5).
- Online `init`/`quickstart` pin a working, non-noisy community persona set — TD-011 closed (AC7).
- `go test ./...` passes with all resolver/catalog tests backed by a checked-in snapshot, zero live network in CI; an `atcr models refresh` command regenerates the snapshot on demand; and docs document the family/channel/lock model plus reproducible-vs-upgrade behavior (AC8).

## Risk Mitigation
- **Risk:** OpenRouter's `~`-prefixed aliases may not be completion-routable, undermining the 7-persona alias-bind design. **Mitigation:** AC1's spike runs first and is a real authenticated call, not an assumption; the hybrid resolver's `created`-timestamp/explicit-pin fallback covers a negative result without blocking the epic.
- **Risk:** A checked-in catalog snapshot fixture silently drifts from the real OpenRouter schema over time, masking a real API change from CI. **Mitigation:** Proposed Solution #9 explicitly includes a refresh command to regenerate the snapshot on demand.
- **Risk:** `init.go` and `quickstart.go` are fixed independently for the roster/index reconciliation (AC7), repeating the exact two-call-site drift pattern that produced 19.6's TD-006/TD-007. **Mitigation:** route the fix through one shared reconciliation point (or the roster/index source itself) rather than patching each call site separately.

## Next Steps
1. `/find-documentation @.planning/plans/active/19.7_live_model_resolution/`
2. `/create-documentation @.planning/plans/active/19.7_live_model_resolution/`
3. `/create-user-stories @.planning/plans/active/19.7_live_model_resolution/`
4. `/create-acceptance-criteria @.planning/plans/active/19.7_live_model_resolution/`
5. `/design-sprint @.planning/plans/active/19.7_live_model_resolution/`
