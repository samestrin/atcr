# atcr Reconciled Review

## Summary

- Total findings: 4
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 2
- Authority promoted: 2
- Consensus filtered: 1 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 3 | 0 | 0 |
| MEDIUM | 1 | 0 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 5 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/fanout/resume.go:268` (HIGH) · score 3
- Reviewers: brad (independence 1)
- Problem: Resume rebuilds payloads using the caller&#39;s NoIgnore flag instead of the original run&#39;s value, violating payload consistency for pending agents

### 2. solo_finding — `internal/payload/diff.go:236` (HIGH) · score 3
- Reviewers: brad (independence 1)
- Problem: (applyIgnore) Unbounded exclude pathspec list exceeds OS execve ARG_MAX limits and crashes git diff

### 3. severity_split — `internal/payload/ignore.go:84` (HIGH) · score 2
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: brad, greta (independence 2)
- Problem: (loadAtcrignore) Reading .atcrignore with os.ReadFile allows a malicious multi-gigabyte file to OOM the process

### 4. severity_split — `internal/payload/diff.go:208` (MEDIUM) · score 2
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, otto (independence 2)
- Problem: (changedFilesMemo) Lazy initialization of ignore matcher lacks synchronization, causing data races if gitRunner is ever shared or called concurrently

### 5. gray_zone — `internal/payload/ignore.go:67` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: &#96;loadAtcrignore&#96; uses &#96;strings.Split(string(data), &#34;\n&#34;)&#96;
- Detail: similarity 0.00
- Positions:
  - otto — LOW: &#96;loadAtcrignore&#96; uses &#96;strings.Split(string(data), &#34;\n&#34;)&#96;

## Findings

### HIGH

- `internal/fanout/resume.go:268` — confidence HIGH, reviewers: brad
  - Problem: Resume rebuilds payloads using the caller&#39;s NoIgnore flag instead of the original run&#39;s value, violating payload consistency for pending agents
  - Fix: Persist NoIgnore in the review manifest and validate it matches the resume request before rebuilding payloads
  - Evidence: payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head, req.NoIgnore)
- `internal/payload/diff.go:236` — confidence HIGH, reviewers: brad
  - Problem: (applyIgnore) Unbounded exclude pathspec list exceeds OS execve ARG_MAX limits and crashes git diff
  - Fix: Filter ignored files in Go after retrieving the full diff or batch excludes into a temp file passed via stdin
  - Evidence: exclude = append(exclude, &#34;:(exclude,literal)&#34;+f.path) passed directly to git args at line 395
- `internal/payload/ignore.go:84` — confidence HIGH, reviewers: brad, greta
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: (loadAtcrignore) Reading .atcrignore with os.ReadFile allows a malicious multi-gigabyte file to OOM the process
  - Fix: Use bufio.Scanner with a strict total size limit or io.LimitReader before parsing
  - Evidence: [brad] data, err := os.ReadFile(path) / [greta] lines = append(lines, ln) preserves carriage returns from CRLF endings so gitignore patterns never match forward-slash paths

### MEDIUM

- `internal/payload/diff.go:208` — confidence HIGH, reviewers: brad, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: (changedFilesMemo) Lazy initialization of ignore matcher lacks synchronization, causing data races if gitRunner is ever shared or called concurrently
  - Fix: Use &#96;fmt.Sprintf&#96; or a constant prefix for clarity, though string concatenation is performant here
  - Evidence: [brad] if !g.ignoreReady { g.ignore = newIgnoreMatcher(g.dir, g.log()); g.ignoreReady = true } / [otto] line 209
