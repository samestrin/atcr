# atcr Documentation

The canonical index of atcr's documentation. Every reference below is a
standard GitHub Flavored Markdown file in this directory; this index is the
single source of truth the website build consumes, so it links every doc in
`docs/`.

## Overview & configuration

- [Architecture Overview](architecture.md) — how the multi-model reviewer panel
  and the deterministic reconciler pipeline fit together, stage by stage.
- [Configuration Reference](registry.md) — `.atcr/config.yaml`, the registry
  files, resolution tiers, and the persona resolution chain.
- [Providers](providers.md) — configuring OpenAI-compatible model providers.
- [atcr Agent Skill — Installation & Usage](skill-usage.md) — installing and
  driving atcr as an agent skill.

## Pipeline stages

- [Payload Modes](payload-modes.md) — `diff`, `blocks`, and `files`: how much
  code each reviewer sees.
- [Execution reproduction (`--exec`)](execution.md) — sandboxed reproduction of
  findings during a single stage.
- [Adversarial Verification](verification.md) — skeptic agents that try to
  disprove reconciled findings.
- [Cross-Examination (Debate Stage)](cross-examination.md) — proposer /
  challenger / judge resolution of disputed findings.
- [Disagreement Radar](disagreement-radar.md) — the focused view over reviewer
  disagreement.
- [Findings Format — `atcr-findings/v1`](findings-format.md) — the stable,
  machine-parseable on-disk findings contract.

## Personas

- [Authoring a Persona](personas-authoring.md) — writing a reviewer persona.
- [Installing Community Personas](personas-install.md) — installing and managing
  community personas.

## Integration

- [CI Integration](ci-integration.md) — wiring atcr into a CI gate.
- [Agentic Consumption](agentic-consumption.md) — driving `atcr` from another
  autonomous agent via `--axi`: token-dense TOON output, exit codes, pagination,
  and stdout/stderr isolation.
- [GitHub Action — PR Review](github-action.md) — posting reconciled findings to
  a pull request.
- [Hermes Maintenance Agents](hermes-maintenance-agents.md) — configuration
  surface for wiring the external hermes agents to `atcr models check` across the
  mechanical/judgment/drafting roles.
- [Using atcr as a code-review backend (`--output-dir`)](code-review-backend.md)
  — driving atcr as the reviewer backend for a separate pipeline.
- [Migrating private skills to the `/atcr` dispatcher](external-migration.md) —
  the manual operator step to move external `claude-prompts` skills onto the
  dispatcher pattern, and why it is out of this repo's automated scope.
- [Release Process](release-process.md) — the bare `vX.Y.Z` tag convention and
  the tag-triggered goreleaser workflow a maintainer uses to cut a release.
- [Technical-Debt Tooling (`atcr debt`)](technical-debt.md) — query, capture, and
  report on technical debt; the `--check` dashboard gate for CI and pre-commit.
- [Symbol-anchored Problem cells (`(symbolName)` prefix)](technical-debt-format.md)
  — the stable AST-symbol anchor prepended to each finding's `problem`, and how a
  `resolve-td` consumer parses it as a drift-proof relocation key.

## Benchmarking & observability

- [Benchmark Suite](benchmark.md) — the standard benchmark tooling behind the
  public leaderboard.
- [Scorecard](scorecard.md) — the per-reviewer scorecard for a reconcile run.
- [Metrics](metrics.md) — the metrics atcr records.
- [Telemetry](telemetry.md) — the opt-out usage ping and the `--sync-cloud` push,
  and the privacy model for both.
- [Logging](logging.md) — log levels and output formats.
