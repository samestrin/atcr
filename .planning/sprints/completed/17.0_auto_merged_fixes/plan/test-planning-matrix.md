# Test Planning Matrix

**Generated:** 2026-07-02
**Plan:** 17.0_auto_merged_fixes
**Total ACs:** 23

---

## Summary by Story

| Story | ACs | Unit | Integration | E2E | Complexity |
|-------|-----|------|-------------|-----|------------|
| 01 - Apply a Parsed Patch to the Working Tree Without Corruption | 4 | 4 | 0 | 0 | Medium-High |
| 02 - Configurable Local Validation | 3 | 3 | 0 | 0 | Medium |
| 03 - Automatic Revert on Validation Failure | 4 | 4 | 1 | 0 | Medium-High |
| 04 - Create a Branch and Commit the Verified Fix | 5 | 4 | 1 | 0 | High |
| 05 - Open or Update Pull Request via GitHub API | 4 | 3 | 1 | 0 | Medium |
| 06 - Opt In via `--auto-fix` with a Refuse-Without-Backend Gate | 3 | 3 | 3 | 0 | Medium-High |

---

## Detailed AC List

### Story 1: Apply a Parsed Patch to the Working Tree Without Corruption

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 01-01 | Parse and Apply Hunks for Modify, Create, and Delete Entries | Unit | High | P1 |
| 01-02 | Every Target Write Goes Through Atomic Sibling-Temp-Then-Rename | Unit | Medium | P1 |
| 01-03 | Per-File Backup via BackupToDotBak Before Any Overwrite | Unit | Low | P1 |
| 01-04 | A Failed Hunk Reports a Clear Per-File Error Without Corrupting Prior Successes | Unit | Medium | P2 |

### Story 2: Configurable Local Validation

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 02-01 | Configurable Validation Command Runner | Unit | Medium | P1 |
| 02-02 | Validation Result Capture and Reporting | Unit | Low | P2 |
| 02-03 | Conservative Pass/Fail Gate (No Mutation, No Partial Success) | Unit | Medium | P1 |

### Story 3: Automatic Revert on Validation Failure

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 03-01 | Per-File Backup Map Precondition and Tracking | Unit | Low | P2 |
| 03-02 | Restore All Touched Files on Validation Failure | Unit + Integration | High | P1 |
| 03-03 | Cleanup Backup Files on Validation Success | Unit | Low | P2 |
| 03-04 | Hard Error Surfacing on Restore Failure | Unit | Medium | P1 |

### Story 4: Create a Branch and Commit the Verified Fix

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 04-01 | CreateBranch Creates a New Git Ref at a Base SHA | Unit | Medium | P1 |
| 04-02 | Branch Name Collision Is Distinguishable and Recoverable | Unit | Low | P2 |
| 04-03 | CreateCommit Builds a Multi-File Commit via Blob/Tree/Commit/Ref Sequence | Unit | High | P1 |
| 04-04 | CreateBranch/CreateCommit Are Unreachable Without a Prior Validation Success | Integration | High | P1 |
| 04-05 | New Endpoints Inherit Existing Retry, Backoff, and Redaction Behavior | Unit | Medium | P2 |

### Story 5: Open or Update Pull Request via GitHub API

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 05-01 | CreatePullRequest Opens a New PR From the Auto-Fix Branch | Unit | Medium | P1 |
| 05-02 | Existence Check Decides Create-vs-Update and Avoids Duplicate PRs | Integration | Medium | P1 |
| 05-03 | UpdatePullRequest Refreshes an Existing Open PR | Unit | Low | P2 |
| 05-04 | PR Endpoints Reuse Existing Retry/Backoff Plumbing and Redact Secrets From PR Content | Unit | Medium | P2 |

### Story 6: Opt In via `--auto-fix` with a Refuse-Without-Backend Gate

| AC ID | Title | Test Type | Complexity | Priority |
|-------|-------|-----------|------------|----------|
| 06-01 | `--auto-fix` Off by Default, Zero Behavior Change When Absent | Unit + Integration | Medium | P1 |
| 06-02 | Single Gate Refuses the Entire `--auto-fix` Run on Any Missing/Malformed Backend Piece | Unit + Integration | High | P1 |
| 06-03 | Gate Passes Silently When Fully Configured, No Overhead Into Story 1 | Unit + Integration | Medium | P2 |

---

## Test Coverage Notes

- **Unit Tests:** 21 ACs require unit tests (18 unit-only, 3 combined unit+integration also counted here)
- **Integration Tests:** 6 ACs require integration tests (03-02, 04-04, 05-02, 06-01, 06-02, 06-03)
- **E2E Tests:** 0 ACs require E2E tests — the plan's cross-system surface (AC5) is verified against a stubbed GitHub API (`httptest.Server`), not a live GitHub E2E run
- **High Complexity:** 5 ACs marked high complexity (01-01, 03-02, 04-03, 04-04, 06-02) — concentrated in the core patch-apply logic, the revert safety net, and the Git Data API multi-call sequence, matching the plan's own risk assessment that AC2/AC4/AC5 are the genuinely new, highest-risk surface
