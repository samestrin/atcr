# User Story 2: Structured Model Metadata Schema

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** persona author publishing to the community repo (samestrin/atcr)
**I want** `index.json` entries to carry structured `provider`/`model` (and `tasks`/`tags`) metadata fields alongside the existing `name`/`version`/`description`/`path` fields
**So that** downstream search and discovery tooling can match a persona to the model a user already has, instead of relying on the model name happening to appear in free-text

## Story Context

- **Background:** `PersonaIndexEntry` in `internal/personas/search.go` currently defines only `Name`, `Version`, `Description`, and `Path` (json tags `name`/`version`/`description`/`path`). `Search()` performs case-insensitive substring matching against `Name` and `Description` only — there is no field that carries model identity. Persona YAML source files already declare `provider` and `model` as required, schema-validated keys (per docs/personas-authoring.md), but that metadata is never propagated into the generated `index.json`, so it is invisible to the index consumer today.
- **Assumptions:** The index.json generation path (wherever it is produced — registry build tooling or a publish step) has access to each persona's parsed YAML front matter at generation time, so `provider`/`model`/`tasks`/`tags` can be lifted from the source YAML into the index entry without a second read pass. `encoding/json` in Go silently ignores unknown fields on decode, so older `index.json` payloads that predate this change (missing the new keys) continue to decode into the extended struct with zero-value fields, and newer payloads still decode correctly against any consumer that has not yet picked up the new struct fields.
- **Constraints:** The struct change must be strictly additive — no renaming or removal of `Name`/`Version`/`Description`/`Path`, and no change to their existing `json` tags — so existing index.json files, existing callers of `Search()`, and any cached/pinned index snapshots keep working unmodified. New fields must use `omitempty` (or equivalent) so personas without `tasks`/`tags` don't bloat every entry with empty arrays. This story delivers the schema and index-population layer only; the actual model-aware `Search()` matching logic and the `--model` / `search <model>` CLI-facing behavior are out of scope here (covered by the story that consumes this schema).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `PersonaIndexEntry` in `internal/personas/search.go` gains `Provider`, `Model`, and `Tasks`/`Tags` fields (with appropriate `json`/`yaml` struct tags and `omitempty`), and the code path that generates `index.json` entries populates `Provider`/`Model` from each persona's YAML `provider`/`model` keys.
- **Measurable:** Every persona currently shipped through the community index gets a non-empty `provider` and `model` value in its generated index.json entry; a unit test decoding a pre-change (old-shape) index.json fixture into the extended struct succeeds with zero-value new fields and no decode error.
- **Achievable:** Confined to one struct definition plus the index-generation call site(s) that construct `PersonaIndexEntry` values; no new external dependencies, no changes to HTTP fetch/transport code in `search.go`.
- **Relevant:** This is the prerequisite schema layer for AC2 (model-indexed search) and half of AC7 (authoring contract enforcing model-in-structured-metadata) — without these fields in the index, no consumer can ever search by bound model without falling back to fragile free-text matching.
- **Time-bound:** Completed as a single-sprint task ahead of the Search()-behavior story that depends on it.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-persona-index-entry-schema-extension.md) | PersonaIndexEntry Schema Extension | Unit |
| [02-02](../acceptance-criteria/02-02-index-json-field-population-contract.md) | index.json Field Population Contract | Unit |
| [02-03](../acceptance-criteria/02-03-backward-compatible-decode-test.md) | Backward-Compatible Old-Shape Decode Test | Unit |

## Original Criteria Overview

1. `PersonaIndexEntry` struct is extended with `Provider`, `Model`, and `Tasks`/`Tags` fields carrying correct struct tags, without altering the existing four fields' names or tags.
2. The index.json generation path populates the new fields from each persona's validated `provider`/`model` (and, where present, `tasks`/`tags`) YAML metadata.
3. Decoding an old-shape (pre-change) index.json against the extended struct succeeds without error, and the new fields decode as their zero values (proving backward compatibility).

## Technical Considerations

- **Implementation Notes:** Add `Provider string `json:"provider,omitempty" yaml:"provider,omitempty"``, `Model string `json:"model,omitempty" yaml:"model,omitempty"``, and a `Tasks []string` / `Tags []string` slice (per documentation/persona-yaml-schema.md guidance on `omitempty` and optional `flow`/`inline` styling) to `PersonaIndexEntry` in `internal/personas/search.go`. Keep the index reader's decode path in default (permissive) mode — do not apply `KnownFields(true)` here, since that strict mode belongs on the persona-loading decode path per the documented split, not on the index struct which must stay tolerant of index.json entries from personas published before or after this change.
- **Integration Points:** The index-generation/publish tooling that currently emits `name`/`version`/`description`/`path` per persona (wherever `index.json` is built for the community repo) needs to also emit `provider`/`model`/`tasks`/`tags` sourced from each persona's parsed YAML. This story does not touch `Search()`'s matching logic — that consumes these new fields in a follow-on story.
- **Data Requirements:** `provider` and `model` are already required, schema-validated keys on persona YAML source (per docs/personas-authoring.md / personas/_base.md), so no new authoring data needs to be collected — this story only relays existing validated data into the index schema. `tasks`/`tags` are optional/warranted additions and should default to absent (omitempty) rather than empty-array when not authored.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Old cached/pinned index.json snapshots (generated before this change) lack `provider`/`model`, causing search-by-model to silently miss those personas until the index is regenerated | Medium | Document that `provider`/`model` visibility depends on index regeneration; backward-compatible decode ensures no crash, only reduced discoverability until the index republishes |
| Adding `Tasks`/`Tags` as a nested/flow-style structure (per yaml.v3 `flow`/`inline` options) instead of a flat string slice could complicate the JSON side of the schema, since `index.json` is JSON, not YAML | Low | Keep `Tasks`/`Tags` as a flat `[]string` on both the JSON and YAML tag sides for this story; do not introduce nested struct/flow-style encoding unless a later story demonstrates a concrete need |
| Struct-tag change accidentally alters the JSON key casing or omits `omitempty` on an existing field, breaking compatibility with existing index.json consumers | High | Constrain the diff to strictly additive new fields; leave `Name`/`Version`/`Description`/`Path` tags byte-for-byte unchanged; add a decode-compatibility unit test against an old-shape fixture |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
