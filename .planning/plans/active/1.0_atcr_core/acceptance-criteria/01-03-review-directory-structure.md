# Acceptance Criteria: Review Directory Structure

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Directory Layout | Go os package | mkdir, file creation |
| Manifest Serialization | encoding/json | manifest.json |
| Latest Pointer | Go os package | symlink or text file |
| Test Framework | testify | assertions |

## Related Files
- `internal/reviewdir/creator.go` - create: review directory scaffolding and manifest writing
- `internal/reviewdir/creator_test.go` - create: tests for directory creation and manifest
- `cmd/atcr/review.go` - modify: call directory creator after range resolution

## Happy Path Scenarios

**Scenario 1: Directory created with correct structure**
- **Given** range resolution succeeded with base=abc123, head=def456
- **When** review directory is created
- **Then** directory `.atcr/reviews/YYYY-MM-DD_branch-slug/` exists with subdirs: `payload/`, `sources/pool/raw/agent/{agent-name}/`, `sources/host/`, `reconciled/`

**Scenario 2: manifest.json contains required fields**
- **Given** review directory is created
- **When** manifest.json is written
- **Then** manifest contains: base SHA, head SHA, detection_mode, payload_mode(s), roster, timestamps, partial=false

**Scenario 3: Latest pointer updated**
- **Given** a previous review exists at `.atcr/reviews/2026-06-09_old-feature/`
- **When** new review completes at `.atcr/reviews/2026-06-10_new-feature/`
- **Then** `.atcr/latest` file contains the new review ID `2026-06-10_new-feature`

**Scenario 4: Branch slug generation**
- **Given** current branch is `feature/JIRA-123-add-auth`
- **When** slug is generated from branch name
- **Then** slug is `JIRA-123-add-auth` (prefix `feature/` stripped, special chars replaced with `-`)

## Edge Cases

**Edge Case 1: Review directory already exists**
- **Given** `.atcr/reviews/2026-06-10_feature/` already exists from a prior run
- **When** user runs `atcr review` again on the same day with same branch
- **Then** timestamp suffix appended to avoid collision: `2026-06-10_feature-143022/`

**Edge Case 2: --id flag override**
- **Given** user provides `--id custom-review-id`
- **When** review directory is created
- **Then** directory is `.atcr/reviews/custom-review-id/` regardless of date/branch

**Edge Case 3: .atcr directory does not exist**
- **Given** fresh project without `.atcr/` directory
- **When** review directory creation runs
- **Then** `.atcr/` and `.atcr/reviews/` are created automatically via MkdirAll

## Error Conditions

**Error Scenario 1: Permission denied creating directory**
- Error message: "failed to create review directory: permission denied"
- Exit code: 1

**Error Scenario 2: Failed to write manifest**
- Error message: "failed to write manifest.json: <reason>"
- Exit code: 1

## Performance Requirements
- **Response Time:** Directory creation and manifest write completes in <100ms
- **Throughput:** N/A (single sequential operation)

## Security Considerations
- **Input Validation:** Review ID sanitized to prevent path traversal (no `..`, `/`, or absolute paths)
- **File Permissions:** Directories created with 0755; files with 0644

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Temporary directory as test fixture; sample branch names, SHAs, and roster data
**Mock/Stub Requirements:** Use `t.TempDir()` for isolated filesystem tests; no external mocks needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Directory structure matches spec: `payload/`, `sources/pool/raw/agent/`, `sources/host/`, `reconciled/`
- [ ] manifest.json contains all required fields (base, head, detection_mode, payload_modes, roster, timestamps, partial)
- [ ] `.atcr/latest` pointer updated to most recent review ID
- [ ] Slug generation strips branch prefixes and sanitizes special characters

**Manual Review:**
- [ ] Code reviewed and approved
