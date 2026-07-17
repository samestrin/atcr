# Acceptance Criteria: Distinct Subcommand Registration Alongside Existing `atcr report`

**Related User Story:** [04: Maintainer-Facing Prompt Quality Report](../user-stories/04-maintainer-facing-prompt-quality-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go Cobra command registration | `cmd/atcr/main.go` `root.AddCommand(...)` list, `cmd/atcr/telemetry_report.go`'s `Use`/`Short` fields |
| Test Framework | Go `testing` (Cobra command-tree assertions) | Asserts command name uniqueness and non-interference with `atcr report` |
| Key Dependencies | `spf13/cobra` | No new dependency |

## Related Files
- `cmd/atcr/telemetry_report.go` - create: new Cobra command with a distinct `Use` name (e.g. `quality-report`) and `Short`/`Long` help text cross-referencing `atcr report` so the two are not confused
- `cmd/atcr/main.go` - modify: add the new command to `root.AddCommand(...)` (alongside `newReportCmd()` at line 217) without altering the existing `newReportCmd()` entry or its position
- `cmd/atcr/report.go` - reference only: MUST NOT be modified, aliased, or have its output changed by this story (explicit story constraint)
- `cmd/atcr/main_test.go` (or equivalent existing CLI command-tree test) - modify/extend: assert `atcr report` and the new subcommand both resolve to distinct, independently-invocable commands

### Related Files (from codebase-discovery.json)

- `cmd/atcr/telemetry_report.go` - create: distinct `Use` name plus cross-referencing `Short`/`Long` help text
- `cmd/atcr/main.go` - update: add the new command to `root.AddCommand(...)` alongside `newReportCmd()` (`:217`) without altering existing entries
- `cmd/atcr/main_test.go` (or equivalent CLI command-tree test) - update: distinct-command resolution assertions

## Happy Path Scenarios
**Scenario 1: New subcommand is independently invocable and does not alter `atcr report`**
- **Given** the root Cobra command tree after this story's changes
- **When** `atcr report` is invoked against an existing `findings.json`
- **Then** it produces byte-for-byte the same output as before this story (no behavior change to the existing command)

**Scenario 2: New subcommand has a distinct, self-explanatory name**
- **Given** `atcr --help`
- **When** the help output is inspected
- **Then** the new subcommand appears under a name distinct from `report` (e.g. `quality-report`) with `Short` help text that clearly differentiates it from `atcr report` (e.g. "Render the aggregate persona+model dismissed/confirmed quality signal" vs. "Render md, json, checklist, or sarif views over reconciled findings")

## Edge Cases
**Edge Case 1: Command name collision check**
- **Given** the full list of registered subcommands in `cmd/atcr/main.go`'s `root.AddCommand(...)` call
- **When** the new command is added
- **Then** its `Use` name does not collide with any existing subcommand name (`report`, `review`, `reconcile`, `verify`, `debate`, `github`, `range`, `status`, `init`, `quickstart`, `serve`, `doctor`, etc.)

**Edge Case 2: Cross-reference in help text**
- **Given** a maintainer runs `atcr report --help` or the new command's `--help`
- **When** the help text is read
- **Then** each command's `Long`/`Short` text mentions the other's existence and scope, so a maintainer unfamiliar with the distinction is pointed to the right command (mitigates the story's documented "name collides with or is confused for `atcr report`" risk)

## Error Conditions
**Error Scenario 1: Accidental reuse of `runReport`/`newReportCmd`**
- **Given** a hypothetical implementation mistake that calls `newReportCmd()` or `runReport` internally instead of building an independent command
- **When** the command-tree test runs
- **Then** it fails, asserting the new command is a structurally separate `*cobra.Command` with its own `RunE`, not a wrapper or alias around the existing report command

## Performance Requirements
- **Response Time:** Not applicable — command registration is a one-time startup cost, no measurable runtime impact.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no new access surface; command registration only.
- **Input Validation:** Not applicable to this AC (covered by AC 04-01's `--format` validation).

## Test Implementation Guidance
**Test Type:** UNIT (Cobra command-tree structural assertions)
**Test Data Requirements:** None beyond the constructed root command tree.
**Mock/Stub Requirements:** None — pure structural inspection of the Cobra command tree (`root.Commands()`), no I/O.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors (`go vet`, project linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr report` output and behavior are byte-for-byte unchanged after this story
- [ ] The new subcommand has a distinct `Use` name with no collision against any existing registered subcommand
- [ ] `--help` text on both commands cross-references the other to prevent maintainer confusion
- [ ] The new command is a structurally independent `*cobra.Command` (own `RunE`), not a wrapper/alias of `newReportCmd`/`runReport`

**Manual Review:**
- [ ] Code reviewed and approved
