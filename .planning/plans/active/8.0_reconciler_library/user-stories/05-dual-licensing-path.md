# User Story 5: Commercial License Placeholder for Proprietary Embedding

**Plan:** [8.0: Reconciler Library Module Extraction](../plan.md)

## User Story

**As a** proprietary devtools vendor evaluating the reconciler as a white-label/OEM merge layer
**I want** a clear commercial licensing option alongside Apache 2.0, with a documented contact path and no enforcement code
**So that** my team can evaluate embedding the reconciler into a closed-source product with a known legal wrapper and contact before committing engineering effort

## Story Context

- **Background:** The reconciler module ships under a dual-licensing model — Apache 2.0 for open-source use and a commercial license for proprietary embedding. This story covers the commercial half of AC#6: a `reconcile/LICENSE-COMMERCIAL.md` placeholder that states commercial terms exist, names a contact path for licensing discussions, and clarifies that no automated enforcement or payment gating ships with the code. A proprietary vendor's blocker is not the library's API — it is legal clarity: they need to know, before investing integration time, that commercial embedding is an intended, supported path rather than a gray area. The placeholder is documentation-only; the actual commercial license text and legal wrapper are a future step tied to a paying licensee or design partner.
- **Assumptions:** The Apache 2.0 `reconcile/LICENSE` lands in the same story batch (or an adjacent story); this story depends on that file existing so the dual-license pairing is complete. The contact path points to a channel Sam controls (e.g., a GitHub issue template, an email, or a `LICENSE-COMMERCIAL.md` contact line) but this story does not stand up new infrastructure beyond the placeholder file. The placeholder is sufficient for evaluation — a vendor can read it and know how to initiate a commercial conversation.
- **Constraints:** No enforcement code, no license-check logic, no payment gating, and no telemetry — the epic explicitly defers automated license enforcement as out of scope. The licensing layer is documentation plus a future legal wrapper, not runtime code. The file ships inside the `reconcile/` module so a vendor cloning or importing the module sees both licenses together.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Apache 2.0 `reconcile/LICENSE` (companion file, AC#6 OSS half); module scaffold at `./reconcile/` with its own `go.mod` |

## Success Criteria (SMART Format)

- **Specific:** A `reconcile/LICENSE-COMMERCIAL.md` file exists in the extracted module, states that a commercial license is available for proprietary embedding, and names a contact path a vendor can use to initiate licensing.
- **Measurable:** The file is present at the expected path, references the Apache 2.0 dual-license pairing, contains a reachable contact path, and contains zero enforcement or payment-gating code anywhere in the module.
- **Achievable:** The deliverable is a single documentation file plus a README licensing section — no code, no infrastructure, no legal review of final commercial terms (those wait for a licensee).
- **Relevant:** Unblocks the proprietary-vendor persona and the white-label/OEM revenue line without requiring SaaS or enforcement infrastructure; completes the dual-licensing path called out in the epic's revenue model.
- **Time-bound:** Lands within the extraction sprint alongside the Apache 2.0 `LICENSE`; the file is a draft-and-review artifact, not a long-pole legal drafting effort.

## Acceptance Criteria Overview

1. `reconcile/LICENSE-COMMERCIAL.md` exists and clearly states a commercial license is available for proprietary/closed-source embedding, paired with the Apache 2.0 `LICENSE` so both halves of the dual-license are visible in the module.
2. The file names a contact path (e.g., a GitHub issue/ Discussions link or a contact line) a vendor can use to initiate commercial licensing, and is explicit that no automated enforcement or payment gating ships with the code.
3. The module README includes a short licensing section cross-referencing both files so a vendor evaluating the module discovers the commercial path without hunting for it, and the repository contains no license-check or payment-gating code.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`_

## Technical Considerations

- **Implementation Notes:** The deliverable is a single Markdown file at `reconcile/LICENSE-COMMERCIAL.md` plus a short licensing section in `reconcile/README.md`. The placeholder should state: (a) the module is dual-licensed Apache 2.0 (OSS) and commercial (proprietary embedding); (b) commercial terms are available on request via the named contact path; (c) no enforcement, license-check, or payment-gating code is present or planned as part of this extraction. Keep the contact path to a channel Sam controls and can respond to — a GitHub issue template or Discussions link is preferred over a personal email so the path survives contact with public repos. The file is documentation; it is not compiled, tested, or imported by any Go code.
- **Integration Points:** The file pairs with `reconcile/LICENSE` (Apache 2.0, the OSS half of AC#6) and is cross-referenced from the module README's licensing section. A vendor consuming the module via `github.com/samestrin/atcr/reconcile` sees both files at the module root. No Go import paths, build tags, or CI jobs are affected — the module's `reconcile-module.yml` tag-push CI (AC#7) and the PR-time `./reconcile` job in `ci.yml` validate code, not licensing text.
- **Data Requirements:** None. No schema, no runtime data, no configuration. The only "data" is the contact path string, which should be stable and owned by Sam so it does not rot between extraction and separate-repo publication.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Placeholder language overstates commercial terms and creates a legal commitment before a real license exists | Medium | Keep the file explicitly a placeholder for evaluation; state that final commercial terms are negotiated on contact, not granted by the file. Avoid drafting actual license grant language — that waits for a licensee and legal review. |
| Contact path rots (dead email, abandoned issue tracker) before a vendor reaches out, silently breaking the commercial entry point | Medium | Use a GitHub-hosted contact path (issue template or Discussions) that moves with the repository rather than a personal email; verify the path resolves during the extraction sprint. |
| Vendor reads the placeholder, expects enforcement/telemetry that would make commercial detection automatic, and is confused by the absence | Low | State plainly in the file that the project intentionally ships no enforcement or payment-gating code, and that commercial compliance is a legal-wrapper concern, not a runtime one — matching the epic's explicit out-of-scope. |
| Dual-license pairing is incomplete if the Apache 2.0 `LICENSE` lands in a separate story and slips, leaving the commercial placeholder pointing at a missing OSS license | Medium | Treat the two license files as a single batch within the sprint; this story's AC explicitly verifies both files coexist, so a missing `LICENSE` blocks this story's completion rather than shipping an orphan commercial placeholder. |

---

**Created:** June 23, 2026 11:48:36AM
**Status:** Draft - Awaiting Acceptance Criteria
