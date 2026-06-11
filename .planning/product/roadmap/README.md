# atcr Product Roadmap

**Last Updated:** June 10, 2026
**Theme:** From prompted model panel to genuine agent team.

atcr v1 (Epic 1.0) is honestly a *prompted model panel* — single-shot, stateless persona calls. Each "agent" sees a payload once, emits findings, and is done. The only agentic reviewer in v1 is the host Skill reviewer, because it runs inside Claude Code with tools. This roadmap is the ladder from there to a true agent team. Every stage emits into the same spine built in 1.0 (findings contract, reconciler, manifest, registry, payload engine) — nothing gets rewritten, the invoke loop gets richer and the reconciler gains new confidence inputs.

---

## The Ladder

| Stage | Epic | What changes | Headline benefit |
|-------|------|--------------|------------------|
| 1. Static panel | 1.0 (+1.1) | Fan-out + deterministic reconcile | Model diversity, agreement-confidence, CI gate; the contract layer everything else stands on |
| 2. Tool-using reviewers | 2.0 | Read-only repo tools via function calling; invoke becomes an agent loop | Evidence-grounded findings; cross-file bug classes become reachable; small models punch up by looking things up |
| 3. Adversarial verification | 3.0 | Skeptic agents try to refute each unique finding | Kills false positives — the adoption killer; makes `--fail-on` trustworthy enough to block merges |
| 4. Cross-examination | 4.0 | Structured proposer/challenger/judge exchange on disagreements | Disagreement becomes signal; `ambiguous.json` resolves automatically; strongest confidence tier |
| 5. Executing reviewers | 5.0 | Sandboxed execution: run tests, write repros | Findings become *demonstrated* bugs; EVIDENCE becomes a failing command |

Numbering: major = Epic, minor = Sprint; executed in numerical order. Stages 2 and 3 are committed for immediate execution after 1.x; 4 and 5 are drafted and sequenced but may be re-scoped after field experience with 2–3.

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

## Stage 4 — Cross-examination (Epic 4.0)

Where reviewers disagree (severity disputes, similarity gray zone), a short structured exchange runs: the finder defends, a challenger attacks, a judge rules. Converts the `ambiguous.json` sidecar from "punt to the Skill" into an automated resolution stage.

**Benefits:** disagreements become signal; the reconciler's gray zone shrinks; findings that survive hostile challenge from a different model form the top confidence tier; fewer items punted to humans.

## Stage 5 — Executing reviewers (Epic 5.0)

Opt-in sandboxed execution: agents run the test suite, write minimal repros, demonstrate the bug. Serious sandboxing (containers, resource caps) — which is why it is last.

**Benefits:** a demonstrated bug with a repro command is a categorically different deliverable than an opinion; the EVIDENCE column becomes executable proof.

---

## Cost discipline across stages

Each stage multiplies tokens and latency. The v1 machinery — lanes, per-agent timeouts, byte budgets, partial-success semantics, recorded truncation — is the cost-control plane the agentic stages run on. Every stage must keep: deterministic artifacts on disk, per-agent status accounting, partial-success semantics, and the versioned findings contract.

## Epic index

| Epic | File | Status |
|------|------|--------|
| 1.0 atcr Core | `.planning/plans/active/1.0_atcr_core/` (plan) / `.planning/epics/active/1.0_atcr_core.md` (origin) | Planning |
| 1.1 Schema Reservations | `.planning/epics/active/1.1_schema_reservations.md` | Queued |
| 2.0 Tool-Using Reviewers | `.planning/epics/active/2.0_tool_using_reviewers.md` | Queued (immediate after 1.x) |
| 3.0 Adversarial Verification | `.planning/epics/active/3.0_adversarial_verification.md` | Queued (immediate after 2.0) |
| 4.0 Cross-Examination | `.planning/epics/active/4.0_cross_examination.md` | Drafted |
| 5.0 Executing Reviewers | `.planning/epics/active/5.0_executing_reviewers.md` | Drafted |
