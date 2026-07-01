# atcr Reconciled Review

## Summary

- Total findings: 6
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 0 | 3 | 0 |
| LOW | 0 | 2 | 0 |

## Disagreements

Top 6 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/reconcile/consensus_filter_test.go:1` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: No test exercises the &#96;panelReviewers&#96; path when a source has findings but all have empty Reviewer strings

### 2. solo_finding — `reconcile/consensus.go:52` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: &#96;consensusExempt&#96; dead-code branch for &#96;Verification.Verdict == VerdictConfirmed&#96; is exercised only by a direct unit test, never through &#96;Reconcile&#96;

### 3. solo_finding — `reconcile/consensus.go:91` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: &#96;consensusNoiseCluster&#96; sets &#96;Similarity&#96; to 0 but no test asserts the field value

### 4. solo_finding — `reconcile/consensus_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test for &#96;consensusSingleton&#96; on a &#96;ConfVerified&#96; merged finding

### 5. solo_finding — `reconcile/consensus.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: &#96;categorySecurity&#96; constant is exported implicitly (lowercase) but &#96;CategoryOutOfScope&#96; is used directly in the same switch; inconsistency may confuse future readers

### 6. solo_finding — `reconcile/reconcile_test.go:25` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: The sort-order test comment explains the reviewer-sharing fix but the test still uses &#96;recAt()&#96; which sets a fixed time — the comment doesn&#39;t mention that &#96;recAt()&#96; is irrelevant to the fix

## Findings

### HIGH

- `internal/reconcile/consensus_filter_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test exercises the &#96;panelReviewers&#96; path when a source has findings but all have empty Reviewer strings
  - Fix: Add a test case where a source with findings carries only &#96;Reviewer: &#34;&#34;&#96; and assert &#96;panelReviewers&#96; returns 0
  - Evidence: &#96;panelReviewers&#96; ignores empty reviewers but no test proves it; &#96;TestConsensusSingleton_And_PanelReviewers&#96; only tests the unattributed finding mixed with attributed ones

### MEDIUM

- `reconcile/consensus.go:52` — confidence MEDIUM, reviewers: dax
  - Problem: &#96;consensusExempt&#96; dead-code branch for &#96;Verification.Verdict == VerdictConfirmed&#96; is exercised only by a direct unit test, never through &#96;Reconcile&#96;
  - Fix: Keep the forward-looking guard but add a comment that &#96;TestConsensusExempt_Predicate&#96; is the sole coverage
  - Evidence: &#96;consensusExempt&#96; confirmed-verdict branch cannot fire through &#96;Reconcile&#96; (Merge nils input Verification); only &#96;TestConsensusExempt_Predicate&#96; hits it
- `reconcile/consensus.go:91` — confidence MEDIUM, reviewers: dax
  - Problem: &#96;consensusNoiseCluster&#96; sets &#96;Similarity&#96; to 0 but no test asserts the field value
  - Fix: Assert &#96;Similarity == 0&#96; in any test that inspects the sidecar cluster produced by the filter
  - Evidence: &#96;consensusNoiseCluster&#96; returns &#96;AmbiguousCluster&#96; with zero-value &#96;Similarity&#96;; &#96;inAmbiguousSingleton&#96; only checks &#96;File&#96; and &#96;len(Findings)&#96;
- `reconcile/consensus_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test for &#96;consensusSingleton&#96; on a &#96;ConfVerified&#96; merged finding
  - Fix: Add a case: &#96;consensusSingleton(Merged{Finding{Confidence: ConfVerified}})&#96; must be &#96;false&#96;
  - Evidence: &#96;consensusSingleton&#96; keys on &#96;!ConfidenceAtOrAbove(_, ConfHigh)&#96;, which covers &#96;ConfVerified&#96; transitively, but no explicit case guards against a regression that reorders the confidence enum

### LOW

- `reconcile/consensus.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: &#96;categorySecurity&#96; constant is exported implicitly (lowercase) but &#96;CategoryOutOfScope&#96; is used directly in the same switch; inconsistency may confuse future readers
  - Fix: Add a comment that &#96;categorySecurity&#96; is unexported because it&#39;s internal to the filter, while &#96;CategoryOutOfScope&#96; is the canonical public constant
  - Evidence: &#96;consensusExempt&#96; switches on &#96;categorySecurity&#96; (local const) and &#96;CategoryOutOfScope&#96; (exported); no comment explains the asymmetry
- `reconcile/reconcile_test.go:25` — confidence MEDIUM, reviewers: dax
  - Problem: The sort-order test comment explains the reviewer-sharing fix but the test still uses &#96;recAt()&#96; which sets a fixed time — the comment doesn&#39;t mention that &#96;recAt()&#96; is irrelevant to the fix
  - Fix: Remove the &#96;recAt()&#96; call or note that it&#39;s orthogonal to the reviewer-sharing fix
  - Evidence: &#96;TestReconcile_SortedBySeverityThenLocation&#96; calls &#96;recAt()&#96; but the comment only explains the reviewer-sharing change
