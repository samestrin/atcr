# Tech Debt Captured — Sprint 28.0 Telemetry Expansion & Cloud Sync

Deferrals and adversarial-review findings surfaced during `/execute-sprint`.
Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged
`SOURCE=execute-sprint`).

---

## TD-001 — Cloud-sync "time/credits-saved" metric does not yet exist (MEDIUM)
**Origin:** Phase 1, task 1.1 design spike, 2026-07-15
**Issue:** The plan (task 4.2 / sprint-design Architecture) states the `--sync-cloud` payload carries "time/credits-saved metrics already computed for the local scorecard." A repo-wide search (`Saved/Credits/Savings/time_saved/credits_saved` across all `.go` and `docs/scorecard.md`) found no such metric. Existing `scorecard.Record` fields are raw only: `CostUSD`, `TokensIn`, `TokensOut`, `LatencyMS` (scorecard.go:63-66).
**Why accepted:** Phase 1 is design-only; the gap is Phase-4-scoped and does not affect the Shape 1-3 isolation guarantees confirmed by the spike.
**Fix in:** Phase 4 (Story 4 GREEN, task 4.2) — either derive time/credits-saved from the existing raw metrics with an explicit documented formula, or narrow `CloudSyncRecord` to the raw metrics that actually exist. Must be resolved before the Phase 4 payload-shape assertions (AC 04-02) are finalized.

## TD-002 — Global `telemetryClient` needs a test-isolation seam (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review (round 4), 2026-07-15
**Issue:** `telemetryClient` is a process-global set in `newRootCmd`. In a shared `go test` binary it stays non-nil once any test constructs the root, so a later test building a bare `newReviewCmd()`/`newReconcileCmd()` sees a non-nil receiver rather than the nil no-op the design assumes — test-order-dependent behavior.
**Mitigation this sprint:** The empty-endpoint hard no-op (TD-003) already prevents emission regardless of nil-ness. This TD covers the additional determinism seam.
**Fix in:** Phase 2 (task 2.2 GREEN) — make `telemetryClient` (and/or the endpoint) test-overridable following the existing `forceExit`/`gracefulShutdownTimeout` var precedent (cmd/atcr/main.go:25,30) so tests force a no-op or capture client deterministically.

## TD-003 — Telemetry endpoint must be a hard no-op when empty; default unset (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review (round 4), 2026-07-15
**Issue:** With opt-out defaulting to enabled and Phase-2 `telemetryGate()` returning `true`, an unconditionally-constructed live client would emit outbound telemetry on every `runReview`/`runReconcile` reached through `newRootCmd` in the existing test suite (errors swallowed, so silent). The usage-ping endpoint is not defined by this plan.
**File:** internal/telemetry/client.go (new)
**Fix in:** Phase 2 (task 2.2 GREEN) — `telemetry.New` returns a client whose `Send` no-ops when `endpoint == ""`; the Phase-2 default endpoint is empty/unset so CI/dev test runs emit zero network. Emission tests inject a non-empty `httptest` endpoint explicitly. Re-confirm when a real usage-ping endpoint is chosen (coordinate with the `atcr.dev` backend owner, same as the Story 4 cloud endpoint).

## TD-004 — `Send`'s `ctx` parameter is dead API surface (LOW)
**Origin:** Phase 1, task 1.LAST gate review (round 4), 2026-07-15
**File:** internal/telemetry/client.go (new)
**Issue:** `Send(ctx, enabled, ev)` accepts `ctx` but intentionally derives its deadline from `context.Background()` (to survive command-context cancellation so `Close` can drain), leaving `ctx` unused — a discarded required param invites a caller to assume cancellation/tracing propagates.
**Why accepted:** Design intent (detach from command ctx) is correct; only the signature ergonomics are at issue.
**Fix in:** Phase 2 (task 2.3 REFACTOR) — drop `ctx` from `Send`'s signature, or document at the call site that it is intentionally non-propagating.

## TD-005 — Reconcile telemetry event field derivation undefined (LOW)
**Origin:** Phase 1, task 1.LAST gate review (round 4), 2026-07-15
**File:** cmd/atcr/reconcile.go
**Issue:** The 4-key payload `{event, lang, lines, status}` has no obvious `lang`/`lines` source at the `runReconcile` call site (a reconcile run has no single language / line count like a review does), risking empty/meaningless fields.
**Why accepted:** Non-blocking; `event`/`status` are well-defined per call site.
**Fix in:** Phase 2 (task 2.2 GREEN) — define an explicit zero-value / `"reconcile"` contract for `lang`/`lines` on the reconcile event so the payload is intentional, not accidentally empty.
**Resolved:** 2026-07-15 — `reconcileTelemetryEvent` (cmd/atcr/telemetry.go) sets `event:"reconcile_run"`, `lang:""`, `lines:0` by explicit documented contract.

## TD-006 — Telemetry ping never drains before process exit (MEDIUM)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-15
**File:** cmd/atcr/main.go:46
**Issue:** `Client.Send` fires the POST on a detached goroutine at the end of `runReview`/`runReconcile`, then `main()` returns or calls `os.Exit(code)` immediately. `os.Exit` does not wait for goroutines, so once a real (non-empty) telemetry endpoint is configured the DNS/TLS/POST is killed mid-flight and delivery is effectively ~0%. The provided `Client.Wait()` drain is intentionally not wired into shutdown.
**Why accepted:** Harmless this sprint — `defaultTelemetryEndpoint` is empty, so `Send` is a hard no-op (no goroutine spawns). The drain-on-shutdown design pairs naturally with the real-endpoint decision (also TD-003), which is owned outside this plan.
**Fix in:** Whichever sprint wires a real `defaultTelemetryEndpoint` — add a bounded drain (`telemetryClient.Wait()` under a short ~1s timeout) in `main()` before `os.Exit`/return so a configured endpoint actually receives the ping without risking a hang.

## TD-007 — Persona ID hash is unsalted SHA-256 over enumerable low-entropy input (HIGH)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-15
**File:** internal/scorecard/telemetry.go:18
**Issue:** `HashPersonaID` is a bare, unsalted hex SHA-256 of `Record.Reviewer`. Persona IDs are a small, enumerable, often publicly-known set (community-registry persona names). An adversary who obtains a telemetry/cloud-sync payload can precompute a rainbow table of every known persona name and trivially invert the digest — so the "non-reversible" claim is a dictionary-attack-reversible pseudonym, not a true non-reversibility guarantee.
**Why accepted:** The acceptance criteria pin plain SHA-256 with an exact empty-string digest constant (AC 03-01 Edge Case 1 = `e3b0c442...`, AC 03-04 pins plain-SHA-256 outputs). The Phase-2 fix was limited to accurate docstring wording (pseudonymization + its bound). There is also zero live exposure this sprint — the telemetry/cloud-sync endpoint is a hard no-op (empty `defaultTelemetryEndpoint`, TD-003), so no digest is transmitted anywhere yet.
**User decision (2026-07-15):** Explicitly accept plain SHA-256 as shipped; do NOT escalate to HMAC this sprint. Rationale (important — do not naively reach for a client-side pepper when resolving this): cross-install leaderboard aggregation requires every user's independent CLI to hash the same persona name to the *same* digest, so an HMAC pepper would have to be a shared secret baked into the distributed binary. That secret is trivially extractable by disassembly, buying only marginal protection over the current unsalted digest while still fully sacrificing the plaintext-equivalence aggregation depends on. A client-baked pepper is therefore the wrong fix.
**Fix in:** A backend architecture decision owned by the (still-undefined) `atcr.dev` team — the same dependency already blocking TD-003's real endpoint. Correct options are (a) a **server-side keyed hash** where raw persona names never leave the client (client sends the raw name over the authenticated channel; the server applies its own secret keying), or (b) a **properly distributed/rotatable secret** managed outside the binary. Either changes AC 03-01/03-04's pinned digests, so it cannot land inside Phase 2's pinned spec. Revisit only when that backend contract is defined.

## TD-008 — Telemetry schema copies Model through unhashed on an unenforced non-PII assumption (LOW)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-15
**File:** internal/scorecard/telemetry.go:30
**Issue:** `NewTelemetryPersonaRecord` copies `Record.Model` into the telemetry payload unhashed on the asserted assumption that a model identifier is non-PII and already public. A future free-form or fine-tuned model id (e.g. a customer-named fine-tune) could carry sensitive data and would leak unscrubbed.
**Why accepted:** True for every model identifier in the codebase today (closed provider/model set); the assumption is documented in the field's doc comment.
**Fix in:** A later hardening pass — enforce the assumption (validate `Model` against a known non-sensitive enum at the payload boundary, or scrub it) rather than asserting it in a comment.

## TD-009 — Reconcile telemetry status is always "success", fired before the gate (LOW)
**Origin:** Phase 2, task 2.LAST gate review, 2026-07-15
**File:** cmd/atcr/reconcile.go:163
**Issue:** `reconcileTelemetryEvent("success")` is hardcoded and fires before the `--require-verified` / `--fail-on` gate evaluation that sets the exit code, so a reconcile that trips the threshold (exit 1) still reports `status:"success"`. Unlike `runReview` (which derives status from `err`), the reconcile `status` field carries no gate signal and its `"failure"` branch is effectively dead — a Phase-4 analytics consumer of `{event,status}` cannot distinguish gate-passed from gate-failed reconciles.
**Why accepted:** Non-blocking; telemetry is a no-op this sprint (empty endpoint). The current contract "success = the reconcile computation completed" is coherent, just narrower than review's.
**Fix in:** A later pass — move the Send below the gate evaluation (thread the gate outcome into the event) so reconcile status mirrors review's success/failure contract, or document in `event.go`/`telemetry.go` that reconcile status intentionally means "computation completed", not "gate passed".
