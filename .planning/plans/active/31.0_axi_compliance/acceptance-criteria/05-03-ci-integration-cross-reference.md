# Acceptance Criteria: Additive Cross-Reference from `docs/ci-integration.md`

**Related User Story:** [05: Publish the Agentic Consumption Orchestration Guide](../user-stories/05-publish-agentic-consumption-guide.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Documentation (Markdown, single additive edit) | No runtime behavior change |
| Test Framework | N/A (documentation) — verified via manual link-check or a grep-based CI check confirming the link target exists | |
| Key Dependencies | None | Must land after `docs/agentic-consumption.md` exists (AC 05-01), so the link target is valid |

## Related Files
- `docs/ci-integration.md` - modify: add a single additive link/pointer to `docs/agentic-consumption.md`, without restructuring or duplicating the existing exit-semantics table (lines 11-19).
- `docs/agentic-consumption.md` - reference: the link target created by AC 05-01; this AC depends on that file existing first.
- `docs/github-action.md` - reference: existing precedent in `docs/ci-integration.md` for how a "see also"-style cross-reference to another doc file is already phrased (`See [github-action.md](github-action.md) for inputs...`), used as the style template for this AC's new link.

## Happy Path Scenarios
**Scenario 1: Link is added and discoverable**
- **Given** a reader is on `docs/ci-integration.md`
- **When** they scan the page (near the top, the exit-semantics section, or a dedicated "See also" pointer)
- **Then** they find a Markdown link to `docs/agentic-consumption.md` with descriptive anchor text (e.g., "for autonomous-agent/orchestrator invocation patterns, see agentic-consumption.md") that makes it clear the link is relevant to programmatic/agentic consumers specifically

**Scenario 2: Existing exit-semantics table is untouched**
- **Given** the same edit
- **When** a reviewer diffs `docs/ci-integration.md` before and after
- **Then** the exit-semantics table (lines 11-19) and its surrounding notes are byte-identical except for the reconciliation note added by AC 05-01's companion Story-2 documentation task (out of scope for this AC) — this AC's own diff contributes only the new link/pointer, not table changes

## Edge Cases
**Edge Case 1: Cross-reference added before the target file exists**
- **Given** this AC could theoretically be executed out of order relative to AC 05-01
- **When** the link is added
- **Then** the link is not considered complete/mergeable until `docs/agentic-consumption.md` actually exists at the linked path — a link to a nonexistent file is a broken-link defect

**Edge Case 2: Cross-reference placement does not disturb existing anchors**
- **Given** `docs/ci-integration.md` has existing internal structure (e.g., a `#maintained-pr-action` anchor referenced in prose)
- **When** the new link/section is inserted
- **Then** it is added as a new, self-contained addition (e.g., a short paragraph or list item) that does not renumber, reorder, or rename any existing heading, preserving all existing anchor links

## Error Conditions
**Error Scenario 1: Edit balloons into a restructuring**
- **Given** the constraint in the user story that this edit "must be minimal and additive... not a restructuring of that file"
- **When** a reviewer finds the diff reorders sections, rewrites unrelated prose, or duplicates the exit-semantics table
- **Then** this is flagged as scope creep in review and must be trimmed back to a single additive link/pointer before merge, per the Potential Risks table's "scope creep" risk
- Error message: N/A (documentation content defect, not a runtime error)
- HTTP status / error code: N/A

**Error Scenario 2: Broken link**
- **Given** the added link target path
- **When** a link-check (manual or automated) resolves `docs/agentic-consumption.md` relative to `docs/ci-integration.md`
- **Then** the link must resolve to an existing file; a 404/missing-file result is a blocking defect
- Error message: "link target does not exist: docs/agentic-consumption.md" (illustrative, for a grep/link-check tool if one is used)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.
- **Content-accuracy requirement (substitute for performance):** the link text must accurately describe what `docs/agentic-consumption.md` covers (agentic/orchestration invocation, not a general restatement of exit codes) so readers are not misled about the link's purpose.

## Security Considerations
- **Authentication/Authorization:** N/A — no runtime behavior change.
- **Input Validation:** N/A — not applicable to a documentation link edit.

## Test Implementation Guidance
**Test Type:** MANUAL / documentation review; optionally a lightweight grep or markdown-link-check CI step verifying `docs/agentic-consumption.md` exists relative to `docs/ci-integration.md`'s link, but not required for this AC to pass.
**Test Data Requirements:** N/A.
**Mock/Stub Requirements:** N/A.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/ci-integration.md` contains a working Markdown link to `docs/agentic-consumption.md`
- [ ] The link's anchor text clearly signals it targets agentic/orchestration invocation, distinct from the existing CI-gate content
- [ ] No existing section of `docs/ci-integration.md` (table, headings, anchors) was reordered, rewritten, or duplicated by this edit
- [ ] The link target file exists at merge time (i.e., this AC is sequenced after AC 05-01)

**Manual Review:**
- [ ] Code reviewed and approved
