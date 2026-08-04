# Technical-Debt Tooling (`atcr debt`)

`atcr debt` queries, captures, and reports on the project's technical debt. All
of its subcommands read and write **one** store: the append-only, month-sharded
JSONL backlog under `.atcr/debt/` that `atcr reconcile` populates. An item filed
by `atcr debt add` is therefore visible to `atcr debt list` and closeable by
`atcr debt resolve`.

Every subcommand resolves the store through `--dir`, which defaults to
`.atcr/debt` relative to the current working directory. Point it elsewhere to
work against another checkout or a fixture tree.

`.atcr/` is local, uncommitted state by default; whether the store is tracked is
your per-repo `.gitignore` decision.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr debt list` | Print debt items as a table, with filtering and sorting |
| `atcr debt add` | File a new item into the store |
| `atcr debt dashboard` | Render an aggregated Markdown dashboard |
| `atcr debt resolve` | List open items and record resolutions |
| `atcr debt compact` | Fold the append-only store to its live records |

### `atcr debt list`

Reads the store, folds it to one effective record per finding id, and prints an
aligned table led by that id. Filters compose (all must match); sorting is
deterministic.

```bash
atcr debt list                                  # everything, sorted by severity
atcr debt list --severity HIGH                  # only HIGH items
atcr debt list --status open --component cmd/atcr
atcr debt list --category security --sort age   # oldest security debt first
```

Flags: `--dir`, `--severity`, `--status` (`open|deferred|resolved|wontfix`),
`--category` (substring), `--component` (path prefix, e.g. `internal/autofix`),
`--sort` (`severity|age|est|file`).

`--status` has no default, so a bare `list` shows every item including closed
ones, each with its effective status. `--status open` is the filter that hides
them; it also matches the empty status that open records carry on disk.

The `ID` column is the finding id, rendered untruncated so it can be pasted
straight into `atcr debt resolve <id>`.

### `atcr debt add`

Files a new item into the store and echoes its id.

Flag-driven (the scriptable, primary contract):

```bash
atcr debt add \
  --severity HIGH --file internal/x/y.go:12 \
  --problem "unbounded retry loop on 5xx" \
  --fix "cap retries and add jittered backoff" \
  --category correctness --est 30
```

Required in flag mode: `--severity`, `--file`, `--problem`, `--fix`,
`--category`. Optional: `--dir`, `--status` (default `open`), `--est`.

`--file` takes a `path:line` location; a purely numeric suffix is parsed into
the record's line number, and anything else is kept verbatim as the path.

Omit the required flags on an interactive terminal to be walked through a
prompt instead. In a non-interactive context (CI, a pipe) with required flags
missing, the command exits with a usage error rather than blocking on input.

### `atcr debt dashboard`

Renders an aggregated rollup — totals, by-severity, by-component, by-age (by
month), and a top-priority list — as Markdown. It writes to **stdout** by
default; pass `--output <file>` to write a file instead, mirroring `atcr report`.

```bash
atcr debt dashboard                            # print the dashboard
atcr debt dashboard --output DASHBOARD.md      # write it to a file
atcr debt dashboard --top 20                   # list the 20 highest-priority items
atcr debt dashboard --output DASHBOARD.md --check   # exit non-zero if that file is stale
```

`--check` compares against the file named by `--output`, so the two are used
together; `--check` without `--output` is a usage error.

### `atcr debt resolve`

Lists the open backlog for a fix cycle and records resolutions as append-only
status records.

```bash
atcr debt resolve --list                       # open items, most severe first
atcr debt resolve --json --max 5               # the same, as JSON, capped
atcr debt resolve --resolve <id>               # mark it fixed
atcr debt resolve --resolve <id> --status wontfix --reason "accepted pattern"
```

Flags: `--dir`, `--list`, `--json`, `--severity`, `--max`, `--resolve <id>`,
`--status` (`resolved|wontfix`), `--reason`. `--status wontfix` requires a
`--reason` — it is a permanent dismissal, so the rationale is recorded with it.

### `atcr debt compact`

Folds the append-only store to its live records, dropping superseded
occurrences. The resolution trail is kept: when an item was closed and has since
regressed, compaction retains both the current record and the resolution that
closed it, so the `--reason` text is never destroyed.

```bash
atcr debt compact          # reports records before/after and how many were dropped
```

## Status lifetimes

A finding's id is a hash of its **file, line, and problem text**. A genuine fix
shifts surrounding lines, so the id changes and the finding returns as a fresh
item regardless. The only thing a terminal record can suppress is a re-detection
at the *same* file, line, and text — which after a real fix means a regression.
Resolution lifetime follows from that:

| Status | Lifetime | Why |
|--------|----------|-----|
| `wontfix` | Permanent | Suppression is the point. A false positive is stable at a stable location, so its id is stable. Requires a `--reason`. |
| `resolved` | Re-opens on re-detection | The same id after a fix means the fix regressed or never landed — the thing most worth surfacing. |
| `deferred` | Re-surfaces on re-detection | "Not now" is not "never". A deferred item stays in the backlog and stays closeable. |

So `atcr debt list` can show an item as `resolved` today and as open again after
a later `atcr reconcile` re-detects it. Only `wontfix` is final.

The render is **deterministic** — no generation timestamp, age grouped by
calendar month — so `--check` flags real content drift, not clock movement.
Secret-shaped tokens (bearer / `sk-` API keys) accidentally pasted into finding
text are scrubbed from the output; file paths are preserved (they are required
core data and already public in the tree).

## CI/CD integration

`atcr debt dashboard --output <file> --check` is the hook: it regenerates the
dashboard in-memory and compares it to the committed file, exiting non-zero when
they differ. Wire it wherever you want to guarantee the dashboard stays current.

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
if ! go run ./cmd/atcr debt dashboard --output DASHBOARD.md --check; then
  echo "The technical-debt dashboard is stale."
  echo "Regenerate and stage it:  go run ./cmd/atcr debt dashboard --output DASHBOARD.md && git add DASHBOARD.md"
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
      - run: go run ./cmd/atcr debt dashboard --output DASHBOARD.md --check
```

To auto-regenerate instead of gating, drop the `--check` and commit the result
from a scheduled workflow (the pattern the repo already uses for other
generated files), rather than editing the shared pre-commit hook.
