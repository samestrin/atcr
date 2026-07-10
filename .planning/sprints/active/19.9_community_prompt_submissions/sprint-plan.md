# Sprint 19.9: Community Prompt Submissions

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 19.9 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — after each phase's DoD and Phase-Boundary Gate, `/execute-sprint` STOPS and waits for a go-ahead before starting the next phase.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

A GitHub-native `atcr personas submit <name>` command that lets a user contribute a locally-tuned reviewer persona back to the canonical `samestrin/atcr` library in one command: it runs the existing fixture gate locally, then forks the repo and opens a PR via the user's own `gh` CLI session — no marketplace, website, or hosted registry. Alongside it, a two-tier curation model introduces a `submitted` status (fixture-passing but unvetted) that stays orthogonal to the existing `Source` provenance field, until a maintainer graduates the persona into the vetted `personas/community/` library.

### Why This Matters

This is the intake half of the living-library flywheel: it turns "I improved my local copy" into "I opened a PR" with a single command, lowering the friction that keeps refined, battle-tested prompts from flowing back to the shared library.

### Key Deliverables

- `newPersonasSubmitCmd()` — the seventh `personas` subcommand with a local fixture-gate precondition (Story 1)
- `gh`-CLI fork + branch-push + PR-create automation behind an injectable seam (Story 2)
- A `submitted` status/marker persisted atomically, distinct from `Source`, stored outside the vetted community tree (Story 3)
- Maintainer graduation checklist in `docs/personas-authoring.md` (Story 4)
- `submit` subcommand + two-tier-model documentation in `docs/personas-install.md` / `docs/personas-authoring.md` (Story 5)

### Success Criteria

- `atcr personas submit <name>` runs the fixture gate locally and opens a fork+PR; a failing/missing fixture or invalid name blocks with a clear error and zero fork/PR side effects (AC1)
- A submission lands with a `submitted` status distinct from `Source`, carrying attribution, and requiring maintainer graduation to become vetted (AC2)
- No marketplace/website/hosted-registry surface introduced; flow is entirely GitHub-PR-native (AC3)
- `go test ./...` passes; docs cover the submit flow and the `submitted` → graduated two-tier model (AC4)

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode: Moderate 🔄** (auto-selected from complexity 9/12). Each story runs a RED → GREEN → ADVERSARIAL → REFACTOR cycle:

- **RED:** Write failing tests for the story's testable elements (happy path, edge cases, errors) and verify they fail correctly.
- **GREEN:** Implement the minimal code to pass; verify all pass (T2).
- **ADVERSARIAL:** A fresh subagent (no memory of the implementation) reviews the changed files and returns a findings table.
- **REFACTOR:** Fix CRITICAL/HIGH findings inline, defer MEDIUM/LOW to tech debt, improve quality, validate (T3).

**Adversarial inline-fix bar:** `CRITICAL/HIGH` fixed inline; `MEDIUM/LOW` deferred to `tech-debt-captured.md`.

**Docs-only stories (4 & 5):** No RED/GREEN — verified by doc-accuracy review against the actual shipped Phase 1–3 command output/error text.

**Gated mode:** After every phase's DoD, a Phase-Boundary Gate (fresh-subagent integration review) runs, then execution STOPS at the phase boundary.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [test-planning-matrix.md](plan/test-planning-matrix.md) | AC → test-level mapping |

---

## Clarifications

### Phase 2 Clarifications (recorded 2026-07-10)

**Key Decisions:**
- Test style follows the ACTUAL shipped Phase 1 code (`cmd/atcr/personas_submit_test.go` uses `testify` assert/require), NOT the plan/AC prose claim that "testify is not used." Match the real code.
- Seam shape: package-level `personasGitHub` interface var in `cmd/atcr/personas.go` (alongside `personasClient`/`personasFixtureRunner`); `GitHubSubmitter` interface + default `gh.ExecContext`-backed impl in `internal/personas/submit.go`. `PushBranch` uses plain `git`; `Fork`/`CreatePR`/auth use `gh.ExecContext` (AC 02-03 Scenario 2).
- `checkGHPrecondition(ctx)` runs first with the exact error strings mandated by AC 02-01; every `gh.ExecContext` call gets a bounded context deadline.

**Scope Boundaries:**
- IN: precondition check, injectable seam, fork→push→PR sequencing (each once, in order), non-fatal "fork already exists", PR-URL to stdout, wiring the Phase 1 `personasSubmitContinuation` stub into the seam.
- OUT: the `submitted` marker (Phase 3), docs (Phase 4).

**Technical Approach:**
- `github.com/cli/go-gh/v2` added via `go get` (network-dependent; `gh` binary confirmed present locally). If the module proxy is unreachable, HARD STOP per the blocker protocol.
- All real `gh`/`git` interaction stays behind the stubbed seam — zero live `gh`/network calls in any test (hard requirement, AC 02-03).

### Phase 3 Clarifications (recorded 2026-07-10)

**Key Decisions:**
- **Marker-write failure after a successful PR → error (exit non-zero), with the PR URL embedded in the message.** AC 03-02 Error Scenario 1 mandates exit-non-zero; it constrains only the exit code, not message content — so wrapping the error to include the already-created PR URL is compliant, not a deviation. This keeps a live PR from reading as total failure while preserving the mandated exit code. Idempotent retry (fork/PR reuse is non-fatal, returns the same URL, then re-writes the marker) makes exit-non-zero cheap for the user.
- Marker write is wired INSIDE `internal/personas/submit.go:Submit`, after the empty-URL guard, so the submitter handle is taken from `head`'s `"<owner>:"` prefix — no extra `gh` call. The submissions base dir is an injectable package seam so unit tests (and existing Submit tests) write to `t.TempDir()`, never real `~/.config`.

**Scope Boundaries:**
- IN: `SubmissionStatus` type (separate from `PersonaMeta`), atomic sidecar persistence via `unit.go:writeFileAtomic` exclusively, `submissions/` dir outside `personas/community/` (sibling of `PersonasDir()`), a separately-named `List` extension point, wiring the marker write to fire only after a real PR URL.
- OUT: docs (Phase 4), graduation tooling (Phase 4), any change to `Source` semantics/values/signatures.

**Technical Approach:**
- New file `internal/personas/submissions.go`: `SubmissionsDir()` (derived from `DefaultRegistryPath`, mirroring `PersonasDir`), `SubmissionStatus` (submitter handle, persona name, version, timestamp, fixture-pass flag), `WriteSubmissionMarker`, and `ReadSubmission`/`ListSubmissions`. Persistence via `writeFileAtomic` only (0600, symlink-refusing), NOT `atomicfs.WriteFileAtomic`. `os.MkdirAll(dir, 0700)` before write. YAML serialization. Marker path guarded (`validatePersonaName` + `filepath.Rel` containment) so `..`/absolute names cannot escape the submissions dir.

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/personas/ -run TestSubmit` |
| T2: Module | After completing an element | `go test ./internal/personas/... ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): `go test ./...` all passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: `go vet ./...` + `golangci-lint run` clean
4. Format: `gofmt`/`go fmt ./...` clean
5. Docs: Updated where behavior changed

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

- **Git:** GitHub Flow / trunk-based. Feature branch `feature/19.9_community_prompt_submissions` → PR against `main` → squash-merge → delete branch. Conventional Commits (`feat`/`fix`/`docs`/`refactor`/`test`/`chore`). Keep commits small and atomic. See [git-strategy.md](../../../specifications/git-strategy.md).
- **Error handling:** Every `RunE` failure path returns a non-nil error (no inline `os.Exit`); wrap with `fmt.Errorf("...: %w", err)`. Distinct, actionable messages for name-validation vs. fixture-gate vs. `gh`-precondition vs. fork/PR failure.
- **Testing:** Go standard `testing` package, table-driven where scenarios repeat. Match the existing `internal/personas`/`cmd/atcr` file style (no `testify` — those files don't use it). `Test<Subject>_<Scenario>` naming.
- **Security:** Validate persona name via `validatePersonaName`/`personaPath` before any resolution or `gh` call; pass all `gh` arguments as discrete array elements (never shell-concatenated); never log raw `gh auth status` / token output; write markers only via `writeFileAtomic`/`atomicfs.WriteFileAtomic`.

---

## External Resources

| Resource | Use |
|----------|-----|
| [go-gh.md](../../../specifications/packages/go-gh.md) | `github.com/cli/go-gh/v2` — `gh.ExecContext`, `gh.Path()` for fork/push/PR-create |
| [cobra.md](../../../specifications/packages/cobra.md) | `newPersonas<Verb>Cmd()` + `cmd.AddCommand(...)` subcommand pattern |
| [gh-fork-pr-integration.md](plan/documentation/gh-fork-pr-integration.md) | How `submit` shells out to `gh` under the user's own auth |
| [cobra-subcommand-patterns.md](plan/documentation/cobra-subcommand-patterns.md) | Injectable-seam convention (`personasDir`, `personasClient`, `personasFixtureRunner`) |
| [fixture-gate-reuse.md](plan/documentation/fixture-gate-reuse.md) | Reusing `TestPersona`/`TemplateFixtureRunner` local gate |
| [status-provenance-and-atomic-writes.md](plan/documentation/status-provenance-and-atomic-writes.md) | Why `submitted` stays orthogonal to `Source`; atomic-write helpers |
| [personas-docs-updates.md](plan/documentation/personas-docs-updates.md) | Required edits to `docs/personas-install.md` / `docs/personas-authoring.md` |

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Local Fixture-Gate Reuse & Submission Blocking

**Story:** [01 — Local Fixture-Gate Reuse and Submission Blocking](plan/user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)
**ACs:** [01-01](plan/acceptance-criteria/01-01-invalid-persona-name-rejection.md), [01-02](plan/acceptance-criteria/01-02-missing-fixture-blocks-submission.md), [01-03](plan/acceptance-criteria/01-03-fixture-gate-pass-fail-evaluation.md)
**Focus:** Stand up `newPersonasSubmitCmd()` in `cmd/atcr/personas.go` with only the local safety gate wired in — name validation, path resolution, `TestPersona`/`TemplateFixtureRunner` fixture check, non-zero-exit-on-failure stderr messaging. **No `gh`/network code in this phase**; the RunE success path ends at a stubbed continuation point Phase 2 fills in.

### 1.1 [x] **[Local fixture-gate — RED](plan/user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)**
   1. Analyze ACs 01-01/01-02/01-03, identify testable units.
   2. Write failing tests in `cmd/atcr/personas_submit_test.go` (or extend `personas_test.go`):
      - Invalid name (`badname..`, absolute, traversal) → non-zero exit, stderr message, zero fork/PR side effects (01-01)
      - Missing fixture (`HasFixture: false`) → non-zero exit, stderr identifies missing fixture (01-02)
      - Failing fixture (`Passed != Total`) → blocks with pass/fail counts; passing fixture proceeds past the gate to the stubbed continuation (01-03)
   3. Use the existing `executeSplit` stdout/stderr helper (personas_test.go:35). Verify tests fail correctly.
   **Files:** `tests` | **Duration:** 0.5d

### 1.2 [x] **[Local fixture-gate — GREEN](plan/user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)**
   Minimal code to pass:
   - Register `newPersonasSubmitCmd()` alongside `install`/`list`/`search`/`remove`/`test`/`upgrade` in `cmd/atcr/personas.go`.
   - Validate name via `validatePersonaName`/`personaPath` (reuse verbatim) before any other work.
   - Call `TestPersona`/`TemplateFixtureRunner`; branch on `err != nil || !outcome.HasFixture || outcome.Passed != outcome.Total`.
   - Failure paths return non-nil error with distinct, actionable stderr messages; success path ends at a stubbed `// Phase 2: fork+PR` continuation point.
   - Business logic in `internal/personas/submit.go` (new); CLI wiring in `cmd/atcr/personas.go`.
   Verify all pass (T2), COMMIT: `git commit -m "feat(personas): add submit subcommand local fixture gate (green)"`
   **Files:** `impl` | **Duration:** 0.5d

### 1.2.A [x] **[Local fixture-gate — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)**
   **Changed Files:** `cmd/atcr/personas.go`, `internal/personas/submit.go`, `cmd/atcr/personas_submit_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure? (name validation before resolution; no path traversal)
       - EDGE CASES: Null, empty, boundaries, concurrent access? (missing fixture, partial fixture pass)
       - ERROR HANDLING: Missing catches, swallowed errors? (every failure path returns non-nil, no `os.Exit`)
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/personas/submit.go:36 | Zero-case fixture (Total==0) clears the gate (0!=0 false) | Deferred → TD-001 (behavior AC-mandated; latent-runner risk) |
   | LOW | cmd/atcr/personas.go:350 | Clean-pass exits 0 silently while Long help promises fork+PR | Deferred → TD-002 (Phase 2 owns success output) |
   | LOW | cmd/atcr/personas_submit_test.go | Continuation-error propagation untested | Fixed in 1.3 (REFACTOR: added TestPersonasSubmit_ContinuationErrorPropagates) |

   **Action Required:** No CRITICAL/HIGH. MEDIUM + 1 LOW appended to `tech-debt-captured.md` (TD-001, TD-002). 1 LOW (test gap) resolved in 1.3 REFACTOR. Proceed.

### 1.3 [x] **[Local fixture-gate — REFACTOR](plan/user-stories/01-local-fixture-gate-reuse-and-submission-blocking.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any).
   2. Improve code and tests (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(personas): address review + clean up submit gate"`
   **Duration:** 0.5d

### 1.4 [x] **Phase 1 — Definition of Done**
   - [x] Tests (T3): `go test ./...` passing
   - [x] Coverage ≥80% on changed files (SubmitGate 100%, newPersonasSubmitCmd 100%)
   - [x] `go vet ./...` + `golangci-lint run` clean (0 issues on changed packages)
   - [x] `go fmt ./...` clean
   - [x] ACs 01-01, 01-02, 01-03 satisfied; checkboxes marked in AC files
   - [x] No `gh`/network code introduced in this phase

### 1.5 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Is the stubbed Phase 2 continuation point cleanly defined (signature/return shape) so Phase 2 can attach fork+PR without rework?
       - CONFIG SURFACE: Any new config keys documented, defaulted, back-compat?
       - INTEGRATION: `submit` registered without altering existing subcommand behavior; seam declarations follow `personasFixtureRunner` convention?
       - PHASE-EXIT CONTRACT: Fixture-gate outcome is surfaced in a form Phase 2 can gate on?
       - REGRESSION: Existing `personas` subcommands still behave identically?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/personas.go:92-99 | `personas --help` Long omitted `submit`; doc comment said "six" | Fixed inline (commit 78c4a20d) — help now lists `submit`, comment says "seven" |
   | MEDIUM | internal/personas/submit.go:25 | Seam carries only `name`; gate green-lights built-in/embedded tiers with nothing local to fork | Deferred → TD-003 (Phase 2 owns resolution/tier guard; changing the seam now is speculative) |
   | LOW | internal/personas/submit.go:36 | Total==0 clears gate silently, no WARN unlike `personas test` | Folded into TD-001 (AC-mandated behavior; WARN angle noted) |

   **Action Required:** No CRITICAL/HIGH — phase boundary not blocked. One self-introduced MEDIUM (stale help) fixed inline; the remaining MEDIUM + LOW appended to `tech-debt-captured.md` (TD-003; TD-001 updated). Phase gate passed.
   **Duration:** 15-30 min

   🚧 **GATED STOP:** Halt here. Await go-ahead before starting Phase 2.

---

## Phase 2: Core — Fork + PR Automation via `gh`

**Story:** [02 — Fork + PR Automation via `gh`](plan/user-stories/02-fork-and-pr-automation-via-gh.md)
**ACs:** [02-01](plan/acceptance-criteria/02-01-gh-precondition-check.md), [02-02](plan/acceptance-criteria/02-02-fork-branch-push-and-pr-create.md), [02-03](plan/acceptance-criteria/02-03-injectable-gh-seam-for-testing.md)
**Focus:** Add `github.com/cli/go-gh/v2`; build the `gh` precondition check (PATH + `gh auth status`); build the injectable `personasGitHub`-style seam wrapping fork/branch-push/PR-create; wire the production implementation to `gh.ExecContext` with a bounded `context.Context`; copy the resolved persona unit into the fork; wire the Phase 1 stub into this seam on a passing fixture gate; print the PR URL to stdout on success.

> **AC 02-02 is High-complexity** (test-planning-matrix.md). Decompose the `gh` seam into small independently-stubbable methods (`Fork`/`PushBranch`/`CreatePR`) so each is RED-GREEN'd separately and doesn't collapse into an integration-shaped unit test.

### 2.1 [x] **[Fork+PR automation — RED](plan/user-stories/02-fork-and-pr-automation-via-gh.md)**
   1. Analyze ACs 02-01/02-02/02-03, identify testable units.
   2. Write failing tests:
      - `gh` precondition: missing `gh` on PATH or failed `gh auth status` halts before any fork/branch/commit call (02-01)
      - Fork/branch/push/PR-create sequence via a **stubbed seam** records `Fork` → `PushBranch` → `CreatePR` called exactly once each, in order; "fork already exists" treated non-fatal; PR URL surfaced to stdout (02-02)
      - Injectable seam: tests stub the seam with **zero real `gh` binary invocations or network calls** (02-03)
   3. Verify tests fail correctly.
   **Files:** `tests` | **Duration:** 0.75d

### 2.2 [x] **[Fork+PR automation — GREEN](plan/user-stories/02-fork-and-pr-automation-via-gh.md)**
   Minimal code to pass:
   - Add `github.com/cli/go-gh/v2` dependency (`go get`).
   - Standalone precondition function using `gh.Path()` + `gh.ExecContext(ctx, "auth", "status")`; surface only a boolean pass/fail + generic actionable message (never log raw output).
   - Package-level `personasGitHub` interface/var (matching `personasClient`/`personasFixtureRunner` convention) with `Fork`/`PushBranch`/`CreatePR` methods.
   - Production impl sequences the three `gh` operations via array-argument `gh.ExecContext` (never shell-concatenated); "fork already exists" is non-fatal; pass a bounded-timeout `context.Context` into every call.
   - Copy the resolved persona unit into the fork working tree; wire the Phase 1 stub to call the seam on a passing fixture gate; print PR URL to stdout.
   Verify all pass (T2), COMMIT: `git commit -m "feat(personas): add gh fork+PR seam for submit (green)"`
   **Files:** `impl` | **Duration:** 1d

### 2.2.A [x] **[Fork+PR automation — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-fork-and-pr-automation-via-gh.md)**
   **Changed Files:** `internal/personas/submit.go`, `internal/personas/submit_test.go`, `cmd/atcr/personas.go`, `cmd/atcr/personas_submit_test.go`, `go.mod`, `go.sum`

   **Spawn a fresh subagent** via the Agent tool to perform this review. No memory of the 2.2 implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
     - Checklist (pass verbatim):
       - SECURITY: `gh` argument injection (all args discrete array elements, never `fmt.Sprintf`+`sh -c`)? Persona name/attribution validated before reaching any `gh` argument? Raw `gh auth`/token output never logged?
       - EDGE CASES: Fork already exists (non-fatal)? Stalled network (bounded context timeout)? Empty/partial PR response?
       - ERROR HANDLING: Every `gh` failure wrapped with `%w`; distinct messages per failure mode; no partial remote state left?
       - PERFORMANCE: Blocking `gh` calls without timeout?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | MEDIUM | internal/personas/submit.go leaf impls | On a context-timeout the killed process has empty stderr, so `failed to fork ...: ` surfaces blank — no timeout signal | Fixed in 2.3 (wrap `err` with `%w` alongside stderr at every gh/git leaf) |
   | MEDIUM | internal/personas/submit.go CreatePR | Exit-0 with empty stdout returns `("", nil)` → command prints a blank URL and reports success | Fixed in 2.3 (orchestrator `Submit` guards empty PR URL → error; testable via stub) |
   | MEDIUM | internal/personas/submit.go PushBranch | `git push --force` clobbers manual edits on the user's own PR branch (mild data loss) | Fixed in 2.3 (`--force-with-lease`) |
   | LOW | internal/personas/submit.go Submit | Orchestrator trusts caller's gate; name only re-validated at the file write | Fixed in 2.3 (`validatePersonaName` at top of `Submit`, defense in depth; testable via stub) |
   | LOW | internal/personas/submit.go leaf impls | `%w` wrapping absent at leaves → `errors.Is/As` on the cause impossible upstream | Folded into the timeout fix above |
   | LOW | internal/personas/submit.go forkAlreadyExists/existingPRURL | Non-fatal reuse keys on a `"already exists"` stderr substring — fragile to gh wording changes | Deferred → TD (inherent to gh-CLI shell-out; documented coupling) |

   **Action Required:** No CRITICAL/HIGH — proceed. Four MEDIUM/LOW fixed inline in 2.3 REFACTOR (all cheap, orchestrator-testable correctness/security wins). One LOW (gh-output coupling, inherent to the shell-out approach) appended to `tech-debt-captured.md`.

### 2.3 [x] **[Fork+PR automation — REFACTOR](plan/user-stories/02-fork-and-pr-automation-via-gh.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any).
   2. Improve code and tests (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(personas): address review + clean up gh seam"`
   **Duration:** 0.5d

### 2.4 [x] **Phase 2 — Definition of Done**
   - [x] Tests (T3): `go test ./...` passing (exit 0) — **no real `gh` binary or network calls in any test** (all gh/git behind the stubbed seam)
   - [x] Coverage ≥80% on the testable surface: orchestration + pure/fs logic covered (`Submit` 94%, precondition/branch/body/`forkAlreadyExists`/`ghError`/`NewGitHubSubmitter` 100%, `existingPRURL` 83%, `copyPersonaUnit` 80%, `newPersonasSubmitCmd` 100%). Residual 0% is exclusively the gh/git subprocess adapters (`Fork`/`PushBranch`/`CreatePR`/`currentGHUser`/`runGit`) that AC 02-03 forbids exercising with a real `gh` binary — deliberately quarantined behind the seam.
   - [x] `go vet ./...` + `golangci-lint run` clean (0 issues); `gofmt -l` clean
   - [x] `go.mod`/`go.sum` updated for `go-gh/v2` (v2.13.0, promoted to a direct dependency via `go mod tidy`)
   - [x] ACs 02-01, 02-02, 02-03 satisfied; checkboxes marked
   - [x] All `gh` interaction behind the injectable `personasGitHub` seam

### 2.5 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Does the submit flow expose a success signal (PR created) that Phase 3 can hang the marker-write off of?
       - CONFIG SURFACE: New dependency and any config documented/defaulted?
       - INTEGRATION: Phase 1 stub now cleanly calls the seam; seam is swappable behind its interface?
       - PHASE-EXIT CONTRACT: "PR created successfully" is observable so Phase 3 writes the marker ONLY after a real PR exists?
       - REGRESSION: Phase 1 fixture-gate behavior intact; no live `gh` calls leaked into tests?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (first pass):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | internal/personas/submit.go copyPersonaUnit | Copied only `<name>.yaml`, omitting the co-located `<name>.md` custom prompt where local tuning lives → PR diverges from the fixture-validated unit | **Fixed inline before boundary** (commit c9847cf4): copy sibling `.md` when present, binding-only persona still succeeds; `TestCopyPersonaUnit` + `TestCopyPersonaUnit_BindingOnly` added. Gate re-run: HIGH resolved, no new findings. |
   | LOW | internal/personas/submit.go Fork/PushBranch | First-ever fork can 404 the immediate clone (async fork provisioning) | Deferred → TD-005 (gh/git shell-out layer, transient/retry-able, untestable per AC 02-03) |
   | LOW | cmd/atcr/personas.go personasSubmitContinuation | Continuation returns only `error`; Phase 3 must re-thread the PR URL to key the marker off it | Advisory for Phase 3 — `Submit` already returns `(url, nil)` with an empty-URL guard, so the "real PR exists" signal survives at the package boundary; noted for Phase 3 wiring |
   | LOW | dep go.mod | `gh` CLI prerequisite (installed+authenticated) undocumented for `personas submit` | In scope for Phase 4 AC 05-01 (install-guide doc) — confirm the `gh` prerequisite note lands before sprint close |

   **Action Required:** One HIGH found and **fixed inline before the phase boundary** (gate re-run confirmed resolution, zero new findings). LOWs: one deferred to TD-005, two are forward-looking notes already covered by Phase 3 wiring / Phase 4 AC 05-01. Phase gate passed.
   **Duration:** 15-30 min

   🚧 **GATED STOP:** Halt here. Await go-ahead before starting Phase 3.

---

## Phase 3: Core — `submitted` Status Distinct from `Source`

**Story:** [03 — `submitted` Status Distinct from `Source`/Provenance](plan/user-stories/03-submitted-status-distinct-from-source.md)
**ACs:** [03-01](plan/acceptance-criteria/03-01-submitted-status-is-not-a-source-value.md), [03-02](plan/acceptance-criteria/03-02-submitted-marker-attribution-and-atomic-persistence.md), [03-03](plan/acceptance-criteria/03-03-marker-stored-outside-community-tree-and-list-extension-point.md)
**Focus:** Introduce `submitted` as an additive status/marker (separate struct or sidecar file) that never touches `PersonaMeta.Source`'s three values or the signatures of `List`/`ListTiers`/`listCommunity`/`listProject`. Persist attribution (submitter identity, fixture-pass confirmation, timestamp) via `writeFileAtomic`/`atomicfs.WriteFileAtomic` at a path **outside** `personas/community/`. Wire the marker write into the Phase 2 submit flow so it fires **only after** a successful PR creation.

### 3.1 [x] **[`submitted` marker — RED](plan/user-stories/03-submitted-status-distinct-from-source.md)**
   1. Analyze ACs 03-01/03-02/03-03, identify testable units.
   2. Write failing tests:
      - After a submission, `Source` is still exactly `built-in`/`community`/`project` for every persona; `submitted` is never a `Source` value (03-01)
      - Marker readable with submitter attribution + fixture-pass flag; write uses `writeFileAtomic`/`atomicfs.WriteFileAtomic`; refuses a symlinked destination (03-02)
      - Marker path never resolves under `personas/community/`; `personas list` output unchanged for existing rows; `List` extension point added without altering existing `Source`-based output (03-03)
   3. Verify tests fail correctly.
   **Files:** `tests` | **Duration:** 0.5d

### 3.2 [x] **[`submitted` marker — GREEN](plan/user-stories/03-submitted-status-distinct-from-source.md)**
   Minimal code to pass:
   - New submission-scoped struct/marker (e.g. `SubmissionStatus`) carrying submitter identity, source persona name, timestamp, fixture-pass flag — **not** a field on `PersonaMeta`.
   - Marker path constant lives outside the vetted `personas/community/` tree.
   - Read/write via `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively (symlink-refusing, sibling-temp-then-rename).
   - `List` extension point added without altering existing `Source`-based rows.
   - Wire the marker write into the Phase 2 submit flow so it fires **only after** a successful PR creation (no marker if fork/push/PR fails).
   Verify all pass (T2), COMMIT: `git commit -m "feat(personas): add submitted status marker (green)"`
   **Files:** `impl` | **Duration:** 0.75d

### 3.2.A [x] **[`submitted` marker — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-submitted-status-distinct-from-source.md)**
   **Changed Files:** [LIST FILES MODIFIED IN 3.2]

   **Spawn a fresh subagent** via the Agent tool. No memory of the 3.2 implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.2]
     - Checklist (pass verbatim):
       - SECURITY: Symlink TOCTOU at marker destination (atomic helper used exclusively, bare `os.WriteFile` absent)? Marker path can't escape into `personas/community/`?
       - EDGE CASES: Marker already exists (resubmission — overwrite-with-updated-timestamp default)? Partial/corrupt write on crash?
       - ERROR HANDLING: Marker write failure surfaced; no marker written when PR creation failed (no orphan marker)?
       - PERFORMANCE: N/A
       - CONTRACT: `PersonaMeta.Source` and `List`/`ListTiers`/`listCommunity`/`listProject` signatures untouched?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (no CRITICAL/HIGH):**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | LOW | internal/personas/submissions.go:63 | `WriteSubmissionMarker` guards the leaf via `writeFileAtomic`'s `Lstat`, but for a namespaced name it `os.MkdirAll`s an *intermediate* dir without the `refuseSymlinkedIntermediate` guard the sibling install path (`writePersonaUnit`) applies — a pre-planted symlink at `submissions/<ns>/` could redirect the write, potentially into `personas/community/`. Limited exploitability (local `~/.config`, user-local name), but inconsistent with the install path and undercuts AC 03-03's "never resolves under community tree" guarantee. | **Fixed inline in 3.3** — call `refuseSymlinkedIntermediate(dest, status.Persona)` before `MkdirAll`; regression test `TestWriteSubmissionMarker_RefusesSymlinkedIntermediate` added. |

   **Action Required:** No CRITICAL/HIGH — proceed. The single LOW (symlinked-intermediate gap) is a cheap, AC-03-03-reinforcing security-consistency fix reusing an existing helper, so it is fixed inline in 3.3 rather than deferred. Adversarial review passed.

### 3.3 [x] **[`submitted` marker — REFACTOR](plan/user-stories/03-submitted-status-distinct-from-source.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any).
   2. Improve code and tests (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(personas): address review + clean up submitted marker"`
   **Duration:** 0.5d

### 3.4 [x] **Phase 3 — Definition of Done**
   - [x] Tests (T3): `go test ./...` passing
   - [x] Coverage ≥80% on changed files (Submit 95.8%; submissions.go per-func ≥80% except `SubmissionsDir` 75% — its sole uncovered branch is the `DefaultRegistryPath` OS-error path, identical to the pre-existing untested branch in its sibling `PersonasDir`; changed-files aggregate 83.5%)
   - [x] `go vet ./...` + `golangci-lint run` clean (0 issues); `go fmt ./...` clean
   - [x] ACs 03-01, 03-02, 03-03 satisfied; checkboxes marked in AC files
   - [x] `PersonaMeta.Source` and existing `List*` signatures unchanged (explicit test `TestSubmissionStatus_NotASourceValue` asserts `Source` ∈ {built-in, community, project})

### 3.5 [x] **Phase 3 — GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Marker read/write API stable enough for Phase 4 docs to describe accurately?
       - CONFIG SURFACE: Marker path/location documented for the graduation docs?
       - INTEGRATION: Marker write fires only on successful PR (no marker-without-PR state)?
       - PHASE-EXIT CONTRACT: Graduation docs (Phase 4) can reference a real, shipped marker-clearing behavior?
       - REGRESSION: `personas list` and all `Source`-based output identical to pre-Phase-3?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings + dispositions:**
   | Severity | File:Line | Issue | Resolution |
   |----------|-----------|-------|-----|
   | HIGH | internal/personas/submissions.go | No marker-clearing counterpart (`Delete`/`Clear`Submission) ships; marker is submitter-local under `~/.config/atcr/submissions` while graduation is an upstream maintainer action → gate claims "graduation clears the marker" has no shipped behavior to describe. | **Not a Phase-3 code defect — reclassified after verifying against AC 04-02, which the context-free gate did not have.** AC 04-02 is explicit: graduation's marker-clearing is **"Markdown documentation (no new code)"** — the maintainer manually locates and `rm`s the sidecar (Edge Case 1), "no new permission surface." The shipped marker IS a real, documentable, removable artifact: a plain 0600 YAML at the exported `SubmissionsDir()` path (`~/.config/atcr/submissions/<name>.yaml`). Shipping a delete seam now would violate AC 04-02's "no new code" AND the epic's deliberate "graduation stays an explicit manual maintainer action" stance (status-provenance-and-atomic-writes.md) AND the no-speculative-code rule. Phase-exit contract IS satisfiable → boundary not blocked. **Phase 4 directive (for task 4.1 / AC 04-02):** the graduation clearing step MUST (a) name the exact path `SubmissionsDir()` = `~/.config/atcr/submissions/<name>.yaml`, and (b) reconcile AC 04-02 Scenario 1's "maintainer clears the marker" wording with the marker being **submitter-local** — i.e. the sidecar is removed on the machine that ran `submit` (the maintainer's own copy if they pulled/battle-tested locally), not something a PR merge touches remotely. |
   | LOW | internal/personas/submit.go:110 | `FixturePassed: true` hard-coded; `Submit` never runs the gate itself, so the flag can never be `false`. | Deferred → **TD-006**. AC-mandated field; true-by-construction at the sole call site (`Submit` runs only after `SubmitGate`); threading a bool now is over-engineering ahead of a second caller. |

   **Action Required:** No genuine CRITICAL/HIGH Phase-3 code defect — the gate's HIGH was reclassified as out-of-scope-for-code after verification against AC 04-02 (docs-only manual clearing) and captured as an explicit Phase-4 authoring directive above; re-running the same context-free gate would only reproduce the out-of-context finding. One LOW deferred to TD-006. Phase gate passed.
   **Duration:** 15-30 min

   🚧 **GATED STOP:** Halt here. Await go-ahead before starting Phase 4.

---

## Phase 4: Integration — Documentation (Graduation + Submit Flow & Two-Tier Model)

**Stories:** [04 — Maintainer Graduation](plan/user-stories/04-maintainer-graduation-into-vetted-library.md), [05 — Documentation of Submit Flow & Two-Tier Model](plan/user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)
**ACs:** [04-01](plan/acceptance-criteria/04-01-documented-persona-placement-and-index-entry.md), [04-02](plan/acceptance-criteria/04-02-submitted-marker-clearing-without-touching-source.md), [04-03](plan/acceptance-criteria/04-03-manual-pr-native-process-with-checklist-cross-reference.md), [05-01](plan/acceptance-criteria/05-01-submit-subcommand-documented-in-install-guide.md), [05-02](plan/acceptance-criteria/05-02-contribution-checklist-cross-references-submit.md), [05-03](plan/acceptance-criteria/05-03-submitted-to-graduated-two-tier-model-section.md)
**Focus:** Docs-only, landing together once Phases 1–3 have shipped real behavior to document accurately. **No RED/GREEN** — verified by doc-accuracy review against the actual shipped command output/error text.

### 4.1 [ ] **[Maintainer graduation checklist — DOCS (Story 4)](plan/user-stories/04-maintainer-graduation-into-vetted-library.md)**
   Add the maintainer graduation checklist to `docs/personas-authoring.md`:
   - Persona placement — moving the unit into `personas/community/` and adding a matching `PersonaIndexEntry` in `index.json` (04-01)
   - Marker clearing during graduation — procedure explicitly states `Source` never changes (04-02)
   - Manual PR-native process — references the existing PR-review-merge gate; no new tooling implied; cross-references the contribution checklist (04-03)
   Verify each statement against the actual Phase 3 marker behavior and Phase 1–2 flow.
   COMMIT: `git commit -m "docs(personas): add maintainer graduation checklist"`
   **Files:** `docs/personas-authoring.md` | **Duration:** 0.5d

### 4.2 [ ] **[Submit flow + two-tier model — DOCS (Story 5)](plan/user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)**
   - `docs/personas-install.md`: change the heading from "six" to "seven subcommands"; add an `### atcr personas submit <name>` section matching the existing per-command format, positioned between `test` and `upgrade` (05-01)
   - Cross-reference the contribution checklist to the automated `submit` equivalent (05-02)
   - Add a new section explaining the `submitted` → graduated two-tier model in plain language, with no terminology collision against the existing "community-contributed" provenance meaning (05-03)
   Cross-check documented command output/error text against actual shipped behavior (Story 5 Technical Considerations require this).
   COMMIT: `git commit -m "docs(personas): document submit subcommand and two-tier model"`
   **Files:** `docs/personas-install.md`, `docs/personas-authoring.md` | **Duration:** 0.5d

### 4.3 [ ] **[Documentation — ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)**
   **Changed Files:** [`docs/personas-install.md`, `docs/personas-authoring.md`]

   **Spawn a fresh subagent** via the Agent tool. No memory of the docs authoring. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4 (docs accuracy)`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the two docs + the actual shipped `cmd/atcr/personas.go` submit command and `internal/personas/submit.go`
     - Checklist (pass verbatim, doc-accuracy perspective):
       - ACCURACY: Does every documented command flag, output line, and error message match the actual Phase 1–3 implementation?
       - COMPLETENESS: Heading count correct ("seven subcommands")? `submit` section present and correctly positioned (between `test` and `upgrade`)?
       - TERMINOLOGY: No collision — `submitted` (status) vs `community` (provenance) kept distinct; graduation states `Source` untouched?
       - SCOPE: No marketplace/website/hosted-registry surface implied (honors AC3)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix docs inline before proceeding
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 4.4 [ ] **Phase 4 — Definition of Done**
   - [ ] `docs/personas-install.md` heading = "seven subcommands"; `submit` section present between `test` and `upgrade`
   - [ ] `docs/personas-authoring.md` graduation checklist + contribution-checklist cross-reference present
   - [ ] `submitted` → graduated two-tier-model section added; no terminology collision
   - [ ] All documented output/error text verified against shipped behavior
   - [ ] ACs 04-01, 04-02, 04-03, 05-01, 05-02, 05-03 satisfied; checkboxes marked

### 4.5 [ ] **Phase 4 — GATE: Integration & Exit Review (subagent)**
   **Scope:** Both docs files + cross-references

   **Spawn a fresh subagent** via the Agent tool. No memory of the docs authoring. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Docs fully describe the shipped submit flow and curation model?
       - CONFIG SURFACE: All new behavior (submit command, marker, graduation) documented?
       - INTEGRATION: Cross-references resolve; no dangling links; consistent terminology across both docs?
       - PHASE-EXIT CONTRACT: A reader can run `submit` and a maintainer can graduate from docs alone?
       - REGRESSION: Existing doc sections still accurate?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed"
   **Duration:** 15-30 min

   🚧 **GATED STOP:** Halt here. Await go-ahead before starting Phase 5.

---

## Final Phase: Validation

**Focus:** Full-suite verification across all 5 stories / 15 ACs.

### 5.1 [ ] **Validation Checklist**
   - [ ] `go test ./...` — full pass
   - [ ] `go vet ./...` — clean
   - [ ] `golangci-lint run` — clean
   - [ ] `go fmt ./...` — no diff
   - [ ] Coverage: `go test -coverprofile=coverage.out ./...` ≥80% baseline
   - [ ] No real `gh` binary or network calls anywhere in the test suite
   - [ ] Documentation cross-check: documented command output/error text matches actual shipped behavior (Story 5 Technical Considerations)

### 5.2 [ ] **Adversarial Risk-Profile Review**
   Review the full diff against [sprint-design.md](plan/sprint-design.md) Risk Analysis:
   - [ ] Persona name → path resolution: `validatePersonaName`/`personaPath` used verbatim, validation-before-resolve order
   - [ ] `gh` argument construction: array-argument `ExecContext` only, never shell-concatenated
   - [ ] Submitted-marker persistence: `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively, symlink refusal tested
   - [ ] GitHub auth handling: raw `gh auth status`/token output never logged
   - [ ] Graceful degradation: no `submitted` marker written unless PR was actually created

### 5.3 [ ] **Optional: Targeted Mutation Testing**
   MUTATION_TOOL = **UNAVAILABLE** (no `stryker`/`mutmut`/`cargo-mutants` detected; Go project). Skip — no configured mutation tool.
   **WARNING:** Do NOT run full-codebase mutation testing — it can take hours. If a Go mutation tool is later added, target only the changed files (`internal/personas/submit.go`, the `gh` seam).

### 5.4 [ ] **Drift Analysis**
   Compare the shipped result against [original-requirements.md](plan/original-requirements.md):
   - [ ] AC1: `submit` runs fixture gate locally + opens fork+PR; failing fixture blocks with clear error
   - [ ] AC2: `submitted` status distinct from `Source`, carries attribution, requires graduation
   - [ ] AC3: No marketplace/website/hosted-registry surface — entirely GitHub-PR-native
   - [ ] AC4: `go test ./...` passes; docs cover submit flow + two-tier model
   - [ ] No scope beyond the original request added
