# Case `half-up-rounding-misses-credit`

A **resemblance-only** case, authored for epic 35.16.12's measurement gate: the defect sits in a sibling of the changed code that nothing in the change refers to. It is the shape "a parallel code path implementing the same invariant by hand".

## What the case plants

Two functions compute a prorated amount owed back to a customer and round it to the cent: `refund_amount()` in `billing/refund.py` and `downgrade_credit()` in `billing/credit.py`. Both round with `round(x, 2)`. On a `Decimal`, `round()` rounds half to even, so a share of exactly half a cent rounds down half the time.

The change fixes `refund_amount()`. It quantizes to `Decimal("0.01")` with `ROUND_HALF_UP`.

`downgrade_credit()` is untouched (head `billing/credit.py:7`):

```python
return round(share, 2)
```

| Claim in the commit message | What the change actually does |
|---|---|
| `refund_amount()` quantizes half-up to the cent | **Delivered.** |
| "every amount owed back to a customer now rounds half-up, never half-even" | **False.** `downgrade_credit()` is an amount owed back to a customer and still rounds half to even. |

To confirm by hand, apply the diff and run, from the head tree's root. The same 10.625 share comes out differently on the two paths:

```python
from decimal import Decimal
from billing.refund import refund_amount
from billing.credit import downgrade_credit
refund_amount(Decimal("17.00"), 3, 8)      # Decimal('10.63')
downgrade_credit(Decimal("17.00"), 5, 8)   # Decimal('10.62')
```

## Why reference retrieval cannot reach the sibling

Epic 35.16.8's pre-fetch (`internal/payload/prefetch.go`, `buildPrefetch`) takes the symbols that enclose the diff's added lines, then runs one `git grep -F -w` for those names over the head tree, skipping the changed files. Each hit adds the block around it to the payload, and only those blocks become groundable.

- The changed symbol here is `refund_amount`. The added import and the module-level `CENT` assignment enclose in no named declaration. `billing/credit.py` contains neither `refund_amount` nor `CENT` as a whole word (`git grep` is case-sensitive, so "the cent" in its docstring does not match), and it imports nothing from `billing/refund.py`.
- The diff and the commit message do not name `billing/credit.py` or `downgrade_credit`.

So the only thing that links `downgrade_credit()` to the change is that it does the same arithmetic the same way. That is the gap 35.16.12 is about.

This is checked against the real retrieval code by `TestShippedResemblanceCases_SiblingIsUnreachableByReferenceRetrieval` (`cli/benchmark_repostate_resemblance_test.go`). It materializes the case, builds every payload mode on one `RangeBuilder` as a review does, and asserts that `billing/credit.py` is not in the grounding map and that no payload text names the file or `downgrade_credit`.

## Consequence for scoring (read before running the panel)

With only 35.16.8 retrieval, **this case cannot be won**. The sibling is not in the payload, and the Epic 14.1 grounding gate drops any finding on `billing/credit.py` before scoring, even a correct guess. A miss is guaranteed by construction. That is what the gate asks for, but a miss does not tell you whether a reviewer would find the defect if similarity retrieval showed it the sibling.

## Expected findings

1. **`credit-still-rounds-half-even`**: `billing/credit.py:7`, `outside_diff: true`. The diff touches `billing/refund.py` only.

## Files

| File | Role |
|---|---|
| `base/billing/refund.py` | The rounding path the change fixes. |
| `base/billing/credit.py` | The sibling rounding path that still rounds half to even. The finding settles here. |
| `commit-message.txt` | The claim source, including the false "every amount owed back" claim. |
| `change.diff` | Switches `refund_amount()` to `quantize(..., ROUND_HALF_UP)`. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
