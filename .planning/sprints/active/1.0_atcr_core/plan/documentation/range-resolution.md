# Range Resolution & Git Integration [CRITICAL]

## Overview

The range resolver is the entry point of every `atcr` run. It translates user intent (a flag, a merge commit, or a feature branch) into a concrete `base..head` SHA pair that downstream packages — payload, fan-out, reconcile — consume. The resolver follows a strict decision tree: explicit flags win, then a single merge-commit SHA resolves to its parent, and finally an auto-detection path walks `origin/HEAD` → `origin/main` → `origin/master` → local `main` → local `master` to find a default-branch merge-base. **Empty range (0 commits or base == head) is a hard error** before any provider call — the tool never silently emits a zero-findings report.

> Source: [plan.md:Range Resolution decision tree], [original-requirements.md:Range resolution]

## Key Concepts

### Decision Tree

The resolver evaluates input in this exact order:

1. **Explicit `--base X [--head Y]`** — used as-is; `--head` defaults to `HEAD` when omitted (clarification 2026-06-11). `MarkFlagsMutuallyExclusive` in cobra prevents `--merge-commit` from being combined with `--base`.
2. **`--merge-commit SHA`** — base = `SHA^`, head = `SHA`. The single carry-over from the source system's sprint pipeline.
3. **Auto-detect** — `git merge-base HEAD <default-branch>` where `<default-branch>` is the first existing ref in this list: `origin/HEAD` (via `git symbolic-ref`) → `origin/main` → `origin/master` → local `main` → local `master`.
4. **No match** — hard error with guidance to set a default branch or pass `--base`/`--head` explicitly.

> Source: [plan.md:Range Resolution], [original-requirements.md:Range resolution]

### Empty-Range Hard Error

If `git rev-list --count base..head` returns `0`, the resolver fails immediately with a clear error message — **before** any provider call, payload build, or directory creation. A silent zero-findings pass would falsely clear CI gates. The error message names the resolved SHAs so the user can see exactly what was checked.

> Source: [plan.md:Range Resolution — "Empty range is a hard error before any provider call"], [plan.md:Risk Mitigation]

### Shallow-Clone Guard

`git rev-parse --is-shallow-repository` returns `true` on shallow clones. When the resolver detects this, it produces a hard error with guidance: `error: shallow clone detected; run \`git fetch --unshallow\` and retry`. The resolver does not attempt to unshallow automatically — that's a destructive operation the user must opt into.

> Source: [original-requirements.md:Range resolution — shallow detection]

### Default-Branch Detection

`git symbolic-ref refs/remotes/origin/HEAD` is the first probe. If that fails (no upstream configured, fork clone, etc.), the resolver walks the static fallback list. The list is intentionally short and ordered by observed frequency in real repos; extending it to arbitrary branch names would be a config decision deferred to v2.

> Source: [plan.md:Architecture Notes — auto-detection path]

## Code Examples

### Resolution via `os/exec`

```go
// From .planning/specifications/packages/standard-library.md (os/exec — git interaction):
// exec.CommandContext(ctx, "git", "-C", repo, ...) everywhere,
// so a cancelled run never leaves orphaned git processes.
runGit := func(args ...string) (string, error) {
    cmd := exec.CommandContext(ctx, "git", "-C", repoDir, args...)
    var out, errOut bytes.Buffer
    cmd.Stdout = &out
    cmd.Stderr = &errOut
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, errOut.String())
    }
    return strings.TrimSpace(out.String()), nil
}
```

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction]

### Commands Used by the Resolver

| Git invocation | Purpose |
|----------------|---------|
| `git rev-parse HEAD` | Resolve current branch to a SHA |
| `git symbolic-ref refs/remotes/origin/HEAD` | Probe upstream default branch |
| `git merge-base <head> <default>` | Auto-detect base SHA |
| `git rev-parse --is-shallow-repository` | Shallow-clone guard |
| `git rev-list --count base..head` | Empty-range check (must be ≥1) |
| `git diff base..head` | Unified diff (used by `atcr range` pre-flight and by the `diff` payload mode) |

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction], [plan.md:Range Resolution]

### Resolver Output Shape

The resolver returns a struct consumed by every downstream package:

```go
type Resolution struct {
    Base           string    `json:"base"`            // resolved SHA
    Head           string    `json:"head"`            // resolved SHA
    DetectionMode  string    `json:"detection_mode"`  // "explicit" | "merge_commit" | "auto"
    DefaultBranch  string    `json:"default_branch,omitempty"`
    CommitCount    int       `json:"commit_count"`    // always ≥1; 0 is a hard error
    Shallow        bool      `json:"shallow"`         // informational
    ResolvedAt     time.Time `json:"resolved_at"`
}
```

This struct is written to `manifest.json` so every downstream tool reads provenance from disk rather than re-running the resolver.

> Source: [plan.md:Review Manifest Schema], [codebase-discovery.json:Review Manifest Schema]

### `atcr range` Pre-Flight

`atcr range` runs only the resolver and prints the `Resolution` as JSON. This is the lowest-cost way to debug "why is my range empty?" without paying for a full fan-out. The Skill uses this command at the start of its orchestration loop.

> Source: [plan.md:task 3 — atcr range command], [original-requirements.md:Command surface]

## Quick Reference

| Flag | Resolver behavior |
|------|-------------------|
| `--base X [--head Y]` | Use as-is, mode = `explicit`; `--head` defaults to `HEAD` when omitted (base-only is the CI-gate invocation; clarification 2026-06-11) |
| `--merge-commit SHA` | base = `SHA^`, head = `SHA`, mode = `merge_commit` |
| (no flag) | Auto-detect default branch, mode = `auto` |
| (any case, 0 commits) | Hard error before any provider call |
| (any case, shallow clone) | Hard error with `git fetch --unshallow` guidance |

| Edge case | Behavior |
|-----------|----------|
| `origin/HEAD` not set | Fall through to `origin/main` → `origin/master` → local `main` → `local/master` |
| None of the fallbacks exist | Hard error: `no default branch detected; pass --base and --head explicitly` |
| `--base == --head` | Hard error: `empty range` (0 commits) |
| User is on default branch (no diff) | Hard error: `empty range` (same as above) |
| Fork clone, no `origin` | Local `main` / `master` probes succeed; otherwise hard error |

## Anti-Patterns to Avoid

- **Silently zeroing out an empty range** — produces false CI-gate clears.
- **Using `go-git` instead of `os/exec`** — the spec requires `git diff --function-context` and other features not supported by `go-git`. Adds CGO complexity for no benefit.
- **Building git invocations via `sh -c`** — ref names containing shell metacharacters (rare but possible) become injection vectors. Always pass argv directly.
- **Trimming or rewriting git diff output** — payloads must show reviewers the unmodified diff. Truncation is allowed (recorded), mutation is not.
- **Auto-running `git fetch --unshallow`** — destructive network operation; the user must opt in.

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction], [plan.md:Risk Mitigation]

## Related Documentation

- [Plan Document](../plan.md) — Range resolution decision tree in Technical Planning Notes
- [Original Requirements](../original-requirements.md) — Range resolution section under "Range Resolution"
- [Standard Library Usage](../../../../specifications/packages/standard-library.md) — `os/exec` patterns, never shell -c
- [CLI Architecture](cli-architecture.md) — `MarkFlagsMutuallyExclusive` for `--base` / `--merge-commit`
- [LLM Client & Fan-out](llm-client-fanout.md) — How the resolved range feeds into payload building and parallel/serial lanes
