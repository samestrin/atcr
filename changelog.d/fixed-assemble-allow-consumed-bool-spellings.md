- `changelog assemble --allow-consumed=<value>` now accepts only the shape of
  a fragment path, `<category>-<slug>.md`, so boolean spellings like `=True`,
  `=yes` and `=on` and non-fragment names like `notes.md` get the exit-2 usage
  error naming `--allow-consumed=<fragment-path>` instead of the earlier
  exit-1 "not a fragment in this run". A well-shaped path that names no
  fragment in the run still exits 1. (epic 147 T1, epic 150 T1)
