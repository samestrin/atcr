# CI Integration

atcr is a PR gate with no glue code: exit codes carry the verdict.

```bash
atcr review && atcr reconcile --fail-on high
```

One-shot equivalent — `atcr review --fail-on high` resolves the range, fans out, reconciles, and gates the exit code in a single command. A ready-to-adapt wrapper lives at [../examples/ci-gate.sh](../examples/ci-gate.sh).

## Exit semantics

| Condition | Exit |
|-----------|------|
| Run completed, no findings at/above `--fail-on` threshold | 0 |
| Run completed, findings at/above threshold survive reconciliation | 1 (gate failure) |
| Usage or configuration error (empty range, invalid `--fail-on`, missing API key env var, config invalid) | 2 |

The gate failure (1) and the usage/configuration error (2) are distinct codes, so CI can tell "the panel found a real problem" apart from "atcr was invoked wrong."

Notes:

- **Partial success is not failure.** If some agents fail but at least one succeeds, the run completes with `partial: true` recorded in the summary — the gate judges the surviving findings.
- **Empty range is a hard error**, never a silent zero-findings pass.
- With roadmap stage 3 (adversarial verification), `--fail-on` counts only non-refuted findings, and `--require-verified` restricts the gate to skeptic-confirmed findings.

## GitHub Actions sketch

```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0          # full history: atcr needs merge-base
- name: atcr gate
  env:
    OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
  run: |
    atcr review --base origin/${{ github.base_ref }}
    atcr reconcile --fail-on high
```

Shallow checkouts (`fetch-depth: 1`) break merge-base resolution; atcr detects this and says so rather than producing a wrong range.
