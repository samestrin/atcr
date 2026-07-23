# Task 04: Code-to-Docs Accuracy Audit

**Source:** Plan 33.0 – Debt Item #4
**Priority:** P1 | **Effort:** M | **Type:** Fix

## Problem Statement
`atcr` is about to go public (Epic 33.2), and `docs/` plus `README.md` will become the source of truth imported into the `atcr.dev` website. Documentation was last swept in Epic 22.0 (core engine) and Epic 23.0 (persona renaming), but Tasks 1-3 of this plan (code review, adversarial pass, findings triage) may have changed CLI flags, behavior, or file structure since then. Any drift between what the docs claim and what the finalized code actually does becomes a public-facing accuracy problem the moment the repo goes live — misleading users, breaking `atcr.dev` content, and undermining trust in a security review tool. AC4 requires that all features up to Epic 23.0 are fully and accurately documented in the core repository; this task closes that gap.

## Solution Overview
Audit `README.md`, all 29 files under `docs/`, the inline CLI help text in `cmd/atcr/root.go` (and any subcommand files it wires up), and `skill/SKILL.md` against the finalized, post-Task-3 codebase. For each file, cross-reference documented commands/flags/behavior against actual `--help` output and source, correct any drift found, and confirm no feature shipped through Epic 23.0 is undocumented. This is verification against reality, not new authoring — most files are expected to need only minor corrections (per `codebase-discovery.json > files_to_modify`, all `scope: minor` except the TD README). This task must run only after Task 3 (findings triage) has landed its CRITICAL/HIGH fixes, so the audit describes the actual shipped behavior rather than a moving target.

## Technical Implementation
### Steps
1. Confirm Task 3 (findings triage) is complete and its CRITICAL/HIGH fixes are merged into the working tree before starting — re-pull/re-check the branch state if unsure.
2. Enumerate the current CLI surface: run `go run ./cmd/atcr --help` and `go run ./cmd/atcr <subcommand> --help` for every subcommand registered in `cmd/atcr/root.go`, capturing actual flag names, defaults, and descriptions.
3. Audit `README.md` top to bottom against the finalized code: verify every documented command, flag, installation step, and example output matches step 2's captured `--help` output and current behavior; correct discrepancies in place.
4. Audit `docs/README.md` (the 29-file index) — confirm every linked file still exists, every listed feature/command maps to real code, and no file is missing from the index.
5. Walk the remaining `docs/*.md` files (28 files besides `docs/README.md`) and check each one's described commands, config schema fields, and behavior against the corresponding source (`cmd/`, `internal/`, `reconcile/`) — correct any file found to be stale. Prioritize files most likely to have drifted: persona/reviewer docs, CLI usage docs, and configuration/schema docs.
6. Audit `skill/SKILL.md` frontmatter and body: confirm the skill's described invocation, arguments, and behavior match the finalized `cmd/atcr` CLI surface from step 2.
7. Cross-check any documented config/output schemas (e.g., JSON output shapes, `.atcr` config file schema) against the actual Go structs/marshaling code that produces them; correct any field-name, type, or structure drift.
8. For every correction made, note the file and nature of the fix; where a discrepancy traces back to a Task 1-3 finding rather than doc staleness, cross-reference it so the docs commit's rationale is traceable.
9. Re-run `go run ./cmd/atcr --help` (and subcommands) one final time after all doc edits to confirm the corrected docs match current output exactly.

## Files to Create/Modify
- `README.md` – correct any stale commands, flags, examples, or feature descriptions against finalized code
- `docs/README.md` – verify the 29-file index is complete, all links resolve, and no feature is missing from the index
- `docs/personas-authoring.md` – verify persona naming conventions and authoring examples match finalized persona code
- `docs/personas-install.md` – verify installation instructions and default panel definitions match finalized behavior
- `skill/SKILL.md` – verify frontmatter and usage instructions match finalized CLI behavior
- `cmd/atcr/root.go` – correct inline help text strings/command descriptions found inaccurate (code fix, not doc-only; only if drift is in the help string itself rather than external docs)
- `docs/*.md` (remaining 27 files) – spot-audit and correct any file found to have drifted from finalized code

## Documentation Links
- [Persona Naming & Documentation Accuracy](../documentation/persona-naming-doc-accuracy.md)
- [Multi-Agent Review Workflow](../documentation/multi-agent-review-workflow.md)

## Related Files (from codebase-discovery.json)
- `README.md`
- `docs/README.md`
- `docs/personas-authoring.md`
- `docs/personas-install.md`
- `skill/SKILL.md`
- `cmd/atcr/root.go`

## Success Criteria
- [ ] Every command and flag documented in `README.md` matches actual `--help` output from the finalized CLI
- [ ] `docs/README.md` index links to all 29 docs files and no file/feature is missing from the index
- [ ] All persona-related docs (`docs/personas-authoring.md`, `docs/personas-install.md`) accurately describe the finalized `sasha`/`penny`/`ingrid` persona set and installation flow (the codebase resolved the `ingrid`/`ian` naming question in `personas/personas.go:20` — `ingrid` only; see Task 5's Naming-resolution note for the full citation)
- [ ] `skill/SKILL.md` accurately reflects the finalized CLI invocation and behavior
- [ ] No feature shipped through Epic 23.0 is undocumented in `README.md` or `docs/`
- [ ] Any documented config/output schema matches the actual Go structs/marshaling that produce it
- [ ] All corrections are file-scoped edits, not new speculative documentation content

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Verification Method (documentation audit, not code testing):**
- Run `go run ./cmd/atcr --help` and every subcommand's `--help` output; diff manually against the corresponding sections of `README.md`, `docs/*.md`, and `skill/SKILL.md`
- Manually execute a representative sample of documented example commands from `README.md` and `docs/` to confirm described behavior/output matches reality
- Cross-reference any documented JSON/config schema field-by-field against the Go struct tags in the producing source file
- Confirm `docs/README.md`'s file index resolves 1:1 against `ls docs/*.md` (29 files) with no orphaned or missing entries
- Re-run the full `--help` sweep after edits as a final consistency check (no remaining diffs)

**Integration Tests:**
- N/A — this task produces documentation-only changes (with a possible narrow exception for `cmd/atcr/root.go` help-string text); existing Go test suite (`go test ./...`) must still pass unchanged after any `root.go` string edits

**Test Files:**
- N/A — no test files are created or modified by this task; verification is manual cross-referencing per the Test Strategy above

## Risk Mitigation
- **Risk:** Auditing all 29 `docs/` files in depth could balloon scope past the plan's estimated effort. **Mitigation:** Prioritize files most likely to have drifted (CLI usage, persona, config/schema docs) per step 5; treat the remaining files as a lighter spot-check rather than a line-by-line rewrite.
- **Risk:** Running this audit before Task 3's fixes land would mean documenting soon-to-change code. **Mitigation:** Hard dependency on Task 3 completion (see Dependencies below); confirm the working tree includes Task 3's merged fixes before starting.
- **Risk:** Overlap with Task 5 (persona naming) and Task 6 (website formatting) could cause duplicate or conflicting edits to the same files. **Mitigation:** This task's edits are scoped strictly to factual/behavioral accuracy (commands, flags, schemas, feature coverage); persona-slug correctness is Task 5's concern and markdown formatting/website-import cleanliness is Task 6's concern — do not fix those issues here even if noticed, just flag them for the respective task.

## Dependencies
- Task-03 (Findings triage) — docs must describe finalized, post-fix code

## Definition of Done
- All commands, flags, and examples in `README.md`, `docs/`, and `skill/SKILL.md` verified against actual `--help` output and source behavior of the finalized codebase
- No feature shipped through Epic 23.0 is missing from documentation
- `docs/README.md` index verified complete and link-correct against all 29 files
- Any documented schema verified against its producing Go struct
- All discrepancies found have been corrected in place
- AC4 satisfied: all new features up to Epic 23.0 are fully and accurately documented in the core `atcr` repository
