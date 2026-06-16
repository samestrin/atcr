# Acceptance Criteria: Anonymization Pass — PII Stripping

**Related User Story:** [04: Export Public Leaderboard Submission](../user-stories/04-export-public-leaderboard.md)

## Acceptance Criteria Statement
The export anonymization pass transforms each internal scorecard record into a `PublicRecord` containing only the v1 allowlist fields, stripping `run_id`, file paths, provider API keys, hostnames, usernames, and repository identifiers.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Package | `internal/scorecard` | `export.go` — `AnonymizeRecord` function |
| Data Model | Go structs | `PublicRecord` (allowlist schema) vs `ScorecardRecord` (internal) |
| Serialization | `encoding/json` (stdlib) | Allowlist-based field copy, not denylist |
| Test Framework | `go test` + `testify/assert` | Regex-based PII pattern detection in output |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/export.go` — create: `AnonymizeRecord(raw ScorecardRecord) PublicRecord`
- `internal/scorecard/scorecard.go` — reference: `ScorecardRecord` struct definition
- `internal/scorecard/export_test.go` — create: tests asserting PII absence and metric preservation
- `internal/scorecard/store.go` — reference: record loading for integration verification

## Happy Path Scenarios

**Scenario 1: AnonymizeRecord strips run_id and replaces with sequential index**
- **Given** a `ScorecardRecord` with `run_id` set to `"2026-06-15T10:00:00Z-abc123"`
- **When** `AnonymizeRecord` is called and the result is placed in the export records array
- **Then** the public record has no `run_id` field; instead it has an `index` field set to its sequential position (0, 1, 2, ...) in the sorted output array

**Scenario 2: Export output contains no file system paths**
- **Given** aggregated records where internal fields contain paths like `/Users/sam/projects/myrepo` or `~/.config/atcr/`
- **When** `atcr leaderboard --export` is executed
- **Then** the output JSON contains zero strings matching file system path patterns (absolute paths starting with `/`, home directory references with `~`, Windows drive letters)

**Scenario 3: Export output contains no provider API keys**
- **Given** internal records that may have had API key references in metadata fields
- **When** `atcr leaderboard --export` is executed
- **Then** the output JSON contains zero strings matching common API key patterns (e.g., `sk-ant-...`, `sk-...`, `ghp_...`, `xoxb-...`, base64 blobs > 32 chars)

**Scenario 4: Export output contains no hostnames or usernames**
- **Given** internal records with `hostname: "dev-machine.local"` or `user: "sam"`
- **When** `atcr leaderboard --export` is executed
- **Then** these fields are absent from the output JSON; no hostname or username values appear anywhere in the serialized document

**Scenario 5: Export output contains no repository identifiers**
- **Given** internal records with `repo: "github.com/myorg/myrepo"` or `organization: "myorg"`
- **When** `atcr leaderboard --export` is executed
- **Then** `repo`, `organization`, and any related fields are absent from the output; no repository URLs or org names appear

**Scenario 6: Allowlist-based schema drops unknown fields**
- **Given** a `ScorecardRecord` with an unexpected field `secret_notes: "contains sensitive info"`
- **When** `AnonymizeRecord` is called
- **Then** the `PublicRecord` does not contain `secret_notes`; only fields explicitly defined in the v1 public schema are present

## Edge Cases

**Edge Case 1: Record with all PII fields populated**
- **Given** a record where every PII-carrying field (`run_id`, `repo`, `path`, `organization`, `hostname`, `user`) is populated
- **When** `AnonymizeRecord` is called
- **Then** all PII fields are stripped; only `reviewer`, `model`, `role`, numeric metrics, and the assigned `index` remain

**Edge Case 2: Record with empty PII fields**
- **Given** a record where PII fields are already empty strings or zero values
- **When** `AnonymizeRecord` is called
- **Then** the anonymization pass completes without error; output is identical to what it would be for a clean record

**Edge Case 3: Multiple records from same run get distinct indices**
- **Given** 3 records from the same `run_id` (different reviewers)
- **When** all 3 are exported together
- **Then** each receives a unique sequential `index` (0, 1, 2) based on sort order; no two records share the same index

## Error Conditions

**Error Scenario 1: Anonymization function receives nil record**
- **Given** a nil `ScorecardRecord` pointer
- **When** `AnonymizeRecord` is called
- **Then** the function returns a zero-value `PublicRecord` or panics with a clear message (depending on project convention for nil inputs)

## Performance Requirements
- **Throughput:** Anonymization of 10,000 records completes in < 500ms (field copy is O(1) per record)
- **Memory:** No additional allocations beyond the `PublicRecord` structs themselves

## Security Considerations
- **Allowlist strategy:** The v1 public schema defines exactly which fields are permitted. Any field not in the schema is dropped by default. This is safer than a denylist, which risks missing new PII fields added in future internal schema changes.
- **No PII in serialized output:** The final JSON must pass a regex sweep for: file paths (`/`, `~`), API key prefixes (`sk-`, `ghp_`, `xoxb-`), email patterns, hostname patterns. This is verified in unit tests.
- **Deterministic anonymization:** No random salts, no UUIDs, no timestamps injected per-record. Same input always produces the same anonymized output.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- `ScorecardRecord` fixtures with all PII fields populated (paths, hostnames, usernames, repo URLs, org names, API-key-like strings)
- Expected `PublicRecord` fixtures showing only permitted fields

**Mock/Stub Requirements:**
- No mocks needed; `AnonymizeRecord` is a pure function
- Use `regexp.MatchString` in test assertions to verify no PII patterns leak into marshaled JSON output
- Test patterns: `^/`, `^~`, `sk-ant-`, `sk-`, `ghp_`, `xoxb-`, `@`, `\w+\.\w+\.\w+` (hostname-like)

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/scorecard/...` passes, including PII sweep tests
- [x] `go vet ./internal/scorecard/...` clean
- [x] Test assertion: marshaled export JSON contains zero matches for path, API key, hostname, and email regex patterns

**Story-Specific:**
- [x] `AnonymizeRecord` function exists in `internal/scorecard/export.go`
- [x] `PublicRecord` struct defines only v1 schema fields (allowlist)
- [x] `run_id` is stripped from public output; replaced with sequential `index`
- [x] Fields `repo`, `path`, `organization`, `hostname`, `user` are absent from `PublicRecord`
- [x] Unknown/internal fields are not copied to `PublicRecord`

**Manual Review:**
- [x] Code reviewed and approved
- [x] Allowlist approach confirmed (not denylist)
- [x] PII regex patterns in tests cover common secrets formats
