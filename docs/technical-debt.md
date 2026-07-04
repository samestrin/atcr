# Technical-Debt Tooling (`atcr debt`)

`atcr debt` queries, captures, and reports on the project's technical debt. It
reads the sharded technical-debt store under
`.planning/technical-debt/items/` (the structured, per-source YAML format
introduced in Epic 12.1) via the same loader the migration tooling uses, so the
CLI and the on-disk format never drift apart.

The authoritative source remains the Markdown table at
`.planning/technical-debt/README.md`. The YAML shards are derived from it by
`td-migrate migrate`; `atcr debt` reads those shards. When you suspect the
shards lag the table, pass `--sync` (or run `go run ./cmd/td-migrate migrate`)
to regenerate them from the authoritative README before reading.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr debt list` | Print debt items as a table, with filtering and sorting |
| `atcr debt add` | File a new item into the README (write-master) and regenerate shards |
| `atcr debt dashboard` | Generate an aggregated read-only Markdown dashboard |

### `atcr debt list`

Reads the shards and prints an aligned table. Filters compose (all must match);
sorting is deterministic.

```bash
atcr debt list                                  # everything, sorted by severity
atcr debt list --severity HIGH                  # only HIGH items
atcr debt list --status open --component cmd/atcr
atcr debt list --category security --sort age   # oldest security debt first
atcr debt list --sync                           # resync shards from the README first
```

Flags: `--severity`, `--status` (`open|deferred|resolved`), `--category`
(substring), `--component` (path prefix, e.g. `internal/autofix`), `--group`,
`--sort` (`severity|age|est|file`), `--items`, `--readme`, `--sync`.

### `atcr debt add`

Files a new item into the authoritative README table and then regenerates the
shard store so the item is immediately queryable. It never writes a shard
directly — a shard-only write would be destroyed by the next migrate.

Flag-driven (the scriptable, primary contract):

```bash
atcr debt add \
  --severity HIGH --file internal/x/y.go:12 \
  --problem "unbounded retry loop on 5xx" \
  --fix "cap retries and add jittered backoff" \
  --category correctness --est 30 \
  --label manual --source manual
```

Required in flag mode: `--severity`, `--file`, `--problem`, `--fix`,
`--category`. Optional: `--group` (default `U`), `--status` (default `open`),
`--est`, `--source` (default `manual`), `--date` (default today, UTC),
`--label` (default `manual`), `--source-type` (`Sprint|Review`, default
`Sprint`).

Omit the required flags on an interactive terminal to be walked through a
prompt instead. In a non-interactive context (CI, a pipe) with required flags
missing, the command exits with a usage error rather than blocking on input.

### `atcr debt dashboard`

Renders an aggregated rollup — totals, by-severity, by-component, by-age (by
month), and a top-priority list — to a read-only Markdown file
(`.planning/technical-debt/DASHBOARD.md` by default), distinct from the
authoritative README (it never rewrites the README).

```bash
atcr debt dashboard                    # write .planning/technical-debt/DASHBOARD.md
atcr debt dashboard --stdout           # print instead of writing
atcr debt dashboard --top 20           # list the 20 highest-priority items
atcr debt dashboard --check            # exit non-zero if the on-disk file is stale
```

The render is **deterministic** — no generation timestamp, age grouped by
calendar month — so `--check` flags real content drift, not clock movement.
Secret-shaped tokens (bearer / `sk-` API keys) accidentally pasted into finding
text are scrubbed from the output; file paths are preserved (they are required
core data and already public in the tree).

## CI/CD integration

`atcr debt dashboard --check` is the hook: it regenerates the dashboard
in-memory and compares it to the committed file, exiting non-zero when they
differ. Wire it wherever you want to guarantee the dashboard stays current.

> These are opt-in examples. The tracked `.githooks/` are deliberately scoped to
> CI-mirroring gates and are **not** modified by this tooling — copy a snippet
> below into your own hook or workflow if you want the check.

### Pre-commit hook

Add to your local `.git/hooks/pre-commit` (or a custom hooks path), after
building the binary:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Fail the commit if the technical-debt dashboard is out of date.
if ! go run ./cmd/atcr debt dashboard --check; then
  echo "The technical-debt dashboard is stale."
  echo "Regenerate and stage it:  go run ./cmd/atcr debt dashboard && git add .planning/technical-debt/DASHBOARD.md"
  exit 1
fi
```

### GitHub Actions

```yaml
name: technical-debt-dashboard
on: [pull_request]

jobs:
  dashboard-drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      # Fails the job if the committed dashboard no longer matches the source data.
      - run: go run ./cmd/atcr debt dashboard --check
```

To auto-regenerate instead of gating, drop the `--check` and commit the result
from a scheduled workflow (the pattern the repo already uses for other
generated files), rather than editing the shared pre-commit hook.
