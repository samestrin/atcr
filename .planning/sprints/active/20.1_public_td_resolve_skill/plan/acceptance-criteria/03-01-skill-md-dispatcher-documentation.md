# Acceptance Criteria: SKILL.md Dispatcher Documentation for `/atcr debt resolve`

**Related User Story:** [03: `/atcr debt resolve` Skill Route](../user-stories/03-atcr-debt-resolve-skill-route.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown Agent Skill (dispatcher routing table) | `skill/SKILL.md` |
| Test Framework | go test (`skill/skill_test.go`) | Assertions run against the embedded `SkillMD` string, not a live agent invocation |
| Key Dependencies | `skill/skill.go` (`//go:embed`), existing `dispatcherCommands` test fixture | No new Go dependency |

### Related Files (from codebase-discovery.json)
- `skill/SKILL.md` — modify: extend the existing `atcr debt` command-table row (line 79) with a mention of the `resolve` subcommand, and add an on-demand load pointer to `skill/debt-resolve/SKILL.md`, mirroring how `## Host Review Instructions` points at `host-review.md`
- `skill/skill_test.go` — modify: add an assertion that `SKILL.md` documents `debt resolve` and that the pointer to `skill/debt-resolve/SKILL.md` (or its embedded constant) is present
- `skill/skill.go` — modify: embed the new `skill/debt-resolve/SKILL.md` file as a build-time constant (e.g. `DebtResolveMD`), following the exact pattern already used for `HostReviewMD`
- `skill/debt-resolve/SKILL.md` — create (referenced target of the pointer added here; full content is AC 03-03/03-04's responsibility)
- `skill/host-review.md` — reference (read-only): on-demand sibling-file pattern to mirror for the new `debt-resolve/SKILL.md` pointer
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/skill-dispatcher-conventions.md` — reference: dispatcher extension and routing-table-drift conventions
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: Skill frontmatter and progressive-disclosure requirements

## Happy Path Scenarios
**Scenario 1: `atcr debt` row documents the resolve subcommand without a new dispatcher row**
- **Given** `skill/SKILL.md`'s command table already lists `atcr debt` — "Query and report on technical debt" — at its existing row
- **When** the row (or an added subsection immediately below the table) is updated for this story
- **Then** the text names `atcr debt resolve` explicitly and states it autonomously resolves items from the local `.atcr/`-scoped TD store, without adding a second top-level `| atcr debt resolve | ... |` row to the table (per `skill/SKILL.md:84-87`'s routing-table-drift convention and the subcommand-discovery rule already stated at `skill/SKILL.md:57`)

**Scenario 2: on-demand pointer resolves to the new secondary file**
- **Given** a user or agent reads `skill/SKILL.md` end to end
- **When** it reaches the `atcr debt` documentation
- **Then** it finds a load-on-demand reference of the form `` `debt-resolve/SKILL.md` `` (backtick-wrapped sibling path, matching the existing `` `host-review.md` ``, `` `ambiguity-adjudication.md` ``, `` `findings-format.md` `` pointer style) instructing the agent to load it only when driving a resolve run

**Scenario 3: dispatcher test suite continues to pass unmodified in spirit**
- **Given** `skill/skill_test.go`'s `dispatcherCommands` list (line 133) already contains `"debt"`
- **When** `TestSkill_DispatcherRoutingTable` runs after this story's edit
- **Then** it still passes without a new list entry, because `debt resolve` is a subcommand extension, not a new top-level Cobra command registered in `newRootCmd`

## Edge Cases
**Edge Case 1: no invented subcommand names**
- **Given** the discovery snapshot and Story Context explicitly forbid inventing subcommand names beyond what is implemented
- **When** the SKILL.md text is written
- **Then** it names only `resolve` (not e.g. `fix`, `apply`, or other unimplemented verbs) and does not imply flags/subcommands that `cmd/atcr/debt.go`'s `newDebtCmd()` does not register

**Edge Case 2: SKILL.md stays within its documented ~500-line budget**
- **Given** `skill/SKILL.md`'s HTML comment convention states the file must stay within a ~500-line budget
- **When** the `atcr debt` row/subsection is extended
- **Then** the addition is a short pointer (a sentence or two plus the on-demand reference), not the full resolve-cycle documentation, which lives in `skill/debt-resolve/SKILL.md` instead

## Error Conditions
**Error Scenario 1: pointer file missing at embed time**
- Error message: Go build failure — `pattern debt-resolve/SKILL.md: no matching files found`
- HTTP status / error code: N/A (compile-time `go:embed` failure, not a runtime error)

## Performance Requirements
- **Response Time:** N/A — static documentation; no runtime execution path
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — documentation-only change
- **Input Validation:** The added text must not reference absolute filesystem paths or `.claude`-specific paths, per `TestSkill_NoAbsoluteOrClaudePaths`'s existing bar (extended to cover the new pointer text)

## Test Implementation Guidance
**Test Type:** UNIT (Go test over embedded string constants; no live CLI or agent invocation)
**Test Data Requirements:** None beyond the embedded `SkillMD`/`DebtResolveMD` constants themselves
**Mock/Stub Requirements:** None — `skill/skill_test.go` asserts directly against the embedded strings

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`, verifying the new `go:embed` directive resolves)

**Story-Specific:**
- [ ] `skill/SKILL.md`'s `atcr debt` row/subsection names `atcr debt resolve` and does not add a new top-level dispatcher table row
- [ ] `skill/SKILL.md` contains a backtick-wrapped on-demand pointer to `debt-resolve/SKILL.md`
- [ ] `skill/skill_test.go` gains an assertion covering both of the above and continues to pass `TestSkill_DispatcherRoutingTable` without a `dispatcherCommands` change

**Manual Review:**
- [ ] Code reviewed and approved
