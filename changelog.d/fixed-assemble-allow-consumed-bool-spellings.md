- `changelog assemble --allow-consumed=True`, `=TRUE`, `=t`, `=F` (and every
  other boolean spelling) now get the same usage error as `=false`, naming the
  `--allow-consumed=<fragment-path>` spelling, instead of failing later with
  "not a fragment in this run". (epic 147 T1)
