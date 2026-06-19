# atcr Product Roadmap

**Last Updated:** June 19, 2026
**Theme:** From prompted model panel to genuine agent team.

> **Numbering note (2026-06-19).** The *capability ladder* below is the original product
> narrative and is still accurate as a story. The **epic numbers** that implement each rung
> have since changed, and the `4.x`/`5.x` ranges were repurposed: after Epic 4.4 (metrics)
> shipped, `4.5–4.10` and `5.0–5.3` became a **reliability / validation hardening cluster**
> inserted before the higher agentic stages. Cross-examination is now **Epic 6.0** (was 4.0)
> and executing reviewers is now **Epic 11.0** (was 5.0). The "Epic index" at the bottom is
> the authoritative, current list; the prose ladder is conceptual.

atcr v1 (Epic 1.0) is honestly a *prompted model panel* — single-shot, stateless persona calls. Each "agent" sees a payload once, emits findings, and is done. The only agentic reviewer in v1 is the host Skill reviewer, because it runs inside Claude Code with tools. This roadmap is the ladder from there to a true agent team. Every stage emits into the same spine built in 1.0 (findings contract, reconciler, manifest, registry, payload engine) — nothing gets rewritten, the invoke loop gets richer and the reconciler gains new confidence inputs.

---

## The Ladder

| Stage | Epic | What changes | Headline benefit |
|-------|------|--------------|------------------|
| 1. Static panel | 1.0 (+1.1) | Fan-out + deterministic reconcile | Model diversity, agreement-confidence, CI gate; the contract layer everything else stands on |
| 2. Tool-using reviewers | 2.0 | Read-only repo tools via function calling; invoke becomes an agent loop | Evidence-grounded findings; cross-file bug classes become reachable; small models punch up by looking things up |
| 3. Adversarial verification | 3.0 | Skeptic agents try to refute each unique finding | Kills false positives — the adoption killer; makes `--fail-on` trustworthy enough to block merges |
| 4. Cross-examination | 6.0 | Structured proposer/challenger/judge exchange on disagreements | Disagreement becomes signal; `ambiguous.json` resolves automatically; strongest confidence tier |
| 5. Executing reviewers | 11.0 | Sandboxed execution: run tests, write repros | Findings become *demonstrated* bugs; EVIDENCE becomes a failing command |

Numbering: major = Epic, minor = Sprint; **executed in strict numerical order**. Stages 1–3 (Epics 1.x, 2.0, 3.x) are **complete**. The capability stages then renumbered: cross-examination is **Epic 6.0**, executing reviewers is **Epic 11.0**. Between metrics (4.4) and those agentic stages sits a reliability/validation hardening cluster (4.5–4.10, 5.0–5.3) plus differentiator and ecosystem epics (7.x executor + fixes, 8.0 reconciler library, 9.0 personas, 10.0 leaderboard, 12.0 skill integration, 13.0 team-edition validation).

---

## Stage 1 — Static panel (Epics 1.0, 1.1)

Single-shot persona calls across heterogeneous providers; deterministic reconciliation (cluster → dedupe → severity merge → agreement-confidence); payload modes (blocks/diff/files); CI exit codes; MCP server; host-review Skill.

**Why first:** agent loops without deterministic reconcile just produce expensive noise faster. 1.1 reserves schema room (registry `tools`/`max_turns`/budgets, findings `verification` block) so later stages don't break the published v1 format.

## Stage 2 — Tool-using reviewers (Epic 2.0)

Pool agents get read-only, path-jailed repo tools (`read_file`, `grep`, `list_files`) via OpenAI-compatible function calling. The Go engine owns the tool harness — this deliberately replaces what openclaw provided (tool calling + filesystem access) with a fully standalone, in-binary implementation.

**Benefits:** hallucinated line numbers and phantom APIs mostly die (the model checks instead of recalling); cross-file interaction bugs become findable (the caller passing nil, the invariant broken two packages away); low-active-parameter models compensate for weak recall with lookup.

**Cost:** 3–10× the API calls of single-shot per agent; bounded by turn caps, byte budgets, and per-agent timeouts.

## Stage 3 — Adversarial verification (Epic 3.0)

After reconcile dedupes, each unique finding goes to a skeptic — a different model with tool access, prompted to refute it. Verdicts (confirmed / refuted / unverifiable) feed a second confidence axis; refuted findings are demoted, never deleted.

**Benefits:** directly attacks false positives — a panel that is 60% noise gets ignored within a week regardless of the other 40%. Survived-a-skeptic outranks caught-by-two-reviewers. `--fail-on` counts only non-refuted findings, making the CI gate strict enough to actually enforce.

**Cost:** roughly doubles a run (mitigated by verifying only deduped findings at/above a severity floor).

## Stage 4 — Cross-examination (Epic 6.0)

Where reviewers disagree (severity disputes, similarity gray zone), a short structured exchange runs: the finder defends, a challenger attacks, a judge rules. Converts the `ambiguous.json` sidecar from "punt to the Skill" into an automated resolution stage.

**Benefits:** disagreements become signal; the reconciler's gray zone shrinks; findings that survive hostile challenge from a different model form the top confidence tier; fewer items punted to humans.

## Stage 5 — Executing reviewers (Epic 11.0)

Opt-in sandboxed execution: agents run the test suite, write minimal repros, demonstrate the bug. Serious sandboxing (containers, resource caps) — which is why it is last.

**Benefits:** a demonstrated bug with a repro command is a categorically different deliverable than an opinion; the EVIDENCE column becomes executable proof.

---

## Cost discipline across stages

Each stage multiplies tokens and latency. The v1 machinery — lanes, per-agent timeouts, byte budgets, partial-success semantics, recorded truncation — is the cost-control plane the agentic stages run on. Every stage must keep: deterministic artifacts on disk, per-agent status accounting, partial-success semantics, and the versioned findings contract.

## Epic index

Executed in numerical order. **Complete** epics (1.0–4.4) live in `.planning/epics/completed/`;
**active** epics (4.5–13.0) live in `.planning/epics/active/`.

| Epic | Title | Status |
|------|-------|--------|
| 1.0 – 3.6 | Core, schema, tool-using reviewers, adversarial verification, scorecard/radar | ✅ Complete |
| 4.0 – 4.4 | Structured logging, graceful shutdown + resume (4.1.1), config/input validation, metrics | ✅ Complete |
| 4.5 | Circuit Breaker & Provider Health Tracking | 🔧 In progress (active branch) |
| 4.6 | Robust Rate-Limit & Backoff Handling | Draft (runs right after 4.5; renumbered from 4.10) |
| 4.7 | Idempotency & Safe Retry | Draft (composes with shipped `--resume`) |
| 4.8 | MCP Server Test Coverage (≥80%) | Draft |
| 4.9 | Exact-Value Secret Redaction in Logs | Draft |
| 4.10 | Health Checks for MCP Server | ⛔ Deferred (blocked — `atcr serve` is stdio-only; renumbered from 4.6) |
| 5.0 | File Path Validation & Correction | Draft (High — correctness) |
| 5.1 | OpenAI-Compatible Conformance Suite | Planned |
| 5.2 | Diff Caching & Incremental Reviews | Draft |
| 5.3 | Anthropic-Native Conformance | Draft (stub — gap closure) |
| 6.0 | Cross-Examination (Debate Stage) | Draft (ready) |
| 7.0 | Executor Model for Fix Generation | Draft (High — differentiator) |
| 7.1 | Local Syntax/Compile Guard for Fixes | Draft (after 7.0) |
| 7.2 | Radar Renderer Consolidation | Draft (TD refactor) |
| 7.3 | GitHub Action / PR Integration | Draft (stub — gap closure; renders 7.0 fixes on PRs) |
| 8.0 | Reconciler Library (standalone module) | Draft |
| 9.0 | Persona Ecosystem | Draft |
| 10.0 | Model-Eval Leaderboard | Draft |
| 11.0 | Executing Reviewers (sandboxed) | Draft (outline; after 6.0) |
| 12.0 | Skill Integration (atcr backend swap) | Draft (off critical path, unblocked) |
| 13.0 | Team Edition Validation | Draft (after 10.0) |

> **Open sequencing decision:** 5.0 (File Path Validation, High/correctness) and 7.0 (Executor,
> High/differentiator) currently execute *after* the lower-value 4.x ops cluster. The strict
> numeric order is dependency-valid, but if high-value work should ship sooner those epics need
> to be renumbered into the 4.x range — a deliberate strategic call, not yet made.
