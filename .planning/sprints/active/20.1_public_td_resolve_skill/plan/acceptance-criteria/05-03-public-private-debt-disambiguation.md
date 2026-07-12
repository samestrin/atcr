# Acceptance Criteria: Public/Local vs. Private `.planning/`-Scoped Debt Disambiguation

**Related User Story:** [05: Document Debt-Resolve in skill-usage.md](../user-stories/05-document-debt-resolve-in-skill-usage.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/skill-usage.md`, cross-linking `docs/technical-debt.md`) | An explicit, unmissable callout, not a passing mention (per the story's stated risk) |
| Test Framework | Go `testing` (doc-presence/content assertions), mirroring `internal/scorecard/docs_test.go` | Verifies the callout and cross-link both exist as literal content |
| Key Dependencies | `docs/technical-debt.md` (existing, unmodified — the private `atcr debt list/add/dashboard` reference this section links to) | Read-only reference; this AC does not modify `docs/technical-debt.md` |

### Related Files (from codebase-discovery.json)
- `docs/skill-usage.md` — modify: add an explicit disambiguation callout (e.g. a blockquote or a "Not to be confused with" subsection) contrasting the new public/local `/atcr debt resolve` + `.atcr/`-scoped store against the private `.planning/`-scoped `atcr debt list/add/dashboard` family, with a cross-link to `docs/technical-debt.md`
- `docs/technical-debt.md` — reference (read-only): the existing doc for the private pipeline's `atcr debt list/add/dashboard` commands (`.planning/technical-debt/` scope) that this callout must accurately describe and link to, without duplicating its content
- `cmd/atcr/debt.go` — reference (read-only): the actual CLI command surface — confirms whether `atcr debt resolve` is a subcommand under the same `atcr debt` command as `list`/`add`/`dashboard` (same binary verb, different scope), which the callout must state precisely rather than implying they are unrelated commands
- `cmd/atcr/debt_add.go`, `cmd/atcr/debt_dashboard.go`, `cmd/atcr/debt_list.go` — reference (read-only): existing `.planning/`-scoped subcommand implementations to contrast with the new `.atcr/`-scoped `debt resolve` subcommand
- `internal/debt/debt.go`, `internal/tdmigrate/item.go`, `internal/tdmigrate/parse.go` — reference (read-only): private-pipeline debt store parsing/aggregation backing `atcr debt list/add/dashboard`
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/cli-integration-points.md` — reference: naming collision risk and namespace-sharing guidance between public and private debt command families

## Happy Path Scenarios
**Scenario 1: Reader distinguishes the two `debt` command families before invoking either**
- **Given** a reader has used or read about the private pipeline's `atcr debt list/add/dashboard` commands (documented in `docs/technical-debt.md`) and is now reading `docs/skill-usage.md`
- **When** they reach the disambiguation callout
- **Then** they can correctly state: (a) `/atcr debt resolve` and its backing store are scoped to `.atcr/debt/` (public/standalone, no `.planning/` required), (b) `atcr debt list/add/dashboard` are scoped to `.planning/technical-debt/` (private pipeline), (c) both share the `atcr debt` command surface but read/write different, non-overlapping data, and (d) which one applies to them depends on whether their repo uses the private `.planning/` sprint workflow

**Scenario 2: Callout is unmissable, not a passing mention**
- **Given** the story's stated risk that readers conflate the two due to the shared `debt` name
- **When** the section is authored
- **Then** the callout is visually distinct (blockquote, admonition, or dedicated subsection heading — not a single inline clause buried in a paragraph) and appears near the top of the Technical Debt Resolution section, not only at the end

## Edge Cases
**Edge Case 1: Command-surface claim is accurate about shared vs. separate namespace**
- **Given** Story 3 constrains `/atcr debt resolve` to be an additive subcommand of the existing `atcr debt` row (not a new top-level dispatcher command)
- **When** the callout describes the relationship between the two families
- **Then** it correctly states they share the same `atcr debt` CLI verb/namespace but operate on entirely separate, non-overlapping stores (`.atcr/debt/` vs. `.planning/technical-debt/`) — it does not imply they are unrelated top-level commands, nor that they share data

**Edge Case 2: Cross-link resolves correctly and stays reciprocal-consistent**
- **Given** `docs/technical-debt.md` is the target of the cross-link
- **When** the link is added
- **Then** it uses the doc's existing relative-link convention (e.g. `[docs/technical-debt.md](technical-debt.md)`, matching how `docs/skill-usage.md` already links `findings-format.md` and `providers.md`), and the link resolves to an existing file

## Error Conditions
**Error Scenario 1: Disambiguation callout missing or too subtle**
- Error message (manual review finding): "docs/skill-usage.md does not clearly distinguish /atcr debt resolve (.atcr/-scoped) from atcr debt list/add/dashboard (.planning/-scoped); risk of user confusion per Story 5 Risks table"
- HTTP status / error code: N/A (documentation-quality defect, caught in manual/acceptance review — cannot be fully automated since "unmissable" is a subjective bar)

**Error Scenario 2: Cross-link is broken or missing**
- Error message (test failure): "docs/skill-usage.md missing expected cross-link to docs/technical-debt.md in the debt-resolve disambiguation section"
- HTTP status / error code: N/A (Go test failure, non-zero exit)

## Performance Requirements
- **Response Time:** N/A (static documentation)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** N/A (no user input; ensure the callout does not imply the two stores share access controls or data — they are fully separate on-disk locations with separate permission models already documented in AC 05-02 and `docs/technical-debt.md`)

## Test Implementation Guidance
**Test Type:** UNIT (doc-presence/content structural assertions, mirroring `internal/scorecard/docs_test.go`; asserts the substring `technical-debt.md` appears as a link target within the new section, and that both `.atcr/debt/` and `.planning/technical-debt/` path strings appear together near the disambiguation callout)
**Test Data Requirements:** None beyond the repository's own `docs/skill-usage.md` and `docs/technical-debt.md` files at test time
**Mock/Stub Requirements:** None — reads real files from disk; no network or link-checker dependency required (relative-path existence check suffices)

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `docs/skill-usage.md` contains an explicit, visually distinct callout contrasting `/atcr debt resolve` (`.atcr/`-scoped, public/standalone) with `atcr debt list/add/dashboard` (`.planning/`-scoped, private pipeline)
- [x] The callout correctly describes the shared `atcr debt` CLI namespace with separate, non-overlapping data stores
- [x] A working cross-link to `docs/technical-debt.md` is present
- [x] Callout placement and prominence reviewed against the story's stated confusion risk (not just a passing inline mention)

**Manual Review:**
- [ ] Code reviewed and approved
