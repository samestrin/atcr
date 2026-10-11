# Case `traversal-check-misses-thumbnails`

A **resemblance-only** case, authored for epic 35.16.12's measurement gate: the defect sits in a sibling of the changed code that nothing in the change refers to. It is the shape "a parallel code path implementing the same invariant by hand".

## What the case plants

Two functions turn a client-supplied name into a path on disk: `download_path()` in `media/download.py` and `thumbnail_path()` in `media/thumbnail.py`. Each joins the name onto its own root with `os.path.join`, and neither checks that the result stays under that root.

The change fixes `download_path()`. It resolves the joined path with `os.path.realpath` and raises `UnsafePath` unless `os.path.commonpath` shows it is still under `UPLOAD_ROOT`.

`thumbnail_path()` is untouched (head `media/thumbnail.py:9`):

```python
return os.path.join(THUMB_ROOT, name)
```

`"../../../etc/passwd"` resolves to `/etc/passwd`. An absolute name such as `"/etc/passwd"` replaces the root outright, because `os.path.join` discards everything before an absolute component.

| Claim in the commit message | What the change actually does |
|---|---|
| the joined path is resolved and must stay inside `UPLOAD_ROOT` | **Delivered**, for `download_path()`. |
| "a client-supplied name can no longer reach a file outside the media store" | **False.** `thumbnail_path()` takes a client-supplied name too, and it is not checked. |

To confirm by hand, apply the diff and run, from the head tree's root:

```python
from media.thumbnail import thumbnail_path
thumbnail_path("/etc/passwd")            # '/etc/passwd'
thumbnail_path("../../../etc/passwd")    # '/srv/uploads/.thumbs/../../../etc/passwd'
```

## Why reference retrieval cannot reach the sibling

Epic 35.16.8's pre-fetch (`internal/payload/prefetch.go`, `buildPrefetch`) takes the symbols that enclose the diff's added lines, then runs one `git grep -F -w` for those names over the head tree, skipping the changed files. Each hit adds the block around it to the payload, and only those blocks become groundable.

- The changed symbols here are `UnsafePath` and `download_path`. `media/thumbnail.py` contains neither as a whole word, and the two modules import nothing from each other. `UPLOAD_ROOT` is not a changed symbol (the diff does not touch its line), and `thumbnail.py` does not use it anyway; it has its own `THUMB_ROOT`.
- The diff and the commit message do not name `media/thumbnail.py` or `thumbnail_path`.

So the only thing that links `thumbnail_path()` to the change is that it does the same job the same way. That is the gap 35.16.12 is about.

This is checked against the real retrieval code by `TestShippedResemblanceCases_SiblingIsUnreachableByReferenceRetrieval` (`cli/benchmark_repostate_resemblance_test.go`). It materializes the case, builds every payload mode on one `RangeBuilder` as a review does, and asserts that `media/thumbnail.py` is not in the grounding map and that no payload text names the file or `thumbnail_path`.

## Consequence for scoring (read before running the panel)

With only 35.16.8 retrieval, **this case cannot be won**. The sibling is not in the payload, and the Epic 14.1 grounding gate drops any finding on `media/thumbnail.py` before scoring, even a correct guess. A miss is guaranteed by construction. That is what the gate asks for, but a miss does not tell you whether a reviewer would find the defect if similarity retrieval showed it the sibling.

## Expected findings

1. **`thumbnail-path-escapes-the-store`**: `media/thumbnail.py:9`, `outside_diff: true`. The diff touches `media/download.py` only.

## Files

| File | Role |
|---|---|
| `base/media/download.py` | The path builder the change hardens. |
| `base/media/thumbnail.py` | The sibling path builder that is still unchecked. The finding settles here. |
| `commit-message.txt` | The claim source, including the false "can no longer reach a file outside the media store" claim. |
| `change.diff` | Adds `UnsafePath` and the containment check in `download_path()`. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
