# Acceptance Criteria: CI One-Shot Mode and Example

**Related User Story:** [03: CI Integration](../user-stories/03-ci-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Command | Go (cobra) | `atcr review --fail-on <severity>` one-shot mode |
| CI Script | Bash | examples/ci-gate.sh |
| Documentation | Markdown | README.md CI integration section |
| Test Framework | testify | Integration tests for one-shot review+reconcile flow |

## Related Files
- `cmd/atcr/review.go` - create: `review` cobra command with `--fail-on` flag for one-shot mode
- `cmd/atcr/main.go` - modify: wire one-shot exit-code logic after review+reconcile
- `examples/ci-gate.sh` - create: example CI gate script showing atcr in a pipeline
- `README.md` - modify: add CI integration section documenting `--fail-on` and example usage

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [CLI Architecture](../documentation/cli-architecture.md) — `atcr review --fail-on` shares the same exit-code logic as `atcr reconcile --fail-on`; both flow through centralized `main()` mapping.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — `--fail-on` exit-code gate; severity ordering and threshold semantics.
- [Configuration & Registry](../documentation/configuration-management.md) — `fail_on` field in `.atcr/config.yaml` is the project-level default; `--fail-on` flag overrides it.

### Spec alignment notes

- **One-shot mode is compositional**: `atcr review --fail-on HIGH` runs `atcr range` → `atcr review` → `atcr reconcile` → threshold check, all in one invocation. The Skill uses this for orchestration (see AC 05-03).
- **CI gate script** (`examples/ci-gate.sh`): a thin shell wrapper that runs `atcr review --fail-on HIGH` and surfaces the exit code. The script itself should be ≤20 lines — no business logic, just plumbing.
- **CI provider portability**: GitHub Actions, GitLab CI, and Jenkins all consume the same exit code semantics. No provider-specific glue code lives in atcr.
- **API key handling in CI**: keys come from CI secrets (e.g., `secrets.OPENAI_API_KEY`); `atcr` reads them at invoke time per `original-requirements.md` (never from a config file). The CI workflow is responsible for masking secrets in logs.
- **README CI integration section** should include: (1) the `--fail-on` flag and its semantics, (2) a copy-pasteable GitHub Actions snippet using `examples/ci-gate.sh`, (3) the exit-code table (0/1/2) and what each means.

## Happy Path Scenarios

**Scenario 1: One-shot review with passing findings**
- **Given** a git diff with no critical issues from LLM reviewers
- **When** `atcr review --fail-on HIGH` is executed
- **Then** review runs, reconcile runs, exit code is 0

**Scenario 2: One-shot review with failing findings**
- **Given** a git diff that produces a CRITICAL finding
- **When** `atcr review --fail-on HIGH` is executed
- **Then** review runs, reconcile runs, exit code is 1

**Scenario 3: Separate review then reconcile with fail-on**
- **Given** `atcr review` has been run successfully
- **When** `atcr reconcile --fail-on HIGH` is executed separately
- **Then** exit code reflects threshold check on existing reconciled findings

## Edge Cases

**Edge Case 1: One-shot review with API key missing**
- **Given** LLM API key environment variable is not set
- **When** `atcr review --fail-on HIGH` is executed
- **Then** exit code is 2 with message: `API key env var not set: <VAR_NAME>`

**Edge Case 2: One-shot review with no diff**
- **Given** an empty git diff (no changes)
- **When** `atcr review --fail-on HIGH` is executed
- **Then** review completes with no findings, exit code is 0

## Error Conditions

**Error Scenario 1: API key not available**
- Error message: `API key env var not set: <VAR_NAME>`
- Exit code: 2

**Error Scenario 2: Review fails mid-pipeline**
- Error message: `review failed: <reason>`
- Exit code: 2 (does not reach reconcile/exit-code check)

## Performance Requirements
- **Response Time:** One-shot mode completes within global timeout (configurable via context)
- **Throughput:** CI gate script exits immediately on threshold violation

## Security Considerations
- **Authentication:** API key read from environment variable only, never hardcoded
- **Input Validation:** `--fail-on` flag validated before API calls in one-shot mode

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Fixture diff files, mock LLM responses returning findings of various severities
**Mock/Stub Requirements:** Mock LLM API calls to return predetermined findings; mock git diff

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr review --fail-on <severity>` runs review + reconcile + exit-code check in one command
- [ ] examples/ci-gate.sh script exists and is executable
- [ ] README.md documents CI integration with `--fail-on` flag and exit codes
- [ ] CI example works in GitHub Actions syntax

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] CI example script tested manually in a CI-like environment
