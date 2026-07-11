# Tech Debt Captured — Sprint 20.0 Standalone Skill Release

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — findings-format.md points outside the skill dir (MEDIUM)
**Origin:** Phase 1, task 1.5 Phase-1 gate review, 2026-07-11
**File:** skill/findings-format.md:3
**Issue:** The relocated `findings-format.md` secondary file delegates the canonical column contract to `docs/findings-format.md`, a repo-relative path outside `skill/`. In a standalone `.claude/skills/atcr/` install (which copies only `skill/*.md`), that pointer dangles because `docs/` is not shipped with the skill.
**Why accepted:** `findings-format.md` is a byte-for-byte verbatim relocation (AC 01-03 requires no content change during the move); altering its body to inline the contract or repoint to a public URL is a deliberate content tradeoff, not a mechanical fix, and the review-flow row format is already carried verbatim in `host-review.md` so the runtime host review is not blocked. host-review.md example row keeps the 8-column format available at runtime.
**Fix in:** Phase 3 Story 4 (docs accuracy pass) or a follow-up — either inline the minimal per-source 8-column / reconciled 9-column contract into `findings-format.md`, or repoint it to the public docs URL rather than a repo-relative `docs/` path.
