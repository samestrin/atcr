# Payload Engine & Three Modes [CRITICAL]

## Overview

The payload engine assembles the input that each reviewer agent sees. Three modes cover the spectrum from token-frugal to audit-style: **`diff`** (unified diff, most compact, ideal for frontier models and large ranges), **`blocks`** (changed hunks expanded to the enclosing function/block via `git diff --function-context`, the **default** for small and MoE models that read code better than diffs), and **`files`** (full head-version content of changed files with changed regions marked, the highest token cost, suited to small ranges or audit-style review).

Each run can mix payload modes per-agent via the registry's `payload: <mode>` override on a specific agent. The chosen mode per agent is recorded in `manifest.json` so post-hoc analysis can see who saw what. Per-payload byte budgets with deterministic truncation (drop whole files by size rank) ensure token limits are never silently exceeded; anything dropped is recorded in the agent's `status.json`.

> Source: [plan.md:Payload Modes], [original-requirements.md:Payload modes (per-agent reviewer input)]

## Key Concepts

### Three Modes

| Mode | What the reviewer sees | Token cost | Default? | Best for |
|------|------------------------|------------|----------|----------|
| `blocks` | Changed hunks expanded to the enclosing function/block with real line numbers (via `git diff --function-context` baseline) | Medium | **Yes** | Small models and MoE models that read code better than diffs |
| `diff` | Unified diff | Lowest | No (override) | Frontier models, large ranges, token-frugal runs |
| `files` | Full head-version content of changed files with changed regions marked | Highest | No (override) | Small ranges, audit-style review where context matters more than cost |

> Source: [plan.md:Payload Modes table], [original-requirements.md:Payload modes]

### Default is `blocks`

`blocks` is the default in `.atcr/config.yaml`. The `diff-vs-blocks` tradeoff is explicit in the original requirements: small MoE models perform better on real code than on unified diffs, so the default favors model quality over token cost. The plan acknowledges the trade-off and explicitly states `diff` is the most compact/token-friendly mode for large ranges — recommending it via documentation rather than changing the default.

> Source: [plan.md:Payload Modes table — Default], [original-requirements.md:Payload modes]

### Per-Agent Overrides

A single `atcr review` run can mix payload modes. The registry's `payload: <mode>` field on an agent overrides the project default. Example `~/.config/atcr/registry.yaml`:

```yaml
agents:
  bruce:
    provider: openai
    model: gpt-4o
    payload: diff          # frontier model on a tight budget
  otto:                    # uses project default (blocks)
    provider: anthropic
    model: claude-3-5-sonnet
    # no payload field -> inherits blocks
  mira:
    provider: ollama
    model: qwen2.5-coder:7b
    payload: blocks        # explicit override
```

The `manifest.json` records `payload_mode` per agent so a re-run with different overrides is reproducible from the manifest alone.

> Source: [plan.md:Per-agent payload override], [original-requirements.md:Per-agent payload override]

### Byte Budgets and Deterministic Truncation

Each payload build accepts a byte budget (per agent or globally). When the constructed payload exceeds the budget, the engine **deterministically** drops whole files by size rank (largest first) until the payload fits. The dropped files are recorded in the agent's `status.json`:

```json
{
  "agent": "bruce",
  "status": "ok",
  "duration_ms": 12450,
  "payload_mode": "blocks",
  "truncated_files": [
    "vendor/large-generated.go",
    "fixtures/big-fixture.json"
  ],
  "truncation_reason": "byte_budget_exceeded"
}
```

This is **never silent**. The reviewer always knows what it didn't see, and the reconcile step can flag findings on truncated files as "out of scope for this reviewer."

> Source: [plan.md:Per-payload byte budget with deterministic truncation], [plan.md:Risk Mitigation — Byte budgets with recorded truncation]

### `blocks` Edge Cases and Fallback

`git diff --function-context` does not work for every language or file type. The engine has explicit fallbacks for:

| Edge case | Fallback |
|-----------|----------|
| Languages without braces (Python, Ruby, Haskell) | Heuristic: detect function header via indentation or `def`/`fn` keywords |
| Generated files (`*.pb.go`, `*_generated.go`) | Skip expansion; emit plain `-U<n>` context diff |
| Renames | Follow the rename; use the new path's function context |
| Binary files | Emit a "binary file changed" marker; no expansion |
| `--function-context` fails on a file | Fall back to plain `-U<10>` context diff for that file only |

> Source: [plan.md:Risk Mitigation — blocks payload builder edge cases], [original-requirements.md:Risk Mitigation]

### `files` Mode and Out-of-Range Findings

`files` mode shows the reviewer the **entire** current head version of changed files. The risk: a reviewer finds a pre-existing bug in a file that's not part of the diff. The persona prompts include a per-payload scope rule that tells the reviewer:

> "When reviewing in `files` mode, surface only findings on changed regions. Pre-existing issues in unchanged regions of changed files are out of scope for this review and should be noted in `findings.txt` with category `out-of-scope` (handled separately)."

The reconciler honors this scope rule and annotates any `out-of-scope` findings in the report so the user can decide whether to track them.

> Source: [plan.md:Risk Mitigation — files mode produces out-of-change findings]

## Code Examples

### Building a `diff` Payload

```go
// From .planning/specifications/packages/standard-library.md (os/exec):
// Diff output is written to disk verbatim (no trimming) so reviewers and
// the payload builder see unmodified git output.
func BuildDiff(ctx context.Context, repo, base, head string) (string, error) {
    cmd := exec.CommandContext(ctx, "git", "-C", repo, "diff", base+".."+head)
    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil { return "", err }
    return out.String(), nil
}
```

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction]

### Building a `blocks` Payload

```go
// git diff --function-context expands each hunk to the enclosing function
// header. Fall back to plain -U<10> for files where function-context fails.
func BuildBlocks(ctx context.Context, repo, base, head string) (string, error) {
    primary := exec.CommandContext(ctx, "git", "-C", repo,
        "diff", "--function-context", base+".."+head)
    var out bytes.Buffer
    primary.Stdout = &out
    if err := primary.Run(); err == nil {
        return out.String(), nil
    }
    // Fallback: plain context diff
    fallback := exec.CommandContext(ctx, "git", "-C", repo,
        "diff", "-U", "10", base+".."+head)
    fallback.Stdout = &out
    if err := fallback.Run(); err != nil {
        return "", err
    }
    return out.String(), nil
}
```

> Source: [plan.md:Payload engine — blocks], [plan.md:Risk Mitigation — Fall back to plain -U<n>]

### Building a `files` Payload

```go
// For each file in the diff, emit the head version with changed regions
// marked via sentinel comments. This is the highest-token mode; byte budget
// is most likely to truncate here.
func BuildFiles(ctx context.Context, repo, base, head string) (string, error) {
    files := changedFiles(ctx, repo, base, head)
    var b strings.Builder
    for _, f := range files {
        b.WriteString(fmt.Sprintf("=== FILE: %s ===\n", f))
        contents, err := readHeadVersion(ctx, repo, f)
        if err != nil { return "", err }
        // Sentinel comment marks the changed line ranges
        for lineNo, line := range strings.Split(contents, "\n") {
            if isInChangedRange(f, lineNo) {
                b.WriteString(fmt.Sprintf("%d| %s\n", lineNo+1, line))
            } else {
                b.WriteString(fmt.Sprintf("%d  %s\n", lineNo+1, line))
            }
        }
        b.WriteString("\n")
    }
    return b.String(), nil
}
```

> Source: [plan.md:Payload engine — files]

### Byte-Budget Truncation

```go
// Deterministic: drop largest files first until payload fits.
type fileEntry struct {
    path string
    size int
    body string
}

func ApplyByteBudget(entries []fileEntry, budget int) (kept []fileEntry, dropped []string) {
    sort.Slice(entries, func(i, j int) bool {
        return entries[i].size > entries[j].size  // largest first
    })
    var used int
    for _, e := range entries {
        if used+e.size > budget {
            dropped = append(dropped, e.path)
            continue
        }
        kept = append(kept, e)
        used += e.size
    }
    return kept, dropped
}
```

> Source: [plan.md:Per-payload byte budget with deterministic truncation]

## Quick Reference

| Mode | Generator | Fallback | Default? |
|------|-----------|----------|----------|
| `diff` | `git diff base..head` (verbatim) | None needed | No |
| `blocks` | `git diff --function-context` | `git diff -U10` per file | **Yes** |
| `files` | Head version of changed files with changed-region sentinels | Drop file from payload; record in status.json | No |

| Configuration | Where | Effect |
|---------------|-------|--------|
| `payload: <mode>` per agent | `~/.config/atcr/registry.yaml` agents.*.payload | Per-agent override |
| Default payload mode | `.atcr/config.yaml` | Project default (`blocks`) |
| `--payload <mode>` CLI flag | cobra command | Per-run override |
| Byte budget | per-agent or global | Truncation policy |

| Edge case | Behavior |
|-----------|----------|
| Binary file in `blocks`/`files` | Emit "binary file changed" marker; no expansion |
| Generated file (`*.pb.go`) | Plain `-U<10>` context diff; no function expansion |
| Rename detected | Follow to new path; expand function context at new path |
| Out-of-range finding in `files` mode | Annotate as `out-of-scope` in report; not promoted to reconciled finding |
| Truncation triggered | Record in `status.json.truncated_files`; never silent |

## Anti-Patterns to Avoid

- **Defaulting to `diff` for small models** — they read code better than diffs. `blocks` is the default for a reason.
- **Silently dropping content from payloads** — every drop is recorded in `status.json`. Silence defeats the audit trail.
- **Mutating git diff output** — the payload builder shows reviewers exactly what `git diff` produced. Truncation is allowed; mutation is not.
- **Treating `files` mode as "more thorough" without scope rules** — without per-payload scope rules, every pre-existing bug in every changed file becomes a "finding." That pollutes reconciliation.
- **Hard-coding a byte budget in code** — budgets are configuration. Defaults are sensible; project-level overrides should be possible.

> Source: [plan.md:Risk Mitigation], [plan.md:Per-payload scope rules in personas]

## Related Documentation

- [Plan Document](../plan.md) — Payload engine in Implementation Strategy (task 4)
- [Original Requirements](../original-requirements.md) — Payload modes section under "Payload modes (per-agent reviewer input)"
- [Configuration Management](configuration-management.md) — How `payload: <mode>` is loaded from registry.yaml
- [Range Resolution](range-resolution.md) — Payload builders consume the resolved `base..head` range
- [LLM Client & Fan-out](llm-client-fanout.md) — How built payloads are sent to the LLM client
- [Codebase Discovery](../codebase-discovery.json) — `internal/payload/builder.go` mapped in `files_to_create`
