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

## TD-008 — VerdictCounts computed twice risks tally drift (LOW)
**Origin:** Phase 3, task 3.5 gate review, 2026-06-14
**File:** internal/verify/emit_verification.go:58
**Issue:** `WriteVerification` derives `VerdictCounts` internally (case-insensitively) but does not return them, and there is no exported helper to compute a `VerdictCounts` from `[]VerificationResult`. `UpdateSummaryVerdicts(reviewDir, counts)` requires a `VerdictCounts`, so the Phase 4 pipeline must recompute the tally separately to feed it — two sources of truth that could drift if the recompute uses different normalization.
**Why accepted:** AC 03-02 pins `WriteVerification(reviewDir, results) error`, so the counts cannot be returned without breaking the signature; the clean fix (an exported `CountVerdicts([]VerificationResult) VerdictCounts`) is Phase 4 plumbing convenience, not Phase 3 emit-layer scope. No live drift today — Phase 3 has no orchestrator caller.
**Fix in:** Phase 4 (verify pipeline) — add an exported `CountVerdicts([]VerificationResult) VerdictCounts` used by both `WriteVerification` and the pipeline's `UpdateSummaryVerdicts` call, so the tally has one source of truth.
