# Technical-Debt Tooling (`atcr debt`)

`atcr debt` queries, captures, and reports on the project's technical debt. All
five of its subcommands — `list`, `add`, `dashboard`, `resolve`, and `compact` —
read and write **one** store: the append-only, month-sharded JSONL backlog under
`.atcr/debt/` that `atcr reconcile` populates. An item filed by `atcr debt add`
is therefore visible to `atcr debt list` and closeable by `atcr debt resolve`.

The Markdown views this tooling renders are **projections** of that log, not the
authoritative copy. The log is the system of record; a table or dashboard is one
rendering of it at a moment in time.

## The store

| Property | Value |
|----------|-------|
| Location | `.atcr/debt/`, resolved from the repository root |
| Layout | One shard per calendar month: `YYYY-MM.jsonl` |
| Shard key | The `YYYY-MM` prefix of the record's `run_id` |
| Mutation model | Append-only; one atomic `O_APPEND` write per record, under a mkdir-based cross-process advisory lock |

Every subcommand resolves the store through `--store`. With no `--store`, atcr walks
up from the working directory to the nearest `.git`/`.atcr` marker and uses that
repository's `.atcr/debt` — so `atcr debt list` reads the same store from a
subdirectory as it does from the root. Pass `--store` to work against another
checkout or a fixture tree. `--dir` remains a deprecated hidden alias.

A resolution never edits a line in place: it appends a new record carrying the
same id and a terminal status. Reads fold the log to one **effective** record per
id, so history is preserved without the views showing an item twice.

`.atcr/` is local, uncommitted state by default, and whether the store is tracked
is your per-repo `.gitignore` decision — the single correct lever. Ignore
`.atcr/` in a public repo and the backlog stays private; track it in a private
repo and the history is preserved with the code.

## Ownership model and the seam

**The producer owns the system of record and must be complete standalone.
Consumers get a stable read contract, never write access.**

atcr is the reviewer, the findings are its output, and it owns the log. It
renders its own human-readable views (`atcr debt list`, `atcr debt dashboard`)
rather than depending on any other tool to be readable.

The store holds **review facts**: the finding, its severity, `file:line`,
confidence, which reviewers raised it, which model, which run, and its
resolution. A downstream tool's view holds **workflow state**: sprint, epic,
group, quick-win versus backlog, checkbox state. That is the seam. Fields
meaningful only inside someone else's process do not enter this schema — `group`
and `source_label` are deliberately absent, because atcr does not know what a
sprint is.

A consumer keys its own overlay by `Record.ID` and reads through
`atcr debt list --json` rather than co-mingling fields into the store.

## Finding ids

```
FindingID = hex(SHA-256(file \x00 line \x00 problem)[:8])   # 16 hex characters
```

Severity is deliberately excluded, so re-settling a finding's severity keeps its
id stable.

**The line number is part of the identity.** A genuine fix shifts the
surrounding lines, so the finding returns under a *new* id regardless of what was
recorded — which means terminal resolution can only ever suppress a re-detection
at the same file, same line, and same problem text. After a real fix, that
combination means a regression or a fix that never landed.

The id appears in `atcr debt list`, in `atcr debt dashboard`'s Top Priority
table, and under `id` in `--json` output — the same string in all three, rendered
untruncated so it pastes verbatim into `atcr debt resolve <id>`. A
record with no id (only reachable by hand-editing the store) renders as `-`
rather than a fabricated value that `resolve` could not match.

## Record schema (v3)

| Field | Type | Meaning |
|-------|------|---------|
| `schema_version` | int | Record format version (currently `3`) |
| `id` | string | Stable finding id; the join key |
| `run_id` | string | Producing run; its `YYYY-MM` prefix selects the shard |
| `ts` | string | RFC3339 UTC timestamp of the append |
| `severity` | string | `CRITICAL` / `HIGH` / `MEDIUM` / `LOW` |
| `file` | string | Path the finding is anchored to |
| `line` | int | Line within that file (part of the id) |
| `problem` | string | What is wrong (part of the id) |
| `fix` | string | Proposed remediation |
| `category` | string | Free-text classification (e.g. `correctness`) |
| `est_minutes` | int | Estimated remediation effort |
| `evidence` | string | Supporting excerpt or rationale |
| `reviewers` | []string | Personas that raised the finding |
| `confidence` | string | Reviewer-panel confidence |
| `model` | string | Model that produced the finding |
| `justification` | string | Why it was resolved/dismissed (`--reason`) |
| `source_report` | object | Report the finding came from: `{"path": …, "line": …, "section": …}` (`line`/`section` omitted when unset) |
| `status` | string | Empty (open), `deferred`, `resolved`, or `wontfix` |
| `resolved_at` | string | Timestamp of the terminal record |
| `origin` | string | **v3** — `review` or `manual` |
| `occurrences` | int | **v3** — times this id has been seen; carried through compaction |
| `counted_through` | string | **v3** — newest sighting `occurrences` accounts for; what makes the count idempotent under repeated compaction |
| `first_seen` | string | **v3** — timestamp of the id's first append; carried through compaction |

Reads are tolerant in one direction only: a v1 or v2 record decodes cleanly, with
zero values for fields that did not exist yet, and a record with no `origin`
predates v3 and means `review`. A record from a *newer* schema (v4+) is skipped
with a warning and left untouched on disk rather than being folded or dropped by
a binary that cannot understand it.

## Status lifetimes

| Status | Lifetime | Rationale |
|--------|----------|-----------|
| `wontfix` | Terminal | Suppression is the feature; a false positive is stable at a stable location, so its id is stable and permanent dismissal works. Requires a `--reason`. |
| `resolved` | Re-openable on re-detection | The same id after a fix implies a regression — the thing most worth surfacing. |
| `deferred` | Re-surfaces on re-detection | "Not now" is not "never". A deferred item leaves the `debt resolve` worklist while it stands, but stays in `debt list` and the dashboard as live debt, and stays closeable by id. |

So `atcr debt list` can show an item as `resolved` today and as open again after
a later `atcr reconcile` re-detects it. Only `wontfix` is final.

`occurrences` is the regression count for an id: `occurrences - 1` re-detections
after the first. When divergent terminal records exist for one id, precedence is
`wontfix` > `resolved` > `deferred`.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr debt list` | Print debt items as a table or JSON, with filtering and sorting |
| `atcr debt add` | File a new item into the store |
| `atcr debt dashboard` | Render an aggregated Markdown dashboard |
| `atcr debt resolve` | List open items and record resolutions |
| `atcr debt compact` | Fold the append-only store to its live records |
| `atcr debt backfill-justifications` | Replay stored justifications from their source `review.md` (one-off repair) |

### `atcr debt list`

Reads the store, folds it to one effective record per finding id, and prints an
aligned table led by that id. Filters compose (all must match); sorting is
deterministic.

```bash
atcr debt list                                  # everything, sorted by severity
atcr debt list --severity HIGH                  # only HIGH items
atcr debt list --status open --component cli
atcr debt list --category correctness --sort age   # oldest correctness debt first
atcr debt list --json                           # the same selection, as JSON
```

Flags: `--store`, `--severity` (exact, case-insensitive), `--status` (exact:
`open|deferred|resolved|wontfix`),
`--category` (substring), `--component` (path prefix, e.g. `internal/autofix`),
`--origin` (exact: `review|manual`), `--sort` (`severity|age|est|file`), `--json`.

`--status` has no default, so a bare `list` shows every item including closed
ones, each with its effective status — a resolved item stays visible (as
`resolved`) until compaction folds it away. `--status open` is the filter that
asks for the live backlog; it also matches the empty status that open records
carry on disk.

The `ID` column is the finding id, rendered untruncated so it can be pasted
straight into `atcr debt resolve <id>`. The `ORIGIN` column shows
each record's effective origin (`review` or `manual`); `--origin` filters
against that same effective value, so a pre-v3 record with no stored `origin`
still matches `--origin review`.

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
`--category`. Optional: `--store`, `--status` (default `open`), `--est`.

`--file` takes a `path:line` location; a purely numeric suffix is parsed into
the record's line number, and anything else is kept verbatim as the path.

The record is written with `origin: manual` and a synthetic `run_id` carrying the
`YYYY-MM` prefix the shard layout requires, so a hand-filed item lands in the
same month shard a reviewed one would.

Omit the required flags on an interactive terminal to be walked through a
prompt instead. In a non-interactive context (CI, a pipe) with required flags
missing, the command exits with a usage error rather than blocking on input.

### `atcr debt dashboard`

Renders an aggregated rollup — totals, by-severity, by-component, by-age (by
month), and a top-priority list led by the finding id — as Markdown. It writes to
**stdout** by default; pass `--output <file>` to write a file instead, mirroring
`atcr report`. On the file path it prints nothing to stdout.

```bash
atcr debt dashboard                                   # print the dashboard
atcr debt dashboard --output docs/debt-report.md      # write it to a file
atcr debt dashboard --top 20                          # the 20 highest-priority items
atcr debt dashboard --output docs/debt-report.md --check   # exit non-zero if stale
```

Flags: `--store`, `--output`, `--top`, `--check`.

`--check` compares against the file named by `--output`, so the two are used
together; `--check` without `--output` is a usage error.

The render is **deterministic** — no generation timestamp, age grouped by
calendar month — so `--check` flags real content drift, not clock movement.
Secret-shaped tokens (bearer / `sk-` API keys) accidentally pasted into finding
text are scrubbed from the output; file paths and finding ids are preserved (both
are required core data, and scrubbing an id would break the join contract).

### `atcr debt resolve`

Lists the open backlog for a fix cycle and records resolutions as append-only
status records.

```bash
atcr debt resolve                              # open items, most severe first
atcr debt resolve --json --max 5               # the same, as JSON, capped
atcr debt resolve <id>                         # mark it fixed
atcr debt resolve <id> --status wontfix --reason "accepted pattern"
```

Flags: `--store`, `--json`, `--severity`, `--max`,
`--status` (`resolved|wontfix`), `--reason`. The id is positional. `--status wontfix` requires a
`--reason` — it is a permanent dismissal, so the rationale is recorded with it.

Resolution is append-only: a terminal record is appended, never edited in place,
and the original id is preserved so the resolution lines up with the finding.

### `atcr debt compact`

Folds the append-only store to one effective record per id and rewrites the
shards atomically, carrying `occurrences` and `first_seen` forward so the
regression signal survives at O(1) size instead of O(history). The resolution
trail is kept: when an item was closed and has since regressed, compaction
retains both the current record and the resolution that closed it, so the
`--reason` text is never destroyed.

```bash
atcr debt compact --dry-run  # reports what a real run would drop; writes nothing
atcr debt compact            # reports records before/after and how many were dropped
```

It is the one irreversible subcommand in the namespace, so `--dry-run` runs the
same fold and reports the same before/after/dropped counts without touching a
file. When nothing is superseded, both modes say so rather than reporting a fold
that changed nothing.

Compaction also runs **automatically** after a reconcile append, once the store
trips **100k records or 100 MiB, whichever comes first** (and only when it has
grown materially since the last compaction, so an already-compact store does not
rewrite itself on every append). The manual command remains for on-demand use.

### `atcr debt backfill-justifications`

Re-derives each **live** record's `justification` from the `review.md` it was
originally stamped from, and rewrites the ones that changed. Live means `open` or
`deferred`. A `resolved` or `wontfix` record is settled and is never scanned: its
justification may be the operator's `--reason` (see `debt resolve`), which exists
nowhere else in the tree and cannot be replayed from anything. `deferred` is the
opposite case — it carries a terminal marker but means "not now", so it is still
closeable debt whose stale excerpt still gates the `wontfix` path.

It exists because a record's id excludes its justification. `StampID` hashes
`file\x00line\x00problem`, so a re-detected finding hashes to the same id, the
reconcile append is deduped away, and an improvement to the narrative extractor
reaches only records persisted after it. Excerpts already on disk keep their
original text — which is what `debt resolve --status wontfix` reads when deciding
whether an item already carries a recorded rationale.

```bash
atcr debt backfill-justifications --dry-run  # reports what would change; writes nothing
atcr debt backfill-justifications            # rewrites the excerpts that changed
```

`--review-root` selects the tree searched for source narratives; unset, it is the
repo root. A record is repaired only when **exactly one** surviving `review.md`
anchors it: `source_report.path` is review-dir-relative
(`sources/pool/raw/agent/dax/review.md`) and every review directory holds the same
relative paths, so the path alone selects dozens of namesakes. The anchor line is
re-scored against the record's own `file:line` to tell them apart, and a record
whose candidates disagree is left alone rather than rewritten from a guess. The
run reports those as `ambiguous`, and records whose narrative tree is gone as
`unresolved`.

Within a repaired id, only the **lines** still carrying the stale excerpt are
written. One id can hold several lines — `debt resolve` appends a copy of the
effective record, and a later re-detection displaces it — and a resolution line's
justification may be an operator's `--reason`. Matching on the stored text leaves
that line alone. The run reports both counts, e.g. `3 rewritten (5 lines)`, and
`--dry-run` prints the before and after of every line it would touch:

```console
$ atcr debt backfill-justifications --dry-run
dry run: 1 scanned, 1 rewritten (1 line), 0 unchanged, 0 unresolved (no surviving review.md), 0 ambiguous (candidates disagreed), 0 skipped (settled: resolved or wontfix)
  2026-08.jsonl:1 "aaaa1111"
    before: "- **internal/thing.go:42** the real narrative explaining the defect."
    after:  "```\n- **internal/thing.go:42** the real narrative explaining the defect."
```

It is deliberately a one-off command and **not** a step in the write path.
Refreshing excerpts on every reconcile would re-append a record whenever a reviewer
reworded its narrative, and store growth would stop being bounded by finding count
— which is the whole reason the append path dedupes. Running the repair once, on
demand, does not. The pass is idempotent: a second run rewrites nothing.

## End to end

```console
$ atcr debt add --severity HIGH --file internal/x/y.go:12 \
    --problem "unbounded retry loop on 5xx" \
    --fix "cap retries and add jittered backoff" --category correctness --est 30
Added HIGH item 8421025ce7cde4d3 to /home/you/repo/.atcr/debt.

$ atcr debt list
ID                SEVERITY  STATUS  EST  FILE                CATEGORY     PROBLEM
8421025ce7cde4d3  HIGH      open    30   internal/x/y.go:12  correctness  unbounded retry loop on 5xx

$ atcr debt dashboard --top 5
## Top Priority

| ID | Severity | File | Est | Problem |
|----|----------|------|-----|---------|
| 8421025ce7cde4d3 | HIGH | internal/x/y.go:12 | 30 | unbounded retry loop on 5xx |

$ atcr debt list --json
[
  {
    "schema_version": 3,
    "id": "8421025ce7cde4d3",
    "run_id": "2026-08-05T04:44:55Z-manual",
    "ts": "2026-08-05T04:44:55Z",
    "severity": "HIGH",
    "file": "internal/x/y.go",
    "line": 12,
    "problem": "unbounded retry loop on 5xx",
    "fix": "cap retries and add jittered backoff",
    "category": "correctness",
    "est_minutes": 30,
    "evidence": "",
    "reviewers": null,
    "confidence": "",
    "origin": "manual",
    "occurrences": 1,
    "counted_through": "2026-08-05T04:44:55Z",
    "first_seen": "2026-08-05T04:44:55Z"
  }
]

$ atcr debt resolve 8421025ce7cde4d3
Marked 8421025ce7cde4d3 resolved.

$ atcr debt list --status open
No matching technical-debt items.

$ atcr debt list
ID                SEVERITY  STATUS    EST  FILE                CATEGORY     PROBLEM
8421025ce7cde4d3  HIGH      resolved  30   internal/x/y.go:12  correctness  unbounded retry loop on 5xx
```

The id is copied character-for-character out of the rendered table — the same
string in every view, which is the whole point of rendering it.

## Consuming `atcr debt list --json`

`--json` emits a JSON **array**, one object per effective record, using the field
names in the schema table above. It is never `null` and never the human-readable
empty message: an empty selection prints `[]` and exits 0, so a consumer never
has to special-case the empty store. Filters and sorting apply before encoding,
so the JSON order matches the table order.

`atcr debt list --json` and `atcr debt resolve --json` share one encoder, so the
two surfaces cannot drift into different shapes.

Contract for a downstream consumer:

- Join on `id`; keep your own workflow state in your own store, keyed by that id.
- Ignore unknown keys, so an additive schema bump stays non-breaking.
- Read only. A consumer never writes into `.atcr/debt/`.

## How findings enter the store

`atcr reconcile` appends each run's reconciled findings to the store as part of
the run (`--no-local-debt` opts out). The MCP `atcr_reconcile` tool persists
through the same code path, so a review driven by an editor or agent lands in the
same backlog a CLI review does.

The store's location resolves by **explicit `--repo-root` > the review manifest >
the current working directory** (`--repo` remains a deprecated alias). The repo root recorded in the manifest at review
time is re-validated before any write (it must exist and look like a repository);
when it does not — an artifact tree copied to another machine carries a stale
absolute path — the run warns and persists nothing rather than writing to the
wrong place.

> **A shared review tree discloses where it was reviewed.** To make that
> resolution work off the reviewing machine, `manifest.json` records the
> absolute path of the repository it was reviewed in — which on a developer
> machine contains your username (`/Users/<you>/src/<repo>`). Nothing renders
> the value, so it is disclosed only by sharing the artifacts: attaching a
> review tree to a PR or bug report, or copying a `--output-dir` result. Strip
> or rewrite the manifest's `root` field before publishing one. Removing it
> costs only the manifest tier of the store-root resolution above — pass
> `--repo-root` / the MCP `repo` argument instead.

The MCP path has only the first two tiers: **`repo` > the review manifest**, with
no working-directory fallback. The server's working directory is whatever
launched it, not the reviewed repo, so a review whose manifest records no root
persists nothing unless the `repo` argument names one — and that argument must be
an **absolute** path (a relative one would resolve against the server's directory,
not yours). The tool result carries `debt_persisted` and, when nothing was
written, `debt_skipped_reason`; pass `no_local_debt: true` to skip the write
deliberately, the way `--no-local-debt` does on the CLI.

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
if ! go run ./cmd/atcr debt dashboard --output docs/debt-report.md --check; then
  echo "The technical-debt dashboard is stale."
  echo "Regenerate and stage it:  go run ./cmd/atcr debt dashboard --output docs/debt-report.md && git add docs/debt-report.md"
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
      - run: go run ./cmd/atcr debt dashboard --output docs/debt-report.md --check
```

To auto-regenerate instead of gating, drop the `--check` and commit the result
from a scheduled workflow (the pattern the repo already uses for other
generated files), rather than editing the shared pre-commit hook.

## Breaking changes from the private-scope tooling

Earlier atcr versions split `atcr debt` across two stores: `list`/`add`/
`dashboard` read a private-scope Markdown-table-plus-YAML-shard tree, while
`resolve`/`compact` read `.atcr/debt/`. `atcr debt list` could not show an item
`atcr debt resolve` was able to close. That split is gone; these flags and
commands went with it.

| Retired | Replaced by | Why |
|---------|-------------|-----|
| `--items` (shard directory) and `--readme` (table file) on `list`/`add`/`dashboard` | `--dir` | That store is no longer read or written by any atcr code |
| `--sync` on `add` | — | Nothing to synchronize: there is one store |
| `--group`, `--label`, `--source-type`, `--source`, `--date` on `add` | — | Consumer workflow vocabulary, excluded by the seam |
| `--group` on `list` | — | Same seam: group is not a record field, so there is nothing to filter on |
| `--out` and `--stdout` on `dashboard` | `--output` (empty = stdout) | Parity with `atcr report` |
| The default `DASHBOARD.md` output path | `--output <file>` (explicit; empty = stdout) | Nothing is written unless you name a file |
| The `Wrote dashboard to <file>.` confirmation line | — | `--output` is silent on stdout, matching `atcr report`; `--check`'s status lines are unaffected |
| The `td-migrate` command | — | The shard format it migrated no longer exists |

Passing a retired flag now fails with an unknown-flag usage error, which is the
correct outcome: each pointed at a store the command no longer reads. Run
`atcr debt <subcommand> --help` for the current flag set.

The legacy private-scope backlog file is **left exactly where it is** — no
import, no archive, no rename. atcr does not read it, and it is drained by the
tooling that owns it on its own timeline.

## Related documentation

- [skill-usage.md](skill-usage.md) — the `/atcr debt resolve` skill route
- [technical-debt-format.md](technical-debt-format.md) — the `(symbolName)` anchor convention in `problem` text
- [findings-format.md](findings-format.md) — the reviewer finding format that feeds reconcile
- [agentic-consumption.md](agentic-consumption.md) — machine-readable output across the CLI
