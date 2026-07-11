# Task 05: on_overflow Config Schema (Parse, Validate, Resolve)

**Source:** Plan 19.10 – Debt Item #4 (config schema)
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
Today the payload sizer has exactly one degradation behavior — an implicit shed-to-fit — with no config surface to select or even name it. F4 introduces a real `on_overflow` policy ladder (`chunk` default, `truncate`, `fallback`, `fail`), but before any dispatch code can read a policy value there has to be somewhere for that value to live: `.atcr/config.yaml` cannot declare `on_overflow` today (strict `KnownFields(true)` decoding would reject it as an unknown key), `internal/registry`'s `Settings`/`ProjectConfig`/`Registry` structs have no field for it, and `ResolveSettings` has no precedence rule to overlay project/registry values onto a default. This task closes that gap purely at the config layer — parsing, validating the enum, defaulting, and threading the resolved value through the precedence chain — without touching the dispatch logic that consumes it (Task 04).

## Solution Overview
Add `on_overflow` as a plain string policy key (not a pointer — unlike `max_sprint_plan_bytes`/F9, there is no "explicit zero vs unset" ambiguity to preserve) to `ProjectConfig`, `Registry`, and `Settings`, following the existing `ReviewStrategy` field exactly: a `yaml:"on_overflow,omitempty"` struct tag, a `DefaultOnOverflow = "chunk"` constant next to `DefaultReviewStrategy` in `internal/registry/project.go`, an `onOverflowValid` enum-membership validator mirroring `reviewStrategyValid` in `internal/registry/review_strategy.go`, load-time validation calls in both `Registry.validate()` (config.go) and `LoadProjectConfig` (project.go), and a precedence overlay in `ResolveSettings` (precedence.go) identical in shape to the existing `ReviewStrategy` overlay (registry tier, then project tier, trimmed non-empty wins; embedded default is `chunk`). Finally, document the new key with its four legal values in `.atcr/config.yaml` and in the `atcr init` scaffold `DefaultProjectConfigYAML` renders, mirroring the comment style already used for `payload_byte_budget`/`max_parallel`.

## Technical Implementation
### Steps
1. In `internal/registry/review_strategy.go`, add a sibling file `internal/registry/on_overflow.go` (or extend the same pattern in a new file) declaring:
   - `var validOnOverflowPolicies = map[string]bool{"chunk": true, "truncate": true, "fallback": true, "fail": true}` — all four F4 ladder values are *config-valid* now, even though `fallback`/`fail` dispatch behavior lands in Task 04.
   - `func onOverflowValid(value string) bool` mirroring `reviewStrategyValid` (internal/registry/review_strategy.go:15-18): trimmed empty is valid (unset, falls through precedence), any non-empty value must be a map key.
2. In `internal/registry/project.go`:
   - Add `DefaultOnOverflow = "chunk"` to the `const` block alongside `DefaultReviewStrategy` (project.go:15-35), with a doc comment referencing F4 and the four-value ladder (chunk/truncate/fallback/fail) and noting `fallback`/`fail` are recognized-but-gated per AC4 (dispatch enforcement lives in Task 04, not here).
   - Add `OnOverflow string \`yaml:"on_overflow,omitempty"\`` to `ProjectConfig` (project.go:41-67), placed near `ReviewStrategy` with a doc comment: "OnOverflow selects the F4 degradation policy when a payload exceeds budget: chunk (default), truncate, fallback, or fail. Empty inherits the registry tier or the embedded default."
   - In `LoadProjectConfig` (project.go:117-175), add a validation call alongside the existing `reviewStrategyValid(cfg.ReviewStrategy)` check (project.go:161-163): `if !onOverflowValid(cfg.OnOverflow) { return nil, fmt.Errorf("%s: invalid on_overflow '%s': must be one of chunk, truncate, fallback, fail", base, strings.TrimSpace(cfg.OnOverflow)) }`.
   - In `DefaultProjectConfigYAML` (project.go:77-112), add a commented block (mirroring the `payload_byte_budget`/`max_parallel` comment style at project.go:89-95) documenting the four legal values and the default, then emit `fmt.Fprintf(&b, "on_overflow: %s\n", DefaultOnOverflow)`.
3. In `internal/registry/config.go`:
   - Add `OnOverflow string \`yaml:"on_overflow,omitempty"\`` to `Registry` (config.go:413-464), placed near `ReviewStrategy` (config.go:421-425) with a matching doc comment noting this is the registry (global) tier of the same policy `ProjectConfig.OnOverflow` carries at the project tier.
   - In `Registry.validate()` (config.go:505-569), add a check alongside the existing `reviewStrategyValid(r.ReviewStrategy)` check (config.go:529-531): `if !onOverflowValid(r.OnOverflow) { errs = append(errs, fmt.Errorf("invalid on_overflow '%s': must be one of chunk, truncate, fallback, fail", r.OnOverflow)) }`.
   - Do NOT touch `applyDefaults()`, `DefaultTimeoutSecs`, `DefaultMaxContextLines`, `EffectiveTimeoutSecs`, or `EffectiveMaxContextLines` — out of scope per the plan's Constraints and this task's grounding. `OnOverflow` intentionally gets no `Effective*()` resolver method: it is resolved once, centrally, in `ResolveSettings` (Settings.OnOverflow), the same way `ReviewStrategy` is — not per-call like the pointer fields.
4. In `internal/registry/precedence.go`:
   - Add `OnOverflow string` to `Settings` (precedence.go:87-110), placed near `ReviewStrategy` (precedence.go:89-92) with a doc comment: "OnOverflow is the resolved F4 degradation policy (chunk/truncate/fallback/fail) dispatched by internal/fanout when a payload exceeds budget (see Task 04)."
   - In `ResolveSettings` (precedence.go:124-252), seed the default in the initial `Settings{}` literal (precedence.go:125-134): `OnOverflow: DefaultOnOverflow,`.
   - Overlay the registry tier immediately after the existing `ReviewStrategy` overlay (precedence.go:153-158): `if v := strings.TrimSpace(reg.OnOverflow); v != "" { s.OnOverflow = v }`.
   - Overlay the project tier immediately after the existing `ReviewStrategy` overlay (precedence.go:165-167): `if v := strings.TrimSpace(proj.OnOverflow); v != "" { s.OnOverflow = v }`.
   - Add a post-resolution sanity re-check mirroring the `reviewStrategyValid(s.ReviewStrategy)` check (precedence.go:227-229), since a directly-constructed `proj`/`reg` bypassing the file loaders could carry an out-of-range value: `if !onOverflowValid(s.OnOverflow) { return Settings{}, fmt.Errorf("invalid on_overflow '%s': must be one of chunk, truncate, fallback, fail", s.OnOverflow) }`.
   - There is no CLI override for `on_overflow` in this task (no `CLIOverrides` field) — out of scope; not requested by F4 or AC4.
5. Update `.atcr/config.yaml` (project root, the repo's own live project config): add `on_overflow: chunk` with a short comment above it in the same style as the `payload_byte_budget` comment at `.atcr/config.yaml:19-22`, listing the four legal values and noting `fallback`/`fail` are recognized but their dispatch prerequisites (Task 04) may not yet be shipped.
6. Add/extend `internal/registry/config_test.go` and `internal/registry/precedence_test.go` (or new sibling `on_overflow_test.go` files matching the `review_strategy_test.go` pattern) with the unit tests described below.
7. Run `go build ./...` and `go test ./internal/registry/...` to confirm the new field, validator, and precedence overlay compile and pass, then `go test ./...` for the full suite.

## Files to Create/Modify
- `internal/registry/config.go` – modify (add `Registry.OnOverflow`, validation call)
- `internal/registry/precedence.go` – modify (add `Settings.OnOverflow`, default seed, tier overlays, post-resolution re-check)
- `internal/registry/project.go` – modify (add `DefaultOnOverflow`, `ProjectConfig.OnOverflow`, `LoadProjectConfig` validation, `DefaultProjectConfigYAML` scaffold)
- `internal/registry/on_overflow.go` – create (new file; `validOnOverflowPolicies` map + `onOverflowValid` validator, mirroring `internal/registry/review_strategy.go`)
- `.atcr/config.yaml` – modify (document and default `on_overflow: chunk`)

## Documentation Links
- [Config YAML Parsing](../documentation/config-yaml-parsing.md)
- [on_overflow Policy](../documentation/on-overflow-policy.md)

## Related Files (from codebase-discovery.json)
- `internal/registry/config.go`
- `internal/registry/precedence.go`
- `internal/registry/project.go`
- `internal/registry/review_strategy.go` – existing plain-string-enum pattern this task mirrors exactly

## Success Criteria
- [ ] `on_overflow` is a recognized YAML key in `.atcr/config.yaml`, `~/.config/atcr/registry.yaml` (project overlay), and any project config — strict `KnownFields(true)` decoding accepts it without error
- [ ] An unset `on_overflow` at every tier resolves to `chunk` via `ResolveSettings`
- [ ] `on_overflow: chunk` in `.atcr/config.yaml` resolves correctly through `LoadProjectConfig` → `ResolveSettings` to `Settings.OnOverflow == "chunk"`
- [ ] An explicit `on_overflow: truncate` (or `fallback`, or `fail`) at the project tier overrides the registry tier and the embedded default
- [ ] An invalid value (e.g. `on_overflow: yolo`) is rejected at load time in both `LoadProjectConfig` and `LoadRegistry`/`Registry.validate()` with a clear error naming the four legal values
- [ ] A typo'd key (e.g. `on_overlow:`) is rejected at load time by strict YAML decoding (proves the struct tag is correctly wired, not that a stray key was silently ignored)
- [ ] `Registry.OnOverflow` and `ProjectConfig.OnOverflow` have no `Effective*()` resolver method (resolution happens once, centrally, in `ResolveSettings`) — confirmed by code review, not a runtime check

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestLoadProjectConfig_OnOverflow_DefaultUnset` — a config.yaml with no `on_overflow` key loads successfully with `ProjectConfig.OnOverflow == ""` (unset, not yet defaulted — defaulting happens in `ResolveSettings`, not the loader)
- `TestLoadProjectConfig_OnOverflow_ExplicitValid` — each of `chunk`, `truncate`, `fallback`, `fail` loads successfully
- `TestLoadProjectConfig_OnOverflow_Invalid` — an unrecognized value (e.g. `yolo`) returns a load error naming the four legal values
- `TestLoadProjectConfig_OnOverflow_TypoKey_StrictModeRejects` — `on_overlow:` (typo) is rejected by strict decoding, proving `KnownFields(true)` + the new struct tag interact correctly
- `TestRegistryValidate_OnOverflow_Invalid` — `Registry{OnOverflow: "yolo"}` fails `validate()` with a clear error
- `TestResolveSettings_OnOverflow_DefaultsToChunk` — nil proj, nil reg → `Settings.OnOverflow == "chunk"`
- `TestResolveSettings_OnOverflow_ProjectOverridesRegistry` — registry tier sets `truncate`, project tier sets `fail` → resolved value is `fail`
- `TestResolveSettings_OnOverflow_RegistryOverridesDefault` — only registry tier sets `truncate`, project nil → resolved value is `truncate`
- `TestResolveSettings_OnOverflow_WhitespaceTreatedAsUnset` — a whitespace-only `OnOverflow` string at any tier falls through to the next tier (mirrors the existing `ReviewStrategy` whitespace behavior)
- `TestResolveSettings_OnOverflow_DirectlyConstructedInvalidRejected` — a directly-constructed `Settings`-producing `proj`/`reg` (bypassing file-load validation) with an out-of-range `OnOverflow` is caught by the post-resolution sanity check

**Integration Tests:**
- None required for this task — dispatch integration (the policy value actually driving chunk/truncate/fallback/fail behavior in `internal/fanout`) is Task 04's scope. This task's integration surface ends at `ResolveSettings` returning the correct `Settings.OnOverflow` string.

**Test Files:**
- `internal/registry/config_test.go`
- `internal/registry/precedence_test.go`
- `internal/registry/project_test.go`
- `internal/registry/on_overflow_test.go` – new file, mirroring `internal/registry/review_strategy_test.go`

## Risk Mitigation
- **Scope creep into dispatch logic.** F4's four-value ladder is easy to over-implement here since the temptation is to "just wire up chunk/truncate while I'm in here." Mitigated by keeping this task strictly to parse/validate/resolve — the value never leaves `Settings.OnOverflow`; no code in `internal/fanout` is touched.
- **Drift between the two validators (`onOverflowValid` vs `reviewStrategyValid`).** Mitigated by mirroring the existing file exactly (same trimmed-empty-is-valid semantics, same map-membership check) so a future reviewer immediately recognizes the pattern instead of a subtly different one.
- **Strict-mode YAML break for existing configs.** Adding a new struct field with `omitempty` is additive and backward-compatible — an existing `.atcr/config.yaml`/`registry.yaml` with no `on_overflow` key continues to load unchanged; only a config that already had a stray `on_overflow`-like typo would now surface it (desirable, not a regression).

## Dependencies
- None hard — can run in parallel with Task-04 (dispatch logic consumes this schema's output, but Task 04 can be developed/tested against a hand-built `Settings{OnOverflow: ...}` without waiting on this task to land)

## Definition of Done
- [ ] `internal/registry/on_overflow.go` created with `validOnOverflowPolicies` and `onOverflowValid`
- [ ] `Registry.OnOverflow`, `ProjectConfig.OnOverflow`, and `Settings.OnOverflow` all added with correct `yaml:"on_overflow,omitempty"` tags where applicable
- [ ] `LoadProjectConfig` and `Registry.validate()` both reject invalid `on_overflow` values with a clear error
- [ ] `ResolveSettings` resolves `on_overflow` through the registry → project precedence chain, defaulting to `chunk`, with a post-resolution sanity re-check
- [ ] `.atcr/config.yaml` documents and sets `on_overflow: chunk`
- [ ] `DefaultProjectConfigYAML` in `internal/registry/project.go` emits a documented `on_overflow` line for `atcr init`-generated configs
- [ ] All new unit tests pass
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] No changes to `internal/fanout` or any dispatch logic — this task is config-schema only
