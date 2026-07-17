# User Story 5: Document the Quality-Signal Telemetry Contract

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** privacy-conscious atcr maintainer or contributor evaluating whether to enable the community prompt quality signal
**I want** `docs/telemetry.md` to state, in one place, the exact allowlisted fields of the quality-signal payload, how to opt in, what `--preview` shows, and the absolute no-code/no-finding-content guarantee (including the unsalted-hash caveat on persona identifiers)
**So that** I can verify the privacy contract by reading documentation alone, without auditing Go source, before ever enabling transmission

## Story Context

- **Background:** `docs/telemetry.md` already documents the Epic 28.0 usage-ping contract (4-field `Event` allowlist, opt-out truth table) and the `--sync-cloud` persona leaderboard payload, including a "Persona Leaderboard data" section that explains `HashPersonaID`'s unsalted-SHA-256, dictionary-attack caveat. This story extends the same file with a new section for the quality-signal payload built by Stories 1-4: the aggregation (`internal/telemetry/quality_signal.go`), the independent opt-in gate (`cmd/atcr/telemetry.go`, persisted via `internal/registry/telemetry_setting.go`, settable via `cmd/atcr/config.go`), and the `--preview` flag (`cmd/atcr/flags.go`). No new code is written by this story — it documents behavior Stories 1-4 already implement and tested.
- **Assumptions:** Stories 1-4 land first (or in the same sprint phase) so the documented field names, gate/config-key names, and `--preview` output format match the shipped implementation exactly; this story is a documentation pass, not a design decision — where Stories 1-4 leave a naming choice ambiguous, this story documents whatever they actually built, verified against the code and its tests. The existing `docs/telemetry.md` structure (usage-ping schema, opt-out table, persona-hash caveat, cloud-sync section) is the pattern to extend, not replace.
- **Constraints:** Must not alter or contradict the existing Epic 28.0 documentation already in the file (usage-ping schema, opt-out mechanism, `--sync-cloud` section) — this is an additive section. Must explicitly restate the `HashPersonaID` unsalted-hash caveat in the new section's context (TD-007) rather than only cross-referencing it, since a reader may reach the quality-signal section without having read the persona-leaderboard section first. Must not describe the payload as a superset or extension of the existing `Event` struct — per the design note, it is documented as its own equally-strict, separately-tested allowlist.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (payload aggregation/allowlist struct), Story 2 (opt-in gate + config key), Story 3 (`--preview` behavior) |

## Success Criteria (SMART Format)

- **Specific:** `docs/telemetry.md` gains a new section (e.g. "Community prompt quality signal") documenting: the exact allowlisted field list of the quality-signal payload as shipped in `internal/telemetry/quality_signal.go` (name, type, one-line meaning per field, mirroring the existing usage-ping schema table's format); the independent opt-in mechanism (env var and/or `atcr config set` key from Story 2, stated as non-overriding with the existing `telemetry`/`--sync-cloud` gates, matching the "OR'd, no precedence" framing already used for the usage-ping opt-out); the `--preview` flag's exact behavior (what it prints, that it never transmits, per Story 3); and a restated, explicit "no code, no finding content, ever" line plus the `HashPersonaID` unsalted-hash/dictionary-attack caveat (TD-007) applied to the persona identifiers in this specific payload.
- **Measurable:** The new section's field table lists every field the shipped struct actually serializes (verified by reading the struct and its allowlist regression test from Story 1) — zero fields undocumented, zero documented fields that don't exist in the struct; a reviewer can cross-check the doc against `internal/telemetry/quality_signal.go` and its test in under 5 minutes and find no discrepancy.
- **Achievable:** Pure documentation edit to one existing file, following an established in-file pattern (the usage-ping schema table, the opt-out truth table, the persona-hash caveat section) that only needs to be replicated for the new payload — no new tooling, no new file.
- **Relevant:** Directly satisfies epic AC3's documentation requirement ("docs document the exact content-free telemetry contract... fields transmitted, opt-in mechanism, preview behavior, and the absolute no-code/no-finding-content line") and is the artifact a privacy-conscious maintainer or contributor reads before deciding to opt in.
- **Time-bound:** Completable within the same sprint phase as, and immediately after, Stories 1-3 land (so the documented contract reflects shipped behavior, not a plan), ahead of `/sprint-complete`.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-document-quality-signal-field-allowlist.md) | Document the Quality-Signal Payload's Exact Field Allowlist | Manual (doc review) |
| [05-02](../acceptance-criteria/05-02-document-optin-mechanism-and-preview-behavior.md) | Document the Independent Opt-In Mechanism and `--preview` Behavior | Manual (doc review) |
| [05-03](../acceptance-criteria/05-03-document-privacy-guarantee-and-persona-hash-caveat.md) | State the Absolute No-Code/No-Finding-Content Guarantee and Restate the Persona-Hash Caveat | Manual (doc review) |

## Original Criteria Overview

1. `docs/telemetry.md` documents the exact allowlisted field set of the quality-signal payload (field name, type, meaning), matching `internal/telemetry/quality_signal.go` and its allowlist regression test with no discrepancy in either direction.
2. `docs/telemetry.md` documents the independent opt-in mechanism (env var / `atcr config set` key from Story 2) as non-overriding with the existing `telemetry` and `--sync-cloud` gates, and documents the `--preview` flag's exact local-only rendering behavior from Story 3.
3. `docs/telemetry.md` states the absolute no-code/no-finding-content guarantee for this payload explicitly (not only by cross-reference) and restates the `HashPersonaID` unsalted-hash/dictionary-attack caveat (TD-007) in the context of this payload's persona identifiers.



## Technical Considerations

- **Implementation Notes:** Edit `docs/telemetry.md` only. Add a new top-level section after the existing "Persona Leaderboard data" / "Cloud sync (`--sync-cloud`)" sections (or wherever Stories 1-4's design-sprint placement lands it structurally), reusing the existing table format for the field allowlist and the existing OR'd-truth-table format for the opt-in gate. Pull the exact field names/types directly from the shipped `internal/telemetry/quality_signal.go` struct and its allowlist test (mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`'s pattern per the epic's architecture note) rather than from the plan-stage design notes, since Stories 1-4 may finalize naming during implementation.
- **Integration Points:** `docs/telemetry.md` (sole file edited); read-only references to `internal/telemetry/quality_signal.go` (Story 1's payload struct + allowlist test), `cmd/atcr/telemetry.go` / `internal/registry/telemetry_setting.go` / `cmd/atcr/config.go` (Story 2's gate + config key), `cmd/atcr/flags.go` (Story 3's `--preview` flag), and `internal/scorecard/scorecard.go`'s `HashPersonaID` (existing TD-007 caveat, already documented once for the leaderboard and restated here for this payload).
- **Data Requirements:** None — this story produces no code, schema, or data; it documents data shapes Stories 1-3 already define and test.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Documentation drifts from the shipped implementation if Stories 1-3 rename a field or the gate/config key after this story is drafted | High | Sequence this story last within the phase (after Stories 1-3 merge or are code-complete) and verify every documented field/key name against the actual source and its allowlist test before finalizing, not against the plan-stage design notes |
| New section is written as if it extends or is a superset of the existing 4-field `Event` schema, blurring two distinct allowlists | Medium | Explicitly state the quality-signal payload is its own separately-defined and separately-tested struct, per the epic's "Technical Planning Notes"; do not merge the two schema tables into one |
| Reader misses the persona-hash pseudonymity caveat because it is only cross-referenced from the existing "Persona Leaderboard data" section | Medium | Restate the `HashPersonaID` unsalted-hash/dictionary-attack caveat (TD-007) directly in the new section's text, not solely as a link/cross-reference |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
</content>
