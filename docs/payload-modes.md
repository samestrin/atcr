# Payload Modes

The **payload** is what a reviewer agent actually sees. It is a first-class, per-agent axis because model capability changes what format works: frontier models read unified diffs fluently; small and low-active-parameter MoE models do markedly better looking at real code with real line numbers.

| Mode | Reviewer sees | Best for |
|------|---------------|----------|
| `blocks` | Changed hunks expanded to the enclosing function/block, with real line numbers (`git diff --function-context`) | **Default.** The sweet spot for small models — readable code, still scoped to the change. |
| `diff` | Unified diff | **The most compact and token-friendly mode.** Right choice for frontier models and large ranges. |
| `files` | Full head-version content of changed files, changed regions marked | Small ranges, audit-style review; highest token cost. |

## Choosing a mode

- **Default is `blocks`** for out-of-the-box quality with mixed rosters. A small model handed a raw unified diff reasons poorly about it; given the enclosing function with real line numbers, its findings improve substantially.
- **Switch to `diff` to save tokens.** This is the explicit token trade-off: `diff` is the most compact representation of a change. When cost or context limits bite — large ranges, many agents, expensive frontier models — `diff` is significantly cheaper for the same change, and a capable model loses little by reading it. `blocks` costs more tokens than `diff` because it includes whole enclosing functions, not just the changed lines; `files` costs the most because it includes entire file bodies.
- **Use `files` for audits.** When you want a reviewer to see a small set of changed files in full (and surface pre-existing issues), `files` gives the widest view at the highest token cost.

Rule of thumb: **`blocks` for quality on small models, `diff` for cost on large ranges/frontier models, `files` for small-range audits.**

## Per-agent override

The payload mode is resolved per agent: an agent's `payload:` field overrides the project/registry/embedded default. One run can therefore mix modes — the frontier model reads the `diff`, the local 8B gets `blocks` — and `manifest.json` records exactly who saw which mode.

```yaml
# ~/.config/atcr/registry.yaml
agents:
  bruce:                    # capable frontier model — cheap diff is fine
    provider: openrouter
    model: anthropic/claude-3.7-sonnet
    payload: diff
  greta:                    # local 8B — real code reads better
    provider: local
    model: qwen-3-8b
    payload: blocks
```

Set the run-wide default in `.atcr/config.yaml` (`payload_mode: blocks`), override per agent in the registry, or override the whole run from the CLI with `atcr review --payload <mode>`. Precedence: CLI flag > project config > registry > embedded default.

## Byte budgets and truncation

Every payload has a byte budget — `payload_byte_budget`, default **524288 bytes (512 KiB)**, configurable with the usual precedence (CLI `--byte-budget` > project config > registry > embedded default). When a payload exceeds its budget, atcr truncates **deterministically** rather than letting a provider silently clip the input:

- Whole files are dropped, **largest-first** by size rank (ties broken by path), keeping as many files as fit within the budget — huge generated files and lockfiles are shed before small source files.
- A budget of **`0` means unlimited** (nothing dropped); a negative budget is rejected at validation.
- Every drop is **recorded in the agent's `status.json`** — what was dropped and why is never silent.

## Changed-region markers (`files` mode)

In `files` mode the reviewer sees each changed file's full head-version content, with the changed regions delimited by sentinel lines so the model can find the change inside the whole file:

```
>>> CHANGED LINES 42-58
<the changed lines>
<<< END CHANGED
```

Special files are represented by one-line markers instead of full content:

- Deleted file → `[deleted file: <path>]`
- Binary file → `[binary file changed: <path>]`
- Renamed file → shown under its new path, with the rename noted.

## Scope rules

Each persona prompt carries a scope rule matched to the payload mode:

- **`diff` and `blocks`** constrain findings to the changed regions. Function-context expansion shows surrounding code for context but does **not** widen the review scope.
- **`files`** intentionally widens visibility. Reviewers may notice pre-existing issues in unchanged regions; the prompt instructs them to focus on the change but to tag any pre-existing issue with `CATEGORY` `out-of-scope`, so the reconciler **annotates** rather than promotes it. Consumers can then filter out-of-scope findings.

### Grounding gate

The scope rule is enforced, not merely requested. After a persona returns its findings — and before they reach the reconciler — atcr drops any finding whose cited `FILE:LINE` is not anchored in the patch's changed lines. A finding is kept when its line falls within a changed range (with a small ±3-line tolerance for reviewer drift), when its `EVIDENCE` text matches a changed line, or when it is tagged `CATEGORY` `out-of-scope` (which stays exempt so the annotate-don't-promote path above is unaffected). Ungrounded findings — the hallucinations a model invents for code it never saw change — are discarded and the per-agent drop count is logged to stderr. The gate needs the live diff, so it applies to `atcr review`; it is disabled for the range-less `atcr reconcile <dir>` path, which has no patch to check against.

## Tool agents (payload as starting point)

For an agent with `tools: true` (see [registry.md](registry.md)), the payload above is the **starting point** of the review, not the whole universe of context. Rather than reasoning only over what the payload contains, a tool agent can look things up during the review: it may read additional files with `read_file`, search the tree with `grep`, and list directories with `list_files`, all within a **read-only, path-jailed snapshot** of the repository at the resolved `head`. There are no write tools, no shell, and no network — a reviewer can never mutate the repo or reach beyond the snapshot.

This widens **evidence gathering**, not **review scope**. A tool agent still targets the changed range exactly as a single-shot reviewer does: findings must concern the change unless the agent explicitly tags a pre-existing issue in unchanged code with `CATEGORY` `out-of-scope` (the same convention as `files` mode above). Tools exist so a reviewer can verify a suspicion — read the caller that passes `nil`, confirm the invariant two packages away — and cite the file and lines it actually read, not to expand what counts as in-scope.

Tool agents are bounded by per-agent budgets (`max_turns`, `tool_budget_bytes`, `timeout_secs`) and typically cost several times the provider calls of a single-shot reviewer; see the [README](../README.md) for cost guidance and [registry.md](registry.md) for the budget fields.
