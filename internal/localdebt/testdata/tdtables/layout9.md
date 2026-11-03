# Technical Debt

## Stats

| Severity | Open | Deferred | Resolved | Unreproducible |
|----------|------|----------|----------|----------------|
| HIGH | 1 | 0 | 0 | 1 |
| [ ] | 1 | 0 | 0 | 0 |

### [2026-09-01] From: epic-10-review

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [ ] | HIGH | internal/a/a.go:12 | A pipe `a \| b` inside code | Escape it | correctness | 30 | epic-review EST-1 |
| U | [x] | LOW | internal/scorecard/trust.go | No line on the file | Add one | docs | 5 | paperclip-cto |
| Solo | [-] | MEDIUM | internal/reconcile/lib.go:109-133 | A line range | Narrow it | style | 10 | otto |

### [2026-09-02] From: epic-11-review

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 2 | [/] | MEDIUM | ~/.config/atcr/registry.yaml:vera | A colon that is not a line | Leave it | config | 15 | vera |
| 2 | [ ] | LOW | internal/x/x.go:7 | Unescaped `a | b` splits the row | Escape it | correctness | 10 | otto |
