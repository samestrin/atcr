# Sprint 28.0: Telemetry Expansion Cloud Sync

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 28.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting — EXCEPT this sprint runs in **gated mode**: stop at each phase-boundary GATE (`N.LAST`) for review before continuing.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-07-15)

**Key Decisions:**
- Gated run scope: this `/execute-sprint` invocation executes **Phase 1 (Research & Spike) only**, then HARD STOPs at the `1.LAST` gate before Phase 2. Phase 1 merges no production code — its deliverable is a confirmed design note.
- "Isolated from `PublicRecord`" is interpreted as a brand-new struct in a new file (`internal/scorecard/telemetry.go`), never a field added to `PublicRecord` nor a bypass flag on `scrubField`.
- stdlib-only implementation (`net/http`, `encoding/json`, `context`, `crypto/sha256`) — no new third-party dependency.

**Scope Boundaries:**
- IN scope: CLI telemetry emission, Persona ID hashing, `--sync-cloud` push, opt-out gate, privacy docs.
- OUT of scope: building the `atcr.dev/dashboard` SaaS UI (per original-requirements Out of Scope). The real cloud endpoint auth/response contract is owned outside this plan; all tests use `httptest` mocks + a `--cloud-endpoint` override — zero live network in CI.

**Technical Approach (verified against live code 2026-07-15):**
- Construction/exit seams exist as planned: `newRootCmd` (main.go:128), `logLevelFromEnv` (:216), `exitFailure`/`exitUsage` (:84-85), `codedError` (:89).
- Call sites exist: `runReview` (review.go:170), `runReconcile` (reconcile.go:71), `EmitForReconcile` (reconcile.go:148).
- Privacy boundary intact and locatable: `PublicRecord` (export.go:35), `AnonymizeRecord` (:143), `ScrubPublicRecord` (:156), `scrubField` (:321); `Record.Reviewer` (scorecard.go:56).
- Idioms to model on: `newDebtCmd` (debt.go:26), `addRangeFlags` (flags.go:14), `ProjectConfig` pointer fields (project.go:74-89).

**Unvalidated assumptions:** None — every cited file/symbol/line verified before execution.

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

An opt-in, lightweight telemetry system for `atcr` that emits anonymous usage pings on `review`/`reconcile` completion, hashes Persona IDs for a crowdsourced Persona Leaderboard, and adds a `--sync-cloud` flag that authenticates via `ATCR_API_KEY` to push anonymized scorecard data (including time/credits-saved metrics) to the upcoming `atcr.dev/dashboard`. A strict `ATCR_TELEMETRY=0` env var and `atcr config set telemetry false` command let privacy-conscious teams fully opt out.

### Why This Matters

`atcr` currently has no visibility into real-world adoption, run-success rates, or which community personas perform best in practice — and no mechanism exists yet to demonstrate ROI to engineering managers evaluating the tool at team scale.

### Key Deliverables

- `internal/telemetry` fire-and-forget, panic-safe HTTP client wired into `runReview`/`runReconcile` (Story 1)
- `ATCR_TELEMETRY=0` env var + `atcr config set telemetry <bool>` persisted opt-out, strict-OR precedence (Story 2)
- `HashPersonaID` deterministic SHA-256 hashing + dedicated telemetry/cloud-sync schema, isolated from `PublicRecord`/`scrubField` (Story 3)
- `--sync-cloud` flag with `ATCR_API_KEY` Bearer-token auth, dedicated `exitAuth` exit code, and a new cloud-sync allowlist schema (Story 4)
- `docs/telemetry.md` + updated `docs/scorecard.md` Privacy Model documenting the new data paths and opt-out mechanisms (Story 5)

### Success Criteria

- Background telemetry never blocks or crashes the CLI (bounded timeout + `defer recover()`), verified by tests simulating network hangs and panics
- `ATCR_TELEMETRY=0` and `atcr config set telemetry false` each independently produce zero telemetry HTTP requests; the two surfaces are strict-OR'd across all 4 combinations
- The Epic 10.0 leaderboard `--export` path's output remains byte-for-byte unchanged (AC 03-03 regression)
- `--sync-cloud` sends a `Bearer`-authenticated push with an allowlisted JSON body; missing/invalid `ATCR_API_KEY` exits with a dedicated `exitAuth` code
- `docs/telemetry.md` is indexed and every documented flag/env var is real (`go test ./cmd/atcr/... -run TestDocs` passes)

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** STRICT 🔒 (RED → GREEN → ADVERSARIAL → REFACTOR) for all elements.

**Rationale:** Complexity 11/12 (VERY COMPLEX) maps to STRICT TDD. Each story gets comprehensive failing tests first, minimal implementation to green, a fresh-subagent adversarial review, then a refactor that incorporates CRITICAL/HIGH findings inline.

**Adversarial Review:** ENABLED 🎯 — Inline-fix severities: **CRITICAL/HIGH**. Deferred to tech debt: **MEDIUM/LOW**.

**Execution Mode:** GATED 🚧 — `/execute-sprint` stops at each phase-boundary GATE (`N.LAST`) after the phase DoD.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/source.md](plan/documentation/source.md) | Documentation scan index (no specs scored ≥5/10 — net-new functionality) |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/<pkg>/ -run <TestName>` |
| T2: Module | After completing element | `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `golangci-lint run` no errors; `go vet ./...` clean
4. Build: `go build ./...` succeeds
5. Docs: Updated where applicable

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

Follow the project standards (read before coding):

- [implementation-standards.md](../../../specifications/implementation-standards.md) — implementation conventions
- [coding-standards.md](../../../specifications/coding-standards.md) — Go style, naming, error handling
- [git-strategy.md](../../../specifications/git-strategy.md) — branch + commit conventions

**Branch:** `feature/28.0_telemetry_expansion_cloud_sync` (push deferred to `/finalize-sprint`).

**Key conventions for this sprint:**
- Go stdlib `net/http`, `encoding/json`, `context`, `crypto/sha256` only — no new third-party dependency required.
- Table-driven `go test`; `httptest.NewServer` mocks the telemetry/cloud endpoints for timeout, panic-injection, and payload-shape assertions — zero live network calls in CI.
- Integration tests (Cobra command execution against `httptest` mocks) tag with `//go:build integration` where the repo convention applies.
- Panic safety: every goroutine wraps its body in `defer recover()` per `implementation-standards.md`'s Panic Safety guidance.
- `PublicRecord`, `scrubField`, `AnonymizeRecord`, `ScrubPublicRecord`, and `runLeaderboardExport` in `internal/scorecard/export.go` / `cmd/atcr/leaderboard.go:156` must remain byte-for-byte unchanged in signature and behavior — verified by the AC 03-03 regression test.
- Pointer idiom for new optional config fields (`ProjectConfig.Telemetry *bool`), matching the existing `Sandbox`/`AutoFix`/`MaxParallel` fields so an explicit `false` survives default application.

---

## External Resources

None — [plan/documentation/source.md](plan/documentation/source.md) confirms no `.planning/specifications/` entry scored ≥5/10 (telemetry/cloud-sync is net-new functionality). All architectural grounding comes from live codebase verification recorded in [plan/sprint-design.md](plan/sprint-design.md)'s Architecture section.

---

## Sprint Phases

> **Pre-implementation grep (all phases):** Before writing RED tests in any phase, run `grep -rn "scrubField\|PublicRecord\|AnonymizeRecord\|ScrubPublicRecord" internal/scorecard/export.go` to confirm the exact current signatures before touching adjacent code — these must remain byte-for-byte unchanged (Story 3 constraint, verified by AC 03-03).

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Research & Spike (Day 1)

**Focus:** Validate the `internal/telemetry` client's construction shape (goroutine + bounded `context.Context` + `defer recover()`) and confirm the separate-schema design for Persona ID hashing before any RED tests are written, since neither pattern has codebase precedent. No production code merged in this phase — output is a confirmed design note, not a deliverable.

### 1.1 [x] **[Telemetry Client + Persona-Hash Schema Design Spike](plan/sprint-design.md)**
   **Design note:** `.planning/.temp/execute-sprint/phase1-design-note.md` (confirmed 2026-07-15). All 3 shapes confirmed: (a) `telemetry.Client.Send` goroutine + bounded 2-3s ctx + `defer recover()`, gate-seam inside `Send` so Story 2 adds no call-site reshape; (b) `HashPersonaID` in new `internal/scorecard/telemetry.go`, no `PublicRecord` field / no `scrubField` bypass; (c) `CloudSyncRecord` NOT a `PublicRecord` superset, consumes Story 3 hash unmodified. GAP surfaced → TD-001 (time/credits-saved metric absent from codebase; Phase-4-scoped, non-blocking).
   1. Draft the `internal/telemetry.Client` construction shape: goroutine + bounded `context.Context` (2-3s timeout) + `defer recover()`; confirm it can be constructed once at `cmd/atcr/main.go:newRootCmd` and later gated to a no-op (Story 2) without reshaping the `runReview`/`runReconcile` call sites.
   2. Draft the dedicated Persona ID hashing shape (`HashPersonaID(raw string) string` + a new telemetry/cloud-sync record type) and confirm it stays structurally isolated from `PublicRecord`/`scrubField`/`AnonymizeRecord`/`ScrubPublicRecord` in `internal/scorecard/export.go`.
   3. Confirm the cloud-sync allowlist schema (Story 4) is NOT a superset of `PublicRecord` and can carry the Story 3 hashed Persona ID plus time/credits-saved metrics.
   4. Record the confirmed shapes as a design note (in this task's checkbox comment or a scratch file under `.planning/.temp/`) — no RED tests are written elsewhere until this spike confirms all three shapes.
   **Files:** none (design note only) | **Duration:** 1 day

### 1.2 [x] **Phase 1 — DoD Validation**
   - No production code changed this phase — `go build ./...` and `go test ./...` remain green at baseline (no regression risk)
   - Design note confirms: (a) telemetry client construction shape, (b) persona-hash schema isolation from `PublicRecord`, (c) cloud-sync schema is not a `PublicRecord` superset
   - DoD report:
     ```
     Phase-1 (Research Spike) DoD Complete
     Auto: N/A (no code changed) | Story-Specific: 3/3 (client shape, hash-schema isolation, cloud-sync schema confirmed)
     Manual Review: [ ] Design note reviewed
     ```

### 1.LAST [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** The Phase 1 design note (no code changed)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - The Phase 1 design note content (paste inline — no files changed)
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does the proposed `Client` construction shape avoid reshaping call sites when Story 2's opt-out gate is added later?
       - CONFIG SURFACE: Does the proposed hash schema avoid any new field on `PublicRecord` or any bypass flag on `scrubField`?
       - INTEGRATION: Can Story 4's cloud-sync payload consume Story 3's hash output without modification?
       - PHASE-EXIT CONTRACT: Can Phase 2 build both stories independently from this design note with no unresolved ambiguity?
       - REGRESSION: N/A — no code changed.
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (final, round 4 — no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | _(none)_ | — | No CRITICAL/HIGH after 4 fresh-subagent gate rounds | — |

   **Phase gate passed.** The gate ran 4 rounds against a fresh subagent each time (design note: `.planning/.temp/execute-sprint/phase1-design-note.md`). Rounds 1-3 caught and resolved: (r1) `enabled` frozen at construction couldn't honor persisted config opt-out → per-run `telemetryGate(cmd)`; (r2) bare-func RunE seam + reconcile has no config in scope → package-var `telemetryClient` + helper self-loads `registry.ProjectConfig`; (r3) nil-receiver panic in tests → `if c == nil` guard in `Send`. Round 4: **no CRITICAL/HIGH**. Remaining 2 MEDIUM + 2 LOW captured as **TD-002..TD-005** in `tech-debt-captured.md` (empty-endpoint no-op, global-client test-isolation, dead `ctx` param, reconcile event-field derivation) and folded into the design note's Phase 2 Build Guidance.

   **Action Required:**
   - CRITICAL/HIGH found -> Revise the design note before Phase 2 starts, do NOT proceed until resolved. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 2: Foundation — Telemetry Client & Persona Hashing (Days 2-5)

**Focus:** User Story 1 (Anonymous Usage Telemetry Ping) and User Story 3 (Persona ID Hashing for the Persona Leaderboard) have no dependencies on each other or on any other story in this plan — build them independently, each through a full RED→GREEN→ADVERSARIAL→REFACTOR cycle.

### Story 1: Anonymous Usage Telemetry Ping

**Story:** [01 - Anonymous Usage Telemetry Ping](plan/user-stories/01-anonymous-usage-telemetry-ping.md) | **ACs:** [01-01](plan/acceptance-criteria/01-01-fire-and-forget-telemetry-send.md), [01-02](plan/acceptance-criteria/01-02-bounded-non-blocking-timeout.md), [01-03](plan/acceptance-criteria/01-03-panic-safe-fail-open.md), [01-04](plan/acceptance-criteria/01-04-schema-constrained-payload.md)

### 2.1 [x] **[Telemetry Client - RED](plan/user-stories/01-anonymous-usage-telemetry-ping.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestClient_Send_FiresFromGoroutine` — call returns immediately, request observed asynchronously via `httptest.NewServer` (01-01)
   - `TestClient_Send_BoundedTimeout_UnblocksOnHangOrUnreachable` — hung/unreachable mock endpoint, parent command still exits promptly (01-02)
   - `TestClient_Send_RecoversFromInternalPanic` — forced panic inside the goroutine body, parent does not crash (01-03)
   - `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` — marshaled JSON asserted to contain only `event`, `lang`, `lines`, `status` (01-04)
   **Files:** `internal/telemetry/client_test.go` | **Duration:** 1 day

### 2.2 [x] **[Telemetry Client - GREEN](plan/user-stories/01-anonymous-usage-telemetry-ping.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `internal/telemetry/client.go` (new package) — `Client` type with a goroutine-based `Send(ctx context.Context, event TelemetryEvent)`; bounded `context.Context` (2-3s timeout); `defer recover()` around the goroutine body per `implementation-standards.md`'s Panic Safety guidance
   - `TelemetryEvent{Event, Lang string; Lines int; Status string}` — the sole allowlisted payload struct
   - `cmd/atcr/main.go:newRootCmd` — construct the `Client` once, following the existing `logLevelFromEnv`-adjacent construction pattern (~line 217)
   - `cmd/atcr/review.go:runReview` (~line 170) and `cmd/atcr/reconcile.go:runReconcile` (~line 71) — invoke `Client.Send` alongside (not replacing) existing non-fatal side effects such as `scorecard.EmitForReconcile`
   COMMIT: `git commit -m "feat(telemetry): fire-and-forget client wired into review/reconcile (green)"`
   **Files:** `internal/telemetry/client.go`, `cmd/atcr/main.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go` | **Duration:** 1.5 days

### 2.2.A [x] **[Telemetry Client - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-anonymous-usage-telemetry-ping.md)**
   **Changed Files:** `internal/telemetry/client.go`, `cmd/atcr/main.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `internal/telemetry/client_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? Any source code, file paths, or identifiers leaking beyond the 4 allowlisted fields?
       - EDGE CASES: Null, empty, boundaries, concurrent access? Multiple overlapping `Send` calls from repeated `review`/`reconcile` runs?
       - ERROR HANDLING: Missing catches, swallowed errors? Is the panic-recovery genuinely unreachable-from-parent, or does it leak via a shared channel/waitgroup?
       - PERFORMANCE: N+1, leaks, blocking ops? Any accidental blocking wait on the main command path?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/main.go:46-52 | Latent non-delivery: `main()` returns/`os.Exit` immediately after Send with no `Wait()` drain, so once a real endpoint lands the in-flight POST is killed mid-flight (~0% delivery). Harmless today (empty endpoint = no-op). | Bounded drain in `main()` before exit — captured as TD-006 (deferred: feature is a no-op this sprint; drain wiring pairs with the real-endpoint decision). |
   | LOW | internal/telemetry/client.go:48 | `New` shares process-global `http.DefaultClient`. | Fixed in 2.3: dedicated `&http.Client{}`. |
   | LOW | internal/telemetry/client.go:94 | Response body closed but not drained (blocks keep-alive reuse). | Fixed in 2.3: drain via `io.Copy(io.Discard, ...)`. |
   | LOW | internal/telemetry/client.go:58 | Case-sensitive `https://` prefix check silently no-ops `HTTPS://`. | Fixed in 2.3: `net/url` scheme compare. |

   **Action taken:** No CRITICAL/HIGH — proceed. MEDIUM → TD-006. The 3 LOWs are trivial pure-quality hardening, fixed inline in 2.3 REFACTOR (resolved > deferred).

### 2.3 [x] **[Telemetry Client - REFACTOR](plan/user-stories/01-anonymous-usage-telemetry-ping.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code quality (T1); confirm the client exposes only `Send`/construction — no `net/http` leakage into `cmd/atcr` call sites
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(telemetry): address review + clean up"`
   **Duration:** 0.5 day

### Story 3: Persona ID Hashing for the Persona Leaderboard

**Story:** [03 - Persona ID Hashing for the Persona Leaderboard](plan/user-stories/03-persona-id-hashing-for-leaderboard.md) | **ACs:** [03-01](plan/acceptance-criteria/03-01-hashed-persona-id-function.md), [03-02](plan/acceptance-criteria/03-02-telemetry-persona-schema.md), [03-03](plan/acceptance-criteria/03-03-leaderboard-export-regression.md), [03-04](plan/acceptance-criteria/03-04-hash-property-unit-tests.md)

### 2.4 [x] **[Persona ID Hashing - RED](plan/user-stories/03-persona-id-hashing-for-leaderboard.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestHashPersonaID_Deterministic` — same input across repeated calls and simulated process restarts produces identical output (03-01, 03-04)
   - `TestHashPersonaID_UniquenessAcrossDifferentInputs` — different Persona IDs produce different hashes (03-04)
   - `TestHashPersonaID_NonReversible` — raw Persona ID string never appears in the hash output or any log/error message on that path (03-04)
   - `TestTelemetryPersonaSchema_SeparateFromPublicRecord` — new schema type has no shared fields/embedding with `PublicRecord` (03-02)
   - `TestRunLeaderboardExport_ByteForByteRegression` — golden-file diff of `runLeaderboardExport`'s existing `--export` output before/after this story's changes (03-03)
   **Files:** `internal/scorecard/export_test.go` (regression) + new `internal/scorecard/telemetry_test.go` (hashing/schema) | **Duration:** 1 day

### 2.5 [x] **[Persona ID Hashing - GREEN](plan/user-stories/03-persona-id-hashing-for-leaderboard.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `internal/scorecard/telemetry.go` (new file, kept out of `export.go` to reduce risk of accidental coupling to `PublicRecord`) — `HashPersonaID(raw string) string` via stdlib `crypto/sha256`, sourced from `Record.Reviewer` (`internal/scorecard/scorecard.go:52`), same field `AnonymizeRecord` already reads; explicit doc comment stating this is NOT part of the `PublicRecord` allowlist path
   - A dedicated telemetry/cloud-sync-scoped record type in the same file — distinct from `PublicRecord`, carries the hashed Persona ID plus time/credits-saved metrics
   - `PublicRecord`, `scrubField`, `AnonymizeRecord`, `ScrubPublicRecord`, and `runLeaderboardExport` (`cmd/atcr/leaderboard.go:156`) remain untouched in signature and behavior
   COMMIT: `git commit -m "feat(scorecard): deterministic Persona ID hashing + dedicated telemetry schema (green)"`
   **Files:** `internal/scorecard/telemetry.go` | **Duration:** 1.5 days

### 2.5.A [x] **[Persona ID Hashing - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-persona-id-hashing-for-leaderboard.md)**
   **Changed Files:** `internal/scorecard/telemetry.go`, `internal/scorecard/telemetry_test.go`, `internal/scorecard/export_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.5]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? Does any code path log or return the raw (pre-hash) Persona ID?
       - EDGE CASES: Null, empty, boundaries, concurrent access? Empty-string Persona ID, unicode input, extremely long input?
       - ERROR HANDLING: Missing catches, swallowed errors? Does `HashPersonaID` ever panic on malformed input?
       - PERFORMANCE: N+1, leaks, blocking ops? Confirm `PublicRecord`/`scrubField`/`AnonymizeRecord`/`ScrubPublicRecord`/`runLeaderboardExport` signatures are byte-for-byte unchanged from `main`.
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | internal/scorecard/telemetry.go:18 | Unsalted plain SHA-256 over low-entropy, enumerable persona names (`Record.Reviewer`, e.g. "bruce") is dictionary/rainbow-reversible — "non-reversible" is overstated. | Reviewer's option A (HMAC+pepper) **conflicts with the AC**: AC 03-01 EC1 pins the empty-string digest to plain-SHA-256 `e3b0c44…`, AC 03-04 pins plain digests, and no secret pepper is provisioned (out of Phase 2 scope). Applied reviewer's option B inline in 2.6: refine the docstring to accurately state the pseudonymization guarantee and its bound. HMAC hardening deferred → **TD-007** (pairs with the real-endpoint decision, needs secret + AC change). |
   | LOW | internal/scorecard/telemetry.go:30 | `Model` copied through unhashed on an asserted "non-PII, already public" assumption; a future free-form/fine-tuned model id could carry sensitive data. | Deferred → **TD-008** (enforce Model is a known non-sensitive enum at the payload boundary). |

   **Action taken:** HIGH triaged — the spec-compatible part of the reviewer's own recommendation (accurate guarantee wording) applied inline in 2.6; the algorithm change (HMAC+pepper) contradicts the AC-pinned plain-SHA-256 digests and needs out-of-scope secret provisioning, so deferred as **TD-007** with rationale. LOW → **TD-008**. Plain SHA-256 kept per AC 03-01/03-04. **Flagged for user decision at the 2.LAST gate.**

### 2.6 [x] **[Persona ID Hashing - REFACTOR](plan/user-stories/03-persona-id-hashing-for-leaderboard.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
   2. Improve code quality (T1); confirm no accidental import/coupling between `telemetry.go` and `PublicRecord`'s allowlist path
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(scorecard): address review + clean up"`
   **Duration:** 0.5 day

### 2.7 [x] **Phase 2 — DoD Validation**
   - `go test ./internal/telemetry/... ./internal/scorecard/...` (T3 scoped) — all passing
   - `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 errors
   - Coverage: `internal/telemetry`, `internal/scorecard` both ≥80%
   - AC 03-03 regression: `runLeaderboardExport`'s `--export` output byte-for-byte unchanged
   - DoD report:
     ```
     Story-01 + Story-03 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 4/4 (Story 1) + 4/4 (Story 3)
     Manual Review: [ ] Code reviewed
     ```

### 2.LAST [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `internal/telemetry.Client.Send` and `internal/scorecard.HashPersonaID` signatures stable and ready for Phase 3/4 consumption?
       - CONFIG SURFACE: No new config keys introduced this phase (deferred to Phase 3) — confirmed?
       - INTEGRATION: `Client` constructed once at root-command time without reshaping `runReview`/`runReconcile`; hashing function isolated from `PublicRecord`?
       - PHASE-EXIT CONTRACT: Can Phase 3 gate the client at construction/dispatch entry without further rework? Can Phase 4 consume the hash output directly?
       - REGRESSION: Existing `PublicRecord`/`scrubField`/leaderboard `--export` behavior fully intact (byte-for-byte)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose integration reviewer — no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | cmd/atcr/reconcile.go:163 | Reconcile telemetry status hardcoded `"success"`, fired before the gate — cannot distinguish gate-passed/failed reconciles (asymmetric with review). | Deferred → **TD-009**. Non-blocking. |

   **Phase gate passed.** Build/vet/tests green across `internal/telemetry`, `internal/scorecard`, `cmd/atcr`; `git diff main --stat` empty for `export.go`/`leaderboard.go` (leaderboard `--export` byte-for-byte intact, pinned by the AC 03-03 checksum regression). Contracts stable: `Client.Send` short-circuits (`if c == nil || !isHTTPS`) before `wg.Add`/goroutine spawn, so Phase 3's opt-out gates at the same `PersistentPreRunE`/`FromContext` seam with zero call-site rework; `HashPersonaID` isolated in `scorecard/telemetry.go`, ready for Phase 4. Only LOW → TD-009.

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 3: Gating — Telemetry Opt-Out (Days 6-7)

**Focus:** User Story 2 (Telemetry Opt-Out). Depends on Phase 2's Story 1 client existing. Adds `telemetryEnabledFromEnv()` (main.go), `cmd/atcr/config.go`'s `atcr config set telemetry <bool>`, and the `ProjectConfig.Telemetry *bool` field — gating Story 1's client at construction/dispatch entry, before any goroutine spawns. Both opt-out surfaces are a strict OR, never an override precedence.

**Story:** [02 - Telemetry Opt-Out](plan/user-stories/02-telemetry-opt-out.md) | **ACs:** [02-01](plan/acceptance-criteria/02-01-env-var-disables-telemetry.md), [02-02](plan/acceptance-criteria/02-02-config-set-telemetry-persists.md), [02-03](plan/acceptance-criteria/02-03-opt-out-surfaces-or-not-override.md), [02-04](plan/acceptance-criteria/02-04-docs-and-flag-coverage.md)

### 3.1 [x] **[Telemetry Opt-Out - RED](plan/user-stories/02-telemetry-opt-out.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestTelemetryEnabledFromEnv_ZeroDisables` — `ATCR_TELEMETRY=0` (and falsy equivalents `false`/`f`/`F`/`False`/`FALSE`) parsed via `strconv.ParseBool` disables; unset/unparseable defaults to enabled (02-01)
   - `TestReview_WithEnvVarDisabled_ZeroHTTPRequests` — `review`/`reconcile` against a mock telemetry endpoint results in zero HTTP requests when `ATCR_TELEMETRY=0` (02-01)
   - `TestConfigSetTelemetry_PersistsToProjectConfig` — `atcr config set telemetry false` persists `Telemetry: false` to `.atcr/config.yaml` via `ProjectConfig` (02-02)
   - `TestConfigSetTelemetry_SubsequentInvocationGatedWithNoEnvVar` — after persisting `false`, a later `atcr review` with no env var set still makes zero HTTP requests (02-02)
   - `TestTelemetryOptOut_FourCombinationMatrix` — {env unset/0} × {config true/false}, disabled wins whenever either surface says off, no override chain (02-03)
   - `TestConfigSet_RejectsNonTelemetryKey` / `TestConfigSet_RejectsNonBoolValue` — `usageError` (`exitUsage`), never silently ignored
   - `TestDocsAudit_ATCRTelemetryEnvVarCoverage` / `TestDocsAudit_ConfigSetTelemetryFlagCoverage` — `docs_audit_test.go` coverage extension for the new env var/command (02-04). In Phase 3 these assert the `atcr config set` `Long`/`--help` text (real this phase); the `docs/telemetry.md` content fact-check they author is **validated in Phase 5 (AC 05-03)** once Story 5 creates the doc. Do NOT create `docs/telemetry.md` in Phase 3 — it is owned solely by Story 5 (task 5.2).
   **Files:** `cmd/atcr/main_test.go`, `cmd/atcr/config_test.go` (new), `internal/registry/project_test.go`, `cmd/atcr/docs_audit_test.go` | **Duration:** 1 day

### 3.2 [x] **[Telemetry Opt-Out - GREEN](plan/user-stories/02-telemetry-opt-out.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `cmd/atcr/main.go` — add `telemetryEnabledFromEnv() bool` beside `logLevelFromEnv` (~line 216-217), read once at root-command construction time; `ATCR_TELEMETRY` names the enabled state directly (0/false disables, 1/true/unset enables) — inverse boolean direction of `ATCR_DISABLE_AST_GROUPING`, documented explicitly in the doc comment
   - `cmd/atcr/config.go` (new) — `newConfigCmd()` (`Use: "config"`, `RunE: cmd.Help`) modeled on `cmd/atcr/debt.go:newDebtCmd`; child `newConfigSetCmd()` (`Use: "set"`, `Args: usageArgs(cobra.ExactArgs(2))`) validating the key is exactly `telemetry` (else `usageError`) and the value parses as bool; registered in `newRootCmd`'s `AddCommand` list (~line 185-208)
   - `internal/registry/project.go` — add `Telemetry *bool` field on `ProjectConfig` (pointer idiom matching `Sandbox`/`AutoFix`/`MaxParallel`); load/mutate/rewrite `.atcr/config.yaml` via `DefaultProjectConfigPath` (~line 93)
   - `internal/telemetry/client.go` — add the construction/dispatch seam so the disabled state short-circuits before any goroutine spawns, not merely before the HTTP call fires; gate is a strict OR of env-var-disabled and config-disabled, disabled always wins
   COMMIT: `git commit -m "feat(telemetry): strict OR opt-out via env var + persisted config (green)"`
   **Files:** `cmd/atcr/main.go`, `cmd/atcr/config.go`, `internal/registry/project.go`, `internal/telemetry/client.go` | **Duration:** 1.5 days

### 3.2.A [x] **[Telemetry Opt-Out - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-telemetry-opt-out.md)**
   **Changed Files:** `cmd/atcr/main.go`, `cmd/atcr/config.go`, `internal/registry/project.go`, `internal/telemetry/client.go`, `cmd/atcr/main_test.go`, `cmd/atcr/config_test.go`, `internal/registry/project_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? Can `atcr config set` be used to write arbitrary keys beyond `telemetry`?
       - EDGE CASES: Null, empty, boundaries, concurrent access? All 4 combinations of {env unset/0} x {config true/false} — does disabled genuinely win in every case?
       - ERROR HANDLING: Missing catches, swallowed errors? Does the disabled check happen before goroutine spawn/allocation, or only before the HTTP send?
       - PERFORMANCE: N+1, leaks, blocking ops? Any per-subcommand re-read of the env var (should be once, at root-command construction)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/main.go:250 | Asymmetric opt-out failure: unparseable `ATCR_TELEMETRY` fails OPEN (enabled) while malformed config fails SAFE (disabled); a misspelled env opt-out (`off`/`no`/`disabled`) silently still tracks. | AC-pinned — AC 02-01 EC2 mandates env "unparseable defaults to enabled". Deferred → **TD-010** (keep default, add stderr warning on unrecognized value; direction change needs AC revision). Zero live exposure (empty endpoint, TD-003). |
   | MEDIUM | internal/registry/telemetry_setting.go:74 | `SetTelemetrySetting` rewrites the whole config with a non-atomic `os.WriteFile` (truncate-in-place); a crash/full-disk mid-write corrupts the entire config, not just `telemetry`. `trust.go` has an atomic temp+rename precedent. | Fixed inline in 3.3: atomic temp-file + `os.Rename`, preserving file mode. |
   | LOW | internal/registry/telemetry_setting.go:65 | An existing 0-byte config is rejected ("not a valid config mapping"), so `config set` cannot record an opt-out on a stub config. | Fixed inline in 3.3: synthesize an empty document/mapping and append the key. |
   | LOW | cmd/atcr/review.go:397 | Review-path gate/Send has no end-to-end counting-send test (only reconcile does); a future divergence in the review path would go uncaught. | Deferred → **TD-011** (`runReview` is heavy to drive; gate is shared, unit-tested, and reconcile-e2e-proven). |

   **Action taken:** No CRITICAL/HIGH — proceed. The atomic-write MEDIUM and the empty-config LOW are genuine, bounded robustness fixes with an existing repo precedent, so they are fixed inline in 3.3 (resolved > deferred). The env-asymmetry MEDIUM is AC-pinned (AC 02-01 EC2) and the review-path e2e LOW is heavy to build now → **TD-010 / TD-011**.

### 3.3 [x] **[Telemetry Opt-Out - REFACTOR](plan/user-stories/02-telemetry-opt-out.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code quality (T1); confirm the OR gate has no precedence/override chain
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(telemetry): address review + clean up opt-out gate"`
   **Duration:** 0.5 day

### 3.4 [x] **Phase 3 — DoD Validation**
   - `go test ./cmd/atcr/... ./internal/registry/... ./internal/telemetry/...` (T3 scoped) — all passing
   - `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 errors
   - Coverage: `cmd/atcr`, `internal/registry`, `internal/telemetry` all ≥80%
   - 4-combination opt-out matrix (AC 02-03) passes; disabled wins in every case
   - DoD report:
     ```
     Story-02 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 4/4
     Manual Review: [ ] Code reviewed
     ```

### 3.LAST [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `ProjectConfig.Telemetry` and the opt-out gate's public shape stable for Phase 4/5 consumption?
       - CONFIG SURFACE: New `telemetry` key in `.atcr/config.yaml` documented, defaulted (nil = enabled), back-compat with configs that predate this field?
       - INTEGRATION: `atcr config` subcommand group correctly registered; no collision with existing commands?
       - PHASE-EXIT CONTRACT: Can Phase 4's `--sync-cloud` reuse the same opt-out gate cleanly, or does it need its own (per Story 4's explicit-invocation distinction)?
       - REGRESSION: Story 1's telemetry ping still fires correctly when NOT disabled; earlier-phase behavior intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 4: Advanced — Cloud Sync Push (Days 8-10)

**Focus:** User Story 4 (`--sync-cloud` Authenticated Push). Depends on Phase 2's Story 1 (HTTP send pattern) and Story 3 (hashed Persona ID field). Adds `addSyncCloudFlags` (flags.go), the new `exitAuth` exit code + `codedError` path (main.go), and a new cloud-sync allowlist schema in `internal/scorecard/export.go` that is NOT a superset of `PublicRecord`. Highest-risk phase — concentrates several high-complexity ACs.

**Story:** [04 - `--sync-cloud` Authenticated Push](plan/user-stories/04-sync-cloud-authenticated-push.md) | **ACs:** [04-01](plan/acceptance-criteria/04-01-sync-cloud-flag-registration.md), [04-02](plan/acceptance-criteria/04-02-successful-authenticated-push.md), [04-03](plan/acceptance-criteria/04-03-missing-api-key-exit-code.md), [04-04](plan/acceptance-criteria/04-04-invalid-rejected-api-key-exit-code.md)

> **Pre-implementation check:** `grep -rn "addRangeFlags" cmd/atcr/flags.go` to confirm the exact PreRunE-chaining convention before adding `addSyncCloudFlags` alongside it.

### 4.1 [ ] **[Cloud Sync Push - RED](plan/user-stories/04-sync-cloud-authenticated-push.md)**
   Write comprehensive failing tests, verify they fail correctly:
   - `TestAddSyncCloudFlags_RegisteredOnReviewAndReconcile` — `--sync-cloud` flag present on both commands (04-01)
   - `TestSyncCloud_SuccessfulPush_BearerHeaderAndAllowlistedBody` — successful push against `httptest.NewServer` sends `Authorization: Bearer <ATCR_API_KEY>` and a JSON body containing only the allowlisted fields (time/credits-saved metrics + hashed Persona ID, no raw source/file paths/un-hashed identifiers) (04-02)
   - `TestSyncCloud_MissingAPIKey_ExitsAuthCode` — `--sync-cloud` set, `ATCR_API_KEY` unset, command exits with the new dedicated `exitAuth` code (04-03)
   - `TestSyncCloud_InvalidOrRejectedAPIKey_ExitsAuthCode` — simulated 401/403 from the mock endpoint, command exits with `exitAuth` (04-04)
   - `TestSyncCloud_FlagOmitted_ZeroCloudNetworkCalls` — omitting `--sync-cloud` entirely results in zero network calls to the cloud endpoint
   **Files:** `cmd/atcr/flags_test.go`, `cmd/atcr/main_test.go` | **Duration:** 1 day

### 4.2 [ ] **[Cloud Sync Push - GREEN](plan/user-stories/04-sync-cloud-authenticated-push.md)**
   Minimal code, one test at a time (T1), verify all (T2), COMMIT:
   - `cmd/atcr/flags.go` — `addSyncCloudFlags(cmd *cobra.Command)` alongside `addRangeFlags`, registering `--sync-cloud` (bool) and an optional `--cloud-endpoint` override (defaulting to the documented `atcr.dev/dashboard` endpoint, for test override)
   - `cmd/atcr/main.go` — new `exitAuth` constant alongside `exitFailure`/`exitUsage`; a corresponding `codedError` value returned when `ATCR_API_KEY` is unset (trimmed, validated) or the remote endpoint responds 401/403, surfaced via the existing `errors.As` dispatch
   - `internal/scorecard/telemetry.go` (or `export.go`) — new cloud-sync allowlist struct, NOT a superset of `PublicRecord`, built from Story 3's hashed-Persona-ID path plus time/credits-saved metrics already computed for the local scorecard
   - `cmd/atcr/review.go:runReview` / `cmd/atcr/reconcile.go:runReconcile` — after the run's primary outcome is finalized, when `--sync-cloud` is set, build the payload and POST it (short request timeout; failure surfaces as a distinct, non-fatal-to-the-already-completed-run error unless it's an auth failure, which is `exitAuth`)
   COMMIT: `git commit -m "feat(cloud-sync): --sync-cloud authenticated push with dedicated exitAuth (green)"`
   **Files:** `cmd/atcr/flags.go`, `cmd/atcr/main.go`, `internal/scorecard/telemetry.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go` | **Duration:** 2 days

### 4.2.A [ ] **[Cloud Sync Push - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-sync-cloud-authenticated-push.md)**
   **Changed Files:** `cmd/atcr/flags.go`, `cmd/atcr/main.go`, `internal/scorecard/telemetry.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `cmd/atcr/flags_test.go`, `cmd/atcr/main_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? Is the raw `ATCR_API_KEY` ever logged, echoed in an error message, or included in the payload?
       - EDGE CASES: Null, empty, boundaries, concurrent access? Empty-string API key, whitespace-only key, key with trailing newline?
       - ERROR HANDLING: Missing catches, swallowed errors? Does a non-auth network failure (timeout, DNS) incorrectly map to `exitAuth`, or correctly surface as a separate error without corrupting the already-finalized run outcome?
       - PERFORMANCE: N+1, leaks, blocking ops? Is the cloud-sync payload schema verified to NOT be a superset of `PublicRecord` (no raw source, file paths, or un-hashed identifiers)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 4.3 [ ] **[Cloud Sync Push - REFACTOR](plan/user-stories/04-sync-cloud-authenticated-push.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve code quality (T1); confirm `exitAuth` dispatch is unambiguous versus `exitUsage`/`exitFailure`
   3. Validate all tests still pass (T3)
   4. COMMIT: `git commit -m "refactor(cloud-sync): address review + clean up"`
   **Duration:** 0.5 day

### 4.4 [ ] **Phase 4 — DoD Validation**
   - `go test ./cmd/atcr/... ./internal/scorecard/...` (T3 scoped) — all passing
   - `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 errors
   - Coverage: `cmd/atcr`, `internal/scorecard` both ≥80%
   - Both missing-key (04-03) and invalid/rejected-key (04-04) paths independently verified to exit `exitAuth`
   - DoD report:
     ```
     Story-04 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 4/4
     Manual Review: [ ] Code reviewed
     ```

### 4.LAST [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: `exitAuth` exit code, `--sync-cloud`/`--cloud-endpoint` flags, and cloud-sync payload schema stable for Phase 5 documentation?
       - CONFIG SURFACE: `ATCR_API_KEY` read pattern consistent with the `LOG_LEVEL`/`ATCR_TELEMETRY` precedent established in Phases 2-3?
       - INTEGRATION: Cloud-sync push correctly sequenced after the run's local scorecard write, never blocking or corrupting the already-finalized outcome?
       - PHASE-EXIT CONTRACT: Can Phase 5's documentation describe real, finalized flag/env-var/exit-code names with no further changes expected?
       - REGRESSION: Phase 2-3 telemetry ping and opt-out gate still function correctly; leaderboard `--export` path still byte-for-byte unchanged?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 5: Documentation (Day 11)

**Focus:** User Story 5 (Telemetry Privacy Documentation). Sequenced last so it documents the real, finalized flag/env-var/exit-code contracts from Phases 2-4 rather than speculative ones. Adds `docs/telemetry.md` (linked from `docs/README.md`) and updates `docs/scorecard.md`'s Privacy Model section to cross-reference it without contradicting the existing Epic 10.0 guarantee.

**Story:** [05 - Telemetry Privacy Documentation](plan/user-stories/05-telemetry-privacy-documentation.md) | **ACs:** [05-01](plan/acceptance-criteria/05-01-telemetry-doc-content-and-index.md), [05-02](plan/acceptance-criteria/05-02-scorecard-privacy-model-updated.md), [05-03](plan/acceptance-criteria/05-03-docs-audit-tests-pass.md)

### 5.1 [ ] **[Telemetry Privacy Documentation - RED](plan/user-stories/05-telemetry-privacy-documentation.md)**
   Confirm the existing gates fail correctly against the not-yet-written docs, verify they fail correctly:
   - Run `go test ./cmd/atcr/... -run TestDocs` before writing any docs — confirm `TestDocsIndexCoversEveryDoc` and `TestDocsClaimedFlagsAreReal` currently pass (no undocumented flags reference `docs/telemetry.md` yet) and note the exact flags/env vars from Phases 2-4 that must appear once the doc is written: `--sync-cloud`, `--cloud-endpoint`, `ATCR_TELEMETRY`, `ATCR_API_KEY`, `atcr config set telemetry`
   - Add `TestDocsAudit_TelemetryDocIndexed` if not already covered generically by `TestDocsIndexCoversEveryDoc` — asserts `docs/telemetry.md` is linked from `docs/README.md` once created (05-01)
   **Files:** `cmd/atcr/docs_audit_test.go` (verify existing coverage, add only if a gap exists) | **Duration:** 0.25 day

### 5.2 [ ] **[Telemetry Privacy Documentation - GREEN](plan/user-stories/05-telemetry-privacy-documentation.md)**
   Minimal content (T1), verify all (T2), COMMIT:
   - `docs/telemetry.md` (new) — modeled on `docs/scorecard.md`'s Privacy Model structure: overview of what runs and when; a Privacy Model section with "Preserved"/"Stripped — never exported" lists for the `{event, lang, lines, status}` telemetry payload (Story 1); an Opt-Out section documenting both `` `ATCR_TELEMETRY=0` `` and `` `atcr config set telemetry false` `` with example commands (Story 2); a Persona Leaderboard Data section explaining the hashed Persona ID and why it is a one-way hash (Story 3); a Cloud Sync (`` `--sync-cloud` ``) section documenting the `ATCR_API_KEY` Bearer-token flow and the distinct `exitAuth` exit code (Story 4)
   - `docs/README.md` — add a new link to `docs/telemetry.md` in the Benchmarking & observability section, alongside `scorecard.md`/`metrics.md`
   - `docs/scorecard.md` — update the Privacy Model section (~line 277) in place (preserve all existing Epic 10.0 content) to cross-reference `docs/telemetry.md` and clearly separate the local-store `--export` allowlist boundary from the new telemetry/cloud-sync data paths
   COMMIT: `git commit -m "docs(telemetry): add telemetry.md, index link, scorecard privacy cross-reference (green)"`
   **Files:** `docs/telemetry.md`, `docs/README.md`, `docs/scorecard.md` | **Duration:** 0.5 day

### 5.2.A [ ] **[Telemetry Privacy Documentation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-telemetry-privacy-documentation.md)**
   **Changed Files:** `docs/telemetry.md`, `docs/README.md`, `docs/scorecard.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.2]
     - Checklist (pass verbatim):
       - SECURITY: Does the doc accurately state no source code/file paths are ever transmitted? Any accidental real-looking API key example (must be synthetic-only)?
       - EDGE CASES: Are all 4 combinations of the opt-out matrix (02-03) described accurately? Is the `ATCR_TELEMETRY` inverse-boolean-direction footgun (vs `ATCR_DISABLE_AST_GROUPING`) called out explicitly?
       - ERROR HANDLING: Does the doc describe the `exitAuth` exit code accurately (missing key + invalid key both map to it)?
       - PERFORMANCE: N/A — doc accuracy: does every `` `--x` `` flag and env var named in the doc match the actual implemented flags/env vars from Phases 2-4 exactly?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> List issues for 5.3, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Adversarial review passed" and proceed

### 5.3 [ ] **[Telemetry Privacy Documentation - REFACTOR](plan/user-stories/05-telemetry-privacy-documentation.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
   2. Cross-reference every documented flag/env var against the actual implemented code from Phases 2-4 (not the plan's proposed names); maintain green (T3)
   3. Manual review: read `docs/telemetry.md` and the updated `docs/scorecard.md` Privacy Model section without source-code lookups, confirm they stand alone and don't contradict each other (05-01, 05-02)
   4. COMMIT: `git commit -m "docs(telemetry): address review + cross-reference finalized contracts"`
   **Duration:** 0.25 day

### 5.4 [ ] **Phase 5 — DoD Validation**
   - `go test ./cmd/atcr/... -run TestDocs` passing — `TestDocsIndexCoversEveryDoc` and `TestDocsClaimedFlagsAreReal` both green (05-03)
   - `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 errors
   - Manual read-through confirms no claim in `docs/telemetry.md` contradicts the actual Phase 2-4 implementation
   - DoD report:
     ```
     Story-05 DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 3/3
     Manual Review: [ ] docs/telemetry.md walkthrough  [ ] docs/scorecard.md cross-reference validated
     ```

### 5.LAST [ ] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Every flag/env var documented in `docs/telemetry.md` matches `canonicalFlags()` in `cmd/atcr/docs_audit_test.go` exactly?
       - CONFIG SURFACE: `docs/README.md` index includes `docs/telemetry.md`; no orphaned doc file?
       - INTEGRATION: `docs/scorecard.md`'s Privacy Model update doesn't contradict or weaken the existing Epic 10.0 guarantee?
       - PHASE-EXIT CONTRACT: Is documentation complete enough for Phase 6's cumulative validation and eventual `/execute-code-review` to proceed without doc gaps?
       - REGRESSION: `TestDocsIndexCoversEveryDoc`/`TestDocsClaimedFlagsAreReal` both green?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 6: Integration & Validation (Days 12-13)

**Focus:** Cumulative cross-story regression — full `go test ./...`, the AC 03-03 byte-for-byte leaderboard-export regression, `go test ./cmd/atcr/... -run TestDocs`, the 4-combination opt-out precedence matrix (AC 02-03), coverage gate (baseline 80%), lint/vet, and adversarial risk-profile prep ahead of `/execute-code-review`.

### 6.1 [ ] **Cumulative Regression Suite**
   1. `go test ./...` — full suite across all packages touched by Stories 1-5
   2. Re-run `TestRunLeaderboardExport_ByteForByteRegression` (AC 03-03) in isolation to confirm no Phase 4/5 change altered it
   3. Re-run `TestTelemetryOptOut_FourCombinationMatrix` (AC 02-03) in isolation
   4. `go test ./cmd/atcr/... -run TestDocs` — `TestDocsIndexCoversEveryDoc`, `TestDocsClaimedFlagsAreReal`
   **Duration:** 0.5 day

### 6.2 [ ] **Coverage & Lint/Vet Gate**
   1. `go test -coverprofile=coverage.out ./...` — confirm ≥80% baseline maintained, with special attention to `internal/telemetry` and `internal/scorecard`
   2. `golangci-lint run` — 0 errors
   3. `go vet ./...` — clean
   4. `go build ./...` — succeeds
   **Duration:** 0.25 day

### 6.3 [ ] **Cumulative Adversarial Risk-Profile Review (subagent)**
   **Scope:** Full sprint diff (all files changed across Phases 1-5)

   **Spawn a fresh subagent** via the Agent tool to perform this review, using the Risk Analysis table from [sprint-design.md](plan/sprint-design.md) as its checklist source. The subagent has no memory of the implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Cumulative risk-profile review: Phase 6`
   - prompt: Self-contained brief including:
     - Full sprint diff scope (absolute paths of all changed files across Phases 1-5)
     - Checklist (pass verbatim, drawn from sprint-design.md's Risk Analysis):
       - Telemetry payload construction: exactly `event`/`lang`/`lines`/`status`, no source code/file paths/identifiers beyond these four
       - Persona ID hashing: SHA-256 one-way, raw string never appears in output or logs
       - `--sync-cloud` auth: `ATCR_API_KEY` trimmed/validated before use, never logged, dedicated `exitAuth` on missing/invalid, HTTPS-only endpoint
       - Leaderboard export boundary: `scrubField`/`PublicRecord` byte-for-byte unchanged, no accidental bypass or extension
       - Telemetry ping: fails open silently, bounded ~2-3s timeout, zero blocking wait on the main command path
       - `--sync-cloud` push: executed after the run's outcome is finalized, bounded request timeout, failure surfaces as a separate error
       - Opt-out: strict OR across all 4 combinations, no precedence/override chain
       - Panic safety: `defer recover()` present around every telemetry/cloud-sync goroutine
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before Phase 6 DoD, do NOT proceed until fixed
   - MEDIUM/LOW found -> Append to `clarifications/tech-debt-captured.md`
   - None found -> Note "Cumulative risk-profile review passed" and proceed
   **Duration:** 30-45 min

### 6.4 [ ] **Phase 6 — DoD Validation**
   - `go test ./...` fully green across the whole sprint diff
   - `go build ./...` clean; `go vet ./...` clean; `golangci-lint run` 0 errors
   - Coverage ≥80% maintained project-wide
   - AC 03-03 and AC 02-03 regression checks both independently confirmed green
   - `TestDocsIndexCoversEveryDoc`/`TestDocsClaimedFlagsAreReal` both green
   - DoD report:
     ```
     Phase-6 (Integration & Validation) DoD Complete
     Auto: 5/5 (tests, coverage, lint, vet, build) | Story-Specific: 19/19 (all ACs across Stories 1-5)
     Manual Review: [ ] Cumulative risk-profile review reviewed
     ```

### 6.LAST [ ] **Phase 6 - GATE: Cumulative Integration & Exit Review (subagent)**
   **Scope:** Cumulative — full sprint diff (integration-level)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 6 cumulative gate review`
   - prompt: Self-contained brief including:
     - Full sprint diff scope (absolute paths of all changed files across Phases 1-5)
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All 19 acceptance criteria across Stories 01-05 satisfied?
       - CONFIG SURFACE: `ATCR_TELEMETRY`, `ATCR_API_KEY`, `atcr config set telemetry`, `--sync-cloud`, `--cloud-endpoint` all documented and back-compat with pre-existing `.atcr/config.yaml` files?
       - INTEGRATION: Telemetry ping, opt-out gate, Persona ID hashing, and cloud-sync push compose correctly end-to-end via `httptest` mocks?
       - PHASE-EXIT CONTRACT: Sprint deliverable is ready for `/execute-code-review` with no unresolved ambiguity?
       - REGRESSION: `go test ./...` clean, `golangci-lint run` clean, `go vet ./...` clean, `go build ./...` clean, leaderboard `--export` path byte-for-byte unchanged?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found -> Fix before sprint completion, do NOT stop. Re-run gate.
   - MEDIUM/LOW found -> Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
   - None found -> Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold (≥80%)
- [ ] Lint/format clean: `golangci-lint run`, `go vet ./...`
- [ ] Build succeeds: `go build ./...`

### Optional: Targeted Mutation Testing
MUTATION_TOOL: **UNAVAILABLE** — no mutation tool detected (`stryker-mutator` / `mutmut` / `cargo-mutants` absent). Skip mutation testing.
**WARNING:** Do NOT run full codebase mutation - it can take hours. Target specific files only if a tool becomes available.

### Drift Analysis
Compare delivered work against [plan/original-requirements.md](plan/original-requirements.md):
- Anonymous, fail-open background telemetry ping on `review`/`reconcile` completion — Phase 2 (Story 1)
- Strict `ATCR_TELEMETRY=0` env var + `atcr config set telemetry false` opt-out, OR'd not overridden — Phase 3 (Story 2)
- Persona ID hashing for the crowdsourced Persona Leaderboard, isolated from `PublicRecord`/`scrubField` — Phase 2 (Story 3)
- `--sync-cloud` flag authenticating via `ATCR_API_KEY` (`Bearer` token), dedicated `exitAuth` exit code — Phase 4 (Story 4)
- Privacy documentation (`docs/telemetry.md`, updated `docs/scorecard.md`) — Phase 5 (Story 5)
- **Explicitly out of scope (confirmed):** Building the actual `atcr.dev/dashboard` SaaS UI — this epic only handles the CLI emission/sync mechanism (per original-requirements.md's Out of Scope section).

If any task drifted from the original request, STOP and validate before marking the sprint complete.

---

**Next:** `/execute-sprint @.planning/sprints/active/28.0_telemetry_expansion_cloud_sync/`
