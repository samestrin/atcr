- `changelog assemble --allow-consumed=<fragment-path>` is now repeatable and
  releases only the named fragments whose bullets already appear in
  `CHANGELOG.md`; the bare flag is rejected and unnamed consumed fragments are
  still refused. `--dry-run` now warns about the consumed bullets the real run
  would refuse while continuing to write and delete nothing. (epic 143 T2)
