# Acceptance Criteria: Worktree Cleanup & Manifest Recording

**Related User Story:** [03: Path Jail & Snapshot Sandbox](../user-stories/03-path-jail-sandbox.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Cleanup Guarantee | Go `defer` statement | Called immediately after `SnapshotFor` returns |
| Worktree Removal | `git worktree remove --force` with `os.RemoveAll` fallback | Ensures cleanup even if git command fails |
| Panic Recovery | `defer` with `recover()` in agent loop | Ensures cleanup runs on panic |
| Manifest Recording | `internal/fanout/manifest.go` JSON marshaling | Records `snapshot_mode`, `snapshot_worktree_path`, `head_sha` |
| Test Framework | `go test` with injected panics | Tests cleanup on error and panic paths |

## Related Files

- `internal/tools/snapshot.go` - modify: cleanup function uses `git worktree remove --force` + `os.RemoveAll` fallback
- `internal/fanout/engine.go` - modify: agent loop calls `SnapshotFor`, defers `cleanup`, records manifest
- `internal/fanout/manifest.go` - modify: review stage struct gains snapshot fields, JSON serialization
- `internal/fanout/engine_test.go` - create: tests for cleanup on success, error, and panic paths
- `internal/fanout/manifest_test.go` - modify: tests for snapshot metadata in manifest output

## Happy Path Scenarios

**Scenario 1: Cleanup runs on successful agent run**
- **Given** `SnapshotFor` returned a worktree root and cleanup function on the slow path
- **When** the agent loop completes successfully and returns
- **Then** `defer cleanup()` executes, the temporary worktree is removed, and `os.Stat(worktreePath)` returns `os.IsNotExist`

**Scenario 2: Cleanup runs on agent error**
- **Given** `SnapshotFor` returned a worktree root and cleanup function
- **When** the agent loop encounters an error (e.g., LLM API failure) and returns the error
- **Then** `defer cleanup()` executes before the error propagates, and the temporary worktree is removed

**Scenario 3: Cleanup runs on panic**
- **Given** `SnapshotFor` returned a worktree root and cleanup function
- **When** the agent loop panics (e.g., nil pointer dereference in tool handler)
- **Then** `defer cleanup()` executes via `recover()` wrapper, and the temporary worktree is removed

**Scenario 4: Manifest records snapshot metadata on success**
- **Given** `SnapshotFor` returned a worktree path and mode `"worktree"` with head SHA `abc1234`
- **When** the manifest is written after the agent run
- **Then** `manifest.json` contains:
  ```json
  {
    "stages": {
      "review": {
        "snapshot_mode": "worktree",
        "snapshot_worktree_path": "/tmp/atcr-snapshot-abc1234",
        "head_sha": "abc1234"
      }
    }
  }
  ```

**Scenario 5: Manifest records live mode on fast path**
- **Given** `SnapshotFor` took the fast path (live worktree)
- **When** the manifest is written
- **Then** `manifest.json` contains `"snapshot_mode": "live"` and `"snapshot_worktree_path": ""`

## Edge Cases

**Edge Case 1: Cleanup called multiple times**
- **Given** a cleanup function from `SnapshotFor`
- **When** it is called three times in succession
- **Then** the first call removes the worktree, subsequent calls are no-ops with no error

**Edge Case 2: `git worktree remove` fails, fallback to `os.RemoveAll`**
- **Given** a temporary worktree where `git worktree remove --force` fails (e.g., git database corrupted)
- **When** cleanup is called
- **Then** the cleanup function falls back to `os.RemoveAll(worktreePath)` and the directory is removed

**Edge Case 3: Worktree path is empty string (live mode)**
- **Given** `SnapshotFor` took the fast path and returned empty `snapshot_worktree_path`
- **When** cleanup is called
- **Then** cleanup is a no-op (does not attempt to remove the live repository root)

**Edge Case 4: Worktree removal partially fails**
- **Given** a temporary worktree where some files have read-only permissions
- **When** cleanup is called
- **Then** `git worktree remove --force` handles permission changes; if it fails, `os.RemoveAll` force-removes remaining files

**Edge Case 5: Manifest write fails after successful agent run**
- **Given** the agent run completed successfully but the manifest output directory is read-only
- **When** manifest marshaling and writing is attempted
- **Then** the error is logged but does not affect the cleanup (cleanup already deferred and runs regardless)

## Error Conditions

**Error Scenario 1: `git worktree remove` returns non-zero exit**
- Error logged: `"snapshot: git worktree remove failed, falling back to os.RemoveAll: [error]"`
- Cleanup continues with `os.RemoveAll` fallback

**Error Scenario 2: `os.RemoveAll` fails on worktree path**
- Error logged: `"snapshot: failed to remove worktree path [path]: [error]"`
- This is a critical failure; the error is surfaced to the operator but does not panic

**Error Scenario 3: Manifest JSON marshaling fails**
- Error message: `"manifest: failed to marshal review stage: [error]"`
- Returned as a warning in logs; agent result is still delivered

## Performance Requirements

- **Cleanup Latency:** `git worktree remove --force` must complete in <2s for typical repositories; `os.RemoveAll` fallback in <1s
- **No blocking:** Cleanup must not block on network calls or LLM responses
- **Manifest write:** Must complete in <100ms for manifest sizes <10KB

## Security Considerations

- **Cleanup uses force removal:** `git worktree remove --force` is used to ensure removal even if the worktree has uncommitted changes (which it should not, but defense-in-depth).
- **Path validation before removal:** The cleanup function validates that the worktree path is under `os.TempDir()` and matches the expected `atcr-snapshot-` prefix before calling `os.RemoveAll`, preventing accidental deletion of arbitrary directories.
- **No symlink following in cleanup:** `os.RemoveAll` is called on the worktree path returned by `SnapshotFor`, which was already validated. No additional symlink resolution is performed during cleanup.
- **Manifest integrity:** Manifest JSON is written atomically (write to temp file, rename) to prevent partial writes from corrupting the manifest.

## Test Implementation Guidance

**Test Type:** UNIT and INTEGRATION
**Test Data Requirements:**
- Temporary git repositories with worktrees for cleanup testing
- Panic injection via `defer func() { panic("test") }()` in agent loop mock
- Read-only directories for fallback testing (where supported by OS)
- Manifest output directories for serialization testing

**Mock/Stub Requirements:**
- Mock LLM client for agent loop tests (to control success/error/panic paths)
- Real git for worktree creation and removal
- Filesystem assertions via `os.Stat` and `os.ReadDir`

**Test Structure:**
```go
func TestCleanup_OnSuccess(t *testing.T) {
    // setup worktree, run agent loop, assert worktree removed
}

func TestCleanup_OnError(t *testing.T) {
    // setup worktree, agent returns error, assert worktree removed
}

func TestCleanup_OnPanic(t *testing.T) {
    // setup worktree, agent panics, recover, assert worktree removed
}

func TestCleanup_Idempotent(t *testing.T) {
    // call cleanup 3 times, assert no error on 2nd and 3rd
}

func TestManifest_SnapshotFields(t *testing.T) {
    // create manifest with snapshot metadata, marshal, assert JSON fields
}
```

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/... ./internal/tools/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Test coverage ≥90% for cleanup paths in `snapshot.go`

**Story-Specific:**
- [ ] Cleanup-on-success test: worktree confirmed absent via `os.Stat` after agent loop returns nil
- [ ] Cleanup-on-error test: worktree confirmed absent after agent loop returns error
- [ ] Cleanup-on-panic test: worktree confirmed absent after panic is recovered
- [ ] Idempotent-cleanup test: cleanup called 3 times, no error on 2nd/3rd call
- [ ] Manifest test: JSON output contains `snapshot_mode`, `snapshot_worktree_path`, and `head_sha` in `stages.review`
- [ ] Live-mode manifest test: `snapshot_mode` is `"live"` and `snapshot_worktree_path` is empty string

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Cleanup function validates worktree path prefix before `os.RemoveAll`
- [ ] `defer cleanup()` is the first statement after `SnapshotFor` returns in the agent loop
- [ ] Manifest write is atomic (temp file + rename)
