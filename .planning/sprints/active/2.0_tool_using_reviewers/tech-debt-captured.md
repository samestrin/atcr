# Tech Debt Captured During Sprint 2.0 Execution

Items deferred during `/execute-sprint`. Read by `/execute-code-review` (Phase 1 init) and pre-seeded into the adversarial TD stream (SOURCE=execute-sprint).

---

## TD-001 — Phase 2 jail Resolve signature/algorithm diverges from validated spike (MEDIUM)
**Origin:** Phase 1, task 1.6 Phase 1 gate review, 2026-06-13
**File:** .planning/sprints/active/2.0_tool_using_reviewers/sprint-plan.md:332
**Issue:** The Phase 2 GREEN task (2.5) still specifies a single-arg `Resolve(path)` using `filepath.Abs`, but the validated spike mandates a two-arg `Resolve(rootCanon, rel)` that uses `filepath.Join(rootCanon, clean)` against a pre-canonicalized root and drops `filepath.Abs`. If Phase 2 authors RED tests from the stale plan wording, the tests target the wrong signature.
**Why accepted:** Phase 1 is gated and stops here; the corrected algorithm is documented authoritatively in clarifications/spike-findings.md (the source of truth Phase 2 reads). No code exists yet to be wrong.
**Fix in:** Phase 2 (Foundation, task 2.4/2.5) — when writing the jail RED tests, follow spike-findings.md sections "Spike 1.2" + note C1-C8, not the stale sprint-plan 2.5 prose; target `Resolve(rootCanon, rel)` with a canonical root.

## TD-002 — Canonical-root invariant lives only in prose, not the Phase 2 task spec (LOW)
**Origin:** Phase 1, task 1.6 Phase 1 gate review, 2026-06-13
**File:** .planning/sprints/active/2.0_tool_using_reviewers/sprint-plan.md:332-333
**Issue:** The "jail root and worktree root must be EvalSymlinks-canonicalized at construction" invariant (the spike's single most important carry-over, needed because macOS aliases /var and /tmp) is stated only in spike-findings.md prose. The Phase 2 GREEN spec for `Jail{Root string}` and `Snapshot.For` does not call it out, so a Phase 2 author could omit it and produce false-rejection bugs on macOS temp paths.
**Why accepted:** Documented in spike-findings.md with a dedicated RED-test recommendation; Phase 1 stops at the gate.
**Fix in:** Phase 2 (Foundation) — assert the canonical-root invariant in the jail RED tests (a legitimate in-root file under a symlinked temp root must resolve and be accepted), and EvalSymlinks the root in the `Jail`/`Snapshot` constructors.

## TD-003 — read_file lacks symlink-swap protection on non-unix platforms (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-06-13
**File:** internal/tools/open_other.go:9
**Issue:** On non-unix build targets `openReadOnly` omits `O_NOFOLLOW`, so `read_file` reopens the EvalSymlinks->Open TOCTOU window the unix build closes (grep/list_files are unaffected — they filter symlinks by file type before opening). atcr targets darwin/linux where `O_NOFOLLOW` is active via the `//go:build unix` path, so this is a residual only for hypothetical non-unix deployment.
**Why accepted:** Supported platforms (darwin/linux) are covered; the degradation is already documented in the open_other.go comment. Closing it on Windows needs a different mechanism.
**Fix in:** Future platform-support work — add a post-open inode re-check (`os.SameFile`) on platforms without `O_NOFOLLOW`, or document non-unix as unsupported for untrusted snapshots.

## TD-004 — Snapshot mode not yet recorded in manifest.json (LOW)
**Origin:** Phase 2, task 2.5 (Story 3), 2026-06-13
**File:** internal/tools/snapshot.go
**Issue:** AC 03-02 Scenario 5 and AC 03-03 Scenarios 4-5 require `manifest.json` `stages.review` to record `snapshot_mode` (live/worktree), `head_sha`, and `snapshot_worktree_path`. Phase 2 is tools-only and gated, and `manifest.go` lives in `internal/payload/` (spike note C2), which Phase 2 does not touch. `SnapshotFor` returns `(root, cleanup, err)`; the engine can derive mode (root==repoRoot => live) at integration time.
**Why accepted:** Manifest wiring is an engine-integration concern (Phase 3 calls `SnapshotFor` at `engine.go:228`; Phase 5 extends `payload.Manifest`). Phase 2 DoD (task 2.7) scopes Story 3 to escape-vector rejection + snapshot lifecycle, not manifest recording.
**Fix in:** Phase 3/5 — when wiring `SnapshotFor` into the agent loop, record `snapshot_mode`/`head_sha`/`snapshot_worktree_path` into `internal/payload/manifest.go` review stage and add the manifest assertion tests from AC 03-02/03-03.

## TD-005 — Worktree add-retry uses repo-wide `git worktree prune` (MEDIUM)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-06-13
**File:** internal/tools/snapshot.go
**Issue:** On a failed `git worktree add`, recovery runs a repo-wide `git worktree prune`. A concurrent `SnapshotFor` whose worktree is mid-registration could have its entry pruned by another call's failure path, corrupting that run's snapshot. Concurrent `SnapshotFor` is a documented supported scenario (AC 03-02 Edge 5). Likelihood is low because each call uses a unique `os.MkdirTemp` leaf, so `add` rarely fails.
**Why accepted:** Low likelihood (unique temp leaves make add-collision near-impossible); the live single-review path is unaffected. Phase 2 is gated and stops at the boundary.
**Fix in:** Phase 3 (engine integration / concurrency) — guard add/prune with a per-manager mutex, or scope recovery to `worktree remove --force <leaf>` for the specific stale leaf instead of repo-wide `prune`.

## TD-006 — Fan-out lane goroutines lack panic recovery (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-13
**File:** internal/fanout/engine.go:202
**Issue:** `Engine.Run` spawns a goroutine per parallel slot (plus one serial-lane goroutine) and writes `results[i]` from each, but neither wraps `invokeSlot`/`invokeAgent` in a `recover()`. A panic anywhere in an agent invocation (e.g. a future nil-deref in the tool loop or JSON path; the dispatcher already recovers its own tool panics) crashes the whole process and the WaitGroup never drains — contradicting the Run doc's "WaitGroup always drains" guarantee and the project's "goroutines must recover from panics" standard. Pre-existing in 1.x (single-shot `Complete` could also panic); the tool loop widens the surface.
**Why accepted:** No known panic path today (the dispatcher recovers tool panics; Chat/JSON ops are panic-free in practice); the fix changes the engine's shared concurrency model (both lanes) which is broader than the Story 1 loop scope. Severity LOW per the adversarial reviewer.
**Fix in:** A focused engine-hardening pass — wrap each parallel-lane goroutine body and the serial-lane loop body in a deferred `recover()` that records a `StatusFailed` result for the affected slot, so a single agent panic degrades to one failed slot instead of a process crash.

## TD-007 — AC 02-02 EC3 / AC 02-04 S2 same-turn double-trip is unreachable under Model A (LOW)
**Origin:** Phase 3, task 3.5 (Story 2 budgets), 2026-06-13
**File:** internal/fanout/loop.go
**Issue:** AC 02-02 Edge Case 3 and AC 02-04 Scenario 2 describe `max_turns` and `tool_budget_bytes` tripping on the SAME turn (turn 3 executes, pushes bytes over budget, and both are recorded). Under the user-confirmed "Model A" max_turns semantics (sprint clarification 2026-06-13), the turn that hits the turn budget does NOT execute its tool_calls, so it cannot also accumulate bytes past the budget on that same turn — the two budgets therefore trip on different turns, never simultaneously. The engine still supports multiple `TrippedBudgets` entries (de-duped) and budgets trip independently; only the literal same-turn-both case is unreachable.
**Why accepted:** Direct corollary of the Model-A decision the user explicitly confirmed (Q1, 98% confidence), which prioritized reconciling AC 01-03 EC3 with AC 02-01 S1. The reachable multi-budget behavior is tested; the unreachable same-turn variant is a spec artifact of the alternative "Model B" semantics.
**Fix in:** Documentation reconciliation — when the ACs are next revised, update AC 02-02 EC3 / AC 02-04 S2 to reflect Model-A semantics (budgets trip across turns, not necessarily the same turn), or revisit the max_turns model if same-turn double-trip is later deemed required.
