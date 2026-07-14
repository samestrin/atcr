# atcr Reconciled Review

## Summary

- Total findings: 3
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 1
- Consensus filtered: 6 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 1 | 1 | 0 |
| MEDIUM | 1 | 0 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 8 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `cmd/atcr/debt_resolve.go:250` (HIGH) · score 3
- Reviewers: brad (independence 1)
- Problem: (markDebtResolved) No length validation on --reason allows multi-MB justification strings to exhaust disk and trigger OOM on subsequent ReadAll calls

### 2. severity_split — `cmd/atcr/debt_resolve.go:216` (HIGH) · score 2
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: brad, vera (independence 2)
- Problem: atcr debt resolve output changed from &#34;Marked %s resolved.&#34; to &#34;Marked %s %s.&#34; breaking scripts that parse the output string

### 3. gray_zone — `cmd/atcr/debt_resolve.go:54` (MEDIUM) · score 2
- Reviewers: bruce (independence 1)
- Problem: Reason-only guard skips validation and silently falls through: &#96;cmd.Flags().Changed(&#34;status&#34;)&#96; does not guard --reason, so &#96;--reason &#34;text&#34; --resolve&#96; is required but &#96;--reason &#34;text&#34;&#96; alone bypasses the guard and proceeds to markDebtResolved which then errors on missing id (user gets an unhelpful &#34;no open item&#34; error instead of a clear usage hint). The --status case correctly uses &#96;Changed(&#34;status&#34;)&#96;. Align --reason guard with the --status pattern: &#96;cmd.Flags().Changed(&#34;status&#34;) 
- Detail: similarity 0.00
- Positions:
  - bruce — MEDIUM: Reason-only guard skips validation and silently falls through: &#96;cmd.Flags().Changed(&#34;status&#34;)&#96; does not guard --reason, so &#96;--reason &#34;text&#34; --resolve&#96; is required but &#96;--reason &#34;text&#34;&#96; alone bypasses the guard and proceeds to markDebtResolved which then errors on missing id (user gets an unhelpful &#34;no open item&#34; error instead of a clear usage hint). The --status case correctly uses &#96;Changed(&#34;status&#34;)&#96;. Align --reason guard with the --status pattern: &#96;cmd.Flags().Changed(&#34;status&#34;) 

### 4. gray_zone — `cmd/atcr/debt_resolve.go:71` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: The error message for invalid &#96;--status&#96; value uses &#96;mustFlag(cmd, &#34;status&#34;)&#96; (original casing) while the lookup uses the lowercased &#96;status&#96; variable, so a user typing &#34;--status RESOLVED&#34; sees &#34;invalid --status &#39;RESOLVED&#39;&#34; rather than the canonical lowercase form. Not a crash, but inconsistent with the lowercasing that actually controls the logic.
- Detail: similarity 0.00
- Positions:
  - mira — MEDIUM: The error message for invalid &#96;--status&#96; value uses &#96;mustFlag(cmd, &#34;status&#34;)&#96; (original casing) while the lookup uses the lowercased &#96;status&#96; variable, so a user typing &#34;--status RESOLVED&#34; sees &#34;invalid --status &#39;RESOLVED&#39;&#34; rather than the canonical lowercase form. Not a crash, but inconsistent with the lowercasing that actually controls the logic.

### 5. gray_zone — `cmd/atcr/debt_resolve.go:67` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: The status validation rejects empty string, but no test covers &#96;--status &#34;&#34;&#96;.
- Detail: similarity 0.00
- Positions:
  - dax — LOW: The status validation rejects empty string, but no test covers &#96;--status &#34;&#34;&#96;.

### 6. gray_zone — `cmd/atcr/debt_resolve.go:260` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: The reason trimming logic treats whitespace-only reason as empty (preserving existing justification), but no test covers &#96;--reason &#34;   &#34;&#96;.
- Detail: similarity 0.00
- Positions:
  - dax — LOW: The reason trimming logic treats whitespace-only reason as empty (preserving existing justification), but no test covers &#96;--reason &#34;   &#34;&#96;.

### 7. gray_zone — `cmd/atcr/debt_resolve_test.go:223` (LOW) · score 1
- Reviewers: mira (independence 1)
- Problem: The new tests (&#96;TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList&#96;, &#96;TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut&#96;, etc.) cover the happy path for wontfix but do not include a test for the idempotency case: resolving an already-closed item with &#96;--status wontfix&#96; when a &#96;resolved&#96; record already exists (or vice versa). The existing &#96;TestDebtResolve_MarkResolvedIsIdempotent&#96; covers this for &#96;--status resolved&#96;, but the wontfix variant is missing, leaving the &#34;already resolved&#34; hardcoded-message regression (finding 1) untested for wontfix closures.
- Detail: similarity 0.00
- Positions:
  - mira — LOW: The new tests (&#96;TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList&#96;, &#96;TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut&#96;, etc.) cover the happy path for wontfix but do not include a test for the idempotency case: resolving an already-closed item with &#96;--status wontfix&#96; when a &#96;resolved&#96; record already exists (or vice versa). The existing &#96;TestDebtResolve_MarkResolvedIsIdempotent&#96; covers this for &#96;--status resolved&#96;, but the wontfix variant is missing, leaving the &#34;already resolved&#34; hardcoded-message regression (finding 1) untested for wontfix closures.

### 8. gray_zone — `cmd/atcr/debt_resolve_test.go:355` (LOW) · score 1
- Reviewers: mira (independence 1)
- Problem: &#96;TestDebtResolve_NoReasonPreservesExistingJustification&#96; verifies that omitting &#96;--reason&#96; preserves an existing justification, but does not test the edge case where the original record has an empty &#96;Justification&#96; and &#96;--reason&#96; is also omitted — does the new record get a blank &#96;Justification&#96; (zero-value field) or does it remain unset? This matters for clients that distinguish between &#34;no justification provided&#34; (nil/empty) and &#34;justification is empty string&#34;.
- Detail: similarity 0.00
- Positions:
  - mira — LOW: &#96;TestDebtResolve_NoReasonPreservesExistingJustification&#96; verifies that omitting &#96;--reason&#96; preserves an existing justification, but does not test the edge case where the original record has an empty &#96;Justification&#96; and &#96;--reason&#96; is also omitted — does the new record get a blank &#96;Justification&#96; (zero-value field) or does it remain unset? This matters for clients that distinguish between &#34;no justification provided&#34; (nil/empty) and &#34;justification is empty string&#34;.

## Findings

### HIGH

- `cmd/atcr/debt_resolve.go:216` — confidence HIGH, reviewers: brad, vera
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: atcr debt resolve output changed from &#34;Marked %s resolved.&#34; to &#34;Marked %s %s.&#34; breaking scripts that parse the output string
  - Fix: Restore the original output string or document the change and update consumers
  - Evidence: [brad] recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}) scales linearly with history size and risks OOM on long-lived repos / [vera] fmt.Fprintf(..., &#34;Marked %s %s.\n&#34;, id, status)
- `cmd/atcr/debt_resolve.go:250` — confidence MEDIUM, reviewers: brad
  - Problem: (markDebtResolved) No length validation on --reason allows multi-MB justification strings to exhaust disk and trigger OOM on subsequent ReadAll calls
  - Fix: Enforce a max length or truncate before assignment
  - Evidence: rec.Justification = r accepts unbounded input directly from CLI flag

### MEDIUM

- `cmd/atcr/debt_resolve.go:64` — confidence HIGH, reviewers: dax, mira
  - Problem: (runDebtResolve) The &#96;cmd.Flags().Changed(&#34;status&#34;)&#96; guard correctly rejects &#96;--status&#96; supplied without &#96;--resolve&#96;, but the error message says &#34;--status/--reason require --resolve &lt;id&gt;&#34; which conflates the two flags. If only &#96;--status&#96; was provided (no &#96;--reason&#96;), the error still mentions &#96;--reason&#96;, which is confusing. The error should mention only the flags that were actually supplied, not all gated flags.
  - Fix: Emit a targeted error listing only the flags that were provided without &#96;--resolve&#96;, e.g. &#34;flag(s) require --resolve &lt;id&gt;&#34; with a list of the offending flags
  - Evidence: [dax] TestDebtResolve_StatusOrReasonWithoutResolveIsUsageError only tests &#96;--status wontfix&#96;; the guard condition &#96;cmd.Flags().Changed(&#34;status&#34;)&#96; is value-agnostic. / [mira] return usageError(fmt.Errorf(&#34;--status/--reason require --resolve &lt;id&gt;&#34;))
