# Acceptance Criteria: Manifest Review Stage Entry

**Related User Story:** [05: Transcript & Accounting](../user-stories/05-transcript-accounting.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Manifest stages map | Go `map[string]interface{}` or typed struct on manifest builder | New `"review"` key added alongside existing stages |
| Agent classification | Per-agent `Tools` and `ToolsDegraded` flags from engine | Determines which list each agent appears in |
| JSON serialization | `encoding/json` marshal of stages map | Backward-compatible with 1.x manifest readers |
| Test framework | `go test` + manifest parsing | Parse `manifest.json` and assert stage entries |

## Related Files

- `internal/fanout/manifest.go` — modify: add `"review"` stage entry to manifest builder with agent lists
- `internal/payload/manifest.go` — modify: add `Review` field to manifest stages struct (if typed)
- `internal/fanout/manifest_test.go` — create/modify: tests for review stage entry with various agent configurations
- `internal/fanout/engine.go` — modify: pass per-agent effective `Tools` flag and degradation state to manifest builder

## Happy Path Scenarios

**Scenario 1: Review stage lists all tool-enabled agents**
- **Given** a roster with 3 agents: agent-a (`tools: true`), agent-b (`tools: true`), agent-c (`tools: false`)
- **When** the run completes and `manifest.json` is written
- **Then** `manifest.json` `stages.review` contains an entry listing agent-a and agent-b as tools-enabled
- **And** agent-c is NOT listed in the review stage (it ran as single-shot without tools)

**Scenario 2: Degraded agent included in review stage**
- **Given** agent-a (`tools: true`) that degraded to single-shot during the run
- **When** the run completes
- **Then** `manifest.json` `stages.review` includes agent-a in the tools-enabled list
- **And** agent-a is also listed in a tools-degraded list (or marked with a degraded flag)
- **And** the entry reflects that the agent _started_ with tools enabled

**Scenario 3: Budget-tripped agent included in review stage**
- **Given** agent-b (`tools: true`) that tripped its tool-byte budget mid-run
- **When** the run completes
- **Then** `manifest.json` `stages.review` includes agent-b in the tools-enabled list
- **And** agent-b is NOT listed as degraded (budget trip is not degradation)

**Scenario 4: All completion paths produce consistent review stage**
- **Given** a roster with agents that: completed normally (agent-a), degraded (agent-b), tripped budget (agent-c), had provider error (agent-d)
- **And** all had `tools: true` configured
- **When** the run completes
- **Then** all 4 agents appear in the review stage's tools-enabled list
- **And** agent-b additionally appears in tools-degraded
- **And** the stage entry is derived from the effective `Tools` flag at invocation time, not the completion path

**Scenario 5: No tool-enabled agents**
- **Given** a roster where all agents have `tools: false`
- **When** the run completes
- **Then** `manifest.json` `stages.review` either is absent, is empty, or lists an empty agents array
- **And** the manifest is still valid JSON

## Edge Cases

**Edge Case 1: Single agent roster with tools: true**
- **Given** a roster with exactly one agent configured with `tools: true`
- **When** the run completes
- **Then** `manifest.json` `stages.review` lists that single agent

**Edge Case 2: Agent with tools: true but MaxTurns: 1**
- **Given** an agent with `tools: true` and `MaxTurns: 1`
- **When** the run completes (the agent runs one turn with tools, then stops)
- **Then** the agent is listed in the review stage's tools-enabled list
- **And** the agent is NOT listed as degraded (it used tools as configured)

**Edge Case 3: Mixed roster with 1.x and 2.0 agents**
- **Given** a roster where some agents have `tools: true` (2.0) and others have `tools: false` (1.x compatible)
- **When** the run completes
- **Then** only the `tools: true` agents appear in the review stage
- **And** the `tools: false` agents appear in their existing stage entries (unmodified)

**Edge Case 4: All agents degrade**
- **Given** a roster where all `tools: true` agents degrade to single-shot
- **When** the run completes
- **Then** all agents still appear in the review stage's tools-enabled list
- **And** all agents also appear in the tools-degraded list
- **And** the review stage accurately reflects that tools were attempted for all

## Error Conditions

**Error Scenario 1: Manifest write fails**
- **Given** the manifest writer encounters an I/O error
- **When** the error occurs
- **Then** the error is logged
- **And** the review result is still produced (manifest write failure does not fail the review)

**Error Scenario 2: Backward compatibility — old reader encounters review stage**
- **Given** a 1.x manifest reader that does not know about the `"review"` stage
- **When** it reads a 2.0 `manifest.json`
- **Then** the reader ignores the unknown `"review"` key (standard JSON/map unmarshaling)
- **And** all existing 1.x stages are still present and correct

## Performance Requirements

- **Manifest write:** Single write at run finalization; negligible overhead
- **Stage entry construction:** O(n) where n is roster size; iterates agent results once to classify

## Security Considerations

- **No sensitive data:** Review stage contains only agent names and boolean flags (tools-enabled, tools-degraded); no tool results or content
- **Derived from engine state:** Stage entry is computed from the engine's per-agent invocation flags, not from external input

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Roster configurations with various tool-enabled/degraded combinations:
  - All tools-enabled, all single-shot, mixed
  - Degraded agents, budget-tripped agents, provider-error agents
  - Empty roster, single-agent roster
- Expected manifest JSON for each configuration

**Mock/Stub Requirements:**
- Fake engine results with controlled per-agent `Tools`, `ToolsDegraded`, and completion status
- `t.TempDir()` for manifest output
- JSON parser to read and assert `manifest.json` stages

**Test Cases to Implement:**
1. All tools-enabled agents listed in review stage
2. Single-shot agents (`tools: false`) excluded from review stage
3. Degraded agent in both tools-enabled and tools-degraded lists
4. Budget-tripped agent in tools-enabled but NOT in tools-degraded
5. Provider-error agent in tools-enabled
6. All agents degrade — all in tools-enabled and tools-degraded
7. No tools-enabled agents — review stage absent or empty
8. Mixed roster — only tools-enabled agents in review stage
9. Backward compatibility — 1.x stages unchanged with review stage added
10. Manifest write failure does not fail review

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `manifest.json` `stages` includes a `"review"` key when any agent has `tools: true`
- [ ] Review stage lists agents that had `tools: true` effective at invocation time
- [ ] Degraded agents appear in both tools-enabled and tools-degraded lists
- [ ] Budget-tripped agents appear in tools-enabled but NOT in tools-degraded
- [ ] Stage entry is derived from invocation-time flags, not completion path
- [ ] Backward compatibility: 1.x stages in `manifest.json` are unchanged

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Review stage schema reviewed for operator clarity
- [ ] Consistency across all completion paths verified (normal, degraded, budget-tripped, error)
