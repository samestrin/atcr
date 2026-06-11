# Acceptance Criteria: Byte Budget, Truncation, and Manifest Recording

**Related User Story:** [06: Payload Mode Selection](../user-stories/06-payload-mode-selection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Byte Budget | Go `int64` arithmetic | Configurable max payload size |
| Truncation Strategy | Sort files by size rank, drop smallest first | Deterministic — same input always produces same output |
| manifest.json | Go `encoding/json` | Records default and per-agent payload modes |
| status.json | Go `encoding/json` | Records truncation per agent |
| Test Framework | `testify` (assert, require) | Table-driven tests with size fixtures |

## Related Files
- `internal/payload/budget.go` - create: Byte budget enforcement, deterministic truncation logic
- `internal/payload/budget_test.go` - create: Tests for budget enforcement and truncation ordering
- `internal/payload/manifest.go` - create: manifest.json generation with payload mode recording
- `internal/payload/manifest_test.go` - create: Tests for manifest structure and content
- `internal/fanout/status.go` - modify: Add truncation recording fields to status.json

## Happy Path Scenarios

**Scenario 1: Payload within byte budget passes through unchanged**
- **Given** a byte budget of 100,000 bytes
- **And** a total payload of 50,000 bytes across 5 files
- **When** the budget enforcer processes the payload
- **Then** all 5 files are included unchanged
- **And** `truncated` in status.json is `false`

**Scenario 2: Payload exceeding budget drops files by size rank (smallest first)**
- **Given** a byte budget of 100,000 bytes
- **And** files sized: A=60KB, B=30KB, C=20KB, D=10KB (total 120KB)
- **When** the budget enforcer processes the payload
- **Then** file D (10KB) is dropped first → total 110KB, still over
- **And** file C (20KB) is dropped next → total 90KB, under budget
- **And** files A and B remain in the payload
- **And** `files_dropped` in status.json lists `["C", "D"]`

**Scenario 3: Truncation is deterministic**
- **Given** the same set of files with the same sizes and the same budget
- **When** the budget enforcer runs twice
- **Then** the same files are dropped in the same order
- **And** the resulting payload is identical

**Scenario 4: manifest.json records default and per-agent payload modes**
- **Given** default payload mode is "blocks"
- **And** agent "bruce" has override "diff", agent "greta" uses default
- **When** the fan-out engine writes manifest.json
- **Then** manifest.json contains:
  ```json
  {
    "payload_mode": "blocks",
    "per_agent_payload": {
      "bruce": "diff",
      "greta": "blocks"
    }
  }
  ```

**Scenario 5: Truncation recorded in agent status.json**
- **Given** agent "bruce" had 3 files dropped due to byte budget
- **When** the fan-out engine writes status.json for "bruce"
- **Then** status.json contains:
  ```json
  {
    "agent": "bruce",
    "status": "completed",
    "payload_mode": "diff",
    "truncated": true,
    "files_dropped": ["file1.py", "file2.py", "file3.py"]
  }
  ```

## Edge Cases

**Edge Case 1: Single file exceeds entire budget**
- **Given** a byte budget of 10,000 bytes
- **And** a single file of 15,000 bytes
- **When** the budget enforcer processes the payload
- **Then** the file is dropped
- **And** the payload is empty
- **And** status.json records the single file as dropped with `truncated: true`

**Edge Case 2: All files dropped — empty payload**
- **Given** a byte budget of 100 bytes
- **And** all files exceed the budget individually
- **When** the budget enforcer processes
- **Then** all files are dropped
- **And** the payload is empty
- **And** status.json lists all files as dropped

**Edge Case 3: Byte budget set to zero (unlimited)**
- **Given** byte budget configured as 0 (meaning no limit)
- **When** the budget enforcer processes
- **Then** no files are dropped regardless of total size
- **And** `truncated` is `false`

**Edge Case 4: Payload exactly at budget limit**
- **Given** a byte budget of 50,000 bytes
- **And** a total payload of exactly 50,000 bytes
- **When** the budget enforcer processes
- **Then** no files are dropped
- **And** `truncated` is `false`

**Edge Case 5: Multiple agents with different modes and budgets**
- **Given** agent "bruce" uses diff mode (small payload) and agent "greta" uses files mode (large payload)
- **And** each agent has independent byte budgets
- **When** the fan-out engine builds payloads for both
- **Then** each agent's truncation is recorded independently in their respective status.json
- **And** manifest.json correctly reflects each agent's payload mode

## Error Conditions

**Error Scenario 1: Negative byte budget**
- Error message: "byte budget must be >= 0, got <n>"
- Exit code: 1

**Error Scenario 2: Failed to write manifest.json**
- Error message: "failed to write manifest.json: <detail>"
- Exit code: 1

**Error Scenario 3: Failed to write status.json**
- Error message: "failed to write status.json for agent '<name>': <detail>"
- Exit code: 1

## Performance Requirements
- **Response Time:** Budget enforcement < 1ms for < 100 files (sort + slice)
- **Throughput:** N/A (single pass per agent)
- **Determinism:** Same input always produces identical output (sorted by size rank, then by filename for ties)

## Security Considerations
- **Input Validation:** Byte budget validated as non-negative integer
- **Path Safety:** File sizes computed from payload content, not filesystem reads at budget stage
- **No Silent Drops:** Truncation is always recorded — invariant enforced by code structure

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- File size fixtures: arrays of (filename, size) pairs
- Budget values: zero, exact fit, under, over, single-file-exceeds
- Expected drop ordering for verification

**Mock/Stub Requirements:**
- Filesystem: use in-memory byte slices for payload content
- No git or network dependencies

**Test Cases:**
1. `TestBudget_UnderLimit` — no files dropped
2. `TestBudget_OverLimit_DropsSmallestFirst` — correct drop ordering
3. `TestBudget_SingleFileExceeds` — file dropped, empty payload
4. `TestBudget_AllFilesDropped` — all exceed individually
5. `TestBudget_ZeroIsUnlimited` — no truncation
6. `TestBudget_ExactFit` — no truncation
7. `TestBudget_Deterministic` — same input → same output on repeated runs
8. `TestBudget_TieBreaking` — files of equal size broken by filename sort
9. `TestManifest_RecordsDefaultAndPerAgent` — verify JSON structure
10. `TestStatusJSON_RecordsTruncation` — verify truncated flag and files_dropped list
11. `TestBudget_NegativeBudget` — verify error

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds
- [ ] Truncation ordering verified against expected output
- [ ] manifest.json and status.json match expected schemas

**Story-Specific:**
- [ ] Byte budget drops whole files by size rank (smallest first) deterministically
- [ ] Truncation always recorded in status.json — `truncated: bool` and `files_dropped: []`
- [ ] Truncation is never silent — invariant enforced in code
- [ ] manifest.json records `payload_mode` (default) and `per_agent_payload` (map)
- [ ] Each agent's truncation is independent

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Truncation invariant (never silent) audited in code path
- [ ] manifest.json schema matches design doc
