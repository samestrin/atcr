# Acceptance Criteria: Default Line Cap with Deterministic Truncation

**Related User Story:** [Story 3: AXI Pagination and Truncation Guarantees](../user-stories/03-axi-pagination-and-truncation-guarantees.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/report`) — line-cap post-processing step | Wraps AXI-mode rendered output; consumed by both `atcr review --axi` and `atcr report --axi` |
| Test Framework | `go test` (standard library `testing`) | Table-driven tests over synthetic multi-line payloads |
| Key Dependencies | `bufio`/`strings` (line splitting), no third-party dependency added | Consistent with plan.md's "hand-rolled formatters over third-party dependencies" stance |

## Related Files
- `internal/report/pagination.go` - create: implements the line-cap wrapping writer/post-processing step (default 500-line cap, deterministic cut point at row index = cap)
- `internal/report/render.go` - modify: `Render` dispatch wires the pagination step into the `FormatAXI` output path (Story 1's renderer)
- `cmd/atcr/review.go` - modify: live AXI output path (`atcr review --axi`) applies the same pagination step as the `atcr report --axi` path
- `docs/payload-modes.md` - reference: existing deterministic-truncation contract (byte-budget, largest-first drops) whose "never silent, always deterministic" philosophy this AC extends to a line-based cap

## Happy Path Scenarios
**Scenario 1: Payload under the cap passes through unchanged**
- **Given** an AXI-mode findings payload that renders to 120 lines and the default cap is 500
- **When** `atcr report --axi` renders the payload
- **Then** all 120 lines are emitted unmodified, no lines are dropped, and the output is byte-identical to the unwrapped renderer output

**Scenario 2: Payload over the cap is truncated at a fixed row boundary**
- **Given** an AXI-mode findings payload that renders to 1,200 lines and the default cap is 500
- **When** `atcr report --axi` renders the payload
- **Then** exactly 500 lines of payload content are emitted (plus any required closing structure), and the cut point falls on a complete row boundary — no row is emitted partially split across the cap

**Scenario 3: Truncation is reproducible across repeated runs**
- **Given** the same 1,200-line synthetic payload rendered twice in separate process invocations
- **When** both invocations run with `atcr review --axi` against identical input
- **Then** the two truncated outputs are byte-identical (same rows dropped, same cut point, same ordering)

## Edge Cases
**Edge Case 1: Payload exactly at the cap**
- **Given** a payload that renders to exactly 500 lines and the cap is 500
- **When** the payload is rendered
- **Then** no truncation occurs (the boundary is inclusive — a payload equal to the cap is not truncated)

**Edge Case 2: Payload one line over the cap**
- **Given** a payload that renders to 501 lines and the cap is 500
- **When** the payload is rendered
- **Then** truncation triggers and exactly 500 lines of content are emitted

**Edge Case 3: Empty payload**
- **Given** a zero-finding AXI payload (empty TOON array, e.g. `findings[0]{...}:`)
- **When** the payload is rendered
- **Then** the cap logic is a no-op — the well-formed empty payload passes through unmodified

**Edge Case 4: Truncation cut point respects row structure, not raw text lines**
- **Given** a TOON tabular array whose individual rows never span multiple physical lines (per Story 1's renderer contract)
- **When** the cap is applied
- **Then** the cut point aligns exactly with a row boundary so no row's fields are split mid-row in the emitted output

## Error Conditions
**Error Scenario 1: Truncation step itself must never fail the run**
- **Given** any valid AXI-mode payload, regardless of size
- **When** the line-cap step processes it
- **Then** no error is returned and no non-zero exit code results — the cap is a bounded, unconditionally-succeeding transformation, not a validation gate

## Performance Requirements
- **Response Time:** Line-counting and truncation add negligible overhead — O(n) single pass over the rendered payload, no re-rendering or backtracking required for payloads up to tens of thousands of lines.
- **Throughput:** Must not materially change the wall-clock time of `atcr review --axi`/`atcr report --axi` for typical PR-sized payloads (hundreds to low thousands of findings/diff lines).

## Security Considerations
- **Authentication/Authorization:** N/A — local rendering step with no external calls.
- **Input Validation:** The line-cap step operates on already-rendered, already-sanitized AXI output (Story 1's renderer has already stripped ANSI/control characters); it must not re-introduce unsanitized content and must treat the rendered payload as opaque text for counting purposes.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Synthetic AXI-mode payloads of varying sizes (0 lines, 120 lines, exactly 500 lines, 501 lines, 1,200 lines) constructed directly as TOON/rendered text fixtures — no need to drive a full review run.
**Mock/Stub Requirements:** None required for the pure truncation function; an `io.Writer`-based test harness (`bytes.Buffer`) is sufficient to capture and assert on output.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Payloads under the cap pass through byte-identical to unwrapped renderer output
- [ ] Payloads over the cap are truncated at exactly the cap line count, at a row boundary
- [ ] Truncation is deterministic and reproducible across repeated runs on identical input
- [ ] The cap applies uniformly via a single shared step consumed by both `atcr review --axi` and `atcr report --axi`

**Manual Review:**
- [ ] Code reviewed and approved
