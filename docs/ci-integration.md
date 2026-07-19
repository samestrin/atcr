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
- **A `--sync-cloud` authentication failure exits 3** (missing/empty `ATCR_API_KEY` or a remote 401/403), distinct from the usage/config code (2) so CI can detect an auth failure specifically.

### `--axi` mode reuses this exact contract

The `--axi` (Agent eXperience Interface) output mode changes only the *shape* of stdout (a token-dense TOON payload), never the exit code. `atcr review --axi`, `atcr review --resume <id> --axi`, and `atcr report --format axi` return **0/1/2/3 identically** to their non-`--axi` counterparts for the same inputs — cross-validated by `atcr verify`'s independently-derived 0/1/2 mapping. New AXI-introduced errors classify into the existing contract: an unsupported `--axi` flag combination (e.g. `--axi --auto-fix`) is a usage error (**2**), and an internal AXI rendering fault is a generic failure (**1**).

- **The epic's original `2` = "internal/syntax error" proposal was considered and rejected.** Exit `2` is already reserved for usage/configuration errors CI scripts depend on; repurposing it would break them. AXI introduces no new exit code and repurposes none.
- **`--axi` structured errors go to stderr, not stdout.** Per axi.md Principle 6 an agent-facing CLI *may* emit structured errors on stdout, but atcr keeps them on stderr (its existing convention) so `--axi` stdout is *always* payload-only: an agent running `atcr review --axi > findings.toon` gets a byte-clean payload file and branches on the exit code — it never parses stdout for an error case. (Any future errors-on-stdout payload would remain subject to the `--axi` escape-free stdout guarantee.)

For the full agent-facing picture — `--axi` invocation on `atcr review`/`atcr report`, the TOON payload shapes, pagination/truncation, and a worked autonomous-sweeper example — see [agentic-consumption.md](agentic-consumption.md).

## Maintained PR Action

For a first-class pull-request surface — a PR **check** with a findings table
and opt-in **inline comments** rendering the `FIX` column — use the maintained
composite Action (`action.yml` at the repo root) instead of hand-wiring the
steps above:

```yaml
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
- uses: samestrin/atcr@v1
  with:
    openrouter-api-key: ${{ secrets.OPENROUTER_API_KEY }}
    fail-on: high
    inline-comments: true
```

See [github-action.md](github-action.md) for inputs, required permissions, and a
manual smoke-test procedure.

## SARIF Upload for Code Scanning

This path is **separate from** the [Maintained PR Action](#maintained-pr-action)
above: `atcr github` posts a PR check and inline "Files Changed" comments directly
via the GitHub API (see [github-action.md](github-action.md)), while `atcr report
--format=sarif` produces a SARIF 2.1.0 file for the *centralized* security
surfaces the PR flow does not reach — GitHub Advanced Security's Code Scanning
"Security" tab and GitLab CI's native SAST report widget. Both can run side by
side; one feeds PR checks/comments, the other feeds the Security tab.

**GitHub Advanced Security — Code Scanning "Security" tab:**

```yaml
jobs:
  atcr-sarif:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      security-events: write   # required by upload-sarif
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }   # full history so atcr can resolve the diff range

      - name: Run atcr and emit SARIF
        run: atcr review && atcr reconcile && atcr report --format=sarif > results.sarif

      - name: Upload SARIF to GitHub Code Scanning
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: results.sarif
```

The `permissions.security-events: write` grant is what lets `upload-sarif` write
to the Security tab; without it the upload step fails.

**GitLab CI — native SAST report widget:**

GitLab has no upload action; it ingests the SARIF file as a native SAST report
artifact instead. Wire `results.sarif` through `artifacts:reports:sast`:

```yaml
atcr-sast:
  script:
    - atcr review && atcr reconcile && atcr report --format=sarif > results.sarif
  artifacts:
    reports:
      sast: results.sarif
```

This is GitLab's own artifact-based mechanism — there is no `upload-sarif`
equivalent step, and the `artifacts:reports:sast` key is what surfaces the results
in GitLab's SAST report widget.
