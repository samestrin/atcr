# Adversarial Verification Interface

## Scope

This design concept defines the human and programmatic interfaces for Epic 3.0 adversarial verification:

- the `atcr verify` CLI subcommand
- the `atcr_verify` MCP tool
- the `atcr review --verify` chaining behavior
- the verified report rendering conventions
- the CI gate semantics exposed through both CLI and MCP

It is grounded in the existing CLI/MCP patterns in `cmd/atcr/` and `internal/mcp/`.

---

## CLI Command: `atcr verify`

### Form

```
atcr verify [id-or-path]
```

- `id-or-path` is optional and follows the same resolution rules as `atcr reconcile`:
  - bare review id → `.atcr/reviews/<id>/`
  - path → used as-is
  - omitted → `.atcr/latest`

### Flags

| Flag | Type | Default | Meaning |
|------|------|---------|---------|
| `--fresh` | bool | false | Re-verify every finding, even those already verified in a previous run. |
| `--thorough` | bool | false | Use 3 skeptics with majority rule instead of the configured `verify.votes` default. |
| `--min-severity` | string | `MEDIUM` | Findings below this severity skip verification and keep v1 confidence. |
| `--fail-on` | string | (unset) | Exit 1 if any finding at/above this severity survives (verdict ≠ `refuted`). Resolved via the shared gate precedence (flag > project config > registry); unset is a no-op. |
| `--require-verified` | bool | false | Only meaningful with `--fail-on`: count only findings whose confidence is `VERIFIED`. |

### Exit codes

- `0` — verification completed (gate passed or no gate requested)
- `1` — verification completed but `--fail-on` / `--require-verified` gate failed
- `2` — usage or configuration error (missing reconciled input, bad severity, etc.)

### Output

Human-readable summary to stdout:

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: pass
```

When `--fail-on` is provided and the gate fails:

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: fail — M finding(s) at/above <severity> survive
```

---

## CLI Chaining: `atcr review --verify`

### Form

```
atcr review --verify [--fail-on <severity>] [--require-verified] ...
```

### Behavior

1. Run the review fan-out.
2. Run reconcile in-process (verification requires reconciled input).
3. Run verification.
4. Evaluate `--fail-on` / `--require-verified` against the final verified findings.

`--verify` is compatible with the existing `--fail-on` flag and shares the same exit-code semantics. It does not introduce a separate `--reconcile` flag; reconcile is always run when `--verify` is set.

---

## MCP Tool: `atcr_verify`

### Input schema

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

### Output schema

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

### Error behavior

- Missing reconciled findings → clear error message: `no reconciled findings found: run 'atcr reconcile' first`
- Invalid severity → usage/configuration error
- Individual skeptic failures → captured as `unverifiable` verdicts; the tool call itself succeeds

---

## Report Rendering

### Confidence v2 ordering

Confidence tiers render in this order, highest to lowest:

1. `VERIFIED`
2. `HIGH`
3. `MEDIUM`
4. `LOW`

`VERIFIED` receives a distinct badge/label, e.g. `[VERIFIED]` or `[VERIFIED ✓]`.

### Skeptic section

For each finding with a non-nil `verification` block, the report shows:

- skeptic agent name (`Verification.Skeptic`)
- verdict (`Verification.Verdict` — `confirmed`, `refuted`, `unverifiable`)
- reasoning (`Verification.Notes`)
- model — sourced from `reconciled/verification.json` (the per-finding `verification` block in `findings.json` carries only verdict/skeptic/notes, not the model)

### Refuted section

All findings where `verification.verdict == "refuted"` are rendered in a collapsed section at the bottom of the report:

```html
<details>
<summary>Refuted Findings (N)</summary>
...
</details>
```

If no findings are refuted, the section is omitted.

### Backward compatibility

Findings without a `verification` block render exactly as they did pre-Epic 3.0.

---

## Gate Semantics

### `--fail-on <severity>`

Counts findings at or above the severity threshold whose verdict is **not** `refuted`.

### `--require-verified`

When combined with `--fail-on`, counts only findings whose confidence is `VERIFIED` at or above the threshold.

### Examples

| Finding | v1 confidence | verdict | `--fail-on high` | `--fail-on high --require-verified` |
|---------|---------------|---------|------------------|-------------------------------------|
| F1 | HIGH | confirmed (VERIFIED) | counts | counts |
| F2 | HIGH | refuted (LOW) | does not count | does not count |
| F3 | HIGH | unverifiable | counts | does not count |
| F4 | MEDIUM | confirmed (VERIFIED) | does not count | does not count |

---

## Related Documents

- Plan: [3.0 Adversarial Verification](../../plans/active/3.0_adversarial_verification/plan.md)
- Planning docs:
  - [Verification Pipeline](../../plans/active/3.0_adversarial_verification/documentation/verification-pipeline.md)
  - [CLI & MCP Integration](../../plans/active/3.0_adversarial_verification/documentation/cli-mcp-integration.md)
  - [LLM Tool Loop](../../plans/active/3.0_adversarial_verification/documentation/llm-tool-loop.md)
  - [Testing & Fixtures](../../plans/active/3.0_adversarial_verification/documentation/testing-fixtures.md)
