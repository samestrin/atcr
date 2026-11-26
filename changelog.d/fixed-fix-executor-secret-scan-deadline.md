- The fix executor's secret scan (`secret_guard`) on the snippet path now runs
  under the executor's fix deadline (`fix_timeout_secs`, or the shared verify
  timeout), so a hung scanner ends that finding with a `fix generation failed`
  FixWarning instead of holding a fix worker with no bound. A scan that
  returns the wrong number of texts also becomes a FixWarning rather than a
  crash. (epic 161 T1)
