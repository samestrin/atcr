- `atcr webhook` with `webhook.linear.auto_fix.daily_token_cap` set now runs one Linear
  auto-fix job at a time, from its claim until it finishes and is charged, so
  a job that starts while another runs can no longer pass the daily token cap
  on a total missing the running job's spend. Without a daily token cap, jobs
  for different pull requests still run at once. (epic 135 T4)
