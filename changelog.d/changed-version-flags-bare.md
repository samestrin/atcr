- `atcr -v`, `atcr -V` and `atcr --version` each print the bare version (for
  example `1.4.0`) on stdout and exit 0, in both human and AXI output modes;
  `-V` is a new spelling. **Migration:** scripts that parse the flag's former
  `atcr version <version>` line must read the bare version instead; `atcr
  version` keeps its `atcr version <version>` output unchanged. (epic 165 T3)
