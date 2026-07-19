# Acceptance Criteria: Worked Orchestration Example (Autonomous Sweeper Scenario)

**Related User Story:** [05: Publish the Agentic Consumption Orchestration Guide](../user-stories/05-publish-agentic-consumption-guide.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Documentation (Markdown, fenced shell/pseudocode example) | No runtime behavior change |
| Test Framework | N/A (documentation) — verified by manually running (or dry-running) the example against a real `atcr` build | |
| Key Dependencies | None | Example must reflect Story 1's actual `--axi` payload shape and Story 2's actual exit codes |

### Related Files (from codebase-discovery.json)
- `docs/agentic-consumption.md` - create/modify: add a worked example section showing an autonomous sweeper invoking `atcr review --axi` as a subprocess, parsing the payload, and branching on exit code.
- `docs/ci-integration.md` - reference: existing one-shot pattern (`atcr review --fail-on high`, the `ci-gate.sh` wrapper reference) used as the structural precedent for a runnable example.
- `docs/findings-format.md` - reference: existing pipe-delimited `atcr-findings/v1` per-source stream example format, used as a style precedent for showing a parsed-payload snippet.
- `examples/ci-gate.sh` - reference: existing example script referenced by `docs/ci-integration.md`, the closest existing precedent for a "ready-to-adapt" runnable snippet.

## Happy Path Scenarios
**Scenario 1: Example shows subprocess invocation**
- **Given** a reader reaches the worked example section
- **When** they read it
- **Then** they see a concrete snippet (shell and/or pseudocode) that invokes `atcr review --axi` as a subprocess from an orchestrator/sweeper process, capturing stdout and stderr separately

**Scenario 2: Example shows payload parsing**
- **Given** the same worked example
- **When** the reader follows the snippet past the subprocess call
- **Then** they see how the captured stdout payload (TOON/JSON, per Story 1's shipped format) is parsed into a structure the orchestrator can inspect (e.g., iterating findings, checking a `truncated` field)

**Scenario 3: Example shows exit-code branching**
- **Given** the same worked example
- **When** the reader follows it to completion
- **Then** they see explicit branching logic on the process exit code — distinguishing 0 (clean, proceed), 1 (gate failure, findings present), 2 (usage error, fix invocation), and 3 (auth error, fix credentials) — matching the contract documented in AC 05-01 and `docs/ci-integration.md`

## Edge Cases
**Edge Case 1: Example must handle `truncated: true`**
- **Given** the sweeper scenario is meant to generalize to large diffs
- **When** the worked example parses the payload
- **Then** it shows (at minimum in prose, ideally in the snippet) how the orchestrator should react when `truncated` is `true` — e.g., re-invoking with a higher `ATCR_AXI_MAX_LINES` or treating the result as partial — rather than silently assuming the payload is complete

**Edge Case 2: Example does not invent behavior beyond Stories 1-4**
- **Given** the example is illustrative
- **When** it is written
- **Then** it does not depict flags, fields, or exit codes that Stories 1-4 did not actually implement (e.g., no invented `--axi-format` sub-flag, no invented exit code 4) — every element in the snippet must be traceable to shipped behavior

**Edge Case 3: Example is near-runnable, not purely abstract**
- **Given** the Success Criteria in the user story require the example be "runnable (or near-runnable)"
- **When** a reader copies the shell portion
- **Then** it uses real command syntax (`atcr review --axi`, real env var names, real exit-code test patterns like `case $? in ... esac` or `if [ $? -eq 1 ]`) rather than fully abstract pseudocode with no connection to an actual shell invocation

## Error Conditions
**Error Scenario 1: Example depicts a rejected/incorrect exit-code scheme**
- **Given** a reviewer reads the exit-code branching in the worked example
- **When** it uses the epic's original rejected 0/1/2="internal error" scheme instead of the reconciled 0/1/2/3 contract
- **Then** this is a documentation defect that must block merge, per the same risk called out in the user story's Potential Risks table
- Error message: N/A (documentation content defect, not a runtime error)
- HTTP status / error code: N/A

**Error Scenario 2: Example omits stderr handling**
- **Given** the sweeper scenario's entire value proposition depends on stderr/stdout isolation (Story 4)
- **When** the worked example captures only stdout and never mentions stderr
- **Then** this is a documentation gap — the example must at least show that diagnostics/progress logs land on stderr and are not mixed into the parsed payload
- Error message: N/A
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.
- **Content-accuracy requirement (substitute for performance):** the example's flag names, field names, and exit codes must match Stories 1-4's shipped implementation, spot-checked by actually running the example command against a built `atcr` binary where feasible.

## Security Considerations
- **Authentication/Authorization:** N/A — the example may reference the auth-error exit code (3) but must not include real credentials, tokens, or API keys in any snippet.
- **Input Validation:** N/A — example is illustrative; it must not imply the orchestrator can skip validating the parsed payload (e.g., must not suggest blindly trusting an unparsed/malformed payload).

## Test Implementation Guidance
**Test Type:** MANUAL / documentation review; where feasible, the shell portion of the example should be manually executed against a built `atcr` binary to confirm it produces the described output shape, as a lightweight smoke check rather than an automated test suite.
**Test Data Requirements:** A small local git diff/PR range sufficient to trigger both a clean (exit 0) and a findings (exit 1) result, used only for manual verification, not committed as fixture data.
**Mock/Stub Requirements:** N/A.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Worked example shows a subprocess invocation of `atcr review --axi` (and/or `atcr report --axi`)
- [x] Worked example shows payload parsing of the captured stdout, including checking the `truncated` field
- [x] Worked example shows explicit branching on exit codes 0/1/2/3 matching the reconciled contract
- [x] Worked example does not depict any flag, field, or exit code not actually shipped by Stories 1-4

**Manual Review:**
- [x] Code reviewed and approved
