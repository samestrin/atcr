- `changelog assemble --allow-consumed=false`, `=1` and `=0` now get the same
  usage error as the bare flag, naming the `--allow-consumed=<fragment-path>`
  spelling, instead of being taken as fragment paths and failing later with
  "not a fragment in this run". (epic 145 T1)
