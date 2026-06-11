# Acceptance Criteria: Adversarial Review and Ambiguity Adjudication

**Related User Story:** [05: Host Review via Skill](../user-stories/05-host-review-via-skill.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Adversarial Prompt | Skill instructions (Markdown) | Personality clause in SKILL.md |
| Ambiguity Adjudication | Host LLM | Reads `ambiguous.json`, judges clusters |
| Re-invocation | `atcr reconcile` | Re-merge after adjudication decisions |
| Ambiguous Sidecar | `ambiguous.json` | Clusters below dedupe threshold |
| Test Framework | `testify` (assert, require) | Tests for adversarial output and adjudication logic |

## Related Files
- `skill/SKILL.md` - modify: Add adversarial personality clause and ambiguity adjudication instructions
- `internal/reconcile/ambiguous.go` - create: Ambiguous cluster types and JSON serialization
- `internal/reconcile/ambiguous_test.go` - create: Tests for ambiguous cluster detection and output
- `docs/findings-format.md` - modify: Document ambiguous.json sidecar format and adjudication flow

## Happy Path Scenarios

**Scenario 1: Host review is adversarial (finds problems, not praise)**
- **Given** the skill instructions include an adversarial personality clause
- **When** the host agent generates findings
- **Then** findings focus on bugs, security issues, logic errors, and code quality problems
- **And** findings do not include praise, compliments, or positive observations
- **And** the review.md tone is direct and critical, not congratulatory

**Scenario 2: Ambiguous clusters written to ambiguous.json**
- **Given** the reconciler finds clusters with text-similarity below the dedupe threshold but above zero
- **When** `atcr reconcile` runs
- **Then** `ambiguous.json` is written to the review directory
- **And** each cluster entry includes: cluster ID, member findings (source, file:line, problem text), and similarity scores

**Scenario 3: Skill adjudicates ambiguous clusters**
- **Given** `ambiguous.json` exists with one or more clusters
- **When** the skill reads the clusters
- **Then** the host agent evaluates each cluster to determine if findings are duplicates or distinct issues
- **And** for duplicates: the skill marks the cluster for merge (keeping highest severity, joining reviewers)
- **And** for distinct issues: the skill marks the cluster as unmerged (all findings remain separate)

**Scenario 4: Adjudication results feed back into reconcile**
- **Given** the skill has adjudicated all ambiguous clusters
- **When** the skill re-invokes `atcr reconcile` with adjudication decisions
- **Then** the reconciler applies merge decisions for marked-duplicate clusters
- **And** preserves all findings in marked-distinct clusters
- **And** the final report reflects the adjudicated results

**Scenario 5: Conservative default when adjudication is skipped**
- **Given** `ambiguous.json` exists but the skill does not adjudicate
- **When** `atcr reconcile` runs without adjudication input
- **Then** all ambiguous clusters remain unmerged (conservative default)
- **And** the report notes that ambiguous clusters were not adjudicated

## Edge Cases

**Edge Case 1: No ambiguous clusters**
- **Given** all findings are clearly duplicates or clearly distinct
- **When** `atcr reconcile` runs
- **Then** `ambiguous.json` is either empty or not created
- **And** the skill skips the adjudication step

**Edge Case 2: Ambiguous cluster with findings from host and pool agent**
- **Given** a cluster contains one finding from `host` and one from a pool agent
- **When** the skill adjudicates
- **Then** the host evaluates whether the two findings describe the same underlying issue
- **And** considers file:line proximity, problem text similarity, and category alignment

**Edge Case 3: Large number of ambiguous clusters (> 20)**
- **Given** `ambiguous.json` contains 25 clusters
- **When** the skill adjudicates
- **Then** the skill processes all clusters
- **And** does not truncate or skip clusters due to volume

**Edge Case 4: Adjudication introduces no new merges**
- **Given** the skill evaluates all ambiguous clusters
- **When** all clusters are judged as distinct issues
- **Then** no findings are merged
- **And** the reconciled output is identical to the pre-adjudication output

## Error Conditions

**Error Scenario 1: ambiguous.json is malformed**
- Error message: "Failed to parse ambiguous.json: <parse error>. Skipping adjudication."
- Skill behavior: Skip adjudication, proceed with unmerged findings

**Error Scenario 2: Reconcile fails after adjudication re-invocation**
- Error message: "Reconcile failed after adjudication: <error>. Original findings preserved."
- Skill behavior: Report error; original unmerged findings remain valid

**Error Scenario 3: Host adjudication produces invalid merge decision**
- Error message: "Invalid adjudication decision for cluster <id>: <reason>. Cluster left unmerged."
- Skill behavior: Leave the cluster unmerged (conservative fallback)

## Performance Requirements
- **Adjudication Time:** Each cluster adjudication completes within the host's normal response time (typically < 30 seconds per cluster)
- **Ambiguous.json Size:** File remains under 100KB for typical reviews (< 50 clusters)
- **Re-invocation Overhead:** Re-running reconcile after adjudication adds < 5 seconds to total time

## Security Considerations
- **Adjudication integrity:** Merge decisions are based on finding content only; no external data influences adjudication
- **No prompt injection via findings:** Findings text in ambiguous.json is treated as data, not instructions; the skill parses it structurally
- **Conservative default:** When in doubt, the skill leaves clusters unmerged — never silently drops findings
- **Audit trail:** Adjudication decisions are logged to the review directory for post-hoc inspection

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Ambiguous cluster fixtures (2-member and multi-member clusters)
- Adjudication decision fixtures (merge, distinct, invalid)
- Pre- and post-adjudication findings files
- Host review output fixtures (adversarial vs. non-adversarial samples)
**Mock/Stub Requirements:**
- Mock LLM response for adjudication (pre-canned merge/distinct decisions)
- Filesystem fixtures for ambiguous.json and findings files
- Mock `atcr reconcile` for re-invocation testing

**Test Cases:**
1. `TestAdversarial_NoPraiseInFindings` — verify host findings contain no positive/praise language
2. `TestAdversarial_FocusOnProblems` — verify findings target bugs, security, logic, quality issues
3. `TestAmbiguous_ClusterDetection` — verify clusters near dedupe threshold are written to ambiguous.json
4. `TestAmbiguous_JSONFormat` — verify ambiguous.json schema (cluster ID, members, similarity scores)
5. `TestAdjudication_MergeDecision` — verify duplicate cluster merged correctly
6. `TestAdjudication_DistinctDecision` — verify distinct cluster left unmerged
7. `TestAdjudication_ConservativeDefault` — verify unadjudicated clusters remain unmerged
8. `TestAdjudication_ReconcileReinvocation` — verify reconcile applies adjudication decisions
9. `TestAdjudication_MalformedJSON` — verify graceful handling of invalid ambiguous.json

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] ambiguous.json format validated against schema

**Story-Specific:**
- [ ] Host review instructions include adversarial personality clause
- [ ] Host findings contain no praise or positive observations
- [ ] Ambiguous clusters written to `ambiguous.json` with correct schema
- [ ] Skill can adjudicate clusters and re-invoke reconcile
- [ ] Conservative default preserves unmerged findings when adjudication is skipped
- [ ] Adjudication decisions are logged for audit

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Adversarial tone of host review verified in a real review run
- [ ] Ambiguity adjudication produces sensible merge/distinct decisions
