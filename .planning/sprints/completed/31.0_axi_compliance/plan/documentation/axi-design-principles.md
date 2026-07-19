# AXI Design Principles (axi.md) — the Epic's Reference Source

**Priority:** Important

## Overview

`original-requirements.md` cites exactly one external reference for this plan: [https://axi.md](https://axi.md). It is a position paper plus benchmark study (Kun Chen) presenting **10 design principles for agent-ergonomic CLI tools**, validated across two benchmarks — 490 browser-automation runs (14 tasks × 7 conditions × 5 repeats) and a separate 425-run GitHub benchmark — comparing CLI, MCP, code-mode, and AXI interface conditions. The headline result: the AXI condition achieved 100% task success at the lowest cost ($0.074/task browser, $0.050 GitHub), fewest turns (4.5), and shortest duration (21.5s), while MCP conditions consumed 2.3× more input tokens per task (185K vs. 79K).

Two facts make this document load-bearing rather than background reading. First, **Principle 1 mandates TOON by name** — "Use TOON (Token-Optimized Object Notation) format *instead of JSON* or tab-separated tables" — which resolves the epic's "TOON or compact JSON" hedge: the epic's own reference source is unambiguous, so a non-TOON encoding is a deviation that design must explicitly justify (see [toon-format-reference.md](toon-format-reference.md) for the syntax itself). Second, the epic restates only a subset of the principles (output format, truncation, exit codes, stderr isolation); the plan's designer needs the full list to know which principles plan 31.0 covers, which it deliberately defers, and **where axi.md's guidance diverges from atcr's existing contracts** — most sharply on exit codes, where axi.md's Principle 6 is a *third* contract beyond the two already tracked in [exit-code-cli-mcp-precedent.md](exit-code-cli-mcp-precedent.md).

## Key Concepts

### The 10 principles, mapped to plan 31.0 scope

| # | Principle (axi.md) | Plan 31.0 status |
|---|---|---|
| 1 | Token-efficient output — TOON, ~40% token savings over JSON | **In scope** — the `--axi` payload itself |
| 2 | Minimal default schemas — 3–4 fields per list item, `--fields` for more | **In scope, with tension** — findings are 8–9 columns; see below |
| 3 | Content truncation — size hints + `--full`-style escape hatch | **In scope** — maps to the 500-line cap, `truncated` flag, `ATCR_AXI_MAX_LINES` |
| 4 | Pre-computed aggregates — e.g. `totalCount`, status summaries | **Partially in scope** — the axi payload can carry run/finding counts; the existing review summary is the human analog |
| 5 | Definitive empty states — explicit "0 results", never bare empty output | **In scope, with tension** — TOON encodes an empty object as empty output; see below |
| 6 | Structured errors & exit codes — errors to **stdout**, no prompts, 0=success/1=errors/2=unknown flags | **In scope, with divergence** — a third exit-code contract; see below |
| 7 | Ambient context — session hooks/plugins + installable skill | Out of scope for 31.0 (not mentioned by the epic) |
| 8 | Content first — no-args shows live data, not help | Out of scope for 31.0 |
| 9 | Contextual disclosure — `help[]` next-step suggestions after output | Out of scope for 31.0 (candidate future enhancement) |
| 10 | Consistent way to get help — concise per-subcommand `--help` | Already satisfied by Cobra; nothing to build |

### Principle 1: TOON is mandated, not suggested

> Source: [axi.md: Efficiency → 1. Token-efficient output](https://axi.md)

"Use TOON (Token-Optimized Object Notation) format instead of JSON or tab-separated tables. TOON omits braces, quotes, and commas, yielding approximately 40% token savings over equivalent JSON while remaining unambiguous to LLMs."

### Principle 3: truncation carries a size hint and an escape hatch

> Source: [axi.md: Efficiency → 3. Content truncation](https://axi.md)

"Truncate large text fields to a configurable limit, appending a size hint such as `(truncated, 2847 chars total — use --full to see complete body)`." This refines the plan's pagination AC: the `truncated` flag should be accompanied by a human-and-agent-readable size hint and a documented way to retrieve the untruncated payload — a stronger contract than a bare boolean, and compatible with the never-silent truncation precedent in [agentic-format-precedents.md](agentic-format-precedents.md).

### Principle 5 vs. TOON's empty-object rule — a concrete design fork

axi.md requires an explicit zero-result signal: "Agents cannot distinguish 'no output' from 'command failed silently' without this signal." But the TOON cheatsheet encodes an empty JSON object as *empty output* and an empty array as `key[0]:` (see [toon-format-reference.md](toon-format-reference.md): Empty containers). For a zero-findings review, the axi payload must therefore be an explicit empty-state structure (e.g., a `findings[0]:` header plus run metadata), never a zero-byte stdout — the design must choose this deliberately, because the naive TOON encoding of `{}` would violate Principle 5.

### Principle 6: a third exit-code contract, and errors on stdout

> Source: [axi.md: Robustness → 6. Structured errors & exit codes](https://axi.md)

"Mutations should be idempotent, errors should be structured and written to **stdout (not stderr)**, and commands must never prompt for interactive input. Reserve stdout for structured data and stderr for debug/log output. Use clean exit codes: **0 for success, 1 for errors**. Unknown flags must fail loud (**exit 2**)."

Mapped against the two contracts already tracked in [exit-code-cli-mcp-precedent.md](exit-code-cli-mcp-precedent.md):

| Code | axi.md P6 | atcr existing (main.go / ci-integration.md) | Epic proposal |
|---|---|---|---|
| `0` | success | clean (no gate failure) | success |
| `1` | errors (any failure) | gate-failure (a *deliberate* CI signal, not an error) | actionable findings |
| `2` | unknown flags (fail loud) | usage/configuration error | internal/syntax error |
| `3` | — | auth-error | — |

Reconciliation notes for design: atcr's `2`=usage-error comfortably absorbs axi.md's unknown-flags case (unknown flags already fail as usage errors via Cobra); the epic's `2`=internal/syntax remains the outlier. The real question is `1`: axi.md's coarse success/error binary has no concept of atcr's gate-failure-as-designed-signal, and the epic's "1 = actionable findings" aligns with neither — the existing atcr contract (1 = gate-failure when `--fail-on` is set, 0 otherwise even with findings) is the only reading that doesn't break CI scripts, and it can be *framed* to agents as the command's defined failure signal. Separately, axi.md's "structured errors to stdout" diverges from atcr's errors-on-stderr convention; in axi mode this is arguably correct (the agent reads stdout, stderr is for logs), and it only applies to the `--axi` surface — design should decide it explicitly rather than inherit by accident.

### Principle 2 vs. the findings schema width

axi.md recommends 3–4 fields per list item by default with a `--fields` escape hatch. The reconciled findings stream is 9 columns (8 per-source). Options the design should weigh: emit the full column set (findings are already token-lean, pipe-delimited rows), or define a default subset (e.g., `SEVERITY`, `FILE:LINE`, `PROBLEM`, `FIX`) with a flag to widen. This interacts with the schema decision in [agentic-format-precedents.md](agentic-format-precedents.md) — but note the precedent doc's warning applies: whatever is chosen must not silently misalign with the `atcr-findings/v1` grammar consumers already parse.

### Why the format matters: the benchmark evidence

> Source: [axi.md: Results, Analysis](https://axi.md)

AXI's cost advantage is mechanistic, not stylistic: MCP tool-schema overhead alone was 2.3× input tokens (185K vs. 79K per task); on the complex `ci_failure_investigation` GitHub task the gap reached 12× ($0.065 AXI vs. $0.758 MCP). Specialized extraction commands, combined operations, and shell composability (`| grep`, `| tail`) drove the rest. For atcr this validates the epic's core premise — the *format and shape* of output, not the transport, is what makes a tool agent-ergonomic — and it is the quantitative backing for the plan's token-budget ACs.

## Code Examples

The following are verbatim from the source document.

### Principle 1's JSON→TOON comparison

> Source: [axi.md: 1. Token-efficient output](https://axi.md)

Conventional (JSON):

```json
[{"number":42,"title":"Fix login bug","state":"open",
 "author":"alice","labels":["bug","P1"]},
{"number":43,"title":"Add dark mode","state":"open",
 "author":"bob","labels":["feature"]}]
```

AXI (TOON):

```
issues[2]{number,title,state}:
  42,Fix login bug,open
  43,Add dark mode,open
```

### Principle 4's aggregate + TOON list example

> Source: [axi.md: 4. Pre-computed aggregates](https://axi.md)

```
$ gh-axi label list
count: 126
labels[126]{name}:
  bug
  docs
  ...
```

### Principle 3's truncation size hint

> Source: [axi.md: 3. Content truncation](https://axi.md)

```
(truncated, 2847 chars total — use --full to see complete body)
```

## Quick Reference

| axi.md requirement | Where plan 31.0 stands |
|---|---|
| TOON output (P1) | Mandated by the epic's own Reference; syntax excerpted in [toon-format-reference.md](toon-format-reference.md) |
| Truncation with size hint + escape hatch (P3) | Fold into the `truncated`-flag AC: emit `(truncated, N lines total …)`-style hint + document how to get the full payload |
| Definitive empty state (P5) | Zero-findings payload must be explicit (`findings[0]:` + metadata), never empty stdout — overrides TOON's empty-object rule |
| 0/1/2 with unknown-flags=2 (P6) | Reconciles cleanly onto atcr's existing 0/1/2/3 *except* the meaning of `1`; keep gate-failure=1, frame it as the defined failure signal; reject the epic's 2=internal/syntax reassignment |
| Structured errors on stdout (P6) | Diverges from atcr's stderr convention; decide explicitly for the `--axi` surface only |
| 3–4 default fields (P2) | In tension with 8–9-column findings; design picks full-width vs. default-subset + widen flag |
| Aggregates like `totalCount` (P4) | Cheap to honor: run/finding counts in the axi payload header |
| Ambient context, content-first, contextual disclosure (P7–P9) | Out of scope for 31.0; record as candidates for future epics |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- [axi.md](https://axi.md) — the source document (principles, benchmarks, case studies)
- [TOON Format Reference](toon-format-reference.md) — the syntax Principle 1 mandates
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent](exit-code-cli-mcp-precedent.md) — the two other exit-code contracts Principle 6 must reconcile with
- [Existing Agent-Facing Format & Output-Safety Contracts](agentic-format-precedents.md) — the in-repo precedents (findings/v1, truncation, sanitization, golden tests) the axi payload must align with
