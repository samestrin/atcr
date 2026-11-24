- `debt backfill-justifications` no longer credits a review with a run on the
  strength of a `reconciled/summary.json` reached through a symlink that leads
  out of that review's `reconciled/` directory; the summary is read under an
  `os.Root`, so a symlinked `reconciled/` now owns nothing. (epic 128 T6)
