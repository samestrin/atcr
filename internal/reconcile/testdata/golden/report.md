# atcr Reconciled Review

## Summary

- Total findings: 6
- Sources: host, pool
- Clusters collapsed: 2
- Severity disagreements: 1
- Out-of-scope findings: 1 (annotated, excluded from the gate)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 1 | 0 | 0 |
| HIGH | 1 | 0 | 0 |
| MEDIUM | 0 | 2 | 0 |
| LOW | 0 | 1 | 0 |

## Disagreements

Top 3 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. verification_disagreement — `db.go:100` (CRITICAL) · score 6
- Severity disagreement: LOW vs CRITICAL
- Skeptics split: skeptic-a, skeptic-b
- Reviewers: greta, host (independence 2)
- Problem: sql injection in query builder
- Detail: skeptics split on exploitability

### 2. gray_zone — `pay.go:10` (MEDIUM) · score 2
- Reviewers: greta, host (independence 2)
- Problem: session token expires without refresh check
- Detail: similarity 0.57
- Positions:
  - greta — MEDIUM: session token expires without refresh check
  - host — MEDIUM: session token expires without bound

### 3. solo_finding — `util.go:5` (LOW) · score 1
- Reviewers: greta (independence 1)
- Problem: unused import lingers in file

## Findings

### CRITICAL

- `db.go:100` — confidence HIGH, reviewers: greta, host
  - Severity disagreement: LOW vs CRITICAL
  - Problem: sql injection in query builder
  - Fix: parametrize input
  - Evidence: [greta] pool repro / [host] host low

### HIGH

- `auth.go:42` — confidence HIGH, reviewers: greta, host
  - Problem: token never expires here
  - Fix: guard it
  - Evidence: [greta] pool saw it / [host] host also

### MEDIUM

- `pay.go:10` — confidence MEDIUM, reviewers: greta
  - Problem: session token expires without refresh check
  - Fix: add refresh
  - Evidence: pool note
- `pay.go:12` — confidence MEDIUM, reviewers: host
  - Problem: session token expires without bound
  - Fix: cap it
  - Evidence: host note

### LOW

- `util.go:5` — confidence MEDIUM, reviewers: greta
  - Problem: unused import lingers in file
  - Fix: remove it
  - Evidence: pool lint

## Out-of-Scope Findings

Pre-existing issues outside the reviewed change — annotated for the record, excluded from the severity gate.

### HIGH

- `legacy.go:7` — confidence MEDIUM, reviewers: greta
  - Problem: preexisting smell outside the diff
  - Fix: n/a
  - Evidence: pool oos
