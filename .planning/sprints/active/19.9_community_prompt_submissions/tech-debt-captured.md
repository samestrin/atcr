# Tech Debt Captured — Sprint 19.9 (Community Prompt Submissions)

Deferred items surfaced during `/execute-sprint`. MEDIUM/LOW adversarial findings land here per the sprint's inline-fix bar (CRITICAL/HIGH fixed inline). Read by `/execute-code-review`.

## TD-001 — Zero-case fixture (Total==0) clears the submit gate (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-10
**File:** internal/personas/submit.go:36
**Issue:** `SubmitGate` treats `HasFixture:true, Total:0` as a pass because `Passed != Total` is `0 != 0` (false), so a fixture that asserts nothing proceeds to submission. This is currently mandated by AC 01-03 Scenario 2 and unreachable via the production `TemplateFixtureRunner` (renderFixture always returns `Total:1`; other paths return `HasFixture:false`), so it is latent — but any future/installed runner that yields an empty multi-case fixture would clear the gate with zero verification.
**Why accepted:** AC 01-03 Scenario 2 explicitly requires Total==0 to proceed like a full pass and to document the choice inline (done). Changing it now would violate the acceptance criteria; the risk is latent given the only production runner never emits Total==0 with HasFixture:true.
**Fix in:** future sprint / revisit if an installed multi-case fixture runner is added — require `outcome.Total > 0` for a clean pass, or gate zero-case fixtures behind an explicit opt-in, and reconcile with AC 01-03 Scenario 2. Also (Phase 1 gate review): `personas test` emits `WARN: no test cases defined` for Total==0 but `submit` is silent — consider surfacing the same WARN so an empty fixture is not silently treated as a full pass.

## TD-002 — `submit` exits 0 silently on a clean gate pass while Long help promises a fork+PR (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-10
**File:** cmd/atcr/personas.go:350
**Issue:** In Phase 1 the success path hands off to the no-op `personasSubmitContinuation` stub and exits 0 with no stdout, while the command's Long help states it "opens a fork+PR." A user running `submit` in the Phase-1 interim sees silent success, which could be mistaken for a completed submission.
**Why accepted:** Phase 2 replaces the stub with the real `gh` fork+PR flow and owns the success/PR-URL output; adding interim output now would be thrown away and could conflict with Phase 2's output design. The interim state is not shipped independently (all phases land in one sprint before release).
**Fix in:** Phase 2 (task 2.2) — the fork+PR GREEN step prints the PR URL to stdout on success, resolving the silent-exit gap.
**Resolved:** 2026-07-10 — Phase 2 GREEN wired `personasSubmitContinuation` to `Submit`, which prints the PR URL to stdout on success (test `TestPersonasSubmit_ForkPRHappyPath`).

## TD-003 — Submit gate green-lights non-submittable tiers (built-in/embedded) (MEDIUM)
**Origin:** Phase 1, task 1.5 integration gate review, 2026-07-10
**File:** internal/personas/submit.go:25
**Issue:** `SubmitGate` returns only `error` and the continuation seam is `func(name string) error`, so Phase 2 receives a bare name with no resolved-source/tier signal. The reused fixture gate green-lights any resolution tier (built-in, embedded community, or installed on-disk unit), so a built-in or embedded-library name passes the gate even though there is no locally-authored file to fork/PR. Submit is meant to contribute a locally-tuned persona.
**Why accepted:** Phase 1 ACs (01-01/02/03) scope the gate to name validation + fixture pass/fail only; the resolved-unit copy and tier awareness are Phase 2's focus ("copy the resolved persona unit into the fork"). Changing the seam signature or adding a tier guard now would be speculative ahead of Phase 2's resolution design.
**Fix in:** Phase 2 (tasks 2.1/2.2) — resolve the persona unit for the fork copy, have the seam carry the resolved unit/tier (e.g. path + source), and reject non-installed/built-in tiers with a clear "nothing local to submit" error so only submittable local units reach fork+PR.
**Mitigation this sprint:** 2026-07-10 — Phase 2 `copyPersonaUnit` reads the unit via `personaPath(personasDir, name)`, so a name with no locally-installed `.yaml` fails at copy time (`reading persona ...`). Still partial: the copy runs AFTER the fork, so a non-local name can leave a fork side effect and gets a generic read error rather than the intended pre-fork "nothing local to submit" guard. Tier awareness/pre-fork rejection still deferred.

## TD-004 — Non-fatal gh reuse detection keys on a stderr substring (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-10
**File:** internal/personas/submit.go
**Issue:** `forkAlreadyExists` and `existingPRURL` decide the benign "fork already exists" / "PR already exists" reuse paths by matching the case-insensitive substring `"already exists"` in gh's stderr. A future `gh` wording change would flip these expected re-submission cases into hard failures (a re-fork erroring out, or a duplicate-PR attempt instead of surfacing the existing PR URL).
**Why accepted:** Inherent to the `gh`-CLI shell-out approach chosen for this epic (documented in gh-fork-pr-integration.md) — gh exposes no stable machine-readable "already exists" signal for these commands, so string-matching its human output is the pragmatic option. Low blast radius: the failure mode is a clear error the user can act on, not silent data loss, and it only triggers on a gh output change.
**Fix in:** future sprint / on a gh dependency bump — revisit if `go-gh` exposes structured fork/PR-exists results, or pin/version-check the gh output contract; add a regression fixture capturing the current gh wording.

## TD-005 — First-time fork creation can 404 the immediate clone (async provisioning) (LOW)
**Origin:** Phase 2, task 2.5 integration gate review, 2026-07-10
**File:** internal/personas/submit.go
**Issue:** `ghSubmitter.Fork` runs `gh repo fork --clone=false` and `PushBranch` then immediately `git clone https://github.com/<owner>/atcr.git`. GitHub fork creation is asynchronous, so on a user's very first submission the clone can race ahead of fork provisioning and 404 before the fork exists.
**Why accepted:** Lives entirely in the gh/git shell-out layer that AC 02-03 keeps out of tests, so it cannot be reproduced in the unit suite; blast radius is a transient, retry-able "clone failed" error on the first-ever submit only (the fork exists on the retry), not data loss or a silent wrong result. Adding retry/poll logic now is speculative ahead of real-world usage of an unreleased command.
**Fix in:** future sprint / first real end-to-end run — have `Fork` block until the fork resolves (poll `gh api repos/<owner>/atcr` with a bounded retry) before `PushBranch` clones, or clone with a short bounded retry-on-404.
