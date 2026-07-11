# Acceptance Criteria: Hermetic Provider Mocking and Git Fixture Isolation

**Related User Story:** [02: Backend Contract Backward-Compatibility Test](../user-stories/02-backend-contract-backward-compatibility-test.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI regression test (test infrastructure/helpers) | `package main` in `cmd/atcr/` |
| Test Framework | stdlib `testing` + `testify` (`assert`/`require`) | `httptest.NewServer` for provider mocks, `os/exec` for git fixtures |
| Key Dependencies | `net/http/httptest`, `os/exec` (`exec.CommandContext`), `t.TempDir()`, `t.Setenv` | No shell invocation anywhere; no new test harness or fixture DSL per story constraints |

## Related Files
- `cmd/atcr/backend_contract_test.go` - create/modify: the helper functions backing AC 02-01 and AC 02-02 (mock provider server, git fixture repo builder) must satisfy this AC's hermeticity requirements.
- `internal/fanout/review_test.go:85-117` - reference: `mockProvider` — the canonical `httptest.NewServer` pattern this story's mock must replicate (registry `base_url` pointed at `server.URL`, zero real network).
- `cmd/atcr/review_test.go:254-275` - reference: `initGitRepoWithChange` — the canonical `os/exec`-based git fixture pattern (`exec.Command("git", args...)`, never through a shell) with pinned `GIT_AUTHOR_*`/`GIT_COMMITTER_*` env vars for deterministic commits.
- `cmd/atcr/reconcile_test.go:52-58` - reference: `isolate(t)` — chdirs into a fresh temp working dir and points `HOME`/`XDG_CONFIG_HOME` at another temp dir, so no real registry or repo state leaks into the test.
- `docs/code-review-backend.md:34-42` (Pre-flight) - reference: the `.atcr/config.yaml` + registry pre-flight the fixture setup must satisfy so `atcr review` does not hard-fail before the mock provider is ever reached.

## Happy Path Scenarios
**Scenario 1: The full backend contract test suite makes zero real network calls**
- **Given** all provider interaction is mocked via `httptest.NewServer` with the registry's `base_url` pointed at `server.URL`
- **When** `go test ./cmd/atcr/... -run TestBackendContract` runs in an environment with network access blocked (or simply observed via the mock server's request count)
- **Then** every HTTP request the review step makes lands on the local `httptest.NewServer` instance, and the test suite passes identically offline

**Scenario 2: Git fixtures are built via `os/exec`, never a shell**
- **Given** a fixture repo helper analogous to `initGitRepoWithChange`
- **When** the test constructs the fixture repo
- **Then** every git invocation uses `exec.Command("git", ...)` or `exec.CommandContext(ctx, "git", "-C", repo, ...)` with an explicit argument slice — no `sh -c`, no string-interpolated shell command — matching the story's stated constraint

**Scenario 3: Test isolation prevents cross-test and cross-machine contamination**
- **Given** `isolate(t)` (or an equivalent helper) chdirs into `t.TempDir()` and redirects `HOME`/`XDG_CONFIG_HOME`
- **When** the backend contract test runs alongside the rest of the `cmd/atcr` package's tests (`go test ./cmd/atcr/...`, parallel or sequential)
- **Then** no test reads or writes a real developer's `~/.config/atcr/registry.yaml`, and repeated runs on a clean CI checkout and a dirty local dev machine produce identical results

## Edge Cases
**Edge Case 1: Provider mock returns a failure for one agent while the roster has more than one**
- **Given** a roster of two agents where the mock server returns HTTP 500 for one agent's model (per the `failModels` pattern in `internal/fanout/review_test.go:87-117`)
- **When** the review step runs
- **Then** the test can assert `partial: true` in `manifest.json`/`sources/pool/summary.json` without ever making a real network call, confirming the mock's failure-injection path is itself hermetic

**Edge Case 2: Fixture git repo has no `.gitignore` for `.atcr/`**
- **Given** the fixture repo helper does not commit a `.atcr/`-ignoring `.gitignore` (unlike `internal/fanout/engine_e2e_test.go:50`, which commits one to keep its worktree-snapshot assertions clean)
- **When** the backend contract test's review step scaffolds `.atcr/reviews/...` inside the fixture repo
- **Then** this AC only requires the test itself remains hermetic (no real I/O outside `t.TempDir()`); it does not require replicating the `.gitignore` convention unless the specific test also asserts on git worktree cleanliness (out of scope for a reconcile/output-dir contract test)

## Error Conditions
**Error Scenario 1: A test accidentally reaches a real provider URL**
- If a helper is miswired (e.g. `base_url` left at a real provider default instead of `server.URL`), the request either times out against a real endpoint or is rejected by that endpoint's real auth, and the test fails with a provider-call error rather than the expected mocked findings content
- Error message: surfaces as an `assert`/`require` failure on the missing/incorrect findings content, or a CI network-egress denial if the sandbox blocks outbound calls — either signal is acceptable proof the hermeticity contract was violated
- HTTP status / error code: N/A (this is a test-authoring defect, not a product error path)

**Error Scenario 2: A git fixture helper shells out via a string command**
- Not directly testable at runtime, but this AC's Definition of Done requires a code-review check (Manual Review section) confirming no `exec.Command("sh", "-c", ...)` or equivalent appears in the new test file, per the story's explicit constraint (`os/exec` git fixtures "never through a shell")

## Performance Requirements
- **Response Time:** Hermetic execution (local mock server, local temp-dir git repo) means the full backend contract suite must run in well under 5 seconds total, with no dependency on external network latency
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** Fake test-only API key env vars only (e.g. `ATCR_TEST_KEY` via `t.Setenv`); no real credentials ever touch the test process
- **Input Validation:** N/A — this AC is about test infrastructure hermeticity, not input validation of product code

## Test Implementation Guidance
**Test Type:** INTEGRATION (test-infrastructure verification woven into AC 02-01's and AC 02-02's test bodies; this AC does not require a separate standalone test function, but the helpers it constrains are shared by both)
**Test Data Requirements:** N/A beyond what AC 02-01/02-02 already require — this AC constrains *how* those fixtures are built, not new data
**Mock/Stub Requirements:** `httptest.NewServer` per `internal/fanout/review_test.go:85-117`; `t.Cleanup(srv.Close)` to avoid leaking listening sockets across the test run

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] No test in `cmd/atcr/backend_contract_test.go` makes a real network call — every provider interaction goes through `httptest.NewServer`
- [ ] All git fixture setup uses `os/exec` with an explicit argument slice, never a shell string
- [ ] Tests are isolated via `isolate(t)` (or equivalent) so no real `HOME`/`XDG_CONFIG_HOME`/registry state leaks in or out
- [ ] Suite runs identically offline and in CI with no flakiness from real network dependency

**Manual Review:**
- [ ] Code reviewed and approved
