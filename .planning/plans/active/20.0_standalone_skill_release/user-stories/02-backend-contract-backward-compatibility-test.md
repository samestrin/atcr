# User Story 2: Backend Contract Backward-Compatibility Test

**Plan:** [20.0: Standalone ATCR Skill Distribution](../plan.md)

## User Story

**As a** maintainer of atcr's internal private `.planning/` skills (`execute-code-review`, `reconcile-code-review`)
**I want** a repo-local automated test that locks in the `--output-dir` + reconcile contract documented in `docs/code-review-backend.md`
**So that** public-facing engine changes made for the standalone skill release (and any future atcr work) cannot silently break the private-skill integration without a test failing first

## Story Context

- **Background:** `docs/code-review-backend.md` documents a contract that the internal `.planning/` skills depend on: `atcr review --output-dir "${OUT_DIR}"` followed by `atcr reconcile "${OUT_DIR}"` produces a specific output tree (`sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/summary.json`), and `atcr reconcile`/`atcr verify` share an id-or-path resolution rule (bare id → `.atcr/reviews/<id>/`, path → used as-is, omitted → `.atcr/latest`). This contract was already validated end-to-end from the external `claude-prompts` side by Epic 12.0. This story is the repo-local half: a regression test living in `atcr` itself so the contract cannot drift unnoticed as this plan's dispatcher rewrite (Story 1) and other engine work land.
- **Assumptions:** The underlying engine behavior is already correct and already unit-tested at the component level (`internal/fanout/reviewdir.go`'s `ReviewsRoot` resolution logic). No new engine code is required — this story adds a regression lock, not new functionality. `cmd/atcr/review_test.go` and `cmd/atcr/reconcile_test.go` already establish the exact test conventions (table-driven, `t.Run` subtests, `t.TempDir()` fixtures, `assert`/`require` split, `httptest.NewServer` provider mocks, `os/exec` git fixtures) this new test must follow with no deviation.
- **Constraints:** Must not touch the external `claude-prompts` repository or re-validate its skills — this is scoped strictly to the repo-local contract as documented in `docs/code-review-backend.md`. Must not modify `internal/fanout/reviewdir.go`'s resolution logic — that code already works and is out of scope. Must not introduce a new test harness, fixture DSL, or testing dependency beyond stdlib `testing` + `testify`, which are already in `go.mod`.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `cmd/atcr/*_test.go` file (e.g. `backend_contract_test.go`) exists that runs `atcr review --output-dir` and `atcr reconcile` against fixture data and asserts the exact output tree and id-or-path resolution rules documented in `docs/code-review-backend.md`.
- **Measurable:** `go test ./cmd/atcr/...` (with any required build tag, e.g. `-tags integration`, if the test shells out to a real binary) passes locally and in CI; the test fails if any of `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, or `reconciled/summary.json` goes missing, or if any of the three id-or-path resolution branches (bare id, path, omitted) resolves incorrectly.
- **Achievable:** No new engine code is needed; the test only exercises existing, working, already-unit-tested behavior end-to-end, following the exact patterns already present in `cmd/atcr/review_test.go` and `cmd/atcr/reconcile_test.go`.
- **Relevant:** Directly satisfies AC3 of the plan — proving backward compatibility for the private `.planning/` skill consumers without re-touching the external `claude-prompts` repo.
- **Time-bound:** Completed within this sprint cycle, as a self-contained addition alongside (not blocking) the dispatcher rewrite (Story 1) and `install.sh` (Story 3) work.

## Acceptance Criteria Overview

1. A new test file under `cmd/atcr/` runs the full `atcr review --output-dir` → `atcr reconcile` flow against a fixture git repo and asserts the documented output tree exists with the expected files (`manifest.json`, `payload/`, `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/findings.json`, `reconciled/report.md`, `reconciled/summary.json`).
2. The test exercises all three id-or-path resolution branches shared by `atcr reconcile`/`atcr verify` (bare review id → `.atcr/reviews/<id>/`, explicit path → used as-is, omitted argument → `.atcr/latest`) as table-driven subtests, confirming none has drifted from `docs/code-review-backend.md`.
3. The test uses `httptest.NewServer` to mock provider interaction (zero real network calls) and `os/exec`-based git fixture helpers consistent with existing conventions, so it runs hermetically and repeatably in CI.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/20.0_standalone_skill_release/`_

## Technical Considerations

- **Implementation Notes:** Follow `cmd/atcr/review_test.go` and `cmd/atcr/reconcile_test.go` conventions exactly: `t.TempDir()` for per-test review-directory fixtures, `t.Run` subtests for the table-driven resolution cases, `assert` for the bulk of contract checks (so multiple failures surface in one run), `require` only for critical preconditions (fixture setup succeeding, directory existence) the test cannot proceed without. If the test needs to shell out to a real `go run ./cmd/atcr` binary rather than invoking the cobra command tree in-process (as `execCmd` in `reconcile_test.go` does), gate it with a build tag (e.g. `//go:build integration`) so it does not slow down the default `go test ./...` loop.
- **Integration Points:** `internal/fanout/reviewdir.go` (`ReviewsRoot` / output-directory resolution — read-only dependency, not modified), the `atcr review` and `atcr reconcile` cobra commands in `cmd/atcr/`, and `docs/code-review-backend.md` as the assertion source of truth. If the fan-out/review path needs exercising (not just reconcile), mock provider responses via `net/http/httptest.NewServer` with the registry's `base_url` pointed at `server.URL`, matching the pattern already documented for atcr's provider mocks.
- **Data Requirements:** Fixture git repos built with `os/exec` (`exec.CommandContext(ctx, "git", "-C", repo, ...)`) in a test helper, never through a shell. No external data sources or new fixtures beyond what a minimal git repo + `.atcr/config.yaml` roster requires to drive `atcr review`/`atcr reconcile`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Test becomes flaky if it shells out to a real subprocess binary instead of driving the cobra command tree in-process | Medium | Prefer in-process invocation via `newRootCmd()`/`ExecuteContext`, matching `execCmd` in `reconcile_test.go`; reserve a real subprocess + build tag only if in-process execution cannot exercise the full `--output-dir` write path |
| Test silently duplicates existing unit-test coverage of `internal/fanout/reviewdir.go` without adding regression value at the CLI/contract boundary | Low | Scope assertions to the documented output tree and id-or-path resolution contract as consumed externally (per `docs/code-review-backend.md`), not to `ReviewsRoot`'s internal logic, which is already covered |
| Provider mocking omission causes the test to make real network calls in CI, causing intermittent failures | Medium | Use `httptest.NewServer` for any provider interaction and point the registry's `base_url` at `server.URL`, per established atcr test convention, before landing the test |

---

**Created:** July 11, 2026 01:48:34PM
**Status:** Draft - Awaiting Acceptance Criteria
