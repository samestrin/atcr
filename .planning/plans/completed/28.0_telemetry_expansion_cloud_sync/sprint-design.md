# Sprint Design: Telemetry Expansion & Cloud Sync

**Created:** July 15, 2026 05:11:33PM
**Plan:** [Plan 28.0: Telemetry Expansion & Cloud Sync](plan.md)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Introduce opt-in, lightweight telemetry to track actual product adoption, gather anonymous data for a crowdsourced Persona Leaderboard, and provide a `--sync-cloud` payload mechanism for the upcoming enterprise SaaS dashboard.

**Referenced Resources:** None — `documentation/source.md` confirms no `.planning/specifications/` entry scored ≥5/10 (telemetry/cloud-sync is net-new functionality with no prior spec coverage); all architectural grounding below comes from live codebase verification instead.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Telemetry & Cloud Sync
**Complexity:** 11/12 (VERY COMPLEX)
**Timeline:** 13 days
**Phases:** 6
**Pattern:** Research & Spike → Foundation → Gating → Advanced → Documentation → Integration & Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
fire-and-forget goroutine HTTP client Go
CLI telemetry opt-out env var pattern
privacy allowlist scrubbing export schema
Bearer token authenticated API push
Cobra config subcommand persistence pattern
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - Codebase has zero prior art for a background/fire-and-forget HTTP client (`internal/telemetry` is a wholly new package and pattern) and zero prior `atcr config` subcommand group; both are new patterns built from stdlib and modeled on existing idioms (`logLevelFromEnv`, `debt.go`'s subcommand-group shape), not a major architectural overhaul.
- **Integration:** 3/3 - Touches 4 internal components (`internal/telemetry` new, `internal/scorecard`, `internal/registry`, `cmd/atcr`) plus a genuine external system (`atcr.dev/dashboard`) whose API contract is owned outside this plan ("requires coordination with whoever owns the `atcr.dev/dashboard` backend endpoint" — plan.md Resource Requirements) — complex multi-system integration.
- **Story/Task & Test:** 3/3 - 5 stories, 19 acceptance criteria, with Stories 3 and 4 marked High complexity, spanning unit + integration coverage including a byte-for-byte regression test on a privacy-critical export path (03-03).
- **Risk/Unknowns:** 3/3 - The cloud-sync endpoint's real auth/response contract is undefined and explicitly out of this plan's scope; the plan carries a documented, high-impact architectural risk (accidentally weakening the Epic 10.0 `scrubField`/`PublicRecord` privacy guarantee); goroutine panic-safety/fail-open correctness is safety-critical and cannot be fully exercised against the real network.

**Time Formula:** Research spike (1d) + Story 1 RGR (2d) + Story 3 RGR (2d) + Story 2 RGR (2d) + Story 4 RGR (3d) + Story 5 docs (1d) + Integration & Validation (2d)
**Calculation:** 1 + 2 + 2 + 2 + 3 + 1 + 2 = 13 days

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** strong
**Suggested command:** `/create-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12. This sprint clears all three thresholds.

---

## Phase Structure

### Phase 1: Research & Spike (1 day)
Validate the `internal/telemetry` client's construction shape (goroutine + bounded `context.Context` + `defer recover()`) and confirm the separate-schema design for Persona ID hashing before any RED tests are written, since neither pattern has codebase precedent. No production code merged in this phase — output is a confirmed design note, not a deliverable.

### Phase 2: Foundation — Telemetry Client & Persona Hashing (4 days)
**Items:** User Story 1 (Anonymous Usage Telemetry Ping), User Story 3 (Persona ID Hashing for the Persona Leaderboard)
**Focus:** Both stories have no dependencies on each other or on any other story in this plan — build them independently, each through a full RED→GREEN→REFACTOR cycle. Story 1 delivers `internal/telemetry`'s `Client.Send` wired (not yet gated) into `runReview`/`runReconcile`. Story 3 delivers `HashPersonaID` and its dedicated schema in `internal/scorecard`, fully isolated from `PublicRecord`/`scrubField`.

### Phase 3: Gating — Telemetry Opt-Out (2 days)
**Items:** User Story 2 (Telemetry Opt-Out)
**Focus:** Depends on Phase 2's Story 1 client existing. Adds `telemetryEnabledFromEnv()` (main.go), `cmd/atcr/config.go`'s `atcr config set telemetry <bool>`, and the `ProjectConfig.Telemetry *bool` field — gating Story 1's client at construction/dispatch entry, before any goroutine spawns. Both opt-out surfaces are a strict OR, never an override precedence.

### Phase 4: Advanced — Cloud Sync Push (3 days)
**Items:** User Story 4 (`--sync-cloud` Authenticated Push)
**Focus:** Depends on Phase 2's Story 1 (HTTP send pattern) and Story 3 (hashed Persona ID field). Adds `addSyncCloudFlags` (flags.go), the new `exitAuth` exit code + `codedError` path (main.go), and a new cloud-sync allowlist schema in `internal/scorecard/export.go` that is NOT a superset of `PublicRecord`. Highest-risk phase — concentrates several high-complexity ACs (04-02, 04-04, plus the shared 03-03 regression it depends on).

### Phase 5: Documentation (1 day)
**Items:** User Story 5 (Telemetry Privacy Documentation)
**Focus:** Sequenced last so it documents the real, finalized flag/env-var/exit-code contracts from Phases 2-4 rather than speculative ones. Adds `docs/telemetry.md` (linked from `docs/README.md`) and updates `docs/scorecard.md`'s Privacy Model section to cross-reference it without contradicting the existing Epic 10.0 guarantee.

### Phase 6: Integration & Validation (2 days)
**Focus:** Cumulative cross-story regression — full `go test ./...`, the AC 03-03 byte-for-byte leaderboard-export regression, `go test ./cmd/atcr/... -run TestDocs` (`TestDocsIndexCoversEveryDoc`, `TestDocsClaimedFlagsAreReal`), the 4-combination opt-out precedence matrix (AC 02-03), coverage gate (baseline 80%), lint/vet, and adversarial risk-profile prep ahead of `/execute-code-review`.

---

## Work Decomposition

| Story | ACs | Unit | Integration | E2E | Complexity | Depends On |
|-------|-----|------|-------------|-----|------------|------------|
| [01 - Anonymous Usage Telemetry Ping](user-stories/01-anonymous-usage-telemetry-ping.md) | 4 | 4 | 0 | 0 | Medium | None |
| [02 - Telemetry Opt-Out](user-stories/02-telemetry-opt-out.md) | 4 | 4 | 3 | 0 | Medium | Story 1 |
| [03 - Persona ID Hashing for the Persona Leaderboard](user-stories/03-persona-id-hashing-for-leaderboard.md) | 4 | 3 | 1 | 0 | High | None |
| [04 - `--sync-cloud` Authenticated Push](user-stories/04-sync-cloud-authenticated-push.md) | 4 | 4 | 2 | 0 | High | Story 1, Story 3 |
| [05 - Telemetry Privacy Documentation](user-stories/05-telemetry-privacy-documentation.md) | 3 | 3 | 0 | 0 | Low | Story 1, 2, 3, 4 |

**Testable elements per story:**
- **Story 1:** `Client.Send` fires from goroutine (01-01); bounded timeout unblocks parent on hang/unreachable network (01-02); `defer recover()` prevents panic propagation (01-03); marshaled payload has exactly `{event, lang, lines, status}` keys (01-04).
- **Story 2:** `ATCR_TELEMETRY=0` zero-request assertion (02-01); `atcr config set telemetry false` persists to `.atcr/config.yaml` and gates a subsequent invocation with no env var set (02-02); 4-combination OR matrix, disabled always wins (02-03); `docs_audit_test.go` flag/env coverage (02-04).
- **Story 3:** `HashPersonaID` determinism/uniqueness/non-reversibility (03-01, 03-04); dedicated schema type separate from `PublicRecord` (03-02); leaderboard `--export` path byte-for-byte unchanged (03-03).
- **Story 4:** `--sync-cloud` flag registered on `review`/`reconcile` (04-01); successful push sends correct `Bearer` header + allowlisted JSON body (04-02); missing key → `exitAuth` (04-03); invalid/rejected key (simulated 401/403) → `exitAuth` (04-04).
- **Story 5:** `docs/telemetry.md` content + `docs/README.md` index link (05-01); `docs/scorecard.md` Privacy Model cross-reference (05-02); `TestDocsIndexCoversEveryDoc`/`TestDocsClaimedFlagsAreReal` pass (05-03).

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Colocated `*_test.go` files next to source (standard Go convention already used by all 351 existing test files in this repo).
**Test File Placement Examples:**
- `internal/telemetry/client_test.go` (Story 1)
- `internal/registry/project_test.go` additions + new `cmd/atcr/config_test.go` (Story 2)
- `internal/scorecard/export_test.go` additions or new `internal/scorecard/telemetry_test.go` (Story 3)
- `cmd/atcr/flags_test.go` additions + `cmd/atcr/main_test.go` additions for `exitAuth` (Story 4)
- `cmd/atcr/docs_audit_test.go` (existing, extended coverage checks — Story 2, Story 5)

**Unit/Integration/E2E:**
- **Unit (18 of 19 ACs):** Table-driven `go test`, `httptest.NewServer` to mock the telemetry/cloud endpoints for timeout, panic-injection, and payload-shape assertions. No new test framework — reuses `go test ./...` (`internal/registry/config.yaml`'s `testing.cmd`).
- **Integration (6 ACs: 02-01, 02-02, 02-03, 03-03, 04-02, 04-04):** Cobra command execution against `httptest` mock servers for opt-out precedence and cloud-sync auth; the leaderboard-export regression (03-03) runs the existing `runLeaderboardExport` path and diffs output byte-for-byte against its pre-change golden output. The integration-heavy ACs are concentrated in the High-complexity Stories 3 and 4.
- **E2E:** None required — CLI/library-level plan with no UI surface (confirmed by `test-planning-matrix.md`).

**Test Environment Status:**
- Framework: `go test` (stdlib `testing`), pattern already colocated — no framework gap.
- Execution: `go test ./...` (per `.planning/.config/config.yaml`); coverage via `go test -coverprofile=coverage.out ./...`.
- Coverage Tools: Go's built-in coverage profiling; baseline 80% (existing project threshold).

---

## Architecture

**Primitives:**
- `TelemetryEvent{Event, Lang string; Lines int; Status string}` (new, `internal/telemetry`) — the sole allowlisted ping payload shape.
- `HashPersonaID(raw string) string` (new, `internal/scorecard`) — deterministic SHA-256 hex digest, stdlib-only.
- A dedicated cloud-sync/telemetry-persona record type (new, `internal/scorecard`) — distinct from `PublicRecord`, carries the hashed Persona ID plus time/credits-saved metrics.
- `ProjectConfig.Telemetry *bool` (new field, `internal/registry/project.go`) — pointer idiom matching `Sandbox`/`AutoFix`/`MaxParallel` so an explicit `false` survives default application.
- `exitAuth` exit code constant + `codedError` value (new, `cmd/atcr/main.go`) — alongside existing `exitFailure=1`/`exitUsage=2`.

**Module Boundaries:**
- `internal/telemetry` exposes only a `Client.Send(ctx, TelemetryEvent)`-style call; HTTP transport, timeout, and panic recovery stay hidden inside it so call sites (`runReview`, `runReconcile`) never touch `net/http` directly.
- `internal/scorecard` gains `HashPersonaID` and the new cloud-sync/telemetry schema as pure, stateless additions; `PublicRecord`, `scrubField`, `AnonymizeRecord`, `ScrubPublicRecord`, and `runLeaderboardExport` (verified in `cmd/atcr/leaderboard.go:156` and `internal/scorecard/export.go`) remain untouched in signature and behavior.
- `cmd/atcr` gains `newConfigCmd`/`newConfigSetCmd` (new `config.go`, modeled on the verified `debt.go:newDebtCmd` subcommand-group shape) and `addSyncCloudFlags` (new helper in `flags.go`, modeled on the verified `addRangeFlags`).

**External Dependencies:**
- `net/http` (stdlib) — wrapped entirely inside `internal/telemetry`'s client and the cloud-sync push path; no direct `net/http` calls from `cmd/atcr` command bodies.
- `crypto/sha256` (stdlib) — wrapped inside `HashPersonaID`; no other caller touches the hash algorithm directly.
- `atcr.dev/dashboard` (external service, contract owned outside this plan) — accessed only through the wrapped HTTP client, never called inline from command handlers, so the endpoint is swappable via a documented `--cloud-endpoint`-style override for testing.

**Replaceability:** `internal/telemetry`'s `Client` is constructor-injectable so tests substitute an `httptest` mock transport without touching call sites; the cloud-sync payload builder in `internal/scorecard` is a pure function independent of HTTP transport, testable and replaceable in isolation from the network layer.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Telemetry payload construction (`internal/telemetry`) | Event schema marshaling | Accidental inclusion of source code, file paths, or identifiers beyond the 4 allowlisted fields | Dedicated narrow struct (`event`, `lang`, `lines`, `status` only); unit test asserting marshaled JSON has exactly these keys |
| Persona ID hashing (`internal/scorecard`) | `HashPersonaID` | Reversibility of low-entropy persona names via lookup/rainbow-table attack | SHA-256 one-way hash; unit tests assert non-reversibility (raw string never appears in output or logs); documented as a hash, not encryption |
| `--sync-cloud` auth (`cmd/atcr`) | `ATCR_API_KEY` handling | Key leakage via logs/error messages; missing key silently proceeding; invalid key silently accepted | Trim + validate key before use; dedicated `exitAuth` code on missing/invalid; never log the raw key; HTTPS-only endpoint |
| Existing leaderboard export boundary (`internal/scorecard/export.go`) | `scrubField`/`PublicRecord` | New telemetry/cloud-sync code paths accidentally bypassing or extending the existing allowlist | Byte-for-byte regression test (AC 03-03); distinct naming/doc comments enforced at code review per Story 3's stated risk |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Telemetry ping dispatch (`runReview`/`runReconcile` completion) | Every `review`/`reconcile` invocation | No perceptible added latency; bounded ~2-3s timeout, fully backgrounded | Goroutine + `context.Context` timeout; fire-and-forget; zero blocking wait on the main command path |
| `--sync-cloud` push | Explicit, per-invocation only when flag set | Synchronous but must not corrupt an already-finalized command outcome; bounded request timeout | Executed after the run's primary outcome (exit code, local scorecard write) is finalized; short HTTP client timeout; failure surfaces as a separate, clearly-attributed error |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Network failure/hang | DNS failure, connection refused, hung TCP, slow TLS handshake | Telemetry: fails open silently, command exits normally within bounded time. Cloud sync: error surfaced, but only auth failures (missing/invalid key) get the dedicated `exitAuth` code — other network errors don't corrupt the already-completed run outcome |
| Panic inside telemetry goroutine | Marshal panic, nil deref, closed channel send | Recovered via `defer recover()`; parent command exits normally with no crash |
| Opt-out combinations | {env unset/0} × {config true/false} — 4 combinations | Strict OR: disabled wins whenever either surface says off; no precedence/override chain |
| Auth failure modes | Missing `ATCR_API_KEY`, empty string, invalid/expired key (simulated 401/403) | All produce the dedicated `exitAuth` code, distinct from `exitUsage` |
| Config command misuse | `atcr config set` with a wrong key, non-bool value, or missing args | `usageError` (`exitUsage`), never silently ignored |

### Defensive Measures Required

- **Input Validation:** `ATCR_API_KEY` trimmed and checked non-empty before use; `atcr config set` validates the key is exactly `telemetry` and the value parses as bool.
- **Error Handling:** Existing `codedError`/`errors.As` dispatch pattern extended with `exitAuth`; telemetry goroutine wrapped in `defer recover()` per `implementation-standards.md`'s Panic Safety guidance.
- **Logging/Audit:** Telemetry send failures logged at debug/trace only, never surfaced to the user; raw API key and pre-hash Persona ID never logged.
- **Rate Limiting:** Out of scope — one ping per command completion, no bulk-sync loop in this plan.
- **Graceful Degradation:** Telemetry always fails open regardless of failure mode; `--sync-cloud` failure surfaces as a distinct, non-fatal-to-the-already-completed-run error.

---

## Risks

**Technical:**
- Bypassing `scrubField` for telemetry could silently weaken the Epic 10.0 public leaderboard export's documented privacy guarantee → mitigate with a separate, explicitly-named schema/function plus the AC 03-03 regression test.
- A slow/unreachable telemetry or cloud-sync endpoint blocks or delays CLI completion → mitigate with goroutine + bounded timeout (telemetry) and a short request timeout executed after the run's outcome is finalized (`--sync-cloud`).
- Ambiguous auth-failure exit codes for `--sync-cloud` break scripted/CI detection → mitigate with a dedicated `exitAuth` code tested against both missing-key and invalid-key paths.

**TDD-Specific:**
- Stories 1 and 3 have no shared dependency and should be run through independent RED→GREEN→REFACTOR cycles in parallel within Phase 2 rather than serialized, to avoid one story's refactor churn blocking the other.
- Story 4's tests must be written against Story 1's actual client interface and Story 3's actual hash output — sequencing Phase 4 strictly after Phase 2 avoids writing GREEN-phase code against a moving foundation.
- Story 5 (documentation) must not start its RED phase (the `docs_audit_test.go` assertions) until Phases 2-4's real flag/env-var/exit-code names are finalized, per the story's own stated risk of documenting aspirational rather than real behavior.

---

**Next:** `/create-sprint @.planning/plans/active/28.0_telemetry_expansion_cloud_sync/ --gated`
