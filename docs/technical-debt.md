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
| `status` | string | Empty (open), `deferred`, `resolved`, `wontfix`, `unreproducible`, or `attempts-exhausted` |
| `resolved_at` | string | Timestamp of the terminal record |
| `origin` | string | **v3** — `review` or `manual` |
| `occurrences` | int | **v3** — times this id has been seen; carried through compaction |
| `counted_through` | string | **v3** — newest sighting `occurrences` accounts for; what makes the count idempotent under repeated compaction |
| `first_seen` | string | **v3** — timestamp of the id's first append; carried through compaction |
| `confidence_discount` | string | **v3** — why reconcile lowered `confidence` below what the reviewer count earns (today only the panel-wide-instruction reason); absent when it did not. `atcr debt dashboard` appends it to the Top Priority problem cell as `(confidence discounted: …)` |
| `reconciler_feedback` | object | **v3** — a verdict on how reconcile grouped the finding, not on the finding: `{"kind": …, "with": …, "reason": …}`, where `kind` is `wrong-merge`, `wrong-split` or `wrong-category` and `with` (only on `wrong-split`, optional) is the partner finding's id. It never changes `status`, and a record carrying it is not counted as a sighting in `occurrences`; absent on every unannotated record |

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
| `atcr debt dashboard` | Render an aggregated Markdown dashboard |
| `atcr debt resolve` | List open items and record resolutions |
| `atcr debt recluster` | Record feedback on how reconcile grouped a finding |
| `atcr debt compact` | Fold the append-only store to its live records |
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
the [record schema](#record-schema-v3)). It deliberately changes nothing else:
the finding's `status` stays as it is, the annotation is not counted as a
sighting in `occurrences`, and it does not move the `wontfix` count — close or
dismiss the finding itself with `atcr debt resolve`. The separation matters
because a `wontfix` is a verdict on the reviewer (ground truth for the quality
signal), while a recluster annotation is a verdict on the reconciler's grouping.
The dashboard reports these as **Reconciler corrections**, apart from
`Wontfix`, and the opt-in quality signal counts them on their own keys (see
[docs/telemetry.md](telemetry.md#quality-signal-payload-schema)).

An unknown id, an unknown `--kind`, a `--with` on a kind that takes no partner,
or an empty `--reason` is a usage error (exit 2).

### `atcr debt compact`

Folds the append-only store to one effective record per id and rewrites the
shards atomically, carrying `occurrences` and `first_seen` forward so the
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

A final clause, `N summary.json over 1 MiB ignored (only those checked for a policy refusal)`, counts the
`reconciled/summary.json` files the pass ignored for exceeding the size cap —
summary files, not records, so it stands apart from the record counters above. A
review dir whose summary is ignored owns nothing, so a refused `review.md` in it
counts under `policy refusal not tied to the record`, not `declined by policy`. The
pass reads a dir's summary only when a refused `review.md` in that dir made it ask
who owns the record, so `0` here does not prove no oversized summary exists — only
that none was asked about (each skip is also logged at debug level, hidden by
default).

Within a repaired id, only the **lines** still carrying the stale excerpt are
written. One id can hold several lines — `debt resolve` appends a copy of the
effective record, and a later re-detection displaces it — and a resolution line's
justification may be an operator's `--reason`. Matching on the stored text leaves
that line alone. The run reports both counts, e.g. `3 rewritten (5 lines)`, and
`--dry-run` prints the before and after of every line it would touch:

```console
$ atcr debt backfill-justifications --dry-run
dry run: 1 scanned, 1 rewritten (1 line), 0 unchanged, 0 unresolved (0 review tree pruned, 0 declined by policy, 0 policy refusal not tied to the record), 0 ambiguous (candidates disagreed), 0 skipped (carries a rationale: resolved, wontfix, unreproducible or attempts-exhausted), 0 summary.json over 1 MiB ignored (only those checked for a policy refusal)
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
- Paths must stay inside the repository: no absolute paths, no `..` segment, and
  backslashes are folded to slashes first so `..\x.go` is caught too.
- `match` must compile as RE2 (Go's `regexp`): a PCRE-only construct is an
  error, not a rule that never fires.
- Decoding is strict: a typo'd or unsupported key such as `line:` is an error.
  There is deliberately **no line field** — a line number drifts with every edit
  above the finding.
- An invalid `file` glob fails reconcile loudly at load, not silently at match.

### What a match does

On the next `atcr reconcile`, a matched finding leaves the primary stream:
`reconciled/findings.json` and `findings.txt` no longer carry it, and it lands
in [`reconciled/suppressed.json`](findings-format.md#companion-artifact-suppressedjson)
with the rule recorded in its `suppressed_by` field. `report.md` ends with a
`## Suppressed Findings` footer counting them and listing each distinct reason.
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
