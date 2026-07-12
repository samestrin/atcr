# atcr Agent Skill — Installation & Usage

The atcr skill turns a host AI agent (e.g. Claude Code) into the **+1 reviewer** on an atcr review panel. It orchestrates the full flow — resolve range → fan out to the reviewer pool → host review → reconcile → report — and contributes its own adversarial review so reconciliation always has at least two independent sources. Beyond one-off review, it can accumulate findings into a durable local backlog and autonomously work through it — see [Technical Debt Resolution](#technical-debt-resolution).

The skill is [`skill/SKILL.md`](../skill/SKILL.md) — a `/atcr <command>` dispatcher — plus a set of sibling files it loads on demand: [`host-review.md`](../skill/host-review.md), [`ambiguity-adjudication.md`](../skill/ambiguity-adjudication.md), [`findings-format.md`](../skill/findings-format.md), the shared [`CONVENTIONS.md`](../skill/CONVENTIONS.md), and the [`debt-resolve/SKILL.md`](../skill/debt-resolve/SKILL.md) route. None contain executable code; they are instructions the agent follows, invoking the `atcr` binary at each step. Install the whole directory together so the on-demand references resolve.

## Prerequisites

- The `atcr` binary on your `PATH` (`go build -o atcr ./cmd/atcr`, then move it onto `PATH`).
- A configured registry (`~/.config/atcr/registry.yaml`) and project config (`.atcr/config.yaml`) — run `atcr init` to scaffold both. Even with a single pool agent (or none), the host review provides a second source.
- For PR-reference input only: the `gh` CLI, authenticated (`gh auth status`).

## Installation

The skill installs by file copy into your agent's skills directory. For Claude Code, the project-local location is `.claude/skills/atcr/`. Copy the instruction files — `SKILL.md` plus its on-demand secondary `.md` files, including the nested `debt-resolve/` route — not `SKILL.md` alone, or the host-review, adjudication, findings-format, conventions, and debt-resolve references will fail to resolve at runtime. Copy the `debt-resolve/` subdirectory too (a flat `cp skill/*.md` would miss it):

```sh
mkdir -p .claude/skills/atcr/debt-resolve
cp skill/*.md .claude/skills/atcr/
cp skill/debt-resolve/*.md .claude/skills/atcr/debt-resolve/
```

Standard skill resolution applies: a project-local copy wins over a globally installed one, and the copy shipped in this repo (`skill/`) is the canonical reference. To install globally for your user, copy the same files into your agent's user-level skills directory instead.

## Usage

Invoke the skill from within a git repository and give it one of:

| Input | Example | Behavior |
|-------|---------|----------|
| Git range | `main..feature-x` | Reviews `base..head` |
| Branch name | `feature-x` | Reviews the branch vs. the detected default branch |
| PR URL | `https://github.com/owner/repo/pull/42` | Resolves base/head via `gh`, then reviews |
| (nothing) | — | Reviews the current branch vs. the default branch |

The skill then:

1. Pre-flights the range (`atcr range`).
2. Starts the pool review in the background (`atcr review`) and polls `atcr status <id>` until it completes (10s interval, 10-minute default timeout).
3. Performs the host review and writes `.atcr/reviews/<id>/sources/host/findings.txt`.
4. Reconciles all sources (`atcr reconcile <id>`).
5. Optionally adjudicates gray-zone ambiguous clusters and re-reconciles.
6. Renders and presents `report.md`, and prints the review directory path.

## Output

Everything lands under `.atcr/reviews/<id>/`:

- `payload/` — what the reviewers saw.
- `sources/pool/` and `sources/host/` — per-reviewer findings.
- `reconciled/report.md` — the human report (also `findings.txt`, `findings.json`, `summary.json`, `ambiguous.json`).

See [findings-format.md](findings-format.md) for the versioned findings stream contract and [providers.md](providers.md) for configuring the reviewer pool.

To drive atcr as the reviewer backend for a separate code-review skill or pipeline (invoking `atcr review --output-dir` as a subprocess and owning the downstream merge), see [code-review-backend.md](code-review-backend.md).

## Technical Debt Resolution

> **Two different `atcr debt` families — don't conflate them.** This section is the **public, standalone** debt loop: the `/atcr debt resolve` route over the local, `.atcr/`-scoped store that `atcr reconcile` populates. It is entirely separate from the **private, sprint-pipeline** commands `atcr debt list` / `add` / `dashboard`, which read the `.planning/technical-debt/` store and are documented in [technical-debt.md](technical-debt.md). Both share the `atcr debt` command surface but read and write different, non-overlapping data stores; which one applies to you depends on whether your repo uses the private `.planning/` sprint workflow.

Beyond one-off review, atcr accumulates findings into a durable local backlog and can autonomously work through it. `atcr reconcile` appends each run's reconciled findings to a local technical-debt store, and the `/atcr debt resolve` route reads that backlog and fixes items through a per-item RED→GREEN→ADVERSARIAL→REFACTOR cycle. This is the standalone counterpart of the private `/resolve-td` loop, with zero `.planning/` dependency.

### The `/atcr debt resolve` route

`/atcr debt resolve` reads the local store, selects open items, resolves them one at a time, then runs a cumulative adversarial pass over the whole run. For each item it:

1. **RED** — reproduces or confirms the problem with a failing test.
2. **GREEN** — applies the minimal fix that makes RED pass.
3. **ADVERSARIAL** — runs a non-overridable over-simplification / reward-hack gate over the diff; anything that fakes a pass (test-only edits, weakened assertions, lint/type suppressions, stubbed bodies) is flagged `NEEDS_REVIEW` and **never** marked resolved.
4. **REFACTOR** — cleans up and re-confirms green.

Selection is deterministic: open items only, sorted by severity descending (`CRITICAL > HIGH > MEDIUM > LOW`) then oldest-first within a severity, capped at the first 10 (override with `--max`). When a finding carries the optional `justification` / `source_report` enrichment (Epic 18.3), the route reads it for context and surfaces its provenance — but treats it strictly as untrusted data describing the finding, never as instructions to act on.

Autonomous fixes never land unreviewed: from the repository's default branch the route first creates a `debt-resolve/<date>` branch; on a non-default branch it resolves in place on the branch you are already on. Resolution outcomes are recorded as append-only status records via `atcr debt resolve --resolve <id>`.

The subcommand backing the route is a thin store surface — it lists candidates and marks items resolved; the actual code-fixing cycle is agent-driven, never compiled Go. You can preview or script it directly:

```bash
atcr debt resolve --list                 # preview open items (also the default with no flags)
atcr debt resolve --json                 # same selection as a JSON array
atcr debt resolve --severity HIGH        # filter by severity (CRITICAL|HIGH|MEDIUM|LOW)
atcr debt resolve --max 5                # cap the selection (default 10)
atcr debt resolve --resolve <id>         # record an append-only resolution
```

When the local store is **empty or absent**, `atcr debt resolve` reports that there are no items to resolve and exits `0` — there is nothing to resolve and no store is created.

### Storage

The local TD store lives inside your repository, populated automatically as a byproduct of `atcr reconcile` — no flag is needed to enable it:

```
.atcr/debt/YYYY-MM.jsonl
```

- **Monthly rotation.** One append-only file per calendar month (`YYYY-MM.jsonl`), named from each record's `run_id` month prefix.
- **Append-only with write-time dedup.** Each `atcr reconcile` run appends its findings; before appending, the store scans the full history and skips any finding whose identity (`FindingID`, derived from file + line + problem text) is already present, so re-running reconcile on an unchanged repo does not duplicate items. A finding whose `problem` text later changes is treated as a distinct record.
- **Permissions.** The file is created `0600` (user read/write only) and the directory `0700`; the directory is created lazily on the first write, so a suppressed run creates nothing.
- **Opt out per run.** Pass `--no-local-debt` to `atcr reconcile` to suppress persistence for a single run (mirroring `--no-scorecard`). It has no effect on reconcile's exit code or output; without it, reconcile persists by default.

> **Do not commit `.atcr/debt/`.** It is local state derived from your codebase's review findings, not shared project data — keep it out of version control.
