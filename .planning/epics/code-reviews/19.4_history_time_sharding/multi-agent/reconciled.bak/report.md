# atcr Reconciled Review

## Summary

- Total findings: 6
- Sources: pool
- Clusters collapsed: 3
- Severity disagreements: 3
- Authority promoted: 2
- Consensus filtered: 4 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 3 | 1 | 0 |
| MEDIUM | 2 | 0 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 10 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. severity_split — `cmd/atcr/resume.go:210` (HIGH) · score 3
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: brad, greta, otto (independence 3)
- Problem: recordResumeHistory hardcodes cwd-relative ./.planning/history, so running atcr review --resume from a subdirectory writes shards to a location that atcr history (which uses repoRoot()) never reads

### 2. solo_finding — `cmd/atcr/review.go:285` (HIGH) · score 3
- Reviewers: brad (independence 1)
- Problem: Multiple concurrent atcr review processes will append to the same YYYY-MM.jsonl simultaneously without synchronization, causing interleaved JSON lines and ledger corruption

### 3. solo_finding — `internal/history/shard.go:33` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: LoadShards uses filepath.Glob on the directory path, so a repo path containing glob metacharacters (e.g. a [1] segment) is parsed as a character class and silently returns empty history instead of the real records

### 4. severity_split — `internal/history/shard.go:32` (MEDIUM) · score 3
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax, greta (independence 3)
- Problem: LoadShards loads every shard into memory before Filter applies --since, so a narrow time window still reads all shards (acceptable within 1-2yr bound but no month-prefix pruning)

### 5. severity_split — `internal/history/shard.go:43` (HIGH) · score 2
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: brad, dax (independence 2)
- Problem: LoadShards loads every monthly shard into memory before any --since filtering is applied, causing unbounded heap growth on repos with long histories even for narrow queries

### 6. solo_finding — `internal/history/shard.go:40` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: A single unreadable or corrupt shard aborts the entire LoadShards loop and fails atcr history, making one bad month brick all trend queries

### 7. gray_zone — `cmd/atcr/history_test.go:84` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: TestHistoryCmd_MergesLegacyAndShards does not verify the merged order (shards before legacy) or that the legacy file is not mutated
- Detail: similarity 0.00
- Positions:
  - dax — LOW: TestHistoryCmd_MergesLegacyAndShards does not verify the merged order (shards before legacy) or that the legacy file is not mutated

### 8. gray_zone — `internal/history/shard_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test exercises LoadAll when the legacy file exists but is unreadable
- Detail: similarity 0.00
- Positions:
  - dax — LOW: No test exercises LoadAll when the legacy file exists but is unreadable

### 9. gray_zone — `internal/history/shard_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test exercises LoadShards with a dir path containing glob metacharacters
- Detail: similarity 0.00
- Positions:
  - dax — LOW: No test exercises LoadShards with a dir path containing glob metacharacters

### 10. gray_zone — `internal/history/shard_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test exercises an unreadable shard file (permission error) to verify LoadShards behavior
- Detail: similarity 0.00
- Positions:
  - dax — LOW: No test exercises an unreadable shard file (permission error) to verify LoadShards behavior

## Findings

### HIGH

- `cmd/atcr/resume.go:210` — confidence HIGH, reviewers: brad, greta, otto
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: recordResumeHistory hardcodes cwd-relative ./.planning/history, so running atcr review --resume from a subdirectory writes shards to a location that atcr history (which uses repoRoot()) never reads
  - Fix: Pass repo root from runResume to recordResumeHistory to ensure shards are written to the project root rather than the current working directory
  - Evidence: [greta] histPath := history.ShardPath(filepath.Join(&#34;.&#34;, &#34;.planning&#34;, &#34;history&#34;), ts) / [brad] histPath := history.ShardPath(filepath.Join(&#34;.&#34;, &#34;.planning&#34;, &#34;history&#34;), ts) / [otto] histPath := history.ShardPath(filepath.Join(&#34;.&#34;, &#34;.planning&#34;, &#34;history&#34;), ts)
- `cmd/atcr/review.go:285` — confidence HIGH, reviewers: brad
  - Problem: Multiple concurrent atcr review processes will append to the same YYYY-MM.jsonl simultaneously without synchronization, causing interleaved JSON lines and ledger corruption
  - Fix: Acquire a file lock (flock) on the shard or a dedicated lockfile before opening for append
  - Evidence: histPath := history.ShardPath(filepath.Join(req.Root, &#34;.planning&#34;, &#34;history&#34;), now) / history.RecordReview(histPath, dir, now)
- `internal/history/shard.go:33` — confidence MEDIUM, reviewers: dax
  - Problem: LoadShards uses filepath.Glob on the directory path, so a repo path containing glob metacharacters (e.g. a [1] segment) is parsed as a character class and silently returns empty history instead of the real records
  - Fix: Replace filepath.Glob with os.ReadDir + strings.HasSuffix(name, &#34;.jsonl&#34;) so the directory path is treated literally
  - Evidence: &#96;matches, err := filepath.Glob(filepath.Join(dir, &#34;*.jsonl&#34;))&#96; treats metacharacters in dir as glob syntax
- `internal/history/shard.go:43` — confidence HIGH, reviewers: brad, dax
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: LoadShards loads every monthly shard into memory before any --since filtering is applied, causing unbounded heap growth on repos with long histories even for narrow queries
  - Fix: Log-and-skip an unreadable individual shard so remaining shards stay queryable, mirroring Load&#39;s torn-line tolerance
  - Evidence: [brad] var all []Record / for _, path := range matches { recs, err := Load(path) ... all = append(all, recs...) } / [dax] &#96;if err != nil { return nil, err }&#96; inside the shard loop hard-fails on the first unreadable shard

### MEDIUM

- `internal/history/shard.go:32` — confidence HIGH, reviewers: brad, dax, greta
  - Severity disagreement: LOW vs MEDIUM
  - Problem: LoadShards loads every shard into memory before Filter applies --since, so a narrow time window still reads all shards (acceptable within 1-2yr bound but no month-prefix pruning)
  - Fix: Replace filepath.Glob with os.ReadDir and filter using strings.HasSuffix(name, &#34;.jsonl&#34;) to treat the directory path literally
  - Evidence: [brad] matches, err := filepath.Glob(filepath.Join(dir, &#34;*.jsonl&#34;)) / [dax] &#96;for _, path := range matches { recs, err := Load(path) ... }&#96; loads all shards unconditionally / [greta] matches, err := filepath.Glob(filepath.Join(dir, &#34;*.jsonl&#34;))
- `internal/history/shard.go:40` — confidence HIGH, reviewers: brad
  - Problem: A single unreadable or corrupt shard aborts the entire LoadShards loop and fails atcr history, making one bad month brick all trend queries
  - Fix: Wrap Load in a conditional that logs the error and continues so remaining shards stay queryable
  - Evidence: if err != nil { return nil, err } inside the for _, path := range matches loop
