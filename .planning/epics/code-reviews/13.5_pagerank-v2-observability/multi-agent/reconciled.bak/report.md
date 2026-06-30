# atcr Reconciled Review

## Summary

- Total findings: 1
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 1 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 1 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `reconcile/pagerank_confidence_test.go:163` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: countAuthorityFlips oracle assumes single-reviewer HIGH implies authority promotion, missing the edge case where a single reviewer submits a finding with pre-existing HIGH confidence (e.g. from a prior merge or hardcoded severity), causing the test to falsely count it as an authority flip

## Findings

### MEDIUM

- `reconcile/pagerank_confidence_test.go:163` — confidence MEDIUM, reviewers: dax
  - Problem: countAuthorityFlips oracle assumes single-reviewer HIGH implies authority promotion, missing the edge case where a single reviewer submits a finding with pre-existing HIGH confidence (e.g. from a prior merge or hardcoded severity), causing the test to falsely count it as an authority flip
  - Fix: Update countAuthorityFlips to verify the finding&#39;s base confidence was MEDIUM before promotion, or compare against the base Confidence before promoteByAuthority is applied
  - Evidence: countAuthorityFlips checks len(m.Reviewers)==1 &amp;&amp; m.Confidence==ConfHigh without verifying the pre-promotion state
