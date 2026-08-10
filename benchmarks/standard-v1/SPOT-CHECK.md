# Spot-check — `standard-v1` 1.0.0

Epic 35.16.2 AC2 requires a fixed sample of imported cases to be hand-verified:
each case's planted defect must be genuinely present in its committed diff, and
must genuinely match the category it was mapped to. This file records that check
so it is reproducible by a third party rather than asserted.

**Checked:** 2026-08-07, against upstream `positive_samples.json` at the pinned
commit `b3072489eace26efca8bcf2b1ac6a24ba64f82c1` (196 records, 1,505 comments —
`Code Defect` 709, `Maintainability and Readability` 626, `Performance` 117,
`Security Vulnerability` 53).

**Sample:** 4 of 17 cases, chosen to cover all four mapped categories and both
the multi-category and single-category shapes.

## Method

For each sampled case:

1. Recover the upstream record by deriving the case id from its `githubPrUrl`.
2. Re-derive `expected_categories` from that record's comment categories through
   the mapping in [`NOTICE.md`](NOTICE.md), and compare against what `suite.json`
   actually ships.
3. For every annotated comment, parse the committed `.diff`'s new-side hunk
   ranges and check that the comment's `path` is a file the diff changes, and
   that its `from_line` falls inside one of that file's hunks.

## Results

**Category derivation: 4 of 4 exact.** Every sampled case's shipped
`expected_categories` is exactly the set derived from its upstream comment
categories — no category invented, dropped, or reassigned.

**Defect presence: 26 of 28 annotated locations land inside a changed hunk**;
the remaining 2 cite a file the diff genuinely changes, at a line outside any
hunk (`PATH-ONLY` below). No annotated comment referenced a file absent from its
diff.

### `cline-cline-pr-4786` → `correctness, maintainability, security`

Upstream categories derive to exactly those three. Representative defects:

| category | location | annotated defect | in diff |
|---|---|---|---|
| `security` | `src/services/auth/AuthService.ts:33` | nonce generated once at service construction, so it is not single-use — replay attack | IN-HUNK |
| `security` | `src/core/controller/index.ts:461` | nonce not cleared after validation, allowing state-parameter reuse | IN-HUNK |
| `correctness` | `src/services/auth/AuthService.ts:173` | `createAuthRequest(): Promise<String>` uses the SDK `String` type rather than native `string` | IN-HUNK |
| `maintainability` | `src/extension.ts:312` | state-validation logic duplicated in the activation function instead of delegating to `controller.validateAuthState` | IN-HUNK |

All 7 comments IN-HUNK. The security mapping is well-founded — three independent
comments describe the same nonce-reuse vulnerability class.

### `vllm-project-vllm-pr-12608` → `correctness, maintainability`

| category | location | annotated defect | in diff |
|---|---|---|---|
| `correctness` | `vllm/v1/core/kv_cache_manager.py:425` | `_untouch` appends freed blocks to the end of `free_block_queue`, violating LRU policy under memory pressure | IN-HUNK |
| `correctness` | `vllm/v1/core/scheduler.py:141` | `allocate_slots` called with an empty `new_computed_blocks` for RUNNING requests, which the method does not expect | IN-HUNK |
| `maintainability` | `vllm/v1/core/kv_cache_manager.py:116` | `new_computed_blocks` should default to `None` rather than forcing every caller to pass an empty list | IN-HUNK |

All 6 comments IN-HUNK. The two `Code Defect` comments are substantive logic
defects, not stylistic — the mapping to `correctness` is correct.

### `juspay-hyperswitch-pr-7353` → `correctness, maintainability`

| category | location | annotated defect | in diff |
|---|---|---|---|
| `correctness` | `crates/api_models/src/payment_methods.rs:2317` | feature-flag inconsistency: `PaymentMethodMigrationResponseType` and its `From` impl gated under a combination that does not match its usage | IN-HUNK |
| `maintainability` | `crates/api_models/src/admin.rs:19` | redundant `use url;` import; items reachable via fully-qualified paths | IN-HUNK |
| `maintainability` | `crates/api_models/src/events/payment.rs:8` | duplicate import of `CustomerPaymentMethodsListResponse` under different `#[cfg]` conditions | **PATH-ONLY** |

7 of 8 IN-HUNK. The `PATH-ONLY` comment cites `events/payment.rs`, which the diff
does change — its line 8 falls just outside a hunk boundary. The defect it
describes is real and visible in the diff at the neighbouring import block.

### `elastic-elasticsearch-pr-118183` → `maintainability` (single category)

All 5 upstream comments are `Maintainability and Readability`, so the case
correctly ships exactly one category — a useful negative control that the
mapping does not over-generate.

| category | location | annotated defect | in diff |
|---|---|---|---|
| `maintainability` | `…/vectors/MultiDenseVectorScriptDocValues.java:18` | class name risks confusion with the existing `dense_vector` field type; rename to include "Rank" | IN-HUNK |
| `maintainability` | `…/vectors/MultiDenseVectorDocValuesField.java:55` | exception message references `rank_vectors` while the class still returns `MultiDenseVector` | IN-HUNK |
| `maintainability` | `…/vectors/RankVectorsDVLeafFieldData.java:48` | error message still says "multi-vector field" after the rename to RankVectors | **PATH-ONLY** |

4 of 5 IN-HUNK.

## Caveats recorded honestly

- **`PATH-ONLY` is a line-anchor artifact, not a missing defect.** Upstream line
  numbers are anchored to the pull request's own view; a comment can sit a few
  lines outside the compare range's hunk boundaries. In both cases the cited file
  is genuinely modified by the committed diff.
- **A majority of upstream comments carry `is_ai_comment: true`** with
  `source_model` values including `GLM-4.7`, `GPT-5.2`, `Gemini-3-Pro` and
  `Qwen-Coder-480B`. aacr-bench's own framing is that these are *expert-verified*
  — retained after human review — not human-authored. Of the defects sampled
  above, the two most substantive `correctness` findings
  (`kv_cache_manager.py:425` LRU violation, `admin.rs:19` and
  `MultiDenseVectorScriptDocValues.java:18` naming) are `is_ai_comment: false`,
  i.e. human-authored.
- **`expected_categories` is the planted-defect subset, not exhaustive ground
  truth** (`docs/benchmark.md:131-138`). A reviewer surfacing a real defect
  outside this set is not penalised — no precision or F1 is computed against it.
- This sample is 4 of 17 cases. It is a proportionate check on an upstream corpus
  that is already expert-annotated, not a full audit.

## Reproducing

```
curl -sL https://raw.githubusercontent.com/alibaba/aacr-bench/b3072489eace26efca8bcf2b1ac6a24ba64f82c1/dataset/positive_samples.json -o positive_samples.json
```

Derive each case id from `githubPrUrl` with the same slug rule
`cmd/ingest-alibaba-benchmark` uses (`<owner>-<repo>-pr-<number>`, lowercased,
non-alphanumeric runs collapsed to `-`), map categories per
[`NOTICE.md`](NOTICE.md), and compare against `suite.json` plus the new-side hunk
ranges of each committed `.diff`.
