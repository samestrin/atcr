# Architecture Overview

atcr is a **review panel, not a reviewer**. It fans a single code change out to a
panel of heterogeneous LLM reviewer personas — different models, different
providers, different lenses — then deterministically reconciles their findings
into one deduplicated, confidence-scored deliverable. Cross-model agreement is
the signal: a finding two independent models both caught is worth more than
either model's opinion alone.

Two properties shape every design decision:

- **The merge is the product.** Prompts orchestrate; the deterministic Go
  reconciler does everything that must be reproducible. The same inputs always
  produce byte-identical output.
- **Disagreement is preserved, not flattened.** When reviewers disagree on
  severity or whether a finding is real, that disagreement is recorded and, where
  it matters, resolved by an explicit adversarial stage rather than silently
  averaged away.

atcr is local-first and BYO-keys: it talks to any OpenAI-compatible endpoint you
configure, and every cross-stage handoff is a machine-parseable file on disk.

## Pipeline

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
                                    v
              atcr verify  →  atcr debate  →  atcr report
```

Each stage is a separate command that reads the previous stage's on-disk
artifacts and writes its own, so any stage can be re-run, resumed, or driven by
an external orchestrator independently.

### 1. Review — fan-out to the persona pool

`atcr review` resolves the git range (base..head), builds a payload from the
diff, and fans that payload out to the configured roster of reviewer agents.
Each agent is a `(provider, model, persona)` triple; agents run across parallel
and serial lanes with per-agent fallback chains and payload/token budgets. The
per-agent and merged findings are written under
`.atcr/reviews/<id>/sources/`.

- **Payload modes** (`diff`, `blocks`, `files`) control how much code each
  reviewer sees — see [payload-modes.md](payload-modes.md).
- **Personas** are the reviewer lenses; the resolution chain and precedence are
  described in [registry.md](registry.md) and [personas-authoring.md](personas-authoring.md).
- Single-stage execution details live in [execution.md](execution.md).

### 2. Reconcile — cluster, dedupe, score confidence

`atcr reconcile` is the deterministic core. It **discovers** the source
findings, **clusters** them by location, **dedupes** near-duplicate findings
across reviewers, and scores **confidence** from cross-model agreement, then
writes the reconciled artifacts (`findings.txt`, `findings.json`, `report.md`).

- **Clustering** groups findings by AST isomorphism (the smallest covering AST
  block of each finding's line) so findings group together across line-number
  drift, with line proximity as the fallback when no parser is available. Set
  `ATCR_DISABLE_AST_GROUPING` to revert to legacy line-proximity clustering.
- **Deduplication is fixed internal behavior, not a configurable knob.** Within a
  cluster, findings are merged by structural similarity (AST isomorphism plus a
  token Jaccard measure). The cutoffs are hardcoded constants in
  `reconcile/dedupe.go` — findings at or above a `0.7` merge threshold are merged;
  the `0.4`–`0.7` gray zone is preserved as a soft/uncertain match rather than
  collapsed. (An earlier NCD-based distance was evaluated and rejected; see
  `reconcile/distance.go`.) There is no `.atcr/config.yaml` key to tune these.
- **Confidence** rises with the number of distinct reviewers that independently
  reported the same finding — the corroboration signal that drives the CI gate.

The reconciler is also published as a standalone, stdlib-only library
(`github.com/samestrin/atcr/reconcile`) so other tools can embed the exact same
merge.

### 3. Verify — adversarial skeptics

`atcr verify` runs **skeptic** agents — a different model from any reviewer that
produced a finding — that attempt to *disprove* each finding using the same
path-jailed tool loop the reviewers used. Their verdicts feed a second
confidence axis, and the gate can be configured to count only findings that
survive verification. See [verification.md](verification.md).

### 4. Debate — settle disputes

`atcr debate` cross-examines disputed findings with a proposer / challenger /
judge exchange, settling severity splits, gray-zone clusters, and
verification disagreements that the earlier stages surfaced but did not resolve.
See [cross-examination.md](cross-examination.md) and the focused
[disagreement-radar.md](disagreement-radar.md) view.

### 5. Report — render the deliverable

`atcr report` renders `md`, `json`, or `checklist` views over the reconciled
findings. The [findings format](findings-format.md) is stable and machine
-parseable for downstream consumers.

## Configuration model

Settings resolve per field across four tiers, first hit wins:

```
CLI flag  >  .atcr/*  (project)  >  ~/.config/atcr/*  (user)  >  embedded default
```

The user-facing configuration surface that tunes the pipeline is the project
config `.atcr/config.yaml` (written by `atcr init`) plus the registry files. The
reconciler-adjacent blocks are `persona`, `debate:`, `verify:`, and `executor:`;
the full reference is in [registry.md](registry.md). Provider authorization for
project-defined providers goes through `atcr trust`.

## MCP server

`atcr serve` exposes the same review engine over an MCP stdio server, so an
agentic host can drive the identical pipeline programmatically instead of through
the CLI. Stdout is kept protocol-only; logs go to stderr.

## Related documentation

- [registry.md](registry.md) — full configuration reference
- [execution.md](execution.md) — single-stage execution
- [payload-modes.md](payload-modes.md) — how much code each reviewer sees
- [verification.md](verification.md) — adversarial verification
- [cross-examination.md](cross-examination.md) — the debate stage
- [findings-format.md](findings-format.md) — the on-disk findings contract
- [ci-integration.md](ci-integration.md) — wiring atcr into a CI gate
