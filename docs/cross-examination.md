# Cross-Examination (Debate Stage)

Cross-examination resolves reviewer *disagreements* through a structured, bounded debate instead of a heuristic. Where reviewers dispute a finding's severity, where the reconciler's similarity gray zone leaves a cluster ambiguous, or where a skeptic vote ties, `atcr debate` runs a three-seat exchange — a **proposer** defends the finding, a **challenger** (a different model) attacks it, and a **judge** (a third distinct model) rules — and the judge's ruling, not a rule like severity-max, settles the finding. Findings that survive hostile challenge are marked as the top confidence tier.

Disagreement is signal, not noise. Severity-max is blunt — one alarmist reviewer inflates the merged severity and the gate. `ambiguous.json` clusters previously needed a human or the host Skill, so unattended CI runs left them unresolved. A `unverifiable`-by-disagreement verdict from the verify stage is exactly the kind of split a debate can settle. Cross-examination extracts that signal automatically.

## Overview

- **When it runs:** after reconcile (and after verify, when present — verification disagreements only exist once skeptics have voted). Never before reconcile.
- **What it reads:** `reconciled/findings.json`. It rebuilds the disagreement radar from the current findings, so it sees post-verify verification ties as well as severity splits and gray-zone clusters.
- **What it writes:** `reconciled/debate.json` (the ruling record), a re-emitted `reconciled/findings.json` with the settled verdicts/severities, per-item transcripts under `debate/<item-id>/transcript.jsonl`; it appends `"debate"` to the manifest stages.
- **Never drops a finding:** a seat that times out, trips a budget, or errors yields an *unresolved* item — never a dropped finding and never a failed run by itself. An item that cannot be cast (no distinct models, no opt-in) is recorded unresolved with the reason.

Run it standalone, chained off a review, or as an MCP tool:

```bash
atcr debate [id-or-path]                  # debate an already-reconciled (and ideally verified) review
atcr review --verify --debate             # review → reconcile → verify → debate in one run
```

`id-or-path` follows the same resolution as `atcr verify`: a bare review id resolves under `.atcr/reviews/<id>/`, a path is used as-is, and omitting it uses `.atcr/latest`.

## Triggers and Item Selection

The debate stage acts only on disputed items — undisputed findings are never debated. Triggers map to the disagreement radar's tension classes:

| Trigger (`debate.triggers`) | Fires on |
|------------------------------|----------|
| `severity_split` | Reviewers assigned different severities (a `<lo> vs <hi>` disagreement spanning the merged finding). |
| `gray_zone` | An `ambiguous.json` cluster — a similarity-gray-zone pair the reconciler left unmerged. |
| `verification_disagreement` | A skeptic vote tied to `unverifiable` with 2+ skeptics (present only after `atcr verify`). |

All three default **on**. `verification_disagreement` costs nothing when verify is skipped (it simply never fires). Selected items are ordered by **severity priority** (most severe disputes first), then the radar tension score; the cost cap (`debate.max_items`, default 5) keeps the highest-priority items and records the rest as **overflow** — disclosed in `debate.json` and the report, never silently dropped.

## Role Casting and the Distinct-Model Rule

Each item is cast into three seats, enforced — never left to configuration discipline:

- **Proposer** — a crediting reviewer's agent (the model that raised the finding defends it). Falls back to any `role: reviewer` agent when no crediting reviewer resolves.
- **Challenger** — a `role: skeptic` agent whose model differs from the proposer's.
- **Judge** — a `role: judge` agent whose model differs from *both* the proposer and challenger. Assign the strongest model to the judge role; among eligible judges the cast is deterministic (sorted by agent name).

All three models must be pairwise distinct. When three distinct models cannot be assembled (the common case today, when no `role: skeptic` / `role: judge` agents are configured), the outcome depends on `debate.allow_single_model`:

- **`false` (default)** — the item is recorded **unresolved** (`insufficient_distinct_models`) rather than silently loosening the independence requirement.
- **`true`** — the **single-model fallback** casts all three seats on the proposer's model, simulating the debate via distinct proposer/challenger/judge personas. Such rulings are flagged `single_model` in `debate.json` and disclosed as `(single-model fallback)` in the report, because the independence guarantee is weaker. Opt in per run with `--single-model`.

A missing proposer (no usable reviewer-role agent at all) is always unresolved, even under the fallback — a debate needs at least one agent to run.

## Protocol

The exchange is bounded to exactly three turns — never an open-ended conversation:

1. **Proposer** defends the finding, using the read-only tool loop (the same path-jailed code access the reviewers and skeptics use) to cite concrete evidence.
2. **Challenger** attacks it, given the proposer's defense.
3. **Judge** rules, given both statements, and returns the ruling envelope.

Untrusted reviewer- and model-authored text is wrapped in per-item sentinel-tagged blocks, so content containing a literal `</finding>` cannot close the block early and inject instructions into the judge.

## Judge Envelope

The judge returns a strict, parseable envelope:

```json
{"outcome": "uphold|overturn|split", "settled_severity": "CRITICAL|HIGH|MEDIUM|LOW", "cluster_decision": "merge|separate", "reasoning": "why"}
```

- `uphold` — the finding stands; it **survived challenge**.
- `overturn` — a false positive or unsupported; refuted.
- `split` — real, but at a different severity; `settled_severity` replaces severity-max.
- `cluster_decision` — for a gray-zone cluster only: whether the pair should `merge` or stay `separate`.

Parsing is defensive (the same contract as the verify stage): the parser scans for a `{...}` object so a verdict in markdown fences or prose is recovered; an out-of-enum outcome, an empty response, or unparseable output degrades to `unresolved` with the raw text preserved — it never forges an `uphold` or `overturn`. An unknown `settled_severity` is dropped rather than failing the ruling.

## Integration and Confidence

Rulings ride the same `verification` block and confidence axis the verify stage uses — there is no separate `DEBATED` tier (which would be invisible to the gate, since the gate keys on the verdict, not the confidence string). Instead, a surviving finding gains a `challenge_survived` marker:

| Outcome | Verdict written | `challenge_survived` | Confidence | Severity |
|---------|-----------------|----------------------|------------|----------|
| `uphold` | `confirmed` | `true` | `VERIFIED` | unchanged |
| `split` | `confirmed` | `true` | `VERIFIED` | settled to the judge's value |
| `overturn` | `refuted` | — | `LOW` | unchanged |
| `unresolved` | (none) | — | unchanged | unchanged |

Because the judge's ruling is authoritative, it supersedes any prior skeptic verdict on a debated finding. Gray-zone cluster `merge`/`separate` decisions are recorded in `debate.json` and, as of Epic 6.1, **applied inline**: a `merge` ruling physically unions the cluster's member findings in `findings.json` during the debate stage (no Skill, no `adjudication.json` round-trip), flagging the survivor `cluster_merged` so a re-run never re-merges it; a `separate` ruling leaves the members unmerged. The Skill-authored `adjudication.json` path is unchanged and remains available as a manual override.

## Gate Semantics

The CI gate is unchanged — it reads the verdict directly, so the debate rulings slot in with no new gate logic:

- `overturn` writes `refuted`, so an overturned finding **never blocks** `--fail-on` (retained for audit).
- `uphold`/`split` write `confirmed`, so they count toward `--fail-on` and satisfy `--require-verified` (the strictest gate).
- `split` settles the severity first, so the finding gates at the judge's severity, not severity-max.

When chained (`atcr review --verify --debate --fail-on …`), the gate runs **last**, on the post-debate findings.

## Cost Controls

A debated item costs at least three provider calls (one per seat), each with a tool loop, so the stage is bounded:

- **`debate.triggers`** — disable a trigger class to skip it entirely.
- **`debate.max_items`** (default `5`; `0` = unlimited) — the cost cap. Disputed items beyond it are recorded as overflow, never debated.
- **3-turn hard cap** — non-configurable; a debate is never an open-ended conversation.
- **Per-seat budgets** — each seat reuses the reviewer tool-loop budgets (`max_turns`, `tool_budget_bytes`, `timeout_secs`) from its agent config. A tripped budget halts the seat; a halted judge yields an unresolved item.

## Artifacts

| Artifact | What cross-examination adds |
|----------|------------------------------|
| `reconciled/debate.json` | Every debated item's ruling (`outcome`, `settled_severity`, `cluster_decision`, `challenge_survived`, `single_model`, the cast, `reasoning`) plus the recorded `overflow`. |
| `reconciled/findings.json` | Each debated single-finding gains/overwrites its `verification` block with the judge's verdict and `challenge_survived`; a split overwrites the severity. |
| `reconciled/manifest.json` | `"debate"` appended to `stages` (idempotent). |
| `debate/<item-id>/transcript.jsonl` | The replayable per-item exchange: one `turn` event per seat, then the `ruling` event. |

The report gains a **Contested findings** section listing each ruling with a one-line rationale, the severity transition for splits, the judge, any single-model-fallback disclosure, and the overflow count.

## MCP tool

The `atcr_debate` MCP tool mirrors the CLI and routes through the same orchestrator, so the artifacts are identical. It accepts `id_or_path` (review id only), `singleModel`, `failOn`, and `requireVerified`, and returns `selected`, `upheld`, `overturned`, `split`, `unresolved`, `overflow`, `durationMs`, and a `gateStatus` object (omitted when `failOn` is not provided). Missing reconciled findings returns the same error as the CLI: `no reconciled findings found … run 'atcr reconcile' first`.

## Related documents

- [verification.md](verification.md) — the verify stage that produces the verification disagreements debate settles, and the shared confidence axis.
- [disagreement-radar.md](disagreement-radar.md) — the tension classes (`severity_split`, `gray_zone`, `verification_disagreement`) the debate triggers map to.
- [registry.md](registry.md) — the `role: skeptic` / `role: judge` agent configuration and the `debate.*` config block.
- [ci-integration.md](ci-integration.md) — wiring the gate into CI.
