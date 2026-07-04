# atcr Reconciled Review

## Summary

- Total findings: 3
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 1 | 0 |
| LOW | 0 | 2 | 0 |

## Disagreements

Top 3 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/debt/add.go:181` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: AppendItem does an unlocked read-modify-write of the authoritative README

### 2. solo_finding — `cmd/atcr/debt_add.go:96` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: Interactive wizard does not seed defaults from provided flags

### 3. solo_finding — `internal/debt/add.go:195` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: AppendItem writes the README then runs SyncShards; a failure in SyncShards leaves shards lagging the README

## Findings

### MEDIUM

- `internal/debt/add.go:181` — confidence MEDIUM, reviewers: otto
  - Problem: AppendItem does an unlocked read-modify-write of the authoritative README
  - Fix: Wrap the README read-modify-write in the shared mkdir-flock used by the TD flush path or serialize TD writes through a single utility
  - Evidence: &#96;data, err := os.ReadFile(readmePath)&#96; followed by &#96;os.WriteFile(readmePath, ...)&#96; without synchronization

### LOW

- `cmd/atcr/debt_add.go:96` — confidence MEDIUM, reviewers: otto
  - Problem: Interactive wizard does not seed defaults from provided flags
  - Fix: Seed &#96;wizardDefaults&#96; from flags passed to the command so partial flag input carries into the interactive prompts
  - Evidence: &#96;promptEntry&#96; is called with &#96;def&#96; but &#96;sev&#96;, &#96;file&#96;, &#96;problem&#96;, &#96;fix&#96;, and &#96;category&#96; are checked via &#96;mustFlag&#96; and ignored if the wizard is triggered
- `internal/debt/add.go:195` — confidence MEDIUM, reviewers: otto
  - Problem: AppendItem writes the README then runs SyncShards; a failure in SyncShards leaves shards lagging the README
  - Fix: Regenerate shards to a temp dir and swap, or document that post-write sync failure is recovered by re-running &#96;atcr debt list --sync&#96;
  - Evidence: &#96;os.WriteFile(readmePath, ...)&#96; occurs before &#96;SyncShards(readmePath, itemsDir, stderr)&#96;
