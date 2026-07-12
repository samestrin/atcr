# Tech Debt Captured — Sprint 20.0 Standalone Skill Release

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — findings-format.md points outside the skill dir (MEDIUM)
**Origin:** Phase 1, task 1.5 Phase-1 gate review, 2026-07-11
**File:** skill/findings-format.md:3
**Issue:** The relocated `findings-format.md` secondary file delegates the canonical column contract to `docs/findings-format.md`, a repo-relative path outside `skill/`. In a standalone `.claude/skills/atcr/` install (which copies only `skill/*.md`), that pointer dangles because `docs/` is not shipped with the skill.
**Why accepted:** `findings-format.md` is a byte-for-byte verbatim relocation (AC 01-03 requires no content change during the move); altering its body to inline the contract or repoint to a public URL is a deliberate content tradeoff, not a mechanical fix, and the review-flow row format is already carried verbatim in `host-review.md` so the runtime host review is not blocked. host-review.md example row keeps the 8-column format available at runtime.
**Fix in:** Phase 3 Story 4 (docs accuracy pass) or a follow-up — either inline the minimal per-source 8-column / reconciled 9-column contract into `findings-format.md`, or repoint it to the public docs URL rather than a repo-relative `docs/` path.

## TD-002 — Story 3 / AC1 (external `go install`) punted to Epic 33.2 — root cause is repo privacy (HIGH)
**Origin:** Phase 2, task 2.4 pre-flight (blocker; Story 3 paused), 2026-07-11
**File:** go.mod:41
**Issue:** `go install github.com/samestrin/atcr/cmd/atcr@latest` cannot work for external users. Two layered causes, the second binding:
1. The root `go.mod` carries `replace github.com/samestrin/atcr/reconcile => ./reconcile` (in-tree Epic 8.0 module). Go refuses to `go install <module>@<version>` any module whose published `go.mod` has a replace directive.
2. **The `samestrin/atcr` repo is PRIVATE.** A private module is not on the public proxy (`proxy.golang.org` 404s both the root module `@latest` and `reconcile/v0.1.0`), so external `go install` is impossible regardless of the replace directive. Removing the replace is necessary-but-insufficient; the binding constraint is repo privacy. Removing the replace also actively breaks CI while private: it forces the self-hosted `gauntlet` runner to fetch `reconcile` via direct `git ls-remote` (no github.com auth for module fetches) → `fatal: could not read Username`.
This makes AC1 (install via `go install`) unsatisfiable until the repo is public, makes install.sh (AC4) a wrapper over a currently-unusable command, and makes the `go install ...@latest` lines in README.md + docs/skill-usage.md premature.
**Mitigation this sprint:** Story 3 (install.sh + install-script test) paused before any code was written — no knowingly-broken script or permanently-red integration test committed. Story 2 (backend-contract test) is complete and unaffected. The reconcile-publication + `go.mod` change was **prototyped end-to-end** (tag `reconcile/v0.1.0`, drop replace, `go.sum`, local `go.work`) and proven to work the instant the repo is public — that prototype's gotchas are recorded in the Epic 33.2 plan. Prototype artifacts (PR, chore branch) were cleaned up; `main` is untouched (replace directive intact, CI green).
**Fix in:** **Epic 33.2** (Public Launch — `.planning/epics/active/33.2_public_launch.md`). External `go install` is gated on the repo going public, which happens at launch (Epic 33 cluster). 33.2 owns: repo → public, publish `reconcile` + drop the replace directive, deliver `install.sh` + real-install integration test (Story 3), and finish the public-dependent doc accuracy (Stories 4/5 remainder). Note: Epic 21.0 (release automation) shares the same public-repo prerequisite and is effectively gated on the same flip despite its lower number.
