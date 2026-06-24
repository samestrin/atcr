# Acceptance Criteria: Search Community Repo Index

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `search` sub-subcommand under `newPersonasCmd()` |
| HTTP fetch | `net/http` stdlib | Fetch `<RegistryBaseURL>/index.json`; injectable `http.Client` |
| JSON parsing | `encoding/json` | Unmarshal index into `[]PersonaIndexEntry{Name, Version, Description, Path}` |
| Output formatting | `fmt.Fprintf` / `text/tabwriter` | Aligned Name + Description columns |
| Test Framework | `go test` / `testify` + `httptest.NewServer` | Deterministic results; no live network calls |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas/search.go`, `internal/personas/client.go` | |

## Related Files
- `internal/personas/search.go` - create: `Search(client, baseURL, keyword string) ([]PersonaIndexEntry, error)` — fetches index.json and filters by keyword
- `internal/personas/client.go` - modify: add `FetchIndex(client, baseURL) ([]PersonaIndexEntry, error)` helper
- `cmd/atcr/personas.go` - modify: add `search` sub-subcommand wired to `personas.Search()`
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasSearch_*` test cases using `httptest.NewServer`

## Happy Path Scenarios

**Scenario 1: Keyword matches one or more personas**
- **Given** the community repo `httptest.NewServer` serves `/index.json` containing entries for `security/owasp`, `security/sans`, and `performance/tracer`
- **When** the user runs `atcr personas search security`
- **Then** stdout prints a table with `security/owasp` and `security/sans` entries (Name + Description); `performance/tracer` is not shown; command exits 0

**Scenario 2: Case-insensitive keyword match**
- **Given** the index contains an entry with description `"OWASP Top-10 security reviewer"`
- **When** the user runs `atcr personas search SECURITY` (uppercase)
- **Then** the entry appears in results; matching is case-insensitive against both name and description fields

**Scenario 3: No matches returns empty result (not an error)**
- **Given** the index contains no entries matching `quantum`
- **When** the user runs `atcr personas search quantum`
- **Then** stdout prints `"No personas found matching \"quantum\""` and the command exits 0

## Edge Cases

**Edge Case 1: Index is empty (`[]` JSON array)**
- **Given** `index.json` returns an empty array
- **When** `atcr personas search security` is run
- **Then** stdout prints `"No personas found matching \"security\""` and the command exits 0

**Edge Case 2: Keyword matches name but not description (and vice versa)**
- **Given** an entry has name `go/idiomatic` and description `"Enforces Go style guide conventions"`
- **When** the user runs `atcr personas search idiomatic`
- **Then** the entry appears (name match); when the user runs `atcr personas search style`, the entry also appears (description match)

**Edge Case 3: Index JSON contains extra unknown fields**
- **Given** `index.json` entries have an additional `tags` field not in `PersonaIndexEntry`
- **When** the index is parsed
- **Then** unknown fields are silently ignored; known fields are populated correctly

## Error Conditions

**Error Scenario 1: Index endpoint returns HTTP 404**
- Error message: `"community repo index not found at <URL>"`
- Exit code: 1

**Error Scenario 2: Index fetch fails (network error or non-2xx)**
- Error message: `"failed to fetch community repo index: <underlying error>"`
- Exit code: 1

**Error Scenario 3: Index JSON is malformed**
- Error message: `"failed to parse community repo index: <json error>"`
- Exit code: 1

## Performance Requirements
- **Response Time:** Search completes in under 500ms against a local `httptest.NewServer`; live network acceptable up to 10 seconds before timeout
- **Throughput:** Single synchronous fetch + in-memory filter; no concurrency requirement

## Security Considerations
- **Authentication/Authorization:** No authentication required for public index endpoint; HTTPS enforced for the default base URL
- **Input Validation:** Keyword is used only for in-memory string matching; it is not interpolated into URLs or file paths, eliminating injection risk

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/search.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** `index.json` fixture with 3–5 entries spanning different categories; empty index fixture; malformed JSON fixture
**Mock/Stub Requirements:** `httptest.NewServer` returning `/index.json`; inject server URL as `RegistryBaseURL` override via env var or constructor parameter

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas search <keyword>` filters by keyword case-insensitively across name and description fields
- [ ] Empty results print a user-friendly message and exit 0
- [ ] HTTP error paths (404, fetch failure, bad JSON) exit non-zero with descriptive error messages
- [ ] No live HTTP calls in CI — all tests use `httptest.NewServer`

**Manual Review:**
- [ ] Code reviewed and approved
