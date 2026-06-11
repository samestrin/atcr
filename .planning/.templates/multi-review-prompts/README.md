# multi-review prompt templates

Per-agent task message templates for `llm-support multi_review`. Each file
is a Go `text/template` rendered at runtime by the binary's
`LoadAgentPrompt` (in `llm-tools/internal/support/multireview/prompt_loader.go`).

## Files

| File | Purpose |
|---|---|
| `_base.md` | Universal base. Used by any reviewer that doesn't have a per-agent file. Mirrors the binary's hardcoded `buildDefaultTaskMessage` content. |
| `<agent>.md` | Per-agent override (e.g. `bruce.md`, `kai.md`). Whole-file replacement — the binary does NOT compose `_base.md` + overlay. Each agent file is self-contained. |

Currently shipped agents: `bruce.md`, `greta.md`, `kai.md`, `mira.md`, `dax.md`, `otto.md`.

## Fallback chain at runtime

When `multi_review` invokes reviewer `<agent>`:

1. **`--task-message` CLI flag** (explicit user override, wins for ALL agents).
2. **`<agent>.md`** in `~/.llm-tools/multi-review/prompts/` (set up by `update-prompts.sh`).
3. **`_base.md`** in the same dir.
4. **Hardcoded `buildDefaultTaskMessage`** in the binary (last-resort fallback so fresh installs without the templates still work).

## Template variables

Templates can reference these `PromptVars` fields:

| Field | Type | Description |
|---|---|---|
| `{{.DiffPath}}` | string | Absolute path to the pre-computed diff inside the container, e.g. `/tmp/multi-review-1778868610/diff.txt` |
| `{{.DiffBytes}}` | int64 | Byte count from `wc -c` |
| `{{.DiffLines}}` | int | Line count from `wc -l` |
| `{{.DiffMB}}` | float64 | `DiffBytes / 1_000_000`, pre-computed for display |
| `{{.LargeDiff}}` | bool | `true` when `DiffBytes > 1_000_000`. Use `{{if .LargeDiff}}...{{end}}` to gate the directive workflow. |
| `{{.BaseRef}}` | string | Base ref for the diff range |
| `{{.HeadRef}}` | string | Head ref for the diff range (defaulted to `HEAD` if empty) |
| `{{.RemoteRepo}}` | string | Container-local clone path the reviewer can inspect, e.g. `/tmp/multi-review-1778868610/myrepo` |
| `{{.AgentName}}` | string | The reviewer's name (bruce/greta/kai/...). Useful for self-reference. |

## How each agent file is structured

The convention (not enforced by the loader) is:

```
1. Standard intro       (where the diff is, instructions, anti-hallucination clause)
2. {{if .LargeDiff}}    (directive >1MB workflow — same shape across all agents)
3. ## Your lane         (1-paragraph lens echoing the agent's SOUL specialization)
4. ## Operational rules (model-specific failure-mode compensation — kai gets the tool-call budget, otto gets severity honesty, etc.)
5. Standard output      (verdict, findings, TD_STREAM format with pipe-escape note)
```

The lens block reinforces the agent's SOUL specialization for THIS task; the
operational rules compensate for known failure modes observed in production.

## Adding a new reviewer

1. Decide the agent name (lowercase, no extension).
2. Create `<agent>.md` in this directory.
3. Either copy `_base.md` as a starting point and add a "## Your lane" +
   "## Operational rules" section, OR (if the agent is a generic reviewer
   with no quirks) skip this file entirely — the agent will use `_base.md`.
4. Run `./update-prompts.sh` from the repo root to sync to
   `~/.llm-tools/multi-review/prompts/`.

## Caveats

### SOUL drift
The agent's SOUL on the openclaw server (`nucleus.lan`,
`/home/node/.openclaw/workspace-<agent>/SOUL.md`) sets the agent's permanent
identity. The "## Your lane" block in this prompt repeats SOUL keywords as a
recall trigger but is NOT the source of truth for the agent's specialty.
**If you update a SOUL, also update the corresponding `<agent>.md` lens block
here** — the two should tell the same story.

### Template syntax
Go `text/template` uses `{{ }}` for variables and actions. Literal text is
passed through unchanged — backticks, dollar signs, single/double quotes, etc.
are all safe. The ONLY characters that need escaping are literal `{{` or `}}`
in the rendered output (escape as `{{"{{"}}` and `{{"}}"}}` respectively).
None of the current templates need this.

### Resilience
If a per-agent file has a template syntax error OR an execution error (missing
field, bad pipeline), the loader silently falls through to `_base.md`. If
`_base.md` is ALSO broken, it falls through to the binary's hardcoded default.
A single broken per-agent file does NOT crash a `multi_review` run, but
broken files DO mean that agent reverts to the universal prompt — which is
usually not what you want. **Test locally before pushing**:

```bash
./update-prompts.sh                # sync templates to ~/.llm-tools/multi-review/prompts/
llm-support multi_review --help    # smoke check the binary still runs
# Then run an actual review and check raw/<agent>/response.json contains the
# expected per-agent task-message content.
```
