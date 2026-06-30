# Technical Debt Backlog

Items from code review. Use `/resolve-td --group=N` to fix by group.
Use `/promote-tech-debt` to graduate items to formal TD sprint plans.

### [2026-06-30] From Sprint: 13.5_pagerank-v2-observability

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [ ] | SEVERITY | FILE:LINE | PROBLEM | FIX | CATEGORY | 0 | SOURCE | REVIEWERS | CONFIDENCE |
| U | [ ] | MEDIUM | reconcile/pagerank_confidence_test.go:163 | countAuthorityFlips oracle assumes single-reviewer HIGH implies authority promotion, missing the edge case where a single reviewer submits a finding with pre-existing HIGH confidence (e.g. from a prior merge or hardcoded severity), causing the test to falsely count it as an authority flip | Update countAuthorityFlips to verify the finding's base confidence was MEDIUM before promotion, or compare against the base Confidence before promoteByAuthority is applied | testing | 15 | code-review | dax | MEDIUM |
