- In AXI mode (the agent-path default: stdout not a terminal, `ATCR_FORMAT=axi`,
  an agent marker, or `--axi`), a failed command now prints one `error: true`
  TOON document on stdout instead of the `atcr: <message>` line on stderr. The
  document carries the redacted `message`, `code` (the exit code: 2 usage or
  configuration, 1 failure, 3 sync-cloud authentication) and `help[]`: the
  command's valid flags for an unknown flag, the command list for an unknown
  command, otherwise the command's `--help`. Nothing is written on stderr, and
  the exit codes are unchanged. A run that already printed its payload (a
  `reconcile --fail-on` gate exit) keeps that payload with no second document.
  Human mode keeps the stderr line. **Migration:** scripts that pipe atcr and
  read errors from stderr must read the document on stdout, or add `--human` /
  `ATCR_FORMAT=text`. (epic 165 T2)
