- `debt backfill-justifications` no longer credits a review with a run from a
  `reconciled/summary.json` reached through a symlink: a `reconciled/` that is
  a symlink now owns nothing, wherever it leads, and the summary is read under
  an `os.Root` of `reconciled/`, so a `summary.json` symlink that leads out of
  that directory is refused. (epic 128 T6)
