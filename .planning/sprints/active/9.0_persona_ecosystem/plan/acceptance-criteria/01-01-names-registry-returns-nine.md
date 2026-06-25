# Acceptance Criteria: Names Registry Returns Nine

**Related User Story:** [01: Bonus Built-In Domain Personas](../user-stories/01-bonus-built-in-domain-personas.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Persona registry | Go package (`personas`) | `go:embed *.md` + explicit `names` slice |
| Test framework | `go test` / `testing` stdlib | No third-party assertions required |
| Key dependencies | `embed` (stdlib) | Loaded at compile time; no runtime I/O |

## Related Files
- `personas/personas.go` - modify: append `sentinel`, `tracer`, `idiomatic` to the `names` slice (canonical order: after `dax`, before `otto`)
- `personas/personas_test.go` - modify: rename `TestNames_ReturnsAllSix` → `TestNames_ReturnsAllNine` and update expected count from 6 to 9

### Related Files (from codebase-discovery.json)

- `personas/personas.go:16` — embedded persona registry and explicit `names` slice
- `personas/personas_test.go:9` — `TestNames_ReturnsAllSix` to rename and update to 9 names

## Happy Path Scenarios

**Scenario 1: Names() returns exactly 9 entries in canonical order**
- **Given** the `personas` package is compiled with `sentinel.md`, `tracer.md`, and `idiomatic.md` present and the three names appended to the `names` slice
- **When** a caller invokes `personas.Names()`
- **Then** the returned slice has length 9 and equals `["bruce", "greta", "kai", "mira", "dax", "sentinel", "tracer", "idiomatic", "otto"]` in that exact order

**Scenario 2: Get() resolves each bonus persona to a non-empty prompt**
- **Given** the three bonus persona `.md` files are embedded in the binary
- **When** a caller invokes `personas.Get("sentinel")`, `personas.Get("tracer")`, or `personas.Get("idiomatic")`
- **Then** each returns a non-empty string (the rendered system prompt template) and no error

## Edge Cases

**Edge Case 1: Unknown name returns error**
- **Given** the registry contains exactly 9 registered names
- **When** a caller invokes `personas.Get("unknown-persona")`
- **Then** the function returns an empty string and a non-nil error indicating the name is not registered

**Edge Case 2: Duplicate names not introduced**
- **Given** the `names` slice is updated to 9 entries
- **When** the test iterates over `personas.Names()` and deduplicates
- **Then** the deduplicated count equals 9 (no duplicates exist)

**Edge Case 3: Embedded file count does not equal names slice length**
- **Given** `go:embed *.md` may pick up additional unlisted `.md` files placed in `personas/` during development
- **When** `personas.Names()` is called
- **Then** only the 9 names declared in the explicit `names` slice are returned; unlisted embedded files are not exposed

## Error Conditions

**Error Scenario 1: Missing `.md` file for a registered name**
- This would be a compile-time failure — `go:embed` panics at startup if an embedded file is missing; the build fails before any test runs
- Error message: `go:embed: no matching files found`

**Error Scenario 2: `names` slice not updated after adding `.md` files**
- `TestNames_ReturnsAllNine` fails with: `got 6 names, want 9`

## Performance Requirements
- **Response Time:** `personas.Names()` and `personas.Get()` must complete in < 1 ms (in-process slice lookup and template parse; no I/O)
- **Throughput:** No constraint — these are synchronous, read-only registry calls

## Security Considerations
- **Authentication/Authorization:** None — persona registry is a read-only in-process API; no network or filesystem access at runtime
- **Input Validation:** `personas.Get(name)` must reject any name not in the canonical `names` slice; it must not attempt to load arbitrary files by name

## Test Implementation Guidance
**Test Type:** UNIT

**Test Data Requirements:**
- The three bonus `.md` files committed to `personas/`
- No external fixture data needed for this AC (fixture tests live in AC 01-03)

**Mock/Stub Requirements:** None — the package uses `go:embed`; tests run entirely in-process with compiled-in data

**Suggested test structure:**
```go
func TestNames_ReturnsAllNine(t *testing.T) {
    got := personas.Names()
    want := []string{"bruce", "greta", "kai", "mira", "dax", "sentinel", "tracer", "idiomatic", "otto"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("Names() = %v, want %v", got, want)
    }
}

func TestGet_BonusPersonasNonEmpty(t *testing.T) {
    for _, name := range []string{"sentinel", "tracer", "idiomatic"} {
        prompt, err := personas.Get(name)
        if err != nil {
            t.Fatalf("Get(%q) error: %v", name, err)
        }
        if prompt == "" {
            t.Fatalf("Get(%q) returned empty prompt", name)
        }
    }
}
```

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./personas/...`)
- [x] No linting errors (`golangci-lint run ./personas/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `personas.Names()` returns a slice of length 9 matching the canonical order
- [x] `personas.Get()` resolves all three bonus names without error and returns non-empty content
- [x] `TestNames_ReturnsAllNine` exists in `personas_test.go` (old name `TestNames_ReturnsAllSix` is removed)
- [x] No duplicate names in the `names` slice

**Manual Review:**
- [x] Code reviewed and approved
