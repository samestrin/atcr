# Code Review Stream - 1.4_max-parallel-concurrency-cap (Epic)

**Started:** June 12, 2026 01:25:43PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: `max_parallel` resolves through CLI > project > registry > embedded with the chosen default; invalid (negative) values fail with a usage error.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/precedence.go:16` (DefaultMaxParallel=10), `:56` (applied in ResolveSettings), `:80-86` (CLI tier validates negative → error); `internal/registry/config.go:99` (registry-tier `MaxParallel *int`), `:145-146` (load-time negative validation); `internal/registry/project.go:39` (project-tier field), `:145-146` (load-time negative validation); `cmd/atcr/review.go:30,168-170` (`--max-parallel` flag → CLIOverrides)
- **Notes:** Full CLI > project > registry > embedded chain present. `*int` pointers preserve explicit `0` (unbounded). Negative rejected at every tier with message `max_parallel must be >= 0 (0 = unbounded)`.

### Criterion: With `max_parallel: N`, an engine test with a roster larger than N proves peak observed concurrency never exceeds N (the fake completer already tracks peak).
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine_test.go:306-319` (TestRun_MaxParallelBoundsPeakConcurrency: WithMaxParallel(2) on 5-agent roster, asserts peak ≤ 2); plus `:321` (zero = unbounded) and `:333` (N > roster = unbounded) edge cases
- **Notes:** Semaphore (`internal/fanout/engine.go:103-145`) gates provider calls — the exact resource the fake completer's peak counter measures.

### Criterion: Serial-lane behavior and the WaitGroup drain guarantee under cancellation are unchanged (existing engine tests stay green).
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:128-141` (ctx-aware acquire: cancellation short-circuits to `invokeSlot` → timeout result, still `wg.Done()`s — no leak); `:147-176` (serial lane untouched); `internal/fanout/engine_test.go:345-360` (TestRun_MaxParallelDrainsUnderCancellation)
- **Notes:** Serial lane (`Serial` slots) bypasses the semaphore entirely. Drain invariant preserved: a goroutine that loses the acquire race to `ctx.Done()` still records a result and calls `wg.Done()`.

### Criterion: registry.md documents the setting; `atcr init` template carries it as a comment.
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/registry.md:90` (settings-table row), `:43,:79` (example configs), `:128,:134` (precedence section); `internal/registry/project.go:64-65` (init template writes a `# max_parallel: ...` comment line AND an active `max_parallel: 10` key); `cmd/atcr/init_test.go:38-39` (asserts template carries the key)
- **Notes:** Template exceeds the AC — it carries both an explanatory comment and a live, baked default key.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md in epic mode)
**Files Reviewed:** 10 (engine.go, fanout/review.go, precedence.go, config.go, project.go, cmd/review.go, engine_test.go, precedence_test.go, review_test.go, init_test.go)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic)

Three hostile reviewers ran independently; reviewers 1 & 3 additionally ran `go test -race` and source-mutation tests. Concurrency core (semaphore token balance, WaitGroup drain, data-race safety, end-to-end MaxParallel threading) confirmed **correct** — mutation tests proved the cap-bounds test fails on a neutered or off-by-one semaphore, and the drain test fails when results are dropped.

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 5

### Notable findings
- **MEDIUM** `manifest.go:38` — `omitempty` on `MaxParallel` erases the `0`/unbounded value, defeating the field's stated post-hoc-diagnosis purpose (unbounded run indistinguishable from a pre-1.4 manifest).
- **LOW** ×2 (precedence) — `applyTier` trusts file tiers without re-validation (defensive nit); embedded default `10` vs the "0 = unbounded" user-facing messaging (doc nit).
- **LOW** ×3 (tests) — cap tests use a 30ms delay rather than a barrier (robust in practice, no flakes in 60+ runs); drain tests don't assert zero goroutine leak via goleak; serial-lane test labeling clarity.

Dropped (already addressed): reviewer 1's "serial lane makes effective max = max_parallel + 1" observation is already documented at `docs/registry.md:90`.
