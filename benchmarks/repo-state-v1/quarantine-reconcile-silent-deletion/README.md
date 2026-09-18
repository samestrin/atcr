# Case `quarantine-reconcile-silent-deletion`

The case that motivated this tier. It is the shape described in epic 35.16.10's Context & Rationale: the changed file is the store, and the defect is in a reconcile loop in a different, unchanged file.

## What the case plants

The change makes `read_pending()` survive a corrupt store: the damaged file is renamed aside and the function returns `[]` instead of raising `CorruptStore`. Read on its own, every added line is correct — the quarantine works, the rename is sensible, the morning job stops crashing.

The consequence lands somewhere the diff never touches. `queue/evening.py` is unchanged, and its `bump_stale_evening_to_today()` reconciles the evening queue against the pending store using one rule: a queue entry whose pending record is absent was answered overnight, so `delete_entry()` removes it durably.

Before the change, a corrupt store raised and the job stopped. After it, a corrupt store reads as empty, every `pending_id` looks absent, and the reconcile loop deletes **every confirmed escalation** — permanently, and silently. The change converted a loud crash into irreversible data loss.

| Claim in the commit message | What the diff actually does |
|---|---|
| A corrupt store is renamed aside for inspection | **Delivered.** `_quarantine()` does exactly that. |
| `read_pending()` returns an empty list instead of raising | **Delivered**, and this is the defect's mechanism rather than a defect in itself. |
| "the morning job now survives a damaged store and processes what it can" | **False in the way that matters.** The job survives; what it "processes" is the deletion of every escalation it was supposed to protect. |

## Why it is hard

The settling line, `if entry["pending_id"] not in live_ids:`, is in a file the diff does not mention at all. It is not context in the diff — it is absent from the diff entirely. A reviewer reading only added and removed lines cannot reach it by any route.

Reaching it requires following the changed symbol `read_pending` to its caller. That is precisely the retrieval epic 35.16.8 added, which is why this case measures it: the finding is reachable if and only if the reviewer consulted repository state beyond the diff.

It is also the reason `base/` includes `queue/evening.py` at all. Prune that file away and the case becomes unwinnable rather than hard — the failure mode this tier exists to detect, reproduced in the tier's own fixtures.

## Expected findings

1. **`reconcile-deletes-on-quarantined-store`** — `queue/evening.py:18-19`, `outside_diff: true`. The finding this case exists for.
2. **`quarantine-reports-corruption-as-empty`** — `store/pending.py:30-32`, `outside_diff: false`. Reachable from the diff alone: the `return []` is an added line. It is the same defect seen from the shallow end, and a reviewer that reports only this one has described the mechanism without noticing the consequence.

## Files

| File | Role |
|---|---|
| `base/store/pending.py` | The store before the change, raising `CorruptStore` on a bad decode. |
| `base/queue/evening.py` | The only caller of `read_pending()`, and where the defect settles. Do not prune. |
| `commit-message.txt` | The claim source. |
| `change.diff` | Quarantines and returns `[]`. Does not touch the reconcile loop. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
