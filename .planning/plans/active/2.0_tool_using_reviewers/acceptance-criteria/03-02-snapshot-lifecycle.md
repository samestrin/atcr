# Acceptance Criteria: Snapshot Manager Lifecycle

**Related User Story:** [03: Path Jail & Snapshot Sandbox](../user-stories/03-path-jail-sandbox.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Snapshot Manager | Go struct `SnapshotManager` in `internal/tools/snapshot.go` | Manages worktree lifecycle |
| Git Integration | `os/exec` with `git worktree add`, `git rev-parse`, `git status` | Stdlib command execution |
| Fast-Path Detection | `git rev-parse HEAD` + `git status --porcelain` | Compares SHAs and checks for dirty state |
| Manifest Recording | `internal/fanout/manifest.go` | Records `snapshot_mode`, `head_sha`, `snapshot_worktree_path` |
| Test Framework | `go test` with fixture git repos | Tests use `git init` in temp dirs |

### Related Files (from codebase-discovery.json)

- `internal/tools/snapshot.go` - create: `SnapshotManager` struct with `SnapshotFor(head string) (root string, cleanup func(), err error)` method
- `internal/tools/snapshot_test.go` - create: tests for fast-path, slow-path, and error conditions
- `internal/payload/manifest.go:18` - modify: add `SnapshotMode`, `SnapshotWorktreePath`, `HeadSHA` fields to review stage
- `internal/fanout/engine.go:228` - modify: call `SnapshotFor` before agent loop, defer `cleanup`

## Happy Path Scenarios

**Scenario 1: Fast path — head matches HEAD and worktree is clean**
- **Given** a git repository at commit `abc1234` with no uncommitted changes
- **When** `SnapshotFor("abc1234")` is called
- **Then** it returns the repository root as `root`, a no-op `cleanup` function, `snapshot_mode` is `"live"`, and no temporary worktree is created

**Scenario 2: Slow path — head differs from HEAD**
- **Given** a git repository currently at commit `def5678`, with commit `abc1234` available in history
- **When** `SnapshotFor("abc1234")` is called
- **Then** it creates a temporary worktree at `abc1234` via `git worktree add`, returns the worktree path as `root`, a cleanup function that removes the worktree, and `snapshot_mode` is `"worktree"`

**Scenario 3: Slow path — head matches HEAD but worktree is dirty**
- **Given** a git repository at commit `abc1234` with unstaged changes to `src/main.go`
- **When** `SnapshotFor("abc1234")` is called
- **Then** it detects dirty state via `git status --porcelain`, falls through to slow path, creates a temporary worktree, and `snapshot_mode` is `"worktree"`

**Scenario 4: Cleanup function is idempotent**
- **Given** a `SnapshotFor` call that returned a cleanup function on the slow path
- **When** the cleanup function is called twice
- **Then** the first call removes the worktree, the second call is a no-op with no error

**Scenario 5: Manifest records snapshot metadata**
- **Given** a successful `SnapshotFor` call
- **When** the manifest is written
- **Then** `manifest.json` `stages.review` contains `snapshot_mode` (`"live"` or `"worktree"`), `head_sha` (the resolved SHA), and `snapshot_worktree_path` (worktree path or empty string for live)

## Edge Cases

**Edge Case 1: Invalid SHA is rejected**
- **Given** a git repository
- **When** `SnapshotFor("not-a-valid-sha")` is called
- **Then** `git rev-parse` fails and `SnapshotFor` returns an error: `"snapshot: invalid head SHA: not-a-valid-sha"`

**Edge Case 2: SHA not reachable from current repository**
- **Given** a git repository that does not contain commit `fff9999`
- **When** `SnapshotFor("fff9999")` is called
- **Then** `git worktree add` fails and `SnapshotFor` returns an error wrapping the git stderr output

**Edge Case 3: Git not available on PATH**
- **Given** an environment where `git` is not on `PATH`
- **When** `SnapshotFor` is called
- **Then** `os/exec.LookPath("git")` fails and `SnapshotFor` returns error: `"snapshot: git not found in PATH"`

**Edge Case 4: Worktree path already exists**
- **Given** a stale worktree directory at the intended temp path (from a previous crashed run)
- **When** `SnapshotFor` on the slow path attempts `git worktree add`
- **Then** git returns an error; `SnapshotFor` attempts `git worktree prune` and retries once, then returns an error if it still fails

**Edge Case 5: Concurrent SnapshotFor calls for same SHA**
- **Given** two goroutines calling `SnapshotFor("abc1234")` simultaneously on slow path
- **When** both attempt `git worktree add`
- **Then** each creates a distinct temporary directory (unique temp path), both succeed independently

## Error Conditions

**Error Scenario 1: `git rev-parse HEAD` fails**
- Error message: `"snapshot: failed to resolve HEAD: [git stderr]"`
- Returned when the current directory is not a git repository or HEAD is detached in an unexpected way

**Error Scenario 2: `git status --porcelain` fails**
- Error message: `"snapshot: failed to check working tree status: [git stderr]"`
- Returned when git status encounters an error (e.g., index lock file exists)

**Error Scenario 3: `git worktree add` fails**
- Error message: `"snapshot: failed to create worktree for [SHA]: [git stderr]"`
- Returned when the slow path cannot create the worktree

## Performance Requirements

- **Fast Path Latency:** `SnapshotFor` on the fast path (live worktree) must complete in <50ms (two git commands: `rev-parse` + `status`)
- **Slow Path Latency:** `SnapshotFor` on the slow path must complete in <5s for repositories <1GB (dominated by `git worktree add` checkout time)
- **Concurrent Safety:** Multiple `SnapshotFor` calls for different SHAs must not block each other

## Security Considerations

- **Read-only enforcement:** The snapshot manager creates the worktree but does not grant write access. All file access goes through the path jail (AC-01).
- **Temporary worktree in OS temp directory:** Uses `os.MkdirTemp` with a predictable prefix (`atcr-snapshot-`) so operators can identify and remove stale snapshots.
- **No arbitrary command execution:** Git commands are constructed with explicit argument arrays, never shell-interpolated.
- **SHA validation:** The `head` parameter must match `[0-9a-f]{7,40}` before being passed to git commands, preventing argument injection.

## Test Implementation Guidance

**Test Type:** UNIT (fixture-based with real git repos in temp dirs)
**Test Data Requirements:**
- Temporary git repositories initialized with `git init`
- Multiple commits created via `git commit --allow-empty`
- Dirty state simulated by writing files without committing
- Both fast-path (clean, matching HEAD) and slow-path (different SHA or dirty) scenarios

**Mock/Stub Requirements:**
- No mocks for git commands; tests use real git in temp directories
- Tests marked with `//go:build !nogit` to skip in environments without git
- Each test creates its own temp git repo to avoid cross-test interference
- Cleanup via `t.Cleanup()` with worktree removal and temp dir cleanup

**Test Structure:**
```go
func TestSnapshotFor_FastPath(t *testing.T) {
    repo := setupGitRepo(t)
    head := commitEmpty(t, repo, "initial")
    mgr := NewSnapshotManager(repo)
    root, cleanup, err := mgr.SnapshotFor(head)
    // assert: err is nil, root == repo, cleanup is no-op
    cleanup()
}

func TestSnapshotFor_SlowPath(t *testing.T) {
    repo := setupGitRepo(t)
    old := commitEmpty(t, repo, "old")
    commitEmpty(t, repo, "new") // HEAD advances
    mgr := NewSnapshotManager(repo)
    root, cleanup, err := mgr.SnapshotFor(old)
    // assert: err is nil, root != repo, root is worktree path
    cleanup()
    // assert: worktree path no longer exists
}
```

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/tools/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Test coverage ≥90% for `snapshot.go`

**Story-Specific:**
- [ ] Fast-path test: `head == HEAD`, clean worktree → returns repo root, mode is `"live"`
- [ ] Slow-path test: `head != HEAD` → returns worktree path, mode is `"worktree"`
- [ ] Dirty-worktree test: `head == HEAD` but `git status --porcelain` non-empty → falls through to slow path
- [ ] Cleanup test: after `cleanup()`, worktree directory confirmed absent via `os.Stat` returning `os.IsNotExist`
- [ ] Manifest test: `manifest.json` contains correct `snapshot_mode`, `head_sha`, and `snapshot_worktree_path` fields

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Git commands use argument arrays, not shell strings
- [ ] Cleanup function is safe to call multiple times (verified in test)
