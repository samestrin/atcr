- Under `secret_guard: redact`, a tool-using reviewer now scans each message of
  its conversation once and reuses that scan on later turns, instead of
  re-scanning the whole conversation on every turn. Every request is still
  redacted. A 20-turn run with gitleaks installed spends about 0.85 s scanning
  instead of 8.6 s. (epic 134 T4)
