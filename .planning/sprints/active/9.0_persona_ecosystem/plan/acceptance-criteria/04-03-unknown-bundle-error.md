# Acceptance Criteria: Unknown Bundle Error Handling

**Related User Story:** [04: Domain Bundle Installation](../user-stories/04-domain-bundles.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Bundle resolver | Go package (`internal/personas/bundles.go`) | Returns typed `ErrUnknownBundle` for unrecognized names |
| CLI error handling | `cmd/atcr/personas.go` | `errors.Is(err, bundles.ErrUnknownBundle)` produces user-facing message |
| Error type | `var ErrUnknownBundle = errors.New("unknown bundle")` | Typed sentinel so callers do not string-match |
| Test framework | `go test` / `testify` | `require.ErrorIs` assertions |

## Related Files
- `internal/personas/bundles.go` - create: `ErrUnknownBundle` sentinel; `Resolve` returns it when name not found in embedded manifests
- `cmd/atcr/personas.go` - modify: `errors.Is` check for `ErrUnknownBundle`, print clear user-facing message and `os.Exit(1)`
- `internal/personas/bundles_test.go` - create: unknown bundle test case asserting `errors.Is(err, ErrUnknownBundle)` and zero persona list returned

### Related Files (from codebase-discovery.json)

- `internal/personas/bundles.go` — create: `ErrUnknownBundle` sentinel and `Resolve`
- `cmd/atcr/personas.go` — modify: `errors.Is` handling for unknown bundles
- `internal/personas/bundles_test.go` — create: unknown bundle test cases

## Happy Path Scenarios

**Scenario 1: Known bundle name succeeds (control case)**
- **Given** `bundle/django` is a registered bundle
- **When** `bundles.Resolve("django")` is called
- **Then** it returns the four-persona list and `nil` error

## Edge Cases

**Edge Case 1: Bundle name with mixed case**
- **Given** the user types `atcr personas install bundle/Django` (capital D)
- **When** the install subcommand calls `bundles.Resolve("Django")`
- **Then** the resolver returns `ErrUnknownBundle` (names are case-sensitive; no normalization); the CLI prints the error and exits 1

**Edge Case 2: Bundle name is empty string**
- **Given** the user types `atcr personas install bundle/`
- **When** the install subcommand calls `bundles.Resolve("")`
- **Then** the resolver returns `ErrUnknownBundle`; CLI prints `unknown bundle: ""` and exits 1

**Edge Case 3: Bundle name contains path traversal**
- **Given** the user types `atcr personas install bundle/../etc/passwd`
- **When** `strings.HasPrefix` detects the `bundle/` prefix and strips it to `../etc/passwd`
- **Then** the resolver returns `ErrUnknownBundle` because `../etc/passwd` is not a registered name; no filesystem access is attempted

## Error Conditions

**Error Scenario 1: Unrecognized bundle name**
- **Given** the user runs `atcr personas install bundle/unknown`
- **When** the install subcommand processes the argument
- **Then** `bundles.Resolve("unknown")` returns `(nil, ErrUnknownBundle)`; the CLI prints a clear error and exits 1; no files are written to `~/.config/atcr/personas/`
- Error message: `unknown bundle: "unknown"`
- Error code: exit status 1

**Error Scenario 2: Partial bundle name (missing slash component)**
- **Given** the user types `atcr personas install bundle`  (no trailing slash, no name)
- **When** `strings.HasPrefix(arg, "bundle/")` is false
- **Then** the install subcommand treats it as a regular persona name (not a bundle), not as a bundle resolution request; behavior falls through to the single-persona install path

## Performance Requirements
- **Response Time:** `bundles.Resolve` for an unknown name must return in < 1 ms (embedded YAML map lookup after initial parse)
- **Throughput:** N/A — single error path, no iteration

## Security Considerations
- **Authentication/Authorization:** Unknown bundle error path must not expose filesystem paths or embedded manifest content in the error message
- **Input Validation:** Bundle name is the raw string after `bundle/` prefix; the resolver's registry lookup is the only gate — no filesystem access occurs for unregistered names

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No external data needed — resolver uses embedded manifests; unknown name is a string literal in the test
**Mock/Stub Requirements:** None — `bundles.Resolve` is a pure function over embedded data

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./internal/personas/... ./cmd/atcr/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `bundles.Resolve("unknown")` returns `errors.Is(err, ErrUnknownBundle) == true` and `nil` persona list
- [x] CLI prints `unknown bundle: "unknown"` and exits 1 when an unrecognized bundle name is given
- [x] No files are written to `~/.config/atcr/personas/` on unknown bundle error
- [x] Path traversal inputs (e.g. `../etc/passwd`) are rejected as unknown bundles without any filesystem access

**Manual Review:**
- [ ] Code reviewed and approved
