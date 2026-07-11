# Acceptance Criteria: Dispatcher Command Routing Table

**Related User Story:** [01: Dispatcher Skill Rewrite](../user-stories/01-dispatcher-skill-rewrite.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown Agent Skill (Level 1 frontmatter + Level 2 body) | `skill/SKILL.md`, no executable code |
| Test Framework | Go `testing` + `testify` (`assert`/`require`) over the embedded string | `skill/skill_test.go` |
| Key Dependencies | `//go:embed` (`skill/skill.go`), Cobra command tree (`cmd/atcr/main.go`) | routing table must mirror `newRootCmd` registration order and names |

### Related Files (from codebase-discovery.json)

- `skill/SKILL.md` — modify: rewrite frontmatter `description` and add a Level 2 command-routing table/section that maps `/atcr <command>` to each of the 21 Cobra commands
- `cmd/atcr/main.go:185-208` — reference only (do not modify): the `newRootCmd` `AddCommand(...)` call is the ground-truth command inventory (`review`, `reconcile`, `verify`, `debate`, `report`, `github`, `range`, `status`, `init`, `quickstart`, `serve`, `doctor`, `trust`, `scorecard`, `leaderboard`, `benchmark`, `personas`, `models`, `debt`, `history`, `audit-report`, `version`)
- `skill/skill_test.go` — modify: add a routing-table coverage test (e.g. `TestSkill_DispatcherRoutingTable`) that asserts every command name from the ground-truth list appears in `SkillMD`
- `.planning/specifications/packages/cobra.md` — reference only: known-stale package doc (references a non-existent `anchor` subcommand); must NOT be used as the source of the routing table

## Design References

- [CLI Dispatcher Conventions](../documentation/cli-dispatcher-conventions.md) — Cobra command/subcommand conventions the `/atcr` dispatcher must mirror
- [Agent Skill Format & Progressive Disclosure](../documentation/agent-skill-format.md) — SKILL.md frontmatter and secondary-file loading model governing the dispatcher rewrite

## Happy Path Scenarios
**Scenario 1: Every live Cobra command is routed**
- **Given** the rewritten `skill/SKILL.md`
- **When** the routing table/section is scanned for command names
- **Then** all 21 names registered in `newRootCmd` (`cmd/atcr/main.go:185-208`) are present, each associated with a one-line summary consistent with that command's `Short` description (e.g. `review` → "Fan a code change out to the reviewer pool")

**Scenario 2: Dispatcher invocation pattern is documented**
- **Given** the rewritten `skill/SKILL.md` body
- **When** an agent reads the Overview/routing section
- **Then** it explicitly documents the `/atcr <command> <flags>` invocation pattern and states that every command maps 1:1 to an `atcr <command>` CLI invocation (never a direct engine call)

**Scenario 3: Frontmatter description reflects the dispatcher**
- **Given** the YAML frontmatter block (delimited by the first two `---` lines)
- **When** `description` is read
- **Then** it describes a general-purpose `/atcr <command>` dispatcher (not only the single review→reconcile→report flow) so Claude Code's skill-discovery step can match a broader range of user requests

## Edge Cases
**Edge Case 1: New command added to `newRootCmd` after this rewrite**
- **Given** a future commit adds a 22nd command to `newRootCmd`
- **When** `skill/skill_test.go`'s routing-table test runs unchanged
- **Then** the test does not silently pass — it must enumerate the same command list from a shared source (or be reviewed) so routing-table drift is caught, not just documented as a manual review step

**Edge Case 2: Command has multiple subcommands (e.g. `personas`, `debt`, `models`, `benchmark`)**
- **Given** a top-level command that itself has subcommands (`personas install`, `debt list`, `models drift`, `benchmark run`, etc.)
- **When** the routing table lists it
- **Then** the top-level command name is routed at Level 2; subcommand-level detail is either a compact inline note or deferred to Level 3 without inventing subcommand names not present in the corresponding `cmd/atcr/*.go` file

**Edge Case 3: No command argument provided**
- **Given** the agent invokes `/atcr` with no command argument
- **When** the dispatcher parses the input
- **Then** it responds with a compact command list or asks for a valid command, and does not silently default to the review-only flow

## Error Conditions
**Error Scenario 1: Invented or drifted command name**
- **Given** the routing table contains a command name not present in `newRootCmd` (e.g. a legacy `anchor` command carried over from the stale `cobra.md` snapshot)
- **Then** this is a story-acceptance failure, not a runtime error — flagged during manual/automated review as "routing table references non-existent command `<name>`"

**Error Scenario 2: Missing command**
- **Given** any of the 21 `newRootCmd` command names is absent from `SkillMD`
- **Then** `TestSkill_DispatcherRoutingTable` fails with an assertion message naming the missing command

## Performance Requirements
- **Response Time:** N/A — static Markdown content; no runtime execution. Skill file load time is bounded only by Claude Code's own file-read latency (out of scope for this AC).
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no new auth surface; the dispatcher still never reaches into the engine directly, only invokes the `atcr` binary (as today).
- **Input Validation:** The routing table itself is static Markdown with no user-controlled input; command *arguments* validation is delegated to the `atcr` binary's own Cobra flag parsing (unchanged).

## Test Implementation Guidance
**Test Type:** UNIT (Go `testing` over the embedded `SkillMD` string)
**Test Data Requirements:** The literal list of 21 command names/short descriptions extracted from `cmd/atcr/main.go:185-208` and each command's `Short:` field; no external fixtures needed.
**Mock/Stub Requirements:** None — pure string assertions against the embedded constant, consistent with the existing `TestSkill_RequiredSections`/`TestSkill_InputForms` pattern in `skill/skill_test.go`.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] All 21 `newRootCmd` command names appear in `SkillMD` with a one-line summary each
- [ ] Frontmatter `description` reflects a general dispatcher, not a single review-only flow
- [ ] No command name in the routing table is absent from, or contradicts, `cmd/atcr/main.go:185-208`
- [ ] `skill/skill_test.go` gains a routing-table coverage test

**Manual Review:**
- [ ] Code reviewed and approved
