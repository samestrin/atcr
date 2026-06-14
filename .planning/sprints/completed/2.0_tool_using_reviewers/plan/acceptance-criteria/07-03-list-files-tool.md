# Acceptance Criteria: `list_files` Tool — Depth-Capped Directory Listing

**Related User Story:** [07: Tool Definitions & Dispatcher](../user-stories/07-tool-definitions-dispatcher.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Directory read | `os.ReadDir` | Lists entries in a single directory |
| Recursion | Manual recursion with depth counter | Capped by `MaxListDepth` |
| Entry cap | `MaxListFiles` constant | Truncates large listings |
| Output format | Plain text, one entry per line | Prefix `d ` for directories, `f ` for files |
| Test framework | `go test` + `t.TempDir()` | Fixture directories, no LLM |

### Related Files (from codebase-discovery.json)

- `internal/tools/defs.go` — create: `list_files` JSON Schema definition
- `internal/tools/list_files.go` — create: `listFilesHandler` implementation
- `internal/tools/dispatch.go` — create: dispatcher routes to handler
- `internal/tools/list_files_test.go` — create: unit tests

## Happy Path Scenarios

**Scenario 1: List root directory**
- **Given** a snapshot root containing files `a.go`, `b.go` and directory `src/`
- **When** `list_files({})` is executed
- **Then** the result contains:
  ```
  f a.go
  f b.go
  d src
  ```

**Scenario 2: List subdirectory**
- **Given** `src/pkg/util.go` exists
- **When** `list_files({dir: "src"})` is executed
- **Then** the result contains:
  ```
  d pkg
  ```

**Scenario 3: Recursive listing within depth cap**
- **Given** a tree `src/pkg/helper.go` and `MaxListDepth >= 2`
- **When** `list_files({dir: "src"})` is executed
- **Then** the result contains:
  ```
  d pkg
  f pkg/helper.go
  ```

## Edge Cases

**Edge Case 1: Depth cap exceeded**
- **Given** a tree deeper than `MaxListDepth`
- **When** `list_files({dir: "src"})` is executed
- **Then** entries below the depth cap are omitted
- **And** the result ends with a marker such as `\n[...depth cap reached...]`
- **And** `truncated` is `true`

**Edge Case 2: Entry cap exceeded**
- **Given** a directory with more entries than `MaxListFiles`
- **When** `list_files({})` is executed
- **Then** only the first `MaxListFiles` entries are returned
- **And** the result ends with a marker such as `\n[...N more entries truncated...]`
- **And** `truncated` is `true`

**Edge Case 3: Empty directory**
- **Given** an empty directory `empty/`
- **When** `list_files({dir: "empty"})` is executed
- **Then** the result content is `""`
- **And** `truncated` is `false`

**Edge Case 4: `dir` defaults to root when omitted**
- **Given** any snapshot root
- **When** `list_files({})` and `list_files({dir: ""})` are executed
- **Then** both return the same listing of the snapshot root

## Error Conditions

**Error Scenario 1: `dir` points to a file**
- **Given** `list_files({dir: "src/main.go"})`
- **When** executed
- **Then** the handler returns a tool error: `"list_files: dir is not a directory: src/main.go"`

**Error Scenario 2: Directory not found**
- **Given** `list_files({dir: "missing"})`
- **When** executed
- **Then** the handler returns a tool error: `"list_files: directory not found: missing"`

## Performance Requirements

- **Scalability:** Listing must complete in <100ms for directories with ≤10,000 entries.
- **Memory:** Do not build unbounded slices; stream entries or cap at `MaxListFiles`.

## Security Considerations

- The dispatcher resolves `dir` through the path jail before listing.
- No directory traversal follows symlinks outside the snapshot root.
- Depth cap prevents the model from forcing unbounded recursion.

## Test Implementation Guidance

**Test Type:** UNIT

**Test Data Requirements:**
- Nested directory fixtures
- Empty directory
- Directory with many files
- Deep directory tree

**Mock/Stub Requirements:**
- Real filesystem via `t.TempDir()`
- Stub `Jail` that prepends the temp root

**Test Cases:**
1. List root
2. List subdirectory
3. Recursive listing within depth cap
4. Depth cap truncation
5. Entry cap truncation
6. Empty directory
7. Default `dir` behavior
8. `dir` is a file error
9. Missing directory error

## Definition of Done

**Auto-Verified:**
- [ ] All tests pass (`go test ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `list_files` returns depth-capped recursive listing
- [ ] Entries are prefixed with `d ` or `f `
- [ ] Depth and entry caps truncate with markers
- [ ] File path and missing directory cases return tool errors

**Manual Review:**
- [ ] Code reviewed and approved
