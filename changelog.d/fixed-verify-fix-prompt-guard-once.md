- `atcr verify` prints one `secret_guard` warning per finding for the fix
  executor's prompt, not a second one when the diff-smell gate retries the fix;
  and the skeptic's copy of a finding's problem and evidence treats a bare
  carriage return as a line break, so a `system:` line after one is removed like
  any other role prefix. (epic 129 T1)
