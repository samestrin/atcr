# atcr Review Report

Total findings: 2

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 1 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 0 | 0 |
| LOW | 0 | 1 | 0 |

## Findings

### CRITICAL

- `auth.go:42` — confidence HIGH, reviewers: greta, host
  - Problem: token never expires
  - Fix: check expiry
  - Evidence: expiresAt unread

### LOW

- `util.go:7` — confidence MEDIUM, reviewers: otto
  - Problem: unused var
