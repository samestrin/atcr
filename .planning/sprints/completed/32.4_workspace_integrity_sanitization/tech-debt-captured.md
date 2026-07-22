# Tech Debt Captured — Sprint 32.4 Workspace Integrity Sanitization

## TD-001 — Phase 5 AC4 regression test must catch variable-based git invocations (MEDIUM)
**Origin:** Phase 1, task 1.4 gate ADVERSARIAL REVIEW finding, 2026-07-21
**File:** internal/tools/snapshot.go:60,147
**Issue:** The Phase 1 gate found a 7th production git subprocess in `internal/tools/snapshot.go`'s `git()` helper that invoked git via a resolved-path variable (`gitPath, _ := exec.LookPath("git")` then `exec.Command(gitPath, ...)`). It escaped BOTH the T3 planning grep and the AC4 grep because neither matches a variable first-argument — only the literal `exec.Command("git",...)`. It has been migrated to gitexec as part of the gate fix, but the AC4 regression test specced in task-05 (Phase 5) still only greps for the literal `"git"` string and would NOT re-catch a future variable-based reintroduction.
**Why accepted:** The actual gap (snapshot.go) is fixed now; this is a forward-looking test-completeness note, not a live vulnerability. Resolving the test-design change belongs in Phase 5 where the AC4 test is authored.
**Fix in:** Phase 5 / task-05 — extend the AC4 whole-tree regression test to also flag `exec.LookPath("git")` + `exec.Command(<var>, ...)` / `exec.CommandContext(ctx, <var>, ...)` patterns (AST-based, or a companion grep for `LookPath("git")` outside internal/gitexec), so a variable-indirected git call cannot silently bypass the guarantee.
**Resolved:** 2026-07-22 — Phase 5 AC4 test (`internal/gitexec/gitexec_test.go` `TestAC4_NoBareGitExecOutsideGitexec`) rewritten to an AST classifier that flags any NON-literal `os/exec` command-name (identifier/selector) — i.e. the `exec.Command(gitPath, ...)` variable-indirected form — as an offender outside internal/gitexec, unless the file is one of two documented non-git exec sites (`internal/verify/localvalidate.go`, `internal/sandbox/docker.go`). A literal `"git"` is an offender even inside those two. Import aliases (`xexec "os/exec"`) are resolved so they cannot evade the scan. `TestAC4_MatcherDetectsIndirectedGit` proves the indirected + aliased cases are caught. Verified by fresh-subagent gate re-run.

## TD-002 — pathguard `.env*` segment matching is broad (LOW)
**Origin:** Phase 1, task 1.4 gate ADVERSARIAL REVIEW finding, 2026-07-21
**File:** internal/security/pathguard.go:101
**Issue:** `IsProtectedPath` implements the `.env*` glob as `strings.HasPrefix(low, ".env")`, so it also blocks legitimately-named path segments such as `.environments/`, `.envoy/`, `.envision` — including via layer-2 symlink resolution of a not-yet-created path landing on an `.env`-prefixed segment.
**Why accepted:** Spec-compliant with the plan's stated `.env*` glob, and deliberately fail-closed (over-blocking a rare lookalike is safer than missing a real `.env` secret / executable `.envrc`); the `--allow-config-edits` operator escape valve (Phase 3) covers any legitimate need.
**Fix in:** Future sprint (optional) — if false positives are observed, tighten to `low == ".env" || strings.HasPrefix(low, ".env.") || low == ".envrc"` so only dotenv secrets and direnv configs match, rather than any `.env`-prefixed name.

## TD-003 — pathguard layer-2 symlink resolution anchors to CWD, not the working-tree root (LOW)
**Origin:** Phase 2, task 2.3 gate integration review finding, 2026-07-21
**File:** internal/security/pathguard.go:127
**Issue:** `IsProtectedPath`'s layer-2 symlink check (`resolveSymlinks` → `filepath.EvalSymlinks`) resolves the raw repo-relative `e.Path` against the process CWD, not against `applyOne`'s `root`/`applyTarget`. When CWD differs from the apply root, a pre-planted, benign-named symlinked directory component pointing into a protected dir (e.g. `root/link -> .git`, patch entry `link/config`) is not caught by the symlink layer; `containedPath` allows it (`.git` is inside root) and `refuseSymlinkLeaf` only guards the leaf, not an interior component.
**Why accepted:** Marginal, non-LLM-reachable risk — the lexical layer (layer 1) already blocks every direct and `..`-normalized protected name, an `--auto-fix` patch cannot itself plant the symlink (that requires prior host write access), and in the production `--auto-fix` path CWD equals the repo root. This is pre-existing Phase-1 pathguard behavior surfaced (not introduced) by Phase 2's `e.Path` wiring; the gate is defense-in-depth over the primary upstream `payload` traversal guard.
**Fix in:** Future sprint (optional) — anchor the layer-2 resolution to the working-tree root (resolve the already-computed `abs` against the resolved root in the same frame as the write, mirroring `containedPath`'s realRoot/realParent handling) so a symlinked interior component is caught regardless of CWD.

## TD-004 — docs/security.md pre-documents FlagsForReview in present tense before T6 lands (LOW)
**Origin:** Phase 3, task 3.3 gate integration review finding, 2026-07-21
**File:** docs/security.md:124-141,149
**Issue:** `docs/security.md` (written in Phase 3/T4, which task-04 required to cover `FlagsForReview`) describes `FlagsForReview(path, oldMode, newMode)` and the `## Review Warnings` PR-body section in definitive present tense, but neither exists yet as of Phase 3 — `internal/security` ships only `IsProtectedPath`, and no PR-body wiring references review warnings. A reader comparing the shipped doc against the Phase-3 source finds forward-referencing claims.
**Why accepted:** Transient intra-sprint state, not shippable debt. T6/Phase 4 (next phase, same sprint, same PR) implements `FlagsForReview` and the `## Review Warnings` PR-body wiring, making the doc accurate before this branch merges. task-04 explicitly required documenting `FlagsForReview` in this doc, so softening the prose in Phase 3 would only be reverted in Phase 4 (needless churn). The gate itself confirmed this does NOT block Phase 4.
**Fix in:** Phase 4 / task-06 — when `FlagsForReview` + the PR-body warning section land, re-read docs/security.md's "Non-blocking review warnings" section and confirm every claim (function signature, `## Review Warnings` section name, executable-bit condition) matches the implementation; adjust wording if the shipped API diverges. Append a `**Resolved:**` line here once verified.
**Resolved:** 2026-07-22 — T6 landed `FlagsForReview(path string, oldMode, newMode int) (bool, string)` and the `## Review Warnings` PR-body section; re-read docs/security.md:124-141,149 and every claim (signature, `oldMode&0111 != newMode&0111` condition, section name, build-script list, byte-identical-when-empty, successfully-applied semantics) matches the shipped implementation — no doc wording change needed.

## TD-005 — AC4 exec allowlist is file-granular, not call-granular (LOW)
**Origin:** Phase 5, task 5.3 gate integration review finding, 2026-07-22
**File:** internal/gitexec/gitexec_test.go:121
**Issue:** The AC4 scan's `indirectNonGitExecFiles` allowlist ({`internal/verify/localvalidate.go`, `internal/sandbox/docker.go`}) is keyed by whole file. Both files provably exec a non-git binary today (docker via `b.cfg.DockerPath`, validate via `argv[0]`), but an indirected git call (`exec.Command(gitVar, ...)`) added specifically to one of those two files would be excused by the scan. A literal `"git"` in either file IS still caught.
**Why accepted:** Deliberate, documented, narrow trust grant on exactly two files that run known non-git binaries, reviewed the same way the allowlist itself is edited (mirrors `internal/boundaries_test.go`'s authorized-caller-set pattern). Fully call-granular matching would require statically matching argv expressions (brittle). No live exposure — neither file constructs git.
**Fix in:** Future sprint (optional) — narrow the exclusion to the specific known non-git call expression (match `b.cfg.DockerPath` / `argv[0]` receiver) rather than the whole file, or add a lint asserting neither file gains a git-named identifier exec.

## TD-006 — AC4 matcher self-test does not cover the allowlist branch (LOW)
**Origin:** Phase 5, task 5.3 gate integration review finding, 2026-07-22
**File:** internal/gitexec/gitexec_test.go:332
**Issue:** `TestAC4_MatcherDetectsIndirectedGit` reuses the load-bearing primitives (`execPkgLocalName`/`classifyExecCall`/`stringLiteralValue`) but re-implements the offender-branching inline with NO file allowlisted, so the tree-walk's `indirectNonGitExecFiles` allowlist branch is exercised only end-to-end (by the real tree passing), not by a dedicated unit assertion. A future inversion of the allowlist condition would still pass the self-test.
**Why accepted:** The allowlist branch is covered end-to-end (docker.go/localvalidate.go pass; snapshot.go would fail if reverted); the gap is only a targeted unit test, and the shared primitives ARE unit-tested. Low value vs. phase-boundary discipline.
**Fix in:** Future sprint (optional) — extract the per-call offender decision (including the allowlist check) into one shared function that both the walk and the self-test call, then add a self-test row with a file marked allowlisted to assert the branch directly.

## TD-007 — AC4 matcher does not flag `exec.Cmd` struct literals or `.Path`/`.Env` mutation (LOW)
**Origin:** Phase 5, task 5.3 gate integration review finding, 2026-07-22
**File:** internal/gitexec/gitexec_test.go:172
**Issue:** The AC4 scan matches only `exec.Command`/`exec.CommandContext` selector calls. A git subprocess constructed via a `&exec.Cmd{Path: ..., Args: ...}` composite literal, or a post-construction `cmd.Path = exec.LookPath("git")` reassignment on a non-gitexec command, would run git (or strip env hardening) without tripping the gate.
**Why accepted:** Pre-existing scope limit, NOT introduced by the Phase 5 rewrite. No offending occurrence on the current tree (grep finds no `exec.Cmd` struct literals in production; snapshot.go's `cmd.Path` pin is applied to a hardened `gitexec.CommandFn` command, keeping the env hardening). Covering this class is materially more matcher complexity than the current threat warrants.
**Fix in:** Future sprint (optional) — if this vector becomes relevant, extend the matcher to also flag `&exec.Cmd{...}` composite literals and `.Path`/`.Env` mutations on commands not constructed through `gitexec`.
