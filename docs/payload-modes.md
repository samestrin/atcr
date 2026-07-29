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

## Automatic per-file escalation

The mode you configure is a **floor, not a ceiling**. A file whose change is structurally confusing is promoted above the configured mode automatically, on its own, while every other file in the same run stays where you put it. This keeps the token trade-off you chose for the bulk of a change while spending more only where a diff genuinely cannot be read correctly.

Two independent signals are scored per file:

| Signal | Fires when | Setting | Default |
|--------|-----------|---------|---------|
| Churn ratio | Changed lines (`max(added, deleted)`, **not** their sum — a moved block is not double-counted) ÷ HEAD line count reaches the threshold | `churn_ratio` | `0.5` |
| Hunk count | The file has at least this many separate hunks | `min_hunks` | `8` |
| Hunk adjacency | Two hunks sit closer than this many unchanged lines apart | `hunk_gap_lines` | `2` |
| Cyclomatic complexity | The most complex **function the diff changed** reaches this McCabe score (branch nodes + 1) | `min_cyclomatic` | `20` |

The first three are **diff-native** — they need no parsing and target the case this feature exists for: a branch that implements something, then rewrites it in a later commit, leaving a net diff whose shape matches neither version. The fourth is the standard **McCabe** measure, read from the same AST parse that produces the skeleton below. It is measured **per function**, not summed across the file — a long file of simple functions is not complex, and thresholding on a whole-file total would escalate almost every real source file. It is also scoped to the functions the diff **touched**: a one-line edit to a trivial helper does not escalate because some unrelated function elsewhere in the file is branchy.

These defaults were tuned against a measured replay of real history rather than chosen by feel, and they ship with an explicit acceptance target: **15–25% of changed Go files promoted, at ≤ +40% payload bytes** over the configured mode. Over the repo's last 40 commits (400 changed Go files) they measure **21.5% promoted at +34% bytes**, down from 37.0% / +48% before tuning — escalation stays the exception it is meant to be. The measurement harness lives in `internal/payload/escalation_replay_test.go` and is opt-in:

```bash
ATCR_REPLAY=1 ATCR_REPLAY_REF=main ATCR_REPLAY_COMMITS=40 \
  go test ./internal/payload/ -run TestEscalationReplay -v
```

It reports the overall and per-signal promotion rates plus the payload-byte delta, and a companion sweep reports what candidate threshold sets would have measured over the same window. Re-run it before changing any threshold above: damping one signal shifts load onto the others, which is only visible by re-measuring all of them.

Escalation is a ladder:

- **Either** family of signals firing promotes the file `diff` → `blocks`.
- **Both** firing promotes it to `files`.
- A file already at `blocks` can only move up to `files`; a run configured for `files` never de-escalates.

Simple changes are unaffected: a one-line edit in a large file trips nothing and stays in `diff`.

### AST skeleton injection

For files reviewed in `diff` or `blocks` mode, atcr prepends a compact structural map of the file **as it exists at `head`** — one line per top-level declaration, with its line number:

```
>>> SKELETON (HEAD) <<<
L14: type Handler struct
L23: func Register(h Handler) error
L41: func Dispatch(kind string, n int) int
>>> END SKELETON <<<
```

This is the cheap fix for the failure it names: reading a multi-commit diff, a model reconstructs the file by mentally applying the patch and frequently lands on a superseded intermediate state. The skeleton removes the guesswork — the final architecture is stated outright, at a cost of a few dozen tokens. `files` mode gets no skeleton, since it already carries the whole file.

A skeleton renders at most `max_skeleton_lines` headers (default 60); beyond that it is truncated with an explicit `... N more declaration(s) elided` line, so a generated file with hundreds of declarations cannot swamp a one-line diff.

Skeletons are currently extracted for **Go** files only. Other languages are reviewed exactly as before.

### Cost cap

Each analyzed file costs one `git show` plus one AST parse, and that cost is paid once per distinct payload mode built for the roster — at most twice, since files mode skips analysis — on one shared range builder before fan-out, not once per parallel agent (`headContentMemo` memoizes the `git show` per range and the resulting entries are reused for every agent). Above `max_files` changed files (default **50**) the entire feature — escalation and skeletons — is skipped, every file renders in the configured mode, a warning is logged, and `manifest.json` records `escalation_degraded: true`. That distinguishes "nothing was complex enough to escalate" from "escalation never ran".

### Configuration

Thresholds are **global**, set in the registry only — there is no project-config mirror and no CLI flag, matching how `verify:` and `debate:` are configured:

```yaml
# ~/.config/atcr/registry.yaml
payload_escalation:
  churn_ratio: 0.5
  min_hunks: 8
  hunk_gap_lines: 2
  min_cyclomatic: 20
  max_files: 50
  max_skeleton_lines: 60
```

Every field is optional and falls back to its default. Setting any threshold to `0` disables that one signal; setting `max_files: 0` turns per-file escalation and skeleton injection off entirely.

## Per-agent override

The payload mode is resolved per agent: an agent's `payload:` field overrides the project/registry/embedded default. One run can therefore mix modes — the frontier model reads the `diff`, the local 8B gets `blocks` — and `manifest.json` records exactly who saw which mode in `per_agent_payload`. Files promoted by [automatic escalation](#automatic-per-file-escalation) are recorded separately in `per_file_payload`, so the manifest distinguishes what was *configured* from what a reviewer *actually saw* per file.

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

A persona prompt carries exactly one scope rule, but per-file escalation can mix modes inside a single payload. When **any** file in a payload was rendered in `files` mode, that whole payload gets the wider `files` rule — a reviewer holding full file bodies must not be told to stay on the diff. The narrower direction is never taken: escalation only ever widens the rule, never tightens it.

### Grounding gate

The scope rule is enforced, not merely requested. After a persona returns its findings — and before they reach the reconciler — atcr drops any finding whose cited `FILE:LINE` is not anchored in the patch's changed lines. A finding is kept when its line falls within a changed range (with a small ±3-line tolerance for reviewer drift), when its `EVIDENCE` text matches a changed line, or when it is tagged `CATEGORY` `out-of-scope` (which stays exempt so the annotate-don't-promote path above is unaffected). Ungrounded findings — the hallucinations a model invents for code it never saw change — are discarded and the per-agent drop count is logged to stderr. The gate needs the live diff, so it applies to `atcr review`; it is disabled for the range-less `atcr reconcile <dir>` path, which has no patch to check against.

## Tool agents (payload as starting point)

For an agent with `tools: true` (see [registry.md](registry.md)), the payload above is the **starting point** of the review, not the whole universe of context. Rather than reasoning only over what the payload contains, a tool agent can look things up during the review: it may read additional files with `read_file`, search the tree with `grep`, and list directories with `list_files`, all within a **read-only, path-jailed snapshot** of the repository at the resolved `head`. There are no write tools, no shell, and no network — a reviewer can never mutate the repo or reach beyond the snapshot.

This widens **evidence gathering**, not **review scope**. A tool agent still targets the changed range exactly as a single-shot reviewer does: findings must concern the change unless the agent explicitly tags a pre-existing issue in unchanged code with `CATEGORY` `out-of-scope` (the same convention as `files` mode above). Tools exist so a reviewer can verify a suspicion — read the caller that passes `nil`, confirm the invariant two packages away — and cite the file and lines it actually read, not to expand what counts as in-scope.

Tool agents are bounded by per-agent budgets (`max_turns`, `tool_budget_bytes`, `timeout_secs`) and typically cost several times the provider calls of a single-shot reviewer; see the [README](../README.md) for cost guidance and [registry.md](registry.md) for the budget fields.
