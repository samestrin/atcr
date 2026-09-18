# Case `mock-outlives-the-contract`

A mock-fidelity case: the change tightens a real implementation, and an unchanged test stub keeps asserting the old behavior. The suite stays green while the contract it claims to cover no longer exists.

## What the case plants

The change makes `lookup_rate()` raise `UnknownPlan` instead of falling back to `DEFAULT_RATE`. That is a reasonable change and the added lines are correct.

Two unchanged files make it a defect:

- `tests/test_invoice.py` monkeypatches `lookup_rate` with `fake_lookup_rate`, which returns `2.5` for **every** plan. That stub never matched the function it stands in for: the base `lookup_rate` billed `DEFAULT_RATE` (1.0) for an unknown plan, so a faithful stub would have failed the test's `10.0` assertion. `test_unknown_plan_still_bills` only ever passed against a stub that was already unfaithful — and the change (raising `UnknownPlan` instead) makes that stub actively misleading about the unknown-plan path. The commit message's claim that "the unknown-plan path stays covered by the existing invoice tests" is false, and the passing suite is the evidence that hides it.
- `billing/invoice.py` calls `lookup_rate` with no unknown-plan branch, and its docstring still asserts that "every plan resolves to a rate". The newly raised exception escapes to its callers.

| Claim in the commit message | What the diff actually does |
|---|---|
| `lookup_rate()` raises `UnknownPlan` instead of falling back | **Delivered.** |
| "the unknown-plan path stays covered by the existing invoice tests" | **False.** The only test of that path replaces the function under test with a stub that still returns a default. |

## Why it is hard

Both settling lines are in files the diff does not touch, and the signal that something is wrong is an **absence**: no test was updated, and no caller grew a handler. A green test suite actively argues against the finding.

The mock is reachable only by following the changed symbol `lookup_rate` to the places that name it — one of which is a stub that lies about it. That is the retrieval shape epic 35.16.8's AC6 was expected to absorb, which is what this case measures.

## Expected findings

1. **`mock-still-returns-a-rate-for-any-plan`** — `tests/test_invoice.py:4-6`, `outside_diff: true`. A stub that never matched the contract, and that the change makes actively misleading.
2. **`invoice-total-never-catches-unknown-plan`** — `billing/invoice.py:11`, `outside_diff: true`. The production consequence of the same change.

Both are `outside_diff: true`, deliberately: this case has no shallow end. A reviewer reading only the diff has nothing to report, because every added line is correct.

## Files

| File | Role |
|---|---|
| `base/billing/rates.py` | The rate lookup before the change, with the `DEFAULT_RATE` fallback. |
| `base/billing/invoice.py` | A caller with no unknown-plan branch. Do not prune. |
| `base/tests/test_invoice.py` | The stub that outlives the contract. Do not prune. |
| `commit-message.txt` | The claim source, including the false coverage claim. |
| `change.diff` | Raises `UnknownPlan`. Touches neither the caller nor the test. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
