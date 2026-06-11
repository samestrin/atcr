# Acceptance Criteria: Payload Builders (diff, blocks, files)

**Related User Story:** [06: Payload Mode Selection](../user-stories/06-payload-mode-selection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Payload Builder | Go package `internal/payload` | Three builder functions |
| Git Integration | `os/exec` — `git diff`, `git diff --function-context`, `git show` | Shell out to git |
| Blocks Fallback | `git diff -U<n>` | When `--function-context` fails (no-brace languages, binary files) |
| Files Mode Marker | Go `text/template` or string builder | Changed region markers in full file content |
| Test Framework | `testify` (assert, require) | Table-driven tests with git repo fixtures |

## Related Files
- `internal/payload/builder.go` - create: `BuildDiff()`, `BuildBlocks()`, `BuildFiles()` builder functions
- `internal/payload/builder_test.go` - create: Tests for all three payload modes and fallback logic
- `internal/payload/testdata/` - create: Golden files for expected diff/blocks/files output
- `internal/git/diff.go` - create or modify: Git command wrappers for diff variants

## Happy Path Scenarios

**Scenario 1: diff mode produces standard unified diff**
- **Given** a git repository with a base ref and head ref containing file changes
- **When** `BuildDiff(baseRef, headRef)` is called
- **Then** the output is a standard unified diff (`git diff baseRef..headRef`)
- **And** the payload contains all changed files in unified diff format

**Scenario 2: blocks mode produces function-context-expanded output**
- **Given** a git repository with changes inside functions in a brace-language file (e.g., Go, Java)
- **When** `BuildBlocks(baseRef, headRef)` is called
- **Then** the output uses `git diff --function-context baseRef..headRef`
- **And** hunks are expanded to enclosing function/block boundaries
- **And** real line numbers are preserved in the output

**Scenario 3: files mode produces full head-version content with changed regions marked**
- **Given** a git repository with changed files between base and head
- **When** `BuildFiles(baseRef, headRef)` is called
- **Then** the output contains the full head-version content of each changed file
- **And** changed regions are marked (e.g., with comment markers or annotations)
- **And** unchanged regions are present but visually distinguishable

**Scenario 4: Builder dispatches on mode string**
- **Given** a `PayloadMode` type with values `"diff"`, `"blocks"`, `"files"`
- **When** `Build(mode, baseRef, headRef)` is called with a valid mode
- **Then** the correct builder function is invoked
- **And** the resulting payload matches the mode specification

## Edge Cases

**Edge Case 1: blocks mode falls back when --function-context fails on no-brace languages**
- **Given** a changed Python file (no braces) where `git diff --function-context` fails or produces empty
- **When** `BuildBlocks(baseRef, headRef)` is called
- **Then** the builder detects the failure
- **And** falls back to `git diff -U<n>` context diff for that file
- **And** the fallback payload is still useful for review

**Edge Case 2: blocks mode falls back for binary files**
- **Given** a changed binary file (e.g., image, compiled asset)
- **When** `BuildBlocks(baseRef, headRef)` is called
- **Then** `--function-context` fails for the binary file
- **And** the builder falls back to plain diff or excludes the binary with a note

**Edge Case 3: No changes between base and head**
- **Given** base and head refs point to the same commit
- **When** any builder is called
- **Then** the payload is empty
- **And** no error is returned

**Edge Case 4: Single-file change**
- **Given** only one file changed between base and head
- **When** any builder is called
- **Then** the payload contains exactly that one file's contribution

## Error Conditions

**Error Scenario 1: Invalid base ref**
- Error message: "failed to resolve base ref '<ref>': unknown revision or path not in the working tree"
- Exit code: 1

**Error Scenario 2: Invalid head ref**
- Error message: "failed to resolve head ref '<ref>': unknown revision or path not in the working tree"
- Exit code: 1

**Error Scenario 3: Invalid payload mode**
- Error message: "unknown payload mode '<mode>': must be one of diff, blocks, files"
- Exit code: 1

**Error Scenario 4: Git command fails unexpectedly (not a fallback-triggering failure)**
- Error message: "git diff failed: <stderr>"
- Exit code: 1

## Performance Requirements
- **Response Time:** Payload build completes in < 2s for repos with < 100 changed files
- **Throughput:** N/A (single build per agent per invocation)
- **Memory:** Payload held in memory; no temp files for standard operation

## Security Considerations
- **Input Validation:** base and head refs validated as git refs before use
- **Command Injection:** Refs passed as arguments to `exec.Command`, not interpolated into shell strings
- **Path Traversal:** File reads (files mode) scoped to git working tree

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- A small git repo fixture with known diffs (created via `git init` in `t.TempDir()`)
- Brace-language files (Go/Java) for blocks mode
- No-brace-language files (Python/YAML) for fallback testing
- Binary files for fallback testing
- Golden files for expected output comparison

**Mock/Stub Requirements:**
- Git commands: run real git in temp repos (no mocking — integration-level fidelity)
- For pure unit tests: mock `exec.Command` via an interface (`CommandRunner`)

**Test Cases:**
1. `TestBuildDiff_BasicChanges` — verify unified diff output
2. `TestBuildBlocks_FunctionExpansion` — verify --function-context expansion for brace languages
3. `TestBuildBlocks_FallbackPython` — verify fallback for no-brace languages
4. `TestBuildBlocks_FallbackBinary` — verify fallback for binary files
5. `TestBuildFiles_FullContentWithMarkers` — verify full content with changed region markers
6. `TestBuild_DispatchByMode` — verify mode string dispatches correctly
7. `TestBuild_EmptyDiff` — verify no error on empty diff
8. `TestBuild_InvalidMode` — verify error on unknown mode
9. `TestBuild_InvalidRefs` — verify error on bad refs

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] All three modes produce correct output against golden files

**Story-Specific:**
- [ ] `BuildDiff` produces standard unified diff via `git diff base..head`
- [ ] `BuildBlocks` uses `git diff --function-context` and falls back to `-U<n>` on failure
- [ ] `BuildFiles` reads full head-version content and marks changed regions
- [ ] Blocks fallback triggers for no-brace languages and binary files
- [ ] Invalid mode returns descriptive error

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Git command wrappers handle stderr correctly for fallback detection
