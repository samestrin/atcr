- The AXI home view now reports the anonymous usage ping's resolved state as a
  `telemetry` column (`on` or `off`), read from `ATCR_TELEMETRY` and config
  `telemetry` through the same gate the send path uses; the deprecated
  `--legacy-pipe` home payload and the human home view are unchanged. The AXI
  catalog entry, the AXI audit and the README Limits section now disclose that
  the ping is on by default and how to opt out (`ATCR_TELEMETRY=0` or
  `atcr config set telemetry false`). (epic 165 T4)
