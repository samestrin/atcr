# Task 07: Technical Debt Capture — Shard MEDIUM/LOW Findings into `.planning/technical-debt/README.md`

**Source:** Plan 33.0 – Debt Item #7
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
Task 3 (Findings Triage) classifies every finding from Task 1 (multi-agent review) and Task 2 (adversarial pass) by severity, fixes CRITICAL/HIGH directly in the codebase, and writes every MEDIUM/LOW finding to a handoff artifact — `.planning/plans/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` — in the 9-column `atcr-findings/v1` format plus a suggested `GROUP` label, explicitly deferring the write into the technical-debt store to this task. AC1 ("all CRITICAL/HIGH findings are fixed and MEDIUM/LOW are captured as technical debt") is not satisfied until those findings actually land in `.planning/technical-debt/README.md`, the project's designated integration point for this data (codebase-discovery.json `integration_points`: location `.planning/technical-debt/README.md`, type `debt-triage`). Until this write happens, Task 3's MEDIUM/LOW findings are an orphaned intermediate file with no visibility to the project's existing TD tracking, stats, or future `atcr debt resolve` workflow.

## Solution Overview
Read the Task 3 handoff artifact and shard its findings into `.planning/technical-debt/README.md` using the file's existing dated-section, pipe-delimited table format (`Group | Status | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence`), matching the column layout and conventions already used by the most recent `### [date] From Sprint: ...` sections in the file (e.g. the `2026-07-22 From Sprint: 32.4_workspace_integrity_sanitization` section). Append one new dated section for this plan's review (`### [2026-07-22] From Review: 33.0_final_documentation_sweep`), populate it with the MEDIUM/LOW rows carried over from Task 3 (all starting as open, `[ ]`), preserve any `(symbolName)` anchor prefix already present in a finding's `Problem` cell per `docs/technical-debt-format.md`'s format contract (do not add anchors that were not already stamped upstream), and update the file's top-of-file Stats table and summary counts to reflect the newly added open items. This is a capture-only task — no resolution, no `atcr debt resolve` work, no fixing of the newly captured items; that is explicitly out of scope and deferred to a future TD-resolution pass.

## Technical Implementation
### Steps
1. Read `.planning/plans/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` (Task 3's output) — the full list of MEDIUM/LOW findings in `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE` format plus a per-finding suggested `GROUP` label.
2. Map each finding's fields onto the TD README's table columns (`Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence`) exactly as used by existing dated sections in `.planning/technical-debt/README.md` — `Source` is a fixed literal identifying provenance (e.g. `code-review`), `Status` starts as `[ ]` (open) for every new row, and `Problem` cells retain any pre-existing `(symbolName)` anchor prefix verbatim per the format contract in `docs/technical-debt-format.md` (do not synthesize new anchors that were not already present in the Task 3 artifact).
3. Append a new dated section header to `.planning/technical-debt/README.md` in the existing convention — `### [2026-07-22] From Review: 33.0_final_documentation_sweep` — immediately followed by the mapped table, inserted in the same position (top of the dated-sections list, most-recent-first) as the file's existing sections. Use the project's `llm_support_group_td` / `llm_support_format_td_table` tooling to perform the deterministic table formatting and insertion if available in this environment; otherwise hand-format the section to be byte-for-byte consistent with the column order, header row, and delimiter style of the adjacent existing sections.
4. Update the Stats table near the top of `.planning/technical-debt/README.md` (the `| Severity | Open | Deferred | Resolved |` table) by adding each newly captured item's count to the `Open` column for its severity, and update the summary line (`**Last Modified:** ... | **Open Items:** ... | **Deferred Items:** ... | **Resolved Items:** ... | **Total Items:** ...`) to reflect the new totals and today's date.
5. Validate the updated file: run the project's existing TD validation tooling (`llm_support_td_validate`, or `go run ./cmd/td-migrate validate` if the sharded `items/` generation step is also exercised) to confirm every new row parses correctly under the existing table schema and does not corrupt any pre-existing section. If no automated validator is available in this environment, manually diff the new section's structure against an existing section and against `docs/technical-debt-format.md`'s contract to confirm compliance.
6. Confirm no MEDIUM/LOW finding from the Task 3 artifact was dropped or duplicated: the count of new rows in `.planning/technical-debt/README.md`'s new section must equal the count of findings in `code-review/triaged-findings-medium-low.md`.

## Files to Create/Modify
- `.planning/technical-debt/README.md` – append new dated `### [2026-07-22] From Review: 33.0_final_documentation_sweep` section containing every MEDIUM/LOW finding from Task 3, and update the Stats table / summary counts

## Documentation Links
- [Technical Debt Triage & Resolution](../documentation/technical-debt-triage-resolution.md)

## Related Files (from codebase-discovery.json)
- `.planning/technical-debt/README.md`
- `docs/technical-debt-format.md`

## Success Criteria
- [ ] Every MEDIUM/LOW finding recorded in `code-review/triaged-findings-medium-low.md` (Task 3 output) appears as a row in a new dated section of `.planning/technical-debt/README.md`, with no findings dropped or duplicated.
- [ ] The new section follows the exact existing table format (`Group | Status | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence`) used by adjacent dated sections, with every new row starting `[ ]` (open).
- [ ] Any pre-existing `(symbolName)` anchor prefix in a finding's Problem cell is preserved verbatim per `docs/technical-debt-format.md`; no anchors are fabricated.
- [ ] The Stats table and summary counts at the top of `.planning/technical-debt/README.md` are updated to include the newly captured open items.
- [ ] The updated README passes the project's TD validation tooling (or an equivalent manual format check against `docs/technical-debt-format.md`) with zero parse errors introduced.
- [ ] AC1's capture clause ("MEDIUM/LOW are captured as technical debt") is satisfied — every routed finding is now visible in `.planning/technical-debt/README.md`, not just in the intermediate Task 3 artifact.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
This task edits a Markdown tracking file, not application code — verification is format/data-integrity validation, not unit tests.

**Format Validation:**
- Run the project's existing TD validation tooling (`llm_support_td_validate`) against `.planning/technical-debt/README.md` after the edit to confirm the table parses cleanly under the existing schema.
- If the sharded `items/` generation is exercised in this environment, run `go run ./cmd/td-migrate validate` to confirm the new section round-trips through the shard schema without error.
- Manual checklist against `docs/technical-debt-format.md`: confirm column order and delimiter match adjacent sections, confirm `(symbolName)` anchors (where present) are untouched, confirm no `|` / control characters in new cells break the pipe-delimited table.

**Integration Tests:**
- Row-count reconciliation: count findings in `code-review/triaged-findings-medium-low.md` and count new rows added to `.planning/technical-debt/README.md`'s new section; the counts must match exactly.
- Stats-table reconciliation: confirm the updated Stats table's `Open` column deltas equal the number of newly added rows per severity, and the summary line's `Open Items` / `Total Items` counts increment by the same amount.

**Test Files:**
- None (no `*_test.go` files are affected by this task; validation is tooling-driven against the Markdown artifact itself).

## Risk Mitigation
- **Risk:** Manually formatting the new table section introduces a delimiter or column-order mismatch that breaks existing TD tooling parsing the flat table. **Mitigation:** Use the project's `llm_support_group_td` / `llm_support_format_td_table` tooling for deterministic insertion where available; otherwise byte-diff the new section's structure against an adjacent existing section before finalizing, and run the validation step (Step 5) before considering the task done.
- **Risk:** Findings dropped or duplicated during the transcription from Task 3's artifact to the README. **Mitigation:** Step 6's explicit row-count reconciliation between the two files is a hard gate before marking the task complete.
- **Risk:** Fabricating or stripping `(symbolName)` anchors during transcription, breaking the relocation contract documented in `docs/technical-debt-format.md`. **Mitigation:** Step 2 explicitly forbids synthesizing new anchors and requires preserving any pre-existing anchor verbatim; Step 5's manual checklist re-confirms this.
- **Risk:** Scope creep into resolving or fixing the newly captured MEDIUM/LOW items in this task. **Mitigation:** This task is capture-only per the plan's Task Theme #7 definition; resolution follows the separate `atcr debt resolve` / `/resolve-td` workflow (documentation/technical-debt-triage-resolution.md) as explicitly out-of-scope future work.

## Dependencies
- Task-03 (Findings triage) — provides classified MEDIUM/LOW findings input (`code-review/triaged-findings-medium-low.md`)

## Definition of Done
- Every MEDIUM/LOW finding from Task 3's handoff artifact is captured as a row in a new dated section of `.planning/technical-debt/README.md`.
- The new section matches the file's existing table format and conventions exactly.
- The Stats table and summary counts are updated to reflect the new open items.
- Row-count reconciliation between the Task 3 artifact and the new README section shows zero findings dropped or duplicated.
- The updated README passes TD validation tooling (or the manual format checklist) with zero new parse errors.
- AC1's MEDIUM/LOW capture clause is closed with the findings visible in the canonical TD store, ready for a future resolution pass.
