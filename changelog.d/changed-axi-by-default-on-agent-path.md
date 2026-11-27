- Commands that have an AXI mode (bare `atcr`, `review`, `review --resume`,
  `reconcile`, `verify`, `debate`, `status`, `debt clean`, `debt filter`,
  `debt group`, `debt validate`, and `report` when it writes to stdout) now print
  their TOON/AXI output by default when stdout is not a terminal, when
  `ATCR_FORMAT=axi` is set, or when an agent marker (`CLAUDECODE`, `CODEX`,
  `OPENCODE`) is in the environment; on a terminal the human output is unchanged.
  A new `--human` flag and `ATCR_FORMAT=text` force the human output anywhere, and
  an explicit `--axi`, `--json`, `--format` or `review --auto-fix` still wins.
  `atcr report --format text` is accepted as an alias of `md`. **Migration:**
  scripts that pipe these commands and parse their text output must add `--human`
  or set `ATCR_FORMAT=text`. (epic 165 T1)
