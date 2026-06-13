# Acceptance Criteria: Path Jail Escape Vector Rejection

**Related User Story:** [03: Path Jail & Snapshot Sandbox](../user-stories/03-path-jail-sandbox.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Path Jail | Go struct `Jail` in `internal/tools/jail.go` | Enforces all escape vector checks |
| Resolution Pipeline | `filepath.Clean`, `filepath.EvalSymlinks`, `strings.HasPrefix` | stdlib only, no third-party deps |
| Error Type | `JailError` struct implementing `error` | Structured error with `Path` and `Reason` fields |
| Test Framework | `go test` with `testing` package | Table-driven tests for all vectors |
| Fixture Support | `os.MkdirTemp`, `os.Symlink` | Creates test filesystems for symlink testing |

### Related Files (from codebase-discovery.json)

- `internal/tools/jail.go` - create: `Jail` struct with `NewJail(root string)` constructor and `Resolve(relPath string) (string, error)` method
- `internal/tools/jail_test.go` - create: comprehensive table-driven tests covering all escape vectors
- `internal/tools/dispatch.go` - create: tool dispatcher that calls `jail.Resolve()` before any filesystem I/O
- `internal/tools/dispatch_test.go` - create: integration tests verifying dispatcher rejects paths before tool execution
- `internal/registry/persona.go:130` - reference: existing `readNonEmpty` symlink-refusal pattern used as security precedent

## Happy Path Scenarios

**Scenario 1: Valid relative path resolves to an absolute path under the snapshot root**
- **Given** a Jail initialized with root `/tmp/snapshot-abc123` containing file `src/main.go`
- **When** `Resolve("src/main.go")` is called
- **Then** it returns the absolute path `/tmp/snapshot-abc123/src/main.go` with no error

**Scenario 2: Root-relative path to existing file**
- **Given** a Jail initialized with root `/tmp/snapshot-abc123` containing file `README.md`
- **When** `Resolve("README.md")` is called
- **Then** it returns `/tmp/snapshot-abc123/README.md` with no error

**Scenario 3: Path with internal `..` that stays within root**
- **Given** a Jail initialized with root `/tmp/snapshot-abc123` containing `src/sub/deep.go`
- **When** `Resolve("src/sub/../../src/main.go")` is called
- **Then** `filepath.Clean` collapses it to `src/main.go` and resolution succeeds with `/tmp/snapshot-abc123/src/main.go`

**Scenario 4: `.gitignore` at root is readable**
- **Given** a Jail initialized with root containing `.gitignore` at the root level
- **When** `Resolve(".gitignore")` is called
- **Then** it returns the resolved path with no error (`.git` component check does not match `.gitignore`)

**Scenario 5: `.github/workflows/ci.yml` is readable**
- **Given** a Jail initialized with root containing `.github/workflows/ci.yml`
- **When** `Resolve(".github/workflows/ci.yml")` is called
- **Then** it returns the resolved path with no error (`.github` is not `.git` as a path component)

## Edge Cases

**Edge Case 1: Absolute path is rejected**
- **Given** a Jail initialized with any root
- **When** `Resolve("/etc/passwd")` is called
- **Then** it returns a `JailError{Path: "/etc/passwd", Reason: "absolute path not allowed"}` without touching the filesystem

**Edge Case 2: `..` traversal escaping root is rejected**
- **Given** a Jail initialized with root `/tmp/snapshot-abc123`
- **When** `Resolve("../../secrets")` is called
- **Then** after `filepath.Clean`, the candidate resolves outside root, and a `JailError{Path: "../../secrets", Reason: "path escapes snapshot root"}` is returned

**Edge Case 3: Symlink pointing outside root is rejected**
- **Given** a Jail root containing symlink `link -> /etc/passwd`
- **When** `Resolve("link")` is called
- **Then** `filepath.EvalSymlinks` resolves to `/etc/passwd`, which fails the prefix check, returning `JailError{Path: "link", Reason: "symlink target escapes snapshot root"}`

**Edge Case 4: Symlink to parent directory is rejected**
- **Given** a Jail root containing symlink `escape -> ..`
- **When** `Resolve("escape/secrets")` is called
- **Then** `filepath.EvalSymlinks` resolves the full path, which escapes root, returning `JailError{Reason: "symlink target escapes snapshot root"}`

**Edge Case 5: Path targeting `.git/config` is rejected**
- **Given** a Jail initialized with root containing `.git/config`
- **When** `Resolve(".git/config")` is called
- **Then** path component check detects `.git` and returns `JailError{Path: ".git/config", Reason: "access to .git directory not allowed"}`

**Edge Case 6: Path targeting `.git/objects/ab/cdef...` is rejected**
- **Given** a Jail initialized with root containing `.git/objects/`
- **When** `Resolve(".git/objects/ab/cdef1234")` is called
- **Then** path component check detects `.git` and returns `JailError{Path: ".git/objects/ab/cdef1234", Reason: "access to .git directory not allowed"}`

**Edge Case 7: Empty path is rejected**
- **Given** a Jail initialized with any root
- **When** `Resolve("")` is called
- **Then** it returns `JailError{Path: "", Reason: "empty path not allowed"}` without filesystem access

**Edge Case 8: Path with embedded NUL byte is rejected**
- **Given** a Jail initialized with any root
- **When** `Resolve("src/main\x00.go")` is called
- **Then** it returns `JailError{Path: "src/main\\x00.go", Reason: "path contains NUL byte"}` without filesystem access

**Edge Case 9: `foo.git/bar` is allowed (not a false positive)**
- **Given** a Jail root containing directory `foo.git` with file `bar`
- **When** `Resolve("foo.git/bar")` is called
- **Then** it resolves successfully (`.git` component match requires exact component, not substring)

**Edge Case 10: Submodule path returns directory error**
- **Given** a Jail root containing a gitlink (submodule entry) at `vendor/dep`
- **When** `Resolve("vendor/dep")` is called
- **Then** path resolution succeeds but the tool layer returns error "is a directory" (submodule content not recursively readable)

## Error Conditions

**Error Scenario 1: Structured error contains path and reason**
- Every `JailError` must include both `Path` (the original input, not the resolved path) and `Reason` (one of the defined rejection reasons)
- Error message format: `"path jail: [Reason]: [Path]"`

**Error Scenario 2: JailError implements error interface**
- `JailError` must satisfy the `error` interface via `Error() string` method
- Callers can use `errors.As(err, &jailErr)` to extract structured fields

## Performance Requirements

- **Response Time:** `Resolve()` must complete in <1ms for typical paths (< 5 segments) on a warm filesystem
- **Throughput:** Must support >1000 resolves/second without contention (no global locks; Jail is safe for concurrent reads)
- **Fast Path:** When no symlinks are involved, `EvalSymlinks` overhead must be minimal (single `Lstat` per component)

## Security Considerations

- **Path validation is structural, not advisory:** The jail is enforced in code, not by prompt instruction to the LLM. A malformed tool-call argument cannot bypass `Resolve()`.
- **Symlink resolution uses `EvalSymlinks`:** This follows the full symlink chain, preventing multi-hop escapes.
- **`.git` matched as path component:** Split on `os.PathSeparator` and check for exact `.git` match. This prevents both false negatives (missing `.git/`) and false positives (blocking `.gitignore`).
- **NUL byte rejection:** Prevents C-string truncation attacks where `"src/main\x00.go"` would be interpreted as `"src/main"` by underlying syscalls.
- **No TOCTOU in v1 threat model:** The snapshot root's symlink state is trusted at creation time. Post-snapshot mutation is out of scope. `O_NOFOLLOW` is used at the open layer for additional defense-in-depth.

## Test Implementation Guidance

**Test Type:** UNIT
**Test Data Requirements:**
- Temporary directory with nested files (`src/main.go`, `README.md`, `.gitignore`)
- Symlinks: one pointing inside root, one pointing outside root, one pointing to parent dir
- `.git/` directory with `config` and `objects/` subdirectories
- `foo.git/bar` directory for false-positive testing
- Empty file for NUL byte testing

**Mock/Stub Requirements:**
- No mocks needed; tests use real filesystem via `os.MkdirTemp`
- Symlinks created with `os.Symlink` (skipped on Windows if permissions unavailable, marked with build tag)
- Cleanup via `t.Cleanup()` with `os.RemoveAll`

**Test Structure:**
```go
func TestJail_Resolve(t *testing.T) {
    tests := []struct {
        name      string
        path      string
        setup     func(root string) // create fixtures
        wantPath  string            // expected resolved suffix
        wantErr   string            // expected error reason, empty for success
    }{
        // table entries for each scenario above
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // setup root, create jail, call Resolve, assert
        })
    }
}
```

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/tools/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Test coverage ≥95% for `jail.go`

**Story-Specific:**
- [ ] Table-driven test covers all 10 edge cases with distinct assertions per vector
- [ ] `JailError` struct has `Path` and `Reason` exported fields and passes `errors.As` extraction test
- [ ] `.git` component matching verified with `.gitignore`, `.github/`, and `foo.git/` fixtures all resolving successfully
- [ ] Path resolution pipeline follows the exact order from Technical Considerations: (1) empty/NUL, (2) absolute, (3) Clean, (4) Join, (5) EvalSymlinks, (6) HasPrefix, (7) .git component

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] No filesystem side effects escape test temp directories
- [ ] Error messages are operator-friendly and name the offending path
