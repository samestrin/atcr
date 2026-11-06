# Technical-Debt Tooling (`atcr debt`)

`atcr debt` queries, captures, and reports on the project's technical debt. All
of its subcommands — `list`, `add`, `import`, `dashboard`, `report`, `resolve`,
`convert`, `compact`, and the read-only `filter`, `clean`, `group` and `validate` views — read and write **one**
store: the append-only JSONL backlog under `.atcr/debt/` (or wherever
`debt.store` / `--store` puts it) that `atcr reconcile` populates. An item filed
by `atcr debt add` is therefore visible to `atcr debt list` and closeable by
`atcr debt resolve`.

The store has two record layouts. Month shards are the default and what every
store used before this change. The opt-in **event layout** writes one file per
add and per resolution, so two clones can share a store through git and merge
with no conflict. atcr reads both, and one store may hold both; `atcr debt
convert` moves a store from the first to the second.

The Markdown views this tooling renders are **projections** of that log, not the
authoritative copy. The log is the system of record; a table or dashboard is one
rendering of it at a moment in time.

## The store

A store is a directory of append-only record files. atcr understands two
layouts, and one store may hold both:

| Layout | Files | Written when |
|--------|-------|--------------|
| Month shards (default) | One `YYYY-MM.jsonl` per calendar month, selected by the `YYYY-MM` prefix of each record's `run_id` | The store was never opted in — every store before the event layout, and every new store |
| Event files (opt-in) | One file per record: `items/<finding id>.<event ts>-<short hash>.json` | The store has an `items/` directory (see [Event layout](#event-layout-one-file-per-item)) |

| Property | Value |
|----------|-------|
| Location | `<repo>/.atcr/debt` by default; `--store`, `debt.store` in `.atcr/config.yaml`, or `debt.store` in `~/.config/atcr/registry.yaml` override it (see [Where the store lives](#where-the-store-lives)) |
| Mutation model | Append-only; a month shard takes one `O_APPEND` write per record, an event is written under a temporary name and renamed into place, both under a cross-process lock: an OS file lock on `.flock` in the store directory (`flock` on Linux and macOS, `LockFileEx` on Windows), released by the kernel if the holder dies; only builds that are neither unix nor Windows (js/wasm, wasip1, plan9; none are shipped) keep the older lock directory (`.lock`) |
| Read model | Every read folds all record files to one **effective** record per id |

Every subcommand resolves the store through `--store`. With no `--store`, atcr walks
up from the working directory to the nearest `.git`/`.atcr` marker, then resolves
the store under that repository from `debt.store` (project, then user), falling
back to that repository's `.atcr/debt` — so `atcr debt list` reads the same store
from a subdirectory as it does from the root. Pass `--store` to work against
another checkout or a fixture tree. `--dir` remains a deprecated hidden alias.

A resolution never edits a line in place: it appends a new record carrying the
same id and a terminal status (a new line in a month shard, a new event file in
the event layout). Reads fold the log to one **effective** record per id, so
history is preserved without the views showing an item twice.

`.atcr/` is local, uncommitted state by default, and whether the store is tracked
is your per-repo `.gitignore` decision — the single correct lever. Ignore
`.atcr/` in a public repo and the backlog stays private; track it in a private
repo and the history is preserved with the code. To let two clones share that
tracked history, use the event layout ([Sharing a store between
clones](#sharing-a-store-between-clones)); a store configured outside the
repository is outside this lever entirely.

## Event layout (one file per item)

Month shards are files that two clones both extend, so git conflicts on every
month both touched. In the event layout each add and each resolution is a new
file whose name no other clone produces differently, so a merge has nothing to
conflict on and the whole history is kept.

Each file under `<store>/items/` is named:

```
<finding id>.<event ts>-<short hash>.json
```

- `<finding id>` is the 16-lowercase-hex [finding id](#finding-ids). It must be
  well formed, because it becomes part of a path: an id holding a dot or a slash
  would break the name or escape `items/`.
- `<event ts>` is the record's own `ts` in UTC, formatted
  `20060102T150405.000000000Z` — fixed width and free of the colons RFC3339
  carries (not legal in a Windows file name), so one item's events sort
  chronologically in name order.
- `<short hash>` is the first 12 hex characters of SHA-256 over the exact bytes
  of the file: the record's JSON line, newline included. It tells apart two
  different events of one item at the same instant; writing the same event twice
  produces the same name over the same bytes.

Each file holds one record, byte for byte the line a month shard would have
carried.

The layout is opt-in: a new store keeps month shards, so a standalone user sees
no change. The presence of `<store>/items/` is the switch — `atcr debt convert`
creates it, and every append after that writes an event file instead of a shard
line. Because git does not track an empty directory, a converted store also
carries an empty `items/.event-layout` marker, so a store opted in before its
first event still clones as an event store. A clone of an opted-in store is
opted in too: the directory is in the tree.

Every read path folds both layouts together — month shards in lexical order
first, then event files in name order — so a store holding both reads as one
backlog. Compaction and justification backfill treat event files as frozen:
`atcr debt compact` never rewrites, moves or removes one, and `atcr debt
backfill-justifications` skips any id held in an event file (reporting it as
left unrepaired) rather than rewriting a file it cannot change. The event layout
keeps full history and is not shrunk by compaction; the month-shard layout is
the one `atcr debt compact` folds.

## Where the store lives

The default is `<repo>/.atcr/debt`, where `<repo>` is the repository root found
by walking up from the working directory as described above. It resolves in this
order; the first one set wins:

1. `--store <path>` on the command. Any path; `--dir` remains a deprecated
   hidden alias.
2. `debt.store` in the project's `.atcr/config.yaml`, relative to the repository
   root and required to stay inside it:

   ```yaml
   debt:
     store: .debt
   ```

3. `debt.store` in the user's `~/.config/atcr/registry.yaml`. Absolute as given,
   or relative to the repository root:

   ```yaml
   debt:
     store: ../planning/debt
   ```

4. Otherwise `<repo>/.atcr/debt`.

The two file tiers are trusted differently, because the project file is
committed and anyone who can land a commit can write it:

- A **project** value must be a clean repo-relative path: no leading `/`, no
  `..` segment, no `./`, `//` or trailing `/`, not `.`, and it must still be
  inside the repository once every symlink on its existing prefix is followed.
  Anything else is an error naming the value, which points at the two ways to
  set an external store instead. A committed config cannot redirect atcr's
  writes outside the repository.
- A **user** value may point anywhere: absolute as given, relative against the
  repository root (so `../planning/debt` in `~/src/app` means
  `~/src/planning/debt` — the same string in another repo resolves elsewhere),
  and it gets the same system-directory guard `--store` gets.
- `ATCR_REGISTRY_URL` is deliberately ignored for this key: the store path is a
  per-user, per-machine choice, so a shared team registry must not choose it for
  everyone.
- An invalid or unreadable value is an error, never a fall-through to the next
  tier: falling through would turn a visible mistake into a silent write to a
  different store. A blank value counts as unset.

One resolver (`localdebt.StoreDir`) applies these rules for every debt command,
`atcr reconcile`'s persistence, the quality-signal payload and the next-step
suggestions, so a configured store cannot be read by one path and missed by
another. The root it resolves under has its own rules — see [How findings enter
the store](#how-findings-enter-the-store).

Because a committed project path may not leave the repository, sharing a store
through a sibling private planning repo is a per-user setting in
`registry.yaml`, or an explicit `--store`, not a project one.

## Sharing a store between clones

Two working copies — a laptop and a server, or two teammates — can read and
write one backlog if the store is committed to git (typically in a private
repo) or lives outside both repositories. The store must be on the event layout:
month shards still conflict at merge.

### Set it up once

1. Choose the store: the repository's own `.atcr/debt`, tracked in git, or a
   store outside the tree set per user through `debt.store` / `--store` (see
   [Where the store lives](#where-the-store-lives)).
2. If it still has month shards, convert it from one clone:

   ```bash
   atcr debt convert --dry-run   # see what would move
   atcr debt convert             # move it; this opts the store in
   ```

3. Commit the store's `items/` directory, including its `.event-layout` marker,
   and any month shard the conversion left in place.
4. Pull in the other clones. Nothing else needs configuring: the `items/`
   directory in the tree opts each clone in.

### Day to day

- An add writes a new event file. `atcr debt add` always does; a reconcile
  append does only when its write-time dedup does not skip the id, which it
  does while the id is open or dismissed as `wontfix`.
- A resolution appends an event file for the same id. It never edits or deletes
  the earlier events, so the item's history crosses the merge intact.
- Merge with plain `git merge`, `git pull` or `git rebase`. Two clones cannot
  produce different files under one name: a name is the finding id, the event's
  own timestamp and a hash of its bytes, so the same name means the same content
  and different content gets a different name. Git sees added paths and merges
  them with no conflict.
- If both clones resolved the same id, both resolutions survive and the fold
  keeps the newest one. A `wontfix` is the one unconditional exception: if any
  clone dismissed the id, the dismissal wins no matter when it was written.
  Otherwise the resolution with the latest timestamp wins, so a later `resolved`
  beats an earlier `unreproducible`. The status chain (`wontfix` >
  `unreproducible` > `attempts-exhausted` > `resolved` > `deferred`) only breaks
  an exact same-timestamp tie; the same chain decides which record
  `atcr debt compact` retains. Read the result with `atcr debt list`; do not
  hand-edit event files to change a status.
- `atcr debt compact` does nothing to `items/`, so a shared event store keeps its
  full history and does not shrink at merge time. If unbounded growth matters
  more than sharing, keep a single-writer month-shard store instead — do not
  share month shards between clones.

### What the lock does and does not cover

The store lock (`.flock` in the store directory) serializes writers that share
one store path on one machine. It cannot see another machine, and each clone has
its own copy of the lock file, so two clones can write at the same instant. That
is safe in the event layout because no write edits a shared file: each event is a
complete new file published by rename, and two clones only collide on a name
when they wrote the same bytes. A month shard's single `O_APPEND` write has no
such protection across machines, which is the other reason a shared store is
converted first.

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

## Record schema (v4)

| Field | Type | Meaning |
|-------|------|---------|
| `schema_version` | int | Record format version (currently `4`) |
| `id` | string | Stable finding id; the join key |
| `run_id` | string | Producing run; its `YYYY-MM` prefix selects the record's month shard when the store uses month shards |
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
| `status` | string | Empty (open), `deferred`, `resolved`, `wontfix`, `unreproducible`, or `attempts-exhausted` |
| `resolved_at` | string | Timestamp of the terminal record |
| `origin` | string | **v3** — `review` or `manual` |
| `occurrences` | int | **v3** — times this id has been seen; carried through compaction |
| `counted_through` | string | **v3** — newest sighting `occurrences` accounts for; what makes the count idempotent under repeated compaction |
| `first_seen` | string | **v3** — timestamp of the id's first append; carried through compaction |
| `confidence_discount` | string | **v3** — why reconcile lowered `confidence` below what the reviewer count earns (today only the panel-wide-instruction reason); absent when it did not. `atcr debt dashboard` appends it to the Top Priority problem cell as `(confidence discounted: …)` |
| `reconciler_feedback` | object | **v3** — a verdict on how reconcile grouped the finding, not on the finding: `{"kind": …, "with": …, "reason": …}`, where `kind` is `wrong-merge`, `wrong-split` or `wrong-category` and `with` (only on `wrong-split`, optional) is the partner finding's id. It never changes `status`, and a record carrying it is not counted as a sighting in `occurrences`; absent on every unannotated record |
| `attempts` | int | **v4** — how many times a fix was tried, from the `Attempts` column of a technical-debt README table; never negative, absent when none was recorded |

Reads are tolerant in one direction only: a v1, v2 or v3 record decodes cleanly, with
zero values for fields that did not exist yet, and a record with no `origin`
predates v3 and means `review`. A record from a *newer* schema (v5+) is skipped
with a warning and left untouched on disk rather than being folded or dropped by
a binary that cannot understand it.

## Status lifetimes

| Status | Lifetime | Rationale |
|--------|----------|-----------|
| `wontfix` | Terminal | Suppression is the feature; a false positive is stable at a stable location, so its id is stable and permanent dismissal works. Requires a `--reason`. |
| `resolved` | Re-openable on re-detection | The same id after a fix implies a regression — the thing most worth surfacing. |
| `deferred` | Re-surfaces on re-detection | "Not now" is not "never". A deferred item leaves the `debt resolve` worklist while it stands, but stays in `debt list` and the dashboard as live debt, and stays closeable by id. |
| `unreproducible` | Re-openable on re-detection | Investigated and could not be reproduced. A determination was reached, so the item leaves the live backlog — but re-detection at the same location is evidence the call was wrong, which is the last thing to suppress. Requires a `--reason`. |
| `attempts-exhausted` | Re-surfaces on re-detection | The fix attempts ran out without a resolution. The defect is presumed real and the work unfinished, so like `deferred` it stays live debt and stays closeable by id. Like `deferred`, it is excluded from the no-argument `debt resolve` fix worklist while it stands — its row is closed, so the worklist treats it as acted-on — but it remains visible in `debt list` and the dashboard. Requires a `--reason`. |

So `atcr debt list` can show an item as `resolved` today and as open again after
a later `atcr reconcile` re-detects it. Only `wontfix` is final.

`occurrences` is the regression count for an id: `occurrences - 1` re-detections
after the first. When divergent terminal records exist for one id, precedence is
`wontfix` > `unreproducible` > `attempts-exhausted` > `resolved` > `deferred`.

That order ranks by how certainly a record carries a human-typed `--reason`,
because the rationale text exists nowhere else in the store and precedence
decides which record survives compaction. `--reason` is mandatory for every
status except `resolved` — or a recorded justification, in the one case the code
allows (`wontfix` accepts a justification already recorded on the open record in
place of a typed `--reason`) — which is why the three mandatory ones sit above it.

## Commands

| Command | Purpose |
|---------|---------|
| `atcr debt list` | Print debt items as a table or JSON, with filtering and sorting |
| `atcr debt add` | File a new item into the store |
| `atcr debt import` | Import every row of a technical-debt README table into the store, with its status and attempts |
| `atcr debt dashboard` | Render an aggregated Markdown dashboard |
| `atcr debt report` | Render every item as a Markdown report, one table per README section |
| `atcr debt filter` | Select open items by severity, confidence, status and mode (replaces `llm-support td-filter`) |
| `atcr debt clean` | Show what `llm-support td-clean` would leave: open, deferred and unreproducible items (read-only) |
| `atcr debt group` | Group open items by path, category or file (replaces `llm-support group-td`; read-only) |
| `atcr debt validate` | Check each row's file, line and named symbol against the working tree; optional coherence scoring (replaces `llm-support td_validate`; read-only) |
| `atcr debt resolve` | List open items and record resolutions |
| `atcr debt recluster` | Record feedback on how reconcile grouped a finding |
| `atcr debt compact` | Fold the append-only store's month shards to their live records |
| `atcr debt convert` | Move month shards to the event layout (one way; checked by a fold comparison) |
| `atcr debt backfill-justifications` | Replay stored justifications from their source `review.md` (one-off repair) |
| `atcr debt sync-outcomes` | Record the outcomes of ticked technical-debt README rows that carry a `debt:<id>` token; `--report` shows whether the scorecard gate is met |

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
atcr debt list --max 20                         # the 20 highest-severity items
```

Flags: `--store`, `--severity` (exact, case-insensitive), `--status` (exact:
`open|deferred|resolved|wontfix|unreproducible|attempts-exhausted`),
`--category` (substring), `--component` (path prefix, e.g. `internal/autofix`),
`--origin` (exact: `review|manual`), `--sort` (`severity|age|est|file`), `--json`,
`--max` (cap on rows listed, applied after the filters and the sort; default
`0` = no cap; a negative value is a usage error, exit 2).

`--max` keeps the first N items of the sorted selection, in the table and in
`--json` alike. It is a cap, not a pager: there is no offset. Unlike
`debt resolve --max`, which defaults to `10`, `list` defaults to no cap, so an
unflagged list is unchanged. On a large store a capped list reads in full only
the N records it prints, but the summary pass that picks them still streams the
whole store, so peak memory falls but is not proportional to N.

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

The record is written with `origin: manual` and a synthetic `run_id` carrying a
`YYYY-MM` prefix, so a hand-filed item lands in the same month bucket a reviewed
one would: in that month's shard, or in its own event file on an opted-in store.

Omit the required flags on an interactive terminal to be walked through a
prompt instead. In a non-interactive context (CI, a pipe) with required flags
missing, the command exits with a usage error rather than blocking on input.

### `atcr debt import`

Reads every item row of a technical-debt README table (the
`| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |`
layout; columns are found by header name, so other orders read alike) and
writes each one to the store, in whichever layout the store uses.

```bash
atcr debt import docs/technical-debt/README.md --dry-run   # what would be written; writes nothing
atcr debt import docs/technical-debt/README.md             # write it
```

Flags: `--store`, `--dry-run`.

Each row keeps its status and attempts. The marker in the second column sets
the status:

| Marker | Status |
|--------|--------|
| `[ ]` | `open` |
| `[/]` | `deferred` |
| `[x]` | `resolved` |
| `[-]` | `unreproducible` |

A row's time and `run_id` come from the date of the `### [date] From: ...`
section it sits under (`2026-11-06` or `November 6, 2026`), never from the
clock. Running the import again on the same table therefore writes nothing,
and a run that stopped part way writes only what is missing. Each record is
filed with `origin: manual`, a `run_id` ending in `-import`, and a
`source_report` holding the README path (relative to the repo root) and the
section heading, which `atcr debt report` groups by.

Every item row ends up in exactly one of three lists, and the first line of the
output counts them:

- **imported**: written to the store, or already there from an earlier run.
- **merged**: the row has the same finding id as another row. The row from the
  newest section is kept (the later row in the file on a tie), and the output
  names the line it was merged into.
- **rejected**: the row cannot be imported as written, with the reason printed:
  it is under no dated section, its marker or severity is unknown, its File or
  Problem cell is empty, its Est Minutes is too large, or the record would be
  too large for the store.

```text
42 item rows: 39 imported, 2 merged, 1 rejected.
Import: wrote 39 records (0 already in the store).
  merged   line 88: id 338d98c562925e37, kept line 17
  rejected line 120: the row is under no dated `### [date]` section
```

Control characters in a cell are stripped before the record is written. The
README itself is never changed.

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
atcr debt dashboard --json                            # the same sections as JSON
```

Flags: `--store`, `--output`, `--top`, `--check`, `--json`.

`--check` compares against the file named by `--output`, so the two are used
together; `--check` without `--output` is a usage error.

`--json` prints the same sections as one JSON document on stdout: `totals`, then
`by_severity`, `by_component`, `by_month` and `top_priority`, each an array with
one element per row of the matching table (empty, never `null`, when the table
is). A `top_priority` row carries `id`, `severity`, `file`, `line`,
`est_minutes` and `problem`. Free text is scrubbed of secret-shaped tokens as in
the Markdown. `--json` with `--output` or `--check` is a usage error (exit 2);
redirect stdout to save the JSON.

The render is **deterministic** — no generation timestamp, age grouped by
calendar month — so `--check` flags real content drift, not clock movement.

The totals header and the By Severity table end with a **Reconciler
corrections** count: findings whose effective record carries a
`reconciler_feedback` annotation recorded by `atcr debt recluster`. It is not a
status bucket and not a summand of Total — an annotated finding is still
counted under its status — which is why it prints last, after every status
counter.
Secret-shaped tokens (bearer / `sk-` API keys) accidentally pasted into finding
text are scrubbed from the output; file paths and finding ids are preserved (both
are required core data, and scrubbing an id would break the join contract).

### `atcr debt report`

Renders every item in the store, in every status, as a Markdown report for
people to read: one table per README section that `atcr debt import` read the
items from. Dated sections come newest first, then undated ones; items that came
from no README section (filed by `atcr reconcile` or `atcr debt add`) come last,
under **Not from a README section**. Within a table the most severe items come
first, then by file and line.

```bash
atcr debt report                                         # print the report
atcr debt report --output docs/technical-debt-report.md  # write it to a file
atcr debt report --output docs/technical-debt-report.md --check   # exit 4 if stale
```

Flags: `--store`, `--format` (`md`, the default and only format), `--output`,
`--check`.

Each table has the columns `ID`, `Status`, `Severity`, `File`, `Problem`,
`Fix`, `Category`, `Est Minutes` and `Attempts`. The second line of the report
marks it as generated. `--output` refuses to overwrite a file that does not
carry that line, so a hand-written file is never lost. Like the dashboard, the
report has no timestamp, so a rerun on an unchanged store is byte-identical and
`--check` flags only real changes. `--check` needs `--output`, and exits 4 when
the file is missing or out of date. Secret-shaped tokens in item text are
scrubbed.

### `atcr debt filter`, `clean` and `group`

Three read-only views over the store, ported from the `llm-support` commands that
read the technical-debt README. They write nothing: no store record, event file
or README is created, changed or deleted. Each emits JSON by default (`--json`)
or a token-dense TOON payload with `--axi`, and takes `--store <path>` (default:
the resolved store, `<repo root>/.atcr/debt` unless `debt.store` sets another).

| Command | Replaces | Output | Flags |
|---------|----------|--------|-------|
| `atcr debt filter` | `llm-support td-filter` | `{items, summary}` | `--severity`, `--confidence`, `--status` (default `open`), `--mode quick-wins\|backlog\|all` (default `quick-wins`), `--max` (default 10, `0` = no cap) |
| `atcr debt clean` | `llm-support td-clean` | `{items, summary}` | none beyond `--store`, `--json`, `--axi` |
| `atcr debt group` | `llm-support group-td` | `{groups, ungrouped, summary}` | `--group-by path\|category\|file` (default `path`), `--path-depth` (default 2), `--min-group-size` (default 3) |

Items are ordered most severe first. `--mode quick-wins` keeps estimates under
30 minutes and `backlog` keeps 30 up to 2880; `all` keeps both.

`atcr debt clean` is a **view, not a cleanup**. `td-clean` deleted `[x]` rows from
the README; here the items it would leave are listed (open, deferred and
unreproducible, with regenerated counts and the per-severity Stats table), and
resolved, wontfix and attempts-exhausted items are counted as excluded. Nothing
is removed from the store; use `atcr debt compact` to fold it.

`atcr debt group` computes its grouping on every run and never stores it: a
theme smaller than `--min-group-size` is left in `ungrouped`, and every CRITICAL
item gets its own group regardless of size.

#### Intended differences from the originals

- They read the store, not the README table, so a README row without a
  `debt:<id>` record is not seen. Run `atcr debt import` to bring a whole
  table into the store, or `atcr debt sync-outcomes` to record ticked rows.
- `filter` does not offer the attempts, group or focus filters. Group is not a
  record field (see [the seam](#ownership-model-and-the-seam)).
- `clean` never rewrites or deletes anything.
- Output is the `{items, summary}` / `{groups, ungrouped, summary}` shape, with
  `--axi` for agents, not the `llm-support` output formats.

#### Other `llm-support` tools

| Tool | Replacement |
|------|-------------|
| `td_dedupe` | `atcr reconcile`, which merges duplicate findings before they reach the store |
| `td_stats` | `atcr debt dashboard` |
| `td_validate` | `atcr debt validate` (the reranker and qdrant grounding of the original are deliberately dropped) |
| `td-matrix` | Not covered by `atcr debt`; tracked as a row in the technical-debt list |

### `atcr debt resolve`

Lists the open backlog for a fix cycle and records resolutions as append-only
status records.

```bash
atcr debt resolve                              # open items, most severe first
atcr debt resolve --json --max 5               # the same, as JSON, capped
atcr debt resolve <id>                         # mark it fixed
atcr debt resolve <id> --status wontfix --reason "accepted pattern"
atcr debt resolve <id> --status unreproducible --reason "no repro on current main"
atcr debt resolve <id> --status attempts-exhausted --reason "three attempts regressed unrelated tests"
```

Flags: `--store`, `--json`, `--severity`, `--max`,
`--status` (`resolved|wontfix|unreproducible|attempts-exhausted`), `--reason`. The id is positional.
Every status other than `resolved` requires a `--reason` (or, for `wontfix`
only, a justification already recorded on the open record — the one stand-in the
CLI accepts): a dismissal, a
not-reproducible determination and an exhausted attempt budget are each a
judgement whose rationale exists nowhere else, while a fix explains itself in the
diff. That rationale is also what makes the technical-debt lifecycle usable as
ground truth when scoring which review lenses produce real findings.

Resolution is append-only: a terminal record is appended, never edited in place,
and the original id is preserved so the resolution lines up with the finding.

### `atcr debt recluster`

Records a verdict on how **reconcile grouped** a finding — not on the finding
itself. The reconciler clusters similar findings from different reviewers into
one; when that clustering is wrong, `recluster` says so:

```bash
atcr debt recluster <id> --kind wrong-merge    --reason "two unrelated races merged"
atcr debt recluster <id> --kind wrong-split    --with <finding-id> --reason "one issue filed twice"
atcr debt recluster <id> --kind wrong-category --reason "this is correctness, not style"
```

The three `--kind` values:

- `wrong-merge` — reconcile merged distinct issues into this finding.
- `wrong-split` — reconcile split one issue, so this finding and another should
  be one; name the other finding with `--with <finding-id>` (the only kind that
  takes a partner).
- `wrong-category` — reconcile filed this finding under the wrong category.

Flags: `--store`, `--kind` (required, one of the three above), `--with`
(the partner finding's id; only with `--kind wrong-split`, and it must name an
existing finding other than the one named positionally), `--reason`
(required). The id is positional and is the leading column of `atcr debt list`.

The feedback is appended to the store as a `reconciler_feedback` annotation (see
the [record schema](#record-schema-v4)). It deliberately changes nothing else:
the finding's `status` stays as it is, the annotation is not counted as a
sighting in `occurrences`, and it does not move the `wontfix` count — close or
dismiss the finding itself with `atcr debt resolve`. The separation matters
because a `wontfix` is a verdict on the reviewer (ground truth for the quality
signal), while a recluster annotation is a verdict on the reconciler's grouping.
The dashboard reports these as **Reconciler corrections**, apart from
`Wontfix`, and the opt-in quality signal counts them on their own keys (see
[docs/telemetry.md](telemetry.md#quality-signal-payload-schema)).

The annotation belongs to the finding, not to one record: a finding keeps its
newest annotation when reconcile detects it again, and when `atcr debt compact`
folds its history. A later `recluster` on the same finding replaces it.

An unknown id, an unknown `--kind`, a `--with` on a kind that takes no partner,
or an empty `--reason` is a usage error (exit 2).

### `atcr debt compact`

Folds the append-only store to one effective record per id and rewrites the
month shards atomically, carrying `occurrences` and `first_seen` forward so the
regression signal survives at O(1) size instead of O(history).

Retention is bounded at **four records per id**: the effective record, at
most one superseded rationale, one donor — the most recent record that carries
model attribution, when it is not already one of the others — and — when the effective record is a
re-detection and `attempts-exhausted` is in play — the latest closed record, so
the re-detection keeps reporting the same outcome after compaction. Two of those
four are the ordinary case; the donor is
the narrow one, and it collapses into the rationale record whenever the
highest-ranked rationale is also the most recent model-carrier. The donor exists
because a fold can select an effective record with no `model` on it, or a later
append can add one, and the quality signal then recovers the model from the most
recent record that carries one. Without the donor, that attribution would be
deleted or moved to an older model by the very pass that is meant only to drop
superseded duplicates. On an exact timestamp tie between the effective record and
the donor, the effective record keeps its status and the model credit can move.

The resolution trail is kept, and it is exactly one record deep: alongside the
effective record, compaction retains the highest-ranked superseded record that
carries a `--reason` of its own. That covers an item closed and since regressed, and an
`attempts-exhausted` checkpoint later closed for good — the closing reason does
not overwrite the checkpoint's. Where **several** superseded records each carry
a distinct `--reason`, only that highest-ranked one survives: the rank order is
`wontfix` > `unreproducible` > `attempts-exhausted` > `resolved`, so two
`attempts-exhausted` rounds followed by a `wontfix` keep the second round's
reason and drop the first. Record anything you need to keep across several
attempt rounds outside the store.

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

Event files are out of reach: compaction folds month shards only, so on an
event-only store it is a successful no-op that says so, and on a mixed store it
reports the event records it left untouched. Automatic compaction therefore has
nothing to shrink in an event-only store.

### `atcr debt convert`

Moves a store from month shards to the [event layout](#event-layout-one-file-per-item).
It is one way: there is no command back, and it is run once per store, not once
per clone.

```bash
atcr debt convert --dry-run   # report what a real run would move; writes nothing
atcr debt convert             # convert, then keep writing event files
```

Flags: `--store`, `--dry-run`.

- **No record is lost, and that is checked rather than assumed.** The run folds
  the store before, folds what the store would be after (the month shards it
  keeps plus every event file, the staged ones included, read back from disk),
  and removes no shard unless every item folds identically both times. If an
  item would fold differently it fails with `converting would change how the
  store folds` (naming up to five ids), changes nothing, and leaves the store on
  month shards.
- **Line order is the one thing the event layout cannot preserve.** An id whose
  records tie on timestamp and status breaks the tie by line order in a month
  shard and by file name in the event layout, so such a store is refused.
  Compacting the month shards first can remove the ambiguity.
- **A shard converts only when every line in it can become an event file.** A
  shard holding a corrupt or over-long line, a record from a newer atcr, or a
  record whose id or timestamp cannot name an event file is left whole and named
  with the reason; its records stay readable where they are. A `.jsonl` file
  that is not a month shard is left alone too.
- **A crash cannot duplicate a record.** New events are staged under
  `<store>/.convert/` (no read path looks there) and the run commits by writing a
  journal naming the shards to remove; past that point it only finishes the plan.
  Running `atcr debt convert` again finishes a conversion a crash left behind
  ("Finished an interrupted conversion") and throws away staging that never
  committed.
- **A dry run, or a failure before the store is opted in, leaves the store
  exactly as it was.** A dry run writes nothing, and a failure during validation
  or the fold comparison happens before anything is enabled. Past that point the
  run has already enabled the event layout and written its commit journal, so a
  failure or crash while publishing the staged events and removing the shards
  can leave the store opted in with month shards and staged events still in
  place. Running `atcr debt convert` again finishes that conversion. On a store
  that already uses the event layout the command writes nothing and says so; on
  a store with no records it says there is nothing to convert.

The `items/` directory and its `.event-layout` marker are part of the store once
the conversion commits: commit them, and every clone that pulls them writes
events too.

### `atcr debt backfill-justifications`

Re-derives a record's `justification` from the `review.md` it was originally
stamped from, and rewrites the ones that changed. A record that **may carry an
operator-typed `--reason`** — `resolved`, `wontfix`, `unreproducible`,
`attempts-exhausted` — is never scanned, because that text exists nowhere else in
the tree and cannot be replayed from anything. Only `open` and `deferred` records
are scanned.

The skip keys on the **rationale**, not on whether the item is settled, and those
two stopped selecting the same records. `attempts-exhausted` is deliberately not
settled — the work is unfinished and the defect presumed real — yet `--reason` is
mandatory for it (see `debt resolve`), so it always carries exactly the text the
skip protects. `deferred` is the mirror image: it carries a terminal marker but
means "not now" and is reason-free, so it stays closeable debt whose stale excerpt
still gates the `wontfix` path.

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
run reports those as `ambiguous`, and records no `review.md` yielded an excerpt for
as `unresolved`. The `unresolved` count is split into three parts. `declined by policy`
counts a record whose own review holds a `review.md` that the producer refuses (a
symlink or other non-regular file, a file over the size cap, a wholly-salvaged reply,
or a desynced bin list), so restoring the file cannot help. The run
treats a review dir as the record's own only when its `reconciled/summary.json`
carries the `reconciled_at` the record's `run_id` was built from, and the dir's name
matches the one in the `run_id`. For a review dir reached through a symlink, either
the link's name or its target's name matches. A `summary.json` that is not a plain
file (a symlink or a named pipe, for example) or is over 1 MiB is not read, so its
review is not the record's own. `policy refusal not tied to the record` counts a
refused `review.md` that sits in a review dir which owns another run, or whose
`reconciled/summary.json` is missing or ignored, so the dir owns nothing: the
refusal may not concern the record at all, and restoring the record's own
`review.md` may still help. `review tree pruned` counts everything else. The three
figures are exclusive and add up to `unresolved`.

A final clause, `N summary.json over 1 MiB ignored (summaries are read only for a policy refusal)`, counts the
`reconciled/summary.json` files the pass ignored for exceeding the size cap —
summary files, not records, so it stands apart from the record counters above. A
review dir whose summary is ignored owns nothing, so a refused `review.md` in it
counts under `policy refusal not tied to the record`, not `declined by policy`. The
pass reads a dir's summary only when a refused `review.md` in that dir made it ask
who owns the record, so `0` here does not prove no oversized summary exists — only
that none was asked about (each skip is also logged at debug level, hidden by
default).

On a store holding event files, ids held there are counted on their own, not in
the partition: `N left unrepaired (held in event files, which are never
rewritten)`. An event file is written once and never changed, so replaying an
excerpt into a shard would report a rewrite that never took effect; the whole id
is skipped, and the clause is absent when no id is held in an event file.

Within a repaired id, only the **lines** still carrying the stale excerpt are
written. One id can hold several lines — `debt resolve` appends a copy of the
effective record, and a later re-detection displaces it — and a resolution line's
justification may be an operator's `--reason`. Matching on the stored text leaves
that line alone. The run reports both counts, e.g. `3 rewritten (5 lines)`, and
`--dry-run` prints the before and after of every line it would touch:

```console
$ atcr debt backfill-justifications --dry-run
dry run: 1 scanned, 1 rewritten (1 line), 0 unchanged, 0 unresolved (0 review tree pruned, 0 declined by policy, 0 policy refusal not tied to the record), 0 ambiguous (candidates disagreed), 0 skipped (carries a rationale: resolved, wontfix, unreproducible or attempts-exhausted), 0 summary.json over 1 MiB ignored (summaries are read only for a policy refusal)
  2026-08.jsonl:1 "aaaa1111"
    before: "- **internal/thing.go:42** the real narrative explaining the defect."
    after:  "```\n- **internal/thing.go:42** the real narrative explaining the defect."
```

It is deliberately a one-off command and **not** a step in the write path.
Refreshing excerpts on every reconcile would re-append a record whenever a reviewer
reworded its narrative, and store growth would stop being bounded by finding count
— which is the whole reason the append path dedupes. Running the repair once, on
demand, does not. The pass is idempotent: a second run rewrites nothing.

### `atcr debt sync-outcomes`

Seats often close debt by ticking a row in a project's own technical-debt
README table rather than through `atcr debt resolve`. Until something carries
that outcome back, the ledger record stays open and the scorecard has no
ground truth for the reviewer that raised it. `sync-outcomes` is that bridge.

```bash
atcr debt sync-outcomes --td path/to/technical-debt/README.md
atcr debt sync-outcomes --report    # read-only; exits 0 either way
```

**The join is the `debt:<id>` token and nothing else.** A row is matched to a
ledger record only when its Problem or Source cell carries `debt:` followed by
the record's 16-hex id. No row is ever matched by file or problem text: rows
written before the convention carry no id, and that kind of matching joined 0
of 374 closed rows when it was measured.

**Capture convention.** When you copy a ledger finding into a TD table row, put
`debt:<id>` in its Problem or Source cell; the id is the leading column of
`atcr debt list`. When you fix a ledger-backed finding, run
`atcr debt resolve <id>` directly. `sync-outcomes` is the catch-up for a row
that was ticked without it.

The row's checkbox marker decides the status, following the TD legend that
`llm-support td-stats` and `td-filter` read:

| Marker | Legend | Recorded as |
|--------|--------|-------------|
| `[x]` or `[X]` | Resolved | `resolved` |
| `[-]` | Unreproducible (closed without a fix) | `unreproducible` |
| `[ ]` | Open | nothing; reported and skipped |
| `[/]` | Deferred | nothing; reported and skipped |

The legend has no marker for `wontfix` or `attempts-exhausted`, so the sync
never writes either; record those with `atcr debt resolve <id> --status ...`
and a typed `--reason`. An `unreproducible` record gets the `--reason` its
status requires from the row: where it came from, and the row's Problem cell.
A `resolved` record keeps the finding's existing justification, as
`atcr debt resolve <id>` does.

Each outcome is appended through the same path `atcr debt resolve <id>` uses,
so reviewer attribution is carried the same way. Run the sync before
`llm-support td-clean`, which deletes `[x]` rows. Every row it does not record
is reported, and the run still exits 0:

- a row with no token (counted, not listed);
- a malformed token, or a row naming more than one id;
- an unknown marker, or an open or deferred one;
- an id the store does not hold;
- two rows that give one id different outcomes (neither is recorded);
- an id already closed as a different settled status;
- an id that was re-detected after this outcome was recorded. The row is
  stale, and re-closing it would close a regression on the strength of the
  old fix.

**Re-running appends nothing.** An id whose folded record already has the
row's status counts as `already recorded`.

`--report` prints each persona's counted terminal outcomes (`resolved`,
`wontfix`, `unreproducible`, `attempts-exhausted`, folded across models, with
persona names trimmed and lower-cased). It then prints `GATE MET` when at least
10 personas have 20 or more, the scorecard's `minConfirmationOutcomes` floor,
and `GATE NOT MET: n of 10 personas at 20` otherwise. `--report` cannot be
combined with `--td`.

### `atcr debt validate`

Checks every selected row against the working tree, read-only: nothing is
appended to the store and no file is changed. Grounding runs on every call; the
optional coherence pass is off unless `--coherence` is passed.

```bash
atcr debt validate                          # ground every open row
atcr debt validate --status deferred        # another status selection
atcr debt validate --coherence              # also score Fix against Problem
atcr debt validate --coherence --coherence-percentile 10
atcr debt validate --axi                    # TOON for agents (JSON is default)
```

Flags: `--store`, `--status` (exact, default `open`), `--root` (directory the
rows' file paths are relative to; default the repository root), `--coherence`,
`--coherence-percentile` (default `20`, 1–100), `--json`, `--axi`.

**Grounding** reports one of four statuses per row: `valid` (the file exists
and the line is inside it), `file_missing` (the file does not exist, or the
line is past its end — the `reason` field says which half failed),
`symbol_not_found` (the row's Problem names a symbol — a `(name) ` anchor or a
backticked identifier — that the file does not contain), and `no_file` (the row
carries no file). The symbol is checked only when the Problem text names one.

**Coherence** (`--coherence`) scores each row's Fix against its Problem by the
cosine of their embeddings from the `context_prefetch.semantic` embedding
endpoint — the same endpoint the semantic prefetch uses; no qdrant collection
is needed. The least coherent `--coherence-percentile` percent of scored rows
is listed under `flagged`. Every Problem and Fix passes the `secret_guard` scan
before it is sent: a blocked text skips the whole pass with reason
`secret-guard-blocked`, and a redacted one is sent redacted.

The coherence result is **`skipped`, never partially checked**: without
`--coherence` it reports `{status: skipped, reason: not-requested}`, and with
it, no configured endpoint, a guard block, or an endpoint failure at any point
all yield `skipped` with a class-only reason (no URL, no row text). Plain HTTP
is used only to a loopback endpoint unless `allow_insecure_http` permits
otherwise.

Flagged rows and failed groundings are advisory: the command always exits 0 on
a completed run (usage errors exit 2). Output is `{items, coherence}` as JSON
by default or a token-dense TOON payload with `--axi`.

### `atcr debt lock` and `unlock`

Marks an item as being worked on so another clone does not take it too. Each
lock is one annotated git ref, `refs/atcr/debt-locks/<hex-encoded id>`, pushed
to the remote of the git repository that holds the store. Pushing a ref that
already exists fails, so when two clones lock one item, exactly one wins.

```bash
atcr debt lock <id>...                  # one atomic push; if any id is held, none is taken
atcr debt lock <id> --holder alice      # record a holder other than the default
atcr debt unlock <id>...                # delete the refs; only for the holder recorded in each
```

Flags: `--store`, `--holder`. The holder is `--holder`, else
`$ATCR_DEBT_HOLDER`, else the OS user name alone (never `user@host`). **The
holder name is stored in a ref on the remote, and is readable by anyone who
can read that remote.**

A store outside a git repository, a repository with no remote, or a remote that
does not answer within 30 seconds is an error; there is no local fallback.
`lock` refuses an id the store does not hold and an id that is already locked;
`unlock` refuses an id that is not locked or is locked under another holder
(pass that name with `--holder` to release it). In every refusal nothing is
locked or released. Locks are advisory: `debt resolve` and `debt add` do not
check them.

## Ignoring false positives (`atcr ignore`)

`atcr debt resolve --status wontfix` closes a backlog item that is already in
the store. `atcr ignore` is the earlier lever: it stops a known false positive
from being **reported** at all, by every future `atcr reconcile` on this
checkout.

```bash
atcr ignore 8421025ce7cde4d3 --reason "sanitizer-bound test fixture, never shipped"
atcr ignore 8421025ce7cde4d3 --reason "..." --review 2026-10-20-abcdef
```

The command reads a review's `reconciled/findings.json` (the latest review by
default; `--review` picks another id or directory), finds the finding with the
given id, and appends one rule for it to `.atcr/ignore.yaml`. `--reason` is
required — say why the finding is a false positive. Re-running it on a finding
an identical rule already covers is a no-op that says so.

The derived rule never stores the finding's id or line: both drift with every
edit above the finding (the id hashes file, line and problem). It keys on the
finding's **file** and **category** plus a **match** regex-quoted from its
problem text, whose `(symbol) ` anchor keeps the rule on the same symbol after
the line moves.

The derived `file` is the finding's path with each glob metacharacter turned
into a one-character class, so a path such as `app/[id]/page.tsx` is written as
`app/[[]id]/page.tsx` and matches only itself, not `app/i/page.tsx`.

### The `.atcr/ignore.yaml` format

```yaml
rules:
  - file: internal/store/cache.go          # repo-relative path or glob; ** spans segments
    category: performance                  # optional; narrows the rule to one category
    match: Unbounded map grows without eviction  # optional RE2 regex on the problem text
    reason: bounded by the caller's pool   # required
  - package: internal/legacy               # alternative scope: one package directory
    reason: vendored code, frozen upstream
```

One document, one `rules:` list. Each rule carries:

| Field | Required | Meaning |
|-------|----------|---------|
| `reason` | always | Why the finding is a false positive. `report.md` lists it beside the suppressed count, so a rule nobody can justify cannot hide a finding silently. |
| `file` | one of `file`/`package` | Repo-relative, slash-separated path or glob. `*` never crosses a `/`; `**` matches zero or more whole segments. |
| `package` | one of `file`/`package` | A package directory (`.` for the repo root), matched exactly, not as a prefix. |
| `category` | no | Narrows the rule to one finding category, compared case-insensitively. |
| `match` | no | An RE2 regex tested unanchored against the finding's problem text. |

A rule covers a finding when **every** field it sets holds. Rules are matched in
file order; the first match wins. A finding with no file never matches — every
rule is scoped by file or package. Hand-written rules are welcome: `atcr ignore`
edits through the YAML node tree, so comments survive, and the write is atomic.

### Guardrails

The file is validated as a whole on every read — by `atcr ignore` before it
appends and by `atcr reconcile` before it applies the rules — and **any invalid
rule rejects the whole file** with an error naming the rule, rather than letting
a bad rule silently match too much or never match:

- `reason` is mandatory on every rule.
- Every rule needs a `file` or `package` scope; a rule cannot be repo-wide.
- A catch-all scope must be narrowed. A `file` glob made only of `*` and `**`
  segments (such as `**`, `*` or `**/*`), a `file` of `.`, or a `package` of `.`
  names no particular file, so a rule with one of these scopes is refused
  unless it also sets `match` or `category`. The file is read from the reviewed
  checkout, so without this one line in a pull request could drop every finding
  and leave the gate green.
- Paths must stay inside the repository: no absolute paths, no `..` segment, and
  backslashes are folded to slashes first so `..\x.go` is caught too.
- `match` must compile as RE2 (Go's `regexp`): a PCRE-only construct is an
  error, not a rule that never fires.
- Decoding is strict: a typo'd or unsupported key such as `line:` is an error.
  There is deliberately **no line field** — a line number drifts with every edit
  above the finding.
- `file` must parse as a glob: `atcr ignore` will not write a malformed one such
  as `a[.go`, and one written by hand fails the whole file at load, not
  silently at match. A backslash cannot escape a metacharacter (it is folded to
  a slash); write a literal `[`, `*` or `?` as `[[]`, `[*]` or `[?]`.

### What a match does

On the next `atcr reconcile`, a matched finding leaves the primary stream:
`reconciled/findings.json` and `findings.txt` no longer carry it, and it lands
in [`reconciled/suppressed.json`](findings-format.md#companion-artifact-suppressedjson)
with the rule recorded in its `suppressed_by` field. `report.md` ends with a
`## Suppressed Findings` footer counting them and listing each distinct reason.
When anything was suppressed, `atcr reconcile` (and the one-shot `atcr review`
gate) also prints `atcr: N finding(s) suppressed by .atcr/ignore.yaml rules` to
stderr, and `atcr github` adds the count to the check run's title and summary,
so a CI reader sees it beside the gate result. Nothing is printed when the
count is zero.
The scorecard still charges each suppressed finding to its reviewer's
`findings_raised` — a rule says the finding is not worth reporting, not that the
reviewer was wrong to raise it — but it is never corroborated and earns no
`weighted_credit` (see [scorecard.md](scorecard.md#ignore-suppressed-findings)).

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
    "schema_version": 4,
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
- Read only. A consumer never writes into the store.

## How findings enter the store

`atcr reconcile` appends each run's reconciled findings to the store as part of
the run (`--no-local-debt` opts out). The MCP `atcr_reconcile` tool persists
through the same code path, so a review driven by an editor or agent lands in the
same backlog a CLI review does.

The store's location resolves by **explicit `--repo-root` > the review manifest >
the current working directory** (`--repo` remains a deprecated alias). Below the
repository root, the store directory is the one `debt.store` resolves, or
`<repo>/.atcr/debt` when it is unset (see [Where the store lives](#where-the-store-lives)).
The repo root recorded in the manifest at review
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
