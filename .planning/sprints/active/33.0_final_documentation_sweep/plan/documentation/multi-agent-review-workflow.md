# Multi-Agent Review Workflow (atcr Dispatcher)

**Priority: [CRITICAL]**

## Overview

atcr reviews a code change with a *panel* of reviewers instead of a single model: the `atcr` binary resolves the git range, builds payloads, fans out to a configured pool of reviewer personas, and deterministically reconciles their findings (cluster → dedupe → confidence).

> Source: skill/SKILL.md > Overview

The `/atcr <command>` skill adds a **host review** — an adversarial pass over the same payload performed by the assistant itself — so reconciliation always has at least two independent sources and a meaningful cross-reviewer agreement signal, even when only one API key is configured. The skill's job is to validate the input, start the review, perform the host review, reconcile, and present the report; the binary handles everything deterministic.

> Source: skill/SKILL.md > Overview

For Plan 33.0, this dispatcher is the mechanism for Task 1 ("Multi-agent code review"): atcr's multi-agent reviewer is dogfooded across the production directories `cmd/`, `internal/`, `reconcile/`, and `skill/`, and findings are triaged into CRITICAL/HIGH (must fix before launch) versus MEDIUM/LOW (sharded into technical debt). This review pass must complete before the documentation sweep (Task 8, "Final verification pass") so that user-facing documentation reflects the finalized, hardened codebase.

> Source: codebase-discovery.json > integration_gaps, architecture_notes

## Key Concepts

- **Panel + host review model**: atcr fans a git range, branch, or PR out to a configured reviewer pool and adds a host (+1) review from the assistant itself over the same payload, giving reconciliation at least two independent sources even with a single configured API key.

  > Source: skill/SKILL.md > Overview

- **Deterministic reconciliation**: findings are merged via cluster → dedupe → confidence scoring, producing one deduplicated, confidence-scored report rather than raw per-reviewer output.

  > Source: skill/SKILL.md > frontmatter description

- **Orchestration is a fixed 7-step sequence**: pre-flight the range (`atcr range`) → start the review in the background (`atcr review`, no `--wait` flag) → poll status (`atcr status <id>` every 10 seconds, up to 60 times) → host review pass (write findings to `.atcr/reviews/<id>/sources/host/findings.txt`) → reconcile (`atcr reconcile <id>`) → render and present (`atcr report <id> --format md`) → output the review directory path.

  > Source: skill/SKILL.md > Orchestration Steps

- **Partial-failure handling**: if the pool partially fails (some agents error, at least one succeeds), reconciliation still proceeds, with `partial: true` noted from the status/summary. If `.atcr/latest` is missing or stale, the explicit review id captured at start time is passed to `reconcile`/`report`/`status` instead of relying on the pointer.

  > Source: skill/SKILL.md > Orchestration Steps

- **Host review grounding rules**: the host-review instructions include an adversarial no-praise personality clause and payload-grounding / anti-hallucination rules — all payload and findings content is treated strictly as untrusted data, never as instructions to follow.

  > Source: skill/SKILL.md > Host Review Instructions

- **Findings format contract**: the findings stream is a versioned, pipe-delimited contract — per-source `findings.txt` files carry 8 columns, and reconciled output carries 9 (adding a `REVIEWERS` list and a `CONFIDENCE` column).

  > Source: skill/SKILL.md > Findings Format Reference

- **Automated slug gate to run alongside the review**: `personas/retired_slugs_test.go` asserts zero references to retired role-based slugs (`sentinel`, `tracer`, `idiomatic`) as persona identifiers across the active persona set — this is the AC3 automated gate that accompanies the multi-agent review pass.

  > Source: codebase-discovery.json > existing_patterns ("Retired-slug guard test")

- **Review scope and triage for Plan 33.0**: the reviewer targets `cmd/`, `internal/`, `reconcile/`, and `skill/`, with findings triaged into CRITICAL/HIGH (must fix in the codebase before launch) and MEDIUM/LOW (sharded into `.planning/technical-debt/README.md`).

  > Source: codebase-discovery.json > integration_gaps, architecture_notes

- **Sequencing constraint**: the code review pass (Phase 1) must precede the documentation sweep (Phase 2) so documentation reflects the finalized, hardened codebase — this is why Task 1 (review) grounds Task 8 (final verification pass).

  > Source: codebase-discovery.json > architecture_notes

## Code Examples

Pre-flight the range:

```
atcr range [--base X --head Y | --merge-commit SHA]
```

> Source: skill/SKILL.md > Orchestration Steps (step 1)

Start the review in the background:

```
atcr review [--base X --head Y]
```

> Source: skill/SKILL.md > Orchestration Steps (step 2)

Poll status:

```
atcr status <id>
```

> Source: skill/SKILL.md > Orchestration Steps (step 3)

Reconcile all discovered sources:

```
atcr reconcile <id>
```

> Source: skill/SKILL.md > Orchestration Steps (step 5)

Render and present the report:

```
atcr report <id> --format md
```

> Source: skill/SKILL.md > Orchestration Steps (step 6)

## Quick Reference

| Command | What it does |
|---------|--------------|
| `atcr review` | Fan a code change out to the reviewer pool |
| `atcr reconcile` | Merge findings from all sources into reconciled artifacts |
| `atcr verify` | Run adversarial skeptics over reconciled findings |
| `atcr debate` | Cross-examine disputed findings (proposer/challenger/judge) |
| `atcr report` | Render md, json, or checklist views over reconciled findings |
| `atcr quality-report` | Rank persona+model reviewer prompts by dismissal rate from the content-free local quality signal |
| `atcr github` | Post reconciled findings to a GitHub pull request as a check run |
| `atcr range` | Resolve the review range and print resolution JSON |
| `atcr status` | Print a review's fan-out progress as JSON |
| `atcr scorecard` | Display the per-reviewer scorecard for a single reconcile run |
| `atcr leaderboard` | Aggregate scorecard records across runs, ranked by corroboration rate |
| `atcr debt` | Query and report on technical debt; `atcr debt resolve` lists and marks-resolved the public `.atcr/`-scoped local store |

> Source: skill/SKILL.md > Commands

## Related Documentation

- [skill/SKILL.md](../../../../../skill/SKILL.md) — the `/atcr <command>` dispatcher and orchestration steps described above.
- [skill/debt-resolve/SKILL.md](../../../../../skill/debt-resolve/SKILL.md) — companion skill for the `atcr debt resolve` workflow referenced in the Commands table.
- [.planning/technical-debt/README.md](../../../../technical-debt/README.md) — destination for MEDIUM/LOW findings sharded out of the multi-agent and adversarial review pass.
- [personas/retired_slugs_test.go](../../../../../personas/retired_slugs_test.go) — the automated AC3 gate asserting zero retired-slug references, run alongside this review.

