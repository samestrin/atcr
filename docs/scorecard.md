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

## Record Schema (v2)

Each reconcile run appends one **reviewer** record per participating reviewer plus
one **aggregate** record summarizing the whole run. Records are JSON objects, one
per line (JSONL). `schema_version` is `2` on every record atcr writes today; a schema change
increments it and leaves old records readable (see [Schema versioning](#schema-versioning)).
Version `2` added `outcome` and `categories_raised`, both optional — a `1` record
carries neither and is read as "not measured", never as a measured zero.

`pair_signals` and `pair_era` arrived later, **still at version `2`**. They are additive and optional, so they do not change how any existing record decodes, and a bump would have reclassified every genuinely measured v2 record as unmeasured. `pair_era` is what marks a record as measured for that field; see its row below.

### Example (per-reviewer record)

```json
{
  "schema_version": 2,
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
  "outcome": "findings",
  "pair_signals": [
    { "peer": "greta", "agreed": 4, "disagreed": 1 }
  ],
  "pair_era": 1,
  "weighted_credit": 7.5,
  "findings_routed": 1,
  "credit_era": 1,
  "findings_verified": 4,
  "findings_refuted": 1,
  "survived_skeptic_rate": 0.8
}
```

### Field reference

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `schema_version` | int | always | Record schema version. Currently `2`. |
| `record_type` | string | always | `"reviewer"` for a per-reviewer row, `"aggregate"` for the run summary. Aggregate rows leave `reviewer`/`model`/`role` empty; consumers key on `record_type`. |
| `run_id` | string | always | `<RFC3339 reconciled_at>-<review-dir base>-<8 hex chars of sha256(absolute review dir)>`, e.g. `2026-06-14T10:00:00Z-abc123-1f2e3d4c`; runs reconciled before the hash was added carry the bare `<reconciled_at>-<review-dir base>` form, which `atcr scorecard <review-dir>` still finds. Uniquely identifies the run and selects the month file. |
| `reviewer` | string | always (empty on aggregate) | Reviewer/persona name (e.g. `bruce`). |
| `model` | string | always (empty on aggregate) | Model id the reviewer ran on (e.g. `claude-sonnet-4-6`). |
| `role` | string | always (empty on aggregate) | Pipeline role. Constant `"reviewer"` for reconcile-derived records. |
| `findings_raised` | int | always | Findings this reviewer raised. Includes findings the Tier 4 content check routed out of the primary stream into `unresolved.json` — they are findings the reviewer raised, so they belong in the denominator, and leaving them out would let a reviewer that produced phantoms score as if it had not. They are therefore NOT all present in `findings.json`. **One exception:** a routed finding whose `unresolved_reason` is `doc_shield` is excluded — its subject WAS named in the tree, in a file classified as prose by its extension, so being routed is not by itself fabrication evidence. Those are counted in `findings_doc_shielded` instead. |
| `findings_corroborated` | int | always | Of those, how many were corroborated (the finding carried 2+ distinct reviewers). A Tier-4-routed finding is NEVER corroborated, however many reviewers named it: agreement on a construct that is declared nowhere in the tracked tree is not corroboration. |
| `findings_doc_shielded` | int | conditional | Routed findings this record deliberately did NOT charge to `findings_raised`, because the Tier 4 check routed them on the documentation-extension heuristic (`unresolved_reason: doc_shield`) rather than on a genuine absence. Omitted when zero. Read it alongside `corroboration_rate`: the exemption is driven by the reviewer's own finding text, so a rate of 1.00 with a nonzero count here is not the same claim as a rate of 1.00 without one. Likewise, a rate of 0.00 alongside a nonzero count here is the zero-denominator case (every finding was shielded), not a corroboration failure. Note the trust prior does NOT extend the carve-out: shielded counts join the trust rate's denominator. |
| `findings_solo` | int | always | `findings_raised - findings_corroborated` — the arithmetic remainder, not "findings nobody else raised". Because a Tier-4-routed finding is never corroborated, two reviewers that independently named the same phantom each count it here. |
| `corroboration_rate` | float | always | `findings_corroborated / findings_raised` (0.0 when none raised; never NaN). |
| `cost_usd` | float | always | Estimated cost from the per-model rate table (see [Cost is approximate](#cost-is-approximate)). |
| `tokens_in` | int | always | Prompt tokens consumed (summed across turns for tool-using agents). |
| `tokens_out` | int | always | Completion tokens produced. |
| `latency_ms` | int | always | Reviewer wall-clock latency in milliseconds. |
| `findings_verified` | int | conditional | Findings confirmed by the skeptic stage. Present only when verification data drove the run. |
| `findings_refuted` | int | conditional | Findings refuted by the skeptic stage. Conditional, same as above. |
| `survived_skeptic_rate` | float | conditional | `findings_verified / (findings_verified + findings_refuted)`. Present only when `findings_verified + findings_refuted > 0` — a *stricter* condition than the two counts above, which are present whenever verification ran. When verification ran but nothing countable survived (every verdict truncated, or this reviewer's findings drew none) the two counts still ship as `0` and this key is omitted: `0/0` would publish `0.0`, which is indistinguishable from a reviewer whose findings were all refuted. Read the three keys individually, not as a set. |
| `raised_includes_unresolved` | bool | conditional | Superseded but retained. `true` when `findings_raised` counts the Tier-4-routed findings (every record written from Epic 35.16.6.5 onward); omitted on records written before it. The denominator has since changed meaning a second time (the 35.16.6.8 `doc_shield` carve-out), which a bool cannot express — `raised_denominator` below is the era discriminator a new reader should use. This field stays because existing readers and stores depend on it, and because `true` is still exactly right about the one thing it claims: routed findings are in the denominator. |
| `raised_denominator` | int | conditional | Which definition of `findings_raised` produced this record: `1` = routed findings excluded (everything before 35.16.6.5; never stamped — it is what an absent discriminator means), `2` = routed findings included (35.16.6.5, stamped as `raised_includes_unresolved: true` before this field existed), `3` = routed findings included EXCEPT the doc-shielded ones (35.16.6.8, the current definition; those are counted in `findings_doc_shielded`). Omitted on records that predate the discriminator — their era is read from `raised_includes_unresolved` instead. `TrustPriors` splits eras on this value (see `unresolvedEraRuns`), so a rate is never averaged across two definitions. |
| `outcome` | string | conditional | WHY this record's counts look the way they do, from the nine-value vocabulary in `internal/benchmark/outcome.go`: `findings`, `clean`, `unparseable`, `truncated`, `incomplete`, `ungrounded`, `filtered`, `failed`, or absent (unknown). It exists because a reviewer that read the diff and correctly found nothing, one that emitted prose no parser could use, and one whose call failed outright all record zero findings and would otherwise score identically. `TrustPriors` counts a record only when its outcome is `findings`, `clean`, `ungrounded` or `filtered` — see the eligibility rule below. Omitted when unknown. Two things read as unknown: every pre-v2 record, and a v2 record whose `AgentStatus` was not internally coherent. A reviewer with no `AgentStatus` at all — a path-anchored review with no pool summary — is NOT unknown: it is recorded as `findings`, because being named on a finding that survived reconcile is exactly what that value means. |
| `categories_raised` | array of string | conditional | The distinct `CATEGORY` values attached to the findings this reviewer participated in, drawn from the closed vocabulary in `reconcile/category.go`. Declared by the v2 bump and **populated since schema 2**. Three streams feed it: the surviving findings, the Tier-4-routed ones (**same exception as `findings_raised`:** a routed finding whose `unresolved_reason` is `doc_shield` is excluded), and the clusters reconcile set aside as ambiguous — the third because a consensus-filtered singleton *was raised*, and reading only the survivors would starve a lens whose missing trust prior is the reason its finding was filtered. The ambiguous stream contributes **categories only**: it moves no count and never mints a reviewer record. Values are deduped, sorted, and validated against `reconcile.Categories()` before they are written; an unrecognized value is dropped rather than persisted. The meaning is **not a per-reviewer claim** — it answers "was this topic in play on the case". Provenance is mixed and worth knowing when reading a stored record: entries from the surviving and Tier-4-routed streams carry `Merge`'s cluster-**modal** category, while the ambiguous stream's DBSCAN-noise and gray-zone-pair routes carry the individual reviewer's own **raw** word, so one finding can contribute both. Omitted when absent, which means "not measured" rather than "measured empty". |
| `pair_signals` | array of object | conditional | How this reviewer related to each **co-reviewer it shared a finding with** on this run. Each entry is `{ "peer": <reviewer>, "agreed": <int>, "disagreed": <int> }`, with `peer` already lower-cased and trimmed. `agreed` counts merged findings the two both raised. `disagreed` counts findings the two **split on severity** — the split `reconcile.Merge` records in the finding's `disagreement` field (`"<lo> vs <hi>"`), which is what the disagreement radar calls a `severity_split`. **A split is only counted when the cluster held exactly two reviewers.** `disagreement` is a property of the whole group, so on a three-reviewer cluster it says the group spanned more than one severity and *not* who sat on which side — and after a merge the per-reviewer severities are unrecoverable. Such a finding therefore contributes **nothing**: not a disagreement, because nobody can say between whom, and not an agreement either, because the group demonstrably did not agree. Discarding it forgoes data; apportioning it would fabricate a durable number. A two-reviewer **gray-zone** ambiguous cluster, by contrast, has an unambiguous pair: each such cluster arrives as one canonical pair key and charges that pair exactly ONE disagreement — on the **pair surface only** (it moves no finding count, the same asymmetry the ambiguous stream keeps everywhere else). Singleton and 3+-reviewer ambiguous clusters contribute nothing, the same unattributable rule the severity-split fold applies. The remaining gap still points one way: evidence is discarded, so the drop-candidate flag goes **un-raised**, never wrongly raised. Counts are **findings, not runs**. Entries are sorted by peer so two byte-identical runs serialize byte-identically. A finding raised alone produces no entry, and a lens that was silent produces none either: silence is **not** recorded as tacit agreement, because a zero-disagreement entry is indistinguishable from "never disagrees" — which is the drop-candidate verdict. Sourced from the surviving findings plus the two-reviewer gray-zone charge; the Tier-4-routed stream contributes nothing here. Omitted when the run produced no pair. |
| `pair_era` | int | conditional | The measurement era for `pair_signals`, currently `1`. It is stamped on **every** reviewer record atcr writes, including a run that produced no pair at all, and that is the whole point: an absent `pair_signals` is byte-identical on a run that genuinely had no co-reviewer and on a record written before the field existed. Without this marker the entire pre-existing store would read as "these lenses never co-occurred", which is the drop-candidate verdict applied to every pair in it. Presence of `pair_era`, not of `pair_signals`, is what says the record was measured; a record without it is excluded from the pair tally rather than folded in as a measured zero. Omitted on a record written before the field existed. |
| `weighted_credit` | float | conditional | The **disagreement-weighted** credit this reviewer earned on the run: the sum, over the findings it participated in, of `1 / <distinct reviewers on that finding>`. A finding nobody else raised is worth a full point; one the whole panel raised is worth a fraction of one. It is the inverse of what `findings_corroborated` counts, and deliberately so — a lens that finds what others missed is the reason a heterogeneous panel is worth running, and a raw agreement count rewards the generalist that overlaps everyone. It is a **separate** field rather than a redefinition of `findings_corroborated`, which is an int whose other consumers (the aggregate rows, the leaderboard export) keep their existing meaning. Routed and doc-shielded findings earn **no** credit: crediting a phantom would pay most for one nobody else raised. They still charge a denominator, but not the same one — a routed finding charges `findings_raised` directly, while a doc-shielded finding is counted only in `findings_doc_shielded` and reaches the trust denominator later, when the era merge folds it back in. **This is only half the score.** Whether a finding turned out to be REAL is not knowable at emit time, so the confirmation half is applied per persona when the score is read, from the local-debt ledger. Omitted when zero. |
| `findings_routed` | int | conditional | How many of `findings_raised` were **chargeable Tier-4-routed** findings — phantoms that charge the denominator and can never earn `weighted_credit`. It is the counterpart to `findings_doc_shielded` rather than a duplicate: a doc-shielded finding is counted **instead of** being counted in `findings_raised`, a chargeable routed one is counted **inside** it. Without this count the honest credit ceiling is not recoverable when the record is read back, and the resulting bound is loose by exactly the routed count — widest for the reviewers carrying the most fabrication evidence, which is the wrong direction. Omitted when zero, which here genuinely means zero: it is written together with `credit_era` on every record, so a record carrying the era carries a true count. |
| `credit_era` | int | conditional | The measurement era for `weighted_credit`, currently `1`. Stamped on **every** reviewer record atcr writes, including one that earned `0.0`, for the same reason `pair_era` is: an absent `weighted_credit` is byte-identical on a genuinely-zero run and on a record written before the field existed, and reading the whole pre-existing store as measured zeroes would drag every lens toward zero on upgrade. Presence of `credit_era`, not of `weighted_credit`, is what says the record was measured; a record without it is excluded from **both** sides of the weighted rate rather than averaged in. Omitted on a record written before the field existed. |


> **The weighted-credit score is not wired into review yet, and its own constants are provisional and unmeasured.** `weighted_credit` is written to every record, but the trust priors reconcile consumes are still the plain `corroboration_rate`. The weighted rate sits on a different scale from that rate, and reconcile's exemption and demotion thresholds were calibrated against the old one — switching the input without re-deriving them would demote much of the panel. Both the isolated-finding weight and the minimum number of closed debt outcomes needed before a confirmation rate is trusted carry a dated note and a named re-measurement trigger in `internal/scorecard/trust.go`. Treat any weighted number as provisional until that measurement exists.

> **The pair surface's two thresholds are provisional and unmeasured.** The sufficiency floor (`20` co-eligible cases) and the drop-candidate threshold (`0.05`) were set by analogy and by argument, not from a live store — there was none to measure when they were written. Both carry a dated note and a named re-measurement trigger in `internal/scorecard/pairtally.go`. Read a "drop candidate" verdict as a prompt to look, never as a measured finding, until that measurement exists.

**Conditional verification fields.** `findings_verified`, `findings_refuted`, and
`survived_skeptic_rate` are included only when the run had a readable, well-formed
`reconciled/verification.json` (i.e. `atcr verify` ran). When verification is
absent, these three keys are **omitted entirely** from the record, and the
`atcr scorecard` / `atcr leaderboard` tables omit the corresponding columns. An
absent, unreadable, or malformed verification file degrades gracefully to "no
verification" — it never fails the run. There is a **second, narrower omission
case** that drops one of the three on its own: when verification DID run but no
countable verdict survived (`findings_verified + findings_refuted == 0` — every
verdict truncated, or this reviewer's findings drew none), both counts still ship
as `0` and `survived_skeptic_rate` alone is omitted, because `0/0` would publish
`0.0` and a published `0.0` reads as a reviewer whose findings were all refuted.
So the three keys travel together only in the first case; read each on its own
condition.

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
plus `VERIFIED  REFUTED  SURV%` when any record carries verification data, plus
`DOC-SHIELDED` when any record has a nonzero doc-shield count.

`RAISED` does not count doc-shield-routed findings, so `DOC-SHIELDED` is the only
place this table reports them. It appears only when some record has one, and every
row then shows a number (`0`, not a dash — the reviewer has a measurement and it is
zero). See [Doc-shielded routings](#doc-shielded-routings).

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

Columns: `REVIEWER  MODEL  RUNS  RAISED  CORROBORATED  CORR%  COST  COST/CORR  LATENCY`,
plus `DOC-SHIELDED` when any group has a nonzero doc-shield count.
`COST/CORR` renders as `-` for a group with zero corroborated findings.

### Doc-shielded routings

A finding whose subject is named ONLY in a documentation-extension file is routed
to `unresolved.json` like any other unresolved finding, but is NOT charged to the
reviewer's `RAISED` denominator: the routing rests on a filename-extension
heuristic, and a scorecard charge is the one consequence of a misfire nothing can
undo later.

That carve-out makes two different reviewers render identically without the
`DOC-SHIELDED` column — `RAISED 6 / CORROBORATED 6 / CORR 100%` is the same row a
reviewer with 10 raised and 4 shielded produces. The shield does NOT apply to the
trust prior (`atcr personas list --scores`), which counts shielded findings in its
denominator precisely so a reviewer cannot launder phantoms by anchoring them on
doc-named tokens. So the two surfaces can legitimately disagree — a 100% row beside
a 0.60 prior — and `DOC-SHIELDED` is what makes that difference readable.

`report.md` reports the same split: its `Unresolved findings:` line counts the two
shapes separately, because "no symbol correspondence in the tracked tree" is false
for a doc-shielded record — its subject IS in the tree.

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
      "latency_p50_ms": 9100,
      "raised_denominator": 3
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
| `survived_skeptic_rate` | float | **omitempty** | Verified / (verified + refuted). **Omitted entirely** in two cases: no verification ran for the group, **and** verification ran but no countable verdict survived it (every verdict truncated, or none drawn) — a group in that state carries no stored rate to fall back on, so nothing is published rather than a `0.0`. Present as `0.0` only when verification ran and every finding was refuted. Absence therefore means "no countable verdict", NOT "no verify stage": the two are not distinguishable from this key alone. |
| `cost_per_corroborated_finding_usd` | float | **omitempty** | Total cost ÷ corroborated findings. **Omitted entirely** when there are zero corroborated findings (the metric is undefined — this is what distinguishes a paid-but-ineffective reviewer from a genuinely free one); present as `0.0` only when corroborated findings exist AND the reviewer's cost was genuinely zero. Never Inf/NaN when present. |
| `latency_p50_ms` | int | always | Median (p50) of per-run latencies — not the mean. |
| `raised_denominator` | int | always | Which definition of "findings raised" produced this row's `findings_raised_avg` and `corroboration_rate` (`1`/`2`/`3` for production eras, `100` for a benchmark-suite row — a different axis, never compared ordinally). **Not omitempty:** a submission that does not say which definition it used is exactly the ambiguity the field exists to remove, so the key is always present. |

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
whose `model` or `reviewer` is non-blank in the store but scrubs away to nothing
(`"admin@internal.host"` loses its email-shaped token whole; so do
`"/models/mistral-7b.gguf"`, `"bedrock@us-east-1/claude"` and `"~/models/foo"`) is
**dropped from the envelope** and named on stderr — it is never published as
`model: ""`. It does not fail the export, because this shape occurs in ordinary
history and `--export` reads the whole unrotated store, so one such record would
otherwise take down an export that had succeeded the day before, clearable only by
hand-editing a JSONL store. If every selected record is dropped, the ordinary
no-records error is raised instead of an empty envelope. An identity that is already
empty — or whitespace-only, which is the same thing to every reader of the store — is
a record written without a model, not a scrub casualty: it is left alone and still
publishes. One carve-out: whitespace that is also a control rune (tab, newline, CR,
VT, FF, U+0085) never reaches this arm: it falls under the control and format rune rejection
described above, which hard-fails the export before the keep-and-warn arm runs; only
whitespace that survives that rejection (space, non-breaking space) takes the
kept-and-warned shape below. A **whitespace-only** identity is additionally **named on
stderr** ("blank
after trimming — the record has no model/reviewer"), because it publishes as `model: ""`, which
would be rejected at the leaderboard — the same consequence the skip report above names
for the same shape. The warning is deliberately worded apart from the skip
report above so a kept record is not mistaken for a dropped one. An identity already
empty in the store is not warned about — that shape is ordinary history, and reporting
it would name a large fraction of an unrotated store on every export.

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
- **Outcome eligibility.** A record counts toward a reviewer's rate only when its
  `outcome` is on the counted side of this split:
  - **Counted:** `findings`, `clean`, `ungrounded`, `filtered`.
  - **Excluded:** `unparseable`, `truncated`, `incomplete`, `failed`, and
    absent/unknown.

  The excluded ones mean the lens did not get a fair attempt, and a durable score
  has to measure judgment rather than hosting. The panel's real history is the
  argument — a lens that hung on a proxy timeout, one whose host silently capped
  prompts at 16,384 tokens while answering HTTP 200, and one auth-failed on a
  billing cap would all have been demoted for their wiring. `ungrounded` and
  `filtered` are counted deliberately: both follow a complete, parseable response
  whose findings were discarded for cause, which is a judgment result. The test
  is an allowlist, so a
  future tenth outcome value is excluded until someone decides otherwise. A
  reviewer left with zero eligible runs is ABSENT from the map, never present at
  `0.0` — never a punitive score for a broken proxy.
- **Opportunity-set scoping.** A record is dropped only when its lens **raised nothing** on a run that was not an *opportunity* for it — that is, when no reviewer on the run raised a `categories_raised` value inside that lens's remit. A lens that raised findings is kept whatever its categories say; see the fifth class below, which is where that rule is stated in full and where the narrower rule it replaced is explained. A narrow lens is supposed to be silent most of the time, so scoring it against the full corpus makes it look like a lens that never contributes while a generalist accumulates standing by being in scope everywhere. Removing out-of-remit runs from the denominator is what makes the two rates comparable. Six kinds of record survive the opportunity gate. The most ordinary is a silent lens whose remit WAS in play — the gate working as intended, and not enumerated below. Of the remaining five, FOUR are never judged, and pass through untouched because nothing on the run says whether the lens's remit was in play; the fifth IS judged, on a different question, and is described after them. The four: aggregates; pre-schema-2 records (their category set is unmeasured, not empty); the registry-only lenses with no in-repo definition to ground a remit against; and any run that contributed no *discriminating* category at all. That last class has three routes, and they are indistinguishable in the store: nobody raised anything; every raised value fell outside the closed vocabulary and was dropped at the write gate (the likeliest one in practice — `reconcile/category.go` records a dry run where 72.3% of findings used a word the scorer did not recognise); or every raised value was non-discriminating, meaning `other`, `out-of-scope` or `invariant`, which carry no topic. Judging any of the three would un-score a whole panel for a labelling failure.

  The fifth is not a pass-through at all and is listed here because it used to be one. **A record that RAISED findings is never dropped by this gate**, whatever its categories say. It is judged — on the raised count, not on the remit — and kept. When it contributed no discriminating category of its own it is additionally annotated as *unlabelled*, which `personas list --scores` reports in its `CASES` column; when it contributed a real topic outside its own remit it is simply counted.

  That rule replaced a narrower one, and the replacement is worth understanding because it repeals something this document previously stated. The narrower rule dropped any out-of-remit record, silent or not, and it made mislabelling profitable: a lens whose phantom findings carried a *recognised* word outside its remit had the whole record removed from its denominator, while the same phantoms under an unrecognised word were charged in full — because an unrecognised word is stripped at write time and the record then read as unlabelled. Measured on a worked example, a lens with twenty honest runs and a hundred uncorroborated out-of-remit phantoms scored a perfect 1.00, clearing the threshold at which reconcile exempts a lens from the consensus filter entirely; the same phantoms under a junk word scored it 0.17. Correct labelling was more exculpating than gibberish.

  **What that costs is a real narrowing of the opportunity-set rule and not a free fix.** "A case is in a lens's denominator only when that lens's remit was in play" now holds for SILENT lenses only. The guarantee the panel actually needs is the narrower one — a lens *correctly silent* on an out-of-remit case is neither credited nor penalised — and a lens that raised a hundred findings on that case was not silent. It made a judgment call and is accountable for it. The change also removed an asymmetry: the five registry-only lenses with no in-repo remit never reached the drop branch at all, so the old rule protected the nine grounded personas and no others.

  **What the change did NOT close, stated here because the paragraph above reads as though it did.** It closed the *labelling asymmetry*: a phantom under a recognised out-of-remit word and one under an unrecognised word now score identically. It did NOT close the 1.00 escape itself on the production path, and the reason is a different mechanism. Under strict consensus — the only level trust priors read — an uncorroborated singleton is routed to the ambiguous stream unless the lens is already trust-exempt. The routing happens in reconcile and is carried into the scorecard by the reconcile bridge, not by the record writer itself; the stream contributes its CATEGORY and no count. So the out-of-lane phantom never reaches this gate with a raised count at all: it arrives at zero, is not charged, and the lens keeps its rate. Keeping that record instead of dropping it was tried and reverted, and the reason is worth stating because it is the opposite of intuitive. A zero-raised record is on neither side of the ratio, so keeping it looks free — but the minimum-run floor counts records, not findings, so a hundred evidence-free records will carry a lens over a floor its real history does not reach and publish a rate computed from a handful of runs. Measured: five honest runs publish nothing; the same five plus a hundred zero-raised out-of-lane contributions publish at 1.00, which is above the threshold at which reconcile exempts a lens from the consensus filter entirely. Charging the ambiguous stream is a scoring change that must answer the mirror case too, where an in-remit ambiguous category buys opportunity-set membership at no denominator cost; both directions are tracked together and neither is resolved.

  **The narrowing cuts both ways, and the second direction is easy to miss.** Keeping an out-of-remit raiser puts its findings in the *numerator* as well as the denominator, so out-of-lane work the panel agreed with now RAISES a lens's rate where it used to be dropped from the calculation entirely. A lens cannot be held accountable for its out-of-lane mistakes and left unrewarded for its out-of-lane hits by the same gate. Expect a lens that ranges outside its remit to move further in both directions than it did before.

  **This reduces the run count the `minRuns` floor sees**, and that floor was measured against unfiltered strict runs; the re-measurement is tracked as a known limit beside `DefaultTrustMinRuns` in `internal/scorecard/trust.go`.
- **The filter is scoped to `TrustPriors`, exactly as `strictRuns` and
  `unresolvedEraRuns` are.** `leaderboard --export` and `PublishedSet` do NOT
  apply it: the leaderboard reports what actually happened across all runs, so a
  `failed` or `truncated` row still contributes to the exported
  `corroboration_rate` and `findings_raised_avg`. The trust prior is a
  behavioural measurement, and only that one is gated.
- **Absent is not the same as neutral.** An absent key is never read as a rate of
  `0.0` — `trustExempt` and `demoteByTrust` both gate on the comma-ok — but its
  absence switches BOTH of them off. A high-trust lens stops being exempted from
  the consensus filter, and a low-trust phantom-raiser stops being demoted to
  `LOW`. That second direction is a LOOSENING, visible in `findings.json`
  confidence. (This is not the `1/N` baseline, which is the per-run PageRank
  uniform authority — a different mechanism entirely.)
- `DefaultTrustMinRuns` is the conservative default floor (`20`) for a caller
  that does not pick its own `minRuns`. `atcr personas list --scores` calls
  `TrustPriors(dir, 0)` explicitly instead — that table is meant to show every
  reviewer with any SCOREABLE history, so it opts out of the default floor rather
  than inheriting it. **It does not opt out of the eligibility filter**, and the
  difference is visible on an upgrade: a store written before `schema_version` 2
  has no `outcome` on any record, so every reviewer in it is excluded and the
  table renders all-`n/a` with the "no data" footer until fresh runs accumulate.
  A reviewer recovered from the findings of a pool-summary-less review is NOT
  affected — it records `findings` and scores normally.
  A rate computed from unclassified runs is not a measurement, so absence is the
  honest answer — but absence is not neutral (see the bullet above: it switches
  demotion off as well as exemption), and on an existing install this is a
  visible change rather than a silent one.
- **`scorecard.ResolveTrustPriorsAndUnmeasured()` (epic 35.9; the unmeasured
  counter added in 36.0)** is the third consumer, and it
  is the one on the primary path of **every** `atcr review`, `review --resume`,
  `reconcile` and MCP `atcr_reconcile` call — so the outcome-eligibility rule
  above reaches production through here, not only through `personas list
  --scores`. (It is `ResolveTrustPriors`' windowed read plus a count of the
  reviewers the outcome gate alone keeps out of the map, reported on the
  reconcile log line; `ResolveTrustPriors` itself remains as the
  `personas list --scores` in-use read and the nil-lookup form of
  `ResolveTrustPriorsWithGroundTruth`.) On an upgrade to `schema_version` 2 every stored record is still v1
  and therefore unclassified, so the priors map is empty until each reviewer
  accumulates `DefaultTrustMinRuns` strict runs under the new schema. Both
  consensus-filter behaviours go dark for that period: a high-trust singleton
  stops being exempted, and a low-trust phantom-raiser stops being demoted to
  `LOW` — the second being a loosening that shows up in `findings.json`
  confidence. This is a one-time upgrade cost and it recovers as new records
  accumulate — every reconcile writes v2 records, including a path-anchored
  one with no pool summary. It is
  documented because it is otherwise invisible. Mechanically, it is
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
  180 days falls back to the "no history" state (absent from the map — the same
  state a brand-new reviewer occupies, and not a neutral one: it disables
  demotion as well as exemption), and
  `TrustPriors(dir, minRuns)` itself is **unchanged and still all-history**, so
  `atcr personas list --scores` keeps reporting on the whole store. Every
  `atcr reconcile` /
  `atcr review --resume` / `atcr review` (one-shot mode) / MCP
  `atcr_reconcile` call site resolves it — through
  `ResolveTrustPriorsAndUnmeasured` — and threads the result into
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
  > **`findings_raised` has changed meaning twice, and is filtered the same way.**
  > Epic 35.16.6.5 put the Tier-4-routed findings into the denominator, and Epic
  > 35.16.6.8 took the doc-shielded ones back out, so `findings_raised` has three
  > definitions and a rate averaged across two of them measures neither. Every
  > record written since 35.16.6.8 carries `raised_denominator: 3`; records from
  > 35.16.6.5 carry `raised_includes_unresolved: true` and no version, which reads
  > as definition 2; anything older is definition 1.
  >
  > `TrustPriors` prefers the NEWEST definition each reviewer actually has: when a
  > reviewer's window holds records under more than one, only the newest count;
  > when it holds only old ones, they are used unchanged. So an existing history is
  > never blacked out — **per reviewer**. A reviewer that has run since the change
  > loses its older records from the window, which is the point: what is excluded
  > is the mix, not the history.
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
  `latency_p50_ms`, `raised_denominator` (a schema discriminator — it says which
  `findings_raised` definition produced the row and carries no run content)

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
> unaltered** — plus `reviewer_coverage.grounding_enabled`, a boolean carrying no
> content of its own.
>
> `grounding_enabled` says whether the Epic 14.1 grounding gate was live for the
> run behind that row. It is published because `corroboration_rate` is scored over
> the post-gate finding set, so a gated row and an ungated row can report the same
> rate about different populations; without the tag the board has no way to tell
> which two rows are comparable. It is additive under the policy below — it does
> not bump `submission_schema`.
>
> **Absent means the gate state was not recorded, which is not the same as "off".**
> A production row has no gate state to report and is always absent. A benchmark row
> normally carries the tag — `false` on `standard-v1`, whose range-less path fails the
> gate open, and `true` on `repo-state-v1`, where it is live — but it is absent there
> too when the state was not observed: a run resumed from a checkpoint written before
> the tag existed, or a row folded across a mix of gated and ungated cases. Treat an
> absent tag on either kind of row as **unmeasured**, never as ungated; a board that
> reads it as "off" would compare it against a genuinely ungated row as though the two
> measured the same population.
>
> **One carve-out supersedes the rule above for benchmark `standard-v1` rows.** The
> tag is recent: a submission produced before it existed carries no tag at all. On
> `repo-state-v1` that absence is genuinely unmeasured. On `standard-v1` it is not —
> that tier's gate has never been live; its range-less path fails open today exactly
> as it did before the tag existed — so a tag-less `standard-v1` row measures the
> same ungated population as one tagged `false`. When comparing across the upgrade
> boundary, treat an absent tag on a `standard-v1` benchmark row as equivalent to
> `false`; the unmeasured reading applies only to `repo-state-v1` rows and to
> production rows.
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

- `schema_version` (`2`) is stamped on every **stored** record (the local JSONL
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
`findings_raised` COUNTS (it now includes the Tier-4-routed findings), and Epic
35.16.6.8 changed it again (the doc-shielded routings came back out), neither time
renaming, retyping, or removing a field — so neither integer moved. The
discriminator is the per-record `raised_denominator` version instead, and both
derived surfaces — `TrustPriors` and `leaderboard --export` — apply the same
prefer-newest rule: a reviewer's records are kept at the newest definition that
reviewer has. So a single submission is always computed under one definition, and
an existing store never stops exporting.

That is enough within one store and not enough between two. Two submitters running
different atcr versions publish rates computed under different rules, both stamped
the same `submission_schema`, and the board ranks them against each other. So each
public reviewer row also carries `raised_denominator` — additively, on
`scorecard.PublicRecord`, the type both the production and benchmark envelopes
share.

Sharing the type puts the KEY on both producers; it does not populate it. Each
producer stamps its own value, and the two are not on the same scale:

| Producer | `raised_denominator` | What the row's `corroboration_rate` means |
|---|---|---|
| `leaderboard --export` | `1`, `2` or `3` — the definition its records were computed under | corroboration: the share of findings a second reviewer also raised |
| `benchmark export` | `100` (`RaisedDenominatorBenchmarkSuite`) | category **recall** against the suite's planted defects |

The gap between `3` and `100` is deliberate. A benchmark row is not a production
row under an older rule — it is a different quantity, and the two must never be
ordered against each other. The envelope's `source` field already separates the
producers; this makes each row self-describing as well.

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

**The production envelope gained nothing at the bump itself.** Under version 2,
`leaderboard --export` initially emitted the same key set it emitted under
version 1 — no field of `ExportEnvelope` or `PublicRecord` was renamed, retyped,
or removed, and the only change on the production path was the integer in
`submission_schema`. (Epic 35.16.6.8 later added `raised_denominator` to
`PublicRecord` under the same version — see the policy below.)

So a version-2 production submission differs from a version-1 one in exactly one
byte-range: the version number. The bump is **additive-only** on the producer side.

#### Versioning policy — additive never bumps

`submission_schema` is bumped **only for breaking changes**: a field renamed,
retyped, or removed, or a semantic shift a consumer cannot ignore. Additive
field additions — `suite_case_ids`/`reviewer_coverage` on the benchmark
envelope, `raised_denominator` on every reviewer row — **never** bump it. That
is why version 2 denotes more than one wire shape, and that is by design: the
board contract is that consumers tolerate unknown keys. (Verifying the board
actually does so is the open hand-off below — it is not claimed settled here.)

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
