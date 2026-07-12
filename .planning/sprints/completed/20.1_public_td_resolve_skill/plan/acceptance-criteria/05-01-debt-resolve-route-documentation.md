# Acceptance Criteria: `/atcr debt resolve` Route Documentation

**Related User Story:** [05: Document Debt-Resolve in skill-usage.md](../user-stories/05-document-debt-resolve-in-skill-usage.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/skill-usage.md`) | Additive edit to an existing public-facing guide; no code changes |
| Test Framework | Go `testing` (doc-presence/content assertions), mirroring `internal/scorecard/docs_test.go` | Structural checks only — cannot verify prose quality, only presence of required facts/sections |
| Key Dependencies | Story 3's landed `skill/debt-resolve/SKILL.md` and `atcr debt resolve` CLI subcommand (source of truth for accuracy) | Content must be verified against landed behavior before this AC is considered done, per the story's stated risk |

### Related Files (from codebase-discovery.json)
- `docs/skill-usage.md` — modify: add a new section (e.g. `## Technical Debt Resolution`) documenting `/atcr debt resolve`'s purpose, invocation syntax, and step-by-step behavior, placed after the existing `## Output` section and following the doc's existing Usage/Output prose-plus-table style
- `skill/debt-resolve/SKILL.md` — reference (read-only): the on-demand skill file this section describes; source of truth for the invocation syntax and behavior steps once Story 3 lands
- `skill/SKILL.md` — reference (read-only): the `atcr debt` command-table row (line ~79) this section must stay consistent with — same command surface, no invented subcommands
- `docs/scorecard.md` — reference (read-only): structural/tone precedent for documenting a local store's format, CLI usage, and privacy model
- `docs/skill-usage.md`'s opening summary paragraph — modify (conditional): update the skill's scope statement ("resolve range → fan out → host review → reconcile → report") if the debt-resolve capability should be mentioned there too
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/skill-dispatcher-conventions.md` — reference: public documentation requirements for the new `/atcr debt resolve` capability

## Happy Path Scenarios
**Scenario 1: Reader learns what `/atcr debt resolve` does and how to invoke it**
- **Given** a standalone/public atcr user has installed the skill per the existing Installation section
- **When** they read the new Technical Debt Resolution section of `docs/skill-usage.md`
- **Then** they can state, without reading source code, what `/atcr debt resolve` does (reads the local backlog, selects items, runs a RED→GREEN→ADVERSARIAL→REFACTOR cycle, updates resolution status), how to invoke it, and what it produces (fixed code, updated store records, a summary of what was resolved)

**Scenario 2: Section follows the doc's existing structural conventions**
- **Given** the existing `## Usage` and `## Output` sections use short intro prose plus a table/step-list
- **When** the new section is added
- **Then** it uses the same prose-plus-table/step-list shape (not a divergent format), placed after `## Output`, and the document's table of contents / heading hierarchy remains consistent (all new headings are `##`/`###`, matching existing depth)

## Edge Cases
**Edge Case 1: Selection rule and outcome behavior are described without overclaiming unimplemented detail**
- **Given** Story 3 defines a deterministic, documented selection rule (e.g. severity/age) for which backlog items get resolved in a run
- **When** the section describes selection behavior
- **Then** it states the actual selection rule as landed (not a placeholder or an aspirational description), and if the exact rule is still in flux at draft time, the section is flagged for the final verification pass rather than asserting unverified behavior as fact

**Edge Case 2: No local store exists yet**
- **Given** a first-time user who has run `/atcr debt resolve` before ever running `atcr reconcile`
- **When** they read the section
- **Then** it explains what happens when the local store is empty or absent (e.g. "nothing to resolve" outcome), so the reader is not left guessing about a zero-backlog case

## Error Conditions
**Error Scenario 1: Section omitted or misplaced fails doc-structure verification**
- Error message (test failure): "docs/skill-usage.md missing required section heading for /atcr debt resolve documentation"
- HTTP status / error code: N/A (Go test failure, non-zero exit from `go test ./internal/... -run TestDocs`)

**Error Scenario 2: Documented invocation syntax drifts from the landed CLI/skill behavior**
- Error message (manual review finding): "docs/skill-usage.md describes `/atcr debt resolve` behavior that does not match skill/debt-resolve/SKILL.md or the atcr debt resolve CLI subcommand"
- HTTP status / error code: N/A (documentation-accuracy defect, caught in the story's final verification pass, not an automated gate)

## Performance Requirements
- **Response Time:** N/A (static documentation; no runtime behavior)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A (public documentation, no secrets or credentials referenced)
- **Input Validation:** The section must not include any example output containing real repo paths, tokens, or credentials — synthetic/example values only, consistent with the rest of `docs/skill-usage.md` and `docs/scorecard.md`'s existing example conventions

## Test Implementation Guidance
**Test Type:** UNIT (doc-presence/content structural assertions, mirroring `internal/scorecard/docs_test.go`)
**Test Data Requirements:** None beyond the repository's own `docs/skill-usage.md` file at test time; no fixtures needed
**Mock/Stub Requirements:** None — the test reads the real file from disk (repo-root-relative, same `repoRoot(t)` walk-up pattern as `internal/scorecard/docs_test.go`)

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `docs/skill-usage.md` contains a new section documenting `/atcr debt resolve`'s purpose, invocation, and behavior
- [x] The section's structure (prose + table/step-list) matches the existing Usage/Output section style
- [x] Selection-rule and empty-store edge-case behavior are described accurately
- [x] Content verified against Story 3's landed `skill/debt-resolve/SKILL.md` and CLI subcommand behavior (not just the story draft) before sign-off

**Manual Review:**
- [ ] Code reviewed and approved
