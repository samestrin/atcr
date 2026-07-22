# Task 06: Website Compatibility Check — Validate `docs/` for Clean, Self-Contained `atcr.dev` Import

**Source:** Plan 33.0 – Debt Item #6
**Priority:** P2 | **Effort:** S | **Type:** Fix

## Problem Statement
Going public (Epic 33.2) means `docs/` becomes the source of truth imported directly into the separate `atcr.dev` website repository. `docs/README.md` already declares itself "the single source of truth the website build consumes," but nothing has verified that the 29 files under `docs/` are actually clean and self-contained enough to survive that import: broken or non-relative links, malformed Markdown, or repo-root-relative assumptions that resolve correctly inside `atcr` but break once the directory is copied into a different repository would silently corrupt the website. AC5 requires that documentation files are validated and ready to be imported into `atcr.dev`; this task closes that gap. It is a formatting/portability/link-integrity check only — content accuracy (Task 4) and persona-name correctness (Task 5) are explicitly out of scope here even if noticed in passing.

## Solution Overview
Walk all 29 files under `docs/` (indexed by `docs/README.md`) after Tasks 4 and 5 have landed their content fixes, and validate: (1) every link in `docs/README.md` resolves to an existing file in `docs/` and every file in `docs/` is reachable from the index, (2) every internal link within any `docs/*.md` file is relative (no `/absolute/repo/paths` or `../../` paths that reach outside `docs/`, other than intentional, clearly-external repo-root references like `README.md` or `skill/SKILL.md` that the website build is expected to handle separately), (3) Markdown formatting is clean GitHub-Flavored-Markdown (consistent heading hierarchy, no broken code fences, no malformed tables/lists), and (4) each file is self-contained — it does not silently depend on repo context (e.g., relative image paths, includes) that would not travel with a `docs/`-only copy. Fix any confirmed issue directly in the affected file; this is a validation-and-cleanup pass, not a rewrite.

## Technical Implementation
### Steps
1. Confirm Task 4 (code-to-docs audit) and Task 5 (persona reference verification) are complete and merged into the working tree before starting, so this pass validates the finalized state of `docs/` rather than a moving target.
2. Enumerate the current file set: run `ls docs/*.md` and cross-check the count and names against every entry linked from `docs/README.md`; flag any file present on disk but missing from the index, or any index link pointing at a file that does not exist.
3. Extract every Markdown link (`[text](path)`) from each of the 29 files (e.g., via `mcp__llm-support-mcp__llm_support_extract_links` or a targeted grep for `](`) and classify each: (a) relative link to another file inside `docs/` — verify the target exists via a relative path resolution from the source file's own location; (b) link to a file outside `docs/` (e.g., `../README.md`, `../skill/SKILL.md`) — confirm these are intentional cross-repo references, not something that should have stayed inside `docs/`; (c) external `https://` links — spot-check a sample for obvious dead-link patterns (not a full crawl).
4. For every relative link found broken (points at a renamed/moved/deleted file), fix the link in place to point at the correct target.
5. Grep `docs/*.md` for repo-root-relative assumptions that would not survive a copy into `atcr.dev` — patterns like a leading `/` in a link path, or prose that assumes the reader is inside the full `atcr` repo checkout (e.g., "see the file at the repo root") — and correct or reword any found.
6. Spot-check Markdown formatting cleanliness across all 29 files: consistent `#`/`##`/`###` heading hierarchy (no skipped levels), properly closed code fences (paired ``` ` ``` ` blocks), and well-formed tables/lists (per the two known `http://localhost` URLs already confirmed as legitimate config examples in `docs/personas-install.md:331` and `docs/providers.md:21` — do not flag these as broken links).
7. Confirm `docs/README.md`'s categorized index (Overview & configuration, Pipeline stages, Personas, Integration, Benchmarking & observability) still groups every file sensibly after Tasks 4/5's edits — no orphaned category, no file listed twice, no file missing a one-line description.
8. Re-run the full link-resolution sweep (step 3) one final time after all fixes to confirm zero remaining broken relative links across all 29 files.

## Files to Create/Modify
- `docs/README.md` – fix any broken/missing index links; confirm all 29 files are indexed and categorized correctly
- `docs/*.md` (remaining 28 files) – fix any broken relative links, repo-root-relative assumptions, or Markdown formatting defects found during the sweep

## Documentation Links
- [Persona Naming & Documentation Accuracy](../documentation/persona-naming-doc-accuracy.md)

## Related Files (from codebase-discovery.json)
- `docs/README.md`
- `docs/personas-authoring.md`
- `docs/personas-install.md`
- `docs/payload-modes.md`
- `docs/cross-examination.md`

## Success Criteria
- [ ] `docs/README.md` links to all 29 files under `docs/`, and every file under `docs/` is reachable from the index (no orphans in either direction)
- [ ] Every relative link inside any `docs/*.md` file resolves to an existing target when the link path is followed from that file's own location
- [ ] No `docs/*.md` file contains a repo-root-relative (leading `/`) link path or prose assuming full-repo checkout context that would break in a `docs/`-only copy
- [ ] All 29 files use clean, well-formed GitHub Flavored Markdown (consistent heading hierarchy, closed code fences, well-formed tables/lists)
- [ ] The two legitimate `http://localhost` config example URLs (`docs/personas-install.md:331`, `docs/providers.md:21`) remain untouched, not misflagged as broken links
- [ ] All fixes are formatting/link-integrity corrections only — no content-accuracy or persona-naming edits made under this task's scope

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Verification Method (link and formatting validation, not code testing — this task has no Go test surface):**
- Link-resolution sweep: for every Markdown link in all 29 `docs/*.md` files, resolve the path relative to the source file and confirm the target exists on disk; treat any unresolved relative link as a failure to fix
- Index completeness check: diff the file list from `ls docs/*.md` against every path linked from `docs/README.md`; both directions must match 1:1 with zero orphans
- Manual formatting walk: open each of the 29 files and confirm heading hierarchy, code fence pairing, and table/list structure render correctly (spot-check via a Markdown preview or `mcp__llm-support-mcp__llm_support_markdown_headers` for heading-level consistency)
- Repo-root-assumption grep: search all 29 files for leading-`/` link paths and confirm zero matches (other than the two known legitimate `http://` config examples)
- Re-run the full link-resolution sweep after all fixes as a final consistency check (zero remaining broken links)

**Integration Tests:**
- N/A — this task produces documentation-only changes; no Go build or test surface is affected. If any fix to `docs/*.md` prose is later found to reference a command/flag, defer that content question to Task 4/5 rather than editing it here.

**Test Files:**
- N/A — no test files are created or modified by this task; verification is the manual link/format checklist above over all 29 `docs/*.md` files

## Risk Mitigation
- **Risk:** Running this check before Tasks 4/5 land their content and persona-naming fixes would mean validating links against soon-to-change file content, wasting the pass if a later edit reintroduces a broken link. **Mitigation:** Hard dependency on Task 4 and Task 5 completion (see Dependencies below); confirm both are merged into the working tree before starting.
- **Risk:** Scope creep into content-accuracy or persona-naming fixes duplicates or conflicts with Task 4/5's work on the same files. **Mitigation:** This task's edits are scoped strictly to link resolution, repo-root-relative path assumptions, and Markdown formatting; if a content or persona-naming issue is noticed in passing, flag it for the respective task instead of fixing it here.
- **Risk:** Misclassifying an intentional cross-repo link (e.g., `docs/*.md` linking back to `../README.md` or `../skill/SKILL.md`) as a "broken self-containment" issue and incorrectly stripping it would remove a legitimate reference. **Mitigation:** Step 3(b) explicitly separates links that intentionally point outside `docs/` from broken in-`docs/` links; only fix links confirmed to point at a missing or moved target, not links that correctly reference the parent repo.
- **Risk:** The two known `http://localhost` URLs in `docs/personas-install.md:331` and `docs/providers.md:21` could be misflagged as broken external links during an automated or careless sweep. **Mitigation:** Explicitly excluded in step 6 and Success Criteria; these are legitimate local-endpoint config examples, not links to validate.

## Dependencies
- Task-04 (Code-to-docs audit) — validates the finalized content of docs/
- Task-05 (Persona reference verification) — validates the finalized persona references in docs/

## Definition of Done
- All 29 files under `docs/` are indexed in `docs/README.md` with no orphans in either direction
- Every relative link within `docs/*.md` resolves to an existing file, verified by a full link-resolution sweep run before and after fixes
- No repo-root-relative link paths or full-repo-checkout-assuming prose remain in any `docs/*.md` file
- All 29 files pass a manual Markdown-formatting spot-check (heading hierarchy, code fences, tables/lists)
- The two legitimate `http://localhost` config example URLs remain unmodified
- AC5 satisfied: documentation files under `docs/` are validated and ready to be imported into the `atcr.dev` repository
