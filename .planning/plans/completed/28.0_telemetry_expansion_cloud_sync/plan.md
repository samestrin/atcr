# Plan 28.0: Telemetry Expansion & Cloud Sync

## Metadata
- **Plan Type:** feature
- **Last Modified:** 2026-07-15
- **Source:** `.planning/epics/active/28.0_telemetry_expansion_cloud_sync.md`

## Plan Overview
**Plan Goal:** Introduce an opt-in, fail-open anonymous usage telemetry client and a `--sync-cloud` payload mechanism so `atcr` can measure real-world product adoption, rank community personas via a crowdsourced Persona Leaderboard, and let teams push local scorecard data to the upcoming `atcr.dev` SaaS dashboard — all while giving privacy-conscious teams a strict, well-documented opt-out.
**Target Users:** Individual `atcr` CLI users (opted into anonymous telemetry by default), privacy-conscious teams needing a hard opt-out, and engineering managers consuming the `atcr.dev/dashboard` via `--sync-cloud`.
**Framework/Technology:** Go 1.25, `spf13/cobra` CLI framework, Go stdlib (`net/http`, `crypto/sha256`, `os`), existing `internal/scorecard` export pipeline.

## Objectives
1. **Anonymous Usage Telemetry** — Implement an opt-in, fail-open background ping on `atcr` command completion that emits events such as `{"event": "review_run", "lang": "go", "lines": 450, "status": "success"}` without capturing source code or blocking CLI execution.
2. **Persona Leaderboard Data** — Extend the scorecard export pipeline to include a cryptographically hashed Persona ID for the run, enabling the backend to aggregate which community personas are empirically most effective.
3. **Cloud Sync Push** — Add a `--sync-cloud` flag that authenticates with an `ATCR_API_KEY` environment variable (sent as a `Bearer` token) and pushes the complete anonymized local scorecard — including time/credits saved metrics — to the user's `atcr.dev/dashboard` account.
4. **Strict Opt-Out** — Provide unambiguous, well-documented opt-out mechanisms via `ATCR_TELEMETRY=0` and `atcr config set telemetry false` that completely disable the background usage ping.
5. **Privacy Documentation** — Update `docs/scorecard.md` and add `docs/telemetry.md` to keep the documented privacy model accurate, describe what is collected, explain the opt-out path, and satisfy the existing `docs_audit_test.go` flag/environment coverage checks.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`

## Feature Analysis Summary
This plan expands `atcr` from a purely local, explicit-opt-in benchmark tool (Epic 10.0) into a product with background usage telemetry and an authenticated cloud sync path. It touches four components: a brand-new `internal/telemetry` package for anonymous background pings, `internal/scorecard/export.go` for Persona ID hashing on the leaderboard export path, `cmd/atcr` for the new `config set telemetry` command and the `ATCR_TELEMETRY` / `ATCR_API_KEY` env var reads, and `docs/` for the privacy policy and opt-out instructions. Codebase discovery confirms `internal/scorecard/export.go` already owns the only anonymization boundary in the codebase (`PublicRecord` allowlist, `scrubField`, `AnonymizeRecord`) and that it is explicitly documented and clarified as by-design privacy-preserving behavior for the existing Epic 10.0 public leaderboard export — this plan must extend that boundary carefully rather than weaken it. No existing package in the codebase performs background/fire-and-forget network calls, so `internal/telemetry` is new from scratch and must be built to fail open per AC1.

## Technical Planning Notes
- `internal/scorecard/export.go`'s `scrubField`/`PublicRecord` allowlist is a documented privacy guarantee for the unrelated Epic 10.0 leaderboard export (see `docs/scorecard.md` Privacy Model and `clarifications-15.1_leaderboard-cost-na-rendering-Q3`). The Persona ID hashing for telemetry must live on a separate, clearly-named schema/allowlist rather than a runtime toggle on the existing `scrubField`/`PublicRecord` path.
- No prior art exists for background/non-blocking HTTP calls in this codebase (confirmed via semantic search) — `internal/telemetry`'s client must be goroutine-based, bounded by a short timeout, and must never block, panic, or fail the parent CLI command (AC1's fail-open requirement). Follow the "Panic Safety" and "Defer Cleanup" guidance in `implementation-standards.md` for the goroutine.
- `cmd/atcr/main.go:logLevelFromEnv` is the existing pattern for a validated `os.Getenv` read at root-command construction time (`LOG_LEVEL`) — model `ATCR_TELEMETRY` and `ATCR_API_KEY` reads on it.
- `ATCR_DISABLE_AST_GROUPING` is the existing precedent for a boolean-shaped, `ATCR_`-prefixed opt-out env var with documentation and test coverage in `cmd/atcr/docs_audit_test.go`'s flag/env coverage check — `ATCR_TELEMETRY=0` and the new `--sync-cloud` flag will need the same documentation coverage or that existing test fails.
- `internal/registry/precedence.go`'s `CLIOverrides` pointer-based (`nil` = unset) precedence pattern is the established model if `--sync-cloud`'s API key or endpoint ever needs config-file/env/flag layering.
- The `ATCR_API_KEY` env var must be sent as a `Bearer` token; a missing or invalid key must exit with a distinct, documented error code (not the generic usage-error code) so scripts/CI can detect the specific auth failure.
- **Decision:** No `atcr config` command exists in the current command tree (`main.go`'s `AddCommand` list has no config group). This plan creates a new `cmd/atcr/config.go` implementing `atcr config set <key> <value>` (scoped to `telemetry` for now, following the `cmd/atcr/debt.go` subcommand-group pattern) rather than relying on `ATCR_TELEMETRY=0` alone — the epic requires both opt-out surfaces.

## Implementation Strategy
Build `internal/telemetry` as a standalone, fail-open background client first (no dependency on the scorecard/export changes), wired into `cmd/atcr/main.go` at root-command construction alongside the existing `LOG_LEVEL`-style env var read. Add the `ATCR_TELEMETRY=0` env var and `atcr config set telemetry false` command as the opt-out surface, verified against `docs_audit_test.go`'s coverage check. Extend `internal/scorecard/export.go` with a separate, explicitly-named Persona ID hashing path for the leaderboard rather than modifying `scrubField`/`PublicRecord` in place, preserving the existing Epic 10.0 export's documented privacy guarantees. Finally add the `--sync-cloud` flag (via a new `cmd/atcr/flags.go`, following the `addSourceFlags`-style helper pattern in `cmd/atcr/debt.go`) that authenticates with `ATCR_API_KEY` as a Bearer token and pushes the anonymized local scorecard (including time/credits saved metrics) to the `atcr.dev/dashboard` endpoint, exiting with a distinct code on a missing/invalid key. Update `docs/scorecard.md` and add `docs/telemetry.md` to keep the documented privacy model accurate and `docs_audit_test.go` passing.

## Recommended Packages
No high-ROI third-party packages identified. The plan's needs (HTTP client, JSON, cryptographic hashing, env var reads, CLI flags) are fully covered by the Go standard library plus the already-present `github.com/spf13/cobra` and `gopkg.in/yaml.v3` dependencies in `go.mod` — introducing a new dependency for a fire-and-forget telemetry ping or SHA-256 hashing would add integration risk without meaningful ROI.

## User Story Themes
1. **Anonymous usage telemetry ping** — a background, fail-open client that emits an anonymous `{event, lang, lines, status}` ping on `atcr` command completion without capturing source code or blocking execution.
2. **Telemetry opt-out** — `ATCR_TELEMETRY=0` and `atcr config set telemetry false` strictly and verifiably disable all background telemetry.
3. **Persona ID hashing for the Persona Leaderboard** — the scorecard export schema gains a hashed Persona ID field, added via a separate allowlist/path that does not weaken the existing Epic 10.0 leaderboard export's `scrubField` privacy guarantee.
4. **`--sync-cloud` authenticated push** — the flag authenticates via `ATCR_API_KEY` as a Bearer token and pushes the complete anonymized local scorecard (including time/credits saved metrics) to `atcr.dev/dashboard`, exiting with a distinct code on a missing/invalid key.
5. **Telemetry privacy documentation** — `docs/telemetry.md` (new) and `docs/scorecard.md` (updated) document what is collected, the opt-out mechanism, and keep `docs_audit_test.go`'s flag/env coverage check passing.

## Planning Success Criteria
- A background telemetry client exists and safely fails open (never blocks CLI execution on network failure).
- The CLI securely sends anonymous usage events on run completion, capturing no source code.
- `ATCR_TELEMETRY=0` strictly disables all background telemetry, verified by test.
- The exported scorecard schema includes Persona ID hashing for the Persona Leaderboard without weakening the existing Epic 10.0 leaderboard export's privacy guarantees.
- The `--sync-cloud` flag successfully authenticates via `ATCR_API_KEY` (Bearer token) and pushes run history to the designated endpoint, exiting with a distinct code on a missing/invalid key.

## Risk Mitigation
1. **Risk:** Bypassing `scrubField` for telemetry could silently weaken the existing Epic 10.0 public leaderboard export's documented privacy guarantee. **Mitigation:** Implement Persona ID hashing as a separate, explicitly-named schema/function rather than a runtime flag on `scrubField`/`PublicRecord`; add a regression test asserting the leaderboard `--export` path's allowlist and scrubbing behavior is unchanged.
2. **Risk:** A slow or unreachable telemetry/cloud-sync endpoint blocks or delays CLI command completion. **Mitigation:** Run telemetry pings in a goroutine with a short bounded timeout and fire-and-forget semantics (no blocking wait on the main command path); cover with a test that simulates a hung/unreachable endpoint and asserts the command still exits promptly.
3. **Risk:** `--sync-cloud`'s missing/invalid `ATCR_API_KEY` is not clearly distinguishable from other CLI usage errors, breaking scripted/CI detection. **Mitigation:** Define and document a distinct, dedicated exit code for auth failures (not the generic usage-error code), and test both the missing-key and invalid-key paths explicitly.

## Next Steps
1. `/find-documentation @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
2. `/create-documentation @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
3. `/create-user-stories @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
4. `/create-acceptance-criteria @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
5. `/design-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
6. `/create-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`
