# Case `incomplete-predicate-guard`

An incomplete-predicate case, the shape epic 35.16.9's persona rule targets: a guard that enumerates some members of a set and silently rejects the rest.

It is the tier's `outside_diff: false` anchor. Its primary finding is settled by an **added** line, so the tier covers both shapes and a run cannot score well on it merely by reading repository state.

## What the case plants

`KINDS` and `HANDLERS` both declare four event kinds: `email`, `sms`, `push`, `webhook`. The change adds an up-front guard so a malformed event names itself instead of surfacing as a bare `KeyError`:

```python
if kind not in ("email", "sms", "push"):
    raise UnknownKind(kind)
```

The guard omits `webhook`. Every webhook event — a kind the router has always served, with a handler sitting three lines above the guard — now raises `UnknownKind`.

| Claim in the commit message | What the diff actually does |
|---|---|
| `dispatch()` validates the kind up front and raises `UnknownKind` | **Delivered.** |
| "every kind in KINDS is accepted exactly as before" | **False.** The guard's literal tuple is a proper subset of `KINDS`. |

## Why it is hard

It is the easier of this suite's cases, and deliberately so. The defect is entirely inside the diff: the added tuple and the declared `KINDS` are both visible, so a reviewer that compares the guard against the set it is guarding finds it without leaving the change.

What makes it non-trivial is that nothing looks wrong locally. The guard is well-formed, the exception is well-named, and the omission is legible only by counting the enumeration against a definition the diff shows as context.

## Expected findings

1. **`guard-omits-webhook`** — `router/dispatch.py:24-25`, `outside_diff: false`. Settled by an added line.
2. **`intake-aborts-the-whole-batch`** — `router/intake.py:8`, `outside_diff: true`. The unchanged caller dispatches every event in one list comprehension, so the newly raised exception discards the events that already succeeded.

## Files

| File | Role |
|---|---|
| `base/router/dispatch.py` | The router before the guard, including the four-member `KINDS`. |
| `base/router/intake.py` | The only caller of `dispatch()`, and where the second finding settles. Do not prune. |
| `commit-message.txt` | The claim source, including the false "exactly as before" claim. |
| `change.diff` | Adds the exception class and the incomplete guard. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
