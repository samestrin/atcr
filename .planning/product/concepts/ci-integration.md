# Concept: First-Class CI Integration + Finding History

**Status:** Conceptual  
**Created:** 2026-06-11  
**Priority:** Medium  

## Problem

ATCR is CLI-first, which is CI-friendly, but:
- No official GitHub Action / GitLab CI template (teams roll their own)
- No PR comment posting (findings are in artifacts, not inline on the PR)
- No finding history (each run is isolated; no trend visibility)
- No block-merge on severity (teams want to block on CRITICAL findings)

Without these, adoption is friction-heavy:
- Teams write custom CI scripts (error-prone, not portable)
- Findings are buried in artifacts (low visibility)
- No way to answer: "has this package gotten better or worse over time?"

## Solution

### Official CI Templates

**GitHub Action:**
```yaml
name: ATCR Review
on: [pull_request]
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: atcr/action@v1
        with:
          fail-on: HIGH  # block merge on HIGH+ findings
          post-comments: true  # post findings as PR comments
```

**GitLab CI:**
```yaml
atcr-review:
  image: atcr/atcr:latest
  script:
    - atcr review --fail-on HIGH
    - atcr report --format sarif > findings.sarif
  artifacts:
    reports:
      codequality: findings.sarif
```

### PR Comment Posting

- `atcr report --format pr-comment` generates markdown for inline PR comments
- GitHub Action posts comments on the PR (one per finding, or a summary comment)
- Comments include: severity, file:line, problem, fix, reviewers, confidence
- Deduplication: don't re-post findings that were already posted in a previous run

### Finding History

- Every `atcr review` run appends to a `findings-history.jsonl` in the repo:
  ```json
  {
    "timestamp": "2026-06-11T10:00:00Z",
    "base": "abc123",
    "head": "def456",
    "findings": [
      {"severity": "HIGH", "file": "auth.go", "line": 42, "problem": "..."}
    ]
  }
  ```
- Query interface: `atcr history --package internal/auth --since 30d`
- Trend analysis: `atcr history --trend` outputs finding counts over time

### Block-Merge on Severity

- `atcr reconcile --fail-on CRITICAL` exits non-zero if any CRITICAL findings survive
- CI blocks the merge until findings are resolved or explicitly waived
- Waiver mechanism: `atcr waive --finding-id abc123 --reason "false positive"`

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| GitHub Action | Official `atcr/action@v1` with `fail-on`, `post-comments` | 1 week |
| GitLab CI template | Official `.gitlab-ci.yml` template | 3 days |
| PR comment posting | `atcr report --format pr-comment` + GitHub API integration | 1 week |
| Finding history | JSONL append-only log, query CLI | 1 week |
| Trend analysis | `atcr history --trend` outputs counts over time | 3 days |
| Block-merge | `--fail-on` exit code semantics (already in 1.0) | Already done |
| Waiver mechanism | `atcr waive` to mark findings as false positives | 1 week |

## Revenue Model

**OSS (free):**
- GitHub Action, GitLab CI template, PR comments, finding history
- All the above is free and OSS — it's distribution, not revenue

**Optional hosted dashboard (paid):**
- Hosted finding history (for teams that don't want to query JSONL)
- Trend dashboard (web UI)
- $X/month per repo
- This is optional — teams can also just use the JSONL + their own dashboard

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| GitHub Action | 1 week | Docker-based action, inputs for `fail-on`, `post-comments` |
| GitLab CI template | 3 days | YAML template, similar to GitHub Action |
| PR comment posting | 1 week | `atcr report --format pr-comment`, GitHub API client |
| Finding history | 1 week | JSONL writer, query CLI (`atcr history`) |
| Trend analysis | 3 days | Aggregation over JSONL, markdown table output |
| Waiver mechanism | 1 week | `atcr waive`, waiver log, reconcile skips waived findings |
| **Total** | **4-5 weeks** | Can be phased: Action first, history later |

## Moat / Differentiation

- **CI integration is table stakes** — without it, adoption is friction-heavy. This is not a differentiator; it's a prerequisite.
- **Finding history is a natural extension** — ATCR already produces deterministic artifacts. The history is just appending to a file.
- **Trend analysis is valuable on its own** — "your auth module has 3x the finding density of the rest of the codebase" is actionable insight.
- **PR comments increase visibility** — findings in artifacts are buried; findings inline on the PR are impossible to ignore.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| GitHub Action becomes a maintenance burden | Medium | Medium | Keep it simple — just a Docker wrapper around `atcr`. No complex logic |
| PR comment spam (too many findings) | Medium | High | Post a summary comment with top N findings; full list in artifacts |
| Finding history grows unbounded | Low | Medium | Rotate history (keep last 90 days); archive older entries |
| Waiver mechanism is abused | Low | Medium | Waivers are logged; audit trail shows who waived what and why |

## Open Questions

- **Where does history live?** Repo-local (each repo has its own `findings-history.jsonl`) or team-shared (one history DB for all repos)?
- **PR comment format?** One comment per finding (detailed, but spammy) or one summary comment (concise, but less visible)?
- **Waiver scope?** Per-finding (specific file:line) or per-category (all "missing error check" findings)?
- **Hosted dashboard?** Is this worth building, or should teams just use the JSONL + their own tools?

## References

- GitHub Super-Linter: official GitHub Action for linting, widely adopted
- CodeClimate: CI-integrated code quality, PR comments, trend dashboard
- SonarCloud: CI-integrated static analysis, quality gates (block merge on severity)
