# Tech Debt Captured — Sprint 19.10 (Reviewer Payload Sizing)

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, `SOURCE=execute-sprint`).

## TD-001 — on_overflow config key missing from docs/registry.md (LOW)
**Origin:** Phase 1, task 1.4 gate review, 2026-07-10
**File:** docs/registry.md:122,164
**Issue:** The new `on_overflow` config key is documented in the `.atcr/config.yaml` scaffold and struct comments but not in the canonical reference `docs/registry.md`. The mirrored `review_strategy` key has both a settings-table row and a slot in the shared-review-settings precedence list; `on_overflow` (identical registry→project precedence) appears in neither.
**Why accepted:** Documentation-surface only; below the sprint's CRITICAL/HIGH inline-fix bar. Code-level docs (scaffold comment, struct doc-comments) already describe the key, values, and default, so the feature is discoverable to config authors.
**Fix in:** Phase 5 (final documentation sweep) or a follow-up docs pass — add an `on_overflow` row to the settings table (default `chunk`, enum chunk/truncate/fallback/fail, note fallback/fail recognized-but-gated, registry+project tiers only, no CLI flag) and to the shared-settings precedence list.

## TD-002 — Degenerate model window (eff==0) falls back to the full global payload (MEDIUM)
**Origin:** Phase 2, task 2.1 gate review, 2026-07-10
**File:** internal/fanout/review.go:951
**Issue:** In the per-agent bulk-shed guard `if eff := EffectiveByteBudget(ac.Model, defaultMaxTokens); eff > 0 && len(mp.Entries) > 0`, a model whose window is ≤ output+promptOverhead (12288 tokens) makes `EffectiveByteBudget` return 0, so the branch is skipped and the agent keeps `mp.Text` — the FULL global-budget payload. A tiny-window model thus gets the LARGEST payload, the inverse of the sizing goal.
**Why accepted:** Latent/unreachable today — the smallest roster window and the unknown-model default are both 32768 (well above the 12288 threshold), so no current model or config triggers it. Becomes reachable only if Epic 19.7 live resolution or a future static-table entry supplies a window ≤12288. Phase 3's `on_overflow` (chunk/truncate) is the designed net for over-window payloads.
**Fix in:** Phase 3 (on_overflow dispatch) or Epic 19.7 — when `eff == 0`, route to the on_overflow policy (or a minimum-viable floor) instead of falling back to the unbudgeted global text.

## TD-003 — Directly-constructed Settings{MaxSprintPlanBytes:0} silently blanks the sprint plan (LOW)
**Origin:** Phase 2, task 2.3 gate review, 2026-07-10
**File:** internal/payload/sprintplan.go:94
**Issue:** `ScopeConstraint(content, 0)` → `capUTF8(plan, 0)` returns an empty plan body with `truncated=true`, injecting a SCOPE CONSTRAINT block whose plan is silently truncated to nothing. All three resolution tiers reject `<= 0` (config.go, project.go, precedence.go), so this is only reachable by a directly-constructed `Settings` that bypasses `ResolveSettings` (tests/embedders). Unlike `payload_byte_budget` (0=unlimited=safe), 0 here fails silently at point of use rather than erroring.
**Why accepted:** Unreachable through any production config path (validation rejects <=0 at every loader + post-resolution). Below the CRITICAL/HIGH inline-fix bar; the fanout test helper was updated to seed the real default so no test is affected.
**Fix in:** A follow-up hardening pass — treat `maxBytes <= 0` in `ScopeConstraint`/`ReadSprintPlan` as a defensive default or explicit error so a bypassed-validation 0 cannot silently blank the plan.
