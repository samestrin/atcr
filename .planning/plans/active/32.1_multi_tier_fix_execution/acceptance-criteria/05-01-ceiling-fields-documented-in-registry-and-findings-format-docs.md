# Acceptance Criteria: Ceiling Fields Documented in Registry and Findings-Format Docs

**Related User Story:** [05: Document the Multi-Tier Workflow](../user-stories/05-document-multi-tier-workflow.md)

## Acceptance Criteria
`docs/registry.md` documents the shipped ceiling field(s) — new rows in the existing executor field table's exact style plus skip-and-log / two-independent-runs prose — and `docs/findings-format.md` gains an `EST_MINUTES` cross-reference to the executor-ceiling routing consumer, with all field names, defaults, and validation wording verified against the actual shipped `internal/registry/config.go` and all relative links resolving.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown docs | `docs/registry.md` field table + prose, `docs/findings-format.md` column table + prose |
| Test Framework | Manual doc review + relative-link check | No executable test framework; verified by diff-checking new rows against existing rows and confirming no broken relative links |
| Key Dependencies | None (pure prose/table edits, no build tooling) | |

### Related Files (from codebase-discovery.json)
- `docs/registry.md` - modify: add `max_estimated_minutes` and `max_severity_for_fix` rows to the `## Executor (fix generation, active in 7.0)` field table (currently lines 341-392, immediately after `min_severity_for_fix`), plus a short prose paragraph on ceiling/skip-and-log behavior and the two-independent-runs multi-tier workflow.
- `docs/findings-format.md` - modify: add a cross-reference sentence to the `EST_MINUTES` row (the Columns table, currently line 62) and/or the surrounding prose (lines 23, 39) noting `EST_MINUTES` is now also an executor-ceiling routing input, linking to the `docs/registry.md` executor section.
- `.planning/plans/active/32.1_multi_tier_fix_execution/user-stories/01-configure-complexity-ceiling.md` - reference only: source of the exact field names (`max_estimated_minutes` int pointer, `max_severity_for_fix` string), defaults ("unset/zero = no ceiling"), and validation rules (range check, floor/ceiling contradiction) this AC's docs must match once Story 1 lands.

## Happy Path Scenarios
**Scenario 1: Registry table gains the new ceiling field rows in the existing style**
- **Given** `docs/registry.md`'s executor field table currently documents `min_severity_for_fix`, `fix_timeout`, `temperature`, and `max_tool_calls` using a fixed `| Key | Default | Notes |` row format
- **When** the table is updated for this story
- **Then** it gains a `max_estimated_minutes` row (default: unset/no ceiling; notes explain the value is compared against a finding's `EST_MINUTES` and an out-of-range value is a load error) and a `max_severity_for_fix` row (default: unset/no ceiling; notes explain the canonical severity enum and that a value below `min_severity_for_fix` is a load error), each written in the same sentence structure, capitalization, and validation-error phrasing as the adjacent `min_severity_for_fix` row

**Scenario 2: Prose explains the skip-and-log and two-tier mechanism**
- **Given** the executor section's existing prose paragraphs (e.g. the `agent_mode`/`max_tool_calls` paragraph) explain a feature's behavior and trade-offs in 3-5 sentences
- **When** the new ceiling prose is added immediately after the field table
- **Then** it explains, at the same density, that a ceiling-ed executor skips-and-logs (not attempts) a too-complex finding, and that a two-tier cheap-then-frontier workflow today means running a second, higher-ceiling (or unceilinged) executor config against the same `findings.json` — no new CLI flag or in-process chaining primitive is introduced

**Scenario 3: findings-format.md cross-references the new EST_MINUTES consumer**
- **Given** `docs/findings-format.md`'s Columns table documents `EST_MINUTES` as "Best-effort; non-numeric parses as `0`. Max wins on merge."
- **When** this story's edit lands
- **Then** the row (or adjacent prose) gains one added sentence noting `EST_MINUTES` is also consumed by executor ceiling routing (`max_estimated_minutes` in `docs/registry.md`), with a relative link to that section, while the existing merge/parsing sentence is preserved verbatim (not rewritten or duplicated)

## Edge Cases
**Edge Case 1: Story 1's field names or defaults change before this story lands**
- **Given** this story is sequenced after Story 1 (which finalizes `MaxEstimatedMinutes`/`MaxSeverityForFix` naming and defaults)
- **When** the documentation is written
- **Then** the field names, YAML keys, and default/validation wording in `docs/registry.md` match Story 1's actual shipped `ExecutorConfig` struct tags and `validateExecutor` error text exactly — not the plan's speculative naming — verified by a side-by-side diff against `internal/registry/config.go` at review time

**Edge Case 2: max_severity_for_fix is optional and may not exist in the shipped implementation**
- **Given** Story 1's Story Context marks `max_severity_for_fix` as "(optionally)" implemented
- **When** documenting the field
- **Then** the docs describe it only if Story 1 actually ships it; if Story 1 ships `max_estimated_minutes` alone, `docs/registry.md` documents only that field and does not reference a `max_severity_for_fix` key that does not exist in code

## Error Conditions
**Error Scenario 1: Documented validation wording drifts from the actual load-error message**
- Error message: "reviewer flags docs/registry.md's ceiling row as inconsistent with the real load-time error text emitted by `validateExecutor`"
- HTTP status / error code: N/A (documentation defect, not a runtime error; caught by manual review, not by a build failure)

**Error Scenario 2: Broken relative link in the findings-format.md cross-reference**
- Error message: "relative link to docs/registry.md executor section does not resolve (wrong anchor or path)"
- HTTP status / error code: N/A (caught by a markdown link check / manual click-through, not a runtime error)

## Performance Requirements
- **Response Time:** N/A — static documentation; no runtime execution path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public, non-secret documentation content; no credentials or tokens are referenced.
- **Input Validation:** The docs must not encourage an insecure default (e.g. must not suggest disabling `min_severity_for_fix` or setting an unbounded ceiling as a "quick fix"); wording must match the safe defaults already documented for sibling fields.

## Test Implementation Guidance
**Test Type:** UNIT (in the sense of a scoped, deterministic check) — manual doc review is the primary verification; no automated test suite covers prose. Where feasible, a lightweight markdown-link-checker pass over `docs/registry.md` and `docs/findings-format.md` catches broken relative links.
**Test Data Requirements:** The final, shipped `ExecutorConfig` field names/defaults/validation messages from Story 1 (and Story 2's `executor_ceiling_skip` warning class, for prose accuracy) — this AC cannot be finalized before those land.
**Mock/Stub Requirements:** None — no code under test, only markdown content.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/registry.md` executor table has `max_estimated_minutes` (and `max_severity_for_fix`, if shipped) rows matching the existing table's format, phrasing, and validation-error style
- [ ] `docs/registry.md` gains a prose paragraph explaining skip-and-log behavior and the two-independent-runs multi-tier mechanism
- [ ] `docs/findings-format.md`'s `EST_MINUTES` documentation gains a cross-reference to the executor-ceiling routing use, with a working relative link, and its existing merge/parsing sentence is unchanged
- [ ] New field names/defaults/validation wording match Story 1's actual shipped code, verified by diff against `internal/registry/config.go`

**Manual Review:**
- [ ] Code reviewed and approved
