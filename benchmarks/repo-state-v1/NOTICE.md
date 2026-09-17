# `repo-state-v1` provenance

This document states the origin and licensing of every file under
`benchmarks/repo-state-v1/`, mirroring the treatment
[`../standard-v1/NOTICE.md`](../standard-v1/NOTICE.md) gives that suite's imported
diffs.

## Every base tree in this suite is synthetic

**No case in `repo-state-v1` contains third-party code.** Every `base/` tree was
written by hand for the case it belongs to. The file names, symbol names and
behaviors are invented to plant one specific defect and to be readable end to end;
they do not correspond to any upstream project, and no upstream license applies.

This is why the suite carries no per-case `NOTICE.md`.
[`FORMAT.md`](FORMAT.md) makes a base tree's origin a genuine choice — "the tree
may be synthetic (hand-written for the case) or vendored from a real project" —
and makes a per-case `NOTICE.md` conditional on actually vendoring. Every case
here took the synthetic option.

The choice is not only about licensing. `FORMAT.md` asks that a reader be able to
hold a case's whole tree in their head, and a hand-written tree is pruned by
construction: it contains what the case needs because nothing else was ever
written. Pruning a real repository down to the same size is a judgment call with a
specific failure mode — prune away the file that consumes the changed symbol and
the case silently becomes unwinnable, which is the exact defect class this tier
exists to detect.

## If a future case vendors upstream code

A case that includes files taken from a real project must ship its own
`NOTICE.md` inside the case directory, naming the upstream source, the revision
taken, and the license the files carry. Update this document to point at it, so
this file stays the one place a reader checks.

## The suite's own contents

Everything else in this directory — `FORMAT.md`, `SPOT-CHECK.md`, `suite.json`,
and each case's `case.json`, `README.md`, `commit-message.txt` and `change.diff` —
is part of atcr and carries the repository's license.
