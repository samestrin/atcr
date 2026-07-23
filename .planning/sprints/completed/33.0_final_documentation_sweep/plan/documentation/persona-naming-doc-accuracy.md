# Persona Naming & Documentation Accuracy

**Priority: [IMPORTANT]**

## Overview

This category grounds Task 2 (persona reference verification), Task 4 (code-to-docs audit), and Task 6 (website compatibility check) of Plan 33.0, the final documentation sweep. The core concern is verifying that legacy, role-based persona slugs — `sentinel`, `tracer`, and `idiomatic` — have been fully eliminated from the active persona set in favor of their finalized, generalized replacement names (`sasha`, `penny`, `ingrid`/`ian`), while ensuring the audit does not misfire on legitimate, unrelated technical usages of the same words.

An automated guard already exists for this: `personas/retired_slugs_test.go`, built during Epic 19.6/23.0, asserts zero references to the retired role slugs across the active persona set (`personas/`, community personas, `index.json`), using a regex scoped to persona-identifier context.

> Source: codebase-discovery.json > build_from

The recommended approach is to treat this test suite as the automated gate for AC3 (no retired slugs remain), and then layer a targeted human-readable sweep over prose surfaces — docs/, README.md, `skill/SKILL.md`, and CLI help strings — that the automated regex cannot reach.

> Source: codebase-discovery.json > build_from.suggested_approach

Beyond persona naming, this category also covers the broader documentation-accuracy and website-compatibility concerns for Plan 33.0: README.md and `skill/SKILL.md` must reflect the finalized persona names and current CLI command set, and the `docs/` directory — a 29-file canonical documentation set — must remain self-contained and link-correct for import into the atcr.dev website.

> Source: codebase-discovery.json > semantic_matches, architecture_notes

## Key Concepts

- **Retired-slug guard test**: `personas/retired_slugs_test.go` is the authoritative automated mechanism enforcing that no persona identifier uses the retired role slugs (`sentinel`, `tracer`, `idiomatic`) anywhere in the active persona set. Related coverage lives in `internal/personas/community_schema_test.go` and `internal/personas/list_test.go`.
  > Source: codebase-discovery.json > existing_patterns > "Retired-slug guard test"

- **Sentinel error / sentinel value is a distinct, legitimate Go idiom** — not a stale persona reference. Files such as `internal/verify/severity.go`, `internal/security/pathguard.go`, `docs/payload-modes.md`, and `docs/cross-examination.md` use "sentinel" in the standard Go sense (a sentinel error value, or a sentinel delimiter line). These must be filtered out of the AC3 audit; only flag "sentinel" when it is used as a persona name.
  > Source: codebase-discovery.json > existing_patterns > "Sentinel error / sentinel value (Go idiom)"

- **The word "idiomatic" is a legitimate adjective in persona prompt prose**, distinct from the retired persona slug of the same name. `personas/ingrid.md` — the renamed successor to the retired `idiomatic` persona — legitimately contains the word "idiomatic" in its prompt text (e.g., describing idiomatic code style) and must not be misflagged as a stale reference.
  > Source: codebase-discovery.json > semantic_matches > "personas/ingrid.md"

- **Shared persona prompt template**: `personas/_base.md` defines the `{{.AgentName}} — code reviewer` structure that all persona documents inherit, and is a reference point for verifying consistent naming conventions across personas.
  > Source: codebase-discovery.json > semantic_matches > "personas/_base.md"

- **Documentation Index as website source of truth**: `docs/README.md` is the canonical index of the 29-file documentation set under `docs/`, intended for import into the atcr.dev website. All paths and links within this set must be relative and self-contained.
  > Source: codebase-discovery.json > semantic_matches > "docs/README.md"; codebase-discovery.json > architecture_notes

- **Architectural distinction to preserve during audit**: the sweep must distinguish technical uses of "sentinel" (Go error sentinels, payload delimiters) and "idiomatic" (plain English adjective in prompt prose) from stale persona names, to avoid false positives during the code-to-docs audit.
  > Source: codebase-discovery.json > architecture_notes

## Quick Reference

### Legitimate technical usage vs. stale persona reference

| Term | Legitimate usage (do NOT flag) | Stale persona reference (DO flag) | Source |
|------|-------------------------------|-------------------------------------|--------|
| `sentinel` | Go sentinel error/value idiom in `internal/verify/severity.go`, `internal/security/pathguard.go`; sentinel delimiter line in `docs/payload-modes.md`, `docs/cross-examination.md` | Any use of "sentinel" as a persona identifier/name in `personas/`, community personas, `index.json`, README, or SKILL.md | codebase-discovery.json > existing_patterns > "Sentinel error / sentinel value (Go idiom)" |
| `idiomatic` | Plain-English adjective describing code style in `personas/ingrid.md` prompt prose | Any use of "idiomatic" as a persona identifier/name (the retired persona now renamed to `ingrid`) | codebase-discovery.json > semantic_matches > "personas/ingrid.md"; codebase-discovery.json > architecture_notes |
| `tracer` | (No known legitimate non-persona usage identified in grounding — see personas/retired_slugs_test.go) | Any use of "tracer" as a persona identifier/name (retired in favor of `penny`) | codebase-discovery.json > build_from |

### Files to modify (audit checklist)

| File | Audit focus | Source |
|------|-------------|--------|
| `README.md` | Primary user-facing entry point; audit for accuracy, persona names, and command flags | codebase-discovery.json > files_to_modify |
| `docs/README.md` | Docs directory index; verify links to all 29 docs for atcr.dev import | codebase-discovery.json > files_to_modify |
| `docs/personas-authoring.md` | Verify persona naming conventions and examples reference sasha/penny/ingrid/ian | codebase-discovery.json > files_to_modify |
| `docs/personas-install.md` | Verify persona installation instructions and default panel definitions | codebase-discovery.json > files_to_modify |
| `skill/SKILL.md` | Verify skill frontmatter and usage instructions match finalized CLI behavior | codebase-discovery.json > files_to_modify |
| `cmd/atcr/root.go` | Audit inline CLI help text strings and command descriptions for accuracy | codebase-discovery.json > files_to_modify |

## Related Documentation

- [personas/retired_slugs_test.go](../../../../../personas/retired_slugs_test.go) — retired-slug guard test
- [docs/README.md](../../../../../docs/README.md) — canonical index of the 29-file documentation set under docs/
- [docs/personas-authoring.md](../../../../../docs/personas-authoring.md) — persona authoring and naming guidelines
- [docs/personas-install.md](../../../../../docs/personas-install.md) — persona installation and panel setup instructions
- [README.md](../../../../../README.md) — top-level project README
- [skill/SKILL.md](../../../../../skill/SKILL.md) — atcr dispatcher skill definition

