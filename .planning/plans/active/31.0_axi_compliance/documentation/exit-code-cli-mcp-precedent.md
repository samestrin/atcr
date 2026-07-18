# Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)

**Priority: Critical**

## Overview

`adversarial-verification-interface.md` is the design concept for Epic 3.0's `atcr verify` feature: a CLI subcommand, a chained `atcr review --verify` mode, and a parallel `atcr_verify` MCP tool that verify review findings via a skeptic pipeline and expose a CI gate through `--fail-on` / `--require-verified`. It is explicitly "grounded in the existing CLI/MCP patterns in `cmd/atcr/` and `internal/mcp/`" (Source: adversarial-verification-interface.md:13), meaning it was written to conform to atcr's established conventions rather than invent new ones.

Two things make this document load-bearing precedent for the AXI plan rather than merely adjacent reading. First, it documents an exit-code contract — `0` / `1` / `2` — for a brand-new subcommand shipped in the same codebase AXI must integrate with, and that contract's meanings are the ones codebase-discovery.json says already govern `atcr review`/`atcr reconcile --fail-on` today (0=clean/1=gate-failure/2=usage-error/3=auth-error). Second, it documents a capability (`verify`) exposed through both a CLI subcommand and an MCP tool with schemas that are parallel in shape but distinct in field naming and detail (e.g., CLI `--fail-on` vs MCP `fail_on`, and an MCP-only `gate_status` object) — a working example of the CLI/MCP parity question the AXI plan faces with a prospective `FormatAXI` report format.

This document does not describe the AXI feature itself; it describes the nearest prior feature that had to answer the same two structural questions (what do exit codes mean, and how does a dual CLI/MCP surface stay in sync) that the AXI plan must now answer for itself.

## Key Concepts

### Exit-code contract (verify subcommand)

> Source: adversarial-verification-interface.md:40-45 (## CLI Command: `atcr verify` > ### Exit codes)

- `0` — verification completed (gate passed or no gate requested)
- `1` — verification completed but `--fail-on` / `--require-verified` gate failed
- `2` — usage or configuration error (missing reconciled input, bad severity, etc.)

This is a **0=success / 1=gate-failure / 2=usage-error** scheme. Per codebase-discovery.json's architecture_note, this is the same mapping already used by `atcr review`/`atcr reconcile --fail-on`: 0=clean / 1=gate-failure / 2=usage-error are documented for CI gating in `docs/ci-integration.md`'s exit-semantics table (lines 11-19), and `cmd/atcr/main.go:126-129` additionally defines `3`=auth-error, which the CI table does not include — `atcr verify` did not introduce a new contract, it adopted the existing one, including reserving `2` for usage/config errors, not internal/syntax errors.

### Chained CLI behavior shares the same exit semantics

> Source: adversarial-verification-interface.md:64-79 (## CLI Chaining: `atcr review --verify`)

"`--verify` is compatible with the existing `--fail-on` flag and shares the same exit-code semantics. It does not introduce a separate `--reconcile` flag; reconcile is always run when `--verify` is set." (Source: adversarial-verification-interface.md:79) — a second, explicit statement that a new capability was deliberately folded into the existing exit-code contract rather than given its own.

### MCP tool is a distinct schema, not a CLI passthrough

> Source: adversarial-verification-interface.md:83-124 (## MCP Tool: `atcr_verify`)

The MCP input schema renames flags to snake_case (`id_or_path`, `fail_on`, `require_verified`) and the output schema adds structure the CLI's stdout summary does not have verbatim — a nested `verdict_counts` object and a `gate_status` object that is "omitted when `fail_on` is not provided" (Source: adversarial-verification-interface.md:117). This is the concrete precedent for "parallel but distinct schemas": the MCP tool exposes the same underlying capability as the CLI but is not a literal mirror of its flags or output text.

### MCP tool success/error boundary does not reuse CLI exit codes

> Source: adversarial-verification-interface.md:119-124 (### Error behavior)

"Individual skeptic failures → captured as `unverifiable` verdicts; the tool call itself succeeds" (Source: adversarial-verification-interface.md:123). The MCP surface has its own error-vs-success boundary (tool call success/failure, error message strings) that does not literally reuse the CLI's numeric exit codes — relevant to how the AXI plan should think about whether/how exit-code semantics need to be re-expressed (not just reused) on the MCP side.

### Codebase pattern for format-enum propagation (MCP parity)

> Source: codebase-discovery.json architecture_note (as supplied): "MCP parity is a deliberate decision, not an afterthought: report.FormatList() feeds the MCP atcr_report schema enum (internal/mcp/tools.go:234) and description (tools.go:216), and handleReport validates with report.ValidFormat (handlers.go:378) — a new FormatAXI constant auto-propagates to MCP clients. Sprint 25.0 (SARIF) made CLI/MCP parity an explicit AC after TD-003 caught enum drift; decide and document the axi parity stance the same way."

This is the direct structural analogue to `atcr_verify`'s dual-surface exposure: just as `atcr verify` and `atcr_verify` had to be designed together so their behavior (especially exit/gate semantics) didn't drift, a `FormatAXI` constant added to `report.FormatList()` would auto-propagate into the MCP `atcr_report` tool's enum and description — meaning the AXI plan inherits MCP exposure whether or not it is explicitly designed for, and must decide the parity stance deliberately rather than let it happen implicitly.

### Reconciliation requirement (not a fresh scheme)

> Source: codebase-discovery.json architecture_note (as supplied): "Two exit-code contracts already coexist and must be reconciled by this plan: atcr review/atcr reconcile --fail-on use 0=clean/1=gate-failure/2=usage-error/3=auth-error (cmd/atcr/main.go, docs/ci-integration.md); the epic's proposed contract (0=success, 1=actionable findings, 2=internal/syntax error) is very close but swaps the meaning of 2. This needs explicit reconciliation, not a fresh scheme, since scripts already depend on the current mapping."

`atcr verify`'s exit-code table (0/1/2 = success/gate-failure/usage-error) matches the existing `review`/`reconcile` contract exactly. The epic's originally proposed AXI contract (0=success, 1=actionable findings, 2=internal/syntax error) diverges specifically at code `2`: existing contract and `atcr verify` both reserve `2` for usage/configuration errors, while the epic proposal would repurpose `2` for internal/syntax errors. This is a direct conflict the AXI plan must resolve explicitly (e.g., by adopting the existing `2`=usage-error meaning and finding a different code or mechanism for internal/syntax errors) rather than silently redefining `2`.

## Code Examples

The following are verbatim from the source document.

### CLI exit codes (`atcr verify`)

> Source: adversarial-verification-interface.md:40-45

```
- `0` — verification completed (gate passed or no gate requested)
- `1` — verification completed but `--fail-on` / `--require-verified` gate failed
- `2` — usage or configuration error (missing reconciled input, bad severity, etc.)
```

### Human-readable stdout output

> Source: adversarial-verification-interface.md:48-60

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: pass
```

When `--fail-on` is provided and the gate fails:

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: fail — M finding(s) at/above <severity> survive
```

### MCP tool input schema (`atcr_verify`)

> Source: adversarial-verification-interface.md:87-96

```json
{
  "id_or_path": "string (optional, review id only; paths are not accepted; defaults to .atcr/latest)",
  "fresh": "boolean (optional, default false)",
  "thorough": "boolean (optional, default false)",
  "min_severity": "string (optional, default 'MEDIUM')",
  "fail_on": "string (optional, CRITICAL|HIGH|MEDIUM|LOW)",
  "require_verified": "boolean (optional, default false)"
}
```

### MCP tool output schema (`atcr_verify`)

> Source: adversarial-verification-interface.md:100-115

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

### Gate-semantics table

> Source: adversarial-verification-interface.md:180-186

| Finding | v1 confidence | verdict | `--fail-on high` | `--fail-on high --require-verified` |
|---------|---------------|---------|------------------|-------------------------------------|
| F1 | HIGH | confirmed (VERIFIED) | counts | counts |
| F2 | HIGH | refuted (LOW) | does not count | does not count |
| F3 | HIGH | unverifiable | counts | does not count |
| F4 | MEDIUM | confirmed (VERIFIED) | does not count | does not count |

## Quick Reference

| Exit code | `atcr verify` meaning (source doc) | Existing `review`/`reconcile --fail-on` meaning (codebase-discovery.json) | Epic's originally proposed AXI meaning (codebase-discovery.json) | Conflict? |
|---|---|---|---|---|
| `0` | verification completed (gate passed or no gate requested) | clean | success | No — all agree |
| `1` | gate failed (`--fail-on`/`--require-verified`) | gate-failure | actionable findings | No — all agree (gate-failure ≈ actionable findings) |
| `2` | usage or configuration error | usage-error | internal/syntax error | **Yes — `2` means usage-error today; epic proposal repurposes it for internal/syntax error** |
| `3` | (not used by `verify`) | auth-error (defined in `cmd/atcr/main.go:126-129`; not included in the `docs/ci-integration.md` exit-semantics table) | (not addressed by epic proposal) | N/A — precedent has no code 3, existing contract does |

| Surface | Capability name | Schema shape | Notes |
|---|---|---|---|
| CLI | `atcr verify [id-or-path]` | Flags: `--fresh`, `--thorough`, `--min-severity`, `--fail-on`, `--require-verified`; stdout text summary + numeric exit code | Source: adversarial-verification-interface.md:17-60 |
| MCP | `atcr_verify` tool | Input: snake_case JSON fields (`id_or_path`, `fresh`, `thorough`, `min_severity`, `fail_on`, `require_verified`); Output: structured JSON (`review_id`, `verdict_counts`, `findings_processed`, `duration_ms`, `gate_status`) | Source: adversarial-verification-interface.md:83-124; `gate_status` omitted when `fail_on` unset |
| Precedent analogy for AXI | `report.FormatList()` → MCP `atcr_report` enum | A new `FormatAXI` constant auto-propagates into `internal/mcp/tools.go:234` enum and `tools.go:216` description, validated via `report.ValidFormat` in `handlers.go:378` | codebase-discovery.json architecture_note; same "designed together or drifts" risk as CLI/MCP `verify` parity, previously caught as TD-003 enum drift in Sprint 25.0 (SARIF) |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- Source document: [.planning/specifications/design-concepts/adversarial-verification-interface.md](../../../../specifications/design-concepts/adversarial-verification-interface.md)
