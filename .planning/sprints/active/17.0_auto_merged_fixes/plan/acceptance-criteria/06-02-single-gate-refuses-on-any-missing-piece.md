# Acceptance Criteria: Single Gate Refuses the Entire `--auto-fix` Run on Any Missing/Malformed Backend Piece

**Related User Story:** [06: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate](../user-stories/06-opt-in-auto-fix-flag-with-refuse-without-backend-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go gate function (`cmd/atcr`), config-shape validation only (no live network/preflight call) | Mirrors `resolveExec` (`cmd/atcr/verify.go:41-58`) and `ResolveExecBackend`'s config-shape step (`internal/verify/exec.go:24-45`), reusing `usageError`/`envOr`/`parseRepo` (`cmd/atcr/github.go:66-82`) |
| Test Framework | Go `testing` + `testify/require`; in-process cobra execution via a shared `execCmdCapture`-style harness (`cmd/atcr/verify_test.go:21-35`) | Table-driven over the three independently-missing-piece cases |
| Key Dependencies | None beyond existing `cmd/atcr` helpers (`usageError`, `envOr`, `parseRepo`) and Story 2's validation-command config accessor | No new dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/autofix.go` - create: `validateAutoFixBackend(cmd *cobra.Command, proj *registry.ProjectConfig) error` — the single gate function checking, in one pass, (1) apply target, (2) Story 2's validation-command config, (3) Stories 4/5's GitHub token/repo shape via `envOr`/`parseRepo`
- `cmd/atcr/autofix_test.go` - create: table-driven tests, one case per independently-missing piece (missing validation command, missing/empty `GITHUB_TOKEN`, malformed `--repo` slug) plus a combined "apply target absent" case, each asserting exit code 2 and a message naming the specific missing piece
- `cmd/atcr/review.go` - modify: call `validateAutoFixBackend` at the very top of the `--auto-fix` code path, strictly before any `internal/autofix` apply call (per the story's Implementation Notes) — mirrors how `resolveExec` is invoked after config load but before any review/verify stage work (`cmd/atcr/review.go:157-161`)
- `cmd/atcr/github.go` - reference: `envOr` (line 66-72) and `parseRepo` (line 74-82) are reused as-is, not duplicated, for GitHub token/repo shape resolution
- `internal/verify/exec.go` - reference: `ErrExecNoBackend` sentinel-error style (line 16) is the precedent this AC's own sentinel/wrapped errors follow for a consistent "refuses without backend" vocabulary across `--exec` and `--auto-fix`

## Happy Path Scenarios
**Scenario 1: Missing validation command produces a usage error naming that piece**
- **Given** `--auto-fix` is passed with GitHub token/repo and an apply target both present and valid, but Story 2's validation-command config is absent from `.atcr/config.yaml`
- **When** `validateAutoFixBackend` runs
- **Then** it returns a `usageError` (exit 2) whose message names the validation-command piece specifically (e.g. `"--auto-fix requires a validation command configured (see .atcr/config.yaml)"`), and no apply/validate/revert/GitHub call is attempted

**Scenario 2: Missing GitHub token produces a usage error naming that piece**
- **Given** `--auto-fix` is passed with a valid apply target and validation command, but neither `--token` nor `GITHUB_TOKEN` is set
- **When** `validateAutoFixBackend` runs
- **Then** it returns a `usageError` (exit 2) whose message names the token piece (mirroring `runGithub`'s existing `"a GitHub token is required (pass --token or set GITHUB_TOKEN)"` at `cmd/atcr/github.go:106`), and no file on disk is touched

**Scenario 3: Malformed `--repo` slug produces a usage error naming that piece**
- **Given** `--auto-fix` is passed with `--repo notaslug` (or `GITHUB_REPOSITORY=notaslug`) that does not parse as `owner/name`
- **When** `validateAutoFixBackend` calls `parseRepo` on the resolved value
- **Then** it returns a `usageError` (exit 2) surfacing `parseRepo`'s own error (`"--repo must be owner/name, got %q"`, `cmd/atcr/github.go:79`), and no network call is attempted

## Edge Cases
**Edge Case 1: Multiple pieces missing simultaneously**
- **Given** both the validation command AND the GitHub token are absent
- **When** `validateAutoFixBackend` runs
- **Then** the gate still fails with exit 2 (all-or-nothing per the story's Constraints) — when more than one backend piece is missing/malformed the message AGGREGATES and names EVERY missing/invalid piece (not just the first one checked), e.g. `"--auto-fix cannot run: validation command not configured (see .atcr/config.yaml); a GitHub token is required (pass --token or set GITHUB_TOKEN)"`, and it never reports success or attempts a partial run

**Edge Case 2: Apply target missing or unreadable**
- **Given** the configured apply target (working tree path `internal/autofix` will patch) does not exist or is not a directory
- **When** `validateAutoFixBackend` runs
- **Then** it returns a `usageError` (exit 2) naming the apply-target piece, before any of Story 1's file operations are attempted

**Edge Case 3: Gate runs before Story 1 touches any file, even under a partially-successful prior run**
- **Given** a prior interrupted `--auto-fix` run left `.bak` files or a partially-applied patch on disk
- **When** a fresh `--auto-fix` invocation with a missing backend piece runs
- **Then** `validateAutoFixBackend` still refuses immediately — it does not inspect or attempt to resume prior run state, since the gate's only job is backend-config shape validation, not run-state recovery

**Edge Case 4: Config-shape check does not perform a live GitHub API call**
- **Given** a syntactically valid but actually-revoked GitHub token
- **When** `validateAutoFixBackend` runs
- **Then** the gate passes (shape is valid) — a revoked-token failure surfaces later as a normal Story 4/5 runtime error, not as this gate's responsibility (per the story's first Potential Risk and its Assumptions section)

## Error Conditions
**Error Scenario 1: Validation command missing**
- Error message: `"--auto-fix requires a validation command configured (see .atcr/config.yaml)"` — names the specific missing piece ("validation command") and the config source to fix it (`.atcr/config.yaml`), mirroring the operator-facing style of the two verbatim-reused messages in Scenarios 2 and 3
- HTTP status / error code: process exit code 2 (`usageError`, `cmd/atcr/main.go:98-101`)

**Error Scenario 2: GitHub token missing/empty**
- Error message: `"a GitHub token is required (pass --token or set GITHUB_TOKEN)"` (reused verbatim from `cmd/atcr/github.go:106`)
- HTTP status / error code: process exit code 2

**Error Scenario 3: Malformed `--repo` slug**
- Error message: `"--repo must be owner/name, got %q"` (reused verbatim from `parseRepo`, `cmd/atcr/github.go:79`)
- HTTP status / error code: process exit code 2

**Error Scenario 4: Apply target missing**
- Error message: `"--auto-fix requires a valid apply target: working tree path %q not found or not a directory (set apply.target in .atcr/config.yaml)"` — names the specific invalid piece (the apply-target path) and the config source to fix it (`apply.target` in `.atcr/config.yaml`), mirroring the operator-facing style of the two verbatim-reused messages in Scenarios 2 and 3
- HTTP status / error code: process exit code 2

## Performance Requirements
- **Response Time:** The gate performs only local checks (env var reads, flag reads, config-shape parsing, a filesystem stat on the apply target) — no network I/O — so it completes in low single-digit milliseconds, matching `resolveExec`'s config-shape step before its separate live `Preflight` call
- **Throughput:** One gate evaluation per `--auto-fix` invocation; not called repeatedly within a run

## Security Considerations
- **Authentication/Authorization:** The gate never transmits the resolved GitHub token over the network — it only checks presence/non-emptiness, following `runGithub`'s existing non-logging handling of `token` (`cmd/atcr/github.go:103-107`)
- **Input Validation:** `--repo` shape is validated via the existing, already-hardened `parseRepo` (no new parsing logic); the apply-target path is validated via `os.Stat`, not shell-interpreted, avoiding path-injection risk

## Test Implementation Guidance
**Test Type:** UNIT (gate function called directly with constructed `*registry.ProjectConfig`/flag values) + INTEGRATION (full `atcr review --auto-fix ...` invocation via the `execCmdCapture`-style harness, asserting exit code 2 and message content, mirroring `TestVerifyCmd_ExecRefusesWithoutSandbox`, `cmd/atcr/verify_test.go:127-136`)
**Test Data Requirements:** Table-driven cases: (a) missing validation command only, (b) missing `GITHUB_TOKEN` only, (c) malformed `--repo` only, (d) missing apply target only, (e) all three missing at once — each asserting exit code 2 and that no file is created/modified and no HTTP call is attempted (verify via a `httptest.Server` that fails the test if hit, or simply absence of any `ghaction.Client` construction); the combined case (e) additionally asserts the aggregated message names EVERY missing piece (validation command, GitHub token, and apply target all appear), not just the first one checked
**Mock/Stub Requirements:** No live GitHub API mock needed (shape-only check); a temp-dir fixture for the apply-target existence check; `.atcr/config.yaml` fixtures with/without the validation-command block, following `writeVerifyRegistry`'s pattern (`cmd/atcr/verify_test.go`)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Each of the three required pieces (apply target, validation command, GitHub token/repo shape), when independently missing or malformed, produces a usage error (exit 2) naming that specific piece, verified by a dedicated test per piece
- [ ] The gate is all-or-nothing: any single missing/malformed piece refuses the entire run — no test observes a partial execution (e.g. apply-and-validate-but-skip-PR)
- [ ] The gate performs no live network call (no GitHub API round-trip, no validation-command execution) — verified by a test that fails if any HTTP request or subprocess exec occurs during gate evaluation
- [ ] `validateAutoFixBackend` runs strictly before any `internal/autofix` apply call, verified by an integration test asserting zero filesystem mutation when the gate fails

**Manual Review:**
- [ ] Code reviewed and approved
