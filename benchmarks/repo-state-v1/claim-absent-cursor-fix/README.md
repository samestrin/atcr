# Case `claim-absent-cursor-fix`

**Not machine-runnable yet.** This case is authored against [`../FORMAT.md`](../FORMAT.md) and is hand-verifiable today, but nothing loads it: the `repo-state-v1` loader, matcher, and scorer are epic 35.16.10's work. Until that lands, the case is checked by reading it, using the authoring checklist at the end of `FORMAT.md`.

## What the case plants

The commit message asserts three things. The diff delivers one of them.

| Claim in the commit message | What the diff actually does |
|---|---|
| `_safe_drain_offset()` returns `None` instead of `0` when the batch holds no well-formed record | **Delivered.** The helper is added, and it does return `None`. |
| `begin()` now keeps its previous offset when the drain yields nothing | **Absent.** `begin()` is untouched. It still calls `_drain_offset()` — the old helper — and still assigns its `0` into `self._offset`. |
| `test_begin_preserves_offset_on_all_malformed_batch` covers the fix | **Delivered in name only.** The test never calls `begin()`. It asserts `_safe_drain_offset()` directly, so it passes while the bug it is named for is still live. |

The bug the message claims to fix is therefore still present after the change: an all-malformed batch still wipes a live cursor, and a test named for the fix goes green anyway.

## Why it is hard

The defect is an **absence**, and an absent change leaves no trace in a diff.

Every added line in this diff is correct. The new helper is well written, documented, and does exactly what its docstring says. The new test passes. A reviewer reading only added and removed lines has nothing to react to — there is no wrong line to point at, because the wrong thing is a line that was never written.

The one line that settles it, `self._offset = self._drain_offset()`, is **not part of the change**. It appears in the diff only as context, and in a narrower payload mode it would not appear at all. Reaching it requires either the surrounding repository state or the claim itself.

That is what makes this a `repo-state-v1` case rather than a `standard-v1` one. `standard-v1` cases are diffs, and this defect is invisible in a diff. It becomes detectable only when the reviewer is handed the author's claim and asked to adjudicate it against what the code does.

## Expected findings

Two, both recorded in `case.json`:

1. **`begin-still-wipes-the-cursor`** — `streamer/cursor.py:46`, `outside_diff: true`. This is the finding the tier exists to measure. A reviewer that reports only in-diff findings will miss it.
2. **`test-named-for-the-fix-asserts-the-helper`** — `tests/test_cursor.py:8`, `outside_diff: false`. This one is reachable from the diff alone, since the whole test file is added, but it is only *interesting* once the claim names what the test was supposed to cover.

## Files

| File | Role |
|---|---|
| `base/streamer/cursor.py` | The cursor before the change, including the `_drain_offset()` that returns `0`. |
| `base/streamer/drain.py` | The only caller of `begin()`, so the blast radius of the unfixed bug is visible in the tree. |
| `commit-message.txt` | The claim source. |
| `change.diff` | Adds the helper and the test. Does not touch `begin()`. |
| `case.json` | The manifest, per `../FORMAT.md`. |

The tree is synthetic and hand-written, so no `NOTICE.md` is required.
