# Acceptance Criteria: Reconciliation Pipeline

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Reconciler | Go package | internal/reconcile/merger.go |
| Clustering | Go maps | FILE + LINE±3 bucketing |
| Deduplication | Go strings | token-set Jaccard similarity |
| Confidence Scoring | Go math | weighted average by agent authority |
| Test Framework | testify | table-driven tests |

## Related Files
- `internal/reconcile/merger.go` - create: discover → normalize → cluster → dedupe → merge → confidence → emit
- `internal/reconcile/merger_test.go` - create: tests for each pipeline stage
- `internal/reconcile/cluster.go` - create: FILE+LINE±3 clustering logic
- `internal/reconcile/dedupe.go` - create: token-set similarity deduplication
- `internal/reconcile/ambiguous.go` - create: ambiguity sidecar types and JSON serialization
- `cmd/atcr/reconcile.go` - modify: integrate merger into reconcile command
- `cmd/atcr/main.go` - modify: wire `--fail-on <severity>` centralized exit-code logic

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Reconciler & Findings Stream](../documentation/reconciler.md) — Authoritative spec for the pipeline stages, merge rules table (REVIEWERS joined, SEVERITY max with disagreement annotation, PROBLEM/FIX longest, CATEGORY modal, EST_MINUTES max), ambiguity threshold (Jaccard ≥ 0.7 merge, < 0.4 separate, [0.4, 0.7) → ambiguous.json), and reconciled output files.
- [Findings Format v1](../documentation/findings-format.md) — `^(CRITICAL|HIGH|MEDIUM|LOW)\|` extraction regex; pipe escape `|` → `/`; short-row padding; per-source 8-col vs reconciled 9-col.
- [CLI Architecture](../documentation/cli-architecture.md) — `--fail-on` flag, centralized exit-code logic in `main()` (exit 0 = pass, 1 = threshold violation, 2 = usage/config error).

### Spec alignment notes

- **Reconciled column count**: per `original-requirements.md`, reconciled `findings.txt` is **9 columns** (per-source 8 with `REVIEWERS` plural + `CONFIDENCE`). The `documentation/findings-format.md` heading reads "10 columns" — treat that heading as a spec-doc typo. The example row, the `Finding` struct in `documentation/reconciler.md`, and `original-requirements.md` are all authoritative for 9 columns in v1.
- **Confidence** mapping: `HIGH` = 2+ distinct reviewers; `MEDIUM` = single reviewer; `LOW` = reserved for untrusted sources.
- **Source discovery rule** (open extension point): any child of `sources/` containing `findings.txt` is a reconcile source. `reconciled/` is never an input source. Per `plan.md`.
- **Severity disagreement** annotation preserved inline: when lower severities appear alongside the max, `disagreement: <lo> vs <hi>` is kept in the merged record.
- **All four reconciled artifacts emitted**: `findings.txt` (9-col), `findings.json`, `report.md`, `summary.json` — plus the `ambiguous.json` sidecar (always written; may be empty array if no gray-zone clusters).

## Happy Path Scenarios

**Scenario 1: Discover and normalize findings from all agents**
- **Given** 3 agents each produced `findings.txt` with pipe-delimited rows (8 columns)
- **When** reconciler discovers `sources/pool/raw/agent/*/findings.txt`
- **Then** all findings normalized into internal struct with fields: SEVERITY, FILE, LINE, PROBLEM, FIX, CATEGORY, EST_MINUTES, EVIDENCE, REVIEWER

**Scenario 2: Cluster by FILE + LINE±3**
- **Given** agent-a flags `main.go:42` and agent-b flags `main.go:44`
- **When** clustering runs
- **Then** both findings are placed in the same cluster (same file, lines within ±3)

**Scenario 3: Deduplicate by token-set similarity**
- **Given** two findings in same cluster with PROBLEM text token overlap >80%
- **When** deduplication runs
- **Then** findings merged into single finding; REVIEWER field lists both reviewers; PROBLEM text uses highest-authority version

**Scenario 4: Confidence scoring**
- **Given** a merged finding agreed on by 2 of 3 agents
- **When** confidence computed
- **Then** confidence = (agreeing_agents / total_agents) * authority_weight; result written as 9th column in findings.txt

**Scenario 5: Reconciled artifacts written**
- **Given** reconciliation completes
- **When** output phase runs
- **Then** files written to `reconciled/`: `findings.txt` (9-column pipe-delimited), `findings.json`, `report.md`, `summary.json`

## Edge Cases

**Edge Case 1: Single agent finding**
- **Given** only 1 agent produced findings (partial review)
- **When** reconciliation runs
- **Then** findings included with confidence = 1.0 (single source); REVIEWER field shows single agent

**Edge Case 2: Conflicting findings on same line**
- **Given** agent-a says `main.go:42` is CRITICAL; agent-b says same line is INFO
- **When** merge runs
- **Then** higher severity wins; confidence reflects disagreement (lower score)

**Edge Case 3: Agent with malformed findings.txt**
- **Given** agent produced findings.txt with wrong column count (6 instead of 8)
- **When** normalizer processes the file
- **Then** malformed rows skipped with warning logged; valid rows still processed

**Edge Case 4: Empty findings across all agents**
- **Given** all agents found zero issues
- **When** reconciliation runs
- **Then** reconciled findings.txt is empty; summary.json records `total_findings: 0`

## Error Conditions

**Error Scenario 1: No agent directories found**
- Error message: "no agent findings found in sources/pool/raw/agent/"
- Exit code: 1

**Error Scenario 2: findings.txt parse failure (all rows malformed)**
- Error message: "agent reviewer-a: all findings rows malformed (expected 8 columns, got N)"
- Exit code: 1

## Performance Requirements
- **Response Time:** Reconciliation of 500 findings across 5 agents completes in <2 seconds
- **Throughput:** Clustering uses map-based bucketing for O(n) performance

## Security Considerations
- **Input Validation:** All file paths in findings validated to prevent path traversal
- **Data Integrity:** Pipe-delimited format parsed with strict column count validation

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Sample findings.txt files with overlapping and distinct findings; edge cases with malformed rows; multi-agent scenarios with varying agreement
**Mock/Stub Requirements:** No external mocks needed; all logic is in-memory. Use table-driven tests for each pipeline stage.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Pipeline stages execute in order: discover → normalize → cluster → dedupe → merge → confidence → emit
- [ ] Clustering uses FILE + LINE±3 bucketing correctly
- [ ] Deduplication uses token-set Jaccard similarity with configurable threshold
- [ ] Confidence score computed and added as 9th column in reconciled findings.txt
- [ ] All 4 reconciled artifacts written: findings.txt, findings.json, report.md, summary.json
- [ ] Malformed findings.txt rows skipped with warning (not fatal)

**Manual Review:**
- [ ] Code reviewed and approved
