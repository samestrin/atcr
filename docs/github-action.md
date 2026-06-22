# GitHub Action — PR Review

The `samestrin/atcr` repository ships a composite GitHub Action (`action.yml` at
the repo root) that runs the full atcr pipeline on a pull request and surfaces
the result as a **PR check** and, optionally, **inline comments**. It is the
maintained delivery surface for findings — no glue code required.

What it does, in one job:

1. Builds the `atcr` binary from the pinned action ref (`actions/setup-go` + `go build`).
2. Runs `atcr review --base origin/<base>` against your checkout.
3. Runs `atcr reconcile` to merge the per-source streams.
4. Runs `atcr github` to post a check run honoring `--fail-on` and (opt-in) inline comments.

The merge gate rides the job's exit code: when findings at or above `fail-on`
survive, the `atcr github` step exits non-zero and the check is marked failed.

## Usage

Add a workflow to the **consumer** repository (the repo whose PRs you want
reviewed):

```yaml
# .github/workflows/atcr-pr.yml
name: ATCR PR Review

on:
  pull_request:

permissions:
  contents: read        # read the diff
  checks: write         # post the check run
  pull-requests: write  # post inline comments (only needed with inline-comments: true)

jobs:
  atcr:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0   # REQUIRED: atcr needs full history to resolve the merge-base

      - uses: samestrin/atcr@v1
        with:
          openrouter-api-key: ${{ secrets.OPENROUTER_API_KEY }}
          fail-on: high            # merge gate threshold; empty for an informational check
          inline-comments: true    # opt-in; default false (check + artifacts only)
```

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `openrouter-api-key` | yes | — | OpenRouter API key used by `atcr review`. Store as a secret. |
| `fail-on` | no | `high` | Merge-gate severity (`CRITICAL`/`HIGH`/`MEDIUM`/`LOW`). Empty → informational, non-blocking check. |
| `inline-comments` | no | `false` | Post inline PR review comments in addition to the check run. |
| `base-ref` | no | `origin/<PR base>` | Git ref to diff against. |
| `check-name` | no | `atcr` | Name of the GitHub check run. |
| `github-token` | no | `${{ github.token }}` | Token to post the check/comments. Needs `checks:write` and, for inline comments, `pull-requests:write`. |
| `go-version` | no | `1.25` | Go toolchain used to build atcr. |

### Required permissions and checkout

- **`fetch-depth: 0`** on `actions/checkout` — a shallow checkout breaks
  merge-base resolution and atcr will refuse to guess a range.
- **`checks: write`** — to post the check run.
- **`pull-requests: write`** — only when `inline-comments: true`.

### atcr configuration

`atcr review` needs an agent/provider registry (see [registry.md](registry.md)).
The consumer repository must carry atcr configuration (a committed
`registry.yaml` / project config) or rely on `atcr init` defaults. The Action
builds and invokes the binary; it does not provision a registry for you.

## How findings render

- **PR check** — a single check run named `atcr` (configurable) with a summary
  table of every finding (Severity / Location / Problem / Confidence) and a
  pass/fail conclusion honoring `fail-on`. A `neutral` conclusion is used when
  `fail-on` is empty (informational only).
- **Inline comments** (opt-in) — one comment per finding that anchors to a
  changed line, formatted as:

  > ATCR found: \<problem\>. Fix: \<fix\>. Suggested by: \<executor\>.

  The `Fix:` clause appears when the `FIX` column is populated (Epic 7.0); the
  `Suggested by:` clause appears when the finding's `EVIDENCE` carries the
  executor's `fix by <name>` attribution token. Findings on unchanged lines
  cannot be anchored in the diff and are surfaced in the check summary instead.

## Manual smoke test

The automated end-to-end coverage runs against a fake GitHub API
(`net/http/httptest`) — see `cmd/atcr/github_integration_test.go` and the
`internal/ghaction` tests. To validate against the **real** GitHub API once,
end-to-end, on a throwaway PR:

1. Push a branch with a deliberate finding (e.g. a hardcoded secret or an
   unverified token parse) and open a PR.
2. In the PR's repo, ensure the workflow above is present and the
   `OPENROUTER_API_KEY` secret is set.
3. Trigger the workflow (the PR `synchronize`/`opened` event runs it).
4. Confirm on the PR:
   - an `atcr` check run appears with the findings table;
   - the check fails when a finding at/above `fail-on` is present;
   - with `inline-comments: true`, a comment appears on the offending line in
     the AC3 format above.
5. Push a fix that clears the finding; confirm the check flips to passing.

This is a one-time manual verification; CI itself never posts to a live PR
(the project's own `ci.yml` runs with `contents: read` only).
