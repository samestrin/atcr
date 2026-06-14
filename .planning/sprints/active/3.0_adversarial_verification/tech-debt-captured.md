# Tech Debt Captured — Sprint 3.0 Adversarial Verification

Items deferred during `/execute-sprint`. Read by `/execute-code-review` (Phase 1) and pre-seeded into the adversarial TD stream (SOURCE=execute-sprint).

---

## TD-001 — SelectEligibleSkeptics returns aliased reference fields (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-14
**File:** internal/verify/select.go:65
**Issue:** Returned `registry.AgentConfig` values are shallow copies — embedded reference fields (`Scope []string`, `*Temperature`, `*TimeoutSecs`, `*MaxTurns`, `*ToolBudgetBytes`, `*MaxFindings`) alias the registry's backing memory. A caller mutating `out[i].Scope[0]` or writing through a pointer field would corrupt the shared registry. Current consumers (Phase 2 skeptic invocation) treat configs read-only, so no live bug.
**Why accepted:** No consumer mutates returned configs; deep-copying every selection would be premature for a read-only access pattern. Behavior is correct for the sprint's needs.
**Fix in:** Epic 3.0 follow-up or whenever a mutating consumer appears — either deep-copy reference fields in `AgentsByRole`/`SelectEligibleSkeptics`, or add an explicit read-only contract to the godoc plus a test that mutates a slice/pointer field and asserts the registry is unchanged.

## TD-002 — namesOf test helper relies on structural equality (LOW) — RESOLVED
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-14
**Issue:** The original `namesOf` test helper reverse-mapped configs to names via `reflect.DeepEqual`, which would mis-attribute names if two fixture configs were identical.
**Resolved:** 2026-06-14 — the Phase 1 gate (1.5) drove `SelectEligibleSkeptics` to return `[]Skeptic{Name, Config}`, so tests now read `Skeptic.Name` directly. The DeepEqual reverse-lookup is deleted. No debt remains.

## TD-003 — Skeptic prompt code-context fence is not collision-safe (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-06-14
**File:** internal/verify/skeptic.go:42
**Issue:** `buildSkepticPrompt` wraps each file body in a fixed triple-backtick fence. A file body that itself contains a triple-backtick line breaks out of the fence, letting code-under-review inject structure (or instructions) into the skeptic prompt. Low risk: the skeptic is adversarial-by-design and the verdict enum is switch-validated downstream, so a broken fence degrades prompt quality but cannot forge a verdict.
**Why accepted:** Prompt-quality hardening, not a correctness or security defect; out of the Phase 2 invocation/parse scope. The caller already owns context (pre-truncation), so fence normalization fits the same future pass.
**Fix in:** Epic 3.0 prompt-tuning follow-up — fence each body with a backtick run longer than any run inside the body (max embedded run + 1), or escape embedded fences, and add a test with a body containing a triple-backtick line.

## TD-004 — `--require-verified` silently passes when the verify stage never ran (MEDIUM)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-14
**File:** internal/reconcile/gate.go:80
**Issue:** `CountAtOrAbove(..., requireVerified=true)` excludes every finding whose `Verification` is nil. If the verify stage never ran (or crashed before re-emit), all findings have nil `Verification`, so even a real CRITICAL yields count 0 and CI passes — the strictest gate becomes the most permissive when verdicts are absent. The pure helper is correct per its contract (only confirmed counts); the missing piece is the caller-side guard.
**Why accepted:** No caller passes `requireVerified=true` yet — Phase 3 wires only `false` at both call sites. The guard belongs to the Phase 4 CLI/MCP wiring that introduces the `--require-verified` flag, alongside AC 05-01 Scenario 4 flag validation.
**Fix in:** Phase 4 (CLI/MCP integration) — refuse `--require-verified` unless the manifest `Stages` contains `"verify"` (or `reconciled/verification.json` exists), failing fast with a "verification has not run" usage error rather than silently passing.

## TD-005 — Atomic writers lack crash durability (fsync) (MEDIUM)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-14
**File:** internal/verify/emit_verification.go:101
**Issue:** The verify-stage `writeFileAtomic` (and its pre-existing peers `internal/reconcile/emit.go:314` and `internal/payload/manifest.go:120`) omit `tmp.Sync()` before `Close()` and never fsync the parent directory after `os.Rename`. The temp-file+rename pattern guarantees no partial reads, but not durability: a power loss / kernel crash can leave a truncated or stale findings.json / verification.json / manifest.json. Process-kill (SIGKILL) is safe; only crash/power-loss is exposed.
**Why accepted:** Repo-wide concern affecting three independent copies, not a defect introduced by Phase 3; the new copy intentionally mirrors the established pattern. Hardening one copy while leaving the others inconsistent would be worse. Out of Phase 3 scope.
**Fix in:** Cross-cutting durability follow-up — factor the three duplicated `writeFileAtomic` copies into one shared helper that calls `tmp.Sync()` before close and fsyncs the containing directory after rename.

## TD-006 — Atomic-write temp files orphan on SIGKILL (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-14
**File:** internal/verify/emit_verification.go:108
**Issue:** On SIGKILL the `defer os.Remove(tmpName)` never runs, leaving `.<name>.tmp-*` files in `reconciled/` and the review-dir root. Impact is low: readers (`ReadReconciledFindings`, `UpdateManifestStage`) open exact filenames and source discovery only walks `sources/`, so orphans are never consumed — they accumulate as litter across killed runs. Shared with the two pre-existing atomic-writer copies.
**Why accepted:** Litter, not a correctness defect; folds naturally into the same shared-helper cleanup as [[TD-005]].
**Fix in:** Same shared atomic-writer helper — best-effort sweep of stale `.*.tmp-*` in the target dir at start of a run, or document the orphan as accepted.

## TD-007 — ReEmitFindings silently drops verdicts whose key matches no finding (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-14
**File:** internal/verify/emit_findings.go:34
**Issue:** `ReEmitFindings` matches verdicts by exact `FindingKey{File,Line,Problem}`. `Merge` (merge.go:65) sets `Problem` to the longest variant across a cluster, so the re-emitted `Problem` is the merged text; a verdict whose key was built from a pre-merge or skeptic-echoed problem string silently fails to attach (finding keeps v1 confidence, nil Verification) with no diagnostic. Correct when keys come from the same findings.json this function reads, but nothing enforces it.
**Why accepted:** The Phase 4 caller builds the verdict map from the same `[]JSONFinding` it read, so keys match by construction — no live bug. A diagnostic for unmatched verdicts is defensive hardening for the orchestration layer.
**Fix in:** Phase 4 (verify pipeline orchestration) — build keys from the same findings slice and/or log/return a count of verdicts that matched zero findings so a key-construction mismatch is visible rather than silent.

## TD-008 — VerdictCounts computed twice risks tally drift (LOW) — RESOLVED
**Origin:** Phase 3, task 3.5 gate review, 2026-06-14
**File:** internal/verify/emit_verification.go:58
**Issue:** `WriteVerification` derived `VerdictCounts` internally but did not return them, and there was no exported helper to compute a `VerdictCounts` from `[]VerificationResult`, so the Phase 4 pipeline would recompute the tally separately — two sources of truth that could drift.
**Resolved:** 2026-06-14 (Phase 4, task 3.2/4.2) — added exported `CountVerdicts([]VerificationResult) VerdictCounts`; both `WriteVerification` and the pipeline's `UpdateSummaryVerdicts` call now use it, so the tally has one source of truth. No debt remains.

## TD-009 — Verify stage processes findings/skeptics serially (MEDIUM)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-14
**File:** internal/verify/pipeline.go:159
**Issue:** `runVerify` iterates findings (and the skeptics within each finding) strictly serially — no goroutines, errgroup, or concurrency cap — unlike the parallel review fan-out (`--max-parallel`). A `--thorough` run is findings × votes provider calls back to back, so a 50-finding × 3-skeptic verify is 150 sequential LLM round-trips (minutes of wall-clock) with no knob to speed it up.
**Why accepted:** Serial keeps the stage simple, deterministic, and easy to reason about for the first cut; AC 04-03 Performance explicitly frames verification as "sequentially per skeptic". The serial behavior is now documented in the `Verify` doc comment so the cost is not a surprise. Parallelization is an optimization, not a correctness gap.
**Fix in:** Epic 3.0 follow-up (or a perf sprint) — process jobs through a bounded worker pool honoring a `--max-parallel`-style limit (reuse fanout's semaphore pattern), preserving deterministic artifact order by keying results back to findings rather than append order.

## TD-010 — Re-emit of the four verify artifacts is not transactional (MEDIUM)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-14
**File:** internal/verify/pipeline.go:167-219
**Issue:** `runVerify` writes findings.json (`ReEmitFindings`), then verification.json (`WriteVerification`), the manifest stage, and summary verdictCounts as four separate atomic-per-file steps. A failure after `ReEmitFindings` but before the others returns an error with findings.json already mutated while verification.json/summary.json are stale or absent — an inconsistent on-disk reconciled tree, even though each individual file is written atomically.
**Why accepted:** Mirrors the existing `reconcile.Emit` pattern, which writes its five artifacts the same non-transactional way (render-all-then-write per file, no cross-file commit), so this is consistent with the established codebase approach rather than a new defect; same theme as [[TD-005]] (atomic-writer durability). A mid-write failure here is rare (local disk, in-process), and the next verify run overwrites all four consistently.
**Fix in:** Cross-cutting artifact-emission follow-up — stage all reconciled artifacts to temp files and rename them as a group (or build verification.json from the in-memory verdict map to drop the second findings.json read), folding into the same shared atomic-writer work as [[TD-005]]/[[TD-006]].

## TD-011 — Degraded verification records carry no model attribution (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-14
**File:** internal/verify/pipeline.go:238-243
**Issue:** `verifyFinding` sets `VerificationResult.Model` only on the multi-skeptic invocation path; the `no_eligible_skeptic` and `tool_harness_unavailable` early returns leave `Model` empty in verification.json. The `Verify` doc advertises verification.json as the "different-model evidence" audit record, so a degraded record silently omits the model attribution.
**Why accepted:** For `no_eligible_skeptic` an empty model is accurate — no skeptic ran, so there is no model to attribute. For `tool_harness_unavailable` the eligible skeptic was never invoked. The omission is truthful, not misleading, and the Notes field already explains why. Low audit-value gap.
**Fix in:** Audit-trail polish — on `tool_harness_unavailable`, record the would-be skeptic name/model (selection already resolved them) so the human can see which skeptic was blocked.

## TD-012 — Verify-failure error mapping differs between CLI entry points (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-14
**File:** cmd/atcr/verify.go:72 vs cmd/atcr/review.go:187
**Issue:** A non-`ErrNoReconciledFindings` failure from `verify.Verify` is mapped to a bare `usageError(err)` by `atcr verify`, but wrapped as `usageError(fmt.Errorf("review failed: %w", err))` by `atcr review --verify`. Same orchestrator, two different message shapes (and prefixes) for the same underlying failure — minor inconsistency for scripts keying on stderr text. Both still map to exit 2.
**Why accepted:** Cosmetic; exit codes are identical and `ErrNoReconciledFindings` (the common case) is already mapped identically across all three entry points. The "review failed:" prefix is arguably correct context for the chained path.
**Fix in:** Small cleanup — route both through one shared `verifyFailureError` helper so the non-reconciled-findings failure reads identically regardless of entry point.

## TD-013 — Tool-harness-unavailable stderr line echoes the snapshot path (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-14
**File:** internal/verify/pipeline.go:147
**Issue:** When the snapshot/dispatcher cannot be built, `runVerify` writes `tool harness unavailable (%v); skeptics degrade to unverifiable` to stderr with the `buildDispatcher` error verbatim, which can include the absolute `repoRoot`/snapshot path and git internals — captured into CI logs.
**Why accepted:** Local-CLI stderr, low impact; the path is the user's own repo, not a secret. Useful for diagnosing a snapshot failure during development.
**Fix in:** CI-log-hygiene follow-up — surface a generic "tool harness unavailable" line to stderr and log the path-bearing detail at debug level only.

## TD-014 — Category exact-match in summary/report display is casing-fragile (LOW)
**Origin:** Phase 4, task 4.5.A gate review, 2026-06-14
**File:** internal/reconcile/merge.go:143 (modalCategory), internal/reconcile/reconcile.go:80 (summary out_of_scope count), internal/reconcile/emit.go:182 (report out-of-scope section)
**Issue:** The gate predicate `IsFailing` now normalizes the out-of-scope category (Phase 4, task 4.6), but the other consumers of `CategoryOutOfScope` still compare the raw token exact-match: `modalCategory` keeps the model's verbatim casing, and the summary `out_of_scope` count and the report's out-of-scope section both test `category == CategoryOutOfScope`. A non-canonical category casing (`Out-Of-Scope`) is now correctly excluded from the gate but would still be mis-counted in summary.json and mis-rendered in report.md (shown as in-scope), so the gate and the human-facing artifacts could disagree.
**Why accepted:** The CI-blocking decision (the gate) is the correctness-critical path and is now hardened; the display inconsistency is cosmetic and only triggers on non-canonical model output, which the personas do not produce (the `_base.md` rubric emits the canonical lowercase token). The exact-match in these three display sites predates Phase 4 — it is not a regression introduced here, and canonicalizing them touches the summary/report rendering that Phase 5 also revisits.
**Fix in:** Reconcile/report follow-up (or Phase 5) — canonicalize category once at parse or in `modalCategory` (lower+trim) so the gate, the summary out_of_scope count, and the report out-of-scope section all agree; add a fixture with non-canonical category casing.
