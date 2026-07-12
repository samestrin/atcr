# Acceptance Criteria: `atcr debt resolve` CLI Subcommand

**Related User Story:** [03: `/atcr debt resolve` Skill Route](../user-stories/03-atcr-debt-resolve-skill-route.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI subcommand (Cobra) | `cmd/atcr/debt.go` — extends `newDebtCmd()` alongside `list`/`add`/`dashboard` |
| Test Framework | go test (`cmd/atcr/debt_test.go` pattern) | Table-driven Cobra command tests against fixture stores, per `debtSampleShards()` precedent |
| Key Dependencies | `internal/localdebt` (Story 1's `ReadAll`), `spf13/cobra` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/debt.go` — modify: add `newDebtResolveCmd()` and register it in `newDebtCmd()`'s `cmd.AddCommand(...)` alongside `newDebtListCmd()`, `newDebtAddCmd()`, `newDebtDashboardCmd()`
- `cmd/atcr/debt_resolve.go` — create: the new subcommand's flag parsing and `internal/localdebt.ReadAll` invocation, kept in its own file per the existing one-file-per-subcommand convention (`debt_add.go`, `debt_dashboard.go`)
- `cmd/atcr/debt_resolve_test.go` — create: unit tests covering flag parsing, empty-store behavior, and JSON/table output shape
- `cmd/atcr/debt_test.go` — reference (read-only): existing table-driven Cobra command tests and `debtSampleShards()` fixture pattern to mirror
- `internal/localdebt/store.go` — reference (Story 1 dependency): `ReadAll(dir string, opts ReadOpts) ([]Record, error)` is the read API this subcommand calls; not modified by this AC
- `skill/SKILL.md` — reference (read-only): command table and subcommand-discovery convention that this CLI surface must satisfy
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/cli-integration-points.md` — reference: `atcr debt` family extension point and CLI-invocation-only dispatcher contract

## Happy Path Scenarios
**Scenario 1: `atcr debt resolve` lists resolvable items from the local store**
- **Given** `.atcr/debt/2026-07.jsonl` contains one or more `status: open` records written by Story 2's reconcile hook
- **When** a user or the `debt-resolve` skill runs `atcr debt resolve --list` (or the command's default no-flag preview mode)
- **Then** the command reads the store via `internal/localdebt.ReadAll(".atcr/debt", opts)` and prints the candidate items (id, severity, file:line, problem) without requiring any `.planning/` path to exist

**Scenario 2: subcommand is discoverable via `--help`, consistent with SKILL.md's contract**
- **Given** `skill/SKILL.md` states subcommands of `atcr debt` are discovered via `atcr <command> --help`
- **When** `atcr debt --help` is run
- **Then** `resolve` appears in the listed subcommands alongside `list`, `add`, `dashboard`, with a one-line `Short` description distinguishing it as the `.atcr/`-scoped public-store resolver (not the `.planning/`-scoped `list`/`add`/`dashboard` trio)

**Scenario 3: command is CLI-invocation-only, matching the dispatcher's stated contract**
- **Given** `skill/SKILL.md:16,37` states every dispatcher command is a single `atcr` CLI invocation and never a direct engine call
- **When** `skill/debt-resolve/SKILL.md` (AC 03-03/03-04) drives a resolve run
- **Then** it does so by shelling out to `atcr debt resolve` (or its read/mark-resolved variants), never by reading `.atcr/debt/*.jsonl` directly from the agent — resolving the open design decision recorded in `documentation/cli-integration-points.md` in favor of the CLI-subcommand path

## Edge Cases
**Edge Case 1: empty or missing `.atcr/debt/` directory**
- **Given** no `atcr reconcile` run has ever populated `.atcr/debt/`
- **When** `atcr debt resolve --list` is run
- **Then** it prints a "no items" message and exits 0 (matching `internal/localdebt.ReadAll`'s documented `(nil, nil)` contract for a missing directory), never a stack trace or non-zero exit

**Edge Case 2: `.planning/` directory absent from the working tree**
- **Given** the command runs in a repo with zero `.planning/` directory
- **When** `atcr debt resolve` executes any of its subcommands
- **Then** it neither reads nor writes anything under `.planning/`, and never invokes `defaultTDReadme`/`defaultTDItems` (the existing `.planning/`-scoped constants used by `list`/`add`/`dashboard`)

**Edge Case 3: malformed or forward-incompatible records in the store**
- **Given** a `.atcr/debt/*.jsonl` shard contains a line with a `schema_version` newer than this build understands
- **When** `atcr debt resolve` reads the store
- **Then** it surfaces the same tolerant-skip behavior `internal/localdebt.ReadAll` already guarantees (skip with warning, do not abort), and the command's own output does not duplicate that warning logic

## Error Conditions
**Error Scenario 1: `.atcr/debt/` unreadable due to permissions**
- Error message: `atcr debt resolve: failed to read local debt store: <underlying os error>`
- HTTP status / error code: process exit code 1 (non-usage runtime error, matching existing `atcr debt` subcommand error conventions)

**Error Scenario 2: invalid flag value**
- Error message: `Error: invalid argument "<value>" for "--<flag>" flag: <cobra usage error>`
- HTTP status / error code: process exit code 2 (usage error, matching `runDebtList`'s existing `usageError(err)` convention for a bad `--sort` value)

## Performance Requirements
- **Response Time:** Reading and listing a store with up to several thousand records completes in under 1 second on typical developer hardware (bounded by `bufio.Reader` streaming, no full-file buffering)
- **Throughput:** N/A — single-invocation CLI command, not a service

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem CLI tool, no network/auth surface
- **Input Validation:** Flag values (e.g. a `--severity` filter) are validated against the same enum set (`CRITICAL|HIGH|MEDIUM|LOW`) used elsewhere in `cmd/atcr/debt.go`; the store path itself stays rooted under `.atcr/debt/` relative to CWD — no user-supplied path traversal outside the repo root

## Test Implementation Guidance
**Test Type:** UNIT (Cobra command tests against a temp-dir-backed fixture store, following `cmd/atcr/debt_test.go`'s `writeItems`/`debtSampleShards` pattern)
**Test Data Requirements:** A small set of `internal/localdebt.Record` fixtures written to a `t.TempDir()`-rooted `.atcr/debt/YYYY-MM.jsonl`, covering open/resolved/missing-optional-fields cases
**Mock/Stub Requirements:** None — `internal/localdebt` is called directly against real temp files (matching the existing `internal/debt`/`internal/tdmigrate` test style, no filesystem mocking layer in this codebase)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr debt resolve` is registered under `newDebtCmd()` and discoverable via `atcr debt --help`
- [ ] The subcommand reads via `internal/localdebt.ReadAll` and never touches `.planning/`
- [ ] Empty/missing store and malformed-record edge cases are covered by tests without panicking or non-zero exit

**Manual Review:**
- [ ] Code reviewed and approved
