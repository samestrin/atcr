# `atcr dedupe` / `td-dedupe` parity differences

`cli/dedupe_parity_test.go` runs `atcr dedupe` over the streams in `streams/` and
compares the decoded JSON with `<case>.golden.json`, the output captured from
`llm-support td-dedupe` for the same streams. Every case below names the
llm-tools test whose fixture streams it reuses, so the parity claim is pinned to
llm-tools' own fixtures rather than invented ones.

The goldens are the `td-dedupe` output **verbatim**. The test reads the
"Allowed deviations" table below and applies it to the golden before comparing,
so a difference that is not a row in that table fails the test, and a row whose
value never appears in its case's golden also fails it (a stale row cannot
outlive the deviation it describes).

## Provenance

- `llm-support` v1.5.0 (`td-dedupe`), `llm-tools` at `bee4124` (2026-09-12).
- Fixtures copied from `internal/support/commands/td_dedupe_test.go`,
  `td_dedupe_header_test.go` and `td_dedupe_cmd_test.go` on that commit.
- Each golden was produced with:

  ```bash
  llm-support td-dedupe \
    --streams cli/testdata/dedupe/streams/<a>.txt,cli/testdata/dedupe/streams/<b>.txt \
    --source-tags <tag-a>,<tag-b> --tolerance 3 --json \
    > cli/testdata/dedupe/<case>.golden.json
  ```

  adding `--untrusted <tags>` for the `untrusted-source` case only. The streams
  are passed in the order `<case>.golden.json` records in `summary.sources`, and
  the tags are the ones the llm-tools test gave the stream.

## Allowed deviations

| Case | Field | `td-dedupe` | `atcr dedupe` | Why |
|---|---|---|---|---|
| `member-category-normalized` | category | `performance` | `other` | D2: `ReadStream` canonicalizes each row's CATEGORY; the alias word `PERF` is not in atcr's vocabulary, so the pooled vote and the member carry `other` |
| `member-category-normalized` | member category | `performance` | `other` | D2: the member keeps the same canonical word the vote counted, as `td-dedupe` did |
| `member-category-normalized` | member category | `error-handling` | `other` | D2: `ERROR_HANDLING` is not canonicalized to `error-handling` (atcr folds case and plurals, not `_`), so the unknown word becomes `other` |
| `category-aliases` | category | `performance` | `other` | D2: the alias word `perf` is not in atcr's vocabulary, so it becomes `other` |
| `category-aliases` | category | `testing` | `other` | D2: the alias word `tests` is not in atcr's vocabulary, so it becomes `other` |
| `category-aliases` | category | `cross-cutting` | `other` | D2: an unknown word becomes `other` instead of being passed through |
| `category-out-of-scope-cased` | category | `out-of-scope` | `other` | D2: a casing variant (`Out-Of-Scope`) is not the exact `out-of-scope` control token, so it is an unknown word and becomes `other` |
| `members-across-widths` | category | `security` | `performance` | modal vote: the cluster has a 1–1 tie, `td-dedupe` breaks it by stream order and atcr's `reconcile.ModalCategory` breaks it alphabetically, so the result cannot depend on which stream was read first |
| `format-blocking-column` | source | `post-review` | `post` | `source` is the stream's `--source-tags` tag (here the parent directory name), not the row's `SOURCE` column; a v1 stream's eighth column is REVIEWER, so the tag is the only value every layout has |
| `format-evidence-source` | source | `execute-epic` | `epic` | same: `source` names the stream, not the skill that wrote the row |
| `panel-wide-confidence` | confidence | `HIGH` | `MEDIUM` | `atcr dedupe` merges through `reconcile.Merge`, whose panel-wide-instruction discount lowers `HIGH` to `MEDIUM` for a 2+ reviewer `invariant` cluster whose quotes are not independent |

## The two families, in prose

**Category spelling (D2).** `atcr dedupe` canonicalizes every row's CATEGORY at
the parse boundary with `internal/stream.CanonicalCategory` (board answer on
EST-5476), so the merge vote counts canonical words. `td-dedupe` used its own
alias table instead. The two agree on every spelling in the fixtures except the
rows above: the alias folds (`perf`, `tests`), `_`-joined words, casing variants
of the reserved `out-of-scope` token, and any word outside atcr's closed
vocabulary all become `other`. `doc`/`documentation` → `docs` agrees and is not
listed. The README and `docs/technical-debt.md` call this out for callers: a
category may arrive as `other` where `td-dedupe` echoed the reviewer's word.

**Confidence.** `atcr dedupe` reuses the library's `Merge`, so it also inherits
the panel-wide-instruction discount: a cluster of 2+ reviewers that all quote one
panel-wide instruction (same file, same category `invariant`, mutually
redundant evidence) is `MEDIUM`, not `HIGH`. No llm-tools fixture is in that
class, so `panel-wide-confidence` is a fixture written for this difference; its
two rows carry identical evidence and the `invariant` category.

`source` and the modal tie-break are consequences of the same design decisions
(D4's "the tag names the stream", and reusing `reconcile.ModalCategory`); they
are listed here because the parity test sees them, not because the command
special-cases them.