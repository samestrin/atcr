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
| Run failed (internal error, e.g. render or harness fault, or every agent failed) | 1 |
| Usage or configuration error (empty range, invalid `--fail-on`, missing API key env var, config invalid) | 2 |
| `--sync-cloud` authentication failure (missing or empty key, or a remote 401/403) | 3 |
| `atcr debt dashboard --check` or `atcr debt report --check`: the generated file is out of date or missing | 4 (regenerate and re-stage) |

Exit 1 covers both a gate failure and a run that failed outright (every agent failed, or an internal render or harness fault). The failure (1) and the usage/configuration error (2) are distinct codes, so CI can tell "the panel found a real problem" apart from "atcr was invoked wrong."

Notes:

- **Partial success is not failure.** If some agents fail but at least one succeeds, the run completes with `partial: true` recorded in the summary — the gate judges the surviving findings.
- **Empty range is a hard error**, never a silent zero-findings pass.
- With adversarial verification (`--verify`), `--fail-on` counts only non-refuted findings, and `--require-verified` restricts the gate to skeptic-confirmed findings.
- **A `--sync-cloud` authentication failure exits 3** (missing/empty `ATCR_API_KEY` or a remote 401/403), distinct from the usage/config code (2) so CI can detect an auth failure specifically. Note the ordering: `--cloud-endpoint` is **required** (this build compiles in no default destination), and it is validated *before* the credential — so a `--sync-cloud` run that omits the endpoint exits **2**, not 3, even when `ATCR_API_KEY` is also absent.

### `atcr diff-smell` — gating is opt-in

`atcr diff-smell` (the deterministic diff-smell scanner, promoted from `atcr verify diff` — still a hidden deprecated alias for one minor version) reuses the codes above but **inverts the default**: it exits `0` for every verdict unless `--fail-on` is passed. That preserves drop-in parity with the upstream `diff-smell` tool, which never exits nonzero on content.

| Condition | Exit |
|-----------|------|
| Scan completed, no `--fail-on` given (any verdict, including `hard`) | 0 |
| Scan completed, verdict below the `--fail-on` level | 0 |
| Verdict at/above the `--fail-on` level | 1 (gate failure) |
| Usage error (see the full list below) | 2 |

Exit `2` covers: an invalid `--fail-on` value (an empty one is *unset*, not an error); an unreadable `--diff` file; a git failure; two named diff sources; input over the size cap ("too large to scan"); an explicitly empty `--rev` or `--diff` value; and a `--rev` value that starts with `-` — a deliberate argument-injection guard, since `--rev --output=/tmp/pwn` would otherwise make the scan write a file of the caller's choosing.

Its `--fail-on` takes **verdicts** (`hard`, `soft`, `none`), not the severities `atcr review` and `atcr reconcile` take — disjoint value sets, so a severity passed here is a usage error rather than a silent misread. An empty diff and non-diff input both report `clean` and exit `0` with a note on stderr, so a pre-commit hook on a nothing-staged commit is a no-op rather than a spurious block. See [diff-smell.md](diff-smell.md).

**Merge commits are scanned against their first parent.** `--rev` defaults to `HEAD`, and CI checkouts (`refs/pull/N/merge`) and post-merge branches routinely have a merge there. A plain `git show` prints no diff for a merge, so the gate would report `clean` and exit `0` exactly where it is meant to run; atcr passes `--first-parent -m` instead, which asks what the merge introduces to the branch it lands on. Single-parent commits are unaffected.

### AXI mode reuses this exact contract

The AXI (Agent eXperience Interface) output mode changes only the *shape* of stdout (a token-dense TOON payload), never the exit code. `atcr review`, `atcr review --resume <id>`, and `atcr report` return the same **0/1/2/3** codes on the agent path — stdout not a terminal, `ATCR_FORMAT=axi`, or an agent marker — as in human mode for the same inputs, and an explicit `--axi` or `--format axi` behaves the same. This is cross-validated by `atcr verify`'s independently-derived 0/1/2 mapping. New AXI-introduced errors classify into the existing contract: an unsupported `--axi` flag combination (e.g. `--axi --auto-fix`) is a usage error (**2**), and an internal AXI rendering fault is a generic failure (**1**). For the `--auto-fix` flow itself — its sandboxed-by-default validation and the `--no-sandbox` opt-out's host-execution risk — see [Auto-fix sandboxed validation](auto-fix.md).

- **The epic's original `2` = "internal/syntax error" proposal was considered and rejected.** Exit `2` is already reserved for usage/configuration errors CI scripts depend on; repurposing it would break them. AXI introduces no new exit code and repurposes none.
- **In AXI mode a failure is one `error: true` document on stdout, and the `atcr: <message>` line is not written to stderr** (stderr may still carry diagnostics and logs emitted before the failure). The document carries the redacted `message`, `code` (the same numeric exit code: 2 usage/configuration, 1 failure, 3 `--sync-cloud` authentication) and `help[]`, which names the command's valid flags for an unknown flag, the valid commands for an unknown command, and otherwise the command's `--help`. A run that already wrote a payload keeps it and adds no second document: a `--fail-on` gate exit prints its payload and exits 1. Human mode keeps the `atcr: <message>` line on stderr. See [agentic-consumption.md](agentic-consumption.md#errors-one-error-true-document-on-stdout).

For the full agent-facing picture — where AXI is the default and how to force it or `--human`, what each command prints, the TOON payload shapes, the error document, pagination/truncation, and a worked autonomous-sweeper example — see [agentic-consumption.md](agentic-consumption.md).

## Maintained PR Action

For a first-class pull-request surface — a PR **check** with a findings table
and opt-in **inline comments** rendering the `FIX` column — use the maintained
composite Action (`action.yml` at the repo root) instead of hand-wiring the
steps above:

```yaml
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }
# a fresh runner has no user registry: copy a committed one into place
- run: install -D -m 644 .github/atcr/registry.yaml "$HOME/.config/atcr/registry.yaml"
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

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go install github.com/samestrin/atcr/cmd/atcr@latest
      # A fresh runner has no user registry, and a committed .atcr/registry.yaml
      # only overlays one. Commit your registry (it names key env vars, never keys)
      # and copy it into place, or set ATCR_REGISTRY_URL to a shared one.
      - run: install -D -m 644 .github/atcr/registry.yaml "$HOME/.config/atcr/registry.yaml"

      - name: Run atcr and emit SARIF
        env:
          # one entry per api_key_env your registry's providers name
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
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
  image: golang:1.25
  # Set each api_key_env your registry's providers name (for example
  # OPENROUTER_API_KEY) as a masked CI/CD variable; GitLab exports it to the job.
  script:
    - go install github.com/samestrin/atcr/cmd/atcr@latest
    # A fresh runner has no user registry: copy your committed one into place,
    # or set ATCR_REGISTRY_URL to a shared one.
    - install -D -m 644 .gitlab/atcr/registry.yaml "$HOME/.config/atcr/registry.yaml"
    - atcr review && atcr reconcile && atcr report --format=sarif > results.sarif
  artifacts:
    reports:
      sast: results.sarif
```

This is GitLab's own artifact-based mechanism — there is no `upload-sarif`
equivalent step, and the `artifacts:reports:sast` key is what surfaces the results
in GitLab's SAST report widget.
