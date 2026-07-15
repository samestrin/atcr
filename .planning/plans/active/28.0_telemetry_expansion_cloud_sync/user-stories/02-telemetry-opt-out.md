# User Story 2: Telemetry Opt-Out

**Plan:** [28.0: Telemetry Expansion & Cloud Sync](../plan.md)

## User Story

**As a** privacy-conscious team lead running `atcr` in CI or on a network-restricted developer machine
**I want** a strict, well-documented `ATCR_TELEMETRY=0` environment variable and a persisted `atcr config set telemetry false` command
**So that** I can completely and verifiably disable the background usage ping from Story 1 — for a single invocation, a CI job, or permanently for my whole team — without trusting an unverifiable "opt-in" default

## Story Context

- **Background:** Story 1 introduces `internal/telemetry`, a fail-open background client wired into `cmd/atcr/review.go:runReview` and `cmd/atcr/reconcile.go:runReconcile`, enabled by default. This story is the gating control on top of that client: without it, telemetry has no opt-out surface at all and cannot ship to privacy-conscious teams. There are two existing precedents to follow exactly: `ATCR_DISABLE_AST_GROUPING` (`internal/reconcile/astgrouping.go:27`) is the only existing boolean-shaped, `ATCR_`-prefixed opt-out env var in the codebase, with documented `strconv.ParseBool`-based parsing and dedicated coverage in `cmd/atcr/docs_audit_test.go` (`TestArchitectureDocDescribesReconciler`'s fact-check list and `TestDocsClaimedFlagsAreReal`'s flag-doc cross-check); and `cmd/atcr/main.go:logLevelFromEnv` (line 216) is the existing pattern for a validated `os.Getenv` read performed once at root-command construction time. Additionally, there is currently no `atcr config` command anywhere in `main.go`'s `AddCommand` list (`cmd/atcr/main.go:185-208`) — this story creates that command group from scratch, following the `cmd/atcr/debt.go:newDebtCmd` subcommand-group pattern (a parent `Use: "config"` command with `RunE: cmd.Help` and a `newConfigSetCmd()` child registered via `AddCommand`).
- **Assumptions:** Story 1's telemetry client exposes a construction/dispatch seam that can be short-circuited into a true no-op (no goroutine spawned, zero network calls) rather than merely suppressing errors after a call attempt. `.atcr/config.yaml`, loaded via `internal/registry/project.go`'s `ProjectConfig` struct, is the correct persistence target for `atcr config set telemetry false` — it already holds project-level toggles (e.g. `Sandbox`, `AutoFix`) using the pointer-for-explicit-value idiom, and `DefaultProjectConfigPath` (`internal/registry/project.go:93`) resolves its path. The opt-out is process-wide: once disabled by either surface, no `atcr` command in that invocation emits a ping, regardless of which subcommand runs.
- **Constraints:** `ATCR_TELEMETRY=0` must be read once at root-command construction time (mirroring `logLevelFromEnv`), not per-subcommand, so no code path can accidentally bypass it. Both opt-out surfaces are OR'd, not layered with override precedence: telemetry is disabled if the env var says so, OR the persisted config says so — there is no "config re-enables what the env var disabled" escape hatch, since that would violate the "strict" opt-out requirement. The env var's boolean direction is the inverse of `ATCR_DISABLE_AST_GROUPING`'s: `ATCR_TELEMETRY` names the enabled state directly (0/false disables, 1/true/unset enables), not a "disable" flag, so the value semantics must be documented clearly to avoid the same "presence-only footgun" `astgroup.go`'s comment already warns about. Must not weaken or touch `internal/scorecard/export.go`'s `scrubField`/`PublicRecord` allowlist — this story is purely the on/off gate, with no data-shape changes.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (Anonymous Usage Telemetry Ping) — this story gates the client Story 1 builds; it cannot be implemented or tested without that client existing. |

## Success Criteria (SMART Format)

- **Specific:** Setting `ATCR_TELEMETRY=0` (read once in `cmd/atcr/main.go`, alongside `logLevelFromEnv`) and running `atcr config set telemetry false` (new `cmd/atcr/config.go`, registered in `newRootCmd`'s `AddCommand` list) each independently and completely disable the Story 1 telemetry client for every subsequent `atcr` invocation, with the config-file route persisting to `.atcr/config.yaml` via `ProjectConfig`.
- **Measurable:** Tests prove (1) with `ATCR_TELEMETRY=0` set, running `review` or `reconcile` against a mock telemetry endpoint results in zero HTTP requests, (2) after `atcr config set telemetry false` runs once, a subsequent `atcr review` invocation with no env var set still makes zero HTTP requests, (3) `cmd/atcr/docs_audit_test.go`'s existing flag/env coverage checks pass for the new `ATCR_TELEMETRY` env var and `atcr config set` command documentation.
- **Achievable:** Reuses two proven patterns verbatim — the `LOG_LEVEL`-style single-read env var and the `debt.go` subcommand-group — so no new architectural surface is required beyond a `Telemetry *bool` field on `ProjectConfig` and a construction-time no-op switch on the Story 1 client.
- **Relevant:** Directly satisfies epic AC3 ("`ATCR_TELEMETRY=0` strictly disables all background telemetry") verbatim, and is the trust surface that makes AC1/AC2's default-on telemetry acceptable for privacy-conscious adopters — without it the epic cannot ship telemetry at all.
- **Time-bound:** Deliverable within this sprint cycle immediately after Story 1 lands the client it gates, and before Stories 3-5 (which extend the export schema and add `--sync-cloud`) so the opt-out is in place before any additional data leaves the machine.

## Acceptance Criteria Overview

1. `ATCR_TELEMETRY=0` (and recognized falsy equivalents), read once at root-command construction time, disables the Story 1 telemetry client for the entire process — verified by a test asserting zero network calls regardless of which command runs.
2. `atcr config set telemetry false` persists the opt-out to `.atcr/config.yaml` (new `cmd/atcr/config.go`, following the `debt.go` subcommand-group pattern) so telemetry stays disabled on subsequent invocations without requiring the env var to be set each time; `atcr config set telemetry true` re-enables it.
3. The two opt-out surfaces are OR'd (either one disabling telemetry is sufficient and final for that invocation) — no combination of flags/env/config can silently re-enable telemetry once either says "off".
4. `docs/telemetry.md` documents `ATCR_TELEMETRY` and `atcr config set telemetry` with their exact accepted values, and `cmd/atcr/docs_audit_test.go`'s flag/env-var coverage checks pass for both, following the existing `ATCR_DISABLE_AST_GROUPING` coverage precedent.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`_

## Technical Considerations

- **Implementation Notes:** Add `telemetryEnabledFromEnv() bool` to `cmd/atcr/main.go` beside `logLevelFromEnv`, parsing `ATCR_TELEMETRY` with `strconv.ParseBool` where an unset or unparseable value defaults to `true` (enabled) and a valid falsy value (`0`, `false`, `f`, `F`, `False`, `FALSE`) disables — the inverse boolean direction of `ATCR_DISABLE_AST_GROUPING`, so the doc comment must state this explicitly to avoid confusion between the two `ATCR_`-prefixed env vars. Add `cmd/atcr/config.go` with `newConfigCmd()` (`Use: "config"`, `RunE: cmd.Help`) and a child `newConfigSetCmd()` (`Use: "set"`, `Args: usageArgs(cobra.ExactArgs(2))`) that validates the key is `telemetry` (returning a `usageError` for any other key, scoping the surface to telemetry only per the plan's decision) and the value parses as a bool, then loads, mutates, and rewrites `.atcr/config.yaml` via a new `Telemetry *bool` field on `registry.ProjectConfig` (pointer, matching the existing `Sandbox`/`AutoFix`/`MaxParallel` idiom so an explicit `false` survives default application). Register `newConfigCmd()` in `newRootCmd`'s `AddCommand` list (`cmd/atcr/main.go:185-208`). Story 1's telemetry client needs a small seam added here (if not already present) so the disabled state short-circuits before any goroutine spawns — not merely before the HTTP call fires.
- **Integration Points:** `cmd/atcr/main.go:newRootCmd` (env var read + config command registration), `cmd/atcr/config.go` (new file), `internal/registry/project.go` (`ProjectConfig.Telemetry` field + `DefaultProjectConfigPath`), Story 1's `internal/telemetry` client (disabled-state construction), `cmd/atcr/docs_audit_test.go` (flag/env coverage — likely requires extending the fact-check list the way `ATCR_DISABLE_AST_GROUPING` is checked today, or adding an equivalent check for `ATCR_TELEMETRY`), `docs/telemetry.md` (new, per the plan).
- **Data Requirements:** No new event/payload schema — this story only gates whether the Story 1 payload is ever sent. The one new persisted field is `telemetry: false` (or `true`) in `.atcr/config.yaml`'s YAML body, following the same `omitempty`-on-pointer convention as the file's other optional blocks.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The disabled check happens too late (e.g. only suppressing the HTTP send after a goroutine and payload are already built), so "strictly disables" is not literally true and a network call or partial resource use still occurs | High | Gate at client construction/dispatch entry, before any goroutine spawns; add a test that asserts no goroutine is scheduled and no allocation of the HTTP payload occurs when disabled, not just that no request is observed |
| Env var and persisted config disagree (e.g. config says `true`, env var says `0`) and an implementation bug treats the config as authoritative, silently re-enabling telemetry the user explicitly disabled via env var | High | Implement as a strict OR (disabled wins) rather than a precedence/override chain; add a test matrix covering all four combinations of {env unset/0} x {config true/false} asserting disabled wins whenever either says so |
| `ATCR_TELEMETRY`'s inverse boolean direction relative to `ATCR_DISABLE_AST_GROUPING` causes a maintainer or user to set `ATCR_TELEMETRY=true` intending to disable it (confusing the two env var conventions) | Medium | Document the exact accepted values and semantics prominently in both `docs/telemetry.md` and the `--help` long text for `atcr config set`; the `docs_audit_test.go` coverage check enforces the doc stays accurate |

---

**Created:** July 15, 2026
**Status:** Draft - Awaiting Acceptance Criteria
