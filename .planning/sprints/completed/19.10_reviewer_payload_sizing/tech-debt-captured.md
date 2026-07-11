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

## TD-004 — on_overflow policy dispatch (applyOverflowPolicy) is not wired into the live review path (MEDIUM)
**Origin:** Phase 4, task 4.3 gate review, 2026-07-11
**File:** internal/fanout/review.go:877 (buildSlots dispatch branch); internal/fanout/overflow.go:96 (applyOverflowPolicy)
**Issue:** `applyOverflowPolicy` (Task 04, the resolved `on_overflow` chunk/truncate/fallback/fail dispatcher) is implemented and unit-tested but never called from the live dispatch path. `buildSlots` still selects chunking via `cfg.Settings.ReviewStrategy == reviewStrategyChunked`, not via the resolved `on_overflow` policy. Consequences: (1) the `on_overflow` config key is resolved (Task 05) but does not actually drive degradation on the default review path; (2) the F8 `degradation_action` field therefore reports the MECHANISM that fired (chunk when review_strategy=chunked split; truncate when the per-agent byte shed dropped files) rather than the configured `on_overflow` policy — a consumer expecting the field to mirror `on_overflow` would be misled.
**Why accepted:** Out of scope for F6/F7/F8 (Phase 4). No sprint task wires `applyOverflowPolicy` into `buildSlots`; the chunk primitive is reached via `review_strategy`, and the byte shed is the F2 default. Below the inline-fix bar for this phase (the diagnosability field is honest about the mechanism; documented as such in `AgentStatus.DegradationAction`).
**Fix in:** A follow-up sprint/epic — route `buildSlots`' overflow decision through `applyOverflowPolicy(resolvedOnOverflow, ...)` so the resolved policy drives dispatch and `degradation_action` mirrors the configured policy, retiring the `review_strategy`/`on_overflow` split.

## TD-005 — Aggregate timeout scales by single worst persona, not contended parallel-lane load (LOW)
**Origin:** Phase 4, task 4.5 gate re-review, 2026-07-11
**File:** internal/fanout/timeout.go:69 (maxLaneChunkTotal); internal/fanout/review.go:524 (runEngine)
**Issue:** The aggregate `runCtx` deadline is scaled by the largest SINGLE-persona `ChunkTotal` (capped at `chunkTimeoutCeilingFactor` = 8x). This fully covers one chunked persona, but when several chunked personas contend for a small `max_parallel` the total wall-clock can exceed the provisioned deadline — e.g. greta+vera+brad × 6 chunks = 18 slots at `max_parallel: 4` ≈ 5 sequential waves, whereas the aggregate only provisions `scaledTimeoutSecs(base, 6)` for the single worst persona.
**Why accepted:** A bounded, conservative heuristic ("conservative estimate, not a live measurement"), dramatically better than the prior flat wall and clamped to `registry.MaxTimeoutSecs`. Not an AC6 violation for the named single-persona-load scenario (AC6 is now verified for both serial and parallel single chunked personas). Below the inline-fix bar.
**Fix in:** An optional future refinement — derive the aggregate from `ceil(total chunked slots / max_parallel)` (wave count) rather than the single-persona max when the parallel lane is contended.

## TD-006 — max_sprint_plan_bytes absent from docs/registry.md canonical reference (LOW)
**Origin:** Phase 5, task 5.3 gate review, 2026-07-11
**File:** docs/registry.md:122,164
**Issue:** The `max_sprint_plan_bytes` config key (F9) is documented in the `.atcr/config.yaml` scaffold and struct comments but not in the canonical reference `docs/registry.md` — neither the settings table (~line 122) nor the per-field precedence-resolution list (~line 164). Same documentation gap as TD-001 (which covers `on_overflow`); both new 19.10 config keys are missing from the reference doc. Since registry.md advertises "unknown keys are load errors," an undocumented-but-valid key is user-invisible.
**Why accepted:** Documentation-surface only; below the CRITICAL/HIGH inline-fix bar. Code-level docs (scaffold comment, struct doc-comments) already describe the key, its 65536 default, and `>0` validation, so it is discoverable to config authors. Batch with TD-001 in a single docs pass.
**Fix in:** Phase 5 final documentation sweep or a follow-up docs pass (with TD-001) — add a `max_sprint_plan_bytes` row (default 65536 / 64KB, `>0`, registry+project tiers, no CLI flag) to the settings table and the precedence list.

## TD-007 — Live-audit skip-guard counts aggregate reachable agents, not the 5 gated personas (LOW)
**Origin:** Phase 5, task 5.3 gate review, 2026-07-11
**File:** examples/19.10-live-audit.sh:63-67 (reachable-count skip guard)
**Issue:** The skip guard proceeds when `>=1` agent is reachable via `atcr doctor`, rather than checking reachability of the five gated personas (`dax`/`otto`/`greta`/`vera`/`brad`). An environment where the litellm proxy is up but those five are not in the resolved roster would proceed and fail Gate B (exit 1) instead of skipping — conflating environment misconfiguration with a real regression.
**Why accepted:** Marginal — the harness is deliberately env-coupled to the 19.6 roster (the committed local `.atcr/config.yaml` lists exactly that panel), so in practice a reachable proxy means the five personas are present. Below the inline-fix bar; the confirmed 2026-07-11 live run passed with all five present.
**Fix in:** An optional future refinement — intersect the doctor-reachable set with the five `PREV_FAILED_AGENTS` and `skip` (not gate-fail) when none of the five are reachable.
