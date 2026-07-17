# Code Review Stream - 28.0_telemetry_expansion_cloud_sync (Epic)

**Started:** July 16, 2026 04:21:57PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: A background telemetry client exists and safely fails open (does not block CLI execution if the network drops)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/telemetry/client.go:106-155`
- **Notes:** `Client.Send` dispatches a detached goroutine and returns immediately; the request runs under `context.WithoutCancel` with a 3s bound, so the caller never waits on the network. `send` has a deferred `recover()` (panic-safe), no error return, and every failure mode (non-2xx, transport error, marshal error, panic) is logged at debug and swallowed. Nil client / empty / non-HTTPS endpoint = no-op.

### Criterion: The CLI securely sends anonymous usage events on run completion
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:420-425`, `cmd/atcr/reconcile.go:186-191`, `internal/telemetry/event.go:8-12`, `cmd/atcr/telemetry.go:67-83`
- **Notes:** Ping fires on run completion (review + reconcile) after the outcome is finalized. `Event` is exactly 4 allowlisted fields (event/lang/lines/status), no `omitempty`, deliberately not embedding scorecard; payload built from aggregate counts only (`changedLineCount`, `dominantLang`) — no source, paths, or findings text. "Securely" enforced by `isHTTPS` (plaintext http refused).

### Criterion: `ATCR_TELEMETRY=0` strictly disables all background telemetry
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/main.go:262-277`, `cmd/atcr/telemetry.go:36-64`
- **Notes:** `telemetryEnabledFromEnv` parses via `strconv.ParseBool`; `0`/`false`/`f`/`False` → disabled. `telemetryGate` combines env AND persisted config with strict OR-disable (`telemetryEnabled`): env opt-out always wins, malformed config fails SAFE to disabled. Gate is checked BEFORE `Send` at both call sites, so a disabled run spawns no goroutine and builds no payload.

### Criterion: The exported scorecard schema includes Persona ID hashing for the Persona Leaderboard
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/telemetry.go:26-66`, `internal/scorecard/cloudsync.go:48-62,98-110`
- **Notes:** `HashPersonaID` (SHA-256 hex), `TelemetryPersonaRecord{persona_id_hash, model}`, `NewTelemetryPersonaRecord`, and `CloudSyncPersona.PersonaIDHash` provide persona hashing for the Leaderboard. Implementation deliberately built a SEPARATE schema in `telemetry.go` rather than modifying `export.go`'s `scrubField` (proposed-solution item 5) — the architecturally-correct choice that preserves the Epic 10.0 `PublicRecord` scrub boundary, matching the refine advisory. AC intent (schema includes hashing) satisfied. NOTE: hash is UNSALTED/pseudonymous — self-documented TD (HMAC hardening deferred).

### Criterion: The `--sync-cloud` flag successfully authenticates and pushes run history to a designated endpoint
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/flags.go:47-69`, `cmd/atcr/cloudsync.go:34-49`, `internal/scorecard/cloudsync.go:160-205`, `cmd/atcr/review.go:439-459`, `cmd/atcr/reconcile.go:83,206`
- **Notes:** `--sync-cloud` + `--cloud-endpoint` declared; `resolveSyncCloud` validates fast (missing/blank `ATCR_API_KEY` → `authError` exit 3; bad endpoint → `usageError` exit 2). `scorecard.Push` POSTs JSON with `Authorization: Bearer <key>` (header only, never body/error), 401/403 → `ErrCloudAuthRejected` → exit 3; redirects blocked (`noRedirect`) to prevent Bearer leak on scheme downgrade. Distinct exit codes confirmed (`exitAuth=3`, `exitUsage=2`). Placeholder default endpoint warns rather than silently POSTing.

## Adversarial Analysis (Discovery-Only Mode)

**Mode:** Verification + Discovery (no sprint-design.md risk profile in epic path)
**Files Reviewed:** 13 (sprint-28.0 source files)
**Issues Found:** 20 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 20

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 5
- Low: 14

### Notable findings
- **HIGH** — `internal/registry/telemetry_setting.go:153`: non-atomic stale-lock reclamation (unconditional `RemoveAll` race → two processes run the critical section concurrently → lost update on config.yaml).
- **MED** — `cmd/atcr/review.go:420`: review telemetry `status` is computed pre-gate, so a findings-gate exit-1 records `status="success"`, diverging from the reconcile path (TD-009).
- **MED** — `cmd/atcr/main.go:52` (2 reviewers): fire-and-forget ping is never drained before `os.Exit`; latent-only today because the endpoint is empty, but once wired most pings drop.
- **MED** — config-lock deadline (60s) < stale threshold (300s) leaves a ~4-minute unrecoverable window after a crash-while-locked.
- **MED** — symlink guard TOCTOU + skipped-on-Lstat-error in `SetTelemetrySetting`.
- The core privacy/fail-open/secure-transport guarantees (4-field Event allowlist, HTTPS-only, panic-recovery, header-only Bearer, `noRedirect` scheme-downgrade defense, distinct exit codes) were independently probed and **hold in code**. The unsalted-hash weakness is present but self-documented as deferred TD (HMAC hardening).
