# Acceptance Criteria: Scores Column Display

**Related User Story:** [05: Corroboration Feedback via Persona Scores](../user-stories/05-corroboration-feedback.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI flag | Go / Cobra | `--scores` boolean flag on `personas list` |
| Scorecard aggregation | `internal/scorecard` package | `Aggregate()` → `[]LeaderboardRow{ReviewerName, CorroborationRate}` |
| Join logic | `internal/personas` package | Map `map[string]float64` keyed by lowercase reviewer name |
| Table rendering | `text/tabwriter` (stdlib) | Column alignment for `CORROBORATION` column |
| Test Framework | `go test` / `testify` | Unit tests for join + format logic |
| Key Dependencies | `internal/scorecard`, `fmt`, `strings`, `text/tabwriter` | No new external deps |

## Related Files
- `internal/personas/list.go` - modify: when `--scores` is set, call `scorecard.Aggregate()`, build `map[string]float64` keyed by `strings.ToLower(row.ReviewerName)`, join against `[]PersonaMeta` using `strings.ToLower(meta.Name)`, append `CORROBORATION` column; format rate as `fmt.Sprintf("%.1f%%", rate*100)` or `"n/a"` when key absent
- `internal/scorecard/aggregate.go` - read-only reference: `Aggregate()` at line 118 returns `[]LeaderboardRow`; no changes required
- `internal/scorecard/scorecard.go` - read-only reference: `Record` struct at line 52 with `CorroborationRate float64`; no changes required
- `internal/personas/list_test.go` - modify: add `TestPersonasListWithScores` covering present-in-map and absent-from-map personas; add mixed-case fixture to confirm case-insensitive join

### Related Files (from codebase-discovery.json)

- `internal/personas/list.go` — modify: `--scores` join and formatting logic
- `internal/scorecard/aggregate.go:118` — `Aggregate()` and `LeaderboardRow`
- `internal/scorecard/scorecard.go:52` — `Record` schema with `CorroborationRate`
- `internal/scorecard/paths.go:23` — `scorecard.DefaultDir()` for scorecard path
- `internal/personas/list_test.go` — modify: add scores column tests

## Happy Path Scenarios

**Scenario 1: Persona present in scorecard map shows formatted rate**
- **Given** `sentinel` has a `CorroborationRate` of `0.725` in the scorecard JSONL
- **When** the user runs `atcr personas list --scores`
- **Then** the `sentinel` row in the `CORROBORATION` column displays `72.5%`

**Scenario 2: Persona absent from scorecard shows n/a**
- **Given** `tracer` has never appeared in any review run recorded in the scorecard
- **When** the user runs `atcr personas list --scores`
- **Then** the `tracer` row in the `CORROBORATION` column displays `n/a`

**Scenario 3: Mixed installed personas — some with data, some without**
- **Given** three personas are installed: `sentinel` (rate 0.72), `idiomatic` (rate 0.50), `tracer` (no data)
- **When** the user runs `atcr personas list --scores`
- **Then** all three rows are present; `sentinel` shows `72.0%`, `idiomatic` shows `50.0%`, `tracer` shows `n/a`

## Edge Cases

**Edge Case 1: Scorecard JSONL is absent (file not found)**
- **Given** `~/.config/atcr/scorecard.jsonl` does not exist
- **When** the user runs `atcr personas list --scores`
- **Then** all persona rows show `n/a` in the `CORROBORATION` column and a footer note reads `No scorecard data found at <path>` (path resolved from `scorecard.DefaultDir()`)

**Edge Case 2: Scorecard JSONL exists but is empty (zero records)**
- **Given** the scorecard file exists with zero bytes or zero valid JSONL lines
- **When** the user runs `atcr personas list --scores`
- **Then** all persona rows show `n/a`; no error is returned; exit code is 0

**Edge Case 3: Reviewer name casing mismatch**
- **Given** the scorecard has `ReviewerName = "Sentinel"` (capital S) and the persona metadata has `Name = "sentinel"` (lowercase)
- **When** the user runs `atcr personas list --scores`
- **Then** the join succeeds and `sentinel` displays its formatted corroboration rate (case-insensitive key lookup via `strings.ToLower`)

**Edge Case 4: Rate is exactly 0.0 (all findings rejected)**
- **Given** `sentinel` has a `CorroborationRate` of `0.0`
- **When** the user runs `atcr personas list --scores`
- **Then** the `sentinel` row shows `0.0%` (not `n/a`) to distinguish "has data, zero corroboration" from "no data"

## Error Conditions

**Error Scenario 1: `scorecard.Aggregate()` returns an unexpected error**
- Error message: `"failed to load scorecard data: <underlying error>"`
- Error code: command exits 1; stderr contains the error message; no partial table is printed

## Performance Requirements
- **Response Time:** `atcr personas list --scores` completes in under 500 ms for a scorecard JSONL with up to 10,000 records (in-memory scan, no additional I/O beyond what `Aggregate()` already performs)
- **Throughput:** N/A — single interactive CLI invocation

## Security Considerations
- **Authentication/Authorization:** Read-only access to `~/.config/atcr/scorecard.jsonl`; no network calls; no privilege escalation
- **Input Validation:** `CorroborationRate` from the scorecard is a `float64`; clamp display to `[0.0, 100.0]%` range; malformed JSONL lines are skipped by the existing `ReadAll` path (no change required)

## Test Implementation Guidance
**Test Type:** UNIT  
**Test Data Requirements:**  
- A `[]LeaderboardRow` fixture with at least 3 rows: one matching a persona (correct case), one matching with different casing, one with no corresponding persona  
- A `[]PersonaMeta` fixture with at least 3 entries: one with scorecard data, one without, one whose name differs in case from the scorecard entry  
- A `CorroborationRate = 0.0` case to verify it renders as `0.0%` not `n/a`  

**Mock/Stub Requirements:** Inject `aggregateFn func() ([]scorecard.LeaderboardRow, error)` into the list command or package so tests supply a fake without touching the filesystem; this same injection point is used in AC 05-01 to verify the function is not called without `--scores`

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./internal/personas/... ./cmd/atcr/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `TestPersonasListWithScores` passes: persona with data shows `XX.X%`, persona without data shows `n/a`
- [x] Mixed-case join fixture test passes (case-insensitive lookup confirmed)
- [x] Rate `0.0` renders as `0.0%`, not `n/a` (zero-rate assertion in test)
- [x] Absent scorecard file produces footer note `No scorecard data found at <path>` and all-`n/a` table

**Manual Review:**
- [ ] Code reviewed and approved
