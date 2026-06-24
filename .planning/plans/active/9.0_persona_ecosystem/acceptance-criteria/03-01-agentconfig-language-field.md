# Acceptance Criteria: AgentConfig Language Field

**Related User Story:** [03: Language-Aware Skeptic Routing](../user-stories/03-language-aware-skeptic-routing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Config struct | Go package (`internal/registry`) | Add `Language []string` to `AgentConfig` |
| Validation | Go (`validateAgent`) | Reject empty strings and control characters |
| Canonicalization | Go (`applyDefaults`) | Strip leading dot, lowercase, trim whitespace |
| Test Framework | go test / testify | Table-driven unit tests |
| Key Dependencies | `gopkg.in/yaml.v3`, standard `strings` package | |

## Related Files
- `internal/registry/config.go:267` - modify: add `Language []string \`yaml:"language,omitempty"\`` field to `AgentConfig` struct
- `internal/registry/config.go` - modify: update `validateAgent` to reject empty strings and control characters in `Language` entries; update `applyDefaults` to canonicalize each entry (trim whitespace, strip leading dot, lowercase)
- `internal/registry/config_test.go` - modify: add table-driven tests for `Language` field validation and canonicalization

### Related Files (from codebase-discovery.json)

- `internal/registry/config.go:267` — `AgentConfig` struct: add `Language []string`
- `internal/registry/config.go:~625` — `validateAgent`: reject empty/control-character entries
- `internal/registry/config.go:~699` — `applyDefaults`: canonicalize language entries
- `internal/registry/config_test.go` — add validation and canonicalization tests

## Happy Path Scenarios

**Scenario 1: Language field loaded from YAML with dot-prefixed entries**
- **Given** a `registry.yaml` agent entry containing `language: [".Go", " .TS "]`
- **When** the registry loads and `applyDefaults` runs
- **Then** `AgentConfig.Language` equals `["go", "ts"]` (dot stripped, whitespace trimmed, lowercased)

**Scenario 2: Language field loaded from YAML with dotless lowercased entries**
- **Given** a `registry.yaml` agent entry containing `language: ["go", "ts"]`
- **When** the registry loads and `applyDefaults` runs
- **Then** `AgentConfig.Language` equals `["go", "ts"]` unchanged

**Scenario 3: Language field omitted from YAML**
- **Given** a `registry.yaml` agent entry with no `language` key
- **When** the registry loads
- **Then** `AgentConfig.Language` is nil or empty slice and the agent loads without error

## Edge Cases

**Edge Case 1: Language entry is an empty string**
- **Given** a `registry.yaml` agent entry containing `language: ["go", ""]`
- **When** `validateAgent` runs
- **Then** an error is returned identifying the empty string entry

**Edge Case 2: Language entry contains only whitespace**
- **Given** a `registry.yaml` agent entry containing `language: ["  "]`
- **When** `validateAgent` runs after `applyDefaults` trims whitespace
- **Then** the entry becomes an empty string and `validateAgent` rejects it

**Edge Case 3: Language entry contains a control character**
- **Given** a `registry.yaml` agent entry containing `language: ["go\x00"]`
- **When** `validateAgent` runs
- **Then** an error is returned identifying the control character violation

**Edge Case 4: Language is an empty slice**
- **Given** a `registry.yaml` agent entry containing `language: []`
- **When** the registry loads
- **Then** `AgentConfig.Language` is an empty (or nil) slice and no error is returned

## Error Conditions

**Error Scenario 1: Empty string in Language**
- Error message: `"agent %q: language entry at index %d must not be empty"`
- HTTP status / error code: config parse error (non-zero exit)

**Error Scenario 2: Control character in Language entry**
- Error message: `"agent %q: language entry %q contains invalid characters"`
- HTTP status / error code: config parse error (non-zero exit)

## Performance Requirements
- **Response Time:** Config load (including `applyDefaults` + `validateAgent`) must complete within existing config-load budget; the Language canonicalization adds at most one `strings.ToLower` + `strings.TrimSpace` + single-prefix strip per entry — O(entries) with negligible cost.
- **Throughput:** No throughput constraint; config is loaded once at startup.

## Security Considerations
- **Input Validation:** Reject control characters (Unicode category Cc) to prevent injection through YAML-sourced language strings. Limit each entry to reasonable length (no explicit limit required now; reject empty strings at minimum).
- **Authentication/Authorization:** No auth impact; this is a local config struct.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table of YAML snippets with `language` values covering: dotless lowercase, dot-prefixed mixed case, whitespace-padded, empty string, control character, omitted field, empty slice.
**Mock/Stub Requirements:** No mocks needed; test calls `applyDefaults` and `validateAgent` directly with constructed `AgentConfig` values.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/registry/...`)
- [ ] No linting errors (`golangci-lint run ./internal/registry/...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `AgentConfig` has `Language []string \`yaml:"language,omitempty"\`` field in `internal/registry/config.go`
- [ ] `applyDefaults` canonicalizes each Language entry (trim → strip leading dot → lowercase)
- [ ] `validateAgent` rejects empty strings and entries containing control characters
- [ ] Existing registry YAML files without `language` key load without error

**Manual Review:**
- [ ] Code reviewed and approved
