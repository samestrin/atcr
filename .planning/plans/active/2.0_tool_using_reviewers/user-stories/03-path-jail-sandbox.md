# User Story 3: Path Jail & Snapshot Sandbox

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** security-conscious operator running tool-using reviewer agents against repositories
**I want** all tool file access confined to a read-only snapshot of the repository at the resolved head SHA, with absolute, relative-escape, symlink-escape, and `.git/` access vectors blocked
**So that** a reviewer agent — no matter how small or confused — cannot mutate the repo, read secrets outside the project, or exfiltrate data beyond the provider call itself

## Story Context

- **Background:** Epic 2.0 transforms single-shot reviewers into bounded agents that call read-only tools (`read_file`, `grep`, `list_files`) across multiple turns. Unlike 1.x, where the payload was the universe, agents now walk the repository. This creates two new attack surfaces: (1) the agent can ask to read files outside the intended scope, and (2) the tool harness touches the filesystem on the operator's behalf. The sandbox must make both surfaces structurally safe — not by policy or prompt instruction, but by code that cannot be bypassed by a well-crafted tool-call argument.
- **Assumptions:**
  - The resolved `head` SHA is available from the payload resolver before the agent loop starts.
  - `git worktree add` is available on the operator's machine (already a dependency of atcr's test infrastructure).
  - The path jail is enforced at the tool-dispatch layer, not inside each tool implementation — tools receive pre-validated paths.
  - Read-only is structural: no write tool exists, and `os.OpenFile` is called with `O_RDONLY` at the harness boundary.
- **Constraints:**
  - No new third-party dependencies — path resolution and symlink checks use Go stdlib (`path/filepath`, `os`).
  - The sandbox must not regress review latency on the fast path (live worktree when `head` == HEAD and clean).
  - Submodule content is explicitly out of scope for v1 and must be documented as unreadable.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None (this story is foundational; Stories 1 and 2 consume the sandbox it provides) |

## Success Criteria (SMART Format)

- **Specific:** The tool dispatcher rejects every tool-call path that is absolute, escapes the snapshot root via `..`, resolves (after symlink expansion) outside the snapshot root, or targets any path under `.git/`. The snapshot manager returns a stable root directory for the resolved head SHA and cleans up any temporary worktree after the run.
- **Measurable:** 100% of the following vectors produce a tool error (not a panic, not a silent fallthrough) in unit tests: absolute path (`/etc/passwd`), `..` traversal (`../secrets`), symlink to parent directory, symlink to `/etc/passwd`, `.git/config`, `.git/objects/...`, empty path, path with embedded NUL. Live-worktree fast path is taken in ≥1 test where `head == HEAD` and worktree is clean. Temporary worktree is confirmed absent after cleanup in ≥1 test where `head != HEAD`.
- **Achievable:** All primitives (`filepath.Abs`, `filepath.Clean`, `filepath.EvalSymlinks`, `strings.HasPrefix`, `os.Stat`, `os.OpenFile` with `O_RDONLY`) are stdlib. The snapshot manager wraps existing `git worktree` commands the test infrastructure already relies on.
- **Relevant:** Without this sandbox, a tool-using agent is a remote-code-execution-shaped hole. The operator's trust in the review output depends on knowing the agent could not have read or written outside bounds. This is the security foundation every other story in the epic builds on.
- **Time-bound:** Delivered before Stories 1 and 2 reach their tool-dispatch integration tests; the sandbox is a prerequisite for any multi-turn agent test.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-path-jail-enforcement.md) | Path Jail Escape Vector Rejection | Unit |
| [03-02](../acceptance-criteria/03-02-snapshot-lifecycle.md) | Snapshot Manager Lifecycle | Unit |
| [03-03](../acceptance-criteria/03-03-worktree-cleanup.md) | Worktree Cleanup & Manifest Recording | Unit/Integration |
| [03-04](../acceptance-criteria/03-04-read-only-guard.md) | Read-Only Enforcement & Write Tool Guard | Unit |

## Original Criteria Overview

1. Path jail rejects all escape vectors (absolute paths, `..`, out-of-root symlinks, `.git/` access) with a structured error message that names the offending path and the rejection reason.
2. Snapshot manager returns a valid root directory for any resolved head SHA, using the live worktree on the fast path and a temporary `git worktree add` on the slow path.
3. Temporary worktree is fully removed after the run (including on error paths via `defer`), and its creation/removal is recorded in `manifest.json`.
4. File opens within the harness use `O_RDONLY`; no write tool definition exists in the tool registry; any attempt to register a write tool is rejected by a compile-time or init-time guard.
5. All sandbox behavior is covered by unit tests that run without network access, without a real git repository (fixture-based where possible), and without any LLM.

## Technical Considerations

- **Implementation Notes:**
  - Path resolution order: (1) reject empty/NUL-containing input, (2) reject absolute paths, (3) `filepath.Clean` to collapse `..`, (4) `filepath.Join(root, cleaned)` to produce candidate, (5) `filepath.EvalSymlinks` on candidate and on root, (6) `strings.HasPrefix(evaluatedCandidate, evaluatedRoot+string(os.PathSeparator))` (or exact equality for the root itself), (7) reject if any path segment is `.git`. Each tool call goes through this pipeline before any filesystem I/O.
  - Snapshot manager interface: `SnapshotFor(head string) (root string, cleanup func(), err error)`. The `cleanup` function is safe to call multiple times and is always `defer`-ed by the caller.
  - Fast-path detection: compare `head` to `git rev-parse HEAD`; if equal, run `git status --porcelain` and take the fast path only if output is empty. Record the decision in `manifest.json` (`"snapshot_mode": "live"` or `"snapshot_mode": "worktree"`).
  - `.git/` check must match the prefix `.git` as a path component, not just a string prefix — a file named `.gitignore` at the root is legitimate and must be readable.
- **Integration Points:**
  - `internal/tools/dispatcher.go` — every tool handler calls `jail.Resolve(relPath)` before touching the filesystem.
  - `internal/tools/snapshot.go` — the agent loop in `internal/fanout/engine.go` calls `SnapshotFor` before entering the turn loop and invokes `cleanup` on exit.
  - `internal/fanout/manifest.go` — records `snapshot_mode`, temporary worktree path, and head SHA.
  - `internal/tools/jail.go` — the `Jail` type and `Resolve` method; exported for unit testing in isolation.
- **Data Requirements:**
  - `manifest.json` `stages.review` gains `snapshot_mode` (`"live"` | `"worktree"`), `snapshot_worktree_path` (string, empty on live), and `head_sha` (string). No new top-level files; the sandbox is a runtime concern, not a persistent artifact beyond the manifest entry.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Symlink race: attacker swaps a regular file for a symlink between `EvalSymlinks` and `Open` | Medium | Open via the already-resolved, validated absolute path with `O_RDONLY | O_NOFOLLOW` where supported; on Linux/macOS `O_NOFOLLOW` blocks mid-flight symlink substitution. Document that v1 trusts the snapshot root's own symlink state (set at `git worktree add` time) — a post-snapshot mutation is out of threat model. |
| `.git` substring false-positive blocks `.gitignore`, `.github/`, etc. | Medium | Match `.git` as a path **component** by splitting on `os.PathSeparator`, not as a `strings.Contains`. Unit test with `.gitignore`, `.github/workflows/ci.yml`, and `foo.git/bar` to confirm they pass. |
| Temporary worktree leak on panic or `os.Exit` | Low | `defer cleanup()` immediately after `SnapshotFor` returns; cleanup uses `git worktree remove --force` plus an `os.RemoveAll` fallback on the worktree path. Test by injecting a panic mid-loop and asserting the worktree is gone afterward. |
| Fast-path false positive: dirty worktree but `head == HEAD` | Medium | `git status --porcelain` check must run *after* the HEAD comparison and must require empty output. If either check fails, fall through to the temporary worktree path. Test with staged, unstaged, and untracked changes. |
| `EvalSymlinks` fails on a path that doesn't exist yet | Low | Resolve the *existing* prefix (walk up until `EvalSymlinks` succeeds), then re-join the trailing components and re-check the prefix. This is a known Go pattern; encapsulate it in a helper and unit-test with a nested symlink chain. |
| Submodule content accidentally readable | Low | Submodules appear as gitlinks in the tree; `read_file` on the submodule path will hit a directory and return a tool error ("is a directory"). `list_files` will list the gitlink entry but not recurse into submodule content. Add a unit test with a fixture submodule to confirm. |

---

**Created:** June 13, 2026
**Status:** AC Generated - Ready for Implementation
