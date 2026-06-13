# Acceptance Criteria: Read-Only Enforcement & Write Tool Guard

**Related User Story:** [03: Path Jail & Snapshot Sandbox](../user-stories/03-path-jail-sandbox.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| File Open | `os.OpenFile` with `os.O_RDONLY` | Harness boundary enforces read-only at syscall level |
| `O_NOFOLLOW` | `syscall.O_NOFOLLOW` (Linux/macOS) | Blocks mid-flight symlink substitution |
| Tool Registry | Go map in `internal/tools/dispatch.go` | Maps tool names to handler functions |
| Init-Time Guard | `init()` function or package-level validation | Rejects write tool registration at startup |
| Tool Definition | Go struct `ToolDef` with `Name`, `Description`, `Parameters` | OpenAI-compatible function schema |
| Test Framework | `go test` with registry manipulation tests | Tests for write tool rejection |

## Related Files

- `internal/tools/dispatch.go` - create: tool registry with `RegisterTool`, `GetTool`, and `ExecuteTool` functions; all file opens use `O_RDONLY`
- `internal/tools/dispatch_test.go` - create: tests for read-only enforcement and write tool rejection
- `internal/tools/jail.go` - modify: `Resolve` method used by dispatch before opening files
- `internal/tools/registry.go` - create: tool definitions for `read_file`, `grep`, `list_files` with OpenAI-compatible schemas
- `internal/tools/registry_test.go` - create: tests verifying no write tools exist and registration guard works

## Happy Path Scenarios

**Scenario 1: `read_file` tool opens with `O_RDONLY`**
- **Given** a tool dispatcher with a valid jailed path `/snapshot/src/main.go`
- **When** the `read_file` tool handler opens the file
- **Then** `os.OpenFile` is called with flags `os.O_RDONLY` (value `0`), and the file is readable

**Scenario 2: `read_file` tool opens with `O_NOFOLLOW` where supported**
- **Given** a tool dispatcher on Linux or macOS
- **When** the `read_file` tool handler opens a file
- **Then** `os.OpenFile` includes `syscall.O_NOFOLLOW` in flags, preventing mid-flight symlink substitution

**Scenario 3: `grep` tool opens files with `O_RDONLY`**
- **Given** a `grep` tool handler that searches file contents
- **When** it opens each file to search
- **Then** all file opens use `os.O_RDONLY` flags

**Scenario 4: `list_files` tool uses `os.ReadDir` (inherently read-only)**
- **Given** a `list_files` tool handler
- **When** it lists directory contents
- **Then** it uses `os.ReadDir` which is inherently read-only (no open flags needed)

**Scenario 5: Tool registry contains only read-only tools**
- **Given** the tool registry at startup
- **When** `GetRegisteredTools()` is called
- **Then** it returns exactly `read_file`, `grep`, `list_files` — no other tools registered

## Edge Cases

**Edge Case 1: Attempt to register `write_file` tool is rejected**
- **Given** the tool registry initialization
- **When** code attempts to call `RegisterTool("write_file", handler)`
- **Then** the registration function panics or returns an error: `"tool registry: write tools are not allowed: write_file"`

**Edge Case 2: Attempt to register `delete_file` tool is rejected**
- **Given** the tool registry initialization
- **When** code attempts to call `RegisterTool("delete_file", handler)`
- **Then** registration is rejected with error: `"tool registry: write tools are not allowed: delete_file"`

**Edge Case 3: Tool name matching is case-sensitive**
- **Given** the tool registry
- **When** code attempts to call `RegisterTool("Write_File", handler)`
- **Then** the guard checks for write-related name patterns case-insensitively and rejects it

**Edge Case 4: Attempt to register tool with write-related name pattern**
- **Given** the tool registry
- **When** code attempts `RegisterTool("file_modifier", handler)`
- **Then** the name-pattern guard rejects any tool name containing `write`, `create`, `delete`, `remove`, `modify`, `update`, `append`, `patch` (case-insensitive)

**Edge Case 5: `read_file` on a directory returns error**
- **Given** a jailed path pointing to a directory
- **When** `read_file` handler attempts to open it
- **Then** `os.OpenFile` succeeds but the handler detects `IsDir()` and returns tool error: `"read_file: [path] is a directory"`

**Edge Case 6: `read_file` on a non-existent file returns error**
- **Given** a jailed path that does not exist
- **When** `read_file` handler attempts to open it
- **Then** `os.OpenFile` returns `os.ErrNotExist` and the handler returns tool error: `"read_file: file not found: [path]"`

**Edge Case 7: `read_file` on a file larger than max size returns error**
- **Given** a jailed path pointing to a file larger than the configured max size (e.g., 1MB)
- **When** `read_file` handler opens it
- **Then** the handler checks file size via `Stat()` before reading and returns error: `"read_file: file exceeds maximum size: [path] ([size] bytes)"`

## Error Conditions

**Error Scenario 1: Write tool registration attempted**
- Error message: `"tool registry: write tools are not allowed: [tool_name]"`
- This is an init-time or registration-time guard; it prevents the program from starting with write tools registered

**Error Scenario 2: File open with write flags attempted**
- If any code path attempts `os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)` within the tool harness, the compiler/linter should catch it (code review); at runtime the jail ensures the path is read-only

**Error Scenario 3: Tool handler attempts to bypass jail**
- If a tool handler calls `os.Open` directly instead of going through `jail.Resolve()`, the path is not validated and may escape the sandbox
- Mitigation: Tool handlers receive pre-validated paths from the dispatcher; they do not call `jail.Resolve()` themselves

## Performance Requirements

- **Open overhead:** `O_RDONLY | O_NOFOLLOW` adds <0.1ms per file open compared to `O_RDONLY` alone
- **Registry lookup:** Tool name lookup in the registry map must complete in <1μs (O(1) map access)
- **No runtime guard overhead:** The write-tool guard runs at init time only; runtime tool dispatch has no guard overhead

## Security Considerations

- **`O_RDONLY` at harness boundary:** Every file open in the tool harness uses `os.O_RDONLY`. No tool handler has access to `os.O_WRONLY`, `os.O_CREATE`, or `os.O_TRUNC` flags.
- **`O_NOFOLLOW` prevents TOCTOU:** On Linux/macOS, `syscall.O_NOFOLLOW` blocks symlink substitution between `jail.Resolve()` and `os.OpenFile()`.
- **Init-time write tool guard:** The tool registry validates all registered tool names against a blocklist of write-related patterns at init time. This is a compile-time/startup-time safety net, not a runtime check.
- **Tool definitions are static:** Tool schemas (name, description, parameters) are defined as constants, not loaded from external configuration. An LLM cannot register new tools.
- **No `exec` tool:** The tool registry does not include any tool that can execute system commands. The only tools are `read_file`, `grep`, and `list_files`.
- **Dispatcher enforces jail:** The dispatcher calls `jail.Resolve(relPath)` for every tool call before passing the validated absolute path to the tool handler. Tool handlers never see or process raw user input paths.

## Test Implementation Guidance

**Test Type:** UNIT
**Test Data Requirements:**
- Temporary files and directories for tool handler testing
- Files of various sizes (small, at limit, over limit)
- Symlinks for `O_NOFOLLOW` testing
- Tool registration test cases with valid and invalid tool names

**Mock/Stub Requirements:**
- No mocks needed for registry tests; they test the actual registry data structure
- File handler tests use real filesystem via `os.MkdirTemp`
- `O_NOFOLLOW` tests create symlinks and verify the open fails with `syscall.ELOOP` (or equivalent)

**Test Structure:**
```go
func TestReadOnlyEnforcement(t *testing.T) {
    // Verify all tool handlers use O_RDONLY
    // Open file with O_RDONLY, confirm readable
    // Attempt to open with O_WRONLY, confirm rejected at code level
}

func TestWriteToolRejection(t *testing.T) {
    tests := []struct{
        name string
        toolName string
        wantErr bool
    }{
        {"write_file rejected", "write_file", true},
        {"delete_file rejected", "delete_file", true},
        {"file_modifier rejected", "file_modifier", true},
        {"read_file allowed", "read_file", false},
        {"grep allowed", "grep", false},
    }
    // for each: attempt RegisterTool, assert error or success
}

func TestToolRegistry_Completeness(t *testing.T) {
    tools := GetRegisteredTools()
    assert.ElementsMatch(t, []string{"read_file", "grep", "list_files"}, toolNames(tools))
}

func TestReadFile_Directory(t *testing.T) {
    // create temp dir, call read_file handler, assert "is a directory" error
}

func TestReadFile_NotFound(t *testing.T) {
    // call read_file on non-existent path, assert "file not found" error
}

func TestReadFile_TooLarge(t *testing.T) {
    // create file > max size, call read_file, assert "exceeds maximum size" error
}
```

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/tools/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Test coverage ≥95% for `dispatch.go` and `registry.go`
- [ ] `grep -r "O_WRONLY\|O_CREATE\|O_TRUNC" internal/tools/` returns no matches (no write flags in tool code)

**Story-Specific:**
- [ ] Every `os.OpenFile` call in tool handlers uses `os.O_RDONLY` as the flag
- [ ] `syscall.O_NOFOLLOW` is included in open flags on Linux/macOS (build-tagged for portability)
- [ ] Write tool registration guard rejects all names containing write-related patterns (case-insensitive)
- [ ] Tool registry at runtime contains exactly 3 tools: `read_file`, `grep`, `list_files`
- [ ] `read_file` handler returns distinct errors for: directory, not found, exceeds max size
- [ ] Dispatcher calls `jail.Resolve()` before every tool handler invocation

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] No `os.OpenFile` with write flags exists anywhere in `internal/tools/`
- [ ] Tool definitions are hardcoded constants, not loaded from config
- [ ] Write-tool guard blocklist is comprehensive and documented
