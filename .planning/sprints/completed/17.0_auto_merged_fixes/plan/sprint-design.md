# Sprint Design: Auto-Merged Fixes Execution

**Created:** July 02, 2026 10:44:35PM
**Plan:** [Auto-Merged Fixes Execution](.planning/plans/active/17.0_auto_merged_fixes/)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> ATCR currently leaves the burden of applying fixes on the developer. This creates friction and slows down the remediation of identified issues.
>
> Enable ATCR to actively apply the fixes it generates. This includes parsing model-generated diffs, applying patches to the local tree, running local validation (e.g. `go build`), and orchestrating Git commits/PRs via the GitHub API.
>
> **Acceptance Criteria:** AC1 robust diff parsing (handles missing context lines), AC2 safe patch application (no corruption), AC3 configurable validation step, AC4 automatic revert on validation failure, AC5 GitHub API orchestration (branch/commit/PR), AC6 opt-in via a flag (e.g. `--auto-fix`).
>
> **Success Criteria:** Auto-fix at least 70% of the simple technical debt items flagged; zero broken builds introduced by auto-merged fixes in the test corpus.
>
> **Out of Scope:** Resolving complex merge conflicts on a diverged target branch; auto-fixing architectural or cross-repository issues.

**Referenced Resources:** None — the original epic content (`.planning/epics/active/17.0_auto_merged_fixes.md`) is captured verbatim in `original-requirements.md` as the source of truth; no separate external reference files were cited for this plan.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Auto-Merged Fixes
**Complexity:** 11/12 (VERY COMPLEX)
**Timeline:** 14 days
**Phases:** 7
**Pattern:** Research & Spike → Foundation → Core Items → Advanced → Integration → Testing → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go atomic file patch apply
GitHub API branch commit PR orchestration
opt-in CLI flag refuse-without-backend gate
crash-safe backup restore on failure
configurable local validation before commit
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New `internal/autofix` orchestrator package plus four new `ghaction.Client` methods; a genuinely new pattern in the codebase, but built entirely from existing primitives (`atomicfs`, `payload`, `ghaction`'s `postDo`/`get`) rather than a wholesale rewrite.
- **Integration:** 3/3 - Touches 5 internal packages (`payload`, `atomicfs`, `verify`, `ghaction`, new `autofix`) plus one new external dependency (`go-gitdiff`), culminating in live GitHub API mutation (branch/commit/PR creation) — `HAS_CROSS_SYSTEM=true` per the epic's own `/refine-epic --deep` findings.
- **Story/Task & Test:** 3/3 - 6 user stories, 23 acceptance criteria, mixed unit + integration coverage (6 ACs require integration-level orchestration tests: 03-02, 04-04, 05-02, 06-01, 06-02, 06-03).
- **Risk/Unknowns:** 3/3 - The cross-system rollback gap is explicitly unmitigated in the plan (a pushed branch/PR cannot be undone by the local revert); `go-gitdiff` is a brand-new third-party dependency never before used in this codebase; the Git Data API's 4-call sequence (blob → tree → commit → ref) carries partial-failure/orphaned-object risk called out directly in Story 4's own ACs (04-03, 04-04).

**Time Formula:** VERY COMPLEX tier baseline (13+ days) cross-checked against per-story RGR estimates.
**Calculation:** 6 stories × ~2 days each (apply, validate, revert, branch/commit, PR, opt-in gate) = 12 days core implementation + 1 day cross-story integration phase + 1 day validation/hardening (lint, vet, coverage, DoD) = **14 days**.

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** strong
**Suggested command:** `/create-sprint @.planning/plans/active/17.0_auto_merged_fixes/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12. This sprint hits all three thresholds (11/12, 7 phases, 14 days) — strong gated recommendation.

---

## Phase Structure

**Phase 1: Research & Spike** (1 day)
- Items: spike `go-gitdiff` against representative fixture diffs (modify/create/delete, drifted-context); spike the GitHub Git Data API 4-call sequence (blob → tree → commit → ref) against a `httptest.Server` stub.
- Focus: de-risk the two most novel technical unknowns (a never-before-used third-party diff library, and a multi-call GitHub API sequence with partial-failure risk) before committing to `internal/autofix` and `ghaction.Client` interfaces.

**Phase 2: Foundation** (4 days)
- Items: Story 1 (Apply a Parsed Patch to the Working Tree Without Corruption, AC2) + Story 2 (Configurable Local Validation, AC3).
- Focus: build the local write-path (`internal/autofix/apply.go`) and the post-apply validation gate (`internal/verify` extension) — the foundation every later story depends on.

**Phase 3: Core Items** (2 days)
- Items: Story 3 (Automatic Revert on Validation Failure, AC4).
- Focus: complete the local safety net (`internal/autofix/revert.go`) that makes the apply→validate pipeline trustworthy enough to gate remote mutation on.

**Phase 4: Advanced** (4 days)
- Items: Story 4 (Create a Branch and Commit the Verified Fix, AC5 new half) + Story 5 (Open or Update Pull Request via GitHub API, AC5 new half).
- Focus: the cross-system GitHub orchestration — the highest-risk surface per the epic's own refinement notes (`HAS_CROSS_SYSTEM=true`).

**Phase 5: Integration** (2 days)
- Items: Story 6 (Opt In via `--auto-fix` with a Refuse-Without-Backend Gate, AC6).
- Focus: wire Stories 1-5 into a single gated entry point in `cmd/atcr`; default behavior must remain byte-identical when the flag is absent.

**Phase 6: Testing** (1 day)
- Items: cross-story integration tests exercising the full flow (apply → validate → revert-or-continue → branch/commit → PR) against `httptest.Server` stubs; zero-HTTP-calls-on-validation-failure regression test.
- Focus: prove the sequencing guarantees (no GitHub mutation before local validation passes) hold end-to-end, not just per-story.

**Phase 7: Validation** (extra day folded into hardening budget)
- Items: Definition of Done verification across all 23 ACs; `go vet`, `golangci-lint run`, `go test -coverprofile=coverage.out ./...` against the 80% coverage baseline.
- Focus: final quality gate before `/execute-code-review`.

---

## Work Decomposition

### Story 1: Apply a Parsed Patch to the Working Tree Without Corruption
**File:** [user-stories/01-apply-patch-to-working-tree-without-corruption.md](user-stories/01-apply-patch-to-working-tree-without-corruption.md) | **Effort:** M | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [01-01](acceptance-criteria/01-01-parse-and-apply-hunks.md) | Parse and apply hunks for modify/create/delete entries via `gitdiff.Parse`/`gitdiff.Apply` | Unit |
| [01-02](acceptance-criteria/01-02-atomic-write-to-target-path.md) | Every write goes through `atomicfs.WriteFileAtomic` (sibling-temp-then-rename) | Unit |
| [01-03](acceptance-criteria/01-03-per-file-backup-before-overwrite.md) | Per-file backup via `atomicfs.BackupToDotBak` before any overwrite | Unit |
| [01-04](acceptance-criteria/01-04-per-file-error-isolation.md) | A failed hunk reports a clear per-file error without corrupting prior successes | Unit |

### Story 2: Configurable Local Validation
**File:** [user-stories/02-configurable-local-validation.md](user-stories/02-configurable-local-validation.md) | **Effort:** M | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [02-01](acceptance-criteria/02-01-configurable-validation-command-runner.md) | Configurable validation command runner via `os/exec.CommandContext` with bounded timeout | Unit |
| [02-02](acceptance-criteria/02-02-result-capture-and-reporting.md) | Validation result capture (exit code, stdout, stderr, duration) and reporting | Unit |
| [02-03](acceptance-criteria/02-03-conservative-pass-fail-gate.md) | Conservative pass/fail gate — exit-code-only, no partial-success interpretation, no mutation | Unit |

### Story 3: Automatic Revert on Validation Failure
**File:** [user-stories/03-automatic-revert-on-validation-failure.md](user-stories/03-automatic-revert-on-validation-failure.md) | **Effort:** M | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [03-01](acceptance-criteria/03-01-backup-map-tracking.md) | Per-file backup map precondition and tracking (input contract from Story 1) | Unit |
| [03-02](acceptance-criteria/03-02-restore-on-validation-failure.md) | Restore all touched files on validation failure, strictly before any `ghaction` call | Unit + Integration |
| [03-03](acceptance-criteria/03-03-cleanup-on-validation-success.md) | Cleanup backup files on validation success | Unit |
| [03-04](acceptance-criteria/03-04-hard-error-on-restore-failure.md) | Hard error surfacing on restore failure (never silently continue) | Unit |

### Story 4: Create a Branch and Commit the Verified Fix
**File:** [user-stories/04-create-branch-and-commit-verified-fix.md](user-stories/04-create-branch-and-commit-verified-fix.md) | **Effort:** L | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [04-01](acceptance-criteria/04-01-create-branch-ref.md) | `CreateBranch` creates a new Git ref at a base SHA | Unit |
| [04-02](acceptance-criteria/04-02-branch-collision-handling.md) | Branch name collision (422 ref-exists) is distinguishable and recoverable | Unit |
| [04-03](acceptance-criteria/04-03-create-commit-multi-file-sequence.md) | `CreateCommit` builds a multi-file commit via blob → tree → commit → ref-update sequence | Unit |
| [04-04](acceptance-criteria/04-04-validation-gated-call-site.md) | `CreateBranch`/`CreateCommit` are unreachable without a prior validation success | Integration |
| [04-05](acceptance-criteria/04-05-retry-backoff-redaction-reuse.md) | New endpoints inherit existing retry/backoff/redaction behavior from `postDo`/`get` | Unit |

### Story 5: Open or Update Pull Request via GitHub API
**File:** [user-stories/05-open-or-update-pull-request-via-github-api.md](user-stories/05-open-or-update-pull-request-via-github-api.md) | **Effort:** M | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [05-01](acceptance-criteria/05-01-create-pull-request.md) | `CreatePullRequest` opens a new PR from the auto-fix branch | Unit |
| [05-02](acceptance-criteria/05-02-existence-check-avoids-duplicate-prs.md) | Existence check decides create-vs-update and avoids duplicate PRs | Integration |
| [05-03](acceptance-criteria/05-03-update-pull-request.md) | `UpdatePullRequest` refreshes an existing open PR | Unit |
| [05-04](acceptance-criteria/05-04-retry-backoff-redaction-reuse.md) | PR endpoints reuse retry/backoff plumbing and redact secrets from outbound PR content | Unit |

### Story 6: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate
**File:** [user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md](user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md) | **Effort:** S | **Priority:** High

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| [06-01](acceptance-criteria/06-01-auto-fix-off-by-default.md) | `--auto-fix` off by default, zero behavior change when absent | Unit + Integration |
| [06-02](acceptance-criteria/06-02-single-gate-refuses-on-any-missing-piece.md) | Single gate refuses the entire run on any missing/malformed backend piece | Unit + Integration |
| [06-03](acceptance-criteria/06-03-gate-passes-silently-when-fully-configured.md) | Gate passes silently when fully configured, no overhead into Story 1 | Unit + Integration |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Alongside source, following Go convention and this repo's `coding-standards.md` (test files named `*_test.go`, resident in the same package directory as the code under test).

**Test File Placement Examples:**
- `internal/autofix/apply_test.go` (Story 1)
- `internal/verify/localvalidate_test.go` or a sibling to `syntaxguard_test.go` (Story 2)
- `internal/autofix/revert_test.go` (Story 3)
- `internal/ghaction/client_test.go` (Stories 4-5, extending existing test file)
- `cmd/atcr/autofix_test.go` or an addition to `cmd/atcr/review_test.go` (Story 6)

**Unit/Integration/E2E:** 21 of 23 ACs require unit coverage (18 unit-only, plus unit halves of the 5 unit+integration ACs); 6 ACs require integration-level coverage (03-02, 04-04, 05-02, 06-01, 06-02, 06-03) exercising cross-function sequencing via `httptest.Server` GitHub stubs and in-process `cobra.Command` execution (mirroring `cmd/atcr/verify_test.go`'s `TestVerifyCmd_ExecRefusesWithoutSandbox` pattern). Zero E2E tests — the plan's cross-system surface is verified against a stubbed GitHub API, never a live GitHub call, matching `internal/ghaction`'s existing test style.

**Test Environment Status:**
- Framework: `go test` standard library + `github.com/stretchr/testify` `assert`/`require` (per `coding-standards.md` and the existing `internal/verify/syntaxguard_test.go` pattern)
- Execution: `go test ./...` (project config `testing.cmd`)
- Coverage Tools: `go test -coverprofile=coverage.out ./...` against an 80% coverage baseline (project config `testing.coverage_baseline`)

---

## Architecture

**Primitives:**
- `payload.FileEntry{Path, Size, Body}` — existing, unmodified input shape from `BuildEntriesFromDiff` (Story 1's input).
- New `autofix.BackupMap` (`map[string]string` of `originalPath -> backupPath`) — the Story 1 → Story 3 handoff contract.
- New `verify.ValidationResult{Passed bool, ExitCode int, Stdout, Stderr string, Duration time.Duration}` — Story 2's output, Story 3's trigger signal.
- New `ghaction.CommitRequest{Branch, Message, ParentSHA string, Files []CommitFile}` (with `CommitFile{Path, Content string, Deleted bool}`) and `ghaction.PullRequestRequest{Head, Base, Title, Body string}` — Story 4/5 request shapes. (Corrected 2026-07-03: implementation and AC 04-03 use `CommitFile`, not the earlier `FileChange` placeholder.)

**Module Boundaries:**
- `internal/autofix`: exposes `ApplyPatch(entries []payload.FileEntry) (BackupMap, error)` and `Revert(backupMap BackupMap) error` — hides `go-gitdiff` and `atomicfs` call sequencing entirely.
- `internal/verify`: exposes a new `RunConfiguredValidation(cmd string, timeout time.Duration) (ValidationResult, error)` sibling to the existing `validateGoFixSyntax` — hides `os/exec` details.
- `internal/ghaction.Client`: exposes `CreateBranch`/`CreateCommit`/`CreatePullRequest`/`UpdatePullRequest` — hides the Git Data API's blob/tree/commit/ref HTTP mechanics behind the existing `postDo`/`get` plumbing.
- `cmd/atcr`: exposes the `--auto-fix` flag and a `validateAutoFixBackend` gate function — hides env-var/flag/config resolution.

**External Dependencies:**
- `github.com/bluekeyes/go-gitdiff` — the one genuinely new external dependency (go.mod/go.sum change), wrapped entirely inside `internal/autofix/apply.go`; never imported by any caller of the `autofix` package.
- GitHub REST/Git Data API — reached exclusively through the existing `internal/ghaction.Client`, never a second HTTP client.

**Replaceability:** `internal/autofix.ApplyPatch`'s signature is decoupled from `go-gitdiff` internals, so the underlying patch-apply library could be swapped without touching any caller. `ghaction.Client`'s new methods route through the same `postDo`/`get` auth/retry/redaction plumbing as existing methods, so a future auth-mechanism change is centralized in one place, not scattered across Stories 4/5.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| GitHub token handling (Stories 4/5/6) | Token used for branch/commit/PR creation | Token leaked via error messages, PR title/body content, or debug logging | Reuse `redactSecrets` on all outbound AND inbound content (PR body is new outbound attack surface per AC 05-04); require minimal `contents:write`/`repo` scope; gate validates token presence before any call |
| Patch application / arbitrary file write (Story 1) | `internal/autofix.ApplyPatch` writes to disk at `FileEntry.Path` | Crafted diff with path-traversal (`../../etc/passwd`-style) target paths | Confirm `internal/payload`'s existing symlink-escape guard is preserved end-to-end; `autofix` must not bypass or duplicate that validation |
| Validation command execution (Story 2) | Operator-configured shell command run via `os/exec` | Command injection if the configured string is ever attacker-influenced rather than operator-configured | Command is sourced from operator config only, never from diff/PR content; invoke via explicit argv where possible rather than shell string interpolation |
| `--auto-fix` gate (Story 6) | Single entry point for the entire remote-mutating flow | A misconfigured or partially-configured backend fails open instead of closed | Refuse-without-backend gate, off-by-default flag, all-or-nothing check (any one missing piece refuses the whole run) |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Patch apply loop (Story 1) | Small patches, typically 1-10 files (technical-debt fixes) | Completes well under the validation command's own timeout budget | Process files independently; no unnecessary full-tree scans |
| Validation command execution (Story 2) | Single validation run per `--auto-fix` invocation (e.g. `go build ./...`) | Bounded by a configurable timeout (default ~2 min) | `context.WithTimeout`; a timeout is a hard failure, never retried |
| GitHub Git Data API sequence (Story 4/5) | Small patches, typically <10 blobs, 4+ sequential HTTP calls | Completes within the existing `postDo` retry/backoff budget | Reuse existing retry-on-5xx/429 backoff rather than custom throttling |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Multi-file partial apply failure | Some files succeed, one fails mid-batch | Per-file isolation — the batch reports which file failed; already-succeeded files remain revertible via their individual backups |
| Diff entry type variance | New-file creation, deletion, and modification (`/dev/null` old/new-side markers) | Each type explicitly branched and tested, not assumed to share one code path |
| Validation failure modes | Command missing/not executable vs. command runs and exits non-zero | Distinct error classes — a missing command refuses the whole `--auto-fix` run fast; a non-zero exit triggers the normal revert path |
| Branch name collision | Retried or concurrent `--auto-fix` runs targeting the same branch name | 422 "ref already exists" is a recoverable, distinguishable condition, not a generic failure |
| PR already exists for branch | Second `--auto-fix` run on an already-open-PR branch | Existence check routes to update, never creates a duplicate |
| Partial `--auto-fix` backend configuration | Any one of apply target / validation command / GitHub credentials missing or malformed, independently and in combination | All-or-nothing refusal — never a partial run (e.g. apply+validate locally then silently skip PR) |

### Defensive Measures Required

- **Input Validation:** `FileEntry` paths stay within the repo root (reusing `internal/payload`'s existing symlink-escape guard); `--repo` slug validated via `parseRepo`; validation command's existence checked before first invocation.
- **Error Handling:** Conservative failure throughout — non-zero exit, timeout, or malformed input is always treated as failure, never partial success; errors wrapped with `fmt.Errorf("doing action: %w", err)` per `coding-standards.md`.
- **Logging/Audit:** Validation stdout/stderr captured for diagnostics but redacted before inclusion in PR body or logs; each `--auto-fix` run's outcome (applied/reverted/PR number) reported clearly to the operator.
- **Rate Limiting:** Reuse `ghaction.Client`'s existing retry/backoff on 429/5xx; no new custom rate limiting — this is a low-frequency, operator-triggered flow, not a hot path.
- **Graceful Degradation:** Not applicable to this CLI batch flow — failure at any gate (validation, revert, GitHub call) surfaces as a clear operator-facing error and a non-zero exit rather than silently continuing.

---

## Risks

**Technical:**
- A pushed branch or opened PR cannot be undone by Story 3's local revert (working-tree-only) → Mitigation: sequence the flow so no GitHub-mutating call happens until local validation has already passed; treat branch/PR creation as the last, not first, step (already encoded in Story 4/5's dependency on Story 2).
- A generic `go build`/lint/format validation step is more failure-prone across languages/toolchains than the existing Go-only `go/parser` guard → Mitigation: scope Story 2 to a configurable, operator-supplied *command*, not a hardcoded multi-language validator; keep the existing conservative-failure philosophy from `syntaxguard.go` for the Go path.
- Complex merge conflicts on a diverged target branch could corrupt the apply step → Mitigation: explicitly out of scope per the epic; Stories 1/3 assume a clean apply target and fail fast (then revert) rather than attempt conflict resolution.
- `go-gitdiff`'s fuzzy hunk matching could mis-apply a drifted-context hunk to the wrong location rather than failing cleanly → Mitigation: treat any non-nil `gitdiff.Apply` error as a hard failure for that file; no custom leniency layer beyond the library's own tolerance (per Story 1's own risk analysis).
- The Git Data API's 4-call sequence (blob → tree → commit → ref) can leave orphaned Git objects or a branch pointing at the wrong commit on partial failure → Mitigation: surface a clear `APIError` identifying which step failed; orphaned blobs/trees are inert and GitHub garbage-collects them, so the only stateful risk is a stale/missing branch ref (per Story 4's own risk analysis).

**TDD-Specific:**
- Testing `go-gitdiff`'s fuzzy-matching edge cases is inherently exploratory — under-testing hunk-drift scenarios is a real risk for a library new to this codebase → Mitigation: Phase 1's spike explicitly targets drifted-context fixtures before Phase 2 commits to the `internal/autofix` interface.
- Mocking the GitHub Git Data API's 4-call sequence risks getting the `httptest` routing wrong and producing a false-green test → Mitigation: route stub matching on method+path explicitly (per AC 04-03's test guidance), and add the zero-HTTP-calls-on-validation-failure regression test in Phase 6 as an independent cross-check.
- Story 6's gate must be tested to refuse on each of the three required pieces *independently and in combination* — a naive test suite could cover only the "all missing" case and miss partial-configuration gaps → Mitigation: AC 06-02 explicitly requires per-piece missing/malformed test cases, not just the all-missing case.

---

**Next:** `/create-sprint @.planning/plans/active/17.0_auto_merged_fixes/`
