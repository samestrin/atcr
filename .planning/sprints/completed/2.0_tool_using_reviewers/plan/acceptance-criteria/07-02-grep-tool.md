# Acceptance Criteria: `grep` Tool — Regex, Glob Filter, and Match Cap

**Related User Story:** [07: Tool Definitions & Dispatcher](../user-stories/07-tool-definitions-dispatcher.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Regex engine | Go `regexp` stdlib | Compiles `pattern` argument |
| File walk | `filepath.WalkDir` | Starting at snapshot root or `dir` |
| Glob filter | `filepath.Match` | Optional `glob` argument |
| Match cap | `MaxGrepMatches` constant | Truncates match list with marker |
| Per-match cap | `MaxGrepFileBytes` | Truncates individual match context |
| Test framework | `go test` + `t.TempDir()` | Fixture files, no LLM |

### Related Files (from codebase-discovery.json)

- `internal/tools/defs.go` — create: `grep` JSON Schema definition
- `internal/tools/grep.go` — create: `grepHandler` implementation
- `internal/tools/dispatch.go` — create: dispatcher routes to handler
- `internal/tools/grep_test.go` — create: unit tests

## Happy Path Scenarios

**Scenario 1: Find matches across files**
- **Given** files `a.go` containing `func Foo() {}` and `b.go` containing `func Bar() {}`
- **When** `grep({pattern: "func \\w+"})` is executed
- **Then** the result contains matches for both files with file path, line number, and matching line
- **And** the format is consistent, e.g., `a.go:1: func Foo() {}`

**Scenario 2: Glob filter restricts search**
- **Given** files `a.go`, `b.go`, and `readme.md`
- **When** `grep({pattern: "func", glob: "*.go"})` is executed
- **Then** only matches from `a.go` and `b.go` are returned

**Scenario 3: No matches found**
- **Given** files containing no occurrence of `xyz123`
- **When** `grep({pattern: "xyz123"})` is executed
- **Then** the result content is a clear message such as `"no matches for 'xyz123'"`
- **And** `truncated` is `false`

## Edge Cases

**Edge Case 1: Match cap exceeded**
- **Given** a repository with more matching lines than `MaxGrepMatches`
- **When** `grep({pattern: "."})` is executed
- **Then** the result contains up to `MaxGrepMatches` matches
- **And** `truncated` is `true`
- **And** `original_bytes` reflects the total count of matches found
- **And** the content ends with a marker such as `\n[...N more matches truncated...]`

**Edge Case 2: Invalid regex pattern**
- **Given** `grep({pattern: "[invalid"})`
- **When** executed
- **Then** the handler returns a tool error: `"grep: invalid regex: ..."`

**Edge Case 3: Empty pattern**
- **Given** `grep({pattern: ""})`
- **When** executed
- **Then** the handler returns a tool error: `"grep: pattern cannot be empty"`

**Edge Case 4: Glob does not match any files**
- **Given** files `a.go`, `b.go`
- **When** `grep({pattern: "func", glob: "*.md"})` is executed
- **Then** the result content is `"no matches for 'func' in glob '*.md'"`

**Edge Case 5: Directory argument restricts search**
- **Given** `src/a.go` and `test/b.go`
- **When** `grep({pattern: "func", dir: "src"})` is executed
- **Then** only matches from `src/a.go` are returned

## Error Conditions

**Error Scenario 1: `dir` points to a file**
- **Given** `grep({pattern: "func", dir: "src/main.go"})`
- **When** executed
- **Then** the handler returns a tool error: `"grep: dir is not a directory: src/main.go"`

**Error Scenario 2: `dir` path escapes jail**
- **Given** the dispatcher resolves `dir` and the jail rejects it
- **When** executed
- **Then** the dispatcher returns the jail error as a tool result

## Performance Requirements

- **Scalability:** Search must complete in <1s for repositories with ≤10,000 files.
- **Memory:** Do not hold full file contents; stream line-by-line while collecting matches.
- **Cancellation:** Respect `context.Context` cancellation during the walk.

## Security Considerations

- The dispatcher resolves `dir` through the path jail; relative globs are applied only within the snapshot root.
- ReDoS mitigation: limit total walk time via context, cap total matches, and reject patterns that fail a sanity check.
- No file is opened for writing.

## Test Implementation Guidance

**Test Type:** UNIT

**Test Data Requirements:**
- Fixture files with known content
- Files exceeding match cap
- Invalid regex patterns
- Glob patterns

**Mock/Stub Requirements:**
- Real filesystem via `t.TempDir()`
- Stub `Jail` that prepends the temp root

**Test Cases:**
1. Find matches across multiple files
2. Glob filter
3. No matches
4. Match-cap truncation
5. Invalid regex
6. Empty pattern
7. Directory-restricted search
8. `dir` is a file error
9. Glob with no matches

## Definition of Done

**Auto-Verified:**
- [ ] All tests pass (`go test ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `grep` compiles the pattern with Go stdlib `regexp`
- [ ] Optional `glob` filters files with `filepath.Match`
- [ ] Match cap truncates with marker
- [ ] Invalid regex and empty pattern return tool errors

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] ReDoS mitigations documented
