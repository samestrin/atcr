# Task 05: Persona Reference Verification — Confirm sasha/penny/ingrid Consistency, No Legacy Slugs Remain

**Source:** Plan 33.0 – Debt Item #5
**Priority:** P2 | **Effort:** S | **Type:** Fix

## Problem Statement
Epic 23.0 renamed the built-in reviewer personas from role-based slugs (`sentinel`, `tracer`, `idiomatic`) to human names (`sasha`, `penny`, `ingrid`/`ian`) with generalized multi-language capabilities. Going public (Epic 33.2) makes `README.md`, `docs/`, `skill/SKILL.md`, and CLI help text the world-visible source of truth — any lingering reference to a retired slug in prose (as opposed to code, which is already gated by an automated test) would mislead users and contradict the finalized persona set. This task closes AC3: no legacy persona names (`sentinel`, `tracer`, `idiomatic`) remain in any documentation or command help screens. The audit must be precise, not blunt: `sentinel` and `idiomatic` both have legitimate, unrelated technical meanings elsewhere in the codebase (Go sentinel error/value idiom; the plain-English adjective "idiomatic") and a naive grep-and-flag pass would produce false positives against those files.

## Solution Overview
Run the existing automated guard (`personas/retired_slugs_test.go` / `TestNoRetiredSlugs`) first as the authoritative, regex-scoped AC3 gate — it already asserts zero retired-slug persona identifiers across `personas/`, community personas, and `index.json`. Then layer a targeted human-readable grep sweep over the prose surfaces the automated test does not reach: `README.md`, `docs/**/*.md`, `skill/SKILL.md`, and CLI help/description strings in `cmd/atcr/`. For every hit on `sentinel`, `tracer`, or `idiomatic`, classify it against the false-positive table below before flagging — only persona-identifier usages are stale references; Go sentinel-error idiom and the adjective "idiomatic" in prompt prose are legitimate and must be left untouched. Fix any confirmed stale references by replacing them with the correct finalized name (`sasha`, `penny`, or `ingrid` — **not `ian`**, see the naming-resolution note below) and confirm consistent usage across all persona docs against the shared `personas/_base.md` template.

> **Naming-resolution note (verified against the live codebase, not just the original request):** the original request and this plan's grounding docs describe the `idiomatic` replacement as `ingrid`/`ian`, reflecting an open naming question at Epic 23.0's proposal stage. That question is already resolved in the shipped code: `personas/personas.go:20`'s `names` slice is `["bruce","greta","kai","mira","dax","sasha","penny","ingrid","otto"]` — there is no `ian` persona file, slug, alias, or reference anywhere in the codebase. The refinement record in `.planning/epics/superseded/23.0_human_persona_renaming.md` confirms this explicitly: *"The name ambiguity ('ingrid or ian') is resolved: the codebase uses ingrid."* Treat every `idiomatic` replacement as `ingrid`; do not introduce `ian` anywhere — doing so would itself be a new stale/incorrect reference, not a fix.

## Technical Implementation
### Steps
1. Run the automated gate first: `go test ./personas/... ./internal/personas/...`. Confirm `TestNoRetiredSlugs` (and related coverage in `internal/personas/community_schema_test.go`, `internal/personas/list_test.go`) passes. This is the authoritative check for persona-identifier-scoped retired slugs in `personas/`, community personas, and `index.json`. If it fails, that is a Task 3/Task 4 code-fix concern, not this task's scope — note the failure and stop (do not attempt to patch persona identifier code from this documentation-focused task).
2. Grep the prose surfaces the automated test cannot reach: `README.md`, all files under `docs/`, `skill/SKILL.md`, and `cmd/atcr/root.go` (plus any other `cmd/atcr/*.go` files containing CLI help/description/usage strings), searching case-insensitively for `sentinel`, `tracer`, and `idiomatic`.
3. Classify every hit using the false-positive table in `documentation/persona-naming-doc-accuracy.md` Quick Reference:
   - `sentinel` in `internal/verify/severity.go`, `internal/security/pathguard.go`, or a sentinel-delimiter-line context in `docs/payload-modes.md`/`docs/cross-examination.md` → legitimate Go idiom / delimiter marker, do NOT flag or edit.
   - `idiomatic` used as a plain-English adjective (e.g., "idiomatic Go", "idiomatic code style") in `personas/ingrid.md` prompt prose or elsewhere → legitimate, do NOT flag or edit.
   - Any occurrence of `sentinel`, `tracer`, or `idiomatic` used as a persona name/identifier (e.g., "the sentinel persona", "run tracer against...", "the idiomatic reviewer") → stale reference, DO flag.
4. For each confirmed stale reference, replace it with the correct finalized name based on context: `sentinel` → `sasha`, `tracer` → `penny`, `idiomatic` → `ingrid`. There is no `ingrid`/`ian` distinction to resolve — `ian` never shipped (see the Naming-resolution note above; `personas/personas.go:20` has no `ian` entry) — so every `idiomatic` replacement is `ingrid`, unconditionally.
5. Cross-check naming consistency across all persona documentation against the shared `personas/_base.md` `{{.AgentName}} — code reviewer}}` template — confirm `personas/sasha.md`, `personas/penny.md`, `personas/ingrid.md`, and any others reference each other and themselves consistently, and that `docs/personas-authoring.md` and `docs/personas-install.md` (flagged in grounding as high-relevance persona docs) use only the finalized names.
6. Verify CLI help output directly rather than only reading source: run `go run ./cmd/atcr --help` (and relevant subcommand `--help` invocations, e.g. persona-listing or panel-related commands) and grep the printed output for the three retired slugs, since generated/templated help text can diverge from what's visible in source.
7. Re-run `go test ./personas/... ./internal/personas/...` once more after any edits to confirm the automated gate still passes post-fix.

## Files to Create/Modify
- `README.md` – verify/fix persona name references (modify only if stale reference found)
- `docs/personas-authoring.md` – verify/fix persona naming convention examples
- `docs/personas-install.md` – verify/fix persona installation instructions and panel definitions
- `docs/payload-modes.md` – verify only, no edit expected (contains legitimate sentinel-delimiter usage)
- `docs/cross-examination.md` – verify only, no edit expected (contains legitimate sentinel-delimiter usage)
- `skill/SKILL.md` – verify/fix skill frontmatter and usage instructions
- `cmd/atcr/root.go` – verify/fix inline CLI help text and command descriptions
- `personas/ingrid.md` – verify only, no edit expected (contains legitimate "idiomatic" adjective usage in prompt prose)

## Documentation Links
- [Persona Naming & Documentation Accuracy](../documentation/persona-naming-doc-accuracy.md)

## Related Files (from codebase-discovery.json)
- `personas/retired_slugs_test.go` — automated AC3 guard (`TestNoRetiredSlugs`), the authoritative check for persona-identifier-scoped retired slugs
- `internal/personas/community_schema_test.go` — related retired-slug coverage
- `internal/personas/list_test.go` — related retired-slug coverage
- `personas/_base.md` — shared persona prompt template defining `{{.AgentName}} — code reviewer` structure
- `personas/ingrid.md` — renamed successor to retired `idiomatic`; contains legitimate non-persona use of the word
- `README.md`
- `docs/README.md`
- `docs/personas-authoring.md`
- `docs/personas-install.md`
- `skill/SKILL.md`
- `cmd/atcr/root.go`

## Success Criteria
- [ ] `go test ./personas/... ./internal/personas/...` passes both before and after this task's edits (TestNoRetiredSlugs and related coverage)
- [ ] Grep sweep of `README.md`, `docs/`, `skill/SKILL.md`, and `cmd/atcr/` CLI help strings for `sentinel`, `tracer`, `idiomatic` completed, with every hit classified against the legitimate-usage/stale-reference table
- [ ] Zero confirmed stale persona-identifier references to `sentinel`, `tracer`, or `idiomatic` remain in any documentation or CLI help output
- [ ] Legitimate technical usages (Go sentinel-error idiom, sentinel-delimiter lines, the adjective "idiomatic" in prompt prose) are left unmodified — verified by re-reading the specific files listed in the false-positive table after the sweep
- [ ] `sasha`, `penny`, `ingrid` are used consistently across `personas/*.md`, `README.md`, `docs/personas-authoring.md`, `docs/personas-install.md`, and `skill/SKILL.md` (no `ian` reference is introduced anywhere — see Naming-resolution note in Solution Overview)
- [ ] `go run ./cmd/atcr --help` (and relevant subcommand help) output contains no retired slugs

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- Run `go test ./personas/... ./internal/personas/...` — must pass (TestNoRetiredSlugs and related coverage in `internal/personas/community_schema_test.go`, `internal/personas/list_test.go`)

**Integration Tests:**
- Run `go run ./cmd/atcr --help` and subcommand `--help` variants; manually grep the actual printed output (not just source) for `sentinel`, `tracer`, `idiomatic` to catch template/generated-text drift the automated test cannot see
- Manual grep sweep across `README.md`, `docs/**/*.md`, `skill/SKILL.md` for the three retired slug terms, with each hit manually classified per the false-positive table before being treated as a finding

**Test Files:**
- `personas/retired_slugs_test.go`
- `internal/personas/community_schema_test.go`
- `internal/personas/list_test.go`

## Risk Mitigation
- **Risk:** A naive grep-and-replace pass misflags legitimate technical usages — the Go sentinel error/value idiom in `internal/verify/severity.go` and `internal/security/pathguard.go`, sentinel-delimiter lines in `docs/payload-modes.md` and `docs/cross-examination.md`, or the plain-English adjective "idiomatic" in `personas/ingrid.md` prompt prose — and corrupts correct, unrelated content. **Mitigation:** Classify every grep hit against the false-positive table in `documentation/persona-naming-doc-accuracy.md` before editing anything; when in doubt about whether a hit is a persona reference vs. a technical term, read the surrounding sentence for persona-identifier context (e.g., "the X persona", "run X against") rather than pattern-matching the bare word.
- **Risk:** `tracer` has no known legitimate non-persona usage per the grounding doc, which could tempt a blanket search-and-replace without individual review, risking mangled unrelated prose (e.g., "stack tracer" in an unrelated context, if one exists). **Mitigation:** Still review each `tracer` hit individually rather than assuming zero false positives; the grounding notes "no known legitimate usage identified," not "verified absent," so confirm case-by-case.
- **Risk:** CLI help text may be generated/templated (e.g., from Cobra command registration) rather than a literal string match in `cmd/atcr/root.go`, so a source-only grep could miss drift visible only in actual `--help` output. **Mitigation:** Step 6 explicitly runs `go run ./cmd/atcr --help` and greps the rendered output, not just source files.
- **Risk:** Treating `ingrid`/`ian` as an unresolved choice and introducing `ian` somewhere (e.g., renaming a stale `idiomatic` reference to `ian` instead of `ingrid`) would fabricate a persona name that does not exist in the codebase, introducing a new inaccuracy while fixing the old one. **Mitigation:** Per the Naming-resolution note in Solution Overview, `personas/personas.go:20` and the superseded Epic 23.0 refinement record both confirm the codebase uses `ingrid` only — there is no ambiguity to resolve case-by-case; always replace `idiomatic` with `ingrid`.

## Dependencies
- Task-03 (Findings triage) — docs must describe finalized, post-fix code, since this task's scope is prose-only verification and must run against the codebase state after CRITICAL/HIGH review findings are resolved
- Can run in parallel with or after Task-04 (code-to-docs audit) — both are documentation-sweep tasks operating on the same post-fix codebase, but this task's scope (persona-name correctness) is narrower and independent of Task-04's broader code-to-docs accuracy scope

## Definition of Done
- `go test ./personas/... ./internal/personas/...` passes as the automated AC3 gate.
- The prose grep sweep across `README.md`, `docs/`, `skill/SKILL.md`, and CLI help output/source is complete, with every hit on `sentinel`, `tracer`, `idiomatic` explicitly classified as legitimate or stale.
- All confirmed stale persona-identifier references have been corrected to the appropriate finalized name (`sasha`, `penny`, or `ingrid` — never `ian`, which never shipped).
- All legitimate technical usages (Go sentinel idiom, sentinel-delimiter lines, "idiomatic" adjective in prompt prose) remain unmodified, spot-checked after the sweep.
- AC3 ("No legacy persona names remain in any documentation or command help screens") is satisfied and verifiable via both the automated test and the manual sweep record.
