# Review Status (`atcr status`)

`atcr status [id-or-path]` prints one review's progress as indented JSON. With
no argument it reads the review `.atcr/latest` points at. The `atcr_status` MCP
tool returns the same document, from the same reader.

After the JSON, the command prints the usual next steps on stderr, so stdout
stays one JSON document:

```
Next: atcr report <id>
Next: atcr review --resume <id>
```

The `review --resume` line appears only while `agents_pending` is above zero.
`<id>` is a placeholder for the review id. The MCP tool does not add these lines.

It reads only files the review has already written, so it never claims more
than is on disk: a persona still working shows as `pending`, and a stage that is
running shows as the stage before it. It can be polled at any interval while a
review runs; every file it reads is written whole (temp file, then rename), so a
poll never sees a half-written one.

---

## Example

A `--all` review with three personas, part way through its fan-out:

```json
{
  "review_id": "2026-10-22_143005_ab12",
  "status": "in_progress",
  "agent_count": 3,
  "agents_done": 1,
  "agents_pending": 2,
  "partial": false,
  "current_stage": "fanout",
  "completed_agents": [
    "kai"
  ],
  "agents": [
    {
      "name": "kai",
      "state": "done",
      "chunks_done": 1,
      "chunks_total": 1,
      "last_activity_at": "2026-10-22T14:31:12Z"
    },
    {
      "name": "greta",
      "state": "pending",
      "chunks_done": 1,
      "chunks_total": 3,
      "last_activity_at": "2026-10-22T14:33:40Z"
    },
    {
      "name": "bruce",
      "state": "pending",
      "chunks_done": 0,
      "chunks_total": 1
    }
  ]
}
```

## Fields

| Field | Type | Meaning |
|-------|------|---------|
| `review_id` | string | The review's id (its folder name under `.atcr/reviews/`). |
| `status` | string | `in_progress` (the fan-out has not written `sources/pool/summary.json`), `completed` (at least one persona succeeded), `failed` (every persona failed), `interrupted` (the run was stopped by a signal) or `stale` (see [Stale](#stale)). |
| `agent_count` | int | Personas in the roster. |
| `agents_done` | int | Personas that have finished, whatever the outcome. While the fan-out runs, this is the number of personas with a valid `status.json`, so it rises as each one finishes; once `summary.json` exists it is that file's final tally. |
| `agents_pending` | int | `agent_count - agents_done`, never below 0. |
| `partial` | bool | The run's results are incomplete (an agent failed or the pool write stopped partway). Read from `summary.json`; `false` while the fan-out runs. |
| `current_stage` | string | The latest stage that has **finished** (see [Stages](#stages)). Absent only in output from an `atcr` older than this field. |
| `completed_agents` | string array | The roster personas with a valid `status.json`, sorted by name, whatever outcome that file records (`ok`, `failed` or `timeout`). Absent while no persona has finished. |
| `agents` | object array | One row per roster persona, in roster order (see [Agent rows](#agent-rows)). Absent when the roster is empty. |

`current_stage`, `completed_agents` and `agents` were added after the first six
fields, and each is left out of the JSON when it is empty. The first six keep
their names, types and meaning, and read the same as before once a run ends.

### Stages

`current_stage` never says a stage is running: `atcr reconcile`, `atcr verify`
and `atcr debate` are separate commands, and nothing records that one has
started. It names the latest stage whose output is on disk.

| Value | When |
|-------|------|
| `fanout` | `status` is `in_progress`: the reviewers are still running. |
| `reviewed` | The fan-out finished (`summary.json` exists) and nothing later has. |
| `reconciled` | `reconciled/findings.json` exists. |
| `verified` | `reconciled/verification.json` exists, or the manifest's `stages` lists `verify`. |
| `debated` | `reconciled/debate.json` exists, or the manifest's `stages` lists `debate`. |
| `failed`, `interrupted`, `stale` | The run ended that way; `current_stage` repeats `status`. |

When several markers are present the highest wins: a review that was debated
but never verified reads `debated`. A marker counts only as a regular file
inside the review folder; a directory or a symlink pointing outside it is
ignored.

### Agent rows

| Field | Type | Meaning |
|-------|------|---------|
| `name` | string | The persona, as named in the roster. |
| `state` | string | `pending`, `done`, `failed` or `timeout`, from the persona's `status.json`; `pending` until that file exists. There is no `running` state: which persona a worker is serving is held only in memory, so a `pending` persona may already be mid-run. |
| `chunks_done` | int | Bins that have finished (`ok`, `failed` or `timeout`) in the persona's `chunks.json`. |
| `chunks_total` | int | Bins the persona planned, from its `chunks.json`. A persona with no `chunks.json` (a diff or single-chunk review) reads as one chunk: `0` of `1` until its `status.json` lands, then `1` of `1`. |
| `last_activity_at` | string | The newer write time of the persona's `status.json` and `chunks.json`, in RFC 3339 UTC (`2026-10-22T14:33:40Z`). Absent while neither file exists. A write time up to 60 seconds in the future reads as now; one further ahead (a skewed clock, or files restored with their old timestamps) is ignored, so the field is absent if that is the only write. |

A `status.json` or `chunks.json` that is corrupt, too large, not a regular file
or outside the review folder (for example a symlink to somewhere else) is
skipped, and the persona reads as if that file did not exist. It is never an
error for the whole read.

A bin the old 64-bin ceiling `deferred` is never sent, so a persona carrying one
can finish with `chunks_done` below `chunks_total`.

### Stale

A review with no `summary.json` reads `stale` once the manifest's `started_at`
plus its `timeout_secs` plus 60 seconds has passed. A review whose manifest
records neither reads `in_progress` until `summary.json` lands. `stale` means
the fan-out is presumed dead; `atcr review --resume` continues it.
`timeout_secs` is the deadline of one bin, not of the whole run, so a long
multi-bin `--all` / `--scope` review can pass this point while its bins are
still finishing; check whether `last_activity_at` is still moving before
resuming one.
A file write time more than 60 seconds in the future does not count as
activity, so it cannot hold a dead review at `in_progress`.

## Spotting a stalled review

`last_activity_at` lets a poller notice a review that has stopped making
progress without asking a model. **The rule:** a review is stalled when its
`status` is `in_progress` and the newest `last_activity_at` across all `agents`
rows (the manifest's `started_at` while no row has one) is older than the stall
threshold.

Pick the threshold with these facts in mind, or it will flag a healthy review:

- **Nothing is written while a bin is in flight.** A persona's `chunks.json` is
  written when its bins are planned and when a bin finishes; its `status.json`
  when the persona finishes. A persona with no `chunks.json` (a diff review) has
  no `last_activity_at` at all until it finishes.
- **One bin may legitimately run for a long time.** Its deadline is the
  persona's `timeout_secs` (recorded in `manifest.json` as `timeout_secs`; an
  agent's own `timeout_secs` in the registry overrides it), multiplied by the
  persona's chunk count up to 8 times. A failed call tries the persona's backup
  models, each with its own deadline, and a rate-limited or failed bin can wait
  up to 10 minutes in all before it is sent again.
- **A pending persona may simply be queued.** Only `max_parallel` bins run at
  once, so a persona can wait behind others with no activity of its own. Judge
  the review by its newest activity, not one persona's.

So the threshold must be **above `timeout_secs`**, and above the scaled deadline
for a chunked persona, plus 10 minutes for retries. A threshold below
`timeout_secs` will report a slow but healthy bin as stalled.

A stall is a hint for a person to look, not proof: atcr itself never acts on it.
To find which persona is stuck, look at the rows whose `state` is `pending`.
