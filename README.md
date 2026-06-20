# atcr — Agent Team Code Review

> A review panel, not a reviewer.

**Website:** [atcr.dev](https://atcr.dev)

**atcr** fans a code change out to a panel of heterogeneous LLM reviewer personas — different models, different providers, different lenses — then deterministically reconciles their findings into a single deduplicated, confidence-scored report. Cross-model agreement drives the confidence signal: a finding two independent models both caught is worth more than either model's opinion alone.

One Go binary, three faces: a **CLI**, an **MCP server** (`atcr serve`) over the same engine, and a companion **Agent Skill** that contributes a host-model review as the "+1" reviewer so even a single API key yields 2+ sources and a working confidence signal.

## Why

Existing LLM review tools are single-model, single-vendor, mostly SaaS. Anyone who manually fans a diff out to N models gets N walls of prose and no way to merge them. atcr is local-first and BYO-keys, and the merge is the product: cluster by location, dedupe by similarity, score confidence by reviewer agreement, preserve disagreements instead of flattening them.

The deterministic Go reconciler — cluster → dedupe → merge → confidence — is the core value-add. Prompts orchestrate; the binary does everything that must be reproducible. Every cross-stage handoff is a machine-parseable file on disk.

## How it works

```
                 ┌──────────────────────────────────────────────┐
                 │   atcr review                                │
                 │   range → payload → fan-out to persona pool  │
                 │   (parallel/serial lanes, fallbacks, budgets)│
                 └──────────────────┬───────────────────────────┘
                                    v
   .atcr/reviews/<id>/sources/{pool,host,...}/findings.txt
                                    v
                 ┌──────────────────────────────────────────────┐
                 │   atcr reconcile                             │
                 │   discover → cluster → dedupe → confidence   │
                 └──────────────────┬───────────────────────────┘
                                    v
       reconciled/{findings.txt, findings.json, report.md}
```

## Quickstart

```bash
# 1. Install (Go 1.24+)
go install github.com/samestrin/atcr/cmd/atcr@latest

# 2. Scaffold project config + the six editable personas into .atcr/
atcr init

# 3. Point a provider + agents at an OpenAI-compatible endpoint
#    (~/.config/atcr/registry.yaml — see docs/registry.md)
export OPENROUTER_API_KEY=sk-...

# 4. Verify every configured endpoint before spending a real review on it
atcr doctor

# 5. Run the panel on the current feature branch, then reconcile — zero arguments
atcr review && atcr reconcile

# 6. Read the report
atcr report --format md
```

`atcr doctor` is the recommended post-`atcr init` verification step: it invokes every configured model endpoint once with a trivial prompt and reports any misconfigured provider, model, key, or base URL — so a bad config is caught in seconds instead of mid-review. See [Commands](#commands) for its flags and exit codes.

`atcr review` resolves the range against the default branch, fans the change out to the roster, and records the review id in `.atcr/latest`. Every later command takes an id or path as its single anchor argument and defaults to `latest`, so the two-command pipeline above just works on a feature branch.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr review` | Resolve the git range, build payloads, fan out to the reviewer pool, write per-agent + merged findings |
| `atcr reconcile` | Discover sources, cluster, dedupe, score confidence, write reconciled artifacts |
| `atcr report` | Render md / json / checklist views over the reconciled findings |
| `atcr range` | Pre-flight base..head resolution only; prints resolution JSON |
| `atcr status` | Print a review's fan-out progress as JSON (roster + per-agent state) |
| `atcr init` | Write `.atcr/config.yaml` and the six default personas (editable) |
| `atcr serve` | Run the MCP stdio server over the same engine |
| `atcr doctor` | Self-test every configured endpoint (dedup'd by provider+model+base_url, fallbacks included); per-agent table or `--json`, with a `SOURCE` (user/project) provenance column |
| `atcr trust` | Authorize project-defined providers from `.atcr/registry.yaml` before they can receive a key |

Key flags:

- `atcr review --base X --head Y` / `--merge-commit SHA` / `--id <id>` / `--output-dir <path>` (write the tree to an explicit path; see below) / `--payload diff|blocks|files` / `--timeout <secs>` / `--fail-on <severity>` (one-shot review + reconcile + gate) / `--resume <latest\|id\|path>` (finish an interrupted/failed review by running only its pending agents, then reconcile; see below) / `--force` (overwrite an existing `--id` or `--output-dir` collision, backing the prior tree up to `<dir>.bak` first; mutually exclusive with `--resume`)
- `atcr reconcile --fail-on <severity>` / `--sources <a,b>` (restrict to named source dirs)
- `atcr report --format md|json|checklist` / `--output <file>` / `--disagreements` (focused disagreement-radar view — see [docs/disagreement-radar.md](docs/disagreement-radar.md))
- `atcr doctor` / `--json` / `--max-tokens <n>` (default 2048, high enough for thinking models) / `--timeout <secs>` (default 60) / `--agents <a,b>` (test a subset of listed agents; their fallback chains are still probed). Exit **0** when every agent has a working invocation path (primary or fallback), **1** when any agent has none, **2** for usage/config errors.

### Redirecting output for orchestrators (`--output-dir`)

By default `atcr review` writes the review tree to `.atcr/reviews/<id>/` and points `.atcr/latest` at it — the right default for interactive use. An external orchestrator (a skill, CI step, or wrapper script) that needs the output at a specific location can pass `--output-dir <path>` instead:

```bash
atcr review --output-dir ./artifacts/review        # full tree (manifest.json, payload/, sources/) lands here
atcr reconcile ./artifacts/review                   # reconcile + report take the same path as their anchor
atcr report ./artifacts/review --format md
```

- The tree is written verbatim to `<path>` (relative paths resolve against the current directory). The path must be new or empty — a non-empty directory is rejected with exit **2** so existing content is never clobbered.
- `.atcr/latest` is **not** updated, so `--output-dir` runs never disturb the interactive pointer.
- `--output-dir` and `--id` are mutually exclusive (the id is meaningless when the path is explicit).
- `atcr reconcile` and `atcr report` need no extra flag — they already accept a filesystem path as their `[id-or-path]` argument, so hand them the same `--output-dir` path.

### Resuming an interrupted review (`--resume`)

When a review is interrupted (Ctrl-C/SIGINT) or some agents fail, the completed agents' results are already on disk. `--resume` finishes the run by fanning out **only** the agents that did not complete, then reconciles — so you never re-spend tokens on agents that already produced a result:

```bash
atcr review --resume latest        # resolve .atcr/latest
atcr review --resume <id>          # a review id under .atcr/reviews/
atcr review --resume ./path        # an explicit review directory
```

- The panel is locked: resume re-resolves the current git range and compares it (plus the configured roster) against the interrupted run's `manifest.json`. A changed range or roster aborts with exit code **2** — resuming against changed code or a different panel would mix inconsistent results, so start a fresh `atcr review` instead.
- An agent counts as complete only when its per-agent `status.json` records `ok` (a clean reviewer that found nothing is complete; a failed/timed-out one is re-run). Pass the same range flags (`--base`/`--head`/`--merge-commit`) the original review used so the range matches.
- If every agent already completed, resume just re-runs reconciliation. `--resume` cannot be combined with `--id` or `--output-dir`.
- Re-running an explicit `--id` (or a non-empty `--output-dir`) whose directory already exists is rejected; the error names the two ways forward — `--resume <id>` to continue it non-destructively, or `--force` to back the prior tree up to `<dir>.bak` and start fresh. `--resume` and `--force` are mutually exclusive (opposite collision resolutions).

## Payload modes

`atcr` ships three payload modes that control what each reviewer agent sees. The default is `blocks`; set per-agent overrides in `~/.config/atcr/registry.yaml` when a model handles a different format better.

## Project-defined providers and agents

A repo can ship its own providers and agents in `.atcr/registry.yaml`, overlaying the user registry so a clone is self-contained — project entries shadow same-named user entries whole; new names are added. Because a project-defined provider could direct a key to an arbitrary endpoint, atcr gates them: run `atcr trust` to authorize a project provider (it pins the `base_url` + `api_key_env` pair) before any review or `atcr doctor` will use it. See [docs/registry.md](docs/registry.md#project-registry-overlay).

| Mode | What the reviewer sees | When to use |
|------|------------------------|-------------|
| `diff` | Unified diff | **The most compact and token-friendly mode.** Right choice for frontier models and large ranges. |
| `blocks` | Changed hunks expanded to the enclosing function/block (`git diff --function-context`), with real line numbers | **Default for v1.** Best findings quality from small / MoE models reading real code. |
| `files` | Full head-version content of changed files with changed regions marked | Highest token cost. Audit-style review of small ranges. |

One run can mix payloads — the frontier model reads the `diff`, the local 8B gets `blocks` — and `manifest.json` records who saw what. See [docs/payload-modes.md](docs/payload-modes.md) for the decision guide, byte-budget truncation, and per-mode scope rules.

## Tool-using reviewers (cost guidance)

Set `tools: true` on a function-calling-capable agent to turn it from a single-shot reviewer into a bounded, multi-turn **tool-using agent**: it can `read_file`, `grep`, and `list_files` across a read-only, path-jailed snapshot of the repo to verify a suspicion before reporting it. The payload becomes the starting point of the review, not the whole picture — see [docs/payload-modes.md](docs/payload-modes.md).

Tool agents are not free. A tool-using reviewer **typically consumes 3-10× the provider calls** of a single-shot reviewer (one call per turn, plus the final answer), so cost scales with how much exploration the model does. Each agent is bounded by per-agent budgets — `max_turns`, `tool_budget_bytes`, and `timeout_secs` — documented in [docs/registry.md](docs/registry.md). Enable `tools` selectively (your strongest models, your highest-value lanes) rather than across the whole roster, and tune the budgets to cap spend. A `tools: true` agent on a model without `supports_function_calling: true` degrades cleanly to the single-shot path.

## CI integration

atcr is a PR gate with no glue code: `--fail-on <severity>` returns a nonzero exit when any finding at or above the threshold survives reconciliation.

```bash
atcr review && atcr reconcile --fail-on high   # exit 1 if HIGH+ findings survive
```

Exit codes: **0** success · **1** failure (including a `--fail-on` threshold violation) · **2** usage or configuration error.

GitHub Actions:

```yaml
name: atcr review
on: [pull_request]
jobs:
  atcr:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0          # full history: atcr needs the merge-base
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go install github.com/samestrin/atcr/cmd/atcr@latest
      - name: atcr gate
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
        run: |
          atcr review --base "origin/${{ github.base_ref }}"
          atcr reconcile --fail-on high
```

A ready-to-adapt script lives at [examples/ci-gate.sh](examples/ci-gate.sh). Shallow checkouts (`fetch-depth: 1`) break merge-base resolution; atcr detects this and errors with `git fetch --unshallow` guidance rather than producing a wrong range. See [docs/ci-integration.md](docs/ci-integration.md).

## Providers

atcr speaks to any OpenAI-compatible `/chat/completions` endpoint directly — no SDKs, no infrastructure, keys from environment variables resolved at invoke time. For maximum compatibility across providers, routing through a normalizing proxy such as [LiteLLM](https://github.com/BerriAI/litellm) is supported but not required. See [docs/providers.md](docs/providers.md).

## Documentation

- [docs/providers.md](docs/providers.md) — direct vs. proxy setups, normalization guidance
- [docs/registry.md](docs/registry.md) — providers, personas, agents, fallbacks, lanes, precedence
- [docs/payload-modes.md](docs/payload-modes.md) — blocks vs. diff vs. files, token guidance
- [docs/findings-format.md](docs/findings-format.md) — the versioned `atcr-findings/v1` contract
- [docs/disagreement-radar.md](docs/disagreement-radar.md) — the disagreement radar, `--disagreements` view, and `disagreements.json` handoff schema
- [docs/ci-integration.md](docs/ci-integration.md) — exit codes and PR gates
- [docs/skill-usage.md](docs/skill-usage.md) — installing and running the Agent Skill
- [docs/metrics.md](docs/metrics.md) — metric catalog, end-of-review CLI summary, and the `atcr_metrics` MCP tool

## Repository layout

- `cmd/atcr/` — binary entry point and subcommands
- `internal/` — engine packages (`gitrange`, `payload`, `registry`, `llmclient`, `fanout`, `stream`, `reconcile`, `report`, `mcp`)
- `personas/` — the six embedded default personas + `_base.md`
- `skill/` — the atcr Agent Skill (host review + orchestration)
- `docs/` — user documentation
- `examples/` — CI gate script
- `.planning/` — development planning artifacts

## Development

| Operation | Command |
|-----------|---------|
| Build | `go build -o bin/atcr ./cmd/atcr` |
| Test | `go test ./...` |
| Coverage | `go test -coverprofile=coverage.out ./...` |
| Lint | `golangci-lint run` |
| Vet | `go vet ./...` |

Go 1.24+. Three direct dependencies: `spf13/cobra`, `gopkg.in/yaml.v3`, `modelcontextprotocol/go-sdk`.
