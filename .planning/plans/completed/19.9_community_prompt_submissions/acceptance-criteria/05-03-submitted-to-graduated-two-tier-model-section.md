# Acceptance Criteria: New Section Explains the `submitted` → Graduated Two-Tier Model

**Related User Story:** [05: Documentation of the Submit Flow and Two-Tier Model](../user-stories/05-documentation-of-submit-flow-and-two-tier-model.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no code) | New numbered section in `docs/personas-authoring.md`, following the file's existing numbered-section convention (`## 1.` ... `## 5.`, `## 6.` already used by Epic 19.7's bindings section) |
| Test Framework | None (docs-only) | No `go test` is added or run by this AC |
| Key Dependencies | None — pure content edit; conceptually grounded in Theme 3's `submitted`/`Source` separation and Theme 4's graduation procedure, which this section explains in plain language without duplicating their implementation detail | |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` (modify) — add a new section (e.g. "## 7. From submitted to graduated" — numbered after the existing `## 6. Model family/channel bindings and resolved locks (Epic 19.7)` section, or renumbered/placed per the maintainer's final section ordering) explaining, in plain language: `Source` stays `community` for every submission; `submitted` is a separate, orthogonal status assigned when `atcr personas submit` succeeds; graduation is a maintainer PR-merge action that promotes the persona into `personas/community/` and clears the `submitted` marker without ever changing `Source`
- `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/03-01-submitted-status-is-not-a-source-value.md` (reference only) — source of the exact invariant this section must state — `Source` only ever takes `"built-in"`, `"community"`, or `"project"`; `submitted` is tracked by a separate type/marker, never a fourth `Source` value
- `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/04-01-documented-persona-placement-and-index-entry.md` (reference only) — this AC's section states the *concept* of graduation (what it means, what axis it changes); Theme 4's "Graduating a submitted persona" section states the maintainer *procedure* (index-entry creation, file placement) — this section should link to Theme 4's section for the step-by-step mechanics rather than duplicating them
- `.planning/plans/active/19.9_community_prompt_submissions/original-requirements.md` (reference only, lines 33, 38, 98) — source of the fixed terminology this section must use verbatim — "community-contributed" for provenance, "submitted" for the unvetted status tier, "graduates"/"graduation" for the promotion action

## Design References
- [Status/Provenance Separation and Atomic Persistence](../documentation/status-provenance-and-atomic-writes.md) — the `Source`/`submitted` orthogonality this section must explain
- [Personas Install & Authoring Doc Updates (AC4)](../documentation/personas-docs-updates.md) — the high-level two-tier model content and terminology for `docs/personas-authoring.md`

## Happy Path Scenarios
**Scenario 1: A reader learns the two axes are independent**
- **Given** a reader (user or maintainer) reads the new section for the first time
- **When** they finish reading it
- **Then** they can state, without consulting source code: (a) every submission keeps `Source: community`, (b) `submitted` is a separate status assigned on successful `atcr personas submit`, and (c) a submitted persona is not yet vetted — a fixture pass proves the fixture renders and matches its expected category, not that a human has reviewed it

**Scenario 2: A reader learns what graduation does and does not change**
- **Given** a reader wants to know how a `submitted` persona becomes part of the shipped library
- **When** they read the graduation explanation in this section
- **Then** they learn graduation is a maintainer action performed via GitHub PR review/merge, that it promotes the persona file into `personas/community/` and adds/updates its `index.json` entry, that it clears the `submitted` marker, and that `Source` never changes as part of this action (linking to Theme 4's section for the exact procedural steps)

## Edge Cases
**Edge Case 1: Reader conflates "submitted" with "community-contributed"**
- **Given** a reader is used to the existing CLI help text describing personas as "community-contributed" (`cmd/atcr/personas.go:98`)
- **When** they read this new section
- **Then** the text explicitly distinguishes the two terms: "community-contributed" describes *where a persona came from* (provenance, the `Source` field), while "submitted" describes *how far along the curation pipeline it is* (an unvetted status) — and the section does not use the two terms interchangeably or introduce a third synonym

**Edge Case 2: Reader wonders whether a marketplace or hosted registry is involved**
- **Given** a reader unfamiliar with the epic's scope decisions
- **When** they read this section
- **Then** it explicitly states the entire flow — submission, review, and graduation — happens through `atcr personas submit`, `gh`, and a standard GitHub pull request, with no marketplace, website, or hosted-registry surface of any kind (per AC3 of the epic's original requirements)

**Edge Case 3: Reader asks "is a submitted persona safe to install and run?"**
- **Given** a cautious reader considering installing a `submitted`-status persona before it graduates
- **When** they read this section
- **Then** it states plainly that fixture-passing is a mechanical check (the template renders and the finding category matches), not a human security/quality review, and that graduation is the signal a maintainer has battle-tested it — consistent with the existing "Trust note" already in `docs/personas-install.md`

## Error Conditions
**Error Scenario 1: Section describes `submitted` as a fourth `Source` value**
- Error message: N/A — a content-accuracy defect, not a runtime error; caught by manual review cross-checking this section's wording against `.planning/plans/active/19.9_community_prompt_submissions/acceptance-criteria/03-01-submitted-status-is-not-a-source-value.md`'s invariant and, once Theme 3 lands, against `internal/personas/list.go`'s actual `PersonaMeta.Source` comment/value set
- HTTP status / error code: N/A

**Error Scenario 2: Section implies a marketplace, website, or hosted-registry surface exists**
- Error message: N/A — scope-accuracy defect; caught by manual review against the epic's AC3 constraint and `plan.md`'s stated objectives
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — conceptual/explanatory section; no auth flow described here (covered by AC 05-01's `gh` precondition text and the existing graduation procedure's maintainer-permissions note).
- **Input Validation:** N/A — no user input parsed; the section is prose only, and must not introduce any new terminology or synonym for "submitted"/"community-contributed" beyond what `original-requirements.md` fixes.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** The rendered `docs/personas-authoring.md`, reviewed to confirm the new section states the `Source`/`submitted` orthogonality correctly, does not conflate provenance and status terminology, explicitly denies any marketplace/hosted-registry surface, and links to Theme 4's graduation-procedure section rather than duplicating its steps.
**Mock/Stub Requirements:** None — no code or automated test harness; verification is a documentation-accuracy and terminology-consistency read-through against `original-requirements.md`'s terminology-collision resolution.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no test suite changes; pre-existing suite remains green)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] New section states `Source` stays `community` (or `project`/`built-in` as applicable) for every submission and is never set to `submitted`
- [ ] New section states `submitted` is an orthogonal status assigned on successful `atcr personas submit`
- [ ] New section states graduation is a maintainer PR-merge action promoting the persona into `personas/community/`, links to Theme 4's procedural section for the exact steps, and does not duplicate that section's step-by-step content
- [ ] New section uses "community-contributed" only for provenance and "submitted" only for status, with no interchangeable use or invented synonym, and explicitly denies any marketplace/website/hosted-registry surface

**Manual Review:**
- [ ] Code reviewed and approved
