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
