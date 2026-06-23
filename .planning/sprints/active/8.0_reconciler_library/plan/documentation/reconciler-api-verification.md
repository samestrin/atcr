# Reconciler Public API & Verification Interface [CRITICAL]

## Overview

Epic 8.0 extracts ATCR's deterministic reconciler out of `internal/reconcile` into a standalone, stdlib-only Go module (`github.com/samestrin/atcr/reconcile`), consumed via a root `replace` directive, with zero behavioral change to ATCR. The central Phase-0 task is a public/private split: `emit.go` and `discover.go` each mix public types (`Verification`, `JSONFinding`, `Verdict` consts, `Source`) with ATCR-internal file I/O, and the extraction moves the public types + pure logic into the library while file I/O + path-validation stays in an ATCR adapter package. The public API surface is lifted as-is — `Reconcile(sources []Source, opts Options) Result` and `Options{ReconciledAt, Partial, Merges, Root}` stay exactly; the epic's proposed clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) is deferred to a follow-on epic.

> Source: [codebase-discovery.json:architecture_notes / "CORE ENTANGLEMENT"]
> Source: [codebase-discovery.json:architecture_notes / "LIFT-AS-IS"]

The verification contract defines the public/private boundary. `internal/reconcile/merge.go` is the heart of the reconciler and the central extraction target: it holds the finding-merge rules (Merge, mergeSeverity, modalCategory, mergeEvidence, confidenceFor), the `Merged` struct that embeds `stream.Finding + Disagreement + *Verification` (the exact entanglement the epic flagged), and the `mergeVerification` + `verdictRank` verify-precedence logic. `mergeVerification` (confirmed>unverifiable>refuted, skeptic provenance unioned) travels with the library because it is embedded in `Merged` and used inside the merge algorithm. `Merged` embeds `*Verification`; `gate.go` (ATCR) and `internal/debate` both read/mutate it, so `Verification` becomes public library API and consumers import the library.

> Source: [codebase-discovery.json:build_from / primary_file]
> Source: [codebase-discovery.json:semantic_matches / mergeVerification]
> Source: [codebase-discovery.json:integration_points / "internal/reconcile/merge.go:Merged.Verification"]

The `Verification` struct (fields Skeptic/Verdict/Notes), the verdict enum (`VerdictConfirmed/Refuted/Unverifiable`), and the confidence v2 ordinal (`VERIFIED/HIGH/MEDIUM/LOW`) move to the library as public API. ATCR-internal consumers — `gate.go` (CI pass/fail predicates), `validate.go` (path validation), and `emit.go` file I/O — import these now-public types unchanged; no visibility change is required because the verdict constants are already exported. The `atcr verify` CLI, the `atcr_verify` MCP tool, and the `--fail-on` / `--require-verified` gate semantics stay ATCR-side, consuming the library's `Verification` + `Verdict` constants.

> Source: [codebase-discovery.json:architecture_notes / "Verdict constants"]
> Source: [codebase-discovery.json:semantic_matches / gate.go]
> Source: [adversarial-verification-interface.md:Scope]

## Key Concepts

### Lifted-as-is public API

`Reconcile(sources []Source, opts Options) Result` is the public pipeline entry (reconcile.go:64), lifted as-is per AC#3. `Options{ReconciledAt, Partial, Merges, Root}` is kept exactly; the proposed clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) is deferred. `Source.Findings` becomes `[]reconcile.Finding` (library-owned) and ATCR converts `stream.Finding` at the boundary.

> Source: [codebase-discovery.json:semantic_matches / Reconcile]
> Source: [codebase-discovery.json:architecture_notes / "LIFT-AS-IS"]
> Source: [codebase-discovery.json:integration_points / "internal/reconcile/reconcile.go:Reconcile"]

### Merged struct & mergeVerification

`merge.go` holds the finding-merge rules and the `Merged` struct embedding `stream.Finding + Disagreement + *Verification`, plus `mergeVerification` (merge.go:418) and `verdictRank`. `mergeVerification` is the verify-precedence merge (confirmed>unverifiable>refuted, skeptic provenance unioned). It travels with the library because it is embedded in `Merged` and used inside the merge algorithm.

> Source: [codebase-discovery.json:build_from / primary_file]
> Source: [codebase-discovery.json:semantic_matches / mergeVerification]

### Verification struct fields (Skeptic/Verdict/Notes)

For each finding with a non-nil `verification` block, the report shows the skeptic agent name (`Verification.Skeptic`), the verdict (`Verification.Verdict` — `confirmed`, `refuted`, `unverifiable`), and the reasoning (`Verification.Notes`). The model is sourced from `reconciled/verification.json`; the per-finding `verification` block in `findings.json` carries only verdict/skeptic/notes, not the model. `emit.go:40` defines `Verification` (line 40) alongside the file-I/O emit layer — this mixing of public types with I/O is the core extraction split.

> Source: [adversarial-verification-interface.md:Report Rendering / Skeptic section]
> Source: [codebase-discovery.json:semantic_matches / emit.go]

### Verdict enum (confirmed / refuted / unverifiable)

`emit.go` defines `VerdictConfirmed/Refuted/Unverifiable` (lines 61-63). These constants are already exported; `gate.go` (ATCR-internal), `confidence.go`, `disagree.go`, and `merge.go` all use them. They move to the library unchanged; `gate.go` imports them. No visibility change. `Merged` embeds `*Verification`; `gate.go` and `internal/debate` both read/mutate it, so `Verification` becomes public library API.

> Source: [codebase-discovery.json:semantic_matches / emit.go]
> Source: [codebase-discovery.json:architecture_notes / "Verdict constants"]
> Source: [codebase-discovery.json:integration_points / "internal/reconcile/merge.go:Merged.Verification"]

### Two finding types (input vs wire)

`stream.Finding` is the per-source input; `JSONFinding` is the reconciled wire form carrying the `Reviewers` list + path-validation fields + `Verification` + `ClusterMerged`. The library defines ONE `Finding` carrying the core wire fields (Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer(s), Confidence) + Disagreement + Verification. ATCR's `JSONFinding` embeds/wraps it and adds PathValid/PathWarning/PathSuggestion/ClusterMerged at the adapter boundary.

> Source: [codebase-discovery.json:existing_patterns / "Input vs wire type split"]
> Source: [codebase-discovery.json:architecture_notes / "TWO FINDING TYPES"]

### Source type

`discover.go:25` defines the public `Source` type alongside the findings.txt file-discovery I/O. The type moves to the library; the `Discover()` file reading stays ATCR-internal.

> Source: [codebase-discovery.json:semantic_matches / discover.go]

### Confidence v2 (VERIFIED/HIGH/MEDIUM/LOW)

`confidence.go:22` defines `ConfidenceForVerdict`/`ConfidenceAtOrAbove` — the confidence-from-verdict rule and v2 confidence ordinal, consuming `VerdictConfirmed/Refuted`. Moves to the library; `verdictRank` lives in `merge.go`. Confidence tiers render highest to lowest: `VERIFIED`, `HIGH`, `MEDIUM`, `LOW`.

> Source: [codebase-discovery.json:semantic_matches / confidence.go]
> Source: [adversarial-verification-interface.md:Report Rendering / Confidence v2 ordering]

### Gate semantics (ATCR-internal)

`gate.go:96` defines `IsFailing`/`CountAtOrAbove` — ATCR-internal gate predicates that consume `VerdictRefuted/VerdictConfirmed` from `Merged.Verification`. Stays in ATCR; imports the library's now-public `Verification` + `Verdict` constants. `--fail-on <severity>` counts findings at or above the threshold whose verdict is not `refuted`; `--require-verified` narrows that to findings whose confidence is `VERIFIED`. Gate precedence resolves via flag > project config > registry; unset is a no-op.

> Source: [codebase-discovery.json:semantic_matches / gate.go]
> Source: [adversarial-verification-interface.md:Gate Semantics]
> Source: [adversarial-verification-interface.md:CLI Command / Flags]

### Extraction scope boundary

In scope: `Verification` struct + verdict enum + `mergeVerification` (travels with the library because it is embedded in `Merged` and used inside the merge algorithm) + library-owned `Finding`. Out of scope: `gate.go` (CI pass/fail), `validate.go` (path validation), `emit.go` file I/O, path-validation fields (PathValid/PathWarning/PathSuggestion).

> Source: [codebase-discovery.json:original-requirements.md scope]

### Verification interface (CLI + MCP)

The `atcr verify` CLI subcommand, the `atcr_verify` MCP tool, the `atcr review --verify` chaining behavior, the verified report rendering conventions, and the CI gate semantics are exposed through both CLI and MCP. The MCP handler (`internal/mcp/handlers.go:handleReconcile`) shares the same boundary adapter and gate semantics with the CLI.

> Source: [adversarial-verification-interface.md:Scope]
> Source: [codebase-discovery.json:integration_points / "internal/mcp/handlers.go:handleReconcile"]

## Code Examples

The blocks below are quoted verbatim from the sources. No Go library type definitions are invented — the epic's proposed clean API is deferred, so only the lifted-as-is surface and the verification interface schemas (which exist in the sources) are shown.

### `atcr verify` CLI form (verbatim)

```
atcr verify [id-or-path]
```

- `id-or-path` is optional and follows the same resolution rules as `atcr reconcile`:
  - bare review id → `.atcr/reviews/<id>/`
  - path → used as-is
  - omitted → `.atcr/latest`

> Source: [adversarial-verification-interface.md:CLI Command / Form]

### `atcr verify` flags (verbatim)

| Flag | Type | Default | Meaning |
|------|------|---------|---------|
| `--fresh` | bool | false | Re-verify every finding, even those already verified in a previous run. |
| `--thorough` | bool | false | Use 3 skeptics with majority rule instead of the configured `verify.votes` default. |
| `--min-severity` | string | `MEDIUM` | Findings below this severity skip verification and keep v1 confidence. |
| `--fail-on` | string | (unset) | Exit 1 if any finding at/above this severity survives (verdict ≠ `refuted`). Resolved via the shared gate precedence (flag > project config > registry); unset is a no-op. |
| `--require-verified` | bool | false | Only meaningful with `--fail-on`: count only findings whose confidence is `VERIFIED`. |

> Source: [adversarial-verification-interface.md:CLI Command / Flags]

### `atcr verify` exit codes (verbatim)

- `0` — verification completed (gate passed or no gate requested)
- `1` — verification completed but `--fail-on` / `--require-verified` gate failed
- `2` — usage or configuration error (missing reconciled input, bad severity, etc.)

> Source: [adversarial-verification-interface.md:CLI Command / Exit codes]

### `atcr_verify` MCP output schema (verbatim)

```json
{
  "review_id": "string",
  "verdict_counts": {
    "confirmed": 0,
    "refuted": 0,
    "unverifiable": 0
  },
  "findings_processed": 0,
  "duration_ms": 0,
  "gate_status": {
    "pass": true,
    "failing_count": 0
  }
}
```

`gate_status` is omitted when `fail_on` is not provided.

> Source: [adversarial-verification-interface.md:MCP Tool / Output schema]

### Confidence v2 ordering (verbatim)

Confidence tiers render in this order, highest to lowest:

1. `VERIFIED`
2. `HIGH`
3. `MEDIUM`
4. `LOW`

> Source: [adversarial-verification-interface.md:Report Rendering / Confidence v2 ordering]

### Gate semantics examples (verbatim)

| Finding | v1 confidence | verdict | `--fail-on high` | `--fail-on high --require-verified` |
|---------|---------------|---------|------------------|-------------------------------------|
| F1 | HIGH | confirmed (VERIFIED) | counts | counts |
| F2 | HIGH | refuted (LOW) | does not count | does not count |
| F3 | HIGH | unverifiable | counts | does not count |
| F4 | MEDIUM | confirmed (VERIFIED) | does not count | does not count |

> Source: [adversarial-verification-interface.md:Gate Semantics / Examples]

## Quick Reference

| Symbol / Type | Defined At | Moves to Library? | Stays ATCR-Internal | Source |
|---------------|------------|-------------------|----------------------|--------|
| `Reconcile(sources []Source, opts Options) Result` | internal/reconcile/reconcile.go:64 | Yes (lifted as-is) | — | codebase-discovery.json:semantic_matches / Reconcile |
| `Options{ReconciledAt, Partial, Merges, Root}` | internal/reconcile/reconcile.go | Yes (kept exactly) | — | codebase-discovery.json:architecture_notes / "LIFT-AS-IS" |
| `Result`, `Summary` | internal/reconcile/reconcile.go | Yes | — | codebase-discovery.json:semantic_matches / Reconcile |
| `Source` | internal/reconcile/discover.go:25 | Yes | `Discover()` file reading stays | codebase-discovery.json:semantic_matches / discover.go |
| `Merged` (embeds `stream.Finding + Disagreement + *Verification`) | internal/reconcile/merge.go | Yes | — | codebase-discovery.json:build_from / primary_file |
| `mergeVerification` + `verdictRank` | internal/reconcile/merge.go:418 | Yes (embedded in Merged) | — | codebase-discovery.json:semantic_matches / mergeVerification |
| `Verification` (Skeptic/Verdict/Notes) | internal/reconcile/emit.go:40 | Yes (public API) | — | adversarial-verification-interface.md:Report Rendering / Skeptic section; codebase-discovery.json:semantic_matches / emit.go |
| `VerdictConfirmed`/`Refuted`/`Unverifiable` | internal/reconcile/emit.go:61-63 | Yes (unchanged) | `gate.go` imports them | codebase-discovery.json:architecture_notes / "Verdict constants" |
| `Finding` (library-owned) | new (library) | Yes | — | codebase-discovery.json:architecture_notes / "TWO FINDING TYPES" |
| `JSONFinding` (wraps Finding + path fields) | internal/reconcile/emit.go:74 | — | Yes (adapter) | codebase-discovery.json:semantic_matches / emit.go |
| `ConfidenceForVerdict`/`ConfidenceAtOrAbove` | internal/reconcile/confidence.go:22 | Yes | — | codebase-discovery.json:semantic_matches / confidence.go |
| `BuildDisagreements` | internal/reconcile/disagree.go:102 | Yes | — | codebase-discovery.json:semantic_matches / disagree.go |
| `IsFailing`/`CountAtOrAbove` (gate) | internal/reconcile/gate.go:96 | — | Yes (imports library `Verification`+`Verdict`) | codebase-discovery.json:semantic_matches / gate.go |
| `gate.go` (CI pass/fail) | internal/reconcile/gate.go | — | Yes | codebase-discovery.json:original-requirements.md scope |
| `validate.go` + path fields (PathValid/PathWarning/PathSuggestion) | internal/reconcile/validate.go | — | Yes | codebase-discovery.json:original-requirements.md scope |
| `emit.go` file I/O | internal/reconcile/emit.go | — | Yes | codebase-discovery.json:original-requirements.md scope |
| `atcr verify` CLI / `atcr_verify` MCP / `--fail-on` / `--require-verified` | ATCR (cmd + internal/mcp) | — | Yes (consumes library types) | adversarial-verification-interface.md:Scope |

## Related Documentation

- [Adversarial Verification Interface (design concept)](../../specifications/design-concepts/adversarial-verification-interface.md) — the verbatim source for the `atcr verify` CLI, `atcr_verify` MCP schema, report rendering, and gate semantics cited above.
- [codebase-discovery.json](../codebase-discovery.json) — build_from, semantic_matches, architecture_notes, integration_points, and original-requirements scope that ground the lifted-as-is public API surface and the public/private split.
- [Plan: 8.0 Reconciler Library](../plan.md) — the extraction epic this document supports.
