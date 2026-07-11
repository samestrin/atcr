# Acceptance Criteria: Fork, Branch/Push, and PR Create with PR URL Reported to the User

**Related User Story:** [2: Fork + PR Automation via `gh`](../user-stories/02-fork-and-pr-automation-via-gh.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function/seam (`personasGitHub.Submit` or equivalent) invoked from `newPersonasSubmitCmd` | Sequences `gh repo fork` → branch/push → `gh pr create`, each exactly once per invocation |
| Test Framework | Go `testing` package | Stubbed seam records call order/count; no live `gh` process or network call in tests; testify is not used in this codebase |
| Key Dependencies | `github.com/cli/go-gh/v2` (`gh.ExecContext`), `pkg/repository` (`Parse`/`Current`) for validating the `samestrin/atcr` target | `pkg/repository.Parse("samestrin/atcr")` runs before the fork call to confirm the reference is well-formed |

### Related Files (from codebase-discovery.json)
- `internal/personas/submit.go` (create) — fork/branch-push/PR-create sequencing logic, fork-already-exists detection, PR URL extraction and return to the caller
- `cmd/atcr/personas.go` (modify) — `newPersonasSubmitCmd()` calls the submit sequence only after `commpersonas.TestPersona` passes and the AC 02-01 precondition check succeeds, then prints the returned PR URL to stdout
- `internal/personas/submit_test.go` (create) — unit tests asserting fork/branch/PR call order, exactly-once invocation, and PR URL propagation via a stubbed seam
- `documentation/gh-fork-pr-integration.md` (reference only) — source for `gh repo fork`, branch/push, and `gh pr create` sequencing via `gh.ExecContext`

## Design References
- [GitHub Fork + PR Integration via go-gh](../documentation/gh-fork-pr-integration.md) — the `gh repo fork`, branch/push, and `gh pr create` sequence and the "fork already exists" handling
- [Cobra Subcommand & Injectable-Seam Conventions](../documentation/cobra-subcommand-patterns.md) — the injectable-seam pattern that lets tests stub the `gh` integration without real network calls

## Happy Path Scenarios
**Scenario 1: No existing fork — full fork, branch, push, PR-create sequence**
- **Given** a persona `<name>` whose fixture has already passed (`commpersonas.TestPersona`) and the user has no existing fork of `samestrin/atcr`
- **When** `atcr personas submit <name>` runs past the AC 02-01 precondition check
- **Then** the command invokes `gh repo fork` (or equivalent) exactly once, pushes a branch containing the persona's files exactly once, invokes `gh pr create` exactly once, and prints the resulting PR URL to stdout in the form `https://github.com/<owner>/<repo>/pull/<n>`

**Scenario 2: Existing fork reused**
- **Given** the user already has a fork of `samestrin/atcr` in their account
- **When** `atcr personas submit <name>` runs
- **Then** the "fork already exists" outcome from `gh repo fork` is treated as non-fatal, the flow proceeds to push the branch against the existing fork, and a PR is opened exactly once, with the PR URL reported on success

## Edge Cases
**Edge Case 1: Persona files already match an existing open PR's branch content (re-submission)**
- **Given** the user re-runs `submit` for a persona they already submitted, with no local changes since the last submission
- **When** the branch/push step runs
- **Then** the push is treated as a no-op update to the existing remote branch (or a new PR-create call surfaces GitHub's "a pull request already exists" condition), and the flow reports the existing/updated PR URL rather than crashing or silently opening a duplicate PR

**Edge Case 2: Provenance metadata carried in PR body**
- **Given** a persona submission destined for Theme 3's `submitted` status and Theme 4's maintainer graduation review
- **When** `gh pr create` is invoked
- **Then** the PR body includes the submitting user's identity (as resolved by the authenticated `gh` session) and the source persona's name/path, so downstream curation has enough attribution to work from — without this command owning or writing the `submitted` status field itself

## Error Conditions
**Error Scenario 1: Fork call fails for a reason other than "already exists"**
- **Given** `gh repo fork` returns a non-zero exit for a permissions or network reason
- **Error message:** `"failed to fork samestrin/atcr: <captured stderr>"`
- **Then** the flow halts before any branch/push or PR-create call is attempted; exit code non-zero

**Error Scenario 2: Branch/push fails after a successful fork**
- **Given** the fork step succeeded (new or reused) but the branch push fails (e.g. local git error, permission denied on the fork remote)
- **Error message:** `"failed to push branch to fork: <captured error>"`
- **Then** the flow halts before any `gh pr create` call; no PR is opened against a branch that was never successfully pushed

**Error Scenario 3: PR create fails after a successful branch push**
- **Given** the branch was pushed successfully but `gh pr create` returns a non-zero exit
- **Error message:** `"branch pushed to <fork>, but PR creation failed: <captured stderr>; retry with 'gh pr create' or re-run 'atcr personas submit <name>'"`
- **Then** the command exits non-zero but does not attempt to roll back the already-pushed branch, and the error message gives the user a concrete recovery path

## Performance Requirements
- **Response Time:** The full fork/branch/push/PR-create sequence depends on GitHub API and git network latency; the command itself adds no additional polling or retry loops beyond a single attempt per step (typically completes within a few seconds to ~30s depending on repo size and network conditions)
- **Throughput:** N/A (single-invocation CLI command, one submission per run)

## Security Considerations
- **Authentication/Authorization:** All GitHub operations ride the invoking user's own `gh auth login` session (validated in AC 02-01); no bot token or separately-managed credential is read, stored, or transmitted by this flow
- **Input Validation:** The persona `<name>` argument is validated against the existing persona-resolution rules (already enforced by `commpersonas.TestPersona`/`personasFixtureRunner`) before being used to construct file paths and branch names; the target repo reference (`samestrin/atcr`) is validated via `pkg/repository.Parse` before any shell-out, preventing malformed or attacker-influenced repo arguments from reaching `gh.ExecContext`

## Test Implementation Guidance
**Test Type:** UNIT (seam-stubbed); no INTEGRATION/E2E test invokes a real `gh` binary or live GitHub API
**Test Data Requirements:** A fixture-passing persona name/path fixture (reusing Theme 1's fixture-gate test data) and a stubbed seam returning canned fork/push/PR-create outcomes (success, "already exists", and each failure mode)
**Mock/Stub Requirements:** The injectable seam from AC 02-03 stands in for all three `gh` operations; assertions cover call order (fork → push → pr-create), exactly-once counts, and that a failure at any step short-circuits the remaining steps

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Fork, branch/push, and PR-create each invoked exactly once per successful `submit` run, in that order
- [ ] "Fork already exists" is handled as a non-fatal, expected outcome that proceeds to branch/push
- [ ] Resulting PR URL is printed to stdout on success
- [ ] A failure at any step halts before the next step runs, with an actionable, step-specific error message

**Manual Review:**
- [ ] Code reviewed and approved
