# NOTICE — `fit-v1` benchmark suite

**Not for leaderboard submission.** This suite exists for `atcr benchmark fit`:
it judges whether a (persona, model) pair produces healthy calls on a payload
large enough to split into chunks. It does not measure review quality. Do not
`benchmark export` a `fit-v1` run-result as a submission; its recall is
meaningless.

## Provenance

The one case, `atcr-4c588b6b-8576a42.diff`, is atcr's own public history: the
range `4c588b6b..8576a42` with every Markdown file left out. It was produced
from this repository with exactly:

```
git diff 4c588b6b..8576a42 -- . ':!*.md'
```

It is 502,071 bytes and 10,874 lines across 112 files.

## The case is not scored

`suite.json` gives the case a single `expected_categories` entry,
`correctness`, only because the manifest format requires a non-empty list. It
is a placeholder, not a planted defect or a recall target.

## Running it

A `fit-v1` run needs `review_strategy: chunked`. The embedded default is
`bulk`, under which a 128k-window reviewer cannot hold the case: it sheds files
and every lane reads `incomplete`, which hides the fit signal. Under `chunked`
a 128k-window reviewer splits the case into 2 chunks.

`payload_byte_budget` must be at least 502,071 bytes; the default of 524,288
is enough. A lower budget truncates the case and marks every lane `incomplete`.

`cli/benchmark_fit_suite_test.go` pins both facts: it fails if a 128k-window
reviewer ever reviews the case in one chunk or trips the byte budget.
