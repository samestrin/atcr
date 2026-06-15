# atcr Review Report

Total findings: 4 (1 refuted, shown below)

| Severity | VERIFIED conf | HIGH conf | MEDIUM conf | LOW conf |
|----------|---------------|-----------|-------------|----------|
| CRITICAL | 1 | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 | 1 |
| MEDIUM | 0 | 0 | 1 | 0 |
| LOW | 0 | 0 | 0 | 1 |

## Findings

### CRITICAL

- `auth.go:42` — confidence VERIFIED, reviewers: greta, host
  - Problem: token never expires
  - Fix: check expiry
  - Evidence: expiresAt unread
  - Skeptic: otto — confirmed
    - Reasoning: read auth.go:42 — expiresAt is parsed but never compared; token is accepted past expiry

### MEDIUM

- `cache/store.go:88` — confidence MEDIUM, reviewers: host, greta
  - Problem: data race on the cache map across goroutines
  - Fix: guard map access with a sync.RWMutex
  - Evidence: store.go:88 writes while store.go:71 reads
  - Skeptic: otto — unverifiable (skeptic could not verify)
    - Reasoning: could not establish whether the two call sites run concurrently — call graph is outside the snapshot jail

### LOW

- `util.go:7` — confidence LOW, reviewers: otto
  - Problem: unused var

## Refuted Findings

<details>
<summary>Refuted Findings (1)</summary>

- `db/query.go:13` — confidence LOW, skeptic: otto
  - Problem: SQL string built via concatenation — injection risk
  - Reasoning: the cited line uses db.Query(q, args...) with placeholders — the input is bound, not concatenated; false positive

</details>
