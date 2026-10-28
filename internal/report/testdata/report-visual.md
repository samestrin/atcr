# atcr Review Report

Total findings: 3

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 1 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 0 | 0 |
| LOW | 0 | 1 | 1 |

## Findings

### CRITICAL

- `auth.go:42` — confidence HIGH, reviewers: greta, host
  - Problem: token never expires
  - Fix: check expiry
  - Evidence: expiresAt unread

### LOW

- `util.go:7` — confidence MEDIUM, reviewers: otto
  - Problem: unused var
- `web/index.html:1` — confidence LOW, reviewers: felix
  - Problem: uncaught TypeError on load
  - Evidence: ui-console: visual/console.log, visual/home.png
  - Screenshot: ![screenshot](visual/home.png)
  - Screenshot: ![screenshot](visual/home-mobile.png)
  - Console: `visual/console.log`
    ```text
    TypeError: x is undefined
        at main.js:3
    ```
