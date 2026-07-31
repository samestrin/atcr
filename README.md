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

**New to atcr? Run `atcr quickstart` first** — it is the one-command default. It scaffolds `.atcr/`, sets up the **Synthetic** flat-rate provider, walks you through the one API-key environment variable (the key is never written to atcr's config), shows the signup link, and scaffolds a `.github/workflows/atcr.yml` — so you reach your first review without hand-editing `registry.yaml`.

```bash
# 1. Install (Go 1.25+)
go install github.com/samestrin/atcr/cmd/atcr@latest
# ...or grab a prebuilt binary from GitHub Releases (tagged via GoReleaser),
# ...or, from a clone, run the wrapper (same go install, plus a Go preflight + PATH check):
./install.sh

# 2. One-command onboarding: scaffold .atcr/ + set up the Synthetic provider
atcr quickstart

# 3. Verify every configured endpoint before spending a real review on it
atcr doctor

# 4. Run the panel on the current feature branch, then reconcile — zero arguments
atcr review && atcr reconcile

# 5. Read the report
atcr report --format md
```

Prefer to wire a provider by hand? `atcr init` scaffolds the project config and the nine editable personas into `.atcr/`; then point a provider + agents at any OpenAI-compatible endpoint in `~/.config/atcr/registry.yaml` (see [docs/registry.md](docs/registry.md)) and export its key (for example `export OPENROUTER_API_KEY=sk-...`).

### Choosing a provider

`atcr quickstart` sets up Synthetic because it is a flat-rate endpoint that reaches a working panel in one command. If you outgrow it, the recommended order is:

1. **Synthetic — the default (`atcr quickstart`).** Flat-rate, one-command setup; run this first.
2. **DashScope (Alibaba) — secondary flat-rate option.** A flat-rate alternative to switch to after trying Synthetic. There is no `atcr quickstart` wiring for it this release — see [docs/personas-install.md](docs/personas-install.md) for the manual registry snippet.
3. **Chutes → Featherless — explore, not default.** More models, but slower inference, tighter context windows, and concurrency limits. Try Chutes first, then Featherless: explore, not default.
4. **LiteLLM — Advanced.** An OpenAI-compatible proxy for aggregating several providers behind one endpoint. Keep it Advanced; it is not a first-run recommendation.
5. **Frontier / majors personas — opt-in, bring your own key.** Personas prompt-tuned per each frontier provider's own official prompting guide are installed deliberately by anyone who already holds that provider's API key. They stay opt-in and outside the default funnel — see [docs/personas-install.md](docs/personas-install.md) to discover and install one by the model you have.
6. **Local models (Ollama-class) — privacy-first, zero egress.** Community personas `gerald`, `orson`, and `liam` target local models across three hardware tiers, so the whole panel can run on your own hardware with no code leaving the machine — see [docs/personas-install.md](docs/personas-install.md). The community catalog also ships specialty lenses like `simon`, an anti-slop reviewer targeting AI-generated code bloat and over-engineering.

`atcr doctor` is the recommended post-`atcr init` verification step: it invokes every configured model endpoint once with a trivial prompt and reports any misconfigured provider, model, key, or base URL — so a bad config is caught in seconds instead of mid-review. See [Commands](#commands) for its flags and exit codes.

`atcr review` resolves the range against the default branch, fans the change out to the roster, and records the review id in `.atcr/latest`. Every later command takes an id or path as its single anchor argument and defaults to `latest`, so the two-command pipeline above just works on a feature branch.

> **Run atcr from the repository root.** Subcommands resolve project config (`.atcr/config.yaml`), the git range, and the history/audit ledgers (`.atcr/`) relative to the current working directory. Running from a subdirectory can write ledger records that `atcr audit-report` and `atcr history` — which walk up to the repo root — will not find.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr review` | Resolve the git range (or scan the whole repo with `--all` / a subtree with `--dir`), build payloads, fan out to the reviewer pool, write per-agent + merged findings; `--auto-fix` applies sandboxed fixes and opens a PR |
| `atcr reconcile` | Discover sources, cluster, dedupe, score confidence, write reconciled artifacts; a trust-aware consensus filter uses scorecard history to keep high-trust reviewers' singletons and demote low-trust ones, with `--consensus strict\|lenient\|off` tuning how tolerant it is |
| `atcr verify` | Run adversarial skeptics over reconciled findings; write verdicts and confidence v2 |
| `atcr verify diff` | Scan a unified diff for over-simplification ("reward-hack") fingerprints — a deleted or skipped test, a weakened assertion, a lint suppression. Deterministic and model-free (no provider, no API key, no network) — see [docs/diff-smell.md](docs/diff-smell.md) |
| `atcr debate` | Cross-examine disputed findings (proposer/challenger/judge); settle severity splits, gray-zone clusters, and verification disagreements |
| `atcr report` | Render md / json / checklist / sarif / axi views over the reconciled findings |
| `atcr range` | Pre-flight base..head resolution only; prints resolution JSON |
| `atcr status` | Print a review's fan-out progress as JSON (roster + per-agent state) |
| `atcr init` | Write `.atcr/config.yaml` and the nine default personas (editable) |
| `atcr quickstart` | Interactive onboarding: scaffold `.atcr/` (reusing `init`), set up the synthetic provider + API-key env var, and scaffold a CI workflow (`--open`, `--force`, `--offline`) |
| `atcr config` | Update project configuration in `.atcr/config.yaml` via `atcr config set <telemetry\|quality_signal> <true\|false>` |
| `atcr serve` | Run the MCP stdio server over the same engine |
| `atcr doctor` | Self-test every configured endpoint (dedup'd by provider+model+base_url, fallbacks included); per-agent table or `--json`, with a `SOURCE` (user/project) provenance column |
| `atcr history` | Query the per-package finding-history ledger: trend counts by severity over a `--since` window and optional `--package` prefix |
| `atcr trust` | Authorize project-defined providers from `.atcr/registry.yaml` before they can receive a key |
| `atcr debt` | Query, capture, and report on technical debt (`list` / `add` / `dashboard` / `resolve` / `compact`); `resolve --status wontfix --reason "<text>"` dismisses a false-positive finding so it stops resurfacing — see [docs/technical-debt.md](docs/technical-debt.md) |
| `atcr audit-report` | Render a one-page markdown compliance report for a PR's review runs from the append-only `.atcr/audit.log.jsonl` ledger (`--pr <n>`) |
| `atcr github` | Post reconciled findings to a GitHub pull request as a check run |
| `atcr scorecard` | Display the per-reviewer scorecard for a single reconcile run |
| `atcr quality-report` | Render the aggregate persona+model dismissed/confirmed prompt-quality signal (distinct from `atcr report`; content-free — persona, model, and counts only) |
| `atcr leaderboard` | Aggregate scorecard records across runs, ranked by corroboration rate |
| `atcr benchmark` | Standard benchmark-suite tooling for the public leaderboard |
| `atcr personas` | Manage community reviewer personas; `submit` contributes a locally-tuned persona back upstream as a PR — see [docs/personas-authoring.md](docs/personas-authoring.md) |
| `atcr models` | Inspect model bindings, drift, and the catalog snapshot |
| `atcr skill` | Install the embedded Agent Skill; `atcr skill export [--harness <name>] [--user] [--dir <path>] [--force]` writes it to your agent harness's skills directory |
| `atcr version` | Print the atcr version |

Key flags:

- `atcr review --base X --head Y` / `--merge-commit SHA` / `--id <id>` / `--output-dir <path>` (write the tree to an explicit path; see below) / `--payload diff|blocks|files` / `--timeout <secs>` / `--fail-on <severity>` (one-shot review + reconcile + gate) / `--resume <latest\|id\|path>` (finish an interrupted/failed review by running only its pending agents, then reconcile; see below) / `--force` (overwrite an existing `--id` or `--output-dir` collision, backing the prior tree up to `<dir>.bak` first; mutually exclusive with `--resume`) / `--no-cache` (bypass the diff cache read and force a fresh review; fresh results are still written back to `.atcr/cache`) / `--sprint-plan <path>` (inject a sprint/epic plan as a `SCOPE CONSTRAINT` before the diff so reviewers suppress findings unrelated to the plan's work items; a missing/empty plan is ignored, an unreadable one warns and proceeds) / `--pr <n>` (stamp the pull-request number on this run's audit record; falls back to `GITHUB_REF` when unset) / `--all` / `--dir <path>` (baseline whole-repo or subtree review — see below) / `--fresh` (bypass the baseline incremental cache and rescan everything) / `--no-ignore` (include files matched by `.gitignore` / `.atcrignore`) / `--auto-fix` (apply sandboxed fixes and open a PR — see below) / `--axi` (token-dense TOON output for agent callers — see below) / `--sync-cloud` / `--cloud-endpoint <url>` (upload this run's telemetry; requires `ATCR_API_KEY`) / `--preview` (dry-run the quality-signal payload without sending it) / `--byte-budget <n>` / `--max-parallel <n>`
- One-shot pipeline chaining on `atcr review`: `--verify` / `--require-verified` / `--debate` / `--thorough` / `--single-model` / `--min-severity <severity>` run reconcile → verify → debate in a single command; `--exec` opts into sandboxed reproduction of findings (see [docs/execution.md](docs/execution.md))
- `atcr reconcile --fail-on <severity>` / `--sources <a,b>` (restrict to named source dirs) / `--repo <path>` (validate findings against a repo other than the current directory; also on `atcr verify`)
- `atcr verify diff --rev <rev>` (default `HEAD`) / `--staged` / `--diff <path>` (or `--diff -` for stdin) — name exactly one source / `--repo <path>` / `--json` (payload-only stdout) / `--fail-on hard|soft|none` (opt-in gate; exits **0** on every verdict without it). Note the values are diff-smell **verdicts**, not the finding **severities** `atcr review` / `atcr reconcile` take
- `atcr audit-report --pr <n>` (required — render the compliance report for that PR's recorded review runs; a PR with no recorded runs exits non-zero)
- `atcr report --format md|json|checklist|sarif|axi` / `--output <file>` / `--disagreements` (focused disagreement-radar view — see [docs/disagreement-radar.md](docs/disagreement-radar.md))
- `atcr doctor` / `--json` / `--max-tokens <n>` (default 2048, high enough for thinking models) / `--timeout <secs>` (default 60) / `--agents <a,b>` (test a subset of listed agents; their fallback chains are still probed). Exit **0** when every agent has a working invocation path (primary or fallback), **1** when any agent has none, **2** for usage/config errors.

Environment variables:

- `ATCR_DISABLE_AST_GROUPING` — `atcr reconcile` clusters findings by AST isomorphism (the smallest covering AST block of each finding's line) by default, so findings group together across line-number drift, with line proximity as the per-finding fallback when no parser is available or the source is missing. AST grouping covers Go, Python, TypeScript/JavaScript, PHP, Rust, Bash, Java, Kotlin, C/C++, and C#; any other file type falls back to line proximity. Set this to a truthy value (`1`, `true`) to revert to legacy line-proximity-only clustering; a falsy, unparseable, or unset value keeps AST grouping on.
- `ATCR_TELEMETRY` — names the **enabled** state (note the inverse direction vs. `ATCR_DISABLE_AST_GROUPING`): `ATCR_TELEMETRY=0` disables the anonymous usage ping. See [docs/telemetry.md](docs/telemetry.md).
- `ATCR_API_KEY` — API key for `--sync-cloud` telemetry uploads; a missing or rejected key exits with code **3**.
- `ATCR_AXI_MAX_LINES` — caps `--axi` output before truncation (see [docs/agentic-consumption.md](docs/agentic-consumption.md)).

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

## Baseline review (`--all` / `--dir`)

`atcr review` does not require a diff range: `--all` scans the whole repository and `--dir <path>` scans a subtree — useful for a first pass over a legacy codebase or a periodic full audit. Baseline runs are incremental: files whose content hash is unchanged since the last baseline are skipped, and `--fresh` forces a full rescan. `--all` / `--dir` are mutually exclusive with `--base` / `--head` / `--merge-commit`.

## Auto-fix (`--auto-fix`)

`atcr review --auto-fix` turns findings into fixes: an executor model generates a patch per eligible finding, atcr applies it to the working tree, validates it (build + tests), auto-reverts on failure, and opens a GitHub pull request with the results. It never auto-merges.

- **Sandboxed by default — no opt-in.** Post-apply validation runs model-authored code, so it executes inside the same container isolation as `--exec` (no network, read-only root filesystem, dropped capabilities, resource caps). If no sandbox backend is configured or available, the command fails closed rather than validating on the host; `--no-sandbox` is the explicit danger opt-out.
- **Config edits blocked by default.** Patches touching `.git/`, `.githooks/`, CI workflows, `.env*`, `.planning/`, or `.atcr/` are rejected unless you pass `--allow-config-edits` (danger).
- **Two-tier execution.** Complexity ceilings (`max_severity_for_fix`, `max_estimated_minutes`) let a cheap executor model handle simple fixes and escalate the rest to a frontier model — worked examples in `examples/registry-with-executor*.yaml`.
- **Shortcut fixes rejected.** Fixes that smell like reward-hacking (deleted assertions, added suppressions, swallowed errors, stubbed bodies) are rejected/retried or flagged `NEEDS_REVIEW` rather than applied.

`--repo` / `--token` / `--api-url` target a non-default GitHub remote. See [docs/auto-fix.md](docs/auto-fix.md) and [docs/security.md](docs/security.md).

## Agent consumption (`--axi`)

`--axi` (Agent eXperience Interface) makes atcr comfortable as a subprocess for another agent: token-dense TOON output on stdout, diagnostics kept on stderr, deterministic exit codes, and a line cap tunable via `ATCR_AXI_MAX_LINES`. Available on `atcr review`, bare `atcr`, and as `atcr report --format axi`. See [docs/agentic-consumption.md](docs/agentic-consumption.md).

## Payload modes

`atcr` ships three payload modes that control what each reviewer agent sees. The default is `blocks`; set per-agent overrides in `~/.config/atcr/registry.yaml` when a model handles a different format better.

| Mode | What the reviewer sees | When to use |
|------|------------------------|-------------|
| `diff` | Unified diff | **The most compact and token-friendly mode.** Right choice for frontier models and large ranges. |
| `blocks` | Changed hunks expanded to the enclosing function/block (`git diff --function-context`), with real line numbers | **Default for v1.** Best findings quality from small / MoE models reading real code. |
| `files` | Full head-version content of changed files with changed regions marked | Highest token cost. Audit-style review of small ranges. |

One run can mix payloads — the frontier model reads the `diff`, the local 8B gets `blocks` — and `manifest.json` records who saw what. See [docs/payload-modes.md](docs/payload-modes.md) for the decision guide, byte-budget truncation, and per-mode scope rules.

Payloads are also filtered, promoted, and sized automatically:

- **Ignore filtering.** Changed files matching the repo-root `.gitignore` or `.atcrignore` are excluded from payloads by default; `--no-ignore` opts out.
- **Automatic per-file escalation.** Complex files (high churn, many hunks, high cyclomatic complexity) are promoted up the ladder — `diff` → `blocks` → `files` — and payloads are prepended with an AST skeleton of the changed files' HEAD version so a `diff`-mode reviewer still sees the surrounding structure. Tunable via the `payload_escalation` registry block; `manifest.json` records what each reviewer actually saw per file.
- **Per-model sizing.** Each reviewer's payload is sized to its own model's context window with the output budget reserved; when a payload still overflows, the `on_overflow` policy applies (`chunk` default / `truncate` / `fallback` / `fail`), and `max_sprint_plan_bytes` caps the `--sprint-plan` injection. See [docs/registry.md](docs/registry.md).

## Project-defined providers and agents

A repo can ship its own providers and agents in `.atcr/registry.yaml`, overlaying the user registry so a clone is self-contained — project entries shadow same-named user entries whole; new names are added. Because a project-defined provider could direct a key to an arbitrary endpoint, atcr gates them: run `atcr trust` to authorize a project provider (it pins the `base_url` + `api_key_env` pair) before any review or `atcr doctor` will use it. See [docs/registry.md](docs/registry.md#project-registry-overlay).

## Tool-using reviewers (cost guidance)

Set `tools: true` on a function-calling-capable agent to turn it from a single-shot reviewer into a bounded, multi-turn **tool-using agent**: it can `read_file`, `grep`, and `list_files` across a read-only, path-jailed snapshot of the repo to verify a suspicion before reporting it. The payload becomes the starting point of the review, not the whole picture — see [docs/payload-modes.md](docs/payload-modes.md).

Tool agents are not free. A tool-using reviewer **typically consumes 3-10× the provider calls** of a single-shot reviewer (one call per turn, plus the final answer), so cost scales with how much exploration the model does. Each agent is bounded by per-agent budgets — `max_turns`, `tool_budget_bytes`, and `timeout_secs` — documented in [docs/registry.md](docs/registry.md). Enable `tools` selectively (your strongest models, your highest-value lanes) rather than across the whole roster, and tune the budgets to cap spend. A `tools: true` agent on a model without `supports_function_calling: true` degrades cleanly to the single-shot path.

## CI integration

atcr is a PR gate with no glue code: `--fail-on <severity>` returns a nonzero exit when any finding at or above the threshold survives reconciliation.

```bash
atcr review && atcr reconcile --fail-on high   # exit 1 if HIGH+ findings survive
```

Exit codes: **0** success · **1** failure (including a `--fail-on` threshold violation) · **2** usage or configuration error · **3** authentication failure (`--sync-cloud` missing or rejected `ATCR_API_KEY`).

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
          go-version: '1.25'
      - run: go install github.com/samestrin/atcr/cmd/atcr@latest
      - name: atcr gate
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
        run: |
          atcr review --base "origin/${{ github.base_ref }}"
          atcr reconcile --fail-on high
```

A ready-to-adapt script lives at [examples/ci-gate.sh](examples/ci-gate.sh). Or skip the hand-rolled workflow entirely: the repo ships a composite GitHub Action ([`action.yml`](action.yml)) that runs review → reconcile → `atcr github` and posts the result as a PR check with optional inline comments — see [docs/github-action.md](docs/github-action.md). Shallow checkouts (`fetch-depth: 1`) break merge-base resolution; atcr detects this and errors with `git fetch --unshallow` guidance rather than producing a wrong range. See [docs/ci-integration.md](docs/ci-integration.md).

## Providers

atcr speaks to any OpenAI-compatible `/chat/completions` endpoint directly — no SDKs, no infrastructure, keys from environment variables resolved at invoke time. For maximum compatibility across providers, routing through a normalizing proxy such as [LiteLLM](https://github.com/BerriAI/litellm) is supported but not required. See [docs/providers.md](docs/providers.md).

## Documentation

- [docs/providers.md](docs/providers.md) — direct vs. proxy setups, normalization guidance
- [docs/registry.md](docs/registry.md) — providers, personas, agents, fallbacks, lanes, precedence
- [docs/payload-modes.md](docs/payload-modes.md) — blocks vs. diff vs. files, token guidance
- [docs/findings-format.md](docs/findings-format.md) — the versioned `atcr-findings/v1` contract
- [docs/execution.md](docs/execution.md) — opt-in `--exec` sandboxed reproduction, the `evidence_exec` block, and the security posture
- [docs/auto-fix.md](docs/auto-fix.md) — `--auto-fix` sandboxed validation, executor tiers, the PR flow
- [docs/security.md](docs/security.md) — sandbox guarantees, workspace integrity, blocked paths
- [docs/verification.md](docs/verification.md) — `atcr verify` adversarial skeptics and confidence v2
- [docs/cross-examination.md](docs/cross-examination.md) — the `atcr debate` proposer/challenger/judge stage
- [docs/disagreement-radar.md](docs/disagreement-radar.md) — the disagreement radar, `--disagreements` view, and `disagreements.json` handoff schema
- [docs/ci-integration.md](docs/ci-integration.md) — exit codes and PR gates
- [docs/github-action.md](docs/github-action.md) — the composite PR-review GitHub Action
- [docs/skill-usage.md](docs/skill-usage.md) — installing and running the Agent Skill
- [docs/metrics.md](docs/metrics.md) — metric catalog, end-of-review CLI summary, and the `atcr_metrics` MCP tool
- [docs/scorecard.md](docs/scorecard.md) — per-reviewer scorecards, trust priors, and the leaderboard
- [docs/benchmark.md](docs/benchmark.md) — benchmark-suite tooling for the public leaderboard
- [docs/telemetry.md](docs/telemetry.md) — what telemetry collects, opting out, and `--sync-cloud`
- [docs/personas-install.md](docs/personas-install.md) — discovering and installing provider and community personas
- [docs/personas-authoring.md](docs/personas-authoring.md) — writing and submitting your own community persona
- [docs/agentic-consumption.md](docs/agentic-consumption.md) — driving atcr from another agent via `--axi`

## Repository layout

- `cmd/atcr/` — thin binary entry-point shim
- `cli/` — the importable command tree (all subcommand logic)
- `internal/` — engine packages (`gitrange`, `payload`, `registry`, `llmclient`, `fanout`, `stream`, `report`, `mcp`, `verify`, `debate`, `autofix`, `sandbox`, `scorecard`, `telemetry`, and more)
- `reconcile/` — the deterministic reconciler as a standalone Go module (importable as a library; wired into the build via `go.work`)
- `personas/` — the nine embedded default personas + `_base.md`
- `skills/atcr/` — the atcr Agent Skill (host review + orchestration); it is embedded in the binary, so `atcr skill export` installs it
- `docs/` — user documentation
- `examples/` — CI gate script, a live-audit example, and sample executor registries
- `bin/` — local build output (`go build -o bin/atcr`)
- `.planning/` — development planning artifacts

## Development

| Operation | Command |
|-----------|---------|
| Build | `go build -o bin/atcr ./cmd/atcr` |
| Test | `go test ./...` |
| Coverage | `go test -coverprofile=coverage.out ./...` |
| Lint | `golangci-lint run` |
| Vet | `go vet ./...` |

> **Pro-Tip:** Use `git worktree` to work on multiple branches without disturbing your main checkout — e.g. `git worktree add ../atcr-feature-x feature-x` checks out `feature-x` into a sibling directory you can build/test in isolation, then `git worktree remove ../atcr-feature-x` cleans it up when done.

Go 1.25+. Direct dependencies are listed in `go.mod`; beyond `spf13/cobra`, `gopkg.in/yaml.v3`, and `modelcontextprotocol/go-sdk` they include `tetratelabs/wazero` (WASM parsers), `github.com/cli/go-gh` (PR resolution), and others.
