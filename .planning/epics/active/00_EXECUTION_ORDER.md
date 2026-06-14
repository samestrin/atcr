# Epic Execution Order (Canonical)

**This file is the source of truth for what to execute next.** It overrides plain numeric
filename order. The `major.minor` numbers still identify each epic/sprint; they do **not**
dictate cross-epic sequence — this roadmap does.

The order below is optimized to **avoid wasted work**: finishing each code-conflict cluster
while it is hot, fixing data before its consumers read it, and extracting the reconciler
library only after everything that mutates the Finding struct has landed.

_Last optimized: 2026-06-14. 3.0 is in flight._

---

## Immediate next (post-3.0)

When Epic 3.0 (adversarial verification) merges, execute in this exact order:

1. **TD-009** — verify parallelism (`internal/verify/pipeline.go:159`). Resolve via
   `/resolve-td` (item 7 in the TD README). Hard prerequisite for 3.5.
2. **3.5** — Verify Audit Attribution. Same hot files; fixes `verification.json`
   (`Model` / `TrippedBudgets`) before 3.4/6.x read them.

Do **not** start 3.5 before both 3.0 is merged and TD-009 has landed (see 3.5 Sequencing).

While 3.0 is still in flight, any plan that does **not** touch `internal/verify/` is safe to
start in parallel on its own branch — e.g. **11.0** (logging) or **9.0** (file-path
validation).

---

## Full sequence

| Order | Plan | Cluster | Why here |
|------:|------|---------|----------|
| 1 | **TD-009** (verify parallelism) | V | Hot verify code; hard prereq for 3.5 |
| 2 | **3.5** Verify Audit Attribution | V | Same files as 3.0/TD-009; fixes `verification.json` before consumers read it |
| 3 | **11.0** Structured Logging | X | Low conflict; lets 9.0/10.0/4.0 emit through the shared logger from day one instead of being retrofitted |
| 4 | **3.3** Disagreement Radar | R | Projection over reconcile + (now-correct) verification.json; defines 4.0's input schema (`reconciled/disagreements.json`) |
| 5 | **3.4** Per-Run Scorecard | R | Reads corrected attribution; prereq for 6.x and persona scores |
| 6 | **9.0** File Path Validation | R | Cheap correctness; adds Finding fields → must precede 8.0; uses 11.0 logger |
| 7 | **4.0** Cross-Examination | V | Consumes 3.3's handoff schema; heavy verify/role work |
| 8 | **10.0** Executor Fix Generation | R | Runs after verify + logger; adds findings-format fields → must precede 8.0 |
| 9 | **7.0–7.3** Persona Ecosystem (split — see below) | V/R | Routing (7.x) needs 3.0; scores (7.x) need 3.4 |
| 10 | **6.0 / 6.1** Leaderboard: submission format + benchmark suite (split) | R | Extends 3.4 |
| 11 | **8.0** Reconciler Library | R | **Last of cluster R** — after 3.3/3.4/9.0/10.0 stabilize the Finding struct/reconcile output. Extracting earlier forces every later field addition across the module boundary |
| 12 | **6.2** Public leaderboard site (split) | — | Independent infra; references 8.0 methodology |
| 13 | **5.0** Executing Reviewers | V | After 4.0; design deferred. Adds `evidence_exec` (one additive field post-8.0 — see caveat) |

### Parallel / off-critical-path tracks

These do not block and are not blocked by the core sequence above:

- **3.1** Skill Integration (atcr backend swap) — external `claude-prompts` repo; unblocked
  (Epic 1.8 complete). Run whenever convenient. _(3.2 was merged into 3.1.)_
- **8.1** Team Edition Validation — ~no production code; needs 3.3 + 3.4 + local leaderboard
  shipped for atcr.dev credibility. Run in parallel once those land.

---

## Why this order — conflict clusters

The waste being avoided is **re-opening the same files across epics** and **adding fields
to a struct/format after it has shipped or been extracted**.

| Cluster | Files | Plans that touch it |
|---------|-------|---------------------|
| **V — verify pipeline** | `internal/verify/{pipeline,invoke,select}.go`, `verifyFinding` | 3.0 (done), TD-009, 3.5, 4.0, 7.x-routing, 5.0 |
| **R — reconcile output / Finding struct** | `internal/reconcile`, `internal/stream/parser.go`, Finding struct, findings format | 3.3, 3.4, 9.0, 10.0, **8.0 (extracts it)** |
| **X — cross-cutting** | `cmd/atcr`, `internal/mcp`, `internal/payload` | 11.0 (logger consumed by 9.0, 10.0, 4.0) |

Governing rules:
1. **Finish cluster V while it is hot** (TD-009 → 3.5 immediately after 3.0; 4.0 and
   7.x-routing later in the same neighborhood).
2. **Fix `verification.json` (3.5) before anything reads it** (3.4, 6.x).
3. **Land the logger (11.0) before the diagnostic-heavy epics** (9.0, 10.0, 4.0).
4. **Extract the reconciler (8.0) dead last of cluster R** — after 3.3, 3.4, 9.0, 10.0.

### What was wrong with strict numeric order
- 8.0 before 9.0/10.0 → every later Finding/format field addition becomes a cross-module
  change + version bump + re-pin. **Biggest waste.** 8.0 moved after 10.0.
- 3.5 after 3.3/3.4 and far from 3.0 → merge conflicts on hot verify code, and 3.4 reads
  wrong model attribution. 3.5 moved to immediately after 3.0.
- 11.0 dead last → ad-hoc stderr written in every intervening epic, then migrated. 11.0
  moved early.

---

## Recommended splits (not yet created as files)

Create these sub-plans when you reach the epic — don't scaffold them speculatively now.

- **6.0 → 6.0 / 6.1 / 6.2**: submission format (extends 3.4) / benchmark suite / public site
  (3-week independent infra, deferrable).
- **7.0 → 7.0 / 7.1 / 7.2 / 7.3**: bonus built-in personas (self-contained) / community
  repo + CLI / **language-aware skeptic routing (touches `internal/verify/select.go` —
  cluster V)** / per-persona corroboration scores (needs 3.4).
- **10.0 → 10.0 / 10.1**: core fix pipeline (Phases 1-3) / PR integration + config
  (Phases 4-5, independent of the engine).

---

## Caveats / open decisions

1. **7.x skeptic routing re-opens `select.go` after 4.0.** It depends on the persona install
   registry (7.x Phase 2), so it cannot ride alongside 3.5/4.0 unless a minimal
   persona-language-scope field is pulled forward. Recommendation: accept the second touch
   rather than pull the registry forward.
2. **5.0 adds `evidence_exec` to the findings format after 8.0 extracts the reconciler.**
   That is one additive, semver-friendly field post-extraction — acceptable. Only push 8.0
   after 5.0 if zero cross-module changes are required; blocking the reconciler-library asset
   behind the 4+-week deferred 5.0 is the worse trade.
