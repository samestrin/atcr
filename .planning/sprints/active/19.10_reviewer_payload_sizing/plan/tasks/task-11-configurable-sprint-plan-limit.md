# Task 11: Configurable Sprint-Plan Limit (F9)

**Source:** Plan 19.10 – Debt Item #10
**Priority:** P2 | **Effort:** S | **Type:** Refactor

## Problem Statement
`internal/payload/sprintplan.go:20` hardcodes `MaxSprintPlanBytes int64 = 16384` — the byte ceiling applied to a `--sprint-plan` file before it is wrapped in a SCOPE CONSTRAINT block and prepended to every reviewer's payload. `ReadSprintPlan` (line 35) uses it directly to bound the buffered read (`io.LimitReader(f, MaxSprintPlanBytes+1)`), and `ScopeConstraint` (line 89) uses it directly to cap the embedded plan text (`capUTF8(plan, int(MaxSprintPlanBytes))`). Operators running larger-context models have no way to raise this ceiling to make fuller use of their model's context window — 16 KiB is a conservative floor picked when the feature shipped (Epic 12.2), not a tunable. There is also no config surface: `.atcr/config.yaml` cannot declare a sprint-plan byte limit today.

## Solution Overview
Add `max_sprint_plan_bytes` as a new numeric config key, following the existing pointer-field precedence pattern already used by `PayloadByteBudget`/`CacheMaxBytes`: a `*int64` field on `Registry` and `ProjectConfig` (nil = unset, falls through to the next tier), resolved once into a plain `int64` field on `Settings` by `ResolveSettings` in `internal/registry/precedence.go`, with an embedded default of `65536` (64 KiB, up from the current 16 KiB) defined in `internal/registry/project.go` alongside `DefaultCacheMaxBytes`/`DefaultPayloadByteBudget`. `internal/payload/sprintplan.go` loses its package-level `MaxSprintPlanBytes` constant entirely: `ReadSprintPlan` and `ScopeConstraint` both gain a `maxBytes int64` parameter instead, so the payload package stays free of any dependency on `internal/registry` (avoiding an import cycle) and simply does what its caller tells it. `internal/fanout/review.go` is the caller that threads `cfg.Settings.MaxSprintPlanBytes` through both call sites (`resolveScopeConstraint` and `buildSlots`).

**Design note on the "Effective\*() resolver" pattern named in the plan documentation:** the documentation (`documentation/config-yaml-parsing.md`, "Configurable Sprint-Plan Limit (F9)") repeatedly says this field "must follow the same pointer + `Effective*()` resolver pattern as `PayloadByteBudget`/`TimeoutSecs`." Checked against the actual code: `PayloadByteBudget` and the shared (non-per-agent) `TimeoutSecs` do NOT have an `Effective*()` method at the `Settings` level — they resolve straight into a plain `Settings` field inside `ResolveSettings` and are read directly by callers (e.g. `cfg.Settings.PayloadByteBudget` in `internal/fanout/review.go:797`). `Effective*()` methods (e.g. `AgentConfig.EffectiveTimeoutSecs`, `ExecutorConfig.EffectiveExecutorTimeoutSecs`) exist only where a **per-agent or per-executor override** must be resolved against the shared `Settings` value — a dimension `max_sprint_plan_bytes` does not have (F9 defines no per-agent override). The closest, and most directly analogous, existing precedent is `CacheMaxBytes` (`internal/registry/config.go:433`, `internal/registry/project.go:59`, `internal/registry/precedence.go:101-103,141-143,162-163,221-222`): a `*int64` at the registry and project tiers, overlaid explicitly in `ResolveSettings` (not through `applyTier`, whose signature is fixed at 4 params), resolved to a plain `int64` `Settings` field, with a dedicated `cache_settings_test.go`. **Follow the `CacheMaxBytes` shape exactly; do not invent a standalone `EffectiveMaxSprintPlanBytes()` method** — it would be a pointless identity wrapper (`return s.MaxSprintPlanBytes`) since there is nothing to resolve at that layer. If a reviewer insists on the literal method for documentation-conformance, it is a one-line addition, but it is not required for AC10 and does not change any consumer's behavior.

## Technical Implementation
### Steps
1. **`internal/registry/project.go`** — add the embedded default and the project-tier field:
   - In the `const (...)` block (project.go:15-35) alongside `DefaultCacheMaxBytes`, add:
     ```go
     // DefaultMaxSprintPlanBytes is the embedded byte ceiling applied to a
     // --sprint-plan file before it is wrapped in a SCOPE CONSTRAINT block and
     // prepended to every reviewer's payload (Epic 12.2 / F9). 64 KiB gives
     // operators on larger-context models room for a fuller sprint/epic plan
     // than the original fixed 16 KiB ceiling.
     DefaultMaxSprintPlanBytes int64 = 65536
     ```
   - Add `MaxSprintPlanBytes *int64 \`yaml:"max_sprint_plan_bytes,omitempty"\`` to `ProjectConfig` (project.go:41-67), placed near `CacheMaxBytes`, with a doc comment: "MaxSprintPlanBytes overrides the sprint-plan byte ceiling (F9). A pointer so an explicit value survives default application; unset inherits the registry tier or the embedded `DefaultMaxSprintPlanBytes`."
   - In `LoadProjectConfig` (project.go:117-175), add a validation check alongside the existing `cfg.CacheMaxBytes` check (project.go:155-157): `if cfg.MaxSprintPlanBytes != nil && *cfg.MaxSprintPlanBytes <= 0 { return nil, fmt.Errorf("%s: max_sprint_plan_bytes must be > 0, got %d", base, *cfg.MaxSprintPlanBytes) }`. **Unlike `payload_byte_budget`/`cache_max_bytes`, 0 is NOT treated as "unlimited" here** — a sprint-plan cap of 0 or negative has no sensible meaning (there is no "unbounded plan injection" use case), so reject `<= 0` rather than `< 0`.
   - In `DefaultProjectConfigYAML` (project.go:77-112), add a commented block mirroring the `cache_max_bytes` comment style (project.go:96-98), then emit the value. **Coordinate placement with Task 05**, which is independently adding an `on_overflow: chunk` line to this same function — add the `max_sprint_plan_bytes` block as its own paragraph (e.g. immediately after the `cache_max_bytes` block, before `fail_on`) so the two tasks' diffs land as adjacent additions rather than overlapping edits to the same lines:
     ```go
     b.WriteString("# max_sprint_plan_bytes: byte ceiling for a --sprint-plan file's SCOPE\n")
     b.WriteString("#   CONSTRAINT injection into every reviewer's payload. Default 64 KiB; raise it\n")
     b.WriteString("#   to give larger-context models more sprint/epic plan detail.\n")
     fmt.Fprintf(&b, "max_sprint_plan_bytes: %d\n", DefaultMaxSprintPlanBytes)
     ```
2. **`internal/registry/config.go`** — add the registry (global) tier field:
   - Add `MaxSprintPlanBytes *int64 \`yaml:"max_sprint_plan_bytes,omitempty"\`` to `Registry` (config.go:413-464), placed near `CacheMaxBytes` (config.go:429-433), with a matching doc comment noting this is the registry (global) tier of the same limit `ProjectConfig.MaxSprintPlanBytes` carries at the project tier.
   - In `Registry.validate()` (config.go:505-...), add a check alongside the existing `r.CacheMaxBytes` check (config.go:518-520): `if r.MaxSprintPlanBytes != nil && *r.MaxSprintPlanBytes <= 0 { errs = append(errs, fmt.Errorf("max_sprint_plan_bytes must be > 0, got %d", *r.MaxSprintPlanBytes)) }`.
   - Do NOT touch `DefaultTimeoutSecs`, `DefaultMaxContextLines`, `EffectiveTimeoutSecs`, or `EffectiveMaxContextLines` — out of scope per the plan's Constraints.
3. **`internal/registry/precedence.go`** — thread the field through `ResolveSettings`:
   - Add `MaxSprintPlanBytes int64` to `Settings` (precedence.go:87-110), placed near `PayloadByteBudget`/`CacheMaxBytes`, with a doc comment: "MaxSprintPlanBytes is the resolved byte ceiling applied to a --sprint-plan file's SCOPE CONSTRAINT injection (F9); see internal/payload.ReadSprintPlan/ScopeConstraint."
   - Seed the default in the initial `Settings{}` literal in `ResolveSettings` (precedence.go:125-134): `MaxSprintPlanBytes: DefaultMaxSprintPlanBytes,`.
   - Overlay the registry tier immediately after the `reg.CacheMaxBytes` overlay (precedence.go:141-143): `if reg.MaxSprintPlanBytes != nil { s.MaxSprintPlanBytes = *reg.MaxSprintPlanBytes }`. This is an explicit overlay (like `CacheMaxBytes`), NOT through `applyTier` — that helper's signature is fixed at 4 params (`payloadMode, timeoutSecs, byteBudget, maxParallel`) and is shared with Task 05's `on_overflow`/`ReviewStrategy`-style additions; do not widen it.
   - Overlay the project tier immediately after the `proj.CacheMaxBytes` overlay (precedence.go:162-163): `if proj.MaxSprintPlanBytes != nil { s.MaxSprintPlanBytes = *proj.MaxSprintPlanBytes }`.
   - There is no CLI override for `max_sprint_plan_bytes` in this task (no `CLIOverrides` field) — not requested by F9/AC10, mirrors `CacheMaxBytes`.
   - Add a post-resolution sanity check mirroring the `CacheMaxBytes` check (precedence.go:219-222), since a directly-constructed `proj`/`reg` bypassing the file loaders could carry an invalid value: `if s.MaxSprintPlanBytes <= 0 { return Settings{}, fmt.Errorf("max_sprint_plan_bytes must be > 0, got %d", s.MaxSprintPlanBytes) }`.
4. **`internal/payload/sprintplan.go`** — remove the package constant, parameterize the two functions:
   - Delete `const MaxSprintPlanBytes int64 = 16384` (line 20) and its doc comment (lines 10-19); replace with a short comment on `ReadSprintPlan`/`ScopeConstraint` explaining the cap is now caller-supplied (see below).
   - Change `func ReadSprintPlan(path string) (content string, err error)` → `func ReadSprintPlan(path string, maxBytes int64) (content string, err error)`. Replace `io.LimitReader(f, MaxSprintPlanBytes+1)` (line 58) with `io.LimitReader(f, maxBytes+1)`.
   - Change `func ScopeConstraint(content string) (block string, truncated bool)` → `func ScopeConstraint(content string, maxBytes int64) (block string, truncated bool)`. Replace `capUTF8(plan, int(MaxSprintPlanBytes))` (line 94) with `capUTF8(plan, int(maxBytes))`.
   - Update the doc comments (lines 22-34, 65-88) that reference `MaxSprintPlanBytes` to describe the `maxBytes` parameter instead (the cache-invalidation-limitation note at lines 77-82 still applies verbatim, just parameterized).
   - `capUTF8` (line 118) is unchanged — it already takes `max int` as a parameter.
5. **`internal/fanout/review.go`** — thread `cfg.Settings.MaxSprintPlanBytes` to both call sites:
   - `resolveScopeConstraint` (review.go:773-783): change signature to `func resolveScopeConstraint(req ReviewRequest, maxSprintPlanBytes int64) (constraint, warning string)`. Update the two internal calls: `payload.ReadSprintPlan(req.SprintPlanPath, maxSprintPlanBytes)` and `payload.ScopeConstraint(raw, maxSprintPlanBytes)`. Update the truncation warning (line 780) to use `maxSprintPlanBytes` instead of `payload.MaxSprintPlanBytes`. Update the doc comment (line 768) similarly.
   - Update all three call sites to pass the resolved setting: line 259 (`resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)`), line 354 (`resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)` — `cfg` is already in scope in `finalizePreparedReview`, review.go:278), and line 493 (`resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)`).
   - `buildSlots` (review.go:791, already takes `cfg *ReviewConfig`): replace `payload.MaxSprintPlanBytes` at line 804 with `cfg.Settings.MaxSprintPlanBytes`. Update the comment at line 796 similarly.
6. **`.atcr/config.yaml`** (project root — this repo's own local, gitignored project config; see `.gitignore:66`) — add `max_sprint_plan_bytes: 65536` with a short comment above it in the same style as the `payload_byte_budget`/`max_parallel` comments (`.atcr/config.yaml:19-22`). **Coordinate with Task 05**, which independently adds `on_overflow: chunk` to this same file — add each key as its own line so the two edits merge cleanly regardless of which task lands first.
7. **Update existing tests that reference the removed `payload.MaxSprintPlanBytes` constant** (a breaking rename — these will not compile otherwise):
   - `internal/payload/sprintplan_test.go` (lines 59-133, 192): replace every `MaxSprintPlanBytes` reference with a local test constant, e.g. `const testMaxBytes int64 = 16384` (keep the original 16 KiB value for these byte-math scenarios so the existing assertions' magnitudes stay meaningful), and pass it explicitly into every `ReadSprintPlan(path, testMaxBytes)` / `ScopeConstraint(content, testMaxBytes)` call.
   - `internal/fanout/review_sprintplan_test.go` (lines 23-62, 50, 61, 108, 135-138): update every `resolveScopeConstraint(ReviewRequest{...})` call to pass a max-bytes argument (use `registry.DefaultMaxSprintPlanBytes` — import `github.com/samestrin/atcr/internal/registry` if not already imported — so the test reflects the real production default of 65536, not the old 16384), and replace `payload.MaxSprintPlanBytes` references accordingly. `TestPrepareReviewFromDiff_InjectsSprintPlanConstraint` and any end-to-end test that builds a `ReviewConfig`/`Settings` via the normal loader path needs no change beyond this — `cfg.Settings.MaxSprintPlanBytes` will already carry the new embedded default of 65536 once `ResolveSettings` is updated.
8. Add the new registry-tier unit tests described in Test Strategy below (new file `internal/registry/sprintplan_settings_test.go`, mirroring `internal/registry/cache_settings_test.go` exactly).
9. Run `go build ./...` and `go test ./internal/payload/... ./internal/registry/... ./internal/fanout/...` to confirm the parameterization, config schema, and precedence chain compile and pass, then `go test ./...` for the full suite.

## Files to Create/Modify
- `internal/payload/sprintplan.go` – modify (remove `MaxSprintPlanBytes` const at line 20; parameterize `ReadSprintPlan` line 35 and `ScopeConstraint` line 89 with `maxBytes int64`)
- `internal/registry/config.go` – modify (add `Registry.MaxSprintPlanBytes *int64`, validation check)
- `internal/registry/precedence.go` – modify (add `Settings.MaxSprintPlanBytes int64`, default seed, tier overlays, post-resolution re-check)
- `internal/registry/project.go` – modify (add `DefaultMaxSprintPlanBytes`, `ProjectConfig.MaxSprintPlanBytes`, `LoadProjectConfig` validation, `DefaultProjectConfigYAML` scaffold)
- `internal/fanout/review.go` – modify (thread `cfg.Settings.MaxSprintPlanBytes` through `resolveScopeConstraint` at lines 773-783 and its 3 call sites at lines 259, 354, 493, plus `buildSlots` at line 804)
- `.atcr/config.yaml` – modify (document and default `max_sprint_plan_bytes: 65536`; coordinate with Task 05's `on_overflow` addition)
- `internal/payload/sprintplan_test.go` – modify (replace removed `MaxSprintPlanBytes` const references with a local test constant, pass explicitly to both functions)
- `internal/fanout/review_sprintplan_test.go` – modify (pass `registry.DefaultMaxSprintPlanBytes` to `resolveScopeConstraint` calls; update byte-count assertions)
- `internal/registry/sprintplan_settings_test.go` – create (new file, mirrors `internal/registry/cache_settings_test.go`)

## Documentation Links
- [Config YAML Parsing](../documentation/config-yaml-parsing.md) — section "Configurable Sprint-Plan Limit (F9)"

## Related Files (from codebase-discovery.json)
- `internal/payload/sprintplan.go`
- `internal/registry/config.go`
- `internal/registry/precedence.go`
- `internal/registry/project.go`
- `internal/registry/cache_settings_test.go` – existing pointer-field precedence test pattern this task mirrors exactly

## Success Criteria
- [x] `max_sprint_plan_bytes` is a recognized YAML key in `.atcr/config.yaml`, `~/.config/atcr/registry.yaml` (project overlay), and any project config — strict `KnownFields(true)` decoding accepts it without error
- [x] An unset `max_sprint_plan_bytes` at every tier resolves to `65536` via `ResolveSettings` (AC10)
- [x] An explicit `max_sprint_plan_bytes` at the project tier overrides the registry tier and the embedded default (AC10)
- [x] An explicit `max_sprint_plan_bytes` at the registry tier overrides the embedded default when the project tier is unset
- [x] A `<= 0` value is rejected at load time in both `LoadProjectConfig` and `LoadRegistry`/`Registry.validate()`, and by the post-resolution sanity check in `ResolveSettings` for a directly-constructed `proj`/`reg`
- [x] `ReadSprintPlan` and `ScopeConstraint` no longer reference any package-level constant — both take the byte ceiling as a parameter, verified by test with two different caller-supplied values producing different truncation points
- [x] `internal/fanout` threads `cfg.Settings.MaxSprintPlanBytes` end-to-end: a review configured with a custom `max_sprint_plan_bytes` embeds a plan truncated at that custom length, not the old hardcoded 16384 or the new default 65536
- [x] No behavior change to `on_overflow`/Task 05's schema additions — this task's diff to `internal/registry/config.go`, `precedence.go`, `project.go`, and `.atcr/config.yaml` is additive and merges cleanly alongside Task 05's additions to the same files

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestPrecedence_MaxSprintPlanBytesDefault` — nil proj, nil reg → `Settings.MaxSprintPlanBytes == 65536`
- `TestPrecedence_MaxSprintPlanBytesChain` — registry sets one value, project sets another → project wins
- `TestPrecedence_MaxSprintPlanBytesRegistryOverridesEmbedded` — only registry tier sets a value, project nil → registry value wins
- `TestProjectConfig_MaxSprintPlanBytesZeroRejected` — `max_sprint_plan_bytes: 0` in project config is rejected at load (unlike `cache_max_bytes`, 0 is not a valid "unbounded" sentinel here)
- `TestProjectConfig_MaxSprintPlanBytesNegativeRejected` — `max_sprint_plan_bytes: -1` is rejected at load
- `TestRegistry_MaxSprintPlanBytesInvalidRejected` — `max_sprint_plan_bytes: -5` in registry.yaml is rejected by `Registry.validate()`
- `TestResolveSettings_MaxSprintPlanBytesDirectlyConstructedInvalidRejected` — a directly-constructed `Settings`-producing `proj`/`reg` with an out-of-range value is caught by the post-resolution sanity check
- `internal/payload` — `TestReadSprintPlan_RespectsCallerSuppliedLimit` and `TestScopeConstraint_RespectsCallerSuppliedLimit`: call both functions with two different `maxBytes` values (e.g. 100 and 500) against the same oversized input and assert the truncation point matches each caller-supplied limit, not a fixed constant

**Integration Tests:**
- `internal/fanout/review_sprintplan_test.go` — extend `TestResolveScopeConstraint` (or add a sibling) to pass a custom `maxSprintPlanBytes` and confirm the embedded plan length matches that custom value, not the production default
- `internal/fanout/review_sprintplan_test.go` — a `ReviewConfig` built from a project config that sets `max_sprint_plan_bytes: 200` end-to-end produces a SCOPE CONSTRAINT block whose embedded plan is capped at 200 bytes, proving the value flows `.atcr/config.yaml` → `LoadProjectConfig` → `ResolveSettings` → `internal/fanout` → `internal/payload`

**Test Files:**
- `internal/payload/sprintplan_test.go`
- `internal/registry/sprintplan_settings_test.go` (new)
- `internal/fanout/review_sprintplan_test.go`

## Risk Mitigation
- **Merge conflict with Task 05.** Task 05 (`task-05-on-overflow-config-schema.md`) independently modifies the SAME four files (`internal/registry/config.go`, `internal/registry/precedence.go`, `internal/registry/project.go`, `.atcr/config.yaml`) to add a DIFFERENT config key (`on_overflow`, a plain string) using a DIFFERENT pattern (no pointer, no `ResolveSettings` explicit-overlay-outside-`applyTier` — it follows `ReviewStrategy`'s shape instead). Mitigated by: (a) placing every `max_sprint_plan_bytes` edit near the existing `CacheMaxBytes`/`PayloadByteBudget` fields, physically separated from where `on_overflow` sits near `ReviewStrategy`, so the diffs rarely touch the same lines; (b) both tasks are purely additive (new struct fields, new `if` blocks, new scaffold lines) — neither reverts or restructures existing code, so a merge/rebase of one onto the other should apply cleanly; (c) whoever lands second should re-run `go build ./...` and `go test ./internal/registry/...` immediately after merging to catch any accidental clobber before it ships.
- **Breaking API change to `payload.ReadSprintPlan`/`ScopeConstraint`.** These are exported functions; removing the constant and adding a required parameter breaks any external caller. Mitigated by: `internal/fanout` is the only in-repo caller (confirmed via `grep -rn "ReadSprintPlan\|ScopeConstraint" --include="*.go"`), and this is a pre-1.0 internal refactor within the same module, not a published external API.
- **Documentation says "Effective\*() resolver pattern"; the closer precedent (`CacheMaxBytes`) has no such method.** See the Design Note in Solution Overview above — resolved by following `CacheMaxBytes` (the structurally identical existing case: registry+project pointer tiers only, no CLI override, no per-agent dimension) rather than inventing an unnecessary identity-wrapper method.
- **Test default drift.** `internal/payload/sprintplan_test.go`'s existing assertions hard-code byte-math against the old 16384 default. Mitigated by giving that test file its own local `testMaxBytes` constant decoupled from the production default, so the test keeps validating the truncation *mechanism* rather than depending on whatever the current production default happens to be.

## Dependencies
- None hard — independent of F1-F8, can run in parallel with the rest of the sprint. Shares files with Task 05 (`on_overflow` config schema); coordinate merges per the Risk Mitigation above.

## Definition of Done
- [x] `internal/payload/sprintplan.go`'s `MaxSprintPlanBytes` constant is removed; `ReadSprintPlan`/`ScopeConstraint` both take `maxBytes int64`
- [x] `Registry.MaxSprintPlanBytes`, `ProjectConfig.MaxSprintPlanBytes`, and `Settings.MaxSprintPlanBytes` all added with correct `yaml:"max_sprint_plan_bytes,omitempty"` tags where applicable
- [x] `LoadProjectConfig` and `Registry.validate()` both reject `max_sprint_plan_bytes <= 0` with a clear error
- [x] `ResolveSettings` resolves `max_sprint_plan_bytes` through the registry → project precedence chain, defaulting to 65536, with a post-resolution sanity re-check
- [x] `internal/fanout/review.go` threads `cfg.Settings.MaxSprintPlanBytes` through `resolveScopeConstraint` and `buildSlots`, with no remaining reference to the old package constant anywhere in the repo (`grep -rn "payload.MaxSprintPlanBytes"` returns nothing)
- [x] `.atcr/config.yaml` documents and sets `max_sprint_plan_bytes: 65536`
- [x] `DefaultProjectConfigYAML` in `internal/registry/project.go` emits a documented `max_sprint_plan_bytes` line for `atcr init`-generated configs
- [x] All new and updated unit/integration tests pass, including the byte-truncation-at-custom-limit end-to-end test
- [x] `go build ./...` succeeds
- [x] `go test ./...` passes
- [x] AC10 verified: the sprint-plan byte limit is configurable via `max_sprint_plan_bytes` in `.atcr/config.yaml`, proven by test
