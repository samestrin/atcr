# Case `archived-guard-misses-transfer`

A **resemblance-only** case, authored for epic 35.16.12's measurement gate: the defect sits in a sibling of the changed code that nothing in the change refers to. It is the shape "a second handler that needs the same guard the diff adds to the first".

## What the case plants

Two endpoints mutate a project: `rename_project()` in `projects/rename.py` and `transfer_project()` in `projects/transfer.py`. Both load the project and write to it. Neither checks `Project.archived`.

The change fixes one of them. It adds `ProjectArchived` and a guard to `rename_project()`:

```python
project = store.load(project_id)
if project.archived:
    raise ProjectArchived(project_id)
```

`transfer_project()` is untouched. An archived project can still be handed to a new owner (head `projects/transfer.py:6-7`).

| Claim in the commit message | What the change actually does |
|---|---|
| `rename_project()` raises `ProjectArchived` for an archived project | **Delivered.** |
| "an archived project can no longer be modified" | **False.** `transfer_project()` still loads an archived project, reassigns `owner` and saves it. |

To confirm by hand, apply the diff and run, from the head tree's root:

```python
from projects.model import Project
from projects.store import ProjectStore
from projects.transfer import transfer_project
s = ProjectStore(); s.save(Project("p1", "n", "alice", archived=True))
transfer_project(s, "p1", "mallory").owner   # 'mallory'
```

## Why reference retrieval cannot reach the sibling

Epic 35.16.8's pre-fetch (`internal/payload/prefetch.go`, `buildPrefetch`) takes the symbols that enclose the diff's added lines, then runs one `git grep -F -w` for those names over the head tree, skipping the changed files. Each hit adds the block around it to the payload, and only those blocks become groundable.

- The changed symbols here are `ProjectArchived` and `rename_project`. `projects/transfer.py` contains neither as a whole word. It does not import `projects/rename.py`, and `projects/rename.py` does not import it.
- The diff and the commit message do not name `projects/transfer.py` or `transfer_project`.
- `model.py` and `store.py` are shared by both endpoints, but they are dependencies, not consumers. Pre-fetch searches only for changed symbols, so it never looks at them.

So the only thing that links `transfer_project()` to the change is that it looks like `rename_project()`. That is the gap 35.16.12 is about.

This is checked against the real retrieval code by `TestShippedResemblanceCases_SiblingIsUnreachableByReferenceRetrieval` (`cli/benchmark_repostate_resemblance_test.go`). It materializes the case, builds every payload mode on one `RangeBuilder` as a review does, and asserts that `projects/transfer.py` is not in the grounding map and that no payload text names the file or `transfer_project`. A control test in the same file proves pre-fetch does reach a real consumer in that harness.

## Consequence for scoring (read before running the panel)

With only 35.16.8 retrieval, **this case cannot be won**. The sibling is not in the payload, so a reviewer cannot cite it from what it was shown, and the Epic 14.1 grounding gate drops any finding on `projects/transfer.py` before scoring, even a correct guess. A miss here is guaranteed by construction. That is the property the gate needs ("absent from the payload, reachable only by resemblance"). But it means a miss does not tell you whether a reviewer *would* find the defect if similarity retrieval put the sibling in front of it.

## Expected findings

1. **`transfer-still-writes-archived-projects`**: `projects/transfer.py:6-7`, `outside_diff: true`. The diff touches `projects/rename.py` only.

## Files

| File | Role |
|---|---|
| `base/projects/rename.py` | The endpoint the change guards. |
| `base/projects/transfer.py` | The sibling endpoint that still needs the guard. The finding settles here. |
| `base/projects/model.py` | `Project`, including the `archived` flag. |
| `base/projects/store.py` | The in-memory store both endpoints load from and save to. |
| `commit-message.txt` | The claim source, including the false "can no longer be modified" claim. |
| `change.diff` | Adds `ProjectArchived` and the guard in `rename_project()`. |
| `case.json` | The manifest, per [`../FORMAT.md`](../FORMAT.md). |

The tree is synthetic and hand-written, so no per-case `NOTICE.md` is required.
