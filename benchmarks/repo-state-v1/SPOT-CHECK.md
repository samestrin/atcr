# Spot-check — `repo-state-v1` 1.1.2

> Revised 2026-09-17 at `suite_version` 1.1.1 (tolerance and summary corrections, recorded below) and 1.1.2 (the two `error_handling` categories corrected to `error-handling` — the closed vocabulary's member is hyphenated and normalize() folds no separators, so no reviewer could ever have raised them; half the suite's category-recall denominator was unwinnable as spelled). The original hand-check and the AC8 panel run were performed at 1.1.0; **the recorded AC8 numbers predate all of these corrections — in particular the category-recall column is not comparable with a run against 1.1.2**, because the corrected expectations can now be satisfied at all.

[`../standard-v1/SPOT-CHECK.md`](../standard-v1/SPOT-CHECK.md) records the invariant that makes that suite trustworthy: *each case's planted defect must be genuinely present in its committed diff.* This tier exists precisely because that invariant excludes the defect class it targets, so it needs its own, and a stricter one:

> Each case's planted defect must be genuinely present at the cited file and line of the **materialized head tree**, and each finding's `outside_diff` value must be true of the case's **own diff**.

The second clause is the one with teeth. An `outside_diff: true` finding whose cited line is actually an added line would score the tier's headline metric while measuring nothing — the tier would report success at detecting out-of-diff reasoning from a reviewer who had read only the diff.

**Checked:** 2026-09-16, against all 4 cases at `suite_version` 1.1.0. Unlike `standard-v1`, this is not a sample: the suite is small enough to check exhaustively, and every case is hand-written rather than imported, so there is no upstream record to recover.

## Method

Three of the four checks are **mechanical and enforced by the Go test suite**, so they cannot rot silently. The fourth is genuinely manual.

| # | Check | How |
|---|---|---|
| 1 | The case loads, and every file reference resolves and stays inside the case directory | `TestLoadRepoState_ShippedSuiteLoads` (`internal/benchmark/repostate_test.go`) |
| 2 | The diff applies cleanly to `base/`, and every `expected_findings[].file` exists in the resulting head tree | `TestMaterializeCase_ShippedCaseMaterializes` (`internal/benchmark/materialize_test.go`) |
| 3 | Every `outside_diff: true` finding's cited line is genuinely **not** an added line of that case's own diff | `TestParseDiffLineMap_ShippedCasesOutsideDiffValuesAreTrue` (`internal/benchmark/difflines_test.go`) |
| 4 | The **pruning check**: the file that consumes the changed symbol survived the prune, so the case is hard rather than unwinnable | By hand, recorded per case below |

Check 4 cannot be automated, and it is the one that matters most. `FORMAT.md` asks a base tree to be pruned to what a reviewer genuinely needs. Prune away the consumer of the changed symbol and the case silently stops being winnable by anyone — the exact defect class this tier exists to detect, reproduced in the tier's own fixtures. Nothing about a case that has been over-pruned looks wrong: it parses, it materializes, it scores everyone zero, and it reads as a very hard case.

## Results

### `claim-absent-cursor-fix` — 2 findings (1 outside, 1 within)

| Finding | Location (head) | `outside_diff` | Verified |
|---|---|---|---|
| `begin-still-wipes-the-cursor` | `streamer/cursor.py:46` | `true` | `begin()` appears in the diff only as **context**. The diff's added lines are the `_safe_drain_offset` helper; line 46 is not among them. |
| `test-named-for-the-fix-asserts-the-helper` | `tests/test_cursor.py:8` | `false` | The whole test file is added by the diff, so line 8 is an added line. |

**Pruning check: passes.** `base/streamer/drain.py` is the only caller of `begin()` and is present, so the blast radius of the unfixed bug is visible in the tree.

### `quarantine-reconcile-silent-deletion` — 2 findings (1 outside, 1 within)

| Finding | Location (head) | `outside_diff` | Verified |
|---|---|---|---|
| `reconcile-deletes-on-quarantined-store` | `queue/evening.py:18-19` | `true` | The diff touches `store/pending.py` only. `queue/evening.py` is absent from the diff entirely — not even context. |
| `quarantine-reports-corruption-as-empty` | `store/pending.py:30-32` | `false` | All three lines (`except ValueError:`, `_quarantine(STORE_PATH)`, `return []`) are added by the diff. |

**Pruning check: passes.** `base/queue/evening.py` calls the changed symbol `read_pending` and contains the reconcile loop the case is about. It is the single file whose removal would make this case unwinnable, and its `README.md` says so explicitly.

### `mock-outlives-the-contract` — 2 findings (2 outside, 0 within)

| Finding | Location (head) | `outside_diff` | Verified |
|---|---|---|---|
| `mock-still-returns-a-rate-for-any-plan` | `tests/test_invoice.py:4-6` | `true` | The diff touches `billing/rates.py` only; the test file is unchanged. |
| `invoice-total-never-catches-unknown-plan` | `billing/invoice.py:11` | `true` | Same — `billing/invoice.py` is not in the diff. |

**Pruning check: passes.** Both `base/tests/test_invoice.py` and `base/billing/invoice.py` name the changed symbol `lookup_rate` and are present.

**Recorded honestly:** this case has **no** `outside_diff: false` finding. That is deliberate — every added line in its diff is correct, so a reviewer reading only the diff has nothing to report — but it means this case alone cannot distinguish a reviewer that read repository state from one that reported nothing at all. Read its within-diff column together with the other three cases, not on its own.

### `incomplete-predicate-guard` — 2 findings (1 outside, 1 within)

| Finding | Location (head) | `outside_diff` | Verified |
|---|---|---|---|
| `guard-omits-webhook` | `router/dispatch.py:24-25` | `false` | Both lines are added by the diff: the guard and its `raise`. |
| `intake-aborts-the-whole-batch` | `router/intake.py:8` | `true` | The diff touches `router/dispatch.py` only. |

**Pruning check: passes.** `base/router/intake.py` calls the changed symbol `dispatch` and is present.

## Suite balance

| | Findings |
|---|---|
| `outside_diff: true` | 5 |
| `outside_diff: false` | 3 |
| **Total** | **8** |

Both shapes are represented in the suite and in three of the four cases individually, so out-of-diff recall and within-diff recall each have a real denominator.

## AC8 — the panel result

Epic 35.16.10's AC8 requires that **at least one case is currently failed by the panel**. A tier where everything already passes measures nothing. This is verified out-of-band by a real run, not by a unit test, and the result is recorded here.

<!-- AC8-RESULT:begin -->

**Run recorded 2026-09-16**, `suite_version` 1.1.0, against the registered 11-reviewer panel via the `litellm` proxy. Each case was run as its own single-case suite so the outcome is attributable per case. **Sibling epics landed at run time: 35.16.7 (claim ledger), 35.16.8 (context-aware pre-fetching) and 35.16.9 (predicate-exhaustiveness persona rule) — all three.** A run recorded before 35.16.9 is not directly comparable, because that epic changed every persona's prompt.

**AC8 is satisfied, and not narrowly.** Three of the four cases were solved completely by **nobody**, and the out-of-diff half was missed by the entire panel on three of four.

| Case | Expected (out / in) | Solved fully | Any out-of-diff hit | Any within-diff hit |
|---|---|---|---|---|
| `claim-absent-cursor-fix` | 2 (1 / 1) | **3 / 11** | 3 / 11 | 7 / 11 |
| `quarantine-reconcile-silent-deletion` | 2 (1 / 1) | **0 / 11** | **0 / 11** | 6 / 11 |
| `mock-outlives-the-contract` | 2 (2 / 0) | **0 / 11** | 1 / 11 | — |
| `incomplete-predicate-guard` | 2 (1 / 1) | **0 / 11** | **0 / 11** | **1 / 11** |

Across all four cases, out-of-diff recall was **4 hits out of 55 reviewer-findings** (11 reviewers × 5 out-of-diff findings). No reviewer scored more than one.

### What the run says, beyond passing AC8

**The tier measures what it was built to measure.** On `claim-absent-cursor-fix` the two halves separate cleanly: 7 of 11 reviewers found the in-diff finding and only 3 found the out-of-diff one. A blended single score would have reported those seven reviewers as middling; the separated number says precisely which capability they lack. That separation is the epic's whole argument, and it is visible in the first case.

**`quarantine-reconcile-silent-deletion` is currently unwon.** Six of eleven reviewers found the shallow half — that `read_pending()` now reports corruption as emptiness — and **not one** followed it into `queue/evening.py` to see that the reconcile loop turns that emptiness into permanent deletion of confirmed escalations. This is the exact finding that motivated the tier, reproduced as a measurement: the panel can see the mechanism and still miss the consequence.

**A prediction in this suite was wrong, and the run is how we know.** `incomplete-predicate-guard`'s README called it "the easier of this suite's cases" because its primary finding sits on an added line, fully inside the diff. Exactly **one** reviewer found it. Being inside the diff turns out not to make a finding easy when the defect is an omission — the guard lists three kinds, `KINDS` lists four, and nothing about the added lines looks wrong on its own. The case README has been corrected to record the measurement rather than the prediction.

**Nothing here is safe to read as a model ranking.** Eleven reviewers over four cases is far too small a sample, several rows were served by a fallback model, and the personas differ by design. Read the columns, not the rows.

<!-- AC8-RESULT:end -->

## Caveats recorded honestly

- **The Epic 14.1 grounding gate is ON during a repo-state run, by design.** A finding whose cited file the patch never touched is dropped before scoring unless context-aware pre-fetching (epic 35.16.8) actually retrieved the cited span. The real reachability rule is narrower than "calls a changed symbol": pre-fetch retrieves with `git grep -F -w` over the changed symbols (`internal/payload/prefetch.go`), so a finding is reachable when its function body contains a whole-word reference to a changed symbol — a function whose NAME merely contains the symbol, or that calls nothing at all, is not. One case leans on exactly that distinction: in `mock-outlives-the-contract`, `fake_lookup_rate` calls nothing (it is a stub returning a literal), and `git grep -w lookup_rate` does not match the name `fake_lookup_rate` (underscore is a word character); the only in-body hit inside the expected 4–6 window is the word `lookup_rate` in the stub's **docstring** on line 5. The case's survival of the gate rests on that docstring reference, not on a call. A case whose out-of-diff finding sat somewhere pre-fetching could never reach would be unwinnable, not hard.
- **Persona changes break run-to-run comparability.** Epic 35.16.9 edited every persona file, so a result recorded before it is not directly comparable with one recorded after. Every stored result below names which sibling epics had landed.
- **`--checkpoint` is not supported for this tier.** `atcr benchmark run` rejects the flag on a `repo-state-v1` suite rather than accepting and ignoring it (`checkRepoStateFlags`, `cli/benchmark_repostate.go`). This supersedes the epic 35.16.10 plan's AC8 instruction to "run it in the background with `--checkpoint"" — that instruction is impossible as written for this tier, and a reader following it gets an error on the first invocation. **The recorded AC8 run below was driven in the foreground with no checkpoint and no resume**, in one uninterrupted pass.
- **The trees are synthetic.** See [`NOTICE.md`](NOTICE.md). No case contains third-party code, and no case is derived from a real defect in a real repository — each is hand-built to plant one specific shape.

## Known unplanted defects (recorded, not removed)

Each case carries a few real, unplanted defects in its head tree, outside every expected window. They absorb reviewer attention and findings budget, so part of any low recall number is competition rather than blindness. Recorded here rather than removed, because editing the fixtures would invalidate the recorded AC8 run:

- `quarantine-reconcile-silent-deletion`: `CorruptStore` (head `store/pending.py:9`) is now unraisable dead code — the diff removed its only raise. `_quarantine` (head line 15) does an unguarded `os.rename` that clobbers any prior `.corrupt` file and leaks `OSError` out of a function documented as making the job survive.
- `mock-outlives-the-contract`: `base/billing/rates.py:6-9` places the new exception class with one blank line before `RATES`, diverging from the file's spacing convention.

## Reproducing

```sh
# Checks 1-3, mechanical:
go test ./internal/benchmark/ -run 'Shipped'

# Check 4 and AC8, a real panel run (exceeds ten minutes; foreground — this tier
# cannot resume: --checkpoint is refused on a repo-state-v1 suite):
atcr benchmark run --suite-path benchmarks/repo-state-v1 --output /tmp/repo-state-run.json
```
