# Tech Debt Captured — Sprint 17.0 (Auto-Merged Fixes)

Deferred MEDIUM/LOW findings surfaced during `/execute-sprint`. Read by
`/execute-code-review` and pre-seeded into the adversarial TD stream.

## TD-001 — GitHub token `contents: write` scope unvalidated by the stub (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**Issue:** Spike 1.2 drove auth against an `httptest.Server` that accepts any bearer token, so the "auth flows on every call" finding validates header plumbing only, not that a real `GITHUB_TOKEN` carries `contents: write`. Creating blobs/trees/commits/refs 403s with a default read-only token or on fork PRs — an integration failure the mock hides.
**Why accepted:** Phase 1 is a stubbed spike by design; the plan never intends a live GitHub call. Story 4/6 own the real precondition.
**Fix in:** Phase 4/5 — record `contents: write` as an explicit `--auto-fix` backend precondition in Story 6's gate (`validateAutoFixBackend`) and document the required token scope; optionally add a permission smoke check.

## TD-002 — Git Data API request/response shapes retained only as prose (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**File:** .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md:88
**Issue:** The 1.2 spike httptest driver was deleted, so the exact request-body keys (blob/tree/commit/ref) and the 422 `Message == "Reference already exists"` extraction survive only as prose in the findings note, not as an executable fixture. (Contrast 1.1, whose contract is retained as `internal/autofix/gitdiff_contract_test.go`.)
**Why accepted:** Phase 4 RED (task 4.1, AC 04-03) writes these `httptest`-driven tests against method+path routing anyway, re-establishing executable fixtures; the prose note + the retained stub-routing pattern are sufficient scaffolding until then.
**Fix in:** Phase 4 — Phase 4.1 RED tests supersede the prose by encoding the 4 request/response bodies and the 422 path as real assertions.

## TD-003 — Findings note overstates plumbing; 422 contract is POST-path-dependent (LOW)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**File:** .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md:88
**Issue:** The note header says "via existing `postDo`/`get`" but the 1.2 spike exercised only `postDo`. `get` returns a plain `fmt.Errorf`, not `*APIError`, so the AC 04-02 422-collision contract (`errors.As(err, *APIError)`) holds only because all 4 Git Data calls are POST — a silent trap if any is later switched to GET.
**Why accepted:** Cosmetic/documentation nuance; all 4 Git Data calls are POST by design and Story 4 has no GET on the mutation path.
**Fix in:** Phase 4 — when Story 4 lands, confirm no mutation-path call uses `get`, or extend `get` to also return `*APIError` if one is ever needed there.

## TD-004 — Create entry silently overwrites an existing target (MEDIUM)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-03
**File:** internal/autofix/apply.go:108
**Issue:** A create entry (`f.IsNew`) whose target already exists is applied against empty content and atomically overwritten, rather than rejected the way `git apply` refuses a create over an existing file. The clobber is silent.
**Why accepted:** Not data loss — the pre-existing content is captured by `atomicfs.BackupToDotBak` before the write and recorded in `BackupMap` with a non-empty backup path, so Story 3's Revert restores it. Only the git-apply-style "refuse create over existing" nicety is missing; the applied result is correct and revertible.
**Fix in:** A later hardening pass — stat the target in the `IsNew` branch and fail the entry with an "already exists" per-file error, or route it through the modify path against the real on-disk content.

## TD-005 — Symlink target modify/delete reverts by deletion, not link restore (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-03
**File:** internal/autofix/apply.go:129
**Issue:** When the patch target is itself a symlink, `atomicfs.BackupToDotBak` Lstat-skips it and returns `("", nil)`, so a modify replaces the link with a regular file (or a delete unlinks it) and `BackupMap` records an empty backup path. Story 3's Revert treats an empty backup as "run-created / remove on revert", so it would delete the file rather than restore the original symlink.
**Why accepted:** Symlinked patch targets are an edge outside the technical-debt-fix use case. The security-relevant vector (a write escaping the tree via a symlinked *directory component*) is closed in task 2.3. Revert semantics for a symlink *leaf* are a Story-3 concern.
**Fix in:** Phase 3 (Story 3, Revert) — detect a symlink target explicitly and either refuse the entry or record enough state to restore the link instead of deleting it.
