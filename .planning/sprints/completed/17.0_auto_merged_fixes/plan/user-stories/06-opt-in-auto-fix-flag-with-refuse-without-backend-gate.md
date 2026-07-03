# User Story 6: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer or CI/CD pipeline operator
**I want** the entire auto-merge flow (apply, validate, revert, branch, PR) to stay completely dormant unless I explicitly pass `--auto-fix` and have every required backend configured
**So that** I never trigger an unattended, remote-mutating fix flow by accident, and a half-configured setup fails immediately with a clear usage error instead of stumbling partway through applying, validating, or pushing a fix

## Story Context

- **Background:** User Stories 1–5 build the individual links of the auto-merge chain: apply a parsed patch (AC2), run local validation (AC3), revert on failure (AC4), and create a branch/commit/PR via the GitHub API (AC5, split across two stories). None of that machinery may run by default — this story is the single gate that turns the whole chain on, and only when it can actually complete safely end-to-end. It directly mirrors Epic 11.0's `--exec` flag on `atcr review`/`atcr verify` (`cmd/atcr/review.go:47`: `Bool("exec", false, ...)`), which is off by default and refuses to run without a configured, preflight-passing sandbox backend — this story reproduces that exact shape for `--auto-fix` against a different backend (apply target + validation command + GitHub credentials, instead of a sandbox).
- **Assumptions:**
  - `--auto-fix` is a new boolean flag, off by default, most likely registered on `atcr review` alongside the existing `--exec`/`--verify` flags (or as a new `atcr autofix` subcommand mirroring `newGithubCmd`'s shape in `cmd/atcr/github.go`) — the exact host command is a design-sprint decision, not fixed by this story.
  - "Fully configured backend" for this flag means three independent pieces are present and syntactically valid before any of Stories 1–5 execute: (1) an apply target (the working tree `internal/autofix` will patch), (2) a validation command (Story 2's AC3 configuration), and (3) GitHub token/repo credentials (Stories 4–5's AC5 configuration, following `runGithub`'s `--token`/`GITHUB_TOKEN` and `--repo`/`GITHUB_REPOSITORY` resolution in `cmd/atcr/github.go:83-100`).
  - This story validates configuration *presence and shape* only (e.g., a token string is non-empty, a repo slug parses as `owner/name` via `parseRepo`) — it does not perform a live preflight call (e.g., an actual GitHub API round-trip to confirm the token has write access), matching Epic 11.0's split between a fast config-shape check and Docker's separate live `Preflight(ctx)` check.
  - The gate runs once, at the top of the `--auto-fix` flow, before Story 1's apply step ever touches a file — a mid-flow discovery of missing configuration is exactly the failure mode this story exists to prevent.
- **Constraints:**
  - Default `atcr` behavior (every command and flag that exists today) must be completely unchanged when `--auto-fix` is absent — this is an additive, opt-in surface only.
  - Missing or invalid configuration must fail fast as a usage error (exit code 2, via the existing `usageError` helper used throughout `cmd/atcr`, e.g. `cmd/atcr/github.go:106,112`), not a deep runtime error surfaced after Story 1 has already written to disk.
  - The refuse-without-backend check must be all-or-nothing: if any one of the three required pieces (apply target, validation command, GitHub credentials) is missing or malformed, the entire `--auto-fix` run refuses to start — partial configuration must not result in partial execution (e.g., applying and validating locally but then silently skipping the PR step).
  - No new configuration mechanism: reuse the existing `.atcr/config.yaml` + flag + env-var resolution precedent (`envOr`, `cmd.Flags().GetString`, `parseRepo`) rather than inventing a second config surface.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | User Story 2 (Configurable local validation — AC3, supplies the validation-command configuration this gate must find present before allowing a run); User Story 4 and User Story 5 (Branch/commit creation and PR open/update — AC5, supply the GitHub token/repo configuration this gate must find present and syntactically valid); does not depend on User Story 1 or User Story 3 directly, but gates all five stories' execution since it is the flow's single entry point |

## Success Criteria (SMART Format)

- **Specific:** `--auto-fix` is absent from `atcr`'s default flag set behavior — running any existing command without it produces byte-identical behavior to before this story shipped — and when passed, the flow refuses to proceed (exit 2, usage error) unless an apply target, a validation command, and valid-shaped GitHub token/repo credentials are all present.
- **Measurable:** 100% of test cases covering each of the three missing/malformed backend pieces (missing validation command, missing/empty GitHub token, malformed `--repo` slug) independently produce a usage error before any file on disk is touched or any network call is attempted; a fully configured backend allows the flow to proceed into Story 1's apply step with zero additional gating overhead.
- **Achievable:** Directly reuses the config-resolution and error-signaling primitives already proven in `cmd/atcr/github.go` (`envOr`, `parseRepo`, `usageError`) and the exact off-by-default/refuse-without-backend shape already shipped for `--exec` in Epic 11.0 — no new validation framework is needed, only wiring three existing-shaped checks into one gate function.
- **Relevant:** This is AC6 and the safety valve for the entire plan — without it, Stories 1–5's powerful, remote-mutating flow (branch creation, commit, PR open) could run accidentally or with a half-configured backend, which is precisely the risk this plan's Risk Mitigation section calls out for AC5.
- **Time-bound:** Deliverable last in the plan's sequence (Theme 6), once Stories 2, 4, and 5 have defined the exact shape of the configuration each of them needs, so this story's gate function can name the concrete config keys/flags/env vars it checks.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [06-01](../acceptance-criteria/06-01-auto-fix-off-by-default.md) | `--auto-fix` Off by Default, Zero Behavior Change When Absent | Integration |
| [06-02](../acceptance-criteria/06-02-single-gate-refuses-on-any-missing-piece.md) | Single Gate Refuses the Entire `--auto-fix` Run on Any Missing/Malformed Backend Piece | Unit/Integration |
| [06-03](../acceptance-criteria/06-03-gate-passes-silently-when-fully-configured.md) | Gate Passes Silently When Fully Configured, No Overhead Into Story 1 | Unit/Integration |

## Original Criteria Overview

1. `--auto-fix` defaults to `false` (or the flow is otherwise absent) on every existing ATCR command, and no existing command's behavior changes when the flag is not passed.
2. When `--auto-fix` is passed, a single gate function runs before Story 1's apply step and checks, in one pass, that an apply target, a validation command (Story 2), and valid-shaped GitHub token/repo credentials (Stories 4/5) are all present — any one missing or malformed piece produces a usage error (exit 2) naming the specific missing/invalid piece, and the flow does not proceed.
3. When all three backend pieces are present and valid-shaped, the gate passes silently and control proceeds into Story 1's apply step with no observable overhead or side effect from the gate itself.


## Technical Considerations

- **Implementation Notes:** Add `cmd.Flags().Bool("auto-fix", false, ...)` following the exact pattern of `cmd/atcr/review.go:47`'s `--exec` flag registration (likely on `atcr review`, alongside `--exec`/`--verify`, per the plan's Technical Planning Notes: "`--auto-fix` flag defined in `cmd/atcr/review.go` and/or a new `cmd/atcr/autofix.go`"). Implement a single `validateAutoFixBackend(cmd *cobra.Command) error` gate function that resolves and checks all three required pieces up front — apply target, Story 2's validation-command config, and Stories 4/5's GitHub token/repo (via `envOr`/`parseRepo`, mirroring `cmd/atcr/github.go:83-100`) — returning a `usageError` on the first missing/invalid piece it finds (or an aggregate message listing all missing pieces, whichever reads more clearly to the operator). This function is called once, at the very top of the `--auto-fix` code path, strictly before any `internal/autofix` apply call.
- **Integration Points:** Gates the entry point to the full Story 1→2→3→4→5 chain in `cmd/atcr` — no other story's code should be reachable via the `--auto-fix` flag until this gate has returned nil. Reuses `usageError` (repo-wide usage-error convention), `envOr` and `parseRepo` (`cmd/atcr/github.go`) for GitHub credential resolution, and whatever config key Story 2 defines for the validation command. Does not modify `internal/ghaction`, `internal/autofix`, or `internal/verify` — this story is purely the CLI-level gate wired in front of them.
- **Data Requirements:** No persistent schema. Reads existing configuration surfaces only: CLI flags, environment variables (`GITHUB_TOKEN`, `GITHUB_REPOSITORY`, matching `runGithub`'s existing resolution), and whatever `.atcr/config.yaml` block Story 2 introduces for the validation command — this story does not define new config schema, only consumes what Stories 2/4/5 already define and checks for its presence.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The gate only checks configuration *presence and shape*, not live validity (e.g., a syntactically valid but revoked GitHub token, or a validation command that doesn't actually exist on `$PATH`) — a "passed the gate" run could still fail deep into Story 4/5's GitHub calls. | Medium | Explicitly scope this story to shape-validation only, matching Epic 11.0's precedent (config-shape check is separate from Docker's live `Preflight(ctx)`); a live-token/live-command failure surfaces as a normal Story 2/4/5 runtime error, not silently treated as this gate's responsibility — document this boundary in the gate's error messages so a shape-pass isn't mistaken for a full guarantee. |
| A future contributor adds a new required backend piece to Stories 2/4/5 (e.g., a new GitHub scope requirement) but forgets to add the corresponding check to this gate, silently reopening the "runs half-configured" risk this story exists to close. | Medium | Keep the three checks colocated in one small `validateAutoFixBackend` function (not scattered across each story's own code) so it is the obvious single place to extend, and cover each of the three checks with an explicit missing-piece test so a regression in any one is caught by CI rather than discovered at runtime. |
| Registering `--auto-fix` on the existing `atcr review` command risks accidental interaction with review's other one-shot flags (`--verify`, `--exec`, `--debate`), since `review.go` already composes several boolean flags with cross-flag validation rules. | Low | Follow `review.go`'s existing precedent for flag interaction checks (e.g. `--inline-comments requires --pr`-style validation in `cmd/atcr/github.go:123-125`) — if `--auto-fix` needs to be mutually exclusive with or dependent on another review flag, that rule is validated in the same fast, pre-execution gate pass, not discovered mid-run; if the design-sprint instead chooses a standalone `atcr autofix` subcommand, this risk does not apply. |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Refined - Ready for Execution
