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
| `findings_raised` | int | always | Findings this reviewer raised. Includes findings the Tier 4 content check routed out of the primary stream into `unresolved.json` — they are findings the reviewer raised, so they belong in the denominator, and leaving them out would let a reviewer that produced phantoms score as if it had not. They are therefore NOT all present in `findings.json`. |
| `findings_corroborated` | int | always | Of those, how many were corroborated (the finding carried 2+ distinct reviewers). A Tier-4-routed finding is NEVER corroborated, however many reviewers named it: agreement on a construct that is declared nowhere in the tracked tree is not corroboration. |
| `findings_solo` | int | always | `findings_raised - findings_corroborated` — the arithmetic remainder, not "findings nobody else raised". Because a Tier-4-routed finding is never corroborated, two reviewers that independently named the same phantom each count it here. |
| `corroboration_rate` | float | always | `findings_corroborated / findings_raised` (0.0 when none raised; never NaN). |
| `cost_usd` | float | always | Estimated cost from the per-model rate table (see [Cost is approximate](#cost-is-approximate)). |
| `tokens_in` | int | always | Prompt tokens consumed (summed across turns for tool-using agents). |
| `tokens_out` | int | always | Completion tokens produced. |
| `latency_ms` | int | always | Reviewer wall-clock latency in milliseconds. |
| `findings_verified` | int | conditional | Findings confirmed by the skeptic stage. Present only when verification data drove the run. |
| `findings_refuted` | int | conditional | Findings refuted by the skeptic stage. Conditional, same as above. |
| `survived_skeptic_rate` | float | conditional | `findings_verified / (findings_verified + findings_refuted)`. Conditional, same as above. |
| `raised_includes_unresolved` | bool | conditional | `true` when `findings_raised` counts the Tier-4-routed findings (every record written from Epic 35.16.6.5 onward). Omitted on records written before it, whose denominator excluded them. `TrustPriors` uses it to avoid averaging the two definitions together — see the caution below. |

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
atcr scorecard ./.atcr/reviews/abc123
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
| `--model` | _(all)_ | Model id filter (case-insensitive substring, matching `personas search --model`; a full exact id always matches). |
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

**Persona digest canonical form.** `scorecard.HashPersonaID` — the function
behind every `persona_id_hash` — canonicalizes its input to
`strings.ToLower(strings.TrimSpace(name))` **before** hashing, so `bruce`,
`Bruce`, and `" bruce "` yield one digest rather than three. A consumer that
needs to reproduce a digest from a known persona name (for example, to map
digests received on the telemetry / --sync-cloud path back to the published persona catalog) must apply the same
transform; for an **ASCII** name — which every catalog persona is — the JS
equivalent is
`crypto.createHash("sha256").update(name.trim().toLowerCase()).digest("hex")`.
That equivalence holds for ASCII only: Go and JS disagree on some non-ASCII
inputs (final sigma, dotted capital I, a leading BOM), which makes such a name a
lookup **miss** rather than a second identity — a stored persona identity MUST
always be the digest received from the Go client, and a JS-computed digest is a
match key only that must never be written as a storage key. See telemetry.md for
the divergence table.
The canonical form and the reasoning behind it are specified in full under
[telemetry.md](telemetry.md#persona-leaderboard-data); the pinned digests for real
catalog personas live in `internal/scorecard/telemetry_test.go`
(`TestHashPersonaID_PinnedPublishedPersonaDigests`).

### `atcr leaderboard --export [--output path]`

Emit the versioned, anonymized public submission document (the Epic 10.0
format). Filters apply **before** anonymization. Output is deterministic
(byte-identical for the same input + export time).

**What "anonymized" means here:** `--export` anonymizes the *submitter* — run
IDs, cost, token counts, and local paths are scrubbed or omitted — while
deliberately keeping `(persona, model)` as the public leaderboard's aggregation
dimensions (a persona is a public catalog identity, not a user identity). This
is a different sense of the word from the telemetry surfaces
([telemetry.md](telemetry.md)), where a persona identity is never transmitted
raw and travels only as a pseudonymous `persona_id_hash`. Both usages are
deliberate; they are not the same guarantee.

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
  "submission_schema": 2,
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
| `submission_schema` | int | Public submission schema version (currently `2`). Decoupled from the on-disk store's `schema_version`: the local record format and the public submission format version independently. |
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

**Unpublishable identity → error, exit `1`.** A record whose `model` or `reviewer`
carries a control (Cc) or format (Cf) rune is rejected before anything is written.
The scrub is not a defense here — it provably leaves both categories alone — so an
invisible rune (U+00AD, U+200B) or a bidi override (U+202E) would otherwise survive
into the published envelope and misattribute a row to a model that was never
measured. `atcr benchmark export` applies the same control/format-rune rule to its own
envelope, so no invisible rune reaches the board from either producer.

**Identity that is empty once scrubbed → row skipped, export continues.** A record
whose `model` or `reviewer` is non-empty in the store but scrubs away to nothing
(`"admin@internal.host"` loses its email-shaped token whole; so do
`"/models/mistral-7b.gguf"`, `"bedrock@us-east-1/claude"` and `"~/models/foo"`) is
**dropped from the envelope** and named on stderr — it is never published as
`model: ""`. It does not fail the export, because this shape occurs in ordinary
history and `--export` reads the whole unrotated store, so one such record would
otherwise take down an export that had succeeded the day before, clearable only by
hand-editing a JSONL store. If every selected record is dropped, the ordinary
no-records error is raised instead of an empty envelope.

`benchmark export` applies the same rule as a **hard rejection** rather than a skip.
The asymmetry is deliberate: it validates one just-produced run-result file, where
the cost of failing is re-running that one command, and there is no unrelated history
in the document to preserve.

The check runs on the records your **filters actually select**, not on the whole
store, so a stale record excluded by `--since` or `--model` cannot fail an export
whose envelope was clean. Both the error and the skip report name the offending
record's `run_id` and quote the offending value, so the row can be located and
repaired in the store.

> **Suite vs production submissions.** `leaderboard --export` produces a
> *production* submission from your local runs. The public board accepts only
> *suite* submissions (`atcr benchmark export`, tagged `source: "benchmark-suite"`)
> so cherry-picked production runs cannot game it — see
> [`docs/benchmark.md`](benchmark.md).

### `scorecard.TrustPriors` (Go API)

`TrustPriors(dir string, minRuns int) (map[string]float64, error)` is the shared
per-reviewer corroboration-rate resolver behind `atcr personas list --scores` —
one aggregation instead of each consumer rolling its own. It reads the store at
`dir` (same store `atcr leaderboard` reads), sums `Runs`, `findings_corroborated`,
and `findings_raised` across a reviewer's `(reviewer, model)` leaderboard rows (a
reviewer that ran under several models is one entry, not one per model), and
returns the recomputed rate keyed by **lowercase reviewer name**. It is designed
to back a second consumer — the skeptic-selection score map at
`internal/verify/select.go` — but that wiring is not yet done (`pipeline.go`
still passes `nil`); a future epic wires it, reusing this same resolver rather
than growing a third aggregation.

- **Absence means "no history", not "zero trust".** A reviewer whose summed
  `Runs` is below `minRuns` is omitted from the map entirely rather than present
  at `0.0` — callers can tell "never measured" apart from "measured and found
  untrustworthy". `minRuns <= 0` applies no floor.
- **A present `0.0` is a distinct case from absence**, and can mean either
  "corroboration rate genuinely measured at zero" or "this reviewer cleared
  `minRuns` but has never raised a finding at all" (the `findings_raised`
  denominator was zero, so the ratio is defined as `0.0` — never `NaN`/`Inf`).
  A caller that treats a present `0.0` as proof of a poorly-performing reviewer
  should first check whether it also raised any findings.
- **Best-effort against the store.** A missing, empty, or unreadable store
  directory yields an empty map and a nil error — this is a read-only,
  never-fails resolver; it does not create the store or write to it.
- `DefaultTrustMinRuns` is the conservative default floor (`20`) for a caller
  that does not pick its own `minRuns`. `atcr personas list --scores` calls
  `TrustPriors(dir, 0)` explicitly instead — that table is meant to show every
  reviewer with any history at all, so it opts out of the default floor rather
  than inheriting it.
- **`scorecard.ResolveTrustPriors()` (epic 35.9)** is the third consumer —
  `DefaultDir()` plus a read at `DefaultTrustMinRuns` in one best-effort call,
  degrading to a nil map on any failure (an unresolvable config dir, a
  missing/unreadable store) rather than erroring. Unlike `TrustPriors`, that
  read is **windowed to the last 180 days** (epic 35.11): because epic 35.9 put
  this call on the primary path of every review and reconcile, its cost is
  unconditional and would otherwise grow without bound as the store accumulates
  months. The window selects whole month **files** before they are opened, so
  history outside it is never read or parsed. Because selection is per **month
  file**, the whole calendar month containing the 180-day cutoff is included, so
  effective retention is 180 days plus however far into that month the cutoff
  falls — up to roughly 210 days, i.e. as many as 7 month files. Two
  consequences: a reviewer with no runs in any month file overlapping the last
  180 days falls back to the neutral "no history" state
  (absent from the map — the same state a brand-new reviewer occupies), and
  `TrustPriors(dir, minRuns)` itself is **unchanged and still all-history**, so
  `atcr personas list --scores` keeps reporting on the whole store. Every
  `atcr reconcile` /
  `atcr review --resume` / `atcr review` (one-shot mode) / MCP
  `atcr_reconcile` call site resolves it and threads the result into
  `reconcile.Options.TrustPriors`, which the epic-14.2 consensus filter
  consumes: a singleton from a historically reliable reviewer survives the
  filter without in-run corroboration, and one from a historically unreliable
  reviewer is demoted to `LOW` confidence. The demotion is independent of the
  `consensus` filter level (`--consensus` / `consensus:`) — it runs ahead of the
  filter, so a low-trust singleton is `LOW` at every level; only whether that
  `LOW` finding is then sidecarred changes. Under `consensus: off` it reaches
  `findings.json` still carrying `LOW`, which is the only configuration in which
  the demotion is observable end-to-end.
  > **Scorecard rates are not comparable across consensus levels.** Reviewer
  > records are computed from the **post-filter** finding set (`res.Findings`)
  > plus the Tier-4-routed set (`res.Unresolved`, which contributes to
  > `findings_raised` only), so under `lenient` or `off` the extra surviving
  > singletons each increment `findings_raised` without incrementing
  > `findings_corroborated`, lowering that reviewer's corroboration rate for that
  > run.
  >
  > **The trust-prior feedback loop this used to create is now closed.** Every
  > record carries the level it was measured under (`consensus_level`), and
  > `TrustPriors` counts **only `strict` runs**, so a relaxed run can no longer
  > depress the priors `trustExempt` and `demoteByTrust` apply on later `strict`
  > runs. A record with no `consensus_level` (any run written before epic 35.9.1)
  > counts as `strict` — those runs were strict by construction. This applies to
  > every surface, CLI and MCP alike, because the filter lives in `TrustPriors`
  > rather than at the emission site.
  >
  > **`findings_raised` also changed meaning once, and is filtered the same way.**
  > Epic 35.16.6.5 put the Tier-4-routed findings into the denominator, so a rate
  > averaged across records from both eras measures neither. Every record written
  > since carries `raised_includes_unresolved: true`, and `TrustPriors` prefers
  > those: when the window holds any, only they count; when it holds none, the
  > older records are used unchanged, so an existing history is never blacked out.
  > What is excluded is the mix.
  >
  > Two consequences worth knowing:
  > - `minRuns` is a floor on **strict** runs. A reviewer with 15 `strict` and 10
  >   `lenient` runs has 15 trusted measurements, not 25, so a long run of relaxed
  >   reconciles contributes no new trust data (by design — those counts are not
  >   comparable).
  > - The **leaderboard** (`atcr scorecard`, `atcr leaderboard`) is deliberately
  >   NOT filtered: it reports what actually happened across all runs. Its
  >   corroboration column still mixes levels, so read it with that in mind.
  >
  > `--no-scorecard` is still useful for keeping a throwaway run out of history
  > entirely, but it is no longer required to protect the trust priors.
  See
  [`reconcile/README.md`](../reconcile/README.md#behavior) for the filter-side
  mechanics. **Cold-start contract:** a reviewer needs `DefaultTrustMinRuns`
  (20) summed `strict` runs **inside the windowed read** — the month files
  overlapping the last 180 days, per the month-granularity note above — before
  its prior applies at all. Every reviewer on a fresh
  install, and any reviewer below that floor, is simply absent from the map,
  so reconcile behaves byte-identically to pre-35.9 until history accumulates.
  This resolver is intentionally not called from inside
  `internal/reconcile` itself: `internal/scorecard` already imports
  `internal/reconcile` (for `EmitForReconcile`), so the reverse import would
  cycle — each CLI/MCP call site resolves and attaches it instead.

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
> store. `--no-scorecard` suppression is a CLI-only flag. Because non-strict runs
> (`lenient`/`off`) are automatically excluded from trust-prior calculations on
> all entry points, MCP-driven exploratory runs cannot depress or corrupt trust
> priors, making `--no-scorecard` unnecessary on the MCP surface.

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

> **This allowlist governs the production `leaderboard --export` envelope only.**
> A `benchmark export` submission is a different document, and since
> `submission_schema` 2 it additionally publishes `suite_case_ids` and each row's
> `reviewer_coverage.case_ids` — the suite's case ids, **scrubbed but otherwise
> unaltered**.
>
> Case ids are producer-controlled and routinely encode repository identity: the
> bundled importer derives them as `<owner>-<repo>-pr-<number>`, so
> `standard-v1` ids read like `bluewave-labs-checkmate-pr-2883`. Exporting a
> submission built from a **private or internal suite therefore discloses those
> org, repo, and PR identifiers**, which the bullet above does not cover.
>
> They pass the same scrubber as `persona`/`model` (so paths, emails, and
> credentials cannot ride inside one, and any id the scrub would rewrite —
> including one it empties — is rejected rather than published) — but the
> scrubber does not
> treat an org or repo name as sensitive, because for the public suite it is not.
> Review your case ids before publishing a submission from a suite you did not
> intend to disclose.

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
  status}` event wired to emit on `review`/`reconcile` completion, on by default
  and disabled from either of two OR'd opt-out surfaces (the ingestion endpoint
  is currently empty, so the ping is an inactive no-op; see telemetry.md).
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
- `submission_schema` (`2`) is stamped on every **public submission** envelope
  (`leaderboard --export` and `benchmark export`).

They are decoupled on purpose: the local store format and the public submission
format evolve separately, so bumping one never silently changes the other. When a
future epic changes either schema:

- That version is incremented independently.
- Old stored records remain readable — the reader tolerates earlier versions, and
  unknown/absent optional fields degrade gracefully.
- Version negotiation for the public submission format is handled by the export
  paths, not by individual stored records.

**Not every meaning change moves a version number.** Epic 35.16.6.5 changed what
`findings_raised` COUNTS (it now includes the Tier-4-routed findings) without
changing any field's name, type, or presence, so neither integer moved. The
discriminator is the per-record `raised_includes_unresolved` flag instead, and
both derived surfaces — `TrustPriors` and `leaderboard --export` — apply the same
prefer-current rule: a set holding any current-era record uses only those, a set
holding none uses the older records unchanged. So a single submission is always
computed under one definition, and an existing store never stops exporting.

The `atcr scorecard` local leaderboard (`Aggregate`) is deliberately NOT filtered
this way — like the consensus-level filter, it reports what actually happened
across all runs.

### `submission_schema` is shared by two producers

`submission_schema` is one constant (`scorecard.SubmissionSchema`) stamped by **two**
envelopes:

| Producer | Envelope | Go type |
|----------|----------|---------|
| `atcr leaderboard --export` | production submission | `scorecard.ExportEnvelope` |
| `atcr benchmark export` | suite submission | `benchmark.Submission` |

Because the constant is shared, **a bump made for one producer versions the other**.
Neither side can evolve its envelope unilaterally, and that is deliberate: a forked
schema would let the same `submission_schema` value describe two different documents.

#### Version 2 — what changed, and for whom

Version 2 was bumped for the **benchmark** side. `benchmark.Submission` gained
`suite_case_ids` and `reviewer_coverage`, so a partial run published via
`--allow-partial-coverage` is now self-describing to a consumer instead of being
indistinguishable from a full one.

**The production envelope gained nothing.** Under version 2,
`leaderboard --export` emits the same key set it emitted under version 1:

- No field of `ExportEnvelope` or `PublicRecord` was renamed, retyped, or removed.
- No field was added to either type.
- The only change on the production path is the integer in `submission_schema`.

So a version-2 production submission differs from a version-1 one in exactly one
byte-range: the version number. The bump is **additive-only** on the producer side.

#### Consumer-side coordination — an open item, not a verified one

The producer-side claim above is checkable in this repository. **The consumer-side
one is not.** `ExportEnvelope` is only ever *marshaled* here — built in
`internal/scorecard/export.go` and written out by `cli/leaderboard.go`. This
repository contains no ingestion, validation, or rendering code for a submission, so
nothing in it can demonstrate how the public board reacts to:

- a `submission_schema` it has not seen before (does it accept, warn, or reject?), and
- the two new keys on a **benchmark** submission (are unknown keys tolerated, or does
  a strict decoder fail closed?).

**This is an explicit hand-off to the board maintainers, not a resolved question.**
Before version 2 submissions are published, they must confirm that the board accepts
`submission_schema: 2` and ignores unrecognized envelope keys. If it does not, the
consumer-side change belongs to the board's own repository — it cannot be made here.

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

During extraction the module lived at `./reconcile/` inside this repository. It is
now consumed through a versioned `require github.com/samestrin/atcr/reconcile`
(currently `v0.1.1`) in the root `go.mod`, published via `reconcile/vX.Y.Z` tags;
a `go.work` `use` entry bridges local development against the in-repo copy.

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
