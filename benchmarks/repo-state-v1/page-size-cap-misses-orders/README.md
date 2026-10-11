# Case `page-size-cap-misses-orders`

A **resemblance-only** case, authored for epic 35.16.12's measurement gate: the defect sits in a sibling of the changed code that nothing in the change refers to. It is the shape "a second handler that needs the same guard the diff adds to the first".

## What the case plants

Two list endpoints read a page size from the client: `list_users()` in `api/users.py` and `list_orders()` in `api/orders.py`. Each parses `?limit=` by hand and passes it straight into a `LIMIT ?` query.

The change adds `MAX_PAGE_SIZE` to `api/paging.py` and clamps `list_users()`:

```python
limit = max(1, min(limit, MAX_PAGE_SIZE))
```

`list_orders()` is untouched (head `api/orders.py:7-8`). `?limit=10000000` still reads the whole orders table, and `?limit=-1` does too, because SQLite reads a negative `LIMIT` as no limit.

| Claim in the commit message | What the change actually does |
|---|---|
| `list_users()` clamps the requested limit to `1..MAX_PAGE_SIZE` | **Delivered.** |
| `MAX_PAGE_SIZE` (200) is defined once, in `api/paging.py` | **Delivered.** |
| "a client can no longer make a list endpoint read an unbounded page" | **False.** `list_orders()` is a list endpoint and still passes the client's limit through. |

To confirm by hand, apply the diff and run, from the head tree's root:

```python
from api.orders import list_orders
class DB:
    def fetch(self, sql, limit): return limit
list_orders(DB(), {"limit": "10000000"})   # 10000000
list_orders(DB(), {"limit": "-1"})         # -1
```

## Why reference retrieval cannot reach the sibling

Epic 35.16.8's pre-fetch (`internal/payload/prefetch.go`, `buildPrefetch`) takes the symbols that enclose the diff's added lines, then runs one `git grep -F -w` for those names over the head tree, skipping the changed files. Each hit adds the block around it to the payload, and only those blocks become groundable.

- The changed symbol here is `list_users`. The `MAX_PAGE_SIZE` line in `api/paging.py` is a module-level assignment, which encloses in no named declaration. `api/orders.py` contains neither `list_users` nor `MAX_PAGE_SIZE`.
- `api/orders.py` does import `DEFAULT_PAGE_SIZE` from `api/paging.py`, a file the diff changes. That is a reference *from* the sibling *to* an unchanged name. Pre-fetch searches only for changed symbols, and the diff does not touch the `DEFAULT_PAGE_SIZE` line, so the import does not make the sibling reachable. This is the closest call among the four cases, and it is why this case has a mechanical check and not only an argument.
- The diff and the commit message do not name `api/orders.py` or `list_orders`. The commit message says "a list endpoint", which describes the sibling without naming it; a reviewer has nothing to look it up by.

This is checked against the real retrieval code by `TestShippedResemblanceCases_SiblingIsUnreachableByReferenceRetrieval` (`cli/benchmark_repostate_resemblance_test.go`). It materializes the case, builds every payload mode on one `RangeBuilder` as a review does, and asserts that `api/orders.py` is not in the grounding map and that no payload text names the file or `list_orders`.

## Consequence for scoring (read before running the panel)

With only 35.16.8 retrieval, **this case cannot be won**. The sibling is not in the payload, and the Epic 14.1 grounding gate drops any finding on `api/orders.py` before scoring, even a correct guess. A miss is guaranteed by construction. That is what the gate asks for, but a miss does not tell you whether a reviewer would find the defect if similarity retrieval showed it the sibling.

## Expected findings

1. **`orders-limit-is-still-unbounded`**: `api/orders.py:7-8`, `outside_diff: true`. The diff touches `api/paging.py` and `api/users.py` only.

## Files

| File | Role |
|---|---|
| `base/api/users.py` | The list endpoint the change clamps. |
| `base/api/orders.py` | The sibling list endpoint that is still unclamped. The finding settles here. |
| `base/api/paging.py` | The shared paging constants; the change adds `MAX_PAGE_SIZE` here. |
| `commit-message.txt` | The claim source, including the false "no list endpoint" claim. |
| `change.diff` | Adds `MAX_PAGE_SIZE` and the clamp in `list_users()`. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
