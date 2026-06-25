# Benchmark Suite

`atcr benchmark` is the standard-suite tooling that feeds the public **Model-Eval
Leaderboard** (Epic 10.0). A benchmark suite is a versioned, fixed set of diff
cases with planted defects; running the same suite across models yields
*comparable* scores, and only suite-sourced submissions are eligible for the
public board — so cherry-picked production runs cannot game it.

This page documents the **bounded in-repo half** that ships today:

- the **suite-manifest contract** (`internal/benchmark`),
- `atcr benchmark verify` — validate a suite and print its reproducibility hash,
- `atcr benchmark export` — emit a suite-tagged public submission record.

> **What is NOT here yet.** Live execution + scoring (`atcr benchmark run`) is
> **Epic 10.1**: it needs a new diff-file→payload ingestion path (the review
> pipeline is built around git ranges, not loose diff files) and the scoring
> rubric. The curated `standard-v1` suite **content** lives in the external
> `github.com/atcr/benchmark-suite` repo (it is not bundled here). Until those
> land, `verify` and `export` operate against any suite directory and run-result
> file that satisfy the contracts below.

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
| `cases[].expected_categories` | string[] | Required, at least one. The planted-defect categories a competent reviewer should surface. (Consumed by the scoring engine in Epic 10.1.) |

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

## `atcr benchmark export --in <run-result.json> [--output <path>]`

Emit a **suite-tagged** public submission record from a suite run-result.

```bash
atcr benchmark export --in ~/.config/atcr/benchmark/run-2026-06-24.json
atcr benchmark export --in run.json --output /tmp/submission.json
```

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

### The run-result contract

`export` reads a **run-result** file rather than your local scorecard — so a
production run can never be passed off as a suite submission. A run-result is:

```json
{
  "suite": "standard-v1",
  "suite_version": "1.0.0",
  "generated_at": "2026-06-24T12:00:00Z",
  "reviewers": [ /* public reviewer rows */ ]
}
```

`atcr benchmark run` (Epic 10.1) produces these under
`~/.config/atcr/benchmark/<run-id>.json`. Until then, you supply a conforming
file via `--in`.

Behavior:
- Missing/malformed run-result, or one missing `suite`/`suite_version` → error.
- `--in` is required. `--output` writes the JSON to a file (`0600`, parents
  created) instead of stdout.

---

## Privacy model

A benchmark submission carries the **same allowlist** as the production export
(`model`, `persona`, and the derived numeric metrics — no `run_id`, no paths, no
keys; see [`docs/scorecard.md` → Privacy Model](scorecard.md#privacy-model)) plus
the suite identity (`source`, `suite`, `suite_version`).

> **Anonymization happens at the producer, with an export-time backstop.** The
> run-result is expected to come from `atcr benchmark run` (Epic 10.1), whose
> scorecard aggregation scrubs identity strings at source, exactly like
> `leaderboard --export` — that producer scrub remains the primary guarantee, so
> do not rely on the backstop and do not hand-craft a run-result from
> un-anonymized data. As defense-in-depth, because `benchmark export` consumes a
> hand-suppliable run-result file, `BuildSubmission` additionally re-scrubs each
> reviewer's `model`/`persona` via `scorecard.ScrubPublicRecord` before emitting,
> so a non-conforming run-result cannot carry PII into a public submission. The
> `PublicRecord` allowlist remains the boundary; the numeric metrics are untouched.

---

## Related

- [`docs/scorecard.md`](scorecard.md) — the local scorecard store, the
  `leaderboard --export` production submission, and the shared public reviewer
  schema + privacy model.
- `github.com/atcr/benchmark-suite` — the external repo holding the curated
  `standard-v1` suite content (Task 3).
- Epic 10.1 (`benchmark run`) — live execution against suite cases + scoring.
