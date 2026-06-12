# atcr Agent Skill — Installation & Usage

The atcr skill turns a host AI agent (e.g. Claude Code) into the **+1 reviewer** on an atcr review panel. It orchestrates the full flow — resolve range → fan out to the reviewer pool → host review → reconcile → report — and contributes its own adversarial review so reconciliation always has at least two independent sources.

The skill itself is a single Markdown file, [`skill/SKILL.md`](../skill/SKILL.md). It contains no executable code; it is instructions the agent follows, invoking the `atcr` binary at each step.

## Prerequisites

- The `atcr` binary on your `PATH` (`go build -o atcr ./cmd/atcr`, then move it onto `PATH`).
- A configured registry (`~/.config/atcr/registry.yaml`) and project config (`.atcr/config.yaml`) — run `atcr init` to scaffold both. Even with a single pool agent (or none), the host review provides a second source.
- For PR-reference input only: the `gh` CLI, authenticated (`gh auth status`).

## Installation

The skill installs by file copy into your agent's skills directory. For Claude Code, the project-local location is `.claude/skills/atcr/`:

```sh
mkdir -p .claude/skills/atcr
cp skill/SKILL.md .claude/skills/atcr/SKILL.md
```

Standard skill resolution applies: a project-local copy wins over a globally installed one, and the copy shipped in this repo (`skill/SKILL.md`) is the canonical reference. To install globally for your user, copy into your agent's user-level skills directory instead.

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
