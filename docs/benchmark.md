# Benchmark Suite

`atcr benchmark` is the standard-suite tooling that feeds the public **Model-Eval
Leaderboard** (Epic 10.0). A benchmark suite is a versioned, fixed set of diff
cases with planted defects; running the same suite across models yields
*comparable* scores, and only suite-sourced submissions are eligible for the
public board — so cherry-picked production runs cannot game it.

This page documents the in-repo tooling:

- the **suite-manifest contract** (`internal/benchmark`),
- `atcr benchmark verify` — validate a suite and print its reproducibility hash,
- `atcr benchmark run` — execute a suite through the review pipeline and write a scored run-result,
- `atcr benchmark export` — emit a suite-tagged public submission record from a run-result, gated on full-suite coverage.

The full loop is **`run` → `export`**: `run` produces a run-result by reviewing
every case's diff and scoring the findings; `export` wraps that run-result in the
public submission envelope. The public board accepts only `source ==
"benchmark-suite"` submissions, so production runs cannot be passed off as suite
scores.

> **Suite content is bundled.** The curated `standard-v1` suite **content** lives
> in [`benchmarks/standard-v1/`](../benchmarks/standard-v1/) in this repo, ingested
> from [`alibaba/aacr-bench`](https://github.com/alibaba/aacr-bench) (Apache-2.0 —
> see [`benchmarks/standard-v1/NOTICE.md`](../benchmarks/standard-v1/NOTICE.md)).
> The tooling here operates against any suite directory that satisfies the contract
> below — including the in-repo `internal/benchmark/testdata/suite-valid` fixture.

---

## Suite manifest contract

A suite is a directory containing a `suite.json` manifest plus the diff files it
references:

```
my-suite/
├── suite.json
├── case-01.diff
└── case-02.diff
```

```json
{
  "suite": "standard-v1",
  "suite_version": "1.0.0",
  "cases": [
    {
      "id": "case-01-nil-deref",
      "diff": "case-01.diff",
      "expected_categories": ["correctness"]
    },
    {
      "id": "case-02-sql-injection",
      "diff": "case-02.diff",
      "expected_categories": ["security", "correctness"]
    }
  ]
}
```

| Field | Type | Rules |
|-------|------|-------|
| `suite` | string | Required, non-empty. The suite identity. |
| `suite_version` | string | Required, non-empty. Pins reproducibility; travels with every submission. |
| `cases` | array | Required, at least one case. |
| `cases[].id` | string | Required, non-empty, **unique** within the suite. |
| `cases[].diff` | string | Required. Path **relative to** the suite directory; must not be absolute or escape the directory (`..` is rejected). The file must exist. |
| `cases[].expected_categories` | string[] | Required, at least one. The planted-defect categories a competent reviewer should surface. Matched case-insensitively against each finding's category by the scorer (see `benchmark run`). |

`internal/benchmark.Load(suitePath)` reads, validates, and confirms every diff
file exists — it returns an error rather than a half-valid suite.

---

## `atcr benchmark verify --suite-path <dir>`

Validate a suite manifest and print its **reproducibility hash**. Read-only.

```bash
atcr benchmark verify --suite-path ./my-suite
```

```
suite "standard-v1" version 1.0.0: 2 cases, valid
reproducibility hash: 3f9a…<64 hex>
```

The reproducibility hash is a deterministic SHA-256 over the suite's *content*:
the suite identity, each case's id + expected categories, and the **bytes** of
each diff file. It is **independent of case order** in the manifest (content, not
ordering, defines reproducibility) and **content-sensitive** (a single changed
diff byte changes the hash). Two people with the same suite version can confirm
they are running byte-identical cases by comparing hashes.

Behavior:
- Missing `suite.json`, malformed JSON, a failing validation rule, or a missing
  diff file → error, non-zero exit.
- `--suite-path` is required.

---

## `atcr benchmark run --suite-path <dir> [--output <path>] [--checkpoint <path>]`

Execute a suite through the **review pipeline** and write a scored run-result.

```bash
atcr benchmark run --suite-path ./my-suite --output run.json
atcr benchmark run --suite-path ./my-suite          # run-result to stdout
atcr benchmark run --suite-path ./my-suite --checkpoint run.ckpt.json   # resumable
```

For each case, `run` ingests the case's diff through the same diff-file ingestion
path production uses (so the suite is scored on the exact payload the real pipeline
sees), fans out the project's configured reviewer roster against it, then scores
each reviewer's findings against the case's `expected_categories`. The roster and
provider settings are discovered from the project config the same way `atcr review`
discovers them.

### Scoring

Each reviewer's per-case findings are folded into the **single public reviewer
schema** (the same row shape `export` and `leaderboard --export` emit):

| Field | Benchmark meaning |
|-------|-------------------|
| `corroboration_rate` | **Category recall** — the macro-average across cases of (distinct `expected_categories` the reviewer satisfied with ≥1 finding) ÷ (distinct expected categories). This is the headline benchmark metric. A finding satisfies an expected category when its own category is a member of that category's **equivalence family** — so a raised `style` satisfies an expected `maintainability`, because aacr-bench's single upstream "Maintainability and Readability" label spans both. The families are scorer-side only (no product category is merged) and are listed in `internal/benchmark/equivalence.go`. **The relation runs coarse-expected → fine-raised only and is *not* symmetric:** a raised `maintainability` does not satisfy an expected `style`, because a vague finding does not corroborate a specific planted defect. An expected category that has no family — any fine word, which a hand-authored `--suite-path` suite is free to plant — is scored by **exact match**, exactly as it was before families existed. |
| `findings_raised_avg` | Mean findings raised per case (volume/thoroughness). |
| `runs` | Number of cases scored. |
| `cost_per_corroborated_finding_usd` | Recorded cost ÷ findings whose category satisfied an expected one (the same equivalence relation `corroboration_rate` uses — the two never disagree about what counts as a match). **Omitted from the JSON entirely** when zero findings matched an expected category — the metric is undefined, not `0.0` (a paid-but-uncorroborated reviewer must not read identically to a genuinely free one). Present as `0.0` only when matched findings exist and the provider reports no usage cost. |
| `latency_p50_ms` | Median per-case latency over cases with reported usage (0 otherwise). |

> **`corroboration_rate` is repurposed as a recall proxy here.** In a production
> submission it means cross-reviewer corroboration; in a benchmark submission it
> carries category recall against the planted defects. The `source ==
> "benchmark-suite"` tag on the submission disambiguates the two. **Precision is
> deliberately not reported:** `expected_categories` is the planted-defect *subset*,
> not exhaustive ground truth, so a precision-vs-planted metric would penalize a
> reviewer for also surfacing legitimate non-planted issues. Category recall plus
> `findings_raised_avg` capture coverage and volume without that distortion.

Category matching is case-insensitive and whitespace-trimmed on both sides.

### Reproducibility

`run` stamps `generated_at` from the wall clock, but the **scoring is
deterministic**: two runs over the same suite and the same transcript (reviewer
outputs, usage, and latency) produce byte-identical scored metrics. Set the same `generated_at` (e.g.
by reusing a captured run-result) to compare two runs field-for-field; otherwise,
a resumed run on a later day differs only in `generated_at`.

Behavior:
- Invalid suite (missing `suite.json`, failing validation, missing diff) → error.
- A case whose entire roster fails to review → error (a case nothing reviewed is
  not scored as zero). Partial failures score the failed reviewers as recall 0 for
  that case.
- `--suite-path` is required. `--output` writes the JSON to a file (atomic, parents
  created) instead of stdout.

### Resumability (`--checkpoint`)

A benchmark over a real suite is many cases × many reviewers of **paid** LLM work,
run serially. Because a single transient failure on case *N* (a total-roster case
failure, a network blip, a rate-limit) aborts the whole run, the completed,
already-paid-for work of cases `1..N-1` would otherwise be lost.

`--checkpoint <path>` makes the run **resumable**:

- After each case is scored — and *before* the next case begins — its scored outcome
  is durably written to the checkpoint file (an atomic temp-file + rename, so a
  process killed mid-suite leaves a checkpoint holding exactly the completed cases).
- Re-running the same suite with the same `--checkpoint` path **resumes** from the
  first unscored case: already-scored cases are replayed from the checkpoint (no
  re-execution, no further LLM cost) and only the remainder is executed.
- A resumed run produces a **byte-identical** run-result to an uninterrupted run over
  the same suite + transcript — the reproducibility contract holds across the resume
  boundary.
- Resume is guarded by **suite identity** and **roster identity**: the checkpoint
  records the suite's reproducibility hash (see `verify`), name, and version, plus the
  reviewer panel (each agent and its configured model). If the suite content changed,
  or the roster changed (a reviewer added/removed, or a model swapped), the run
  **fails closed** with a clear message (remove the checkpoint to start fresh) rather
  than silently mixing inconsistent work into a new run. The roster check is separate
  because the reproducibility hash covers only suite content, not the panel.

Checkpointing is **opt-in**: without `--checkpoint`, behavior is unchanged — a
total-roster case failure still aborts the run (a transient infrastructure failure
is never scored as a genuine missed defect).

---

## `atcr benchmark export --in <run-result.json> [--output <path>]`

Emit a **suite-tagged** public submission record from a suite run-result.

```bash
atcr benchmark export --in run.json
atcr benchmark export --in run.json --output /tmp/submission.json
atcr benchmark export --in run.json --suite-path benchmarks/standard-v1
atcr benchmark export --in run.json --allow-partial-coverage
```

**Export refuses a run-result whose reviewer rows do not each cover the case list
the run-result declares.** Case difficulty varies enormously across the suite, so a recall computed over one
8-case subset is not comparable to one computed over a different 8 — publishing them
side by side silently compares different measurements. Since a run-result reports one
row per *realized* model (see [Realized-model attribution](#realized-model-attribution-and-coverage)),
a run that crossed a provider quota boundary mid-suite normally produces short rows,
and this is the routine case rather than an exotic one.

The rejection names each short row and its shortfall:

```
run-result run.json has reviewer row(s) scored over less than the full 17-case suite:
qwen3.8-max/brad (8/17 cases, missing case-09, case-10, case-11 and 6 more);
llm-large/brad (9/17 cases, missing case-01, case-02, case-03 and 5 more)
```

`--allow-partial-coverage` overrides it, warning on stderr instead of failing. Note
the override is **operator-visible, not consumer-visible**: the shortfall is recorded
in the run-result, but the submission envelope does not carry coverage (adding a key
there is a `submission_schema` decision — see below), so a published partial run is
not self-describing to the board. Prefer re-running the missing cases.

A run-result that records **no** coverage at all — any file written before coverage
existed — is treated as *unmeasured* rather than short: export warns that it could not
be verified and proceeds, mirroring how a nil `out_of_vocabulary_rate` is read.

### What the coverage gate does and does not prove

The denominator the gate divides by is `suite_case_ids`, which comes from the same
run-result the gate is checking. On its own the check is therefore an
**internal-consistency** one: it proves every reviewer row covers the declared case
list. It does not, by itself, prove the declared list is the suite's — a caller who
truncates `suite_case_ids` and every row's `case_ids` to the same 8 of 17 cases
produces a file that passes every check and publishes as fully covered.

`--suite-path <dir>` closes that gap and is what promotes the gate to a real
suite-coverage guarantee: the run-result's `suite`/`suite_version` must match the
manifest, and its `suite_case_ids` must equal the manifest's case list exactly (both
directions — a missing id is a truncated denominator, an extra one is an inflated
one). It is optional because a run-result is portable and the suite tree may not be
on the exporting machine; pass it whenever the suite *is* available, and always for
anything headed to the public board.

```
run-result run.json declares a 8-case suite but the suite manifest at benchmarks/standard-v1
has 17, missing case-09, case-10, case-11 and 6 more; every reviewer row was therefore
scored against a shrunken denominator
```

With `--suite-path`, a run-result recording no coverage at all is an **error** rather
than an unmeasured warning: there is nothing to anchor, and the flag must not read as
a check that silently did nothing.

The output envelope is **distinct from the production `leaderboard --export`** by
its `source`, `suite`, and `suite_version` fields — that is what lets the public
board accept suite submissions and reject production ones:

```json
{
  "submission_schema": 1,
  "atcr_version": "0.0.0",
  "submitted_at": "2026-06-24T12:00:00Z",
  "source": "benchmark-suite",
  "suite": "standard-v1",
  "suite_version": "1.0.0",
  "reviewers": [
    {
      "model": "claude-sonnet-4-6",
      "persona": "bruce",
      "runs": 2,
      "findings_raised_avg": 10.5,
      "corroboration_rate": 0.6,
      "cost_per_corroborated_finding_usd": 0.006,
      "latency_p50_ms": 8900
    }
  ]
}
```

The `reviewers[]` rows reuse the **same public reviewer schema** as
`leaderboard --export` (documented in [`docs/scorecard.md`](scorecard.md)), so the
public board renders one consistent set of columns for both submission sources.

#### `atcr_version` is the scorer discriminator

`suite`, `suite_version`, and the reproducibility hash pin the **inputs** — the
cases, their planted categories, and the raw diff bytes. They say nothing about
how those inputs were **scored**, and the hash is deliberately blind to scorer
changes.

That gap is real. Epic 35.16.5 introduced benchmark-side equivalence-class
matching, so `corroboration_rate` and `cost_per_corroborated_finding_usd` mean
something different before and after it — for the *same* `standard-v1` `1.0.0`.
Two submissions can therefore carry identical suite identity and
non-comparable numbers.

**`atcr_version` is the only field that separates them, and it is
scoring-relevant for exactly this reason.** When comparing or aggregating
submissions:

- Treat `(suite, suite_version, atcr_version)` — not `(suite, suite_version)` —
  as the comparability key for any recall- or cost-derived metric.
- Do not aggregate `corroboration_rate` or
  `cost_per_corroborated_finding_usd` across differing `atcr_version` values
  without stating that the scorer changed between them.

A dedicated scoring/metric version in the envelope would say this more directly,
but `submission_schema` is a frozen public contract; adding a field is a
schema-versioning decision, not a documentation fix. Until such a field exists,
`atcr_version` carries the meaning, and consumers must be told so — which is what
this section does.

### The run-result contract

`export` reads a **run-result** file rather than your local scorecard — so a
production run can never be passed off as a suite submission. A run-result is:

```json
{
  "suite": "standard-v1",
  "suite_version": "1.0.0",
  "generated_at": "2026-06-24T12:00:00Z",
  "out_of_vocabulary_rate": 0.04,
  "reviewers": [ /* public reviewer rows */ ],
  "suite_case_ids": ["case-01-nil-deref", "case-02-sql-injection"],
  "reviewer_coverage": [
    {
      "model": "qwen3.8-max",
      "persona": "brad",
      "case_ids": ["case-01-nil-deref"],
      "outcomes": { "findings": 1 },
      "fallback_cases": 0
    },
    {
      "model": "llm-large",
      "persona": "brad",
      "case_ids": ["case-02-sql-injection"],
      "outcomes": { "unknown": 1 },
      "fallback_cases": 1
    }
  ]
}
```

`suite_case_ids` and `reviewer_coverage` are **run-result-only** — they gate
publication and are not carried into the submission envelope. Both are omitted
entirely by a producer that did not measure them, which is what lets export tell
"unmeasured" apart from "short".

`out_of_vocabulary_rate` is a **run-level diagnostic**, not a reviewer metric: the
share of the run's findings whose category is not a member of the closed reviewer
vocabulary. It answers "did the models actually use the categories they were
offered?", which no other field reveals — a reviewer that invents its own words
quietly zeroes its own recall, and the low score is indistinguishable from one that
simply found less. Five details matter when reading it:

- A run is guarded against a **ceiling of `0.20`**, and the ceiling is **exclusive** —
  a run sitting exactly on `0.20` trips the guard. Treat `0.20` as a *provisional
  fixture guard*, not an empirical bound on model behaviour: it is deliberately loose
  so that the words `reconcile`'s merge table records as *meaning* a taxonomy member
  without *being* one (`bug`, `input`, `clarity`, `consistency`, `structure`, …) have
  headroom until epic 35.16.6 lands parse-boundary canonicalization. It is **not**
  derived from the 35.16.2 dry-run's 72.3%, which measured a different denominator
  entirely. The intent is to **tighten** this once a post-merge validation run supplies
  the first real number under this metric — never to loosen it when a run fails.

- The denominator is **findings, not distinct categories**, so one prolific
  in-vocabulary category cannot mask thirty drifted findings.
- It is **micro-averaged over the whole run**, not averaged per reviewer — and the
  denominator is only the findings that actually survived. On a *partial* roster
  failure (some reviewers errored, see Behavior above) the rate is computed from the
  survivors alone, and nothing in the payload states how many findings that was: a
  run where 8 of 10 reviewers failed on every case can publish a clean-looking rate
  drawn from two reviewers. Reconstruct the denominator from the `reviewers[]` rows
  as the sum of `findings_raised_avg × runs` before trusting a rate as
  representative.
- A finding with an **empty** category counts as drift. Otherwise the rate would
  improve when a reviewer stopped labelling entirely.
- The key is **absent** (not `0.0`) for a run that raised no findings at all, and
  for any run-result predating the field. Absent means *unmeasured*; an explicit
  `0.0` means *measured and clean*. Do not read one as the other.

It is deliberately **not** carried into the submission envelope: the public board
schema is the same allowlist production uses, and this is a benchmark-run
diagnostic.

`atcr benchmark run --output <path>` produces a conforming run-result; you can also
supply one by hand. `export` reuses the run-result's `generated_at` as the
submission's `submitted_at`, so the same run-result always exports identically.
(`--out` remains a deprecated hidden alias for `--output`.)

Behavior:
- Missing/malformed run-result, or one missing `suite`/`suite_version` → error.
- Any reviewer row not covering the run-result's declared case list → error, unless
  `--allow-partial-coverage` is passed. A run-result with no coverage at all warns
  and proceeds (unmeasured, not short).
- `--suite-path <dir>` (optional) anchors that declared case list to the suite
  manifest: a mismatched `suite`/`suite_version`, a missing denominator, or a case
  list that differs from the manifest's in either direction → error. Without it the
  gate can only prove internal consistency.
- A malformed coverage payload → error: a duplicate `(model, persona)` identity, a
  reviewer row with no coverage row, a `runs` that disagrees with the row's covered
  case count, a repeated case id within a row, or a case id absent from the suite.
  None of these are producible by `atcr benchmark run`, so each means the file was
  assembled by hand.
- `--in` is required. `--output` writes the JSON to a file (`0600`, parents
  created) instead of stdout.

---

## Realized-model attribution and coverage

A reviewer row is keyed by the model that **actually served** each case, not by the
lane it was configured under.

This matters because every quota-limited primary in the registry ships with a
cross-provider fallback *by design*, so a multi-hour suite run crossing a provider
quota boundary is arithmetic, not bad luck. When a lane fails over mid-suite, the
run emits **one row per realized `(model, persona)` pair**, each carrying only the
cases its model served — rather than crediting the whole suite to whichever model
happened to serve case 1.

The failover is not corruption; it is data about the backup model. Scoring it as
itself is what keeps both numbers honest, and `reviewer_coverage` is what makes the
resulting uneven coverage visible instead of silently incomparable.

**One exception, stated rather than hidden:** under `review_strategy: chunked` a
single case is split into bins, and a slot whose bins *partly* failed over produced
that case from two models. The merge keeps only one model id per slot, so such a case
cannot be attributed exactly. It is credited to the **fallback** model, never to the
primary — the primary is the one answer known to be wrong, since it demonstrably did
not serve all of the case. Exact attribution would need per-bin model ids carried
through the chunk merge.

### Reviewer outcomes

`reviewer_coverage[].outcomes` tallies what happened on each case, because a
zero-finding result is otherwise ambiguous — a reviewer that read the diff and
correctly found nothing, one that emitted prose no parser could use, and one whose
call failed all raise zero categories and score identically:

| Outcome | Meaning |
|---------|---------|
| `findings` | Raised at least one parseable finding. |
| `clean` | Reviewed successfully and emitted the `NO FINDINGS` sentinel. |
| `unparseable` | Returned content that parsed to zero findings and was not the sentinel. |
| `truncated` | Response cut off on `finish_reason: length`; whatever it raised is incomplete. |
| `incomplete` | A chunked reviewer saw only a fraction of the diff (some bins failed while the slot still reported ok). |
| `failed` | The call never produced a reviewable response. |
| `unknown` | No outcome was recorded — a checkpoint written before this field existed. |

Precedence when signals overlap is `failed > unparseable > truncated > incomplete >
findings > clean`: data-integrity signals outrank volume signals.

`unknown` is deliberately distinct from `clean`. A resumed run whose checkpoint
predates this field reports `unknown`, never "reviewed and found nothing" — the
honest report is that nobody knows. `fallback_cases` counts cases served by a
fallback model and is tracked separately, since a fallback-served case is
independently clean, unparseable, or failed.

---

## Privacy model

A benchmark submission carries the **same allowlist** as the production export
(`model`, `persona`, and the derived numeric metrics — no `run_id`, no paths, no
keys; see [`docs/scorecard.md` → Privacy Model](scorecard.md#privacy-model)) plus
the suite identity (`source`, `suite`, `suite_version`).

> **Anonymization happens at the producer, with an export-time backstop.** The
> run-result is expected to come from `atcr benchmark run`, whose scorer emits the
> public reviewer schema and re-scrubs each `model`/`persona` via
> `scorecard.ScrubPublicRecord` at source — that producer scrub is the primary
> guarantee, so do not rely on the backstop and do not hand-craft a run-result from
> un-anonymized data. As defense-in-depth, because `benchmark export` consumes a
> hand-suppliable run-result file, `BuildSubmission` re-scrubs the same fields
> again before emitting, so a non-conforming run-result cannot carry PII into a
> public submission. The `PublicRecord` allowlist remains the boundary; the numeric
> metrics are untouched.

---

## Related

- [`docs/scorecard.md`](scorecard.md) — the local scorecard store, the
  `leaderboard --export` production submission, and the shared public reviewer
  schema + privacy model.
- [`benchmarks/standard-v1/`](../benchmarks/standard-v1/) — the curated
  `standard-v1` suite content bundled in this repo, with its attribution in
  [`NOTICE.md`](../benchmarks/standard-v1/NOTICE.md). Regenerate it with
  `go run ./cmd/ingest-alibaba-benchmark` — which refuses to rebuild over an
  already-published `suite_version` unless you pass `-force`.
