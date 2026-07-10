# User Story 2: Fork + PR Automation via `gh`

**Plan:** [19.9: Community Prompt Submissions (Intake & Curation)](../plan.md)

## User Story

**As an** atcr user who has tuned a community reviewer persona locally and wants to contribute it back
**I want** `atcr personas submit <name>` to automatically fork `samestrin/atcr`, push a branch with my persona's files, and open a pull request using my own `gh` session
**So that** I can turn "I improved my local copy" into "I opened a PR" without manually forking the repo, creating a branch, or typing out `gh pr create` myself

## Story Context

- **Background:** Theme 1 (Local Fixture-Gate Reuse and Submission Blocking) establishes that `submit` only proceeds past the fixture gate on a passing persona. This story is the next stage of that same command: once `commpersonas.TestPersona` passes, `submit` must actually deliver the persona to `samestrin/atcr` as a real GitHub pull request. No code in the repository currently shells out to the `gh` CLI (confirmed via full-repo grep), so this is the first integration point of its kind. The existing `internal/ghaction.Client` (internal/ghaction/client.go:192, :345) is a token-based Git Data API wrapper built for the fixed-bot `--auto-fix` flow (Epic 17.0) and is architecturally the wrong shape here — it authenticates as one bot against one configured repo, not as an arbitrary user forking a repo into their own account.
- **Assumptions:**
  - The invoking user has the `gh` CLI installed and already has an authenticated session (`gh auth login`) established outside of atcr; `submit` does not perform its own OAuth flow.
  - `github.com/cli/go-gh/v2`'s top-level `gh` package (`gh.ExecContext`, `gh.Path()`) is the correct mechanism — it shells out to the real `gh` binary and inherits the caller's environment/credentials, avoiding any custom token management.
  - The canonical repository target is fixed at `samestrin/atcr`; `pkg/repository.Parse`/`Current` can validate this reference shape before any shell-out occurs.
  - Forking is idempotent from the user's perspective — if the user already has a fork, `gh repo fork` should not error destructively; the flow must handle "fork already exists" as a non-fatal case.
- **Constraints:**
  - Must use `gh.ExecContext(ctx, ...)`, never the fixed-bot `internal/ghaction.Client` — the two integration points serve different ownership models and must not be conflated.
  - Must precondition-check that `gh` is on `PATH` (`gh.Path()`) and authenticated (`gh auth status`) before any fork/branch/commit work begins, mirroring the precondition-check pattern already documented in `skill/SKILL.md`, and halt with a clear, actionable error if either check fails.
  - The `gh` interaction must be wrapped behind an injectable seam (interface or package var), matching the existing `personasClient`/`personasFixtureRunner` testability conventions in `cmd/atcr/personas.go`, so unit tests can stub it out rather than invoking a real `gh` binary or making network calls.
  - No marketplace, website, or hosted-registry surface may be introduced — the entire delivery mechanism must ride GitHub's native fork/branch/PR primitives (AC3).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Theme 1 (Local Fixture-Gate Reuse and Submission Blocking) — `submit` must only reach the fork/PR stage after `TestPersona` has passed |

## Success Criteria (SMART Format)

- **Specific:** After a fixture-passing persona check, `atcr personas submit <name>` forks `samestrin/atcr` into the user's account (or reuses an existing fork), pushes a branch containing the persona's files, opens a PR via `gh`, and prints the resulting PR URL to stdout.
- **Measurable:** Given a stubbed `gh` seam in tests, `submit` invokes fork, branch/push, and `pr create` operations in the correct sequence exactly once each per invocation, and surfaces the returned PR URL string back to the caller; a precondition failure (missing `gh` or unauthenticated session) short-circuits before any of those three calls occur.
- **Achievable:** Uses `github.com/cli/go-gh/v2`'s `gh.ExecContext`, `gh.Path()`, and `pkg/repository.Parse`/`Current`, all already vetted in `documentation/gh-fork-pr-integration.md`; no new auth flow or API client needs to be built.
- **Relevant:** This is the core delivery mechanism for AC1 (fork+PR automation) and AC3 (no marketplace/hosted surface — GitHub-PR-native only); without it, `submit` cannot get a persona out of the user's local machine and into the canonical repo's review queue.
- **Time-bound:** Deliverable within this sprint's implementation phase alongside the fixture-gate story; `go test ./...` must pass with the new seam fully unit-tested via stubs before the sprint's code-review gate.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-gh-precondition-check.md) | `gh` Precondition Check (PATH + Auth) Before Any Fork/Branch Work | Unit |
| [02-02](../acceptance-criteria/02-02-fork-branch-push-and-pr-create.md) | Fork, Branch/Push, and PR Create with PR URL Reported to the User | Unit |
| [02-03](../acceptance-criteria/02-03-injectable-gh-seam-for-testing.md) | Injectable `gh` Seam Matching `personasClient`/`personasFixtureRunner` Conventions | Unit |

## Original Criteria Overview

1. On a fixture-passing persona, `submit` verifies `gh` is on `PATH` and authenticated before performing any fork/branch/commit work, halting with a clear, actionable error if either check fails.
2. `submit` forks `samestrin/atcr` (or detects and reuses an existing fork), pushes a branch containing the persona's files, and opens a PR via `gh pr create`, reporting the resulting PR URL back to the user on success.
3. The `gh` interaction is wrapped behind an injectable seam consistent with `personasClient`/`personasFixtureRunner`, allowing tests to stub fork/branch/PR behavior without invoking a real `gh` binary or network call.

## Technical Considerations

- **Implementation Notes:** Add a `newPersonasSubmitCmd()` to `cmd/atcr/personas.go` following the existing `newPersonas<Verb>Cmd()` pattern. Introduce a package-level seam (e.g., a `personasGitHub` interface or package var) exposing fork/push/pr-create operations, with a default implementation backed by `gh.ExecContext(ctx, "repo", "fork", ...)`, git push, and `gh.ExecContext(ctx, "pr", "create", ...)`. Implement the precondition check (`gh.Path()` + `gh auth status` via `gh.ExecContext`) as a standalone function callable independent of the rest of the flow, per the code example already captured in `documentation/gh-fork-pr-integration.md`.
- **Integration Points:** `cmd/atcr/personas.go` (new `newPersonasSubmitCmd`, alongside existing `personasDir`/`personasClient`/`personasFixtureRunner` seams); `github.com/cli/go-gh/v2` top-level `gh` package and `pkg/repository` for target validation; the Theme 1 fixture-gate call (`commpersonas.TestPersona`) as the immediate predecessor step whose success gates entry into this flow. Explicitly does not integrate with `internal/ghaction.Client` (internal/ghaction/client.go:192, :345) — that remains scoped to the Epic 17.0 `--auto-fix` bot flow.
- **Data Requirements:** No new persistent schema; the PR body/branch must carry enough attribution/provenance metadata (submitting user, source persona name/path) to support Theme 3's `submitted` status and Theme 4's maintainer graduation review — the fork+PR payload is the delivery vehicle for that metadata, though the status field itself is defined in Theme 3.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `gh` CLI absent or unauthenticated on the user's machine, causing a confusing mid-flow failure (e.g., after fork but before PR create) | High | Run the `gh.Path()` + `gh auth status` precondition check first, before any fork/branch/commit call, and halt with a clear, actionable error message per the `skill/SKILL.md` pattern |
| Conflating this per-user fork flow with `internal/ghaction.Client`'s fixed-bot-token flow, breaking the Epic 17.0 `--auto-fix` integration or introducing the wrong ownership model | High | Keep the two integration points fully separate in code; this story introduces its own seam and explicitly does not touch `internal/ghaction.Client` |
| Hard-wiring `gh.ExecContext` calls inline in the cobra `RunE`, making the command untestable in CI without live network/GitHub access | Medium | Wrap all `gh` interaction behind an injectable interface/package var matching the `personasClient`/`personasFixtureRunner` pattern, so unit tests substitute stubs |
| User already has an existing fork of `samestrin/atcr`, and `gh repo fork` errors instead of reusing it, breaking repeat submissions | Medium | Treat "fork already exists" as a non-fatal, expected outcome and proceed to branch/push against the existing fork rather than treating it as a failure |

---

**Created:** July 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
