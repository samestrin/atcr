# Acceptance Criteria: `docs/scorecard.md` Privacy Model Section Updated for Telemetry/Cloud-Sync

**Related User Story:** [05: Telemetry Privacy Documentation](../user-stories/05-telemetry-privacy-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (in-place section edit) | Existing "Privacy Model" section, `docs/scorecard.md` (~line 277) |
| Test Framework | `go test` (Go standard testing package) | `cmd/atcr/docs_audit_test.go` audits the edited section along with the rest of `docs/` |
| Key Dependencies | None (pure documentation, no new package) | Must preserve, not replace, existing Epic 10.0 content |

## Related Files
- `docs/scorecard.md` - modify: extend the "Privacy Model" section (~line 277) with a subsection describing the telemetry ping / hashed-Persona-ID / `--sync-cloud` data path, cross-linking to `docs/telemetry.md`
- `docs/telemetry.md` - reference (created by AC 05-01): the cross-link target this AC's edit must point to

## Happy Path Scenarios

**Scenario 1: Existing Epic 10.0 privacy content is preserved verbatim**
- **Given** `docs/scorecard.md`'s current Privacy Model section documents the local store, the `--export` allowlist ("Preserved"/"Stripped / never exported" lists), and the `scrubField` defense-in-depth scrubber
- **When** the section is updated for this story
- **Then** every existing sentence, list item, and claim about the `--export` allowlist path remains present and unchanged — the edit is additive, not a rewrite

**Scenario 2: New telemetry/cloud-sync data path is described as a distinct path**
- **Given** Stories 1-4 introduce a background telemetry ping and an explicit `--sync-cloud` push, both separate from the `--export` allowlist boundary
- **When** the Privacy Model section is updated
- **Then** a new subsection (e.g. "Telemetry & Cloud Sync") states plainly that these are a separate, additive data path from the local-store `--export` allowlist, and briefly summarizes what leaves the machine (the `{event, lang, lines, status}` ping; the hashed Persona ID and anonymized scorecard payload for `--sync-cloud`) and under what trigger (background/automatic for the ping, explicit opt-in flag for cloud sync)

**Scenario 3: Section cross-links to `docs/telemetry.md`**
- **Given** the full telemetry/cloud-sync privacy detail (opt-out mechanics, exact schema, exit codes) belongs in the dedicated `docs/telemetry.md` document per this story's Constraints
- **When** the new subsection is written
- **Then** it includes a markdown link to `docs/telemetry.md` (e.g. `[docs/telemetry.md](telemetry.md)`) directing the reader there for the complete telemetry privacy model, rather than duplicating the full detail inline

**Scenario 4: No conflation between the two privacy boundaries**
- **Given** the existing `--export` allowlist and the new telemetry/cloud-sync path use overlapping concepts (anonymization, allowlisting) but are implemented as fully separate schemas per Story 3's Constraints
- **When** a reader reads the full Privacy Model section top to bottom
- **Then** the wording clearly attributes each guarantee to its correct path (e.g. "the `--export` allowlist below applies only to the local-store leaderboard export; the telemetry ping and `--sync-cloud` push use a separate schema, described in `docs/telemetry.md`") so no sentence could be misread as describing both paths at once

## Edge Cases

**Edge Case 1: Section heading structure**
- **Given** the existing "Privacy Model" H2 heading and its established list-based structure ("Preserved (allowlist):" / "Stripped / never exported:")
- **When** the new telemetry/cloud-sync subsection is added
- **Then** it uses a consistent heading level (H3 under the existing H2, or a clearly separated new paragraph block) so the document's structure/TOC remains coherent and the new content is not visually merged into the existing allowlist tables

**Edge Case 2: Wording does not imply a privacy regression**
- **Given** the Potential Risks section of this story flags the risk that new telemetry wording could be misread as weakening the existing guarantee
- **When** the update is reviewed
- **Then** no sentence states or implies that telemetry/cloud-sync reduces, replaces, or is governed by the same guarantee as the `--export` allowlist — each data path's scope is stated independently

## Error Conditions

**Error Scenario 1: Cross-link target does not exist**
- **Given** the new subsection links to `docs/telemetry.md`
- **When** `docs/telemetry.md` has not yet been created (e.g. this AC is implemented out of order relative to AC 05-01)
- **Then** the link is a dangling reference; while `docs_audit_test.go`'s link checks operate on `docs/README.md` rather than inline cross-links, this AC's Definition of Done still requires `docs/telemetry.md` to exist before this AC is considered complete, since a link to a nonexistent file fails manual review

**Error Scenario 2: Existing content accidentally removed or altered**
- **Given** the Constraints explicitly forbid weakening the existing Epic 10.0 guarantee
- **When** a diff review of `docs/scorecard.md` shows any existing "Preserved"/"Stripped / never exported" list item removed, reworded, or reordered in a way that changes its meaning
- **Then** the change is rejected in review — the edit must be strictly additive to the existing Privacy Model section

## Performance Requirements
- **Response Time:** Not applicable — static documentation, no runtime execution path.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** N/A — this AC only documents that `--sync-cloud` uses `ATCR_API_KEY`; it must not include example real credentials.
- **Input Validation:** The new subsection's factual claims (e.g. "Persona ID is hashed, not sent raw") must match Story 3's actual implementation so the documented privacy guarantee is not aspirational.

## Test Implementation Guidance
**Test Type:** UNIT (existing Go test suite; no new test code required — verified by pre-existing `docs_audit_test.go` checks plus manual diff review)
**Test Data Requirements:** A side-by-side diff of `docs/scorecard.md` before/after this AC's edit, confirming the existing Privacy Model content is unchanged and the new subsection is additive.
**Mock/Stub Requirements:** None.

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/... -run TestDocs` passes (no flag/index regressions introduced by this edit)
- [ ] No linting errors (markdown lint / repo formatting conventions, if configured)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Existing "Preserved (allowlist)" / "Stripped / never exported" content is unchanged
- [ ] New subsection describes the telemetry ping and `--sync-cloud` path as separate/additive from the `--export` allowlist
- [ ] New subsection cross-links to `docs/telemetry.md`
- [ ] No sentence implies the new data path weakens or supersedes the existing `--export` guarantee

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Diff confirms the edit is additive only (no removed or reworded existing Privacy Model content)
