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
**Resolved:** 2026-07-03 — Story-4 tests (`TestCreateCommitSingleFile`, `TestCreateBranchRefAlreadyExists`, et al.) now encode the blob/tree/commit/ref bodies and the 422 collision path as executable assertions.

## TD-003 — Findings note overstates plumbing; 422 contract is POST-path-dependent (LOW)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**File:** .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md:88
**Issue:** The note header says "via existing `postDo`/`get`" but the 1.2 spike exercised only `postDo`. `get` returns a plain `fmt.Errorf`, not `*APIError`, so the AC 04-02 422-collision contract (`errors.As(err, *APIError)`) holds only because all 4 Git Data calls are POST — a silent trap if any is later switched to GET.
**Why accepted:** Cosmetic/documentation nuance; all 4 Git Data calls are POST by design and Story 4 has no GET on the mutation path.
**Fix in:** Phase 4 — when Story 4 lands, confirm no mutation-path call uses `get`, or extend `get` to also return `*APIError` if one is ever needed there.
**Resolved:** 2026-07-03 — Story 4's `CreateCommit` reads the parent commit via `get` (a mutation-path GET), so `get` was extended to return `*APIError` on non-2xx (mirroring `sendDo`); a missing base SHA now surfaces as an inspectable typed error (`TestCreateCommitParentReadErrorIsTyped`).

## TD-004 — Create entry silently overwrites an existing target (MEDIUM)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-03
**File:** internal/autofix/apply.go:108
**Issue:** A create entry (`f.IsNew`) whose target already exists is applied against empty content and atomically overwritten, rather than rejected the way `git apply` refuses a create over an existing file. The clobber is silent.
**Why accepted:** Not data loss — the pre-existing content is captured by `atomicfs.BackupToDotBak` before the write and recorded in `BackupMap` with a non-empty backup path, so Story 3's Revert restores it. Only the git-apply-style "refuse create over existing" nicety is missing; the applied result is correct and revertible.
**Fix in:** A later hardening pass — stat the target in the `IsNew` branch and fail the entry with an "already exists" per-file error, or route it through the modify path against the real on-disk content.

## TD-005 — BackupMap empty-value sentinel is overloaded (Story 1→3 handoff) (MEDIUM)
**Origin:** Phase 2, tasks 2.2.A adversarial + 2.8 gate reviews, 2026-07-03
**File:** internal/autofix/apply.go:129
**Issue:** `BackupMap`'s empty backup-path value is documented to mean "file created by this run → Revert removes it," but `applyOne` also emits an empty value for (a) a modify/delete entry whose in-tree target is a **symlink** — `atomicfs.BackupToDotBak` Lstat-skips symlinks and returns `("", nil)`, then `WriteFileAtomic` replaces the link with a regular file (or a delete unlinks it) — and (b) an **already-gone delete** (the idempotent branch). Story 3's Revert, told to delete empty-value entries, would delete a pre-existing symlinked target instead of restoring it, defeating the revert safety net for that case. The write-boundary re-check guards a symlinked *directory component* (closed in 2.3) but not the *target leaf* itself.
**Why accepted:** Symlinked patch targets are outside the technical-debt-fix use case; the security-relevant escape (symlinked directory component) is closed. This is a Story-1→Story-3 contract-clarity issue, best resolved where Revert is implemented and the created-vs-restore decision actually lives.
**Fix in:** Phase 3 (Story 3, Revert) precondition — disambiguate the states rather than overloading `""`: either add an explicit per-entry kind (created/modified/deleted) to the handoff, or reject an in-tree symlink target at the write boundary so an empty backup path unambiguously means "created." Add a symlinked-target round-trip-through-Revert regression test.

## TD-008 — No default validation-timeout constant / config key (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-03
**File:** internal/verify/localvalidate.go:130
**Issue:** `ResolveValidateCommand` establishes the command default (`go build ./...`) and hard refusal, but there is no matching constant or resolver for the ~2 min default validation timeout the sprint-design Performance table specifies. The timeout is a bare param on `RunConfiguredValidation`; a Phase-5 caller that passes `0` gets `context.WithTimeout(ctx, 0)` → immediate `DeadlineExceeded` → every validation reports `TimedOut` (fail-closed, but silently mislabeled).
**Why accepted:** Fails closed. Phase 5 owns the `--auto-fix` config surface and gate wiring, where the default naturally lives; Phase 2's runner correctly treats the timeout as a caller-supplied parameter.
**Fix in:** Phase 5 (Story 6 wiring) — add a `defaultValidationTimeout` (~2 min) constant + resolver (e.g. `ResolveValidateTimeout`) and document the `validate_command`/timeout config keys so the gate inherits a defined default rather than relying on each call site.

## TD-006 — Validation timeout leaves orphaned grandchild processes (MEDIUM)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-03
**File:** internal/verify/localvalidate.go:81
**Issue:** `exec.CommandContext` SIGKILLs only the direct child on timeout. A configured validation command that spawns subprocesses (e.g. `sh -c "make ..."`) leaves grandchildren orphaned and still running after the deadline; `cmd.WaitDelay` only force-closes the parent's pipe fds so `Run` returns, it does not reap the process group. A timed-out build/test validation can leave subprocesses holding CPU or locks.
**Mitigation this sprint:** `cmd.WaitDelay = 2s` guarantees `RunConfiguredValidation` itself returns promptly and never stalls `--auto-fix` indefinitely — the core Story-2 requirement is met; only orphan reaping is missing.
**Fix in:** A later hardening pass — set `cmd.SysProcAttr` `Setpgid: true` and kill the whole group (`syscall.Kill(-pid, SIGKILL)`) on the cancel/timeout path (unix build-tagged) so grandchildren are reaped too.

## TD-007 — StartError class polluted by ErrWaitDelay / context.Canceled (LOW)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-03
**File:** internal/verify/localvalidate.go:106
**Issue:** A command that exits 0 but whose child lingers past `WaitDelay` makes `cmd.Run` return `exec.ErrWaitDelay` (not `*exec.ExitError`); a parent-context `context.Canceled` (non-deadline) is likewise not `DeadlineExceeded`. Both fall through the `errors.As(*exec.ExitError)` guard into the catch-all StartError branch and are reported as "command not found or not executable", polluting the StartError class (meant to distinguish "cannot validate" from "validation failed").
**Why accepted:** Fails closed (`Passed()==false`, no unsafe behavior). Not reachable via the `--auto-fix` bounded-timeout path — a deadline hit is caught as `TimedOut` before this branch; it requires a zero-exit command that backgrounds a pipe-holding child, or an external parent-ctx cancel. LOW.
**Fix in:** A later pass — treat `exec.ErrWaitDelay` as a completed-but-failed run and handle `context.Canceled` alongside `DeadlineExceeded`; reserve StartError for genuine start failures (`errors.Is` `exec.ErrNotFound` / `os.ErrPermission`).

## TD-009 — Revert restores file content but not file mode (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-07-03
**File:** internal/autofix/revert.go:41
**Issue:** `RevertPatch` restores pre-patch bytes via `atomicfs.CopyPath`, but `copyFile` opens the existing target with `O_TRUNC` and ignores its `perm` argument for an already-existing file, so a file whose original mode was 0755 (executable) or 0600 comes back as the 0644 the apply step wrote. The `.bak` does carry the original mode (`BackupToDotBak` copies with `info.Mode().Perm()`), so the information to restore it is available; it is simply not re-applied.
**Why accepted:** Out of AC scope — ACs 03-02/03-04 specify byte-for-byte *content* restoration, which holds. The auto-fix target corpus is 0644 Go source, so a mode regression is not reachable for the intended use case. LOW.
**Fix in:** A later hardening pass — after a successful `copyPathFn` restore, `os.Chmod(target, bakMode)` from the backup's mode (or stat the `.bak`); add an executable-fixture mode-fidelity regression test.

## TD-013 — CommitRequest.Message is sent verbatim (no outbound redaction) (LOW)
**Origin:** Phase 4, task 4.8 gate review, 2026-07-03
**File:** internal/ghaction/client.go:245
**Issue:** `CreatePullRequest`/`UpdatePullRequest` run `PullRequestRequest.Title`/`Body` through `redactSecrets` before sending (AC 05-04), but `CreateCommit` sends `CommitRequest.Message` verbatim. If the Phase-5 orchestrator builds a commit message from validation/model output the same way it builds the PR body, a credential could leak into the commit message on GitHub.
**Why accepted:** No AC requires commit-message redaction (05-04 governs PR title/body only), and today the auto-fix commit message is atcr-generated boilerplate, not diagnostics-sourced — adding redaction now would be speculative. Flagged rather than fixed to avoid pre-empting Phase 5's not-yet-defined message construction.
**Fix in:** Phase 5 (Story 6 wiring) precondition — if the orchestrator sources `CommitRequest.Message` (or branch name) from validation/model diagnostics, run it through `Client.redactSecrets` at the call site (or redact `req.Message` inside `CreateCommit`, symmetric with the PR title/body treatment).

## TD-012 — Existence check scopes on head only, not the (head, base) pair (LOW)
**Origin:** Phase 4, task 4.5.A adversarial review, 2026-07-03
**File:** internal/ghaction/client.go:315
**Issue:** `findOpenPullRequest` queries `?head={owner}:{branch}&state=open` and, on multiple matches, returns the lowest-numbered PR. GitHub permits multiple open PRs from the same head to different base branches, so the lowest-number tiebreak could return a PR targeting an unintended base; the Story-6 orchestrator would then update that PR instead of the one against the intended base.
**Why accepted:** AC 05-02 explicitly specifies the query as head + state=open (no base), and its Edge Case 1 deliberately resolves multiple-same-head matches by lowest number — head-only + lowest-number is the AC's chosen design, so scoping on base here would deviate from the contract. In the real `--auto-fix` flow the branch is atcr-generated and always targets the single default base, so multi-base same-head PRs are not a reachable condition for the intended use case.
**Fix in:** A later hardening pass, only if multi-base auto-fix PRs ever become a real scenario — thread a `base` argument into `findOpenPullRequest` and add `q.Set("base", base)` so the existence check matches GitHub's one-open-PR-per-(head,base) invariant, and update AC 05-02's query accordingly.

## TD-011 — Non-idempotent POST retry can yield a spurious 422 collision (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-07-03
**File:** internal/ghaction/client.go:180
**Issue:** `sendDo` retries on transport error / 5xx for all verbs. If `CreateBranch`'s `POST /git/refs` (or a `CreateCommit` sub-POST) succeeds server-side but its response is lost, the retry receives GitHub's 422 "Reference already exists" — indistinguishable from a genuine name collision. The Story-6 caller's collision policy (suffix-and-retry) could then create a redundant branch, or a retried `POST /git/commits` could leave an orphan duplicate commit object.
**Why accepted:** Inherent to the reused `postDo`/`sendDo` retry plumbing shared by every existing mutating endpoint (check-runs, comments) — not introduced by Story 4. The window (lost response on a first-try success) is narrow, orphan blob/commit objects are inert and GitHub-GC'd, and AC 04-02 already delegates collision policy to the caller. Redesigning retry idempotency (e.g. dedup by pre-checking ref existence) is out of Phase 4 scope.
**Fix in:** A later hardening pass — document the spurious-422 possibility on `CreateBranch`'s contract, or gate retry-on-5xx to idempotent verbs and have the Story-6 orchestrator pre-check ref existence before a suffix-retry so a lost-response 422 is not misread as a real collision.

## TD-010 — RevertPatch does not re-check path containment at the revert boundary (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-07-03
**File:** internal/autofix/revert.go:41
**Issue:** `RevertPatch` is exported and trusts every path in the `BackupMap` it is handed, calling `copyPathFn`/`removeFn` with no independent containment re-check. Its safety depends entirely on the upstream invariant that the map was produced by `ApplyPatch` (whose `containedPath` validates every target stays inside root). A hand-built or corrupted map with a target/backup outside the working-tree root would copy or delete outside it.
**Why accepted:** In the real flow the map is always apply-produced and already `containedPath`-validated; this is defense-in-depth, not a reachable bug. The write-side already carries the belt-and-suspenders re-check (`apply.go` `containedPath`). LOW.
**Fix in:** A later pass — either re-assert `contains(root, target)` at the revert boundary (mirroring apply's defense-in-depth, which requires threading `root` into `RevertPatch`), or document `RevertPatch`'s "apply-produced map only" precondition explicitly on the exported signature.

## TD-014 — `--auto-fix` gate does not shape-check `--api-url` (LOW)
**Origin:** Phase 5, task 5.2.A adversarial review, 2026-07-03
**File:** cmd/atcr/autofix.go
**Issue:** `validateAutoFixBackend` resolves `--api-url`/`GITHUB_API_URL` into the backend but never shape-checks it; validation happens lazily in `ghaction.Client.baseURL()` at the first HTTP call. A malformed or insecure (`http://`) api-url therefore passes the all-or-nothing gate, and the run proceeds to apply the patch, run validation, and clean up backups, failing only at `CreateBranch`. No GitHub mutation occurs (the URL parse fails before any request), but it violates the gate's stated "refuse before any file is touched" contract and leaves the tree patched-but-validated.
**Why accepted:** Fails closed — no remote mutation is possible on a bad url, and the applied content already passed local validation, so it is a correct (if surprising) tree state, not data loss. The default/most-CI path leaves api-url empty (→ api.github.com), so the malformed-url case is not reachable in the common flow. Adding a url pre-parse to the gate is a small hardening, not a correctness fix.
**Fix in:** A later pass — reuse ghaction's `baseURL`-style parse (or export it) inside `validateAutoFixBackend` and add a malformed/insecure api-url to the `missing` aggregation, so a bad value is a fail-closed exit-2 refusal before any apply.

## TD-015 — `--auto-fix` live adapter always mints a unique branch (create-only) (LOW)
**Origin:** Phase 5, task 5.2.A adversarial review, 2026-07-03
**File:** cmd/atcr/autofix.go orchestrateAutoFix
**Issue:** `orchestrateAutoFix` names the branch `atcr/auto-fix/<UTC-timestamp>`, unique per run, so `FindOpenPullRequest`/`UpdatePullRequest` in `runAutoFix` never match in production — the create-vs-update path (AC 05-02) is implemented and unit-tested but unreachable via the live adapter, and each re-run opens a fresh PR + branch rather than updating a stable one.
**Why accepted:** Create-per-run is acceptable MVP behavior (each review run yields its own fix PR) and sidesteps the 422-on-existing-branch handling a stable branch name would require. The update path is genuinely exercised by `runAutoFix`'s unit tests and Phase 6's injected-entry integration, so the code is not dead — only the live adapter always creates.
**Fix in:** A later pass — if one-stable-PR-per-target is desired, derive a deterministic branch name (e.g. keyed to the review target/base) and handle `CreateBranch`'s 422 "reference already exists" by advancing the existing ref, so re-runs converge on a single updating PR; otherwise document orchestrate as intentionally create-only.

## TD-016 — Auto-fix scope silently coupled to the --fail-on CI threshold (MEDIUM)
**Origin:** Phase 5, task 5.5 gate review, 2026-07-03
**File:** cmd/atcr/autofix.go selectAutoFixEntries
**Issue:** `orchestrateAutoFix` passes the resolved `--fail-on` threshold to `selectAutoFixEntries`, which drops every finding below it. That threshold comes from `resolveGateThreshold` (flag > project `fail_on` > registry), and the `atcr init` template defaults `fail_on: HIGH` — so on a stock-init project `atcr review --auto-fix` silently fixes only HIGH+ findings and prints "no reconciled finding carried an applicable fix; nothing to apply" for a MEDIUM-only run. No Story-6 AC specifies this coupling and the `--auto-fix` help never mentions it.
**Why accepted:** Matches the user-approved Phase-5 selection policy (Clarification 2: "--fail-on set → findings at/above threshold; absent → all"), and is fail-safe (fewer fixes, never more). It is a discoverability/UX gap, not a correctness bug. Deferred to keep Phase 5 to the gate scope.
**Fix in:** A later pass — either decouple auto-fix scope from the CI gate (a dedicated `auto_fix.min_severity`, defaulting to "all-with-a-fix"), or document the coupling in the `--auto-fix` help and change the empty-selection message to distinguish "all fixes below the --fail-on threshold" from "no findings carried a fix".

## TD-017 — auto_fix config keys absent from the `atcr init` template (MEDIUM)
**Origin:** Phase 5, task 5.5 gate review, 2026-07-03
**File:** internal/registry (DefaultProjectConfigYAML)
**Issue:** The new `auto_fix:` keys (`apply_target` / `validate_command` / `validate_timeout`) are documented only via struct doc-comments and `--auto-fix` flag help; the `atcr init` project-config template emits no commented `auto_fix:` stanza, and there is no README/docs mention. An operator enabling `--auto-fix` has no in-repo template to copy.
**Why accepted:** The keys are all optional with working defaults (apply target = repo root; Go build default; ~2 min timeout), so `--auto-fix` is usable without the block; this is a discoverability gap, not a functional one. Deferred per the gate's MEDIUM→TD protocol.
**Fix in:** A later pass — add a commented `auto_fix:` stanza to `DefaultProjectConfigYAML` (mirroring the `# max_parallel:` / `# cache_max_bytes:` comment style) and document the keys alongside the flag.

## TD-018 — `--auto-fix` bypasses the `--fail-on` CI exit gate (LOW)
**Origin:** Phase 5, task 5.5 gate review, 2026-07-03
**File:** cmd/atcr/review.go (the terminal `if autoFix { return orchestrateAutoFix(...) }`)
**Issue:** The auto-fix terminal return sits before the `--fail-on` gate block, so `atcr review --auto-fix --fail-on HIGH` returns exit 0 even when unfixable HIGH findings (e.g. findings carrying no Fix) survive. Deliberate — under `--auto-fix` the intent is to remediate, not to fail CI — but not stated in the flag help and surprising in a CI-gate context.
**Why accepted:** Intentional design (documented inline in review.go). LOW.
**Fix in:** A later pass — document in the `--auto-fix` help that it supersedes the `--fail-on` exit gate, or preserve the gate for findings that had no applicable fix.

## TD-019 — Gate stores a relative applyTarget when repoRoot=="." (LOW)
**Origin:** Phase 5, task 5.5 gate review, 2026-07-03
**File:** cmd/atcr/autofix.go validateAutoFixBackend
**Issue:** `autoFixBackend.applyTarget` is documented as an absolute path, but `runReview` calls the gate with `repoRoot="."`, so a default/relative `apply_target` resolves to a relative `"."` stored in the field. It works only because CWD == repo root at call time; a latent inconsistency if the gate is ever reused with `repoRoot != CWD`.
**Why accepted:** Correct in the only current call path (CWD is always the repo root for `atcr review`). LOW.
**Fix in:** A later pass — `filepath.Abs` the resolved `repoRoot`/apply target in the gate so the field honors its documented absolute contract regardless of caller CWD.
