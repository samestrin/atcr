# User Story 2: Publish Model-Tuned Personas to the Community Registry Index

**Plan:** [19.6: Default Model-Tuned Community Personas](../plan.md)

## User Story

**As a** community persona contributor maintaining the `atcr/personas` repo
**I want** the new model-tuned persona entries added to `index.json`
**So that** new atcr users can discover them via `atcr personas search` and install them via `atcr personas install <name>` without knowing the personas exist ahead of time

## Story Context

- **Background:** Epic 9.0 (Persona Ecosystem, completed) already shipped the full community-persona distribution mechanism in this codebase (atcr): `atcr personas install/search/list/upgrade/remove`, which fetches `<ATCR_PERSONAS_URL>/index.json` and, on install, `<ATCR_PERSONAS_URL>/<name>.yaml`. `ATCR_PERSONAS_URL` defaults to `https://raw.githubusercontent.com/atcr/personas/main`. Story 1 authors 3 new model-tuned persona YAML + prompt templates — one each for Anthropic Claude, OpenAI GPT, and Google Gemini, each with a flagship-primary + same-family-fallback model pair — plus their passing fixtures in the external `atcr/personas` repo. This story is the publication step: registering those already-authored personas in that repo's `index.json` so they become part of the searchable/installable catalog.
- **Assumptions:** Story 1's persona YAML files and fixtures already exist and pass validation before this story's index.json entries are added; the `index.json` schema and file format are already defined by Epic 9.0 and are not being changed; the `atcr/personas` repo maintainer (which may be the same person operating this plan) has commit access to add entries.
- **Constraints:** **This story's actual implementation work happens entirely in the external `atcr/personas` GitHub repository, not in this codebase (atcr).** This plan's own TDD/sprint execution loop in this repo cannot execute, edit, or directly verify changes to that external repo — it can only track this story as a dependency and confirm the outcome externally (e.g., by running `atcr personas search <keyword>` and `atcr personas install <namespace/name>` against the live `atcr/personas` repo once the index.json update is published). No new index.json schema, hosting mechanism, or distribution channel may be introduced — this is strictly a content addition using the existing contract. This story depends on Story 1: the persona YAML + fixtures must exist and pass their own validation before index.json entries pointing to them are added, otherwise `atcr personas install` would fetch a non-existent or unvalidated YAML file.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (Author Model-Tuned Persona Content) |

## Success Criteria (SMART Format)

- **Specific:** Add one `index.json` entry per new persona authored in Story 1 (3 entries — Anthropic, OpenAI, Google) to the external `atcr/personas` repo, each with the fields required by the existing index schema (name, description, and any other fields Epic 9.0's schema requires), so the persona is listed in the catalog.
- **Measurable:** Running `atcr personas search <keyword>` (where keyword matches the new persona's name or description) against the live `ATCR_PERSONAS_URL` returns the new persona in the results; running `atcr personas install <namespace/name>` for each new persona succeeds and writes a valid YAML file to `~/.config/atcr/personas/`.
- **Achievable:** Uses only the existing, already-shipped index.json schema and fetch contract (`internal/personas/client.go`'s `FetchIndex`/`FetchPersonaYAML`) — no new code, schema, or infrastructure required.
- **Relevant:** Without this publication step, the personas authored in Story 1 exist as files in the external repo but remain invisible and uninstallable to atcr users — this story is what makes them actually reachable via the CLI.
- **Time-bound:** Complete within the same publication window as Story 1's content authoring, before this plan's Definition of Done is marked complete.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-index-json-entries-added.md) | index.json Entries Added for the 3 New Personas | Manual |
| [02-02](../acceptance-criteria/02-02-search-and-install-discoverability.md) | End-to-End Search and Install Discoverability | Manual |

## Original Criteria Overview

1. Each of the 3 new personas from Story 1 (Anthropic, OpenAI, Google) has a corresponding entry in the external `atcr/personas` repo's `index.json`, conforming to the existing index schema (no new fields or format introduced).
2. `atcr personas search <keyword>` against the live community repo returns each new persona when searched by its name or a matching description keyword.
3. `atcr personas install <namespace/name>` against the live community repo successfully fetches, validates, and installs each new persona's YAML to the local personas directory.

## Technical Considerations

- **Implementation Notes:** This story is pure content addition to a JSON file (`index.json`) in the external `atcr/personas` repo — no code changes in this repo (atcr) are required or in scope. The existing fetch/search/install logic in `internal/personas/client.go` already implements the full contract; nothing here needs to change for the new entries to be discoverable and installable.
- **Integration Points:** `<ATCR_PERSONAS_URL>/index.json` (fetched by `atcr personas search`/`list`) and `<ATCR_PERSONAS_URL>/<name>.yaml` (fetched by `atcr personas install`), both already implemented by `internal/personas/client.go`'s `FetchIndex`/`FetchPersonaYAML`. Persona names may contain letters, digits, `_`, `-`, and `/` (the namespace separator), per `docs/personas-install.md`.
- **Data Requirements:** Each index.json entry must reference a persona name/path that resolves to a valid, already-published YAML file (from Story 1) at `<ATCR_PERSONAS_URL>/<name>.yaml`; entries must include whatever fields the existing index schema requires (e.g., name, description) so search matching and install resolution work correctly.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| index.json entry is added before Story 1's corresponding YAML/fixture is fully authored and validated, causing `atcr personas install` to fail against a missing or broken file | Medium | Sequence this story strictly after Story 1 completes; verify the YAML file exists and passes its fixture before adding the index entry |
| This plan's Definition of Done cannot directly execute or verify changes in the external `atcr/personas` repo, risking a false "complete" status | Medium | Treat this story's completion as externally verified only — confirm via live `atcr personas search`/`install` commands against the published repo, not via this repo's own test/sprint loop |
| Malformed or schema-inconsistent JSON entry breaks parsing of the entire index.json for all personas, not just the new ones | High | Validate the edited index.json against the existing schema/parser (e.g., by running `atcr personas search` locally against a test copy) before publishing to the live repo |

---

**Created:** July 06, 2026
**Status:** Acceptance Criteria Generated
