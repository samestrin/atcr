# Acceptance Criteria: Local `.atcr/`-Scoped TD Store Documentation

**Related User Story:** [05: Document Debt-Resolve in skill-usage.md](../user-stories/05-document-debt-resolve-in-skill-usage.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/skill-usage.md`) | New subsection styled after `docs/scorecard.md`'s Storage section |
| Test Framework | Go `testing` (doc-presence/content assertions), mirroring `internal/scorecard/docs_test.go` | Verifies required facts (path, flag name, population trigger) are present as literal substrings |
| Key Dependencies | Story 1's `documentation/local-td-store-schema.md` (schema source of truth) and Story 2's `--no-local-debt` flag (landed CLI behavior) | Content must match the landed store path, flag name, and population trigger exactly |

### Related Files (from codebase-discovery.json)
- `docs/skill-usage.md` — modify: add a Storage/CLI-usage-style subsection (within or adjacent to the Technical Debt Resolution section from AC 05-01) describing the local `.atcr/`-scoped TD store: location (`.atcr/debt/`), how it's populated (by `atcr reconcile`, appended per run), and the `--no-local-debt` opt-out flag
- `docs/scorecard.md` — reference (read-only): the structural/tone precedent (its `## Storage` section: path, rotation, permissions, append-only note, "do not commit" callout) this subsection must mirror
- `documentation/local-td-store-schema.md` — reference (read-only): source of truth for the store's path (`.atcr/debt/`), sharding (`YYYY-MM.jsonl`), and permissions (`0700`/`0600`) claims, to keep the doc accurate without restating the full schema
- `cmd/atcr/reconcile.go` — reference (read-only): the landed `--no-local-debt` flag declaration (Story 2) that the doc's flag name and default-behavior description must match exactly
- `internal/localdebt/store.go` — reference (read-only): append-only store implementation that populates `.atcr/debt/`
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/append-only-store-pattern.md` — reference: atomic-append and cross-run accumulation requirements

## Happy Path Scenarios
**Scenario 1: Reader learns where the store lives and how it fills up**
- **Given** a standalone user has run `atcr reconcile` at least once
- **When** they read the new Storage subsection
- **Then** they can state, without reading source code: the store's path (`.atcr/debt/`, repo-scoped), that it is populated automatically by `atcr reconcile` (no flag needed to enable it, mirroring scorecard's "written silently as a byproduct" framing), and that it is local/uncommitted state (not to be added to version control)

**Scenario 2: Reader learns how to opt out**
- **Given** a user who does not want a given reconcile run's findings persisted
- **When** they read the Storage subsection
- **Then** they find the exact flag name (`--no-local-debt`) and its effect (suppresses persistence for that single run only, mirroring `--no-scorecard`'s single-run suppression semantics)

## Edge Cases
**Edge Case 1: Section mirrors scorecard.md's format-specific details without over-copying irrelevant ones**
- **Given** `docs/scorecard.md`'s Storage section covers path, monthly rotation, append-only guarantee, permissions, size, and maintenance
- **When** the new subsection is written
- **Then** it covers the equivalent facts for the local TD store (path, monthly `YYYY-MM.jsonl` rotation, append-only guarantee, `0700`/`0600` permissions, "do not commit" callout) but does not fabricate scorecard-specific details (e.g. cost-rate tables) that don't apply to the TD store

**Edge Case 2: Dedup/accumulation behavior is described accurately**
- **Given** Story 2 documents a specific dedup strategy (write-time dedup by `FindingID`, read-time dedup, or at-least-once) as part of its own AC
- **When** the subsection describes cross-run accumulation
- **Then** it states the actual landed dedup behavior (not left ambiguous), so a user re-running `atcr reconcile` on the same repo understands whether duplicate findings can appear in the backlog

## Error Conditions
**Error Scenario 1: Documented store path or flag name drifts from landed code**
- Error message (manual review finding): "docs/skill-usage.md states the local TD store lives at <X> / the opt-out flag is <Y>, but cmd/atcr/reconcile.go and documentation/local-td-store-schema.md say otherwise"
- HTTP status / error code: N/A (documentation-accuracy defect caught in the story's final verification pass)

**Error Scenario 2: Storage subsection omitted entirely**
- Error message (test failure): "docs/skill-usage.md missing required Storage subsection content for the local .atcr/-scoped TD store (expected substrings: '.atcr/debt/', '--no-local-debt')"
- HTTP status / error code: N/A (Go test failure, non-zero exit)

## Performance Requirements
- **Response Time:** N/A (static documentation)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** The subsection must explicitly carry forward the "do not commit" / local-state callout (matching `docs/scorecard.md`'s privacy framing) so readers do not accidentally version-control `.atcr/debt/`, which may contain findings text derived from their private codebase

## Test Implementation Guidance
**Test Type:** UNIT (doc-presence/content structural assertions, mirroring `internal/scorecard/docs_test.go`; asserts required substrings `.atcr/debt/` and `--no-local-debt` are present in `docs/skill-usage.md`)
**Test Data Requirements:** None beyond the repository's own `docs/skill-usage.md` file at test time
**Mock/Stub Requirements:** None — reads the real file from disk

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/skill-usage.md` documents the local store's path, population trigger, and permissions/rotation, styled after `docs/scorecard.md`'s Storage section
- [ ] `--no-local-debt` flag name and single-run-suppression behavior are documented accurately
- [ ] Cross-run accumulation/dedup behavior is described per Story 2's actual landed decision, not left ambiguous
- [ ] Content verified against `documentation/local-td-store-schema.md` and the landed `cmd/atcr/reconcile.go` flag before sign-off

**Manual Review:**
- [ ] Code reviewed and approved
