# Acceptance Criteria: `read_file` Tool — Line-Numbered Output and Byte Cap

**Related User Story:** [07: Tool Definitions & Dispatcher](../user-stories/07-tool-definitions-dispatcher.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| File open | `os.OpenFile` with `os.O_RDONLY` (and `syscall.O_NOFOLLOW` where supported) | Read-only, no symlink follow |
| Line numbering | `bufio.Scanner` with `SplitLines` | Prefix each line with `N: ` |
| Slicing | Integer `start_line`/`end_line` parameters (1-based, inclusive) | Returns subset while preserving original line numbers |
| Byte cap | `MaxReadFileBytes` constant | Truncates content above cap with marker |
| Test framework | `go test` + `t.TempDir()` | Fixture files, no LLM |

### Related Files (from codebase-discovery.json)

- `internal/tools/defs.go` — create: `read_file` JSON Schema definition
- `internal/tools/read_file.go` — create: `readFileHandler` implementation
- `internal/tools/dispatch.go` — create: dispatcher calls `jail.Resolve` and routes to handler
- `internal/tools/read_file_test.go` — create: unit tests

## Happy Path Scenarios

**Scenario 1: Read a small file with line numbers**
- **Given** a file at `src/main.go` containing three lines: `package main`, `import "fmt"`, `func main() {}`
- **When** `read_file({path: "src/main.go"})` is executed
- **Then** the result content is:
  ```
  1: package main
  2: import "fmt"
  3: func main() {}
  ```
- **And** `truncated` is `false`
- **And** `original_bytes` equals the byte length of the rendered output

**Scenario 2: Read a slice with start_line and end_line**
- **Given** the same file as Scenario 1
- **When** `read_file({path: "src/main.go", start_line: 2, end_line: 3})` is executed
- **Then** the result content is:
  ```
  2: import "fmt"
  3: func main() {}
  ```
- **And** `truncated` is `false`

**Scenario 3: Read an empty file**
- **Given** an empty file at `empty.go`
- **When** `read_file({path: "empty.go"})` is executed
- **Then** the result content is `""`
- **And** `truncated` is `false`
- **And** `original_bytes` is `0`

## Edge Cases

**Edge Case 1: File content exceeds byte cap**
- **Given** a file whose line-numbered rendered output is larger than `MaxReadFileBytes`
- **When** `read_file({path: "large.go"})` is executed
- **Then** the result content is truncated to `MaxReadFileBytes`
- **And** `truncated` is `true`
- **And** `original_bytes` equals the full rendered byte length before truncation
- **And** the content ends with a marker such as `\n[...truncated...]`

**Edge Case 2: `start_line` equals `end_line`**
- **Given** a file with 10 lines
- **When** `read_file({path: "file.go", start_line: 5, end_line: 5})` is executed
- **Then** the result contains exactly one line prefixed with `5: `

**Edge Case 3: `start_line` beyond file length**
- **Given** a file with 3 lines
- **When** `read_file({path: "file.go", start_line: 10, end_line: 12})` is executed
- **Then** the result content is `""`
- **And** `truncated` is `false`

**Edge Case 4: `end_line` beyond file length**
- **Given** a file with 5 lines
- **When** `read_file({path: "file.go", start_line: 3, end_line: 100})` is executed
- **Then** the result contains lines 3 through 5 with original prefixes

**Edge Case 5: `start_line` > `end_line`**
- **Given** any file
- **When** `read_file({path: "file.go", start_line: 5, end_line: 2})` is executed
- **Then** the handler returns a tool error: `"read_file: start_line cannot be greater than end_line"`

**Edge Case 6: Line numbers are 1-based**
- **Given** a file with 3 lines
- **When** `read_file({path: "file.go", start_line: 1, end_line: 1})` is executed
- **Then** the result contains exactly the first line prefixed with `1: `

## Error Conditions

**Error Scenario 1: Path points to a directory**
- **Given** `read_file({path: "src"})` where `src` is a directory
- **When** executed
- **Then** the handler returns a tool error: `"read_file: src is a directory"`

**Error Scenario 2: File not found**
- **Given** `read_file({path: "missing.go"})`
- **When** executed
- **Then** the handler returns a tool error: `"read_file: file not found: missing.go"`

**Error Scenario 3: Invalid argument JSON**
- **Given** `read_file({path: 123})` (non-string path)
- **When** the dispatcher parses arguments
- **Then** it returns a tool error: `"read_file: invalid arguments: ..."`

## Performance Requirements

- **Memory:** Handler must not materialize the entire file content when applying the byte cap; streaming read with truncation is preferred.
- **Latency:** Reading a 1 MB file completes in <50ms on a warm filesystem.

## Security Considerations

- The dispatcher resolves `path` through the path jail before calling the handler; the handler never sees raw user input.
- File opens use `os.O_RDONLY`; no write flags are present.
- `O_NOFOLLOW` is used where supported to block symlink substitution after jail resolution.

## Test Implementation Guidance

**Test Type:** UNIT

**Test Data Requirements:**
- Temporary files with known line counts and sizes
- A file larger than `MaxReadFileBytes`
- Directory fixture
- Missing file path

**Mock/Stub Requirements:**
- Real filesystem via `t.TempDir()`
- A real or stub `Jail` that prepends the temp root to relative paths

**Test Cases:**
1. Small file line numbering
2. Slice with start/end lines
3. Empty file
4. Byte-cap truncation
5. start_line > end_line
6. start_line beyond file length
7. end_line beyond file length
8. Directory path error
9. Missing file error
10. Invalid argument JSON

## Definition of Done

**Auto-Verified:**
- [ ] All tests pass (`go test ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `read_file` returns line-numbered content for valid files
- [ ] `start_line`/`end_line` slicing works and preserves original line numbers
- [ ] Byte-cap truncation is marked with `truncated: true` and `original_bytes`
- [ ] Directory and missing-file cases return tool errors, not panics

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] No write flags in file open path
