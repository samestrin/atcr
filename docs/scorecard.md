# Scorecard

`atcr` emits a normalized per-reviewer evaluation record alongside every
`atcr reconcile` run, accumulates those records into a local monthly store, and
exposes them through two read-only commands: `atcr scorecard` (one run) and
`atcr leaderboard` (aggregated across runs, with an anonymized public export).

This is the monitoring foundation for the review pipeline — it answers "is review
quality improving over time?" and "for my codebase, which model finds the most
real bugs at what cost?" — and it produces the versioned record schema that feeds
the public Model-Eval Leaderboard (Epic 10.0).

The scorecard is written silently as a byproduct of reconcile. No flag is needed
to enable it; pass `--no-scorecard` to suppress it for a single run.

---

## Record Schema (v1)

Each reconcile run appends one **reviewer** record per participating reviewer plus
one **aggregate** record summarizing the whole run. Records are JSON objects, one
per line (JSONL). `schema_version` is `1` on every record; a future schema change
increments it and leaves old records readable (see [Schema versioning](#schema-versioning)).

### Example (per-reviewer record)

```json
{
  "schema_version": 1,
  "record_type": "reviewer",
  "run_id": "2026-06-14T10:00:00Z-abc123",
  "reviewer": "bruce",
  "model": "claude-sonnet-4-6",
  "role": "reviewer",
  "findings_raised": 12,
  "findings_corroborated": 6,
  "findings_solo": 6,
  "corroboration_rate": 0.5,
  "cost_usd": 0.04,
  "tokens_in": 14200,
  "tokens_out": 4000,
  "latency_ms": 9100,
  "findings_verified": 4,
  "findings_refuted": 1,
  "survived_skeptic_rate": 0.8
}
```

### Field reference

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `schema_version` | int | always | Record schema version. Currently `1`. |
| `record_type` | string | always | `"reviewer"` for a per-reviewer row, `"aggregate"` for the run summary. Aggregate rows leave `reviewer`/`model`/`role` empty; consumers key on `record_type`. |
| `run_id` | string | always | `<RFC3339 reconciled_at>-<review-dir base>`, e.g. `2026-06-14T10:00:00Z-abc123`. Uniquely identifies the run and selects the month file. |
| `reviewer` | string | always (empty on aggregate) | Reviewer/persona name (e.g. `bruce`). |
| `model` | string | always (empty on aggregate) | Model id the reviewer ran on (e.g. `claude-sonnet-4-6`). |
| `role` | string | always (empty on aggregate) | Pipeline role. Constant `"reviewer"` for reconcile-derived records. |
| `findings_raised` | int | always | Findings this reviewer raised. |
| `findings_corroborated` | int | always | Of those, how many were corroborated (the finding carried 2+ distinct reviewers). |
| `findings_solo` | int | always | `findings_raised - findings_corroborated`. |
| `corroboration_rate` | float | always | `findings_corroborated / findings_raised` (0.0 when none raised; never NaN). |
| `cost_usd` | float | always | Estimated cost from the per-model rate table (see [Cost is approximate](#cost-is-approximate)). |
| `tokens_in` | int | always | Prompt tokens consumed (summed across turns for tool-using agents). |
| `tokens_out` | int | always | Completion tokens produced. |
| `latency_ms` | int | always | Reviewer wall-clock latency in milliseconds. |
| `findings_verified` | int | conditional | Findings confirmed by the skeptic stage. Present only when verification data drove the run. |
| `findings_refuted` | int | conditional | Findings refuted by the skeptic stage. Conditional, same as above. |
| `survived_skeptic_rate` | float | conditional | `findings_verified / (findings_verified + findings_refuted)`. Conditional, same as above. |

**Conditional verification fields.** `findings_verified`, `findings_refuted`, and
`survived_skeptic_rate` are included only when the run had a readable, well-formed
`reconciled/verification.json` (i.e. `atcr verify` ran). When verification is
absent, these three keys are **omitted entirely** from the record, and the
`atcr scorecard` / `atcr leaderboard` tables omit the corresponding columns. An
absent, unreadable, or malformed verification file degrades gracefully to "no
verification" — it never fails the run.

**Aggregate record.** The aggregate row sums `findings_*`, `cost_usd`, and token
counts across reviewers, takes the slowest reviewer's latency as the run latency
(reviewers run in parallel), and computes `corroboration_rate` /
`survived_skeptic_rate` from the run totals (not by averaging per-reviewer rates).

---

## Storage

Records are stored locally in the user config directory:

```
~/.config/atcr/scorecard/YYYY-MM.jsonl
```

(`~/.config` is `os.UserConfigDir()` — the platform equivalent applies on macOS
and Windows.)

- **Monthly rotation.** One file per calendar month, named from the run_id's
  `YYYY-MM` prefix (e.g. `2026-06.jsonl`). A run whose timestamp straddles a month
  boundary is still read back whole — `atcr scorecard` scans the neighbouring
  month file when needed.
- **Append-only.** Each run appends its records; existing lines are never
  rewritten. Concurrent reconcile runs append safely without tearing lines.
- **Permissions.** The file is created `0600` (user read/write only) and the
  directory `0700`. The directory is created lazily on the first write — a
  suppressed run (`--no-scorecard`) creates nothing.
- **Size.** Records are ~500 bytes each; even 1000 runs/month is well under 1 MB.
- **Maintenance.** To reclaim space or reset history, delete old `YYYY-MM.jsonl`
  files (or the whole directory) by hand — nothing else references them.

> **Do not commit this directory.** `~/.config/atcr/scorecard/` is local
> monitoring data. It is outside the repository by design; never add it to git or
> share it as-is. To share data publicly, use the anonymized
> [`--export`](#public-export) path instead.

### Cost is approximate

`cost_usd` is computed at read/emit time from a hardcoded per-model rate table
(`internal/llmclient/rates.go`), so a later rate correction retroactively
re-prices historical records. Rates are approximate and can drift; an unknown
model id yields `0`. Treat `cost_usd` as a ballpark, not an invoice.

---

## CLI Usage

### `atcr scorecard [id-or-path]`

Display the per-reviewer table for a single run. The argument is either a
`run_id` or the path to the review directory that produced the run (resolved to
its run_id via `reconciled/summary.json`).

```bash
# By run_id
atcr scorecard 2026-06-14T10:00:00Z-abc123

# By review directory path
atcr scorecard ./.atcr/runs/abc123
```

Columns: `REVIEWER  MODEL  RAISED  CORROBORATED  SOLO  CORR%  COST  LATENCY`,
plus `VERIFIED  REFUTED  SURV%` when any record carries verification data.

Behavior:
- No records for the run → message + exit `1`.
- Malformed JSONL lines → skipped with a stderr warning; valid records still render.
- A bare argument that is neither a valid run_id nor a path → usage error (exit `2`).
- A path argument with no `reconciled/summary.json` → usage error (exit `2`,
  "run reconcile first").
- A path whose `reconciled/summary.json` is present but unreadable or corrupt →
  failure (exit `1`).

### `atcr leaderboard`

Aggregate the stored records across runs, grouped by `(reviewer, model)` and
ranked by corroboration rate (descending). Read-only. Filters compose with AND.

```bash
# Default: last 30 days
atcr leaderboard

# Windowed + filtered
atcr leaderboard --since 7d --model claude-sonnet-4-6 --persona bruce
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--since` | `30d` | Time window. `Nd` (days), `Nw` (weeks), `Nm` (30-day months). `N` is a positive integer. |
| `--model` | _(all)_ | Exact-match model id filter. |
| `--persona` | _(all)_ | Exact-match reviewer/persona filter. |
| `--export` | off | Emit anonymized public JSON instead of the table (see below). |
| `--output` | _(stdout)_ | With `--export`: write JSON to this file (`0600`) instead of stdout. |

Columns: `REVIEWER  MODEL  RUNS  RAISED  CORROBORATED  CORR%  COST  COST/CORR  LATENCY`.
`COST/CORR` renders as `-` for a group with zero corroborated findings.

Behavior:
- Empty store (no data at all) → friendly message, exit `0`.
- Data exists but nothing matches the filters → message naming the active window,
  exit `1`.
- Invalid `--since` value → actionable error, exit `1`.
- `--output` without `--export` → usage error (exit `2`, "--output requires
  --export"); `--output` only routes the export document.

### `atcr leaderboard --export [--output path]`

Emit the versioned, anonymized public submission document (the Epic 10.0
format). Filters apply **before** anonymization. Output is deterministic
(byte-identical for the same input + export time).

```bash
# Anonymized JSON to stdout (pipe to jq, a file, etc.)
atcr leaderboard --export

# Anonymized JSON to a file
atcr leaderboard --export --output /tmp/submission.json

# Export a filtered slice
atcr leaderboard --export --since 30d --model claude-sonnet-4-6
```

The document is the Epic 10.0 submission envelope. It is an **aggregate over the
selected slice** — the active filters are applied but are deliberately **not
echoed** (they would leak query parameters about your local dataset):

```json
{
  "submission_schema": 1,
  "atcr_version": "0.0.0",
  "submitted_at": "2026-06-15T00:00:00Z",
  "reviewers": [
    {
      "model": "claude-sonnet-4-6",
      "persona": "bruce",
      "runs": 8,
      "findings_raised_avg": 12.0,
      "corroboration_rate": 0.5625,
      "survived_skeptic_rate": 0.8333,
      "cost_per_corroborated_finding_usd": 0.0059,
      "latency_p50_ms": 9100
    }
  ]
}
```

| Envelope field | Type | Description |
|----------------|------|-------------|
| `submission_schema` | int | Public submission schema version (currently `1`). Decoupled from the on-disk store's `schema_version`: the local record format and the public submission format version independently. |
| `atcr_version` | string | The ATCR build that produced the submission (`internal/version`; `0.0.0` in dev builds, stamped via ldflags for releases). |
| `submitted_at` | string | RFC3339 export timestamp (also the `--since` window anchor, so output is reproducible). |
| `reviewers` | array | One aggregated row per `(persona, model)`. |

| Reviewer field | Type | Presence | Description |
|----------------|------|----------|-------------|
| `model` | string | always | Model id (scrubbed). |
| `persona` | string | always | Reviewer/persona name (scrubbed; not PII). |
| `runs` | int | always | Number of runs aggregated into this row. |
| `findings_raised_avg` | float | always | Mean findings raised **per run** (not the total). |
| `corroboration_rate` | float | always | Corroborated / raised across the group (clamped to `[0,1]`). |
| `survived_skeptic_rate` | float | **omitempty** | Verified / (verified + refuted). **Omitted entirely** when no verification ran for the group; present as `0.0` only when verification ran and every finding was refuted. The omission is the disambiguator. |
| `cost_per_corroborated_finding_usd` | float | **omitempty** | Total cost ÷ corroborated findings. **Omitted entirely** when there are zero corroborated findings (the metric is undefined — this is what distinguishes a paid-but-ineffective reviewer from a genuinely free one); present as `0.0` only when corroborated findings exist AND the reviewer's cost was genuinely zero. Never Inf/NaN when present. |
| `latency_p50_ms` | int | always | Median (p50) of per-run latencies — not the mean. |

Reviewers are aggregated by `(persona, model)` (role is dropped from the public
schema — it is a constant `"reviewer"` for reconcile records), sorted ascending by
`(model, persona)`. Output is deterministic (byte-identical for the same input +
`submitted_at`). A no-match/empty result writes the canonical guidance to stderr
and exits `1` (so a `--export | jq` pipeline never sees non-JSON on stdout).

> **Suite vs production submissions.** `leaderboard --export` produces a
> *production* submission from your local runs. The public board accepts only
> *suite* submissions (`atcr benchmark export`, tagged `source: "benchmark-suite"`)
> so cherry-picked production runs cannot game it — see
> [`docs/benchmark.md`](benchmark.md).

### `atcr reconcile --no-scorecard`

Suppress scorecard emission for a single reconcile run.

```bash
atcr reconcile --no-scorecard
```

The suppression gate is the first thing checked: with `--no-scorecard`, no
directory is created and no file is opened — truly zero scorecard I/O. The flag
has no effect on reconcile's exit code, stdout, or stderr. Without it, reconcile
writes records by default.

> Scorecard emission fires from both the CLI (`atcr reconcile`) and the MCP
> `atcr_reconcile` handler, via a single shared bridge — the two entry points
> emit identical records, so MCP-driven runs are never silently omitted from the
> store. `--no-scorecard` suppression is a CLI flag.

---

## Privacy Model

The local store (`~/.config/atcr/scorecard/`) holds your real `run_id` and may
carry your reviewer/model names — it is local and never shared. **Only the
`--export` path produces a shareable document, and it is anonymized.**

`--export` is **allowlist-based**: the public submission carries only the fields
listed below. A field that is not on the allowlist cannot leak, because it is
never copied into the public structure in the first place. The Epic 10.0 schema
deliberately **shrank** the allowlist relative to the local store — the smaller
the surface, the less can leak.

**Preserved (allowlist):**

- Envelope: `submission_schema`, `atcr_version`, `submitted_at`
- Per reviewer: `model`, `persona`, `runs`, `findings_raised_avg`,
  `corroboration_rate`, `survived_skeptic_rate` (omitted when no verification ran),
  `cost_per_corroborated_finding_usd` (omitted when zero corroborated findings),
  `latency_p50_ms`

**Stripped / never exported:**

- `run_id`
- The active filters (`since`/`model`/`persona`) — applied to select the slice,
  but **not echoed**, so a submission does not reveal your query parameters.
- The local-store internals: `findings_corroborated`, `findings_solo`,
  `findings_verified`, `findings_refuted`, `cost_usd` (raw total), `tokens_in`,
  `tokens_out`, `latency_ms` (raw per-run), `role`, `index`. Only the derived
  public metrics above are emitted.
- Filesystem paths (absolute, Windows `C:\…`, and `~`-relative — including
  path-like substrings glued into a field)
- Email addresses
- Provider API keys / tokens (`sk-…`, `Bearer …`, GitHub `ghp_`/`gho_`/…,
  GitLab `glpat-…`, Slack `xox*-…`, AWS `AKIA…`, and `api_key=`/`token=`/
  `Authorization:` assignment forms)
- Repository content, hostnames, usernames, and organization names — none are
  collected into a record in the first place

As defense-in-depth, the two string fields that _are_ exported (`persona`,
`model`) additionally pass through a scrubber that removes any path-like, email,
or credential-like substring before emission. The allowlist is the primary
guarantee; the scrubber is the backstop. Export output is deterministic, so you
can diff it before sharing.

> **Accuracy is a contract.** The privacy model above must match
> `internal/scorecard/export.go`. Any discrepancy is treated as a documentation
> bug — fix the doc (or the code) so they agree.

### Telemetry & Cloud Sync

The `--export` allowlist above applies **only** to the local-store leaderboard
export. `atcr` has two other, **separate and additive** data paths, each with its
own schema — neither weakens, replaces, or is governed by the `--export`
guarantee above:

- The **anonymous usage ping** — a background, fail-open `{event, lang, lines,
  status}` event emitted on `review`/`reconcile` completion, on by default and
  disabled from either of two OR'd opt-out surfaces.
- The **`--sync-cloud` push** — an explicit, opt-in upload of an anonymized
  scorecard payload (a hashed Persona ID plus raw run metrics), authenticated
  with `ATCR_API_KEY`, that you request per run.

These use a different schema from the `--export` record and are documented in
full — including the exact fields, the opt-out mechanics, the Persona ID hashing
guarantee, and the auth exit code — in **[docs/telemetry.md](telemetry.md)**.

---

## Schema versioning

There are **two independent version numbers**:

- `schema_version` (`1`) is stamped on every **stored** record (the local JSONL
  store).
- `submission_schema` (`1`) is stamped on every **public submission** envelope
  (`leaderboard --export` and `benchmark export`).

They are decoupled on purpose: the local store format and the public submission
format evolve separately, so bumping one never silently changes the other. When a
future epic changes either schema:

- That version is incremented independently.
- Old stored records remain readable — the reader tolerates earlier versions, and
  unknown/absent optional fields degrade gracefully.
- Version negotiation for the public submission format is handled by the export
  paths, not by individual stored records.

---

## Reference Implementation

Every scorecard record is derived from a reconcile run, and the deterministic
reconciler that produces those runs is published as a standalone, inspectable Go
module: **`github.com/samestrin/atcr/reconcile`**. This is the reference
implementation backing every scorecard and leaderboard record — the clustering,
text-similarity dedupe, confidence scoring, and disagreement-preserving merge that
turn multiple reviewers' findings into one reconciled result. Anyone can `go get`
the module, read its source, and run its tests to reproduce and verify the merge
behavior independently of the full ATCR pipeline.

The module is intentionally narrow: it is the deterministic reconciler only
(clustering, dedupe, merge, confidence, ambiguity), not ATCR's path-validation,
file I/O, or review-orchestration machinery — those stay ATCR-internal. The
library is stdlib-only with no third-party dependencies, which is what makes it
embeddable and independently auditable.

During extraction the module lives at `./reconcile/` inside this repository and is
consumed by ATCR through a root `go.mod` `replace` directive — the documented
development-time bridge. `github.com/samestrin/atcr/reconcile` is the intended
public import path; separate-repository publication follows the extraction.

---

## Related

- [`docs/benchmark.md`](benchmark.md) — the standard benchmark-suite tooling
  (`atcr benchmark verify` / `export`), the suite-manifest contract, and the
  suite-tagged submission format that feeds the public board.
- [`github.com/samestrin/atcr/reconcile`](../reconcile/README.md) — the standalone
  deterministic reconciler module that is the reference implementation backing
  every scorecard record (run and inspect it independently).
- [`docs/verification.md`](verification.md) — the skeptic stage that produces the
  conditional `findings_verified` / `findings_refuted` / `survived_skeptic_rate`
  fields.
- [`docs/findings-format.md`](findings-format.md) — the findings the corroboration
  metrics are computed from.
