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

This case was authored on the expectation that it would be the *easier* of the suite's cases: the defect is entirely inside the diff, the added tuple and the declared `KINDS` are both visible, and a reviewer comparing the guard against the set it guards finds it without leaving the change.

**The first panel run falsified that.** Exactly one reviewer of eleven found it — the lowest within-diff score of any case in the suite, including cases whose findings sit in files the diff never touches. See [`../SPOT-CHECK.md`](../SPOT-CHECK.md) → "AC8 — the panel result".

Being inside the diff turns out not to make a finding easy when the defect is an **omission**. Nothing looks wrong locally: the guard is well-formed, the exception is well-named, and every added line is individually correct. The defect is legible only by counting the guard's enumeration against a definition the diff shows as mere context — which is the same act of consulting unchanged state that the `outside_diff: true` cases demand, just performed without leaving the hunk.

That makes the case more valuable than intended, not less. It is the suite's `outside_diff: false` anchor, so a run cannot score well on the tier merely by reading repository state; and it demonstrates that "inside the diff" and "reachable from the diff" are different properties.

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
