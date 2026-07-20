# User Story 5: Document the Multi-Tier Workflow

**Plan:** [32.1: Multi-Tier Fix Execution Engine](../plan.md)

## User Story

**As an** atcr operator on the BYO-Keys architecture
**I want** `docs/registry.md`, `docs/findings-format.md`, and a worked two-tier example config to explain and demonstrate the new complexity-ceiling fields
**So that** I can discover and correctly configure a cheap-tier-then-frontier-tier fix run without reverse-engineering the executor's routing logic from source

## Story Context

- **Background:** `docs/registry.md`'s "Executor (fix generation, active in 7.0)" section (lines 341-392) documents the full `executor:` config surface as a single field table and a single worked YAML block, written for exactly one executor per run. Stories 1-3 add `max_estimated_minutes` (and optionally `max_severity_for_fix`) to `ExecutorConfig`, add ceiling-skip routing to `generateFixes`, and establish the mechanism for running a second, more capable tier over ceiling-skipped findings. None of that is discoverable to an operator unless this documentation is updated to match. `docs/findings-format.md` already documents `EST_MINUTES` semantics (best-effort integer, non-numeric parses as `0`, max wins on merge) at lines 23, 39, and 62 — it needs a cross-reference noting that this field is now also a routing input for executor ceilings, not just a display/reconciliation value. `examples/registry-with-executor.yaml` currently shows only a single-executor block; it needs a worked two-tier example added so an operator has a copy-pasteable starting point.
- **Assumptions:**
  - Story 3 (Run a second tier over skipped findings) has resolved and locked the actual multi-tier mechanism — most likely two independently-configured registry files run sequentially against the same `findings.json` (a cheap-tier pass, then a frontier-tier pass reading the first pass's skip-tagged findings), not an in-process ordered executor chain. This story's worked example must reflect whichever mechanism Story 3 actually implements; if Story 3 changes the mechanism after this story is drafted, the example must be updated to match before this story is considered done.
  - Stories 1 and 2 have landed `ExecutorConfig.MaxEstimatedMinutes` / `MaxSeverityForFix`, their `EffectiveXxx()` resolvers, and the `executor_ceiling_skip` skip-and-log contract — this story documents behavior those stories implement, it does not implement or validate the behavior itself.
  - No new documentation files are warranted — the original epic's T5 is satisfied by targeted edits to the three existing files named above, not a new guide or tutorial page.
- **Constraints:**
  - Edits are confined to `docs/registry.md`, `docs/findings-format.md`, and `examples/registry-with-executor.yaml` — no new file, no restructuring of unrelated sections.
  - New field documentation in `docs/registry.md` must follow the existing table conventions exactly (Field / Default / Notes columns, same validation-error phrasing style used for `min_severity_for_fix`, `fix_timeout`, `max_tool_calls`), so the new rows read as part of the same table, not a bolted-on addendum.
  - The worked example must be runnable YAML (same schema and comment style as the existing example), not pseudocode.
  - No source code, config schema, or routing logic changes in this story — pure documentation and example content.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (Configure a complexity ceiling) — provides the field names, defaults, and validation rules to document; User Story 3 (Run a second tier over skipped findings) — provides the actual multi-tier mechanism the worked example must demonstrate |

## Success Criteria (SMART Format)

- **Specific:** `docs/registry.md`'s executor section gains table rows and prose for `max_estimated_minutes` and (if implemented) `max_severity_for_fix`, matching the existing field-table format exactly; `docs/findings-format.md` gains a cross-reference from its `EST_MINUTES` column description to the new executor-routing consumer; `examples/registry-with-executor.yaml` gains (or is extended with) a worked two-tier example showing a cheap-tier executor config and a frontier-tier executor config run sequentially against the same `findings.json`.
- **Measurable:** All three files build/render without broken internal links or malformed YAML; the worked example is valid YAML parseable by atcr's existing registry loader (verified by running the example through `atcr`'s config validation, e.g. a dry-run load, with zero load errors); a reviewer can follow the docs alone (no source reading) to write a working two-tier registry pair.
- **Achievable:** Confined to three files already identified in the plan's Technical Planning Notes; no new subsystem or external content to research.
- **Relevant:** Directly closes the original epic's AC4 (a documented multi-tier workflow example) and T5 (update docs/registry.md and user-facing docs) — without this story, Stories 1-3's routing capability is unusable by anyone who has not read the Go source.
- **Time-bound:** A single-session documentation task, sequenced last in the plan since it depends on Stories 1 and 3's final field names and mechanism being locked.

## Acceptance Criteria Overview

1. `docs/registry.md`'s executor field table and surrounding prose document the new ceiling field(s) (name, default, validation range, effect on routing) in the same style as existing fields.
2. `docs/findings-format.md`'s `EST_MINUTES` documentation cross-references the executor-ceiling routing use introduced by Story 1/2, without altering the existing merge/parsing semantics already documented there.
3. `examples/registry-with-executor.yaml` (or a companion example file, if a single file cannot cleanly show two independent registries) contains a complete, valid, runnable worked example of a cheap-tier config and a frontier-tier config applied sequentially against the same `findings.json`, matching the mechanism Story 3 actually implements.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/32.1_multi_tier_fix_execution/`_

## Technical Considerations

- **Implementation Notes:**
  - Add the new field rows to the existing table at `docs/registry.md:341-392` immediately after `min_severity_for_fix` (its natural sibling as the complementary ceiling to that floor), preserving the table's existing column order and phrasing conventions (e.g. "Must be within `[...]`; an out-of-range value is a load error.").
  - Add a short prose paragraph (matching the density of the existing `agent_mode`/`max_tool_calls` paragraphs) explaining that a ceiling-ed executor skips-and-logs rather than attempts a too-complex finding, and that running a second, higher-ceiling (or unceilinged) executor over the same `findings.json` is how a two-tier workflow is assembled today — no new CLI flag or orchestration primitive is introduced.
  - In `docs/findings-format.md`, add one sentence to the `EST_MINUTES` row (or the surrounding prose near lines 23/39/62) noting it is now also consumed by executor ceiling routing, linking to the `docs/registry.md` section for details — do not duplicate the merge-semantics explanation, just cross-reference it.
  - Extend `examples/registry-with-executor.yaml`'s existing single-executor example with a second commented block (or add a clearly-named second file, e.g. `examples/registry-with-executor-tier2.yaml`, if the plan's chosen mechanism is "two independent registry files") showing realistic cheap-tier values (e.g. a local/cheap model, a low `max_estimated_minutes`) alongside frontier-tier values (higher-capability model, no ceiling or a much higher one).
- **Integration Points:**
  - `docs/registry.md` — executor field table and prose, no other section touched.
  - `docs/findings-format.md` — `EST_MINUTES` column description only.
  - `examples/registry-with-executor.yaml` — extended or paired with a new sibling example file.
- **Data Requirements:** None — this story reads Story 1's finalized field names/defaults and Story 3's finalized mechanism as inputs; it produces no schema or data changes itself.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Story 3's actual multi-tier mechanism is decided or changed after this story is drafted, leaving the worked example demonstrating a mechanism the code doesn't support | High | Sequence this story last (already reflected in its dependency list); treat Story 3's final implementation, not this plan's speculative "most likely" framing, as the source of truth for the example before marking this story done |
| New field-table rows drift from the exact style of existing rows (wording, capitalization, validation-error phrasing), making the table read as inconsistently authored | Low | Copy the phrasing pattern of the adjacent `min_severity_for_fix`/`fix_timeout` rows verbatim as a template; a reviewer diff-checks the new rows against existing ones |
| The worked example YAML is written by hand and silently diverges from the real schema (e.g. a typo'd field name), so a copy-pasting operator hits a load error | Medium | Validate the finished example by loading it through atcr's actual registry loader (dry-run) as part of this story's acceptance criteria, not just visual review |

---

**Created:** July 20, 2026
**Status:** Draft - Awaiting Acceptance Criteria
