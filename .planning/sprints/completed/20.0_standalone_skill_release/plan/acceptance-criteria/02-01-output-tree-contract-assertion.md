# Acceptance Criteria: Output Tree Contract Assertion

**Related User Story:** [02: Backend Contract Backward-Compatibility Test](../user-stories/02-backend-contract-backward-compatibility-test.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI regression test | `package main` in `cmd/atcr/`, same package as `newRootCmd`/`execCmd` |
| Test Framework | stdlib `testing` + `testify` (`assert`/`require`) | Already in `go.mod`; no new dependency |
| Key Dependencies | `net/http/httptest`, `os/exec` (git), `cmd/atcr` cobra command tree | In-process invocation via `newRootCmd().ExecuteContext`, matching `execCmd` in `reconcile_test.go:62` |

### Related Files (from codebase-discovery.json)

- `cmd/atcr/backend_contract_test.go` — create: new test file (e.g. `TestBackendContract_OutputDirTreeMatchesDocumentedShape`) that runs `atcr review --output-dir <dir> --base <ref> --head <ref>` followed by `atcr reconcile <dir>` in-process against a fixture git repo and mocked provider, then asserts the full output tree from `docs/code-review-backend.md`
- `docs/code-review-backend.md:44-68` — reference: the documented output tree (`manifest.json`, `payload/`, `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/findings.json`, `reconciled/report.md`, `reconciled/summary.json`) the test asserts against verbatim
- `cmd/atcr/review_test.go:235-275` — reference: `writeReviewFixtureConfig`/`initGitRepoWithChange` conventions for scaffolding a fixture repo + registry/project config the new test must follow with no deviation
- `internal/fanout/review_test.go:85-117` — reference: `mockProvider` — the `httptest.NewServer` OpenAI-chat-completions-shaped mock returning a pipe-delimited findings line (`CRITICAL|auth.go:3|...`), the pattern the new test's provider mock must replicate so the review actually produces non-empty findings rather than an empty/failed run
- `cmd/atcr/review_test.go:347-408` — reference: `outputDirFromFlags` tests establishing `--output-dir` resolves to an absolute path and rejects system directories — the flag this AC's happy path drives
- `internal/fanout/reviewdir.go:155` (`ReviewsRoot`) — reference-only: underlying output-directory resolution logic; not modified by this story

## Design References

- [Backward-Compatibility Contract Test Patterns](../documentation/backward-compat-test-patterns.md) — Go stdlib/testify conventions and the reconcile id-or-path resolution contract the new test must follow
- [CLI Dispatcher Conventions](../documentation/cli-dispatcher-conventions.md) — Cobra command/subcommand conventions used by in-process invocation
- [Adversarial Verification Interface](../../../../.planning/specifications/design-concepts/adversarial-verification-interface.md) — documents the id-or-path resolution rules shared by `atcr reconcile` and `atcr verify`

## Happy Path Scenarios
**Scenario 1: Full output-dir + reconcile flow produces the documented tree**
- **Given** a fixture git repo with two commits (base/head) and a `.atcr/config.yaml` roster of one agent whose provider `base_url` points at an `httptest.NewServer` mock returning a valid findings response
- **When** the test runs `atcr review --output-dir "${OUT_DIR}" --base <base> --head <head>` in-process, then `atcr reconcile "${OUT_DIR}"` in-process
- **Then** both commands exit 0, and `${OUT_DIR}` contains `manifest.json`, `payload/`, `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/findings.json`, `reconciled/report.md`, and `reconciled/summary.json` — the always-present core of the tree documented at `docs/code-review-backend.md:44-64` (see Edge Case 3 for the conditionally-produced entries this AC intentionally does not assert)

**Scenario 2: sources/pool/findings.txt carries the documented 8-column shape**
- **Given** the output tree produced in Scenario 1
- **When** the test reads `sources/pool/findings.txt`
- **Then** the first non-comment line begins with the `# atcr-findings/v1` version header, and each finding row has exactly 8 pipe-delimited columns (`SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER`) per `docs/code-review-backend.md:78`

**Scenario 3: reconciled/findings.txt carries the documented 9-column shape**
- **Given** the output tree produced in Scenario 1
- **When** the test reads `reconciled/findings.txt`
- **Then** each finding row has exactly 9 pipe-delimited columns (`...|REVIEWERS|CONFIDENCE`) per `docs/code-review-backend.md:79`

## Edge Cases
**Edge Case 1: reconciled/summary.json exposes the fields a caller surfaces**
- **Given** the output tree produced in Scenario 1
- **When** the test parses `reconciled/summary.json`
- **Then** it decodes with `total_findings`, `sources_scanned`/`per_source_counts`, and `partial` fields present (per `docs/code-review-backend.md:87-99`), even though this story does not assert their exact values (that is covered by existing reconcile unit tests)

**Edge Case 2: --output-dir does not touch .atcr/latest**
- **Given** the flow in Scenario 1 run inside an isolated CWD with no prior review
- **When** the run completes
- **Then** `.atcr/latest` does not exist (per `docs/code-review-backend.md:24-26`: `--output-dir` "does not update `.atcr/latest`"), distinguishing it from the managed-review default path already covered by `TestReconcileCmd_DefaultsToLatest` in `cmd/atcr/reconcile_test.go`

**Edge Case 3: Conditionally-produced tree entries are intentionally NOT asserted**
- **Given** the documented tree at `docs/code-review-backend.md:44-64` also lists `sources/pool/raw/agent/<agent>/`, `reconciled/ambiguous.json`, and `reconciled/disagreements.json`
- **When** this AC's single-agent hermetic fixture runs (one reviewer, no gray-zone clusters, no cross-reviewer severity conflicts)
- **Then** those three entries may be absent because they are produced conditionally (per-agent `raw/` subdir depends on roster shape; `ambiguous.json`/`disagreements.json` require gray-zone clusters / multi-reviewer severity conflicts the single-agent fixture cannot generate) — this AC asserts only the always-present core (Scenario 1) and does NOT assert these three, so their absence is not a contract regression. Their exclusion from the mandatory-assertion set is deliberate, not drift — AC 04-02's doc cross-check must therefore treat them as valid documented-but-unasserted entries and must NOT remove them from `docs/code-review-backend.md`

## Error Conditions
**Error Scenario 1: a missing required output file fails the test with an actionable message**
- If any of `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, or `reconciled/summary.json` is absent, `require.FileExists` (or equivalent) fails the test naming the missing path — this is the regression signal the story exists to produce, not a scenario to suppress
- Error message: standard `require.FileExists` output, e.g. `"<path>": no such file or directory` prefixed with a `require.FileExists(t, path, "docs/code-review-backend.md output tree: <file> missing")`-style custom message
- Exit code / test result: `go test` reports `FAIL` for the subtest; no CLI exit code is involved at this layer

## Performance Requirements
- **Response Time:** The full review+reconcile flow against a two-agent-or-fewer mocked provider and a tiny fixture repo must complete in well under 5 seconds so it does not measurably slow `go test ./cmd/atcr/...`
- **Throughput:** N/A (single sequential fixture run, not a load test)

## Security Considerations
- **Authentication/Authorization:** The mocked provider's `api_key_env` is a fake test-only env var (e.g. `ATCR_TEST_KEY`) set via `t.Setenv`, never a real credential; no real network egress occurs (all HTTP calls terminate at `httptest.NewServer` on `127.0.0.1`)
- **Input Validation:** N/A — the test drives the CLI's own already-validated flag parsing (`--output-dir`, range flags); it does not need to add new input-validation coverage, which is out of scope per the story's constraints

## Test Implementation Guidance
**Test Type:** INTEGRATION (in-process CLI invocation via `newRootCmd()`/`ExecuteContext`, following `execCmd` in `cmd/atcr/reconcile_test.go:62-70`; no external subprocess, no build tag needed unless in-process execution proves unable to exercise the full `--output-dir` write path per the story's stated risk mitigation)
**Test Data Requirements:** A `t.TempDir()` fixture git repo built via `os/exec` git commands (mirroring `initGitRepoWithChange` in `cmd/atcr/review_test.go:254-275`), a `t.TempDir()`-rooted `--output-dir` target, and a minimal `.atcr/config.yaml` + `~/.config/atcr/registry.yaml` pair pointing the roster's one agent at the mock server's `base_url`
**Mock/Stub Requirements:** `httptest.NewServer` returning an OpenAI-chat-completions-shaped JSON response whose `choices[0].message.content` is a valid pipe-delimited findings line (mirroring `mockProvider` in `internal/fanout/review_test.go:87-117`); `isolate(t)` (per `cmd/atcr/reconcile_test.go:52-58`) to chdir into a fresh temp dir and redirect `HOME`/`XDG_CONFIG_HOME` so no real registry or credentials leak in

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr review --output-dir` + `atcr reconcile` run in-process against a fixture repo and mocked provider produce every file listed in `docs/code-review-backend.md`'s output tree
- [ ] `sources/pool/findings.txt` (8 columns) and `reconciled/findings.txt` (9 columns) match the documented column shapes
- [ ] `reconciled/summary.json` decodes with the documented fields present
- [ ] `--output-dir` is confirmed not to write `.atcr/latest`

**Manual Review:**
- [ ] Code reviewed and approved
