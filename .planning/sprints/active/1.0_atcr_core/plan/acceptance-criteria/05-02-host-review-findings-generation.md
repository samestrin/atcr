# Acceptance Criteria: Host Review Findings Generation

**Related User Story:** [05: Host Review via Skill](../user-stories/05-host-review-via-skill.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Host Review | AI agent (host model) | Agent reads payload and generates findings |
| Findings Format | Pipe-delimited text (v1 spec) | 8 columns per `docs/findings-format.md` |
| File Output | `sources/host/findings.txt` | Version header: `# atcr-findings/v1` |
| Review Markdown | `sources/host/review.md` | Human-readable review narrative |
| Test Framework | `testify` (assert, require) | Table-driven tests with fixture validation |

## Related Files
- `skill/SKILL.md` - modify: Add host review instructions with v1 format example row
- `internal/stream/parser.go` - create: Findings v1 parser (validates format, extracts columns)
- `internal/stream/parser_test.go` - create: Parser tests with valid/invalid fixture data
- `docs/findings-format.md` - create: Canonical v1 findings format specification
- `personas/_base.md` - reference: Skill uses the same adversarial personality clause from the base persona

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Findings Format v1](../documentation/findings-format.md) — Authoritative spec for the `# atcr-findings/v1` header, 8-col per-source format, severity enum, extraction regex `^(CRITICAL|HIGH|MEDIUM|LOW)\|`, pipe escape `|` → `/`, short-row padding.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — How the host's `findings.txt` is consumed by the source-discovery rule in reconcile.
- [Configuration & Registry](../documentation/configuration-management.md) — How the host agent's `REVIEWER` column gets set to `host`: the SKILL writes the complete 8-column v1 row including `REVIEWER` set to `host`; the "personas emit 7 columns and the engine appends REVIEWER" rule applies only to pool agents, whose findings.txt is written by the Go engine — the host path has no engine writer.

### Spec alignment notes

- **Host writes the complete 8-column v1 row** including `REVIEWER` set to `host`. The "personas emit 7 columns and the engine appends `REVIEWER`" rule applies only to pool agents, whose findings.txt is written by the Go engine; the host path has no engine writer. Per `plan.md` clarifications (2026-06-10) and `user-stories/05-host-review-via-skill.md` original criterion #5.
- **Adversarial personality clause** is required: the host review finds problems (bugs, security issues, logic errors, code quality), **not praise**. No-flattery rules; no positive observations. This mirrors the ported persona prompts in `personas/_base.md` per `plan.md` clarifications (2026-06-10).
- **Severity rubric** uses the closed enum: `CRITICAL|HIGH|MEDIUM|LOW`. No severity like `BLOCKER`, `INFO`, or `NIT` — the format spec rejects new severities without a coordinated minor version bump.
- **Files-mode scope rule** applies to the host too: in `files` payload mode, the host should focus on changed regions; pre-existing issues in unchanged regions of changed files use category `out-of-scope`. The reconciler will annotate rather than promote to reconciled findings.
- **`sources/host/` is the second source directory** alongside `sources/pool/`. Both are first-class per the source-discovery rule; the Skill contributes `host` so a user with a single API key still gets 2+ sources for the confidence signal.
- **File-level findings use line 0** (`path/to/file.go:0`), consistent with the v1 format.

## Happy Path Scenarios

**Scenario 1: Host reads payload and generates findings**
- **Given** the skill has invoked `atcr review` and the review directory contains payload files at `.atcr/reviews/<id>/payload/`
- **When** the host agent reads the payload (diff, blocks, or files per manifest)
- **Then** the host generates findings in v1 pipe-delimited format
- **And** each finding has exactly 8 columns: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER

**Scenario 2: Host findings written to sources/host/findings.txt**
- **Given** the host agent has generated findings
- **When** the findings are written to disk
- **Then** the file `.atcr/reviews/<id>/sources/host/findings.txt` is created
- **And** the first line is the version header `# atcr-findings/v1`
- **And** each subsequent non-comment line is a valid pipe-delimited finding

**Scenario 3: REVIEWER column set to host**
- **Given** the host agent generates a finding
- **When** the finding is formatted
- **Then** the REVIEWER column (8th column) contains the value `host`
- **And** the value is lowercase and consistent across all host findings

**Scenario 4: Host review markdown written alongside findings**
- **Given** the host agent has generated findings
- **When** the host writes its output
- **Then** `.atcr/reviews/<id>/sources/host/review.md` is created with a human-readable review narrative
- **And** the review.md content is consistent with the findings (same issues, same severities)

**Scenario 5: Findings format matches pool agent output**
- **Given** a pool agent has written findings to `sources/<agent>/findings.txt`
- **When** the host writes its findings to `sources/host/findings.txt`
- **Then** both files share the same version header and column structure
- **And** the reconciler can parse both files without format-specific logic

## Edge Cases

**Edge Case 1: Host finds no issues**
- **Given** the host agent has reviewed the payload
- **When** no problems are identified
- **Then** `sources/host/findings.txt` is created with only the version header
- **And** `sources/host/review.md` states that no issues were found

**Edge Case 2: Payload contains binary or generated files**
- **Given** the diff includes binary files or generated code
- **When** the host agent reads the payload
- **Then** the host skips non-text files in its review
- **And** does not generate findings for binary content

**Edge Case 3: Very large payload (exceeds host context window)**
- **Given** the payload is larger than the host model's context window
- **When** the host agent attempts to read it
- **Then** the host reviews the truncated payload (payload truncation is performed and recorded by the engine per AC 06-03; the host reviews the payload files as they exist on disk)
- **And** the host notes in review.md that the review covers a truncated payload

**Edge Case 4: Severity values are strictly from the valid set**
- **Given** the host agent generates a finding
- **When** the SEVERITY column is set
- **Then** the value is one of: CRITICAL, HIGH, MEDIUM, LOW
- **And** no other severity labels are used

**Edge Case 5: Literal pipe character in finding text**
- **Given** a host finding whose PROBLEM/FIX/EVIDENCE contains a literal `|`
- **When** the finding is written
- **Then** the `|` is replaced with `/` before writing (column count stays 8)

**Edge Case 6: Host emits a short row**
- **Given** the host emits a short row (e.g., empty EVIDENCE, 6 columns)
- **When** the file is validated/parsed
- **Then** the row is padded to 8 columns with empty strings (per the v1 format spec)

## Error Conditions

**Error Scenario 1: Findings file fails v1 format validation**
- Error message: "Host findings validation failed: line <N> has <M> columns, expected 8"
- Skill behavior: Halt and report format error; do not proceed to reconcile

**Error Scenario 2: Payload directory does not exist**
- Error message: "Payload directory not found: .atcr/reviews/<id>/payload/. Run 'atcr review' first."
- Skill behavior: Halt orchestration and instruct agent to run `atcr review`

**Error Scenario 3: Invalid severity in finding**
- Error message: "Invalid severity '<value>' on line <N>. Must be one of: CRITICAL, HIGH, MEDIUM, LOW"
- Skill behavior: Reject the finding; if all findings are invalid, halt

## Performance Requirements
- **Findings Generation:** The host-review step reads only files under the review directory (payload/) and invokes no atcr commands or network calls of its own
- **File Write:** Findings and review files written in < 500ms
- **Format Validation:** Parser validates a findings file in < 50ms for files up to 1000 lines

## Security Considerations
- **Input Validation:** Findings parser validates column count, severity values, and FILE:LINE format before accepting
- **No code execution:** Host review is text analysis only; the skill does not execute code from the diff
- **Path containment:** findings.txt and review.md are written only to `sources/host/` within the review directory
- **Evidence sanitization:** EVIDENCE column contains only text excerpts from the diff; no shell metacharacters injected

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Sample payload files (diff, blocks, files modes)
- Valid v1 findings fixture (8 columns, correct severities)
- Invalid findings fixtures (wrong column count, bad severity, malformed FILE:LINE)
- Empty payload fixture
**Mock/Stub Requirements:**
- Mock LLM response for host review (pre-canned findings output)
- Filesystem fixtures for review directory structure

**Test Cases:**
1. `TestParser_ValidFindings` — parse a valid v1 findings file with multiple rows
2. `TestParser_InvalidColumnCount` — reject rows with != 8 columns
3. `TestParser_InvalidSeverity` — reject rows with invalid severity values
4. `TestParser_VersionHeader` — verify version header is required
5. `TestParser_EmptyFindings` — handle file with only version header (no findings)
6. `TestHostReview_WritesFindingsFile` — verify host findings written to correct path
7. `TestHostReview_ReviewerColumnIsHost` — verify REVIEWER column = "host"
8. `TestHostReview_FormatMatchesPoolAgent` — verify host and pool findings share same structure

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] Findings parser validates all v1 format rules

**Story-Specific:**
- [ ] Host findings written to `sources/host/findings.txt` with version header
- [ ] All host findings have REVIEWER column set to `host`
- [ ] Findings use exactly 8 pipe-delimited columns matching v1 spec
- [ ] Severity values restricted to CRITICAL, HIGH, MEDIUM, LOW
- [ ] `sources/host/review.md` written alongside findings

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Host findings format verified against pool agent output in a real review run
