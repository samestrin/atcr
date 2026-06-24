# Acceptance Criteria: Bundle Manifest Parse Validation

**Related User Story:** [04: Domain Bundle Installation](../user-stories/04-domain-bundles.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Manifest struct | Go struct in `internal/personas/bundles.go` | `BundleManifest{Name, Description, Personas []string}` |
| YAML parsing | `gopkg.in/yaml.v3` | `yaml.Unmarshal` into struct; field presence validated post-decode |
| Embedding | `go:embed bundles/*.yaml` | Parsed at call time via `fs.ReadFile` |
| Validation | Pure Go field checks | Missing `name`, empty `personas` list → descriptive error |
| Test framework | `go test` / `testify` | In-memory YAML strings; no disk I/O in unit tests |

## Related Files
- `internal/personas/bundles.go` - create: `BundleManifest` struct, `parseManifest(data []byte) (*BundleManifest, error)` with field validation, `go:embed` directive
- `internal/personas/bundles_test.go` - create: parse validation tests covering valid manifest, missing `name`, missing `personas`, empty `personas` list, malformed YAML

### Related Files (from codebase-discovery.json)

- `internal/personas/bundles.go` — create: `BundleManifest` struct and `parseManifest`
- `internal/personas/bundles_test.go` — create: manifest parse validation tests

## Happy Path Scenarios

**Scenario 1: Valid manifest parses successfully**
- **Given** a YAML file with `name: django`, `description: Django application review panel`, and a four-entry `personas` list
- **When** `parseManifest(data)` is called
- **Then** it returns a `*BundleManifest` with all fields populated and `nil` error

**Scenario 2: Both embedded manifests parse at startup check**
- **Given** `internal/personas/bundles/django.yaml` and `internal/personas/bundles/go-production.yaml` are embedded
- **When** `bundles.Resolve` is called for either name
- **Then** the manifests parse without error and the persona list is returned

## Edge Cases

**Edge Case 1: Manifest with extra unknown fields**
- **Given** a YAML file that includes an unrecognized field `tags: [web, orm]`
- **When** `parseManifest` decodes it
- **Then** the extra field is silently ignored (yaml.v3 default behavior); the manifest is valid if required fields are present

**Edge Case 2: Manifest with a single-entry personas list**
- **Given** a YAML file with `personas: [security/owasp]` (one entry)
- **When** `parseManifest` decodes it
- **Then** it is considered valid; a single-persona bundle is a legal (if unusual) configuration

**Edge Case 3: Manifest with duplicate persona entries**
- **Given** `personas: [security/owasp, security/owasp]`
- **When** `parseManifest` decodes it
- **Then** the manifest parses without error; deduplication is the install loop's responsibility, not the parser's

## Error Conditions

**Error Scenario 1: Missing required `name` field**
- **Given** a YAML manifest with `description` and `personas` but no `name` key
- **When** `parseManifest(data)` is called
- **Then** it returns `nil` and a descriptive error
- Error message: `"bundle manifest missing required field: name"`
- Error code: non-nil `error` return (no panic)

**Error Scenario 2: Empty `personas` list**
- **Given** a YAML manifest with valid `name` and `description` but `personas: []`
- **When** `parseManifest(data)` is called
- **Then** it returns a descriptive error
- Error message: `"bundle manifest \"<name>\" has no personas"`
- Error code: non-nil `error` return

**Error Scenario 3: Malformed YAML (syntax error)**
- **Given** a YAML file containing invalid YAML syntax (e.g., unclosed bracket)
- **When** `parseManifest(data)` is called
- **Then** `yaml.Unmarshal` returns an error; `parseManifest` wraps and returns it without panicking
- Error message: `"failed to parse bundle manifest: <yaml error>"`
- Error code: non-nil `error` return

**Error Scenario 4: Missing `description` field**
- **Given** a manifest with `name` and `personas` but no `description`
- **When** `parseManifest(data)` is called
- **Then** the manifest is considered valid (description is optional); no error is returned

## Performance Requirements
- **Response Time:** `parseManifest` for a typical 10-line manifest must complete in < 1 ms (in-memory YAML decode)
- **Throughput:** Both manifests are parsed per `Resolve` call; total parse time for two manifests must remain under 5 ms

## Security Considerations
- **Authentication/Authorization:** Manifests are embedded at compile time via `go:embed`; no runtime file reads from user-controlled paths
- **Input Validation:** All string fields from YAML are treated as untrusted after parsing — persona identifiers are re-validated against the `[a-z][a-z0-9-]*/[a-z][a-z0-9-]*` format before use in filesystem paths

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** In-memory YAML byte slices for each test case (valid, missing-name, missing-personas, empty-personas, malformed YAML); no disk files needed
**Mock/Stub Requirements:** None — `parseManifest` is a pure function taking `[]byte`

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./internal/personas/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `parseManifest` returns a descriptive error (not a panic) for missing `name`
- [x] `parseManifest` returns a descriptive error for an empty `personas` list
- [x] `parseManifest` wraps and returns yaml.v3 errors for malformed YAML without panicking
- [x] Both embedded manifests (`django.yaml`, `go-production.yaml`) parse successfully in a round-trip test

**Manual Review:**
- [ ] Code reviewed and approved
