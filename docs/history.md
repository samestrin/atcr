# Finding History

`atcr` records every finding from every review run to an append-only, per-repo
ledger, and `atcr history` reads that ledger back as a per-package trend table:
how many findings, at what severities, did each package accrue over a window.

The ledger answers "is this package getting better or worse over time?" It is not
a backlog — for the resolution-oriented store of what still needs fixing, see
[Technical-Debt Tooling](technical-debt.md), which keeps a separate
`.atcr/debt/` store with the opposite retention policy.

Recording is silent and automatic: every successful `atcr review` run — including
a resumed one (`atcr review --resume`) — appends to the ledger as a byproduct. A
history write failure never fails a review.

---

## Storage layout

Two locations under the repo root make up the full queryable history. Both live
under `.atcr/`, so a standalone user with no `.planning/` directory has a working
history:

| Path | Role |
|---|---|
| `.atcr/history/YYYY-MM.jsonl` | Monthly shards — the only write target |
| `.atcr/findings-history.jsonl` | Legacy pre-19.4 flat ledger — read-only |

Reads union both; writes only ever go to the current month's shard. The legacy
flat ledger is read in place and is never moved, rewritten, or deleted, so a
repo that accrued history before sharding keeps that data queryable alongside new
shards. There is nothing to migrate.

The month in a shard's name is taken in **UTC**, so shard names are deterministic
regardless of the machine's local zone, and every record from one run lands in
exactly one shard.

- **Append-only.** Each run appends; existing lines are never rewritten.
- **Permissions.** Shard files are created `0600` and the directory `0700`. The
  directory is created lazily on the first write.
- **Tolerant reads.** A blank or malformed line is skipped rather than failing
  the whole read, and an unreadable shard is skipped with a warning so the
  remaining shards stay queryable. The ledger is append-only with no repair
  command, so one torn write must not permanently break `atcr history`.

> **Do not commit `.atcr/`.** It is local, uncommitted state. Trend data is
> per-checkout by design; it is not shared through version control.

### Record schema

One JSON object per line:

```json
{
  "ts": "2026-08-05T19:49:24Z",
  "package": "internal/registry",
  "severity": "HIGH",
  "id": "3f2a9c1b7d4e6058",
  "file": "internal/registry/precedence.go",
  "category": "CORRECTNESS"
}
```

| Field | Meaning |
|---|---|
| `ts` | The review run's start time. Every record from one run shares it. |
| `package` | The finding file's directory — what `--package` matches against. |
| `severity` | The severity at the time of the run. |
| `id` | Stable content hash of file + line + problem. |
| `file` | The finding's cited file path. |
| `category` | The finding's category label. |

`id` deliberately excludes severity: severity is re-settled by the debate and
verify stages, so keying on it would mint a new id whenever severity changed and
defeat cross-run trend tracking. The same finding therefore keeps one id across
runs even as its severity moves.

### Deduplication

The union of the two read locations is deduplicated on **(`ts`, `id`)** — one
occurrence of a finding, recorded by one run.

Both halves of that key matter. Deduplicating on `id` alone would fold every
recurrence of a finding into a single row, destroying exactly the trend the
command reports; deduplicating on `ts` alone would fold every distinct finding
from one run into a single row. A finding that survives six monthly reviews
appears six times, and that is the point.

---

## CLI usage

```bash
atcr history                                   # last 90 days, all packages
atcr history --since 30d                       # narrow the window
atcr history --since 2w --package internal/registry
```

| Flag | Meaning |
|---|---|
| `--since` | Window to report, in `h`/`m`/`s` or `d`/`w` units (`48h`, `30d`, `2w`). Default `90d`. |
| `--package` | Restrict to a package path prefix. Separator-aware: `internal/registry` matches `internal/registry/sub` but never the sibling `internal/registry2`. |
| `--prune` | **Destructive.** Delete monthly shards older than this retention horizon before reporting. No default. |

Output is a markdown table of counts by severity per package, with a totals row.

An absent or fully-filtered history is **not** an error: the command exits 0 with
a short notice. A repo that has never recorded anything is told to run `atcr
review` first; a repo whose history merely predates the window is told the window
is empty instead.

### Windowed reads only open the shards they need

`--since` selects shards by **filename** before opening them. Because the file
name encodes the month, a `--since 30d` query opens at most two shard files no
matter how many years of history a repo has accrued — a narrow query costs
proportionally to its window, not to the size of the ledger.

Selection works at file granularity and may over-select, never under-select: a
month that merely contains the cutoff is read whole, and a `*.jsonl` file whose
stem is not a `YYYY-MM` month is read rather than assumed out of window. The
exact per-record window is still applied after loading, so the report is precise
either way.

The legacy flat ledger has no month in its name and is always read in full.

---

## Retention

History is never trimmed automatically. Review runs only ever append, and reading
history never deletes anything — the ledger's value *is* the history, so growth is
bounded by an explicit retention decision rather than by silently discarding old
data.

There is deliberately **no compaction**. The sibling `.atcr/debt/` store folds to
the latest record per finding id, because a backlog should track live findings;
doing that here would collapse a six-month trend into one row. Retention is by
time instead, at file granularity:

```bash
atcr history --prune 365d      # delete shards wholly older than a year
atcr history --prune 24w
```

`--prune` takes the same units as `--since` and has **no default** — omit it and
nothing is ever deleted. It removes whole shards and reports each one by name, so
a deletion is never silent. Guardrails:

- A month that contains the cutoff is **kept whole**, because it still holds
  in-horizon records. What survives a prune is exactly what a query over the same
  window would have read.
- Only `*.jsonl` files are candidates. A stem that is not a `YYYY-MM` month is
  never deleted — an unrecognized name cannot be proven past the horizon.
- The legacy flat ledger is never pruned. It is one file spanning every pre-19.4
  month, so deleting it would be record-level pruning by proxy.
- A non-positive horizon is a usage error, not a wipe.
- `--prune` cannot be combined with `--package`. A shard holds every package's
  records for its month, so pruning is not scopeable to one package; the
  combination is rejected rather than silently deleting more than asked.

To reset history entirely, delete `.atcr/history/` (and, if you want the pre-19.4
data gone too, `.atcr/findings-history.jsonl`) by hand.
