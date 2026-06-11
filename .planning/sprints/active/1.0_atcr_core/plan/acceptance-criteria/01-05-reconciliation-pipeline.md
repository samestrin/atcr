# Acceptance Criteria: Reconciliation Pipeline

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Reconciler | Go package | internal/reconcile/merger.go |
| Clustering | Go maps | FILE + LINE±3 bucketing |
| Deduplication | Go strings | token-set Jaccard similarity |
| Confidence Assignment | Go package | categorical: HIGH (2+ reviewers), MEDIUM (single), LOW (untrusted) |
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
- **Given** two findings in same cluster with PROBLEM text Jaccard token-set similarity ≥ 0.7
- **When** deduplication runs
- **Then** findings merged into single finding; REVIEWER field lists both reviewers; PROBLEM and FIX use the longest (most detailed) text among duplicates

**Scenario 4: Confidence assignment**
- **Given** a merged finding agreed on by 2 of 3 agents
- **When** confidence is assigned
- **Then** CONFIDENCE = HIGH (2+ distinct reviewers agree on the merged cluster); a single-reviewer finding gets CONFIDENCE = MEDIUM; LOW is reserved for untrusted sources; the value is written as the 9th column in findings.txt

**Scenario 5: Reconciled artifacts written**
- **Given** reconciliation completes
- **When** output phase runs
- **Then** files written to `reconciled/`: `findings.txt` (9-column pipe-delimited), `findings.json`, `report.md`, `summary.json`

**Scenario 6: `summary.json` records run stats**
- **Given** reconciliation completes
- **When** `summary.json` is written
- **Then** the file contains the following fields (per US-01 #13):
  - `sources_scanned`: list of source names whose `findings.txt` was discovered and parsed
  - `per_source_counts`: map of source name → number of input findings
  - `clusters_collapsed`: integer count of clusters that merged ≥2 findings into one
  - `severity_disagreements`: integer count of clusters where the max-severity record had a lower-severity sibling preserved with `disagreement:` annotation
  - `partial`: bool — true when ≥1 source was missing/unreadable but ≥1 succeeded
  - `total_findings`: integer count of findings in the reconciled output
  - `reconciled_at`: RFC3339 timestamp

**Scenario 7: `--sources` allowlist restricts which source directories are reconciled**
- **Given** the user invokes `atcr reconcile --sources pool,host` on a review with sources `{pool, host, ci-extras}` present
- **When** the reconciler discovers sources
- **Then** only `pool` and `host` are read; `ci-extras` is skipped (recorded in `summary.json.sources_scanned` as not included)
- **And** the reconciled output reflects only the allowlisted sources
- **And** if the allowlist is empty or omitted, all sources under `sources/` (excluding `reconciled/`) are processed (default open-discovery behavior per `plan.md` Reconciler step 1)
- **Note:** `--sources` filters the immediate children of `sources/` (e.g. pool, host); within an allowed source, discovery still finds any nested `findings.txt` (e.g. `pool/raw/agent/<agent>/`); `reconciled/` is never an input.

## Edge Cases

**Edge Case 1: Single agent finding**
- **Given** only 1 agent produced findings (partial review)
- **When** reconciliation runs
- **Then** findings included with CONFIDENCE = MEDIUM (single reviewer); REVIEWER field shows single agent

**Edge Case 2: Conflicting findings on same line**
- **Given** agent-a says `main.go:42` is CRITICAL; agent-b says same line is LOW
- **When** merge runs
- **Then** the merged finding keeps SEVERITY = max with a `disagreement: <lo> vs <hi>` annotation preserved; CONFIDENCE follows the reviewer-count rule (disagreement does not change confidence)

**Edge Case 3: Agent with malformed findings.txt**
- **Given** agent produced findings.txt with wrong column count (6 instead of 8)
- **When** normalizer processes the file
- **Then** malformed rows skipped with warning logged; valid rows still processed

**Edge Case 4: Empty findings across all agents**
- **Given** all agents found zero issues
- **When** reconciliation runs
- **Then** reconciled findings.txt is empty; summary.json records `total_findings: 0`

**Edge Case 5: Gray-zone similarity goes to ambiguous.json**
- **Given** two findings at the same location with PROBLEM similarity in the gray zone (0.4–0.7)
- **When** deduplication runs
- **Then** the cluster is written to ambiguous.json and both findings remain unmerged in the output (conservative default); ambiguous.json is always written, containing an empty clusters array when no gray-zone clusters exist

**Edge Case 6: LINE±3 clustering boundary**
- **Given** two findings in the same file at lines N and N+3
- **When** clustering runs
- **Then** they share a cluster; at lines N and N+4 they do not (LINE±3 boundary)

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
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Pipeline stages execute in order: discover → normalize → cluster → dedupe → merge → confidence → emit
- [x] Clustering uses FILE + LINE±3 bucketing correctly
- [x] Deduplication uses token-set Jaccard similarity with fixed v1 thresholds: merge at Jaccard ≥ 0.7; gray zone 0.4–0.7 goes to ambiguous.json (thresholds are not configurable in v1)
- [x] Confidence score computed and added as 9th column in reconciled findings.txt
- [x] All 4 reconciled artifacts written: findings.txt, findings.json, report.md, summary.json
- [x] `summary.json` contains: `sources_scanned`, `per_source_counts`, `clusters_collapsed`, `severity_disagreements`, `partial`, `total_findings`, `reconciled_at` (per US-01 #13)
- [x] `--sources <list>` allowlist flag restricts reconciled source directories; omitted/empty means open discovery (all sources except `reconciled/`)
- [x] Malformed findings.txt rows skipped with warning (not fatal)

**Manual Review:**
- [ ] Code reviewed and approved
